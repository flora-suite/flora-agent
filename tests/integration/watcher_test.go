//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flora-suite/flora-agent/internal/watcher"
)

// TestWatcher_RealTimeDetection tests real-time file detection using fsnotify.
func TestWatcher_RealTimeDetection(t *testing.T) {
	tmpDir := t.TempDir()
	log := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()

	w, err := watcher.New(
		[]string{tmpDir},
		[]string{"*.mcap", "*.bag"},
		[]string{"*.tmp"},
		log,
	)
	require.NoError(t, err)

	err = w.Start()
	require.NoError(t, err)
	defer w.Stop()

	events := w.Events()

	// Track received events
	var mu sync.Mutex
	receivedEvents := make([]watcher.Event, 0)

	done := make(chan struct{})
	go func() {
		timeout := time.After(3 * time.Second)
		for {
			select {
			case e, ok := <-events:
				if !ok {
					close(done)
					return
				}
				mu.Lock()
				receivedEvents = append(receivedEvents, e)
				mu.Unlock()
			case <-timeout:
				close(done)
				return
			}
		}
	}()

	// Wait for watcher to be ready
	time.Sleep(100 * time.Millisecond)

	// Create files
	mcapPath := filepath.Join(tmpDir, "test.mcap")
	err = os.WriteFile(mcapPath, []byte("test content"), 0644)
	require.NoError(t, err)

	bagPath := filepath.Join(tmpDir, "test.bag")
	err = os.WriteFile(bagPath, []byte("bag content"), 0644)
	require.NoError(t, err)

	// Create excluded file (should not trigger event)
	tmpPath := filepath.Join(tmpDir, "test.tmp")
	err = os.WriteFile(tmpPath, []byte("tmp content"), 0644)
	require.NoError(t, err)

	// Wait for events
	time.Sleep(500 * time.Millisecond)

	w.Stop()
	<-done

	// Verify we got events for mcap and bag but not tmp
	mu.Lock()
	defer mu.Unlock()

	mcapFound := false
	bagFound := false
	tmpFound := false

	for _, e := range receivedEvents {
		switch filepath.Base(e.Path) {
		case "test.mcap":
			mcapFound = true
		case "test.bag":
			bagFound = true
		case "test.tmp":
			tmpFound = true
		}
	}

	assert.True(t, mcapFound, "Should detect test.mcap")
	assert.True(t, bagFound, "Should detect test.bag")
	assert.False(t, tmpFound, "Should NOT detect test.tmp (excluded)")
}

// TestWatcher_NestedDirectories tests watching nested directory structures.
func TestWatcher_NestedDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	// Create nested structure
	dirs := []string{
		filepath.Join(tmpDir, "session1"),
		filepath.Join(tmpDir, "session1", "camera"),
		filepath.Join(tmpDir, "session2"),
		filepath.Join(tmpDir, "session2", "lidar"),
	}

	for _, dir := range dirs {
		err := os.MkdirAll(dir, 0755)
		require.NoError(t, err)
	}

	log := zerolog.Nop()

	w, err := watcher.New(
		[]string{tmpDir},
		[]string{"*.mcap"},
		[]string{},
		log,
	)
	require.NoError(t, err)

	err = w.Start()
	require.NoError(t, err)

	// Create files in nested directories
	testFiles := []string{
		filepath.Join(tmpDir, "root.mcap"),
		filepath.Join(tmpDir, "session1", "recording.mcap"),
		filepath.Join(tmpDir, "session1", "camera", "frames.mcap"),
		filepath.Join(tmpDir, "session2", "lidar", "points.mcap"),
	}

	time.Sleep(100 * time.Millisecond)

	for _, path := range testFiles {
		err := os.WriteFile(path, []byte("test"), 0644)
		require.NoError(t, err)
	}

	// Scan should find all files
	files, err := w.Scan()
	require.NoError(t, err)

	w.Stop()

	assert.Len(t, files, 4)

	foundPaths := make(map[string]bool)
	for _, f := range files {
		foundPaths[f] = true
	}

	for _, expected := range testFiles {
		assert.True(t, foundPaths[expected], "Should find %s", expected)
	}
}

