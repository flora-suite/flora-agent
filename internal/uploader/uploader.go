// Package uploader handles file uploads to flora-server.
package uploader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/flora-suite/flora-agent/internal/api"
	"github.com/flora-suite/flora-agent/internal/retry"
	"github.com/flora-suite/flora-agent/internal/store"
)

// Uploader handles file uploads.
type Uploader interface {
	Upload(ctx context.Context, file *store.File) error
}

// ResumableUploader extends Uploader with resumable upload support.
type ResumableUploader interface {
	Uploader
	// UploadWithStore uploads a file with state persistence for resumability.
	UploadWithStore(ctx context.Context, file *store.File, st store.Store) error
}

// HTTPUploader implements Uploader using HTTP.
type HTTPUploader struct {
	client     *api.Client
	concurrent int
	chunkSize  int64
	log        zerolog.Logger
	retryCfg   retry.Config
	limiter    *bandwidthLimiter
}

// Option configures the uploader.
type Option func(*HTTPUploader)

// WithRetryConfig sets custom retry configuration.
func WithRetryConfig(cfg retry.Config) Option {
	return func(u *HTTPUploader) {
		u.retryCfg = cfg
	}
}

// WithBandwidthLimit limits aggregate payload dispatch across uploader workers.
// A limit of zero leaves uploads unrestricted.
func WithBandwidthLimit(bytesPerSecond int64) Option {
	return func(u *HTTPUploader) {
		if bytesPerSecond > 0 {
			u.limiter = &bandwidthLimiter{bytesPerSecond: bytesPerSecond}
		}
	}
}

type bandwidthLimiter struct {
	mu             sync.Mutex
	bytesPerSecond int64
	next           time.Time
}

