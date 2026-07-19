// Package agent provides the core agent logic.
package agent

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/flora-suite/flora-agent/internal/api"
	"github.com/flora-suite/flora-agent/internal/health"
	"github.com/flora-suite/flora-agent/internal/metrics"
	"github.com/flora-suite/flora-agent/internal/retry"
	"github.com/flora-suite/flora-agent/internal/store"
	"github.com/flora-suite/flora-agent/internal/sysinfo"
	"github.com/flora-suite/flora-agent/internal/uploader"
	"github.com/flora-suite/flora-agent/internal/validator"
	"github.com/flora-suite/flora-agent/internal/watcher"
	"github.com/flora-suite/flora-agent/pkg/version"
)

// Agent is the main agent that coordinates file watching, validation, and uploading.
type Agent struct {
	config    *Config
	log       zerolog.Logger
	store     store.Store
	client    *api.Client
	watcher   watcher.Watcher
	validator validator.Validator
	uploader  uploader.Uploader
	sysinfo   *sysinfo.Collector
	health    *health.Handler

	heartbeatInterval time.Duration
	allowedFileTypes  map[string]struct{}
	maxFileSize       int64
	statusMu          sync.RWMutex
	watcherStarted    bool
	lastHeartbeatErr  error

	metricsServer *http.Server
	healthServer  *http.Server

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a new Agent with the given configuration.
func New(cfg *Config, log zerolog.Logger) (*Agent, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize store
	st, err := store.NewSQLite(cfg.Storage.DBPath)
	if err != nil {
		cancel()
		return nil, err
	}

	// Resolve device token (from config, stored, or registration)
	deviceToken, err := ResolveDeviceToken(ctx, cfg, st, log)
	if err != nil {
		cancel()
		st.Close()
		return nil, fmt.Errorf("failed to resolve device token: %w", err)
	}

	// Initialize API client with the resolved token
	client := api.NewClient(cfg.Server.URL, deviceToken)

	// Initialize watcher
	w, err := watcher.New(cfg.Watch.Paths, cfg.Watch.Patterns.Include, cfg.Watch.Patterns.Exclude, log)
	if err != nil {
		cancel()
		return nil, err
	}

	// Initialize validator
	v := validator.New(log)

	// Initialize uploader
	u := newUploader(client, cfg, log)

	// Initialize sysinfo collector
	sys := sysinfo.NewCollector()

	// Initialize health handler
	h := health.NewHandler()

	return &Agent{
		config:            cfg,
		log:               log,
		store:             st,
		client:            client,
		watcher:           w,
		validator:         v,
		uploader:          u,
		sysinfo:           sys,
		health:            h,
		heartbeatInterval: 30 * time.Second,
		allowedFileTypes:  allowedTypes(cfg.Watch.Patterns.Include),
		ctx:               ctx,
		cancel:            cancel,
	}, nil
}

func newUploader(client *api.Client, cfg *Config, log zerolog.Logger) *uploader.HTTPUploader {
	retryCfg := retryConfig(cfg)
	return uploader.New(client, cfg.Upload.Concurrent, cfg.Upload.ChunkSize, log,
		uploader.WithRetryConfig(retryCfg),
		uploader.WithBandwidthLimit(cfg.Upload.BandwidthLimit),
	)
}

func retryConfig(cfg *Config) retry.Config {
	return retry.Config{
		MaxAttempts:  cfg.Upload.RetryAttempts,
		InitialDelay: cfg.Upload.RetryDelay,
		MaxDelay:     cfg.Upload.RetryDelay,
		Multiplier:   1,
		Jitter:       0,
	}
}

func allowedTypes(patterns []string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(patterns))
	for _, pattern := range patterns {
		if strings.HasPrefix(pattern, "*.") {
			allowed[strings.TrimPrefix(pattern, "*")] = struct{}{}
		}
	}
	return allowed
}

// resolveDeviceToken determines the device token to use.
// Priority: 1) Config device_token, 2) Stored token, 3) Register new device
// ResolveDeviceToken determines the device token to use.
func ResolveDeviceToken(ctx context.Context, cfg *Config, st store.Store, log zerolog.Logger) (string, error) {
	// 1. Check if token is provided in config
	if cfg.Server.DeviceToken != "" {
		log.Debug().Msg("using device token from config")
		return cfg.Server.DeviceToken, nil
	}

	// 2. Check if token is stored in database
	storedToken, err := st.GetConfig("device_token")
	if err == nil && storedToken != "" {
		log.Debug().Msg("using stored device token")
		return storedToken, nil
	}

	// 3. If user token is provided, register a new device
	if cfg.Server.UserToken != "" {
		log.Info().Msg("registering device with flora-server")
		return registerDevice(ctx, cfg, st, log)
	}

	// No token available
	return "", fmt.Errorf("no device token configured. Provide server.device_token or server.user_token for registration")
}