// TestWatcher_DynamicDirectoryCreation tests detection of files in newly created directories.
func TestWatcher_DynamicDirectoryCreation(t *testing.T) {
	tmpDir := t.TempDir()
	log := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()

	w, err := watcher.New(
		[]string{tmpDir},
		[]string{"*.mcap"},
		[]string{},
		log,
	)
	require.NoError(t, err)

	err = w.Start()
	require.NoError(t, err)
	defer w.Stop()

	events := w.Events()

	var mu sync.Mutex
	receivedEvents := make([]watcher.Event, 0)

	go func() {
		for e := range events {
			mu.Lock()
			receivedEvents = append(receivedEvents, e)
			mu.Unlock()
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Create a new subdirectory
	newDir := filepath.Join(tmpDir, "new_session")
	err = os.MkdirAll(newDir, 0755)
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	// Create a file in the new directory
	newFile := filepath.Join(newDir, "recording.mcap")
	err = os.WriteFile(newFile, []byte("test"), 0644)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	// Scan should find the new file
	files, err := w.Scan()
	require.NoError(t, err)

	assert.Len(t, files, 1)
	assert.Equal(t, newFile, files[0])
}

// TestWatcher_HighVolumeFileCreation tests handling many files created rapidly.
func TestWatcher_HighVolumeFileCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping high volume test in short mode")
	}

	tmpDir := t.TempDir()
	log := zerolog.Nop()

	w, err := watcher.New(
		[]string{tmpDir},
		[]string{"*.mcap"},
		[]string{},
		log,
	)
	require.NoError(t, err)

	err = w.Start()
	require.NoError(t, err)
	defer w.Stop()

	// Create many files rapidly
	numFiles := 100
	for i := 0; i < numFiles; i++ {
		path := filepath.Join(tmpDir, "file"+string(rune('0'+i/100%10))+string(rune('0'+i/10%10))+string(rune('0'+i%10))+".mcap")
		err := os.WriteFile(path, []byte("test content"), 0644)
		require.NoError(t, err)
	}

	// Wait for filesystem to settle
	time.Sleep(500 * time.Millisecond)

	// Scan should find all files
	files, err := w.Scan()
	require.NoError(t, err)

	assert.Len(t, files, numFiles, "Should find all %d files", numFiles)
}

// TestWatcher_FileModification tests detection of file modifications.
func TestWatcher_FileModification(t *testing.T) {
	tmpDir := t.TempDir()
	log := zerolog.Nop()

	// Create initial file
	mcapPath := filepath.Join(tmpDir, "recording.mcap")
	err := os.WriteFile(mcapPath, []byte("initial content"), 0644)
	require.NoError(t, err)

	w, err := watcher.New(
		[]string{tmpDir},
		[]string{"*.mcap"},
		[]string{},
		log,
	)
	require.NoError(t, err)

	err = w.Start()
	require.NoError(t, err)
	defer w.Stop()

	events := w.Events()

	var mu sync.Mutex
	writeEvents := 0

	go func() {
		for e := range events {
			if e.Op == watcher.Write {
				mu.Lock()
				writeEvents++
				mu.Unlock()
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Modify the file
	err = os.WriteFile(mcapPath, []byte("modified content"), 0644)
	require.NoError(t, err)

	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	assert.GreaterOrEqual(t, writeEvents, 1, "Should detect file modification")
	mu.Unlock()
}

// TestWatcher_ErrorChannel tests that watcher errors are reported.
func TestWatcher_ErrorChannel(t *testing.T) {
	tmpDir := t.TempDir()
	log := zerolog.Nop()

	w, err := watcher.New(
		[]string{tmpDir},
		[]string{"*.mcap"},
		[]string{},
		log,
	)
	require.NoError(t, err)

	err = w.Start()
	require.NoError(t, err)

	errors := w.Errors()
	require.NotNil(t, errors)

	// Errors channel should be available
	// In normal operation, no errors should be received

	w.Stop()
}
