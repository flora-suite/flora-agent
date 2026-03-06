// Package agent provides tests for the core agent logic.
package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flora-suite/flora-agent/internal/store"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, "https://api.flora.fan", cfg.Server.URL)
	assert.Equal(t, []string{"*.mcap", "*.bag", "*.db3"}, cfg.Watch.Patterns.Include)
	assert.Equal(t, []string{"*.active", "*.tmp", "*~"}, cfg.Watch.Patterns.Exclude)
	assert.Equal(t, 30*time.Second, cfg.Watch.ScanInterval)
	assert.Equal(t, 5*time.Second, cfg.Watch.MinFileAge)
	assert.Equal(t, true, cfg.Upload.Enabled)
	assert.Equal(t, 2, cfg.Upload.Concurrent)
	assert.Equal(t, int64(10*1024*1024), cfg.Upload.ChunkSize)
	assert.Equal(t, 3, cfg.Upload.RetryAttempts)
	assert.Equal(t, 5*time.Second, cfg.Upload.RetryDelay)
	assert.Equal(t, "/var/lib/flora-agent/agent.db", cfg.Storage.DBPath)
	assert.Equal(t, false, cfg.Metrics.Enabled)
	assert.Equal(t, 9090, cfg.Metrics.Port)
	assert.Equal(t, "/metrics", cfg.Metrics.Path)
	assert.Equal(t, true, cfg.Health.Enabled)
	assert.Equal(t, 8080, cfg.Health.Port)
	assert.Equal(t, "/health", cfg.Health.Path)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.Equal(t, "json", cfg.Log.Format)
	assert.Equal(t, "stdout", cfg.Log.Output)
}

func TestGetMachineID(t *testing.T) {
	id := getMachineID()
	assert.NotEmpty(t, id)

	// Call again to ensure consistency (on same machine)
	id2 := getMachineID()
	// Note: On systems with /etc/machine-id, these should be equal
	// On systems without it, they might differ due to timestamp
	assert.NotEmpty(t, id2)
}

func TestResolveDeviceToken_FromConfig(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			DeviceToken: "test-token-from-config",
		},
	}
	log := zerolog.New(zerolog.NewTestWriter(t))

	// Use a mock store that doesn't have any stored token
	st := &mockStore{}

	token, err := resolveDeviceToken(context.Background(), cfg, st, log)
	require.NoError(t, err)
	assert.Equal(t, "test-token-from-config", token)
}

func TestResolveDeviceToken_FromStore(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			// No DeviceToken in config
		},
	}
	log := zerolog.New(zerolog.NewTestWriter(t))

	// Mock store with stored token
	st := &mockStore{
		configs: map[string]string{
			"device_token": "stored-device-token",
		},
	}

	token, err := resolveDeviceToken(context.Background(), cfg, st, log)
	require.NoError(t, err)
	assert.Equal(t, "stored-device-token", token)
}

func TestResolveDeviceToken_NoToken(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			// No DeviceToken and no UserToken
		},
	}
	log := zerolog.New(zerolog.NewTestWriter(t))

	st := &mockStore{}

	_, err := resolveDeviceToken(context.Background(), cfg, st, log)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no device token configured")
}

func TestConfigStructure(t *testing.T) {
	// Test that Config struct has proper structure
	cfg := &Config{
		Server: ServerConfig{
			URL:         "http://localhost:3000",
			DeviceToken: "test-token",
			UserToken:   "user-token",
			DeviceName:  "test-device",
			DeviceType:  "robot",
		},
		Watch: WatchConfig{
			Paths: []string{"/data/recordings"},
			Patterns: PatternConfig{
				Include: []string{"*.mcap"},
				Exclude: []string{"*.tmp"},
			},
			ScanInterval: 60 * time.Second,
			MinFileAge:   10 * time.Second,
		},
		Upload: UploadConfig{
			Enabled:        true,
			Concurrent:     4,
			ChunkSize:      20 * 1024 * 1024,
			RetryAttempts:  5,
			RetryDelay:     10 * time.Second,
			BandwidthLimit: 1024 * 1024,
		},
		Storage: StorageConfig{
			DBPath: "/tmp/test.db",
		},
		Metrics: MetricsConfig{
			Enabled: true,
			Port:    9091,
			Path:    "/custom-metrics",
		},
		Health: HealthConfig{
			Enabled: true,
			Port:    8081,
			Path:    "/custom-health",
		},
		Log: LogConfig{
			Level:    "debug",
			Format:   "text",
			Output:   "file",
			FilePath: "/var/log/flora-agent.log",
		},
	}

	assert.Equal(t, "http://localhost:3000", cfg.Server.URL)
	assert.Equal(t, "test-token", cfg.Server.DeviceToken)
	assert.Equal(t, "user-token", cfg.Server.UserToken)
	assert.Equal(t, "test-device", cfg.Server.DeviceName)
	assert.Equal(t, "robot", cfg.Server.DeviceType)
	assert.Equal(t, []string{"/data/recordings"}, cfg.Watch.Paths)
	assert.Equal(t, []string{"*.mcap"}, cfg.Watch.Patterns.Include)
	assert.Equal(t, []string{"*.tmp"}, cfg.Watch.Patterns.Exclude)
	assert.Equal(t, 60*time.Second, cfg.Watch.ScanInterval)
	assert.Equal(t, 10*time.Second, cfg.Watch.MinFileAge)
	assert.True(t, cfg.Upload.Enabled)
	assert.Equal(t, 4, cfg.Upload.Concurrent)
	assert.Equal(t, int64(20*1024*1024), cfg.Upload.ChunkSize)
	assert.Equal(t, 5, cfg.Upload.RetryAttempts)
	assert.Equal(t, 10*time.Second, cfg.Upload.RetryDelay)
	assert.Equal(t, int64(1024*1024), cfg.Upload.BandwidthLimit)
	assert.Equal(t, "/tmp/test.db", cfg.Storage.DBPath)
	assert.True(t, cfg.Metrics.Enabled)
	assert.Equal(t, 9091, cfg.Metrics.Port)
	assert.Equal(t, "/custom-metrics", cfg.Metrics.Path)
	assert.True(t, cfg.Health.Enabled)
	assert.Equal(t, 8081, cfg.Health.Port)
	assert.Equal(t, "/custom-health", cfg.Health.Path)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.Equal(t, "text", cfg.Log.Format)
	assert.Equal(t, "file", cfg.Log.Output)
	assert.Equal(t, "/var/log/flora-agent.log", cfg.Log.FilePath)
}

