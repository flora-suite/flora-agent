// Package main is the entry point for flora-agent.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/flora-suite/flora-agent/internal/agent"
	"github.com/flora-suite/flora-agent/internal/api"
	"github.com/flora-suite/flora-agent/internal/register"
	"github.com/flora-suite/flora-agent/internal/retry"
	"github.com/flora-suite/flora-agent/internal/store"
	"github.com/flora-suite/flora-agent/internal/uploader"
	"github.com/flora-suite/flora-agent/internal/validator"
	"github.com/flora-suite/flora-agent/internal/watcher"
	"github.com/flora-suite/flora-agent/pkg/version"
)

var cfgFile string

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "flora-agent",
	Short: "Flora Agent - Edge agent for syncing recording files to flora-server",
	Long: `Flora Agent is a lightweight edge agent that monitors directories for
recording files (MCAP, ROS bag, db3), validates them, and uploads
them to flora-server.

It runs on robots, edge devices, or any machine that produces
recording files that need to be synchronized to the cloud.`,
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the agent (default mode)",
	Long:  `Start the flora-agent daemon. It will watch configured directories and upload files to flora-server.`,
	RunE:  runAgent,
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "One-time sync and exit",
	Long:  `Perform a one-time scan and upload, then exit. Useful for cron jobs or manual syncing.`,
	RunE:  runSync,
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configuration management",
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration file",
	RunE:  runConfigValidate,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version.Info())
	},
}

var registerCmd = &cobra.Command{
	Use:   "register",
	Short: "Register this device with flora-server",
	Long: `Register this device using a web-based flow.

This command collects device information and initiates a registration process.
You will need to open a URL in your browser to complete the registration.

After registration, a configuration file is generated and optionally a
system service is installed.`,
	RunE: runRegister,
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: /etc/flora-agent/agent.yaml)")
	rootCmd.PersistentFlags().String("log-level", "info", "log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().String("log-format", "json", "log format (json, text)")

	viper.BindPFlag("log.level", rootCmd.PersistentFlags().Lookup("log-level"))
	viper.BindPFlag("log.format", rootCmd.PersistentFlags().Lookup("log-format"))

	// Run command flags
	runCmd.Flags().StringSlice("watch", nil, "directories to watch (can be specified multiple times)")
	runCmd.Flags().String("server-url", "", "flora-server URL")
	runCmd.Flags().String("device-token", "", "device authentication token")

	viper.BindPFlag("watch.paths", runCmd.Flags().Lookup("watch"))
	viper.BindPFlag("server.url", runCmd.Flags().Lookup("server-url"))
	viper.BindPFlag("server.device_token", runCmd.Flags().Lookup("device-token"))

	// Register command flags
	registerCmd.Flags().String("server", "", "flora-server API URL (recommended for self-hosted servers)")
	registerCmd.Flags().String("output", "", "config file output path (default: auto-detect)")
	registerCmd.Flags().Bool("no-service", false, "skip service installation prompt")
	registerCmd.Flags().Bool("install-service", false, "automatically install system service")
	registerCmd.Flags().String("service-type", "", "service type: systemd, launchd (default: auto-detect)")

	// Add commands
	configCmd.AddCommand(configValidateCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(registerCmd)
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("agent")
		viper.SetConfigType("yaml")
		viper.AddConfigPath("/etc/flora-agent")
		viper.AddConfigPath("$HOME/.flora-agent")
		viper.AddConfigPath(".")
	}

	// Environment variables
	viper.SetEnvPrefix("FLORA")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// Set defaults
	defaults := agent.DefaultConfig()
	viper.SetDefault("server.url", defaults.Server.URL)
	viper.SetDefault("watch.patterns.include", defaults.Watch.Patterns.Include)
	viper.SetDefault("watch.patterns.exclude", defaults.Watch.Patterns.Exclude)
	viper.SetDefault("watch.scan_interval", defaults.Watch.ScanInterval)
	viper.SetDefault("watch.min_file_age", defaults.Watch.MinFileAge)
	viper.SetDefault("upload.enabled", defaults.Upload.Enabled)
	viper.SetDefault("upload.concurrent", defaults.Upload.Concurrent)
	viper.SetDefault("upload.chunk_size", defaults.Upload.ChunkSize)
	viper.SetDefault("upload.retry_attempts", defaults.Upload.RetryAttempts)
	viper.SetDefault("upload.retry_delay", defaults.Upload.RetryDelay)
	viper.SetDefault("storage.db_path", defaults.Storage.DBPath)
	viper.SetDefault("health.enabled", defaults.Health.Enabled)
	viper.SetDefault("health.port", defaults.Health.Port)
	viper.SetDefault("health.path", defaults.Health.Path)
	viper.SetDefault("metrics.enabled", defaults.Metrics.Enabled)
	viper.SetDefault("metrics.port", defaults.Metrics.Port)
	viper.SetDefault("metrics.path", defaults.Metrics.Path)
	viper.SetDefault("log.level", defaults.Log.Level)
	viper.SetDefault("log.format", defaults.Log.Format)
	viper.SetDefault("log.output", defaults.Log.Output)
	viper.SetDefault("log.file_path", defaults.Log.FilePath)

	// Read config file
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintf(os.Stderr, "Error reading config file: %v\n", err)
		}
	}
}

