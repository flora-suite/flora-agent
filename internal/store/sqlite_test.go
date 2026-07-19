package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSQLite(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLite(dbPath)
	require.NoError(t, err)
	defer store.Close()

	// Verify database file was created
	_, err = os.Stat(dbPath)
	assert.NoError(t, err)
}

func TestNewSQLite_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "subdir", "nested", "test.db")

	store, err := NewSQLite(dbPath)
	require.NoError(t, err)
	defer store.Close()

	// Verify nested directory was created
	_, err = os.Stat(filepath.Dir(dbPath))
	assert.NoError(t, err)
}

func TestSQLiteStore_UpsertAndGetFile(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	file := &File{
		Path:     "/data/test.mcap",
		Size:     1024,
		MTime:    time.Now().Unix(),
		State:    StateDiscovered,
		FileType: "mcap",
	}

	// Insert file
	err := store.UpsertFile(file)
	require.NoError(t, err)
	assert.NotEmpty(t, file.ID)
	assert.NotZero(t, file.CreatedAt)
	assert.NotZero(t, file.UpdatedAt)

	// Get file
	retrieved, err := store.GetFile("/data/test.mcap")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	assert.Equal(t, file.Path, retrieved.Path)
	assert.Equal(t, file.Size, retrieved.Size)
	assert.Equal(t, file.State, retrieved.State)
	assert.Equal(t, file.FileType, retrieved.FileType)
}

func TestSQLiteStore_GetFile_NotFound(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	file, err := store.GetFile("/nonexistent/file.mcap")
	require.NoError(t, err)
	assert.Nil(t, file)
}

func TestSQLiteStore_UpsertFile_Update(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	file := &File{
		Path:     "/data/test.mcap",
		Size:     1024,
		MTime:    time.Now().Unix(),
		State:    StateDiscovered,
		FileType: "mcap",
	}

	// Insert
	err := store.UpsertFile(file)
	require.NoError(t, err)
	originalUpdatedAt := file.UpdatedAt

	// Update state
	time.Sleep(10 * time.Millisecond) // Ensure different timestamp
	file.State = StateValidated
	file.Checksum = "abc123"
	err = store.UpsertFile(file)
	require.NoError(t, err)

	// Verify update
	retrieved, err := store.GetFile("/data/test.mcap")
	require.NoError(t, err)
	assert.Equal(t, StateValidated, retrieved.State)
	assert.Equal(t, "abc123", retrieved.Checksum)
	assert.GreaterOrEqual(t, retrieved.UpdatedAt, originalUpdatedAt)
}

func TestSQLiteStore_UsesDistinctIDsForSameBasename(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	first := &File{Path: "/data/one/recording.mcap", Size: 1, MTime: 1, State: StateDiscovered}
	second := &File{Path: "/data/two/recording.mcap", Size: 1, MTime: 1, State: StateDiscovered}
	require.NoError(t, store.UpsertFile(first))
	require.NoError(t, store.UpsertFile(second))
	assert.NotEqual(t, first.ID, second.ID)
}

func TestSQLiteStore_MigratesIncompleteUploadsToDiscovery(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	file := &File{ID: "recording.mcap", Path: "/data/recording.mcap", Size: 1, MTime: 1, State: StateUploading, UploadID: "upload-1"}
	require.NoError(t, store.UpsertFile(file))
	require.NoError(t, store.UpsertChunk(&UploadChunk{FileID: file.ID, ChunkIndex: 0, Size: 1, Uploaded: true}))
	_, err := store.db.Exec("DELETE FROM config WHERE key = 'schema_version'")
	require.NoError(t, err)

	require.NoError(t, store.migratePathIDs())
	migrated, err := store.GetFile(file.Path)
	require.NoError(t, err)
	require.Equal(t, StateDiscovered, migrated.State)
	assert.Empty(t, migrated.UploadID)
	assert.Equal(t, generateID(file.Path), migrated.ID)
	chunks, err := store.GetChunks(migrated.ID)
	require.NoError(t, err)
	assert.Empty(t, chunks)
}

