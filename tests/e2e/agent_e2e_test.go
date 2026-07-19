//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flora-suite/flora-agent/internal/store"
)

var agentBinary string

func TestMain(m *testing.M) {
	root, err := filepath.Abs("../..")
	if err != nil {
		panic(err)
	}
	path := filepath.Join(os.TempDir(), fmt.Sprintf("flora-agent-e2e-%d", os.Getpid()))
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	build := exec.Command("go", "build", "-o", path, "./cmd/flora-agent")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("build flora-agent: %v\n%s", err, output))
	}
	agentBinary = path
	code := m.Run()
	_ = os.Remove(path)
	os.Exit(code)
}

func TestAgentLifecycleWithDeviceToken(t *testing.T) {
	server := newMockServer(t, "")
	defer server.Close()

	dbPath := runAgentE2E(t, server.URL, "device-token", "")
	assert.Zero(t, server.registrations.Load())
	assert.GreaterOrEqual(t, server.heartbeats.Load(), int32(1))
	assert.Equal(t, "device-token", server.uploadToken())
	assertUploadedState(t, dbPath)
}

func TestAgentRegistersThenUploadsWithDeviceToken(t *testing.T) {
	server := newMockServer(t, "user-token")
	defer server.Close()

	dbPath := runAgentE2E(t, server.URL, "", "user-token")
	assert.Equal(t, int32(1), server.registrations.Load())
	assert.GreaterOrEqual(t, server.heartbeats.Load(), int32(1))
	assert.Equal(t, "registered-device-token", server.uploadToken())
	assertUploadedState(t, dbPath)
}

func runAgentE2E(t *testing.T, serverURL, deviceToken, userToken string) string {
	t.Helper()
	tmpDir := t.TempDir()
	watchDir := filepath.Join(tmpDir, "recordings")
	require.NoError(t, os.MkdirAll(watchDir, 0755))
	configPath := filepath.Join(tmpDir, "agent.yaml")
	dbPath := filepath.Join(tmpDir, "agent.db")
	config := fmt.Sprintf(`server:
  url: %q
  device_token: %q
  user_token: %q
watch:
  paths: [%q]
  scan_interval: 100ms
  min_file_age: 0s
upload:
  enabled: true
  concurrent: 1
  chunk_size: 1048576
  retry_attempts: 1
  retry_delay: 1ms
storage:
  db_path: %q
health:
  enabled: false
metrics:
  enabled: false
log:
  level: error
  format: text
  output: stdout
`, serverURL, deviceToken, userToken, watchDir, dbPath)
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0600))

	cmd := exec.Command(agentBinary, "run", "--config", configPath)
	output := &strings.Builder{}
	cmd.Stdout = output
	cmd.Stderr = output
	require.NoError(t, cmd.Start())

	bagPath := filepath.Join(watchDir, "recording.bag")
	require.NoError(t, os.WriteFile(bagPath, []byte("#ROSBAG V2.0\nminimal test recording"), 0644))

	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if e2eUploaded.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.Greater(t, e2eUploaded.Load(), int32(0), "agent did not upload recording; output: %s", output.String())

	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		require.NoError(t, err, "agent output: %s", output.String())
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("agent did not stop after SIGTERM")
	}
	return dbPath
}

func assertUploadedState(t *testing.T, dbPath string) {
	t.Helper()
	db, err := store.NewSQLite(dbPath)
	require.NoError(t, err)
	defer db.Close()
	uploaded, err := db.GetFilesByState(store.StateUploaded)
	require.NoError(t, err)
	assert.Len(t, uploaded, 1)
}

var e2eUploaded atomic.Int32

type mockServer struct {
	*httptest.Server
	registrations atomic.Int32
	heartbeats    atomic.Int32
	mu            sync.Mutex
	lastToken     string
	userToken     string
}

func newMockServer(t *testing.T, userToken string) *mockServer {
	t.Helper()
	e2eUploaded.Store(0)
	mock := &mockServer{userToken: userToken}
	mock.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		switch r.URL.Path {
		case "/api/agent/register":
			assert.Equal(t, mock.userToken, token)
			mock.registrations.Add(1)
			writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{
				"device": map[string]any{"id": "device-1", "name": "robot", "type": "robot"},
				"token":  "registered-device-token",
				"isNew":  true,
			}})
		case "/api/agent/config":
			writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
				"uploadChunkSize": 1048576, "maxConcurrentUploads": 1, "heartbeatInterval": 30,
				"allowedFileTypes": []string{".bag"}, "maxFileSize": 10485760,
			}})
		case "/api/agent/heartbeat":
			mock.heartbeats.Add(1)
			writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"ack": true, "serverTime": time.Now().UTC().Format(time.RFC3339)}})
		case "/api/recordings/upload":
			mock.mu.Lock()
			mock.lastToken = token
			mock.mu.Unlock()
			e2eUploaded.Add(1)
			writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{"recordingId": "recording-1", "url": "https://example.invalid/recording-1", "size": 1, "createdAt": time.Now().UTC().Format(time.RFC3339)}})
		default:
			http.NotFound(w, r)
		}
	}))
	return mock
}

func (s *mockServer) uploadToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastToken
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
