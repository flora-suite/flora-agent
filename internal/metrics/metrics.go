// Package metrics provides Prometheus metrics for flora-agent.
package metrics

import (
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	once     sync.Once
	registry *prometheus.Registry

	// FilesDiscovered tracks total files discovered.
	FilesDiscovered = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "flora_agent_files_discovered_total",
		Help: "Total number of files discovered",
	})

	// FilesValidated tracks files that passed validation.
	FilesValidated = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "flora_agent_files_validated_total",
		Help: "Total number of files that passed validation",
	})

	// FilesInvalid tracks files that failed validation.
	FilesInvalid = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "flora_agent_files_invalid_total",
		Help: "Total number of files that failed validation",
	})

	// FilesUploaded tracks successfully uploaded files.
	FilesUploaded = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "flora_agent_files_uploaded_total",
		Help: "Total number of files successfully uploaded",
	})

	// FilesUploadFailed tracks failed file uploads.
	FilesUploadFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "flora_agent_files_upload_failed_total",
		Help: "Total number of file uploads that failed",
	})

	// BytesUploaded tracks total bytes uploaded.
	BytesUploaded = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "flora_agent_bytes_uploaded_total",
		Help: "Total bytes uploaded",
	})

	// UploadDuration tracks upload duration in seconds.
	UploadDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "flora_agent_upload_duration_seconds",
		Help:    "Upload duration in seconds",
		Buckets: prometheus.ExponentialBuckets(0.1, 2, 10), // 0.1s to ~100s
	})

	// ValidationDuration tracks validation duration in seconds.
	ValidationDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "flora_agent_validation_duration_seconds",
		Help:    "Validation duration in seconds",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 10), // 0.01s to ~10s
	})

	// PendingFiles tracks current number of files pending upload.
	PendingFiles = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "flora_agent_pending_files",
		Help: "Current number of files pending upload",
	})

	// UploadingFiles tracks current number of files being uploaded.
	UploadingFiles = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "flora_agent_uploading_files",
		Help: "Current number of files being uploaded",
	})

	// HeartbeatSuccess tracks successful heartbeats.
	HeartbeatSuccess = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "flora_agent_heartbeat_success_total",
		Help: "Total number of successful heartbeats",
	})

	// HeartbeatFailed tracks failed heartbeats.
	HeartbeatFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "flora_agent_heartbeat_failed_total",
		Help: "Total number of failed heartbeats",
	})

	// RetryAttempts tracks retry attempts.
	RetryAttempts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "flora_agent_retry_attempts_total",
		Help: "Total number of retry attempts by operation",
	}, []string{"operation"})

	// WatcherEvents tracks watcher events by type.
	WatcherEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "flora_agent_watcher_events_total",
		Help: "Total number of watcher events by type",
	}, []string{"type"})

	// ActiveWatchers tracks number of active directory watchers.
	ActiveWatchers = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "flora_agent_active_watchers",
		Help: "Number of active directory watchers",
	})

	// FileSize tracks file sizes.
	FileSize = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "flora_agent_file_size_bytes",
		Help:    "File sizes in bytes",
		Buckets: prometheus.ExponentialBuckets(1024, 4, 10), // 1KB to ~1TB
	})

	// ChunksUploaded tracks number of chunks uploaded.
	ChunksUploaded = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "flora_agent_chunks_uploaded_total",
		Help: "Total number of chunks uploaded",
	})

	// AgentInfo provides agent information.
	AgentInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "flora_agent_info",
		Help: "Agent information",
	}, []string{"version", "go_version"})
)

// Init initializes the metrics registry.
func Init() {
	once.Do(func() {
		registry = prometheus.NewRegistry()

		// Register all metrics
		registry.MustRegister(
			FilesDiscovered,
			FilesValidated,
			FilesInvalid,
			FilesUploaded,
			FilesUploadFailed,
			BytesUploaded,
			UploadDuration,
			ValidationDuration,
			PendingFiles,
			UploadingFiles,
			HeartbeatSuccess,
			HeartbeatFailed,
			RetryAttempts,
			WatcherEvents,
			ActiveWatchers,
			FileSize,
			ChunksUploaded,
			AgentInfo,
		)

		// Also register Go runtime metrics
		registry.MustRegister(prometheus.NewGoCollector())
		registry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	})
}

// Handler returns the HTTP handler for metrics endpoint.
func Handler() http.Handler {
	Init()
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// SetAgentInfo sets agent version info.
func SetAgentInfo(version, goVersion string) {
	AgentInfo.WithLabelValues(version, goVersion).Set(1)
}

// RecordUpload records metrics for a file upload.
func RecordUpload(success bool, size int64, duration float64) {
	if success {
		FilesUploaded.Inc()
		BytesUploaded.Add(float64(size))
	} else {
		FilesUploadFailed.Inc()
	}
	UploadDuration.Observe(duration)
	FileSize.Observe(float64(size))
}

// RecordValidation records metrics for file validation.
func RecordValidation(valid bool, duration float64) {
	if valid {
		FilesValidated.Inc()
	} else {
		FilesInvalid.Inc()
	}
	ValidationDuration.Observe(duration)
}

// RecordWatcherEvent records a watcher event.
func RecordWatcherEvent(eventType string) {
	WatcherEvents.WithLabelValues(eventType).Inc()
}

// RecordRetry records a retry attempt.
func RecordRetry(operation string) {
	RetryAttempts.WithLabelValues(operation).Inc()
}