func TestOpenSQLiteReadOnlyDoesNotMigrateState(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agent.db")
	writer, err := NewSQLite(dbPath)
	require.NoError(t, err)

	file := &File{ID: "recording.mcap", Path: "/data/recording.mcap", Size: 1, MTime: 1, State: StateUploading, UploadID: "upload-1"}
	require.NoError(t, writer.UpsertFile(file))
	require.NoError(t, writer.UpsertChunk(&UploadChunk{FileID: file.ID, ChunkIndex: 0, Size: 1, Uploaded: true}))
	_, err = writer.db.Exec("DELETE FROM config WHERE key = 'schema_version'")
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	reader, err := OpenSQLiteReadOnly(dbPath)
	require.NoError(t, err)
	defer reader.Close()

	files, err := reader.GetFilesByState(StateUploading)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "upload-1", files[0].UploadID)
	chunks, err := reader.GetChunks(file.ID)
	require.NoError(t, err)
	assert.Len(t, chunks, 1)
}

func TestSQLiteStore_UpsertFile_WithMetadata(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	startTime := time.Now()
	file := &File{
		Path:     "/data/test.mcap",
		Size:     1024,
		MTime:    time.Now().Unix(),
		State:    StateValidated,
		FileType: "mcap",
		Checksum: "sha256:abc123",
		Metadata: &FileMetadata{
			Topics: []TopicInfo{
				{Name: "/camera/image", Type: "sensor_msgs/Image", MessageCount: 100},
				{Name: "/imu/data", Type: "sensor_msgs/Imu", MessageCount: 1000},
			},
			StartTime:    &startTime,
			Duration:     60.5,
			MessageCount: 1100,
		},
	}

	err := store.UpsertFile(file)
	require.NoError(t, err)

	retrieved, err := store.GetFile("/data/test.mcap")
	require.NoError(t, err)
	require.NotNil(t, retrieved.Metadata)

	assert.Len(t, retrieved.Metadata.Topics, 2)
	assert.Equal(t, "/camera/image", retrieved.Metadata.Topics[0].Name)
	assert.Equal(t, int64(100), retrieved.Metadata.Topics[0].MessageCount)
	assert.Equal(t, 60.5, retrieved.Metadata.Duration)
	assert.Equal(t, int64(1100), retrieved.Metadata.MessageCount)
}

func TestSQLiteStore_GetFilesByState(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Insert files with different states
	files := []*File{
		{Path: "/data/1.mcap", Size: 100, MTime: time.Now().Unix(), State: StateDiscovered, FileType: "mcap"},
		{Path: "/data/2.mcap", Size: 200, MTime: time.Now().Unix(), State: StateValidated, FileType: "mcap"},
		{Path: "/data/3.mcap", Size: 300, MTime: time.Now().Unix(), State: StateValidated, FileType: "mcap"},
		{Path: "/data/4.mcap", Size: 400, MTime: time.Now().Unix(), State: StateUploaded, FileType: "mcap"},
	}

	for _, f := range files {
		err := store.UpsertFile(f)
		require.NoError(t, err)
	}

	// Get validated files
	validated, err := store.GetFilesByState(StateValidated)
	require.NoError(t, err)
	assert.Len(t, validated, 2)

	// Get discovered files
	discovered, err := store.GetFilesByState(StateDiscovered)
	require.NoError(t, err)
	assert.Len(t, discovered, 1)

	// Get uploaded files
	uploaded, err := store.GetFilesByState(StateUploaded)
	require.NoError(t, err)
	assert.Len(t, uploaded, 1)

	// Get files with no matches
	invalid, err := store.GetFilesByState(StateInvalid)
	require.NoError(t, err)
	assert.Len(t, invalid, 0)
}

func TestSQLiteStore_DeleteFile(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	file := &File{
		Path:     "/data/test.mcap",
		Size:     1024,
		MTime:    time.Now().Unix(),
		State:    StateDiscovered,
		FileType: "mcap",
	}

	err := store.UpsertFile(file)
	require.NoError(t, err)

	// Verify exists
	retrieved, err := store.GetFile("/data/test.mcap")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Delete
	err = store.DeleteFile("/data/test.mcap")
	require.NoError(t, err)

	// Verify deleted
	retrieved, err = store.GetFile("/data/test.mcap")
	require.NoError(t, err)
	assert.Nil(t, retrieved)
}

