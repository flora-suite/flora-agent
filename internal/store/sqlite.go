// Package store provides persistent storage for agent state.
package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// FileState represents the state of a file in the processing pipeline.
type FileState string

const (
	StateDiscovered FileState = "discovered"
	StateValidating FileState = "validating"
	StateValidated  FileState = "validated"
	StateUploading  FileState = "uploading"
	StateUploaded   FileState = "uploaded"
	StateInvalid    FileState = "invalid"
	StateFailed     FileState = "failed"
)

// FileMetadata contains extracted metadata from recording files.
type FileMetadata struct {
	Topics       []TopicInfo `json:"topics,omitempty"`
	StartTime    *time.Time  `json:"start_time,omitempty"`
	EndTime      *time.Time  `json:"end_time,omitempty"`
	Duration     float64     `json:"duration,omitempty"`
	MessageCount int64       `json:"message_count,omitempty"`
}

// TopicInfo describes a topic in a recording file.
type TopicInfo struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	MessageCount int64  `json:"message_count"`
}

// File represents a recording file tracked by the agent.
type File struct {
	ID           string        `json:"id"`
	Path         string        `json:"path"`
	Size         int64         `json:"size"`
	MTime        int64         `json:"mtime"`
	State        FileState     `json:"state"`
	FileType     string        `json:"file_type"`
	Checksum     string        `json:"checksum"`
	Metadata     *FileMetadata `json:"metadata,omitempty"`
	ErrorMessage string        `json:"error_message,omitempty"`
	UploadID     string        `json:"upload_id,omitempty"`
	CreatedAt    int64         `json:"created_at"`
	UpdatedAt    int64         `json:"updated_at"`
}

// UploadChunk represents a chunk in a multipart upload.
type UploadChunk struct {
	FileID     string `json:"file_id"`
	ChunkIndex int    `json:"chunk_index"`
	Offset     int64  `json:"offset"`
	Size       int64  `json:"size"`
	Uploaded   bool   `json:"uploaded"`
	ETag       string `json:"etag,omitempty"`
}