func loadConfig() (*agent.Config, error) {
	var cfg agent.Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	return &cfg, nil
}

func setupLogger(cfg *agent.Config) (zerolog.Logger, io.Closer, error) {
	var log zerolog.Logger

	// Set output
	var output io.Writer = os.Stdout
	var closer io.Closer
	if cfg.Log.Output == "file" {
		if cfg.Log.FilePath == "" {
			return zerolog.Logger{}, nil, fmt.Errorf("log.file_path is required when log.output is file")
		}
		if err := os.MkdirAll(filepath.Dir(cfg.Log.FilePath), 0755); err != nil {
			return zerolog.Logger{}, nil, fmt.Errorf("failed to create log directory: %w", err)
		}
		file, err := os.OpenFile(cfg.Log.FilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return zerolog.Logger{}, nil, fmt.Errorf("failed to open log file: %w", err)
		}
		output = file
		closer = file
	}
	if cfg.Log.Format == "text" {
		log = zerolog.New(zerolog.ConsoleWriter{Out: output}).With().Timestamp().Logger()
	} else {
		log = zerolog.New(output).With().Timestamp().Logger()
	}

	// Set level
	switch cfg.Log.Level {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	return log, closer, nil
}

func runAgent(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if len(cfg.Watch.Paths) == 0 {
		return fmt.Errorf("at least one watch path is required (set via --watch, config file, or FLORA_WATCH_PATHS)")
	}

	log, closer, err := setupLogger(cfg)
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer.Close()
	}
	log.Info().
		Str("version", version.Short()).
		Str("server", cfg.Server.URL).
		Strs("paths", cfg.Watch.Paths).
		Msg("starting flora-agent")

	a, err := agent.New(cfg, log)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	return a.Run()
}

