package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHandler(t *testing.T) {
	h := NewHandler()
	require.NotNil(t, h)
	assert.NotNil(t, h.checkers)
	assert.False(t, h.startTime.IsZero())
}

func TestRegisterChecker(t *testing.T) {
	h := NewHandler()
	h.RegisterChecker("test", func() ComponentStatus {
		return ComponentStatus{Status: StatusHealthy}
	})

	assert.Len(t, h.checkers, 1)
}

func TestCheck_NoCheckers(t *testing.T) {
	h := NewHandler()

	response := h.Check()

	assert.Equal(t, StatusHealthy, response.Status)
	assert.NotEmpty(t, response.Version)
	assert.NotEmpty(t, response.Uptime)
	assert.NotEmpty(t, response.Timestamp)
	assert.Empty(t, response.Components)
}

func TestCheck_AllHealthy(t *testing.T) {
	h := NewHandler()
	h.RegisterChecker("db", func() ComponentStatus {
		return ComponentStatus{Status: StatusHealthy, Message: "connected"}
	})
	h.RegisterChecker("server", func() ComponentStatus {
		return ComponentStatus{Status: StatusHealthy, Message: "reachable"}
	})

	response := h.Check()

	assert.Equal(t, StatusHealthy, response.Status)
	assert.Len(t, response.Components, 2)
	assert.Equal(t, StatusHealthy, response.Components["db"].Status)
	assert.Equal(t, StatusHealthy, response.Components["server"].Status)
}

func TestCheck_OneDegraded(t *testing.T) {
	h := NewHandler()
	h.RegisterChecker("db", func() ComponentStatus {
		return ComponentStatus{Status: StatusHealthy}
	})
	h.RegisterChecker("server", func() ComponentStatus {
		return ComponentStatus{Status: StatusDegraded, Message: "slow response"}
	})

	response := h.Check()

	assert.Equal(t, StatusDegraded, response.Status)
}

func TestCheck_OneUnhealthy(t *testing.T) {
	h := NewHandler()
	h.RegisterChecker("db", func() ComponentStatus {
		return ComponentStatus{Status: StatusHealthy}
	})
	h.RegisterChecker("server", func() ComponentStatus {
		return ComponentStatus{Status: StatusUnhealthy, Message: "unreachable"}
	})

	response := h.Check()

	assert.Equal(t, StatusUnhealthy, response.Status)
}

func TestCheck_UnhealthyTakesPrecedence(t *testing.T) {
	h := NewHandler()
	h.RegisterChecker("db", func() ComponentStatus {
		return ComponentStatus{Status: StatusDegraded}
	})
	h.RegisterChecker("server", func() ComponentStatus {
		return ComponentStatus{Status: StatusUnhealthy}
	})
	h.RegisterChecker("cache", func() ComponentStatus {
		return ComponentStatus{Status: StatusHealthy}
	})

	response := h.Check()

	assert.Equal(t, StatusUnhealthy, response.Status)
}

func TestServeHTTP_Healthy(t *testing.T) {
	h := NewHandler()
	h.RegisterChecker("test", func() ComponentStatus {
		return ComponentStatus{Status: StatusHealthy}
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response Response
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, StatusHealthy, response.Status)
}

func TestServeHTTP_Unhealthy(t *testing.T) {
	h := NewHandler()
	h.RegisterChecker("test", func() ComponentStatus {
		return ComponentStatus{Status: StatusUnhealthy, Message: "down"}
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var response Response
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, StatusUnhealthy, response.Status)
}

func TestServeHTTP_MethodNotAllowed(t *testing.T) {
	h := NewHandler()

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestLivenessHandler(t *testing.T) {
	handler := LivenessHandler()

	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]string
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "alive", response["status"])
}

func TestReadinessHandler_Ready(t *testing.T) {
	h := NewHandler()
	h.RegisterChecker("test", func() ComponentStatus {
		return ComponentStatus{Status: StatusHealthy}
	})

	handler := h.ReadinessHandler()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]string
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "ready", response["status"])
}

func TestReadinessHandler_NotReady(t *testing.T) {
	h := NewHandler()
	h.RegisterChecker("test", func() ComponentStatus {
		return ComponentStatus{Status: StatusUnhealthy}
	})

	handler := h.ReadinessHandler()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var response map[string]string
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "not ready", response["status"])
}