// registerDevice registers the device with flora-server using user token.
func registerDevice(ctx context.Context, cfg *Config, st store.Store, log zerolog.Logger) (string, error) {
	// Create temporary client for registration (no device token yet)
	tempClient := api.NewClient(cfg.Server.URL, "")

	// Get device name (use hostname if not configured)
	deviceName := cfg.Server.DeviceName
	if deviceName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			deviceName = "flora-agent"
		} else {
			deviceName = hostname
		}
	}

	// Get device type
	deviceType := cfg.Server.DeviceType
	if deviceType == "" {
		deviceType = "robot"
	}

	// Get machine ID for unique identification
	machineID := getMachineID()

	req := &api.RegisterRequest{
		Name:      deviceName,
		Type:      deviceType,
		MachineID: machineID,
	}

	resp, err := tempClient.Register(ctx, cfg.Server.UserToken, req)
	if err != nil {
		return "", fmt.Errorf("failed to register device: %w", err)
	}

	// Store the device token for future use
	if err := st.SetConfig("device_token", resp.Token); err != nil {
		log.Warn().Err(err).Msg("failed to store device token")
	}

	// Store device ID for reference
	if err := st.SetConfig("device_id", resp.Device.ID); err != nil {
		log.Warn().Err(err).Msg("failed to store device ID")
	}

	if resp.IsNew {
		log.Info().Str("device_id", resp.Device.ID).Str("name", resp.Device.Name).Msg("device registered successfully")
	} else {
		log.Info().Str("device_id", resp.Device.ID).Str("name", resp.Device.Name).Msg("device re-registered (already existed)")
	}

	return resp.Token, nil
}

// getMachineID returns a unique identifier for this machine.
func getMachineID() string {
	// Try to read from /etc/machine-id (Linux)
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		id := string(data)
		if len(id) > 0 {
			// Trim newlines
			for len(id) > 0 && (id[len(id)-1] == '\n' || id[len(id)-1] == '\r') {
				id = id[:len(id)-1]
			}
			return id
		}
	}

	// Try to read from /var/lib/dbus/machine-id (fallback on some Linux systems)
	if data, err := os.ReadFile("/var/lib/dbus/machine-id"); err == nil {
		id := string(data)
		if len(id) > 0 {
			for len(id) > 0 && (id[len(id)-1] == '\n' || id[len(id)-1] == '\r') {
				id = id[:len(id)-1]
			}
			return id
		}
	}

	// Fallback: generate from hostname + random bytes
	hostname, _ := os.Hostname()
	return hostname + "-" + fmt.Sprintf("%d", time.Now().UnixNano())
}

// Run starts the agent and blocks until shutdown.
func (a *Agent) Run() error {
	a.log.Info().Msg("starting flora-agent")
	a.applyRemoteConfig()

	// Initialize metrics
	metrics.Init()
	metrics.SetAgentInfo(version.Short(), runtime.Version())

	// Register health checkers
	a.registerHealthCheckers()

	// Start health server if enabled
	if a.config.Health.Enabled {
		a.wg.Add(1)
		go a.startHealthServer()
	}

	// Start metrics server if enabled
	if a.config.Metrics.Enabled {
		a.wg.Add(1)
		go a.startMetricsServer()
	}

	// Start heartbeat
	a.wg.Add(1)
	go a.heartbeatLoop()

	// Start file watcher
	a.wg.Add(1)
	go a.watchLoop()

	// Start periodic scanner
	a.wg.Add(1)
	go a.scanLoop()

	// Start upload processor
	a.wg.Add(1)
	go a.uploadLoop()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		a.log.Info().Str("signal", sig.String()).Msg("received shutdown signal")
	case <-a.ctx.Done():
	}

	return a.Shutdown()
}

func (a *Agent) applyRemoteConfig() {
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancel()
	remote, err := a.client.GetConfig(ctx)
	if err != nil {
		a.log.Warn().Err(err).Msg("failed to load remote agent configuration; using local configuration")
		return
	}
	if remote.UploadChunkSize > 0 {
		a.config.Upload.ChunkSize = remote.UploadChunkSize
	}
	if remote.MaxConcurrentUploads > 0 {
		a.config.Upload.Concurrent = remote.MaxConcurrentUploads
	}
	if remote.HeartbeatInterval > 0 {
		a.heartbeatInterval = time.Duration(remote.HeartbeatInterval) * time.Second
	}
	if len(remote.AllowedFileTypes) > 0 {
		a.allowedFileTypes = make(map[string]struct{}, len(remote.AllowedFileTypes))
		for _, extension := range remote.AllowedFileTypes {
			a.allowedFileTypes[strings.ToLower(extension)] = struct{}{}
		}
	}
	a.maxFileSize = remote.MaxFileSize
	a.uploader = newUploader(a.client, a.config, a.log)
	a.log.Info().Msg("loaded remote agent configuration")
}

