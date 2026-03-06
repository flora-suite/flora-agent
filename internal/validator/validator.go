// Package validator provides file validation for recording files.
package validator

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/foxglove/mcap/go/mcap"
	"github.com/rs/zerolog"
	_ "modernc.org/sqlite" // SQLite driver

	"github.com/flora-suite/flora-agent/internal/store"
)

// Result contains the validation result for a file.
type Result struct {
	Valid    bool
	FileType string
	Checksum string
	Metadata *store.FileMetadata
	Error    error
}

// Validator validates recording files.
type Validator interface {
	Validate(path string) (*Result, error)
}

// FileValidator implements Validator for MCAP, bag, and db3 files.
type FileValidator struct {
	log zerolog.Logger
}

// New creates a new FileValidator.
func New(log zerolog.Logger) *FileValidator {
	return &FileValidator{log: log}
}

// Validate validates a file and extracts metadata.
func (v *FileValidator) Validate(path string) (*Result, error) {
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".mcap":
		return v.validateMCAP(path)
	case ".bag":
		return v.validateBag(path)
	case ".db3":
		return v.validateDB3(path)
	default:
		return nil, fmt.Errorf("unsupported file type: %s", ext)
	}
}

// validateMCAP validates an MCAP file.
func (v *FileValidator) validateMCAP(path string) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Get file info for size
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	// Check magic bytes at start
	magic := make([]byte, 8)
	if _, err := f.Read(magic); err != nil {
		return nil, fmt.Errorf("failed to read magic bytes: %w", err)
	}

	// MCAP magic: 0x89, 'M', 'C', 'A', 'P', 0x30, '\r', '\n'
	expectedMagic := []byte{0x89, 'M', 'C', 'A', 'P', 0x30, '\r', '\n'}
	if string(magic) != string(expectedMagic) {
		return &Result{Valid: false, Error: fmt.Errorf("invalid MCAP magic bytes")}, nil
	}

	// Check footer magic at end
	if info.Size() >= 8 {
		if _, err := f.Seek(-8, io.SeekEnd); err != nil {
			return nil, err
		}
		footerMagic := make([]byte, 8)
		if _, err := f.Read(footerMagic); err != nil {
			return nil, err
		}
		// MCAP footer magic: same as header
		if string(footerMagic) != string(expectedMagic) {
			return &Result{Valid: false, Error: fmt.Errorf("invalid MCAP footer (file may be incomplete)")}, nil
		}
	}

	// Reset to beginning for full parsing
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	// Parse MCAP to extract metadata
	reader, err := mcap.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("failed to create MCAP reader: %w", err)
	}
	defer reader.Close()

	// Get summary info
	mcapInfo, err := reader.Info()
	if err != nil {
		v.log.Warn().Err(err).Str("path", path).Msg("failed to read MCAP info, continuing with basic validation")
	}

	metadata := &store.FileMetadata{}

	if mcapInfo != nil {
		// Extract statistics
		if mcapInfo.Statistics != nil {
			metadata.MessageCount = int64(mcapInfo.Statistics.MessageCount)

			// Extract time range (timestamps are in nanoseconds)
			if mcapInfo.Statistics.MessageStartTime > 0 {
				startTime := time.Unix(0, int64(mcapInfo.Statistics.MessageStartTime))
				metadata.StartTime = &startTime
			}
			if mcapInfo.Statistics.MessageEndTime > 0 {
				endTime := time.Unix(0, int64(mcapInfo.Statistics.MessageEndTime))
				metadata.EndTime = &endTime
			}
			if metadata.StartTime != nil && metadata.EndTime != nil {
				metadata.Duration = metadata.EndTime.Sub(*metadata.StartTime).Seconds()
			}
		}

		// Extract topics from channels
		for _, channel := range mcapInfo.Channels {
			var schema *mcap.Schema
			if mcapInfo.Schemas != nil {
				schema = mcapInfo.Schemas[channel.SchemaID]
			}

			topicType := ""
			if schema != nil {
				topicType = schema.Name
			}

			metadata.Topics = append(metadata.Topics, store.TopicInfo{
				Name:         channel.Topic,
				Type:         topicType,
				MessageCount: 0, // Per-topic count not easily available from Info()
			})
		}
	}

	// Calculate checksum
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	checksum, err := calculateChecksum(f)
	if err != nil {
		return nil, err
	}

	return &Result{
		Valid:    true,
		FileType: "mcap",
		Checksum: checksum,
		Metadata: metadata,
	}, nil
}

