//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flora-suite/flora-agent/internal/api"
	"github.com/flora-suite/flora-agent/internal/store"
	"github.com/flora-suite/flora-agent/internal/uploader"
	"github.com/flora-suite/flora-agent/internal/validator"
	"github.com/flora-suite/flora-agent/internal/watcher"
)

// TestEndToEnd_FileDiscoveryToUpload tests the complete flow from file discovery to upload.
func TestEndToEnd_FileDiscoveryToUpload(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	watchDir := filepath.Join(tmpDir, "recordings")
	dbPath := filepath.Join(tmpDir, "agent.db")

	err := os.MkdirAll(watchDir, 0755)
	require.NoError(t, err)

	// Track uploaded files
	var mu sync.Mutex
	uploadedFiles := make(map[string]bool)

	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/recordings/upload":
			filename := r.Header.Get("X-Filename")
			mu.Lock()
			uploadedFiles[filename] = true
			mu.Unlock()

			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{
				"recordingId": "rec-" + filename,
			})

		case "/api/devices/agent/heartbeat":
			json.NewEncoder(w).Encode(api.HeartbeatResponse{
				Ack:        true,
				ServerTime: time.Now().Format(time.RFC3339),
			})

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	log := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()

	// Initialize components
	st, err := store.NewSQLite(dbPath)
	require.NoError(t, err)
	defer st.Close()

	client := api.NewClient(server.URL, "test-token")
	v := validator.New(log)
	u := uploader.New(client, 2, 10*1024*1024, log)

	w, err := watcher.New(
		[]string{watchDir},
		[]string{"*.mcap", "*.bag"},
		[]string{"*.active", "*.tmp"},
		log,
	)
	require.NoError(t, err)

	// Start watcher
	err = w.Start()
	require.NoError(t, err)
	defer w.Stop()

	// Create a test MCAP file
	mcapPath := filepath.Join(watchDir, "test_recording.mcap")
	err = createTestMCAPFile(mcapPath)
	require.NoError(t, err)

	// Wait for file to be detected
	time.Sleep(200 * time.Millisecond)

	// Scan for files (simulating periodic scan)
	files, err := w.Scan()
	require.NoError(t, err)
	require.Len(t, files, 1)

	// Process the file through the pipeline
	for _, filePath := range files {
		// Get file info
		info, err := os.Stat(filePath)
		require.NoError(t, err)

		// Create store record
		file := &store.File{
			Path:     filePath,
			Size:     info.Size(),
			MTime:    info.ModTime().Unix(),
			State:    store.StateDiscovered,
			FileType: "mcap",
		}
		err = st.UpsertFile(file)
		require.NoError(t, err)

		// Validate
		result, err := v.Validate(filePath)
		require.NoError(t, err)
		assert.True(t, result.Valid)

		file.State = store.StateValidated
		file.Checksum = result.Checksum
		file.Metadata = result.Metadata
		err = st.UpsertFile(file)
		require.NoError(t, err)

		// Upload
		file.State = store.StateUploading
		err = st.UpsertFile(file)
		require.NoError(t, err)

		err = u.Upload(context.Background(), file)
		require.NoError(t, err)

		file.State = store.StateUploaded
		err = st.UpsertFile(file)
		require.NoError(t, err)
	}

	// Verify upload was received
	mu.Lock()
	assert.True(t, uploadedFiles["test_recording.mcap"])
	mu.Unlock()

	// Verify store state
	uploaded, err := st.GetFilesByState(store.StateUploaded)
	require.NoError(t, err)
	assert.Len(t, uploaded, 1)
}

// TestEndToEnd_MultipleFiles tests processing multiple files concurrently.
func TestEndToEnd_MultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()
	watchDir := filepath.Join(tmpDir, "recordings")
	dbPath := filepath.Join(tmpDir, "agent.db")

	err := os.MkdirAll(watchDir, 0755)
	require.NoError(t, err)

	var mu sync.Mutex
	uploadedFiles := make(map[string]bool)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/recordings/upload" {
			filename := r.Header.Get("X-Filename")
			mu.Lock()
			uploadedFiles[filename] = true
			mu.Unlock()

			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"recordingId": "rec-" + filename})
		}
	}))
	defer server.Close()

	log := zerolog.Nop()

	st, err := store.NewSQLite(dbPath)
	require.NoError(t, err)
	defer st.Close()

	client := api.NewClient(server.URL, "test-token")
	v := validator.New(log)
	u := uploader.New(client, 2, 10*1024*1024, log)

	// Create multiple test files
	fileNames := []string{"recording1.mcap", "recording2.mcap", "recording3.mcap"}
	for _, name := range fileNames {
		path := filepath.Join(watchDir, name)
		err := createTestMCAPFile(path)
		require.NoError(t, err)
	}

	// Also create a .bag file
	bagPath := filepath.Join(watchDir, "recording4.bag")
	err = os.WriteFile(bagPath, []byte("#ROSBAG V2.0\ntest content"), 0644)
	require.NoError(t, err)

	// Scan
	w, err := watcher.New(
		[]string{watchDir},
		[]string{"*.mcap", "*.bag"},
		[]string{},
		log,
	)
	require.NoError(t, err)
	defer w.Stop()

	files, err := w.Scan()
	require.NoError(t, err)
	assert.Len(t, files, 4)

	// Process all files
	for _, filePath := range files {
		info, err := os.Stat(filePath)
		require.NoError(t, err)

		file := &store.File{
			Path:  filePath,
			Size:  info.Size(),
			MTime: info.ModTime().Unix(),
			State: store.StateDiscovered,
		}

		// Validate
		result, err := v.Validate(filePath)
		require.NoError(t, err)

		if result.Valid {
			file.State = store.StateValidated
			file.FileType = result.FileType
			file.Checksum = result.Checksum
			file.Metadata = result.Metadata
			err = st.UpsertFile(file)
			require.NoError(t, err)

			// Upload
			err = u.Upload(context.Background(), file)
			require.NoError(t, err)

			file.State = store.StateUploaded
			err = st.UpsertFile(file)
			require.NoError(t, err)
		}
	}

	// Verify all files uploaded
	mu.Lock()
	assert.Len(t, uploadedFiles, 4)
	assert.True(t, uploadedFiles["recording1.mcap"])
	assert.True(t, uploadedFiles["recording2.mcap"])
	assert.True(t, uploadedFiles["recording3.mcap"])
	assert.True(t, uploadedFiles["recording4.bag"])
	mu.Unlock()

	// Verify store
	uploaded, err := st.GetFilesByState(store.StateUploaded)
	require.NoError(t, err)
	assert.Len(t, uploaded, 4)
}