func (l *bandwidthLimiter) Wait(ctx context.Context, size int64) error {
	if l == nil || size <= 0 {
		return nil
	}
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	start := l.next
	l.next = l.next.Add(time.Duration(float64(size) / float64(l.bytesPerSecond) * float64(time.Second)))
	l.mu.Unlock()

	if delay := time.Until(start); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

// New creates a new HTTPUploader.
func New(client *api.Client, concurrent int, chunkSize int64, log zerolog.Logger, opts ...Option) *HTTPUploader {
	u := &HTTPUploader{
		client:     client,
		concurrent: concurrent,
		chunkSize:  chunkSize,
		log:        log,
		retryCfg: retry.Config{
			MaxAttempts:  5,
			InitialDelay: 1 * time.Second,
			MaxDelay:     60 * time.Second,
			Multiplier:   2.0,
			Jitter:       0.1,
		},
	}

	for _, opt := range opts {
		opt(u)
	}

	return u
}

// Upload uploads a file to flora-server.
func (u *HTTPUploader) Upload(ctx context.Context, file *store.File) error {
	f, err := os.Open(file.Path)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	// Use simple upload for small files
	if info.Size() <= u.chunkSize {
		return u.simpleUpload(ctx, file, f)
	}

	// Use multipart upload for large files
	return u.multipartUpload(ctx, file, f, info.Size())
}

func (u *HTTPUploader) simpleUpload(ctx context.Context, file *store.File, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	if err := u.waitForBandwidth(ctx, int64(len(data))); err != nil {
		return err
	}

	var resp *api.SimpleUploadResponse
	err = retry.Do(ctx, u.retryCfg, func(ctx context.Context) error {
		var uploadErr error
		resp, uploadErr = u.client.SimpleUpload(ctx, filepath.Base(file.Path), data, file.Checksum)
		if uploadErr != nil {
			if isRetryableError(uploadErr) {
				u.log.Warn().Err(uploadErr).Str("path", file.Path).Msg("upload failed, will retry")
				return retry.Retryable(uploadErr)
			}
			return retry.NonRetryable(uploadErr)
		}
		return nil
	})

	if err != nil {
		return err
	}

	u.log.Info().
		Str("path", file.Path).
		Str("recordingId", resp.RecordingID).
		Msg("simple upload completed")

	return nil
}

// UploadWithStore uploads a file with state persistence for resumability.
func (u *HTTPUploader) UploadWithStore(ctx context.Context, file *store.File, st store.Store) error {
	f, err := os.Open(file.Path)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	// Use simple upload for small files (no need for resumability)
	if info.Size() <= u.chunkSize {
		return u.simpleUpload(ctx, file, f)
	}

	// Use resumable multipart upload for large files
	return u.resumableMultipartUpload(ctx, file, f, info.Size(), st)
}

// resumableMultipartUpload handles multipart upload with state persistence.
func (u *HTTPUploader) resumableMultipartUpload(ctx context.Context, file *store.File, f *os.File, size int64, st store.Store) error {
	var uploadID string
	var chunkSize int64
	var totalChunks int
	var uploadedChunks map[int]string // chunk index -> etag

	// Check if there's a previous upload to resume
	if file.UploadID != "" {
		u.log.Info().
			Str("path", file.Path).
			Str("uploadId", file.UploadID).
			Msg("attempting to resume previous upload")

		status, err := u.client.GetUploadStatus(ctx, file.UploadID)
		if err == nil && status != nil {
			// Previous upload exists and is still valid
			uploadID = status.UploadID
			chunkSize = status.ChunkSize
			totalChunks = status.TotalChunks

			// Build map of already uploaded chunks
			uploadedChunks = make(map[int]string)
			existingChunks, err := st.GetChunks(file.ID)
			if err == nil {
				for _, chunk := range existingChunks {
					if chunk.Uploaded && chunk.ETag != "" {
						uploadedChunks[chunk.ChunkIndex] = chunk.ETag
					}
				}
			}

			u.log.Info().
				Str("uploadId", uploadID).
				Int("uploadedChunks", len(uploadedChunks)).
				Int("totalChunks", totalChunks).
				Msg("resuming upload")
		} else {
			// Previous upload no longer valid, clean up
			u.log.Warn().
				Str("uploadId", file.UploadID).
				Msg("previous upload not found or expired, starting fresh")
			_ = st.DeleteChunks(file.ID)
			file.UploadID = ""
		}
	}

	// Initialize new upload if needed
	if uploadID == "" {
		var metadata *api.RecordingMetadata
		if file.Metadata != nil {
			metadata = convertMetadata(file.Metadata)
		}

		initReq := &api.InitMultipartUploadRequest{
			Filename: filepath.Base(file.Path),
			Size:     size,
			Checksum: file.Checksum,
			FileType: file.FileType,
			Metadata: metadata,
		}

		var initResp *api.InitMultipartUploadResponse
		err := retry.Do(ctx, u.retryCfg, func(ctx context.Context) error {
			var initErr error
			initResp, initErr = u.client.InitMultipartUpload(ctx, initReq)
			if initErr != nil {
				if isRetryableError(initErr) {
					u.log.Warn().Err(initErr).Str("path", file.Path).Msg("init multipart upload failed, will retry")
					return retry.Retryable(initErr)
				}
				return retry.NonRetryable(initErr)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to init multipart upload: %w", err)
		}

		uploadID = initResp.UploadID
		chunkSize = initResp.ChunkSize
		if chunkSize == 0 {
			chunkSize = u.chunkSize
		}
		totalChunks = initResp.TotalChunks
		uploadedChunks = make(map[int]string)

		// Store upload ID for future resume
		file.UploadID = uploadID
		if err := st.UpsertFile(file); err != nil {
			u.log.Warn().Err(err).Msg("failed to save upload ID to store")
		}

		// Create chunk entries in store
		for i := 0; i < totalChunks; i++ {
			offset := int64(i) * chunkSize
			remaining := size - offset
			currentChunkSize := chunkSize
			if remaining < chunkSize {
				currentChunkSize = remaining
			}

			chunk := &store.UploadChunk{
				FileID:     file.ID,
				ChunkIndex: i,
				Offset:     offset,
				Size:       currentChunkSize,
				Uploaded:   false,
			}
			if err := st.UpsertChunk(chunk); err != nil {
				u.log.Warn().Err(err).Int("chunk", i).Msg("failed to save chunk info to store")
			}
		}

		u.log.Info().
			Str("path", file.Path).
			Str("uploadId", uploadID).
			Int("totalChunks", totalChunks).
			Msg("started new multipart upload")
	}

	// Upload remaining chunks
	var parts []api.UploadPart

	// First, add already uploaded chunks to parts
	for i := 0; i < totalChunks; i++ {
		if etag, ok := uploadedChunks[i]; ok {
			parts = append(parts, api.UploadPart{
				Index: i,
				ETag:  etag,
			})
		}
	}

	// Upload chunks that haven't been uploaded yet
	for i := 0; i < totalChunks; i++ {
		if _, ok := uploadedChunks[i]; ok {
			// Already uploaded
			continue
		}

		select {
		case <-ctx.Done():
			// Don't abort - allow future resume
			return ctx.Err()
		default:
		}

		offset := int64(i) * chunkSize
		remaining := size - offset
		currentChunkSize := chunkSize
		if remaining < chunkSize {
			currentChunkSize = remaining
		}

		// Read chunk
		chunk := make([]byte, currentChunkSize)
		n, err := f.ReadAt(chunk, offset)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read chunk %d: %w", i, err)
		}
		chunk = chunk[:n]
		if err := u.waitForBandwidth(ctx, int64(len(chunk))); err != nil {
			return err
		}

		// Upload chunk with retry
		var chunkResp *api.UploadChunkResponse
		chunkIdx := i
		err = retry.Do(ctx, u.retryCfg, func(ctx context.Context) error {
			var chunkErr error
			chunkResp, chunkErr = u.client.UploadChunk(ctx, uploadID, chunkIdx, chunk)
			if chunkErr != nil {
				if isRetryableError(chunkErr) {
					u.log.Warn().Err(chunkErr).
						Str("uploadId", uploadID).
						Int("chunk", chunkIdx).
						Msg("chunk upload failed, will retry")
					return retry.Retryable(chunkErr)
				}
				return retry.NonRetryable(chunkErr)
			}
			return nil
		})
		if err != nil {
			// Don't abort - allow future resume
			return fmt.Errorf("failed to upload chunk %d: %w", i, err)
		}

		// Mark chunk as uploaded in store
		storeChunk := &store.UploadChunk{
			FileID:     file.ID,
			ChunkIndex: i,
			Offset:     offset,
			Size:       currentChunkSize,
			Uploaded:   true,
			ETag:       chunkResp.ETag,
		}
		if err := st.UpsertChunk(storeChunk); err != nil {
			u.log.Warn().Err(err).Int("chunk", i).Msg("failed to save chunk progress to store")
		}

		parts = append(parts, api.UploadPart{
			Index: i,
			ETag:  chunkResp.ETag,
		})

		u.log.Debug().
			Str("uploadId", uploadID).
			Int("chunk", i).
			Int("totalChunks", totalChunks).
			Int("completedChunks", len(parts)).
			Msg("uploaded chunk")
	}

	// Sort parts by index (they should already be sorted, but ensure consistency)
	sortParts(parts)

	// Complete upload with retry
	var completeResp *api.CompleteMultipartUploadResponse
	err := retry.Do(ctx, u.retryCfg, func(ctx context.Context) error {
		var completeErr error
		completeResp, completeErr = u.client.CompleteMultipartUpload(ctx, uploadID, parts)
		if completeErr != nil {
			if isRetryableError(completeErr) {
				u.log.Warn().Err(completeErr).
					Str("uploadId", uploadID).
					Msg("complete multipart upload failed, will retry")
				return retry.Retryable(completeErr)
			}
			return retry.NonRetryable(completeErr)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to complete multipart upload: %w", err)
	}

	// Clean up chunk records from store
	if err := st.DeleteChunks(file.ID); err != nil {
		u.log.Warn().Err(err).Msg("failed to clean up chunk records from store")
	}

	// Clear upload ID from file
	file.UploadID = ""
	if err := st.UpsertFile(file); err != nil {
		u.log.Warn().Err(err).Msg("failed to clear upload ID from store")
	}

	u.log.Info().
		Str("path", file.Path).
		Str("recordingId", completeResp.RecordingID).
		Str("url", completeResp.URL).
		Msg("multipart upload completed")

	return nil
}

// sortParts sorts upload parts by index (insertion sort for small arrays).
func sortParts(parts []api.UploadPart) {
	for i := 1; i < len(parts); i++ {
		key := parts[i]
		j := i - 1
		for j >= 0 && parts[j].Index > key.Index {
			parts[j+1] = parts[j]
			j--
		}
		parts[j+1] = key
	}
}

func (u *HTTPUploader) multipartUpload(ctx context.Context, file *store.File, f *os.File, size int64) error {
	// Initialize multipart upload
	var metadata *api.RecordingMetadata
	if file.Metadata != nil {
		metadata = convertMetadata(file.Metadata)
	}

	initReq := &api.InitMultipartUploadRequest{
		Filename: filepath.Base(file.Path),
		Size:     size,
		Checksum: file.Checksum,
		FileType: file.FileType,
		Metadata: metadata,
	}

	var initResp *api.InitMultipartUploadResponse
	err := retry.Do(ctx, u.retryCfg, func(ctx context.Context) error {
		var initErr error
		initResp, initErr = u.client.InitMultipartUpload(ctx, initReq)
		if initErr != nil {
			if isRetryableError(initErr) {
				u.log.Warn().Err(initErr).Str("path", file.Path).Msg("init multipart upload failed, will retry")
				return retry.Retryable(initErr)
			}
			return retry.NonRetryable(initErr)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to init multipart upload: %w", err)
	}

	u.log.Info().
		Str("path", file.Path).
		Str("uploadId", initResp.UploadID).
		Int("totalChunks", initResp.TotalChunks).
		Msg("started multipart upload")

	// Upload chunks
	chunkSize := initResp.ChunkSize
	if chunkSize == 0 {
		chunkSize = u.chunkSize
	}

	var parts []api.UploadPart

	for i := 0; i < initResp.TotalChunks; i++ {
		select {
		case <-ctx.Done():
			// Abort upload on cancellation
			_ = u.client.AbortMultipartUpload(context.Background(), initResp.UploadID)
			return ctx.Err()
		default:
		}

		offset := int64(i) * chunkSize
		remaining := size - offset
		currentChunkSize := chunkSize
		if remaining < chunkSize {
			currentChunkSize = remaining
		}

		// Read chunk
		chunk := make([]byte, currentChunkSize)
		n, err := f.ReadAt(chunk, offset)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read chunk %d: %w", i, err)
		}
		chunk = chunk[:n]
		if err := u.waitForBandwidth(ctx, int64(len(chunk))); err != nil {
			_ = u.client.AbortMultipartUpload(context.Background(), initResp.UploadID)
			return err
		}

		// Upload chunk with retry
		var chunkResp *api.UploadChunkResponse
		chunkIdx := i
		err = retry.Do(ctx, u.retryCfg, func(ctx context.Context) error {
			var chunkErr error
			chunkResp, chunkErr = u.client.UploadChunk(ctx, initResp.UploadID, chunkIdx, chunk)
			if chunkErr != nil {
				if isRetryableError(chunkErr) {
					u.log.Warn().Err(chunkErr).
						Str("uploadId", initResp.UploadID).
						Int("chunk", chunkIdx).
						Msg("chunk upload failed, will retry")
					return retry.Retryable(chunkErr)
				}
				return retry.NonRetryable(chunkErr)
			}
			return nil
		})
		if err != nil {
			// Abort on chunk failure
			_ = u.client.AbortMultipartUpload(context.Background(), initResp.UploadID)
			return fmt.Errorf("failed to upload chunk %d: %w", i, err)
		}

		parts = append(parts, api.UploadPart{
			Index: i,
			ETag:  chunkResp.ETag,
		})

		u.log.Debug().
			Str("uploadId", initResp.UploadID).
			Int("chunk", i).
			Int("totalChunks", initResp.TotalChunks).
			Msg("uploaded chunk")
	}

	// Complete upload with retry
	var completeResp *api.CompleteMultipartUploadResponse
	err = retry.Do(ctx, u.retryCfg, func(ctx context.Context) error {
		var completeErr error
		completeResp, completeErr = u.client.CompleteMultipartUpload(ctx, initResp.UploadID, parts)
		if completeErr != nil {
			if isRetryableError(completeErr) {
				u.log.Warn().Err(completeErr).
					Str("uploadId", initResp.UploadID).
					Msg("complete multipart upload failed, will retry")
				return retry.Retryable(completeErr)
			}
			return retry.NonRetryable(completeErr)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to complete multipart upload: %w", err)
	}

	u.log.Info().
		Str("path", file.Path).
		Str("recordingId", completeResp.RecordingID).
		Str("url", completeResp.URL).
		Msg("multipart upload completed")

	return nil
}

func (u *HTTPUploader) waitForBandwidth(ctx context.Context, size int64) error {
	return u.limiter.Wait(ctx, size)
}

func convertMetadata(m *store.FileMetadata) *api.RecordingMetadata {
	if m == nil {
		return nil
	}

	result := &api.RecordingMetadata{
		Duration:     m.Duration,
		MessageCount: m.MessageCount,
	}

	if m.StartTime != nil {
		result.StartTime = m.StartTime.UTC().Format("2006-01-02T15:04:05Z")
	}
	if m.EndTime != nil {
		result.EndTime = m.EndTime.UTC().Format("2006-01-02T15:04:05Z")
	}

	for _, t := range m.Topics {
		result.Topics = append(result.Topics, api.TopicInfo{
			Name:         t.Name,
			Type:         t.Type,
			MessageCount: t.MessageCount,
		})
	}

	return result
}

// isRetryableError determines if an error is retryable.
// Network errors, timeouts, and 5xx status codes are retryable.
// 4xx status codes (except 408, 429) are not retryable.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for network errors
	var netErr net.Error
	if ok := errors.As(err, &netErr); ok {
		return true // Network errors are retryable
	}

	// Check for API errors with status codes
	var apiErr *api.Error
	if ok := errors.As(err, &apiErr); ok {
		switch {
		case apiErr.StatusCode >= 500:
			// Server errors are retryable
			return true
		case apiErr.StatusCode == http.StatusRequestTimeout:
			// Request timeout is retryable
			return true
		case apiErr.StatusCode == http.StatusTooManyRequests:
			// Rate limited - retryable
			return true
		case apiErr.StatusCode >= 400 && apiErr.StatusCode < 500:
			// Other client errors are not retryable
			return false
		}
	}

	// Check for context errors
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		// Context errors should not be retried
		return false
	}

	// Check for common transient error patterns in error messages
	errStr := strings.ToLower(err.Error())
	transientPatterns := []string{
		"connection reset",
		"connection refused",
		"timeout",
		"temporary failure",
		"no such host",
		"eof",
		"broken pipe",
	}
	for _, pattern := range transientPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	// Default: not retryable
	return false
}