// validateBag validates a ROS 1 bag file.
func (v *FileValidator) validateBag(path string) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Check ROS bag magic: "#ROSBAG V2.0\n"
	magic := make([]byte, 13)
	if _, err := f.Read(magic); err != nil {
		return nil, fmt.Errorf("failed to read magic bytes: %w", err)
	}

	if !strings.HasPrefix(string(magic), "#ROSBAG V") {
		return &Result{Valid: false, Error: fmt.Errorf("invalid ROS bag magic bytes")}, nil
	}

	// Extract metadata from bag file
	metadata, err := v.extractBagMetadata(f)
	if err != nil {
		v.log.Warn().Err(err).Str("path", path).Msg("failed to extract bag metadata, continuing with basic validation")
		metadata = &store.FileMetadata{}
	}

	// Calculate checksum
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	checksum, err := calculateChecksum(f)
	if err != nil {
		return nil, err
	}

	return &Result{
		Valid:    true,
		FileType: "bag",
		Checksum: checksum,
		Metadata: metadata,
	}, nil
}

// extractBagMetadata extracts metadata from a ROS 1 bag file.
// ROS bag format: https://wiki.ros.org/Bags/Format/2.0
func (v *FileValidator) extractBagMetadata(f *os.File) (*store.FileMetadata, error) {
	// Seek back to start after magic
	if _, err := f.Seek(13, io.SeekStart); err != nil {
		return nil, err
	}

	metadata := &store.FileMetadata{}
	topics := make(map[string]*store.TopicInfo)
	var startTime, endTime *time.Time

	// Read records until EOF or we have enough info
	for {
		record, err := readBagRecord(f)
		if err == io.EOF {
			break
		}
		if err != nil {
			// Stop parsing on error but return what we have
			v.log.Debug().Err(err).Msg("bag parsing stopped")
			break
		}

		switch record.op {
		case bagOpConnection:
			// Connection record contains topic info
			topic, topicType := parseBagConnection(record.data)
			if topic != "" {
				if _, exists := topics[topic]; !exists {
					topics[topic] = &store.TopicInfo{
						Name:         topic,
						Type:         topicType,
						MessageCount: 0,
					}
				}
			}

		case bagOpChunkInfo:
			// Chunk info contains connection counts and time range
			chunkStartTime, chunkEndTime, connCounts := parseBagChunkInfo(record.data)
			if chunkStartTime != nil {
				if startTime == nil || chunkStartTime.Before(*startTime) {
					startTime = chunkStartTime
				}
			}
			if chunkEndTime != nil {
				if endTime == nil || chunkEndTime.After(*endTime) {
					endTime = chunkEndTime
				}
			}
			// Add message counts per connection
			for _, count := range connCounts {
				metadata.MessageCount += int64(count)
			}
		}
	}

	// Convert topics map to slice
	for _, ti := range topics {
		metadata.Topics = append(metadata.Topics, *ti)
	}

	// Set time info
	if startTime != nil {
		metadata.StartTime = startTime
	}
	if endTime != nil {
		metadata.EndTime = endTime
	}
	if startTime != nil && endTime != nil {
		metadata.Duration = endTime.Sub(*startTime).Seconds()
	}

	return metadata, nil
}

// Bag record operation types
const (
	bagOpMsgData    byte = 0x02
	bagOpBagHeader  byte = 0x03
	bagOpIndexData  byte = 0x04
	bagOpChunk      byte = 0x05
	bagOpChunkInfo  byte = 0x06
	bagOpConnection byte = 0x07
)

// bagRecord represents a record in a ROS bag file.
type bagRecord struct {
	op   byte
	data []byte
}

