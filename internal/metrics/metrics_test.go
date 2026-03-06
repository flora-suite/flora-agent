package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsHandler(t *testing.T) {
	Init()

	handler := Handler()
	require.NotNil(t, handler)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// Check for some expected metrics
	bodyStr := string(body)
	assert.Contains(t, bodyStr, "flora_agent_files_discovered_total")
	assert.Contains(t, bodyStr, "flora_agent_files_uploaded_total")
	assert.Contains(t, bodyStr, "go_goroutines")
}

func TestRecordUpload_Success(t *testing.T) {
	Init()

	// Record a successful upload
	RecordUpload(true, 1024*1024, 2.5)

	// We can't easily verify the counter value directly without exposing internals
	// but we can verify no panic occurs
}

func TestRecordUpload_Failure(t *testing.T) {
	Init()

	RecordUpload(false, 1024, 0.5)
}

func TestRecordValidation(t *testing.T) {
	Init()

	RecordValidation(true, 0.1)
	RecordValidation(false, 0.05)
}

func TestRecordWatcherEvent(t *testing.T) {
	Init()

	RecordWatcherEvent("create")
	RecordWatcherEvent("write")
	RecordWatcherEvent("remove")
}

func TestRecordRetry(t *testing.T) {
	Init()

	RecordRetry("upload")
	RecordRetry("heartbeat")
}

func TestSetAgentInfo(t *testing.T) {
	Init()

	SetAgentInfo("1.0.0", "go1.22.0")

	// Verify via metrics endpoint
	handler := Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	bodyStr := string(body)
	assert.True(t, strings.Contains(bodyStr, "flora_agent_info"))
	assert.True(t, strings.Contains(bodyStr, "version=\"1.0.0\""))
}

func TestMetricsLabels(t *testing.T) {
	Init()

	// Test metrics with labels
	t.Run("RetryAttempts", func(t *testing.T) {
		RetryAttempts.WithLabelValues("upload").Inc()
		RetryAttempts.WithLabelValues("heartbeat").Inc()
	})

	t.Run("WatcherEvents", func(t *testing.T) {
		WatcherEvents.WithLabelValues("create").Inc()
		WatcherEvents.WithLabelValues("write").Inc()
		WatcherEvents.WithLabelValues("remove").Inc()
	})
}

func TestGaugeMetrics(t *testing.T) {
	Init()

	// Test gauge operations
	t.Run("PendingFiles", func(t *testing.T) {
		PendingFiles.Set(5)
		PendingFiles.Inc()
		PendingFiles.Dec()
	})

	t.Run("UploadingFiles", func(t *testing.T) {
		UploadingFiles.Set(2)
		UploadingFiles.Add(3)
		UploadingFiles.Sub(1)
	})

	t.Run("ActiveWatchers", func(t *testing.T) {
		ActiveWatchers.Set(3)
	})
}