// Shutdown gracefully stops the agent.
func (a *Agent) Shutdown() error {
	a.log.Info().Msg("shutting down flora-agent")
	a.cancel()

	// Shutdown health server if running
	if a.healthServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.healthServer.Shutdown(ctx); err != nil {
			a.log.Error().Err(err).Msg("error shutting down health server")
		}
	}

	// Shutdown metrics server if running
	if a.metricsServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.metricsServer.Shutdown(ctx); err != nil {
			a.log.Error().Err(err).Msg("error shutting down metrics server")
		}
	}

	a.wg.Wait()

	if err := a.store.Close(); err != nil {
		a.log.Error().Err(err).Msg("error closing store")
	}

	a.log.Info().Msg("flora-agent stopped")
	return nil
}

// startMetricsServer starts the Prometheus metrics HTTP server.
func (a *Agent) startMetricsServer() {
	defer a.wg.Done()

	mux := http.NewServeMux()
	mux.Handle(a.config.Metrics.Path, metrics.Handler())

	a.metricsServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", a.config.Metrics.Port),
		Handler: mux,
	}

	a.log.Info().
		Int("port", a.config.Metrics.Port).
		Str("path", a.config.Metrics.Path).
		Msg("starting metrics server")

	if err := a.metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		a.log.Error().Err(err).Msg("metrics server error")
	}
}

// heartbeatLoop sends periodic heartbeats to flora-server.
func (a *Agent) heartbeatLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(a.heartbeatInterval)
	defer ticker.Stop()

	// Send initial heartbeat
	if err := a.sendHeartbeat(); err != nil {
		a.log.Warn().Err(err).Msg("failed to send initial heartbeat")
	}

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if err := a.sendHeartbeat(); err != nil {
				a.log.Warn().Err(err).Msg("failed to send heartbeat")
			}
		}
	}
}

func (a *Agent) sendHeartbeat() error {
	// Collect system stats
	stats := a.sysinfo.CollectFast()

	// Get pending and uploading counts from store
	pendingCount := 0
	uploadingCount := 0

	pendingFiles, err := a.store.GetFilesByState(store.StateValidated)
	if err == nil {
		pendingCount = len(pendingFiles)
	}
	uploadingFiles, err := a.store.GetFilesByState(store.StateUploading)
	if err == nil {
		uploadingCount = len(uploadingFiles)
	}

	status := &api.HeartbeatStatus{
		Online: true,
		System: api.SystemStatus{
			CPUUsage:    stats.CPUUsage,
			MemoryUsage: stats.MemoryUsage,
			DiskUsage:   stats.DiskUsage,
			Uptime:      stats.Uptime,
		},
		Agent: api.AgentStatus{
			Version:        version.Short(),
			WatchedPaths:   a.config.Watch.Paths,
			PendingUploads: pendingCount,
			UploadingCount: uploadingCount,
		},
	}

	_, err = a.client.Heartbeat(a.ctx, status)
	a.statusMu.Lock()
	a.lastHeartbeatErr = err
	a.statusMu.Unlock()
	if err != nil {
		metrics.HeartbeatFailed.Inc()
	} else {
		metrics.HeartbeatSuccess.Inc()
	}
	return err
}

// watchLoop handles real-time file system events.
func (a *Agent) watchLoop() {
	defer a.wg.Done()

	events := a.watcher.Events()
	errors := a.watcher.Errors()

	if err := a.watcher.Start(); err != nil {
		a.statusMu.Lock()
		a.watcherStarted = false
		a.statusMu.Unlock()
		a.log.Error().Err(err).Msg("failed to start watcher")
		return
	}
	a.statusMu.Lock()
	a.watcherStarted = true
	a.statusMu.Unlock()
	defer a.watcher.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			a.handleFileEvent(event)
		case err, ok := <-errors:
			if !ok {
				return
			}
			a.log.Error().Err(err).Msg("watcher error")
		}
	}
}