// readBagRecord reads a single record from a bag file.
func readBagRecord(r io.Reader) (*bagRecord, error) {
	// Read header length (4 bytes, little endian)
	var headerLen uint32
	if err := binary.Read(r, binary.LittleEndian, &headerLen); err != nil {
		return nil, err
	}

	// Sanity check - header shouldn't be too large
	if headerLen > 100*1024*1024 {
		return nil, fmt.Errorf("header length too large: %d", headerLen)
	}

	// Read header
	header := make([]byte, headerLen)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	// Parse header fields to find 'op'
	op := byte(0)
	fields := parseBagFields(header)
	if opData, ok := fields["op"]; ok && len(opData) == 1 {
		op = opData[0]
	}

	// Read data length (4 bytes, little endian)
	var dataLen uint32
	if err := binary.Read(r, binary.LittleEndian, &dataLen); err != nil {
		return nil, err
	}

	// Sanity check
	if dataLen > 1024*1024*1024 {
		return nil, fmt.Errorf("data length too large: %d", dataLen)
	}

	// For chunk data, we skip reading the full data to save memory
	if op == bagOpChunk || op == bagOpMsgData {
		// Skip the data
		if _, err := io.CopyN(io.Discard, r, int64(dataLen)); err != nil {
			return nil, err
		}
		return &bagRecord{op: op, data: nil}, nil
	}

	// Read data
	data := make([]byte, dataLen)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}

	return &bagRecord{op: op, data: data}, nil
}

// parseBagFields parses bag header/data fields.
// Format: field_len (4 bytes) | name=value | field_len | name=value | ...
func parseBagFields(data []byte) map[string][]byte {
	fields := make(map[string][]byte)
	r := bytes.NewReader(data)

	for r.Len() > 0 {
		var fieldLen uint32
		if err := binary.Read(r, binary.LittleEndian, &fieldLen); err != nil {
			break
		}

		if fieldLen > uint32(r.Len()) {
			break
		}

		field := make([]byte, fieldLen)
		if _, err := r.Read(field); err != nil {
			break
		}

		// Find '=' separator
		eqIdx := bytes.IndexByte(field, '=')
		if eqIdx > 0 {
			name := string(field[:eqIdx])
			value := field[eqIdx+1:]
			fields[name] = value
		}
	}

	return fields
}

// parseBagConnection parses a connection record to extract topic and type.
func parseBagConnection(data []byte) (topic, msgType string) {
	fields := parseBagFields(data)

	if topicData, ok := fields["topic"]; ok {
		topic = string(topicData)
	}
	if typeData, ok := fields["type"]; ok {
		msgType = string(typeData)
	}

	return topic, msgType
}

// parseBagChunkInfo parses a chunk info record.
func parseBagChunkInfo(data []byte) (startTime, endTime *time.Time, connCounts map[uint32]uint32) {
	connCounts = make(map[uint32]uint32)

	if len(data) < 16 {
		return nil, nil, connCounts
	}

	r := bytes.NewReader(data)

	// Read version (4 bytes)
	var ver uint32
	binary.Read(r, binary.LittleEndian, &ver)

	// Read chunk position (8 bytes)
	var chunkPos uint64
	binary.Read(r, binary.LittleEndian, &chunkPos)

	// Read start time (8 bytes: sec + nsec)
	var startSec, startNsec uint32
	binary.Read(r, binary.LittleEndian, &startSec)
	binary.Read(r, binary.LittleEndian, &startNsec)
	if startSec > 0 {
		t := time.Unix(int64(startSec), int64(startNsec))
		startTime = &t
	}

	// Read end time (8 bytes: sec + nsec)
	var endSec, endNsec uint32
	binary.Read(r, binary.LittleEndian, &endSec)
	binary.Read(r, binary.LittleEndian, &endNsec)
	if endSec > 0 {
		t := time.Unix(int64(endSec), int64(endNsec))
		endTime = &t
	}

	// Read connection count (4 bytes)
	var connCount uint32
	binary.Read(r, binary.LittleEndian, &connCount)

	// Read connection info entries
	for i := uint32(0); i < connCount && r.Len() >= 8; i++ {
		var connID, count uint32
		binary.Read(r, binary.LittleEndian, &connID)
		binary.Read(r, binary.LittleEndian, &count)
		connCounts[connID] = count
	}

	return startTime, endTime, connCounts
}