// Test agent creation with invalid config
func TestNew_InvalidDBPath(t *testing.T) {
	// Create a config with an invalid DB path (directory that doesn't exist and can't be created)
	cfg := DefaultConfig()
	cfg.Server.DeviceToken = "test-token"
	cfg.Storage.DBPath = "/nonexistent/deeply/nested/path/that/cannot/be/created/agent.db"
	cfg.Watch.Paths = []string{os.TempDir()}

	log := zerolog.New(zerolog.NewTestWriter(t))

	_, err := New(cfg, log)
	// This should fail because the directory doesn't exist
	assert.Error(t, err)
}

func TestNew_ValidConfig(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	watchDir := filepath.Join(tmpDir, "recordings")
	err := os.MkdirAll(watchDir, 0755)
	require.NoError(t, err)

	cfg := DefaultConfig()
	cfg.Server.DeviceToken = "test-token"
	cfg.Server.URL = "http://localhost:3000"
	cfg.Storage.DBPath = dbPath
	cfg.Watch.Paths = []string{watchDir}
	cfg.Health.Enabled = false
	cfg.Metrics.Enabled = false

	log := zerolog.New(zerolog.NewTestWriter(t))

	agent, err := New(cfg, log)
	require.NoError(t, err)
	require.NotNil(t, agent)

	// Clean up
	err = agent.Shutdown()
	assert.NoError(t, err)
}

func TestAgent_HealthCheck(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	watchDir := filepath.Join(tmpDir, "recordings")
	err := os.MkdirAll(watchDir, 0755)
	require.NoError(t, err)

	cfg := DefaultConfig()
	cfg.Server.DeviceToken = "test-token"
	cfg.Server.URL = "http://localhost:3000"
	cfg.Storage.DBPath = dbPath
	cfg.Watch.Paths = []string{watchDir}
	cfg.Health.Enabled = false
	cfg.Metrics.Enabled = false

	log := zerolog.New(zerolog.NewTestWriter(t))

	agent, err := New(cfg, log)
	require.NoError(t, err)
	require.NotNil(t, agent)

	// Register health checkers
	agent.registerHealthCheckers()

	// Check health
	health := agent.HealthCheck()
	assert.NotEmpty(t, health.Status)

	// Clean up
	err = agent.Shutdown()
	assert.NoError(t, err)
}

// mockStore implements store.Store interface for testing
type mockStore struct {
	configs map[string]string
	files   map[string]*store.File
	chunks  map[string][]*store.UploadChunk
}

func (m *mockStore) GetConfig(key string) (string, error) {
	if m.configs == nil {
		return "", nil
	}
	return m.configs[key], nil
}

func (m *mockStore) SetConfig(key, value string) error {
	if m.configs == nil {
		m.configs = make(map[string]string)
	}
	m.configs[key] = value
	return nil
}

func (m *mockStore) Close() error {
	return nil
}

func (m *mockStore) Ping() error {
	return nil
}

func (m *mockStore) GetFile(path string) (*store.File, error) {
	if m.files == nil {
		return nil, nil
	}
	return m.files[path], nil
}

func (m *mockStore) GetFilesByState(state store.FileState) ([]*store.File, error) {
	var result []*store.File
	if m.files != nil {
		for _, f := range m.files {
			if f.State == state {
				result = append(result, f)
			}
		}
	}
	return result, nil
}

func (m *mockStore) UpsertFile(file *store.File) error {
	if m.files == nil {
		m.files = make(map[string]*store.File)
	}
	m.files[file.Path] = file
	return nil
}

func (m *mockStore) DeleteFile(path string) error {
	if m.files != nil {
		delete(m.files, path)
	}
	return nil
}

func (m *mockStore) GetChunks(fileID string) ([]*store.UploadChunk, error) {
	if m.chunks == nil {
		return nil, nil
	}
	return m.chunks[fileID], nil
}

func (m *mockStore) UpsertChunk(chunk *store.UploadChunk) error {
	if m.chunks == nil {
		m.chunks = make(map[string][]*store.UploadChunk)
	}
	chunks := m.chunks[chunk.FileID]
	// Update or append
	found := false
	for i, c := range chunks {
		if c.ChunkIndex == chunk.ChunkIndex {
			chunks[i] = chunk
			found = true
			break
		}
	}
	if !found {
		chunks = append(chunks, chunk)
	}
	m.chunks[chunk.FileID] = chunks
	return nil
}

func (m *mockStore) DeleteChunks(fileID string) error {
	if m.chunks != nil {
		delete(m.chunks, fileID)
	}
	return nil
}