func TestSQLiteStore_Chunks(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// First create a file
	file := &File{
		Path:     "/data/large.mcap",
		Size:     100 * 1024 * 1024,
		MTime:    time.Now().Unix(),
		State:    StateUploading,
		FileType: "mcap",
	}
	err := store.UpsertFile(file)
	require.NoError(t, err)

	// Add chunks
	chunks := []*UploadChunk{
		{FileID: file.ID, ChunkIndex: 0, Offset: 0, Size: 10 * 1024 * 1024, Uploaded: true, ETag: "etag0"},
		{FileID: file.ID, ChunkIndex: 1, Offset: 10 * 1024 * 1024, Size: 10 * 1024 * 1024, Uploaded: true, ETag: "etag1"},
		{FileID: file.ID, ChunkIndex: 2, Offset: 20 * 1024 * 1024, Size: 10 * 1024 * 1024, Uploaded: false},
	}

	for _, c := range chunks {
		err := store.UpsertChunk(c)
		require.NoError(t, err)
	}

	// Get chunks
	retrieved, err := store.GetChunks(file.ID)
	require.NoError(t, err)
	assert.Len(t, retrieved, 3)

	assert.True(t, retrieved[0].Uploaded)
	assert.Equal(t, "etag0", retrieved[0].ETag)
	assert.True(t, retrieved[1].Uploaded)
	assert.False(t, retrieved[2].Uploaded)

	// Update chunk
	chunks[2].Uploaded = true
	chunks[2].ETag = "etag2"
	err = store.UpsertChunk(chunks[2])
	require.NoError(t, err)

	retrieved, err = store.GetChunks(file.ID)
	require.NoError(t, err)
	assert.True(t, retrieved[2].Uploaded)
	assert.Equal(t, "etag2", retrieved[2].ETag)

	// Delete chunks
	err = store.DeleteChunks(file.ID)
	require.NoError(t, err)

	retrieved, err = store.GetChunks(file.ID)
	require.NoError(t, err)
	assert.Len(t, retrieved, 0)
}

func TestSQLiteStore_Config(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Set config
	err := store.SetConfig("device_id", "robot-001")
	require.NoError(t, err)

	err = store.SetConfig("last_sync", "2024-01-15T10:00:00Z")
	require.NoError(t, err)

	// Get config
	deviceID, err := store.GetConfig("device_id")
	require.NoError(t, err)
	assert.Equal(t, "robot-001", deviceID)

	lastSync, err := store.GetConfig("last_sync")
	require.NoError(t, err)
	assert.Equal(t, "2024-01-15T10:00:00Z", lastSync)

	// Get nonexistent config
	notFound, err := store.GetConfig("nonexistent")
	require.NoError(t, err)
	assert.Empty(t, notFound)

	// Update config
	err = store.SetConfig("device_id", "robot-002")
	require.NoError(t, err)

	deviceID, err = store.GetConfig("device_id")
	require.NoError(t, err)
	assert.Equal(t, "robot-002", deviceID)
}

func TestFileState_Transitions(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	file := &File{
		Path:     "/data/test.mcap",
		Size:     1024,
		MTime:    time.Now().Unix(),
		State:    StateDiscovered,
		FileType: "mcap",
	}

	// Test state transitions
	transitions := []FileState{
		StateDiscovered,
		StateValidating,
		StateValidated,
		StateUploading,
		StateUploaded,
	}

	for _, state := range transitions {
		file.State = state
		err := store.UpsertFile(file)
		require.NoError(t, err)

		retrieved, err := store.GetFile(file.Path)
		require.NoError(t, err)
		assert.Equal(t, state, retrieved.State)
	}
}

func TestSQLiteStore_Ping(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Ping should succeed on open database
	err := store.Ping()
	require.NoError(t, err)
}

func TestSQLiteStore_Ping_AfterClose(t *testing.T) {
	store := newTestStore(t)

	// Close the store
	err := store.Close()
	require.NoError(t, err)

	// Ping should fail after close
	err = store.Ping()
	assert.Error(t, err)
}

// Helper function to create a test store
func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLite(dbPath)
	require.NoError(t, err)

	return store
}