// validateDB3 validates a ROS 2 db3 (SQLite) file.
func (v *FileValidator) validateDB3(path string) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Check SQLite magic: "SQLite format 3\x00"
	magic := make([]byte, 16)
	if _, err := f.Read(magic); err != nil {
		return nil, fmt.Errorf("failed to read magic bytes: %w", err)
	}

	if !strings.HasPrefix(string(magic), "SQLite format 3") {
		return &Result{Valid: false, Error: fmt.Errorf("invalid SQLite/db3 magic bytes")}, nil
	}

	// Calculate checksum
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	checksum, err := calculateChecksum(f)
	if err != nil {
		return nil, err
	}

	// Extract metadata from db3 file
	metadata, err := v.extractDB3Metadata(path)
	if err != nil {
		v.log.Warn().Err(err).Str("path", path).Msg("failed to extract db3 metadata, continuing with basic validation")
		metadata = &store.FileMetadata{}
	}

	return &Result{
		Valid:    true,
		FileType: "db3",
		Checksum: checksum,
		Metadata: metadata,
	}, nil
}

// extractDB3Metadata extracts metadata from a ROS 2 db3 (SQLite) file.
// ROS 2 bag db3 schema: https://github.com/ros2/rosbag2/blob/rolling/rosbag2_storage_default_plugins/src/rosbag2_storage_default_plugins/sqlite/sqlite_storage.cpp
func (v *FileValidator) extractDB3Metadata(path string) (*store.FileMetadata, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("failed to open db3 file: %w", err)
	}
	defer db.Close()

	metadata := &store.FileMetadata{}

	// Extract topics from the topics table
	// ROS 2 db3 schema: topics(id, name, type, serialization_format, offered_qos_profiles)
	topicsQuery := `
		SELECT t.id, t.name, t.type, COUNT(m.id) as message_count
		FROM topics t
		LEFT JOIN messages m ON t.id = m.topic_id
		GROUP BY t.id, t.name, t.type
	`
	rows, err := db.Query(topicsQuery)
	if err != nil {
		// Try alternative query without message count (older schema)
		v.log.Debug().Err(err).Msg("failed to query topics with message count, trying simple query")
		rows, err = db.Query("SELECT id, name, type FROM topics")
		if err != nil {
			return nil, fmt.Errorf("failed to query topics: %w", err)
		}
	}
	defer rows.Close()

	topicIDs := make(map[int64]int) // topic_id -> index in Topics slice
	for rows.Next() {
		var topicID int64
		var name, topicType string
		var messageCount sql.NullInt64

		// Try to scan with message count first
		if err := rows.Scan(&topicID, &name, &topicType, &messageCount); err != nil {
			// Fall back to scanning without message count
			if err := rows.Scan(&topicID, &name, &topicType); err != nil {
				v.log.Debug().Err(err).Msg("failed to scan topic row")
				continue
			}
		}

		topicIDs[topicID] = len(metadata.Topics)
		metadata.Topics = append(metadata.Topics, store.TopicInfo{
			Name:         name,
			Type:         topicType,
			MessageCount: messageCount.Int64,
		})
	}

	// Get time range from messages table
	// ROS 2 db3 schema: messages(id, topic_id, timestamp, data)
	// timestamp is in nanoseconds
	var minTimestamp, maxTimestamp sql.NullInt64
	err = db.QueryRow("SELECT MIN(timestamp), MAX(timestamp) FROM messages").Scan(&minTimestamp, &maxTimestamp)
	if err != nil {
		v.log.Debug().Err(err).Msg("failed to query message timestamps")
	} else {
		if minTimestamp.Valid {
			t := time.Unix(0, minTimestamp.Int64)
			metadata.StartTime = &t
		}
		if maxTimestamp.Valid {
			t := time.Unix(0, maxTimestamp.Int64)
			metadata.EndTime = &t
		}
		if metadata.StartTime != nil && metadata.EndTime != nil {
			metadata.Duration = metadata.EndTime.Sub(*metadata.StartTime).Seconds()
		}
	}

	// Get total message count
	var totalMessages sql.NullInt64
	err = db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&totalMessages)
	if err != nil {
		v.log.Debug().Err(err).Msg("failed to query total message count")
	} else {
		metadata.MessageCount = totalMessages.Int64
	}

	return metadata, nil
}

// calculateChecksum computes SHA256 checksum of a file.
func calculateChecksum(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
