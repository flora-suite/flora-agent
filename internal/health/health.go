// Package health provides HTTP health check endpoints for the agent.
package health

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/flora-suite/flora-agent/pkg/version"
)

// Status represents the health status of a component.
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"
)

// ComponentStatus represents the health of a single component.
type ComponentStatus struct {
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
}

// Response is the health check response.
type Response struct {
	Status     Status                     `json:"status"`
	Version    string                     `json:"version"`
	Uptime     string                     `json:"uptime"`
	Components map[string]ComponentStatus `json:"components"`
	Timestamp  string                     `json:"timestamp"`
}

// Checker is a function that checks the health of a component.
type Checker func() ComponentStatus

// Handler manages health check endpoints.
type Handler struct {
	mu        sync.RWMutex
	checkers  map[string]Checker
	startTime time.Time
}

// NewHandler creates a new health handler.
func NewHandler() *Handler {
	return &Handler{
		checkers:  make(map[string]Checker),
		startTime: time.Now(),
	}
}

// RegisterChecker adds a health checker for a component.
func (h *Handler) RegisterChecker(name string, checker Checker) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkers[name] = checker
}

// Check performs all health checks and returns the aggregate status.
func (h *Handler) Check() Response {
	h.mu.RLock()
	defer h.mu.RUnlock()

	components := make(map[string]ComponentStatus)
	overallStatus := StatusHealthy

	for name, checker := range h.checkers {
		status := checker()
		components[name] = status

		// Determine overall status (worst case wins)
		if status.Status == StatusUnhealthy {
			overallStatus = StatusUnhealthy
		} else if status.Status == StatusDegraded && overallStatus != StatusUnhealthy {
			overallStatus = StatusDegraded
		}
	}

	return Response{
		Status:     overallStatus,
		Version:    version.Short(),
		Uptime:     time.Since(h.startTime).Round(time.Second).String(),
		Components: components,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}
}

// ServeHTTP implements http.Handler for the health check endpoint.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	response := h.Check()

	w.Header().Set("Content-Type", "application/json")
	switch response.Status {
	case StatusHealthy:
		w.WriteHeader(http.StatusOK)
	case StatusDegraded:
		w.WriteHeader(http.StatusOK) // Still return 200 for degraded
	case StatusUnhealthy:
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	json.NewEncoder(w).Encode(response)
}

// LivenessHandler returns a simple liveness check (for k8s /livez).
func LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "alive",
		})
	}
}

// ReadinessHandler returns a readiness check (for k8s /readyz).
func (h *Handler) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		response := h.Check()

		w.Header().Set("Content-Type", "application/json")
		if response.Status == StatusUnhealthy {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "not ready",
			})
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ready",
		})
	}
}