func (a *Agent) handleFileEvent(event watcher.Event) {
	a.log.Debug().
		Str("path", event.Path).
		Str("op", event.Op.String()).
		Msg("file event")

	// Record watcher event metric
	metrics.RecordWatcherEvent(event.Op.String())

	if event.Op == watcher.Create || event.Op == watcher.Write {
		if err := a.processFile(event.Path); err != nil {
			a.log.Error().Err(err).Str("path", event.Path).Msg("failed to process file")
		}
	}
}

// scanLoop periodically scans watched directories for missed files.
func (a *Agent) scanLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(a.config.Watch.ScanInterval)
	defer ticker.Stop()

	// Initial scan
	a.scanDirectories()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.scanDirectories()
		}
	}
}

func (a *Agent) scanDirectories() {
	a.log.Debug().Msg("scanning directories")
	files, err := a.watcher.Scan()
	if err != nil {
		a.log.Error().Err(err).Msg("failed to scan directories")
		return
	}

	for _, path := range files {
		if err := a.processFile(path); err != nil {
			a.log.Error().Err(err).Str("path", path).Msg("failed to process file")
		}
	}
}

// processFile handles a discovered file.
func (a *Agent) processFile(path string) error {
	// Check if file is already known
	existing, err := a.store.GetFile(path)
	if err != nil {
		return err
	}
	// Check file age (avoid processing files still being written)
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if time.Since(info.ModTime()) < a.config.Watch.MinFileAge {
		a.log.Debug().Str("path", path).Msg("file too new, skipping")
		return nil
	}

	mtime := info.ModTime().Unix()
	isNewFile := existing == nil
	file := existing
	if existing != nil && existing.Size == info.Size() && existing.MTime == mtime {
		if existing.State == store.StateUploaded || existing.State == store.StateValidated || existing.State == store.StateUploading || existing.State == store.StateFailed {
			return nil
		}
	}
	if existing == nil {
		file = &store.File{Path: path}
	} else if existing.Size != info.Size() || existing.MTime != mtime {
		if err := a.store.DeleteChunks(existing.ID); err != nil {
			return err
		}
		file.UploadID = ""
		file.Metadata = nil
		file.Checksum = ""
		file.FileType = ""
		file.ErrorMessage = ""
	}
	file.Size = info.Size()
	file.MTime = mtime
	file.State = store.StateDiscovered
	if err := a.store.UpsertFile(file); err != nil {
		return err
	}

	// Record discovery if new file
	if isNewFile {
		metrics.FilesDiscovered.Inc()
		metrics.FileSize.Observe(float64(info.Size()))
	}

	if a.maxFileSize > 0 && file.Size > a.maxFileSize {
		file.State = store.StateInvalid
		file.ErrorMessage = fmt.Sprintf("file size %d exceeds server maximum %d", file.Size, a.maxFileSize)
		return a.store.UpsertFile(file)
	}
	if len(a.allowedFileTypes) > 0 {
		if _, ok := a.allowedFileTypes[strings.ToLower(filepath.Ext(path))]; !ok {
			file.State = store.StateInvalid
			file.ErrorMessage = "file type is not allowed by server configuration"
			return a.store.UpsertFile(file)
		}
	}

	// Validate the file
	validationStart := time.Now()
	result, err := a.validator.Validate(path)
	validationDuration := time.Since(validationStart).Seconds()

	if err != nil || !result.Valid {
		metrics.RecordValidation(false, validationDuration)
		file.State = store.StateInvalid
		if err != nil {
			file.ErrorMessage = err.Error()
		} else if result.Error != nil {
			file.ErrorMessage = result.Error.Error()
		} else {
			file.ErrorMessage = "file validation failed"
		}
		return a.store.UpsertFile(file)
	}

	metrics.RecordValidation(true, validationDuration)
	file.State = store.StateValidated
	file.FileType = result.FileType
	file.Checksum = result.Checksum
	file.Metadata = result.Metadata

	// Update pending files gauge
	metrics.PendingFiles.Inc()

	return a.store.UpsertFile(file)
}

// uploadLoop processes validated files for upload.
func (a *Agent) uploadLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.processUploads()
		}
	}
}