// Store defines the interface for persistent storage.
type Store interface {
	// File operations
	GetFile(path string) (*File, error)
	UpsertFile(file *File) error
	GetFilesByState(state FileState) ([]*File, error)
	DeleteFile(path string) error

	// Chunk operations (for resumable uploads)
	GetChunks(fileID string) ([]*UploadChunk, error)
	UpsertChunk(chunk *UploadChunk) error
	DeleteChunks(fileID string) error

	// Config operations
	GetConfig(key string) (string, error)
	SetConfig(key, value string) error

	// Health check
	Ping() error

	// Lifecycle
	Close() error
}

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLite creates a new SQLite-backed store.
func NewSQLite(dbPath string) (*SQLiteStore, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	// The agent coordinates concurrent upload workers through a single local
	// state database. Serializing SQLite access prevents transient write-lock
	// failures while preserving durable state transitions.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &SQLiteStore{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

func (s *SQLiteStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS files (
		id TEXT PRIMARY KEY,
		path TEXT NOT NULL UNIQUE,
		size INTEGER NOT NULL,
		mtime INTEGER NOT NULL,
		state TEXT NOT NULL,
		file_type TEXT NOT NULL DEFAULT '',
		checksum TEXT,
		metadata TEXT,
		error_message TEXT,
		upload_id TEXT,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_files_state ON files(state);
	CREATE INDEX IF NOT EXISTS idx_files_path ON files(path);

	CREATE TABLE IF NOT EXISTS upload_chunks (
		file_id TEXT NOT NULL,
		chunk_index INTEGER NOT NULL,
		offset INTEGER NOT NULL,
		size INTEGER NOT NULL,
		uploaded INTEGER NOT NULL DEFAULT 0,
		etag TEXT,
		PRIMARY KEY (file_id, chunk_index),
		FOREIGN KEY (file_id) REFERENCES files(id)
	);

	CREATE TABLE IF NOT EXISTS config (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	return s.migratePathIDs()
}

// migratePathIDs moves older basename-based IDs to stable path hashes. Incomplete
// uploads are deliberately restarted because their server-side sessions cannot be
// safely associated with a changed local ID.
func (s *SQLiteStore) migratePathIDs() error {
	version, err := s.GetConfig("schema_version")
	if err != nil || version == "2" {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Query("SELECT id, path, state FROM files")
	if err != nil {
		return err
	}
	type record struct {
		id, path string
		state    FileState
	}
	var records []record
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.id, &r.path, &r.state); err != nil {
			rows.Close()
			return err
		}
		records = append(records, r)
	}
	if err := rows.Close(); err != nil {
		return err
	}

	if _, err := tx.Exec("DELETE FROM upload_chunks"); err != nil {
		return err
	}
	for _, r := range records {
		state := r.state
		if state != StateUploaded {
			state = StateDiscovered
		}
		if _, err := tx.Exec(
			"UPDATE files SET id = ?, state = ?, upload_id = NULL, error_message = NULL WHERE path = ?",
			generateID(r.path), state, r.path,
		); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("INSERT INTO config (key, value) VALUES ('schema_version', '2') ON CONFLICT(key) DO UPDATE SET value = excluded.value"); err != nil {
		return err
	}
	return tx.Commit()
}

// GetFile retrieves a file by path.
func (s *SQLiteStore) GetFile(path string) (*File, error) {
	row := s.db.QueryRow(`
		SELECT id, path, size, mtime, state, file_type, checksum, metadata, error_message, upload_id, created_at, updated_at
		FROM files WHERE path = ?
	`, path)

	var f File
	var metadata sql.NullString
	var checksum, errorMessage, uploadID sql.NullString

	err := row.Scan(&f.ID, &f.Path, &f.Size, &f.MTime, &f.State, &f.FileType,
		&checksum, &metadata, &errorMessage, &uploadID, &f.CreatedAt, &f.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if checksum.Valid {
		f.Checksum = checksum.String
	}
	if errorMessage.Valid {
		f.ErrorMessage = errorMessage.String
	}
	if uploadID.Valid {
		f.UploadID = uploadID.String
	}
	if metadata.Valid && metadata.String != "" {
		var m FileMetadata
		if err := json.Unmarshal([]byte(metadata.String), &m); err == nil {
			f.Metadata = &m
		}
	}

	return &f, nil
}

// UpsertFile inserts or updates a file record.
func (s *SQLiteStore) UpsertFile(file *File) error {
	now := time.Now().Unix()

	if file.ID == "" {
		file.ID = generateID(file.Path)
	}
	if file.CreatedAt == 0 {
		file.CreatedAt = now
	}
	file.UpdatedAt = now

	var metadataJSON []byte
	if file.Metadata != nil {
		var err error
		metadataJSON, err = json.Marshal(file.Metadata)
		if err != nil {
			return err
		}
	}

	_, err := s.db.Exec(`
		INSERT INTO files (id, path, size, mtime, state, file_type, checksum, metadata, error_message, upload_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			size = excluded.size,
			mtime = excluded.mtime,
			state = excluded.state,
			file_type = excluded.file_type,
			checksum = excluded.checksum,
			metadata = excluded.metadata,
			error_message = excluded.error_message,
			upload_id = excluded.upload_id,
			updated_at = excluded.updated_at
	`, file.ID, file.Path, file.Size, file.MTime, file.State, file.FileType,
		nullString(file.Checksum), nullString(string(metadataJSON)),
		nullString(file.ErrorMessage), nullString(file.UploadID),
		file.CreatedAt, file.UpdatedAt)

	return err
}

// GetFilesByState retrieves all files with the given state.
func (s *SQLiteStore) GetFilesByState(state FileState) ([]*File, error) {
	rows, err := s.db.Query(`
		SELECT id, path, size, mtime, state, file_type, checksum, metadata, error_message, upload_id, created_at, updated_at
		FROM files WHERE state = ?
		ORDER BY created_at ASC
	`, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []*File
	for rows.Next() {
		var f File
		var metadata sql.NullString
		var checksum, errorMessage, uploadID sql.NullString

		if err := rows.Scan(&f.ID, &f.Path, &f.Size, &f.MTime, &f.State, &f.FileType,
			&checksum, &metadata, &errorMessage, &uploadID, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}

		if checksum.Valid {
			f.Checksum = checksum.String
		}
		if errorMessage.Valid {
			f.ErrorMessage = errorMessage.String
		}
		if uploadID.Valid {
			f.UploadID = uploadID.String
		}
		if metadata.Valid && metadata.String != "" {
			var m FileMetadata
			if err := json.Unmarshal([]byte(metadata.String), &m); err == nil {
				f.Metadata = &m
			}
		}

		files = append(files, &f)
	}

	return files, rows.Err()
}

// DeleteFile removes a file record.
func (s *SQLiteStore) DeleteFile(path string) error {
	_, err := s.db.Exec("DELETE FROM files WHERE path = ?", path)
	return err
}

// GetChunks retrieves all chunks for a file.
func (s *SQLiteStore) GetChunks(fileID string) ([]*UploadChunk, error) {
	rows, err := s.db.Query(`
		SELECT file_id, chunk_index, offset, size, uploaded, etag
		FROM upload_chunks WHERE file_id = ?
		ORDER BY chunk_index ASC
	`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []*UploadChunk
	for rows.Next() {
		var c UploadChunk
		var uploaded int
		var etag sql.NullString

		if err := rows.Scan(&c.FileID, &c.ChunkIndex, &c.Offset, &c.Size, &uploaded, &etag); err != nil {
			return nil, err
		}
		c.Uploaded = uploaded == 1
		if etag.Valid {
			c.ETag = etag.String
		}
		chunks = append(chunks, &c)
	}

	return chunks, rows.Err()
}

// UpsertChunk inserts or updates a chunk record.
func (s *SQLiteStore) UpsertChunk(chunk *UploadChunk) error {
	uploaded := 0
	if chunk.Uploaded {
		uploaded = 1
	}

	_, err := s.db.Exec(`
		INSERT INTO upload_chunks (file_id, chunk_index, offset, size, uploaded, etag)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(file_id, chunk_index) DO UPDATE SET
			uploaded = excluded.uploaded,
			etag = excluded.etag
	`, chunk.FileID, chunk.ChunkIndex, chunk.Offset, chunk.Size, uploaded, nullString(chunk.ETag))

	return err
}

// DeleteChunks removes all chunks for a file.
func (s *SQLiteStore) DeleteChunks(fileID string) error {
	_, err := s.db.Exec("DELETE FROM upload_chunks WHERE file_id = ?", fileID)
	return err
}

// GetConfig retrieves a config value.
func (s *SQLiteStore) GetConfig(key string) (string, error) {
	var value string
	err := s.db.QueryRow("SELECT value FROM config WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetConfig sets a config value.
func (s *SQLiteStore) SetConfig(key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO config (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

// Ping checks if the database connection is alive.
func (s *SQLiteStore) Ping() error {
	return s.db.Ping()
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Helper functions

func generateID(path string) string {
	absPath, err := filepath.Abs(path)
	if err == nil {
		path = absPath
	}
	cleanPath := filepath.Clean(path)
	sum := sha256.Sum256([]byte(cleanPath))
	return hex.EncodeToString(sum[:])
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