func runSync(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if len(cfg.Watch.Paths) == 0 {
		return fmt.Errorf("at least one watch path is required")
	}

	log, closer, err := setupLogger(cfg)
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer.Close()
	}
	log.Info().Msg("running one-time sync")

	// Initialize components
	st, err := store.NewSQLite(cfg.Storage.DBPath)
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer st.Close()

	deviceToken, err := agent.ResolveDeviceToken(context.Background(), cfg, st, log)
	if err != nil {
		return fmt.Errorf("failed to resolve device token: %w", err)
	}
	client := api.NewClient(cfg.Server.URL, deviceToken)
	v := validator.New(log)
	u := uploader.New(client, cfg.Upload.Concurrent, cfg.Upload.ChunkSize, log,
		uploader.WithRetryConfig(retry.Config{MaxAttempts: cfg.Upload.RetryAttempts, InitialDelay: cfg.Upload.RetryDelay, MaxDelay: cfg.Upload.RetryDelay, Multiplier: 1}),
		uploader.WithBandwidthLimit(cfg.Upload.BandwidthLimit),
	)

	w, err := watcher.New(cfg.Watch.Paths, cfg.Watch.Patterns.Include, cfg.Watch.Patterns.Exclude, log)
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer w.Stop()

	// Scan for files
	log.Info().Strs("paths", cfg.Watch.Paths).Msg("scanning directories")
	files, err := w.Scan()
	if err != nil {
		return fmt.Errorf("failed to scan directories: %w", err)
	}

	log.Info().Int("count", len(files)).Msg("found files")

	// Process each file
	ctx := context.Background()
	successCount := 0
	failCount := 0

	for _, filePath := range files {
		// Check if already uploaded
		existing, err := st.GetFile(filePath)
		if err != nil {
			log.Error().Err(err).Str("path", filePath).Msg("failed to check file state")
			continue
		}
		if existing != nil && existing.State == store.StateUploaded {
			log.Debug().Str("path", filePath).Msg("already uploaded, skipping")
			continue
		}

		// Get file info
		info, err := os.Stat(filePath)
		if err != nil {
			log.Error().Err(err).Str("path", filePath).Msg("failed to stat file")
			failCount++
			continue
		}

		// Check file age
		if time.Since(info.ModTime()) < cfg.Watch.MinFileAge {
			log.Debug().Str("path", filePath).Msg("file too new, skipping")
			continue
		}

		log.Info().Str("path", filePath).Int64("size", info.Size()).Msg("processing file")

		// Create or update store record
		file := &store.File{
			Path:  filePath,
			Size:  info.Size(),
			MTime: info.ModTime().Unix(),
			State: store.StateDiscovered,
		}
		if err := st.UpsertFile(file); err != nil {
			log.Error().Err(err).Str("path", filePath).Msg("failed to upsert file")
			failCount++
			continue
		}

		// Validate
		result, err := v.Validate(filePath)
		if err != nil {
			log.Error().Err(err).Str("path", filePath).Msg("validation error")
			file.State = store.StateInvalid
			file.ErrorMessage = err.Error()
			st.UpsertFile(file)
			failCount++
			continue
		}

		if !result.Valid {
			log.Warn().Str("path", filePath).Err(result.Error).Msg("file is invalid")
			file.State = store.StateInvalid
			if result.Error != nil {
				file.ErrorMessage = result.Error.Error()
			}
			st.UpsertFile(file)
			failCount++
			continue
		}

		file.State = store.StateValidated
		file.FileType = result.FileType
		file.Checksum = result.Checksum
		file.Metadata = result.Metadata
		if err := st.UpsertFile(file); err != nil {
			log.Error().Err(err).Str("path", filePath).Msg("failed to update file state")
		}

		// Upload if enabled
		if !cfg.Upload.Enabled {
			log.Info().Str("path", filePath).Msg("upload disabled, skipping upload")
			continue
		}

		file.State = store.StateUploading
		st.UpsertFile(file)

		if err := u.Upload(ctx, file); err != nil {
			log.Error().Err(err).Str("path", filePath).Msg("upload failed")
			file.State = store.StateFailed
			file.ErrorMessage = err.Error()
			st.UpsertFile(file)
			failCount++
			continue
		}

		file.State = store.StateUploaded
		file.ErrorMessage = ""
		if err := st.UpsertFile(file); err != nil {
			log.Error().Err(err).Str("path", filePath).Msg("failed to update file state")
		}

		log.Info().Str("path", filePath).Msg("upload completed")
		successCount++
	}

	log.Info().
		Int("total", len(files)).
		Int("success", successCount).
		Int("failed", failCount).
		Msg("sync completed")

	if failCount > 0 {
		return fmt.Errorf("%d files failed to process", failCount)
	}

	return nil
}

func runConfigValidate(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	fmt.Println("Configuration is valid:")
	fmt.Printf("  Server URL: %s\n", cfg.Server.URL)
	fmt.Printf("  Device Token: %s***\n", cfg.Server.DeviceToken[:min(8, len(cfg.Server.DeviceToken))])
	fmt.Printf("  Watch Paths: %v\n", cfg.Watch.Paths)
	fmt.Printf("  Scan Interval: %s\n", cfg.Watch.ScanInterval)
	fmt.Printf("  Upload Enabled: %v\n", cfg.Upload.Enabled)
	fmt.Printf("  DB Path: %s\n", cfg.Storage.DBPath)

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func runRegister(cmd *cobra.Command, args []string) error {
	serverURL := registrationServerURL(cmd)
	outputPath, _ := cmd.Flags().GetString("output")
	noService, _ := cmd.Flags().GetBool("no-service")
	installService, _ := cmd.Flags().GetBool("install-service")
	serviceType, _ := cmd.Flags().GetString("service-type")

	opts := register.DefaultOptions()
	opts.ServerURL = serverURL

	if outputPath != "" {
		opts.OutputPath = outputPath
	}

	opts.NoService = noService
	opts.InstallService = installService
	opts.ServiceType = serviceType

	return register.Run(opts)
}

func registrationServerURL(cmd *cobra.Command) string {
	serverURL, _ := cmd.Flags().GetString("server")
	serverURL = strings.TrimSpace(serverURL)
	if serverURL != "" {
		return serverURL
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: --server was not provided; using the default API URL %s. To register with a self-hosted server, pass --server https://your-server.example.\n", register.DefaultServerURL)
	return register.DefaultServerURL
}