// TestEndToEnd_ResumeAfterRestart tests state persistence across restarts.
func TestEndToEnd_ResumeAfterRestart(t *testing.T) {
	tmpDir := t.TempDir()
	watchDir := filepath.Join(tmpDir, "recordings")
	dbPath := filepath.Join(tmpDir, "agent.db")

	err := os.MkdirAll(watchDir, 0755)
	require.NoError(t, err)

	// First "session" - discover and validate files
	{
		st, err := store.NewSQLite(dbPath)
		require.NoError(t, err)

		// Create test files
		for i := 1; i <= 3; i++ {
			path := filepath.Join(watchDir, "file"+string(rune('0'+i))+".mcap")
			err := createTestMCAPFile(path)
			require.NoError(t, err)

			info, _ := os.Stat(path)
			file := &store.File{
				Path:     path,
				Size:     info.Size(),
				MTime:    info.ModTime().Unix(),
				State:    store.StateValidated, // Already validated
				FileType: "mcap",
				Checksum: "sha256:test",
			}
			err = st.UpsertFile(file)
			require.NoError(t, err)
		}

		st.Close()
	}

	// Second "session" - resume and upload
	{
		st, err := store.NewSQLite(dbPath)
		require.NoError(t, err)
		defer st.Close()

		// Should find validated files ready for upload
		validated, err := st.GetFilesByState(store.StateValidated)
		require.NoError(t, err)
		assert.Len(t, validated, 3, "Should resume with 3 validated files")

		// Simulate upload
		for _, file := range validated {
			file.State = store.StateUploaded
			err = st.UpsertFile(file)
			require.NoError(t, err)
		}

		uploaded, err := st.GetFilesByState(store.StateUploaded)
		require.NoError(t, err)
		assert.Len(t, uploaded, 3)
	}
}

// TestEndToEnd_ExcludePatterns tests that excluded files are not processed.
func TestEndToEnd_ExcludePatterns(t *testing.T) {
	tmpDir := t.TempDir()
	watchDir := filepath.Join(tmpDir, "recordings")

	err := os.MkdirAll(watchDir, 0755)
	require.NoError(t, err)

	// Create various files
	files := map[string]bool{
		"good1.mcap":        true,  // Should be included
		"good2.bag":         true,  // Should be included
		"inprogress.active": false, // Should be excluded
		"backup.mcap~":      false, // Should be excluded
		"temp.mcap.tmp":     false, // Should be excluded
		"notes.txt":         false, // Should be excluded (not matching include)
	}

	for name := range files {
		path := filepath.Join(watchDir, name)
		if filepath.Ext(name) == ".mcap" && !contains(name, "~") && !contains(name, ".tmp") {
			err := createTestMCAPFile(path)
			require.NoError(t, err)
		} else {
			err := os.WriteFile(path, []byte("test"), 0644)
			require.NoError(t, err)
		}
	}

	w, err := watcher.New(
		[]string{watchDir},
		[]string{"*.mcap", "*.bag"},
		[]string{"*.active", "*~", "*.tmp"},
		zerolog.Nop(),
	)
	require.NoError(t, err)
	defer w.Stop()

	scanned, err := w.Scan()
	require.NoError(t, err)

	// Should only find the two good files
	assert.Len(t, scanned, 2)

	foundNames := make(map[string]bool)
	for _, path := range scanned {
		foundNames[filepath.Base(path)] = true
	}

	assert.True(t, foundNames["good1.mcap"])
	assert.True(t, foundNames["good2.bag"])
	assert.False(t, foundNames["inprogress.active"])
	assert.False(t, foundNames["backup.mcap~"])
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && contains(s[1:], substr) || s[:len(substr)] == substr)
}