func (a *Agent) processUploads() {
	if !a.config.Upload.Enabled {
		return
	}

	// Get validated files, interrupted uploads, and failures for fixed-cycle retry.
	validatedFiles, err := a.store.GetFilesByState(store.StateValidated)
	if err != nil {
		a.log.Error().Err(err).Msg("failed to get validated files for upload")
		return
	}

	uploadingFiles, err := a.store.GetFilesByState(store.StateUploading)
	if err != nil {
		a.log.Error().Err(err).Msg("failed to get uploading files for resume")
		return
	}

	failedFiles, err := a.store.GetFilesByState(store.StateFailed)
	if err != nil {
		a.log.Error().Err(err).Msg("failed to get failed files for retry")
		return
	}

	// Resume takes priority, followed by new uploads and fixed-cycle retries.
	files := append(uploadingFiles, validatedFiles...)
	files = append(files, failedFiles...)

	workers := a.config.Upload.Concurrent
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan *store.File)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				a.processUpload(file)
			}
		}()
	}
	for _, file := range files {
		select {
		case <-a.ctx.Done():
			close(jobs)
			wg.Wait()
			return
		case jobs <- file:
		}
	}
	close(jobs)
	wg.Wait()
}

func (a *Agent) processUpload(file *store.File) {
	select {
	case <-a.ctx.Done():
		return
	default:
	}

	// Track if this is a resume or new upload.
	isResume := file.State == store.StateUploading

	if !isResume {
		wasValidated := file.State == store.StateValidated
		file.State = store.StateUploading
		if err := a.store.UpsertFile(file); err != nil {
			return
		}

		if wasValidated {
			metrics.PendingFiles.Dec()
		}
	}
	metrics.UploadingFiles.Inc()

	uploadStart := time.Now()
	var uploadErr error

	if resumableUploader, ok := a.uploader.(uploader.ResumableUploader); ok {
		uploadErr = resumableUploader.UploadWithStore(a.ctx, file, a.store)
	} else {
		uploadErr = a.uploader.Upload(a.ctx, file)
	}
	uploadDuration := time.Since(uploadStart).Seconds()

	metrics.UploadingFiles.Dec()

	if uploadErr != nil {
		a.log.Error().Err(uploadErr).Str("path", file.Path).Bool("resume", isResume).Msg("upload failed")
		metrics.RecordUpload(false, file.Size, uploadDuration)

		if a.ctx.Err() != nil {
			a.log.Info().Str("path", file.Path).Msg("upload interrupted, will resume later")
			return
		}

		file.State = store.StateFailed
		file.ErrorMessage = uploadErr.Error()
	} else {
		a.log.Info().Str("path", file.Path).Bool("resume", isResume).Msg("upload completed")
		metrics.RecordUpload(true, file.Size, uploadDuration)
		file.State = store.StateUploaded
		file.ErrorMessage = ""
	}

	if err := a.store.UpsertFile(file); err != nil {
		a.log.Error().Err(err).Msg("failed to update file state")
	}
}

// registerHealthCheckers sets up health check functions for each component.
func (a *Agent) registerHealthCheckers() {
	// Database health check
	a.health.RegisterChecker("database", func() health.ComponentStatus {
		if err := a.store.Ping(); err != nil {
			return health.ComponentStatus{
				Status:  health.StatusUnhealthy,
				Message: err.Error(),
			}
		}
		return health.ComponentStatus{
			Status:  health.StatusHealthy,
			Message: "connected",
		}
	})

	a.health.RegisterChecker("server", func() health.ComponentStatus {
		a.statusMu.RLock()
		err := a.lastHeartbeatErr
		a.statusMu.RUnlock()
		if err != nil {
			return health.ComponentStatus{Status: health.StatusDegraded, Message: err.Error()}
		}
		return health.ComponentStatus{
			Status:  health.StatusHealthy,
			Message: "heartbeat healthy",
		}
	})

	// Watcher health check
	a.health.RegisterChecker("watcher", func() health.ComponentStatus {
		a.statusMu.RLock()
		started := a.watcherStarted
		a.statusMu.RUnlock()
		if !started {
			return health.ComponentStatus{Status: health.StatusUnhealthy, Message: "watcher is not running"}
		}
		return health.ComponentStatus{
			Status:  health.StatusHealthy,
			Message: fmt.Sprintf("watching %d paths", len(a.config.Watch.Paths)),
		}
	})
}

// startHealthServer starts the HTTP health check server.
func (a *Agent) startHealthServer() {
	defer a.wg.Done()

	mux := http.NewServeMux()
	mux.Handle(a.config.Health.Path, a.health)
	mux.HandleFunc("/livez", health.LivenessHandler())
	mux.HandleFunc("/readyz", a.health.ReadinessHandler())

	a.healthServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", a.config.Health.Port),
		Handler: mux,
	}

	a.log.Info().
		Int("port", a.config.Health.Port).
		Str("path", a.config.Health.Path).
		Msg("starting health server")

	if err := a.healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		a.log.Error().Err(err).Msg("health server error")
	}
}

// HealthCheck returns the current health status (for programmatic access).
func (a *Agent) HealthCheck() health.Response {
	return a.health.Check()
}
