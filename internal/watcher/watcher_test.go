package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	log := zerolog.Nop()

	w, err := New(
		[]string{"/tmp"},
		[]string{"*.mcap", "*.bag"},
		[]string{"*.tmp"},
		log,
	)
	require.NoError(t, err)
	require.NotNil(t, w)

	assert.Equal(t, []string{"/tmp"}, w.paths)
	assert.Equal(t, []string{"*.mcap", "*.bag"}, w.includes)
	assert.Equal(t, []string{"*.tmp"}, w.excludes)

	err = w.Stop()
	require.NoError(t, err)
}

func TestFSWatcher_MatchesPatterns(t *testing.T) {
	log := zerolog.Nop()

	w, err := New(
		[]string{"/tmp"},
		[]string{"*.mcap", "*.bag", "*.db3"},
		[]string{"*.active", "*.tmp", "*~"},
		log,
	)
	require.NoError(t, err)
	defer w.Stop()

	tests := []struct {
		path     string
		expected bool
	}{
		// Included patterns
		{"/data/recording.mcap", true},
		{"/data/recording.bag", true},
		{"/data/recording.db3", true},
		{"/data/subdir/test.mcap", true},

		// Excluded patterns
		{"/data/recording.mcap.active", false},
		{"/data/recording.tmp", false},
		{"/data/recording.bag~", false},

		// Not matching any include pattern
		{"/data/recording.txt", false},
		{"/data/recording.json", false},
		{"/data/file.log", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := w.matchesPatterns(tt.path)
			assert.Equal(t, tt.expected, result, "path: %s", tt.path)
		})
	}
}

func TestFSWatcher_MatchesPatterns_NoIncludes(t *testing.T) {
	log := zerolog.Nop()

	// No include patterns means match everything except excludes
	w, err := New(
		[]string{"/tmp"},
		[]string{}, // No includes
		[]string{"*.tmp"},
		log,
	)
	require.NoError(t, err)
	defer w.Stop()

	tests := []struct {
		path     string
		expected bool
	}{
		{"/data/recording.mcap", true},
		{"/data/recording.txt", true},
		{"/data/recording.tmp", false}, // Excluded
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := w.matchesPatterns(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFSWatcher_Scan(t *testing.T) {
	// Create temp directory with test files
	tmpDir := t.TempDir()

	// Create test files
	testFiles := []string{
		"recording1.mcap",
		"recording2.bag",
		"recording3.db3",
		"notes.txt",
		"data.json",
		"temp.mcap.active",
		"backup.mcap~",
	}

	for _, name := range testFiles {
		path := filepath.Join(tmpDir, name)
		err := os.WriteFile(path, []byte("test"), 0644)
		require.NoError(t, err)
	}

	// Create subdirectory with more files
	subDir := filepath.Join(tmpDir, "subdir")
	err := os.MkdirAll(subDir, 0755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(subDir, "nested.mcap"), []byte("test"), 0644)
	require.NoError(t, err)

	log := zerolog.Nop()

	w, err := New(
		[]string{tmpDir},
		[]string{"*.mcap", "*.bag", "*.db3"},
		[]string{"*.active", "*~"},
		log,
	)
	require.NoError(t, err)
	defer w.Stop()

	// Scan directory
	files, err := w.Scan()
	require.NoError(t, err)

	// Should find 4 files: recording1.mcap, recording2.bag, recording3.db3, subdir/nested.mcap
	assert.Len(t, files, 4)

	// Verify expected files are present
	fileNames := make(map[string]bool)
	for _, f := range files {
		fileNames[filepath.Base(f)] = true
	}

	assert.True(t, fileNames["recording1.mcap"])
	assert.True(t, fileNames["recording2.bag"])
	assert.True(t, fileNames["recording3.db3"])
	assert.True(t, fileNames["nested.mcap"])

	// Verify excluded files are not present
	assert.False(t, fileNames["temp.mcap.active"])
	assert.False(t, fileNames["backup.mcap~"])
	assert.False(t, fileNames["notes.txt"])
}

func TestFSWatcher_Scan_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	log := zerolog.Nop()

	w, err := New(
		[]string{tmpDir},
		[]string{"*.mcap"},
		[]string{},
		log,
	)
	require.NoError(t, err)
	defer w.Stop()

	files, err := w.Scan()
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestFSWatcher_Scan_NonexistentDirectory(t *testing.T) {
	log := zerolog.Nop()

	w, err := New(
		[]string{"/nonexistent/directory/path"},
		[]string{"*.mcap"},
		[]string{},
		log,
	)
	require.NoError(t, err)
	defer w.Stop()

	files, err := w.Scan()
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestFSWatcher_Events(t *testing.T) {
	tmpDir := t.TempDir()
	log := zerolog.Nop()

	w, err := New(
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
	require.NotNil(t, events)

	// Create a file and wait for event
	testFile := filepath.Join(tmpDir, "test.mcap")

	done := make(chan struct{})
	var receivedEvent Event

	go func() {
		select {
		case e := <-events:
			receivedEvent = e
			close(done)
		case <-time.After(2 * time.Second):
			close(done)
		}
	}()

	// Small delay to ensure watcher is ready
	time.Sleep(100 * time.Millisecond)

	err = os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	<-done

	// Verify we received a create event
	assert.Equal(t, testFile, receivedEvent.Path)
	assert.Equal(t, Create, receivedEvent.Op)
}

func TestFSWatcher_Events_FilteredOut(t *testing.T) {
	tmpDir := t.TempDir()
	log := zerolog.Nop()

	w, err := New(
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

	// Small delay to ensure watcher is ready
	time.Sleep(100 * time.Millisecond)

	// Create a file that doesn't match the pattern
	testFile := filepath.Join(tmpDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	// Wait briefly and ensure no event was received
	select {
	case e := <-events:
		t.Errorf("unexpected event received: %+v", e)
	case <-time.After(200 * time.Millisecond):
		// Expected - no event should be received for .txt file
	}
}

func TestOp_String(t *testing.T) {
	tests := []struct {
		op       Op
		expected string
	}{
		{Create, "create"},
		{Write, "write"},
		{Remove, "remove"},
		{Rename, "rename"},
		{Op(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.op.String())
		})
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern  string
		name     string
		expected bool
	}{
		{"*.mcap", "recording.mcap", true},
		{"*.mcap", "recording.bag", false},
		{"test*.mcap", "test123.mcap", true},
		{"test*.mcap", "other.mcap", false},
		{"[abc].mcap", "a.mcap", true},
		{"[abc].mcap", "d.mcap", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.name, func(t *testing.T) {
			result := MatchPattern(tt.pattern, tt.name)
			assert.Equal(t, tt.expected, result)
		})
	}
}
