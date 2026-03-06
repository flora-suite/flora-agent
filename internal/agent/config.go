// Package agent provides the core agent configuration and lifecycle.
package agent

import (
	"time"
)

// Config represents the agent configuration.
type Config struct {
	Server  ServerConfig  `mapstructure:"server"`
	Watch   WatchConfig   `mapstructure:"watch"`
	Upload  UploadConfig  `mapstructure:"upload"`
	Storage StorageConfig `mapstructure:"storage"`
	Metrics MetricsConfig `mapstructure:"metrics"`
	Health  HealthConfig  `mapstructure:"health"`
	Log     LogConfig     `mapstructure:"log"`
}

// HealthConfig holds health check endpoint settings.
type HealthConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Port    int    `mapstructure:"port"`
	Path    string `mapstructure:"path"`
}

// ServerConfig holds flora-server connection settings.
type ServerConfig struct {
	URL         string `mapstructure:"url"`
	DeviceToken string `mapstructure:"device_token"`
	// Registration settings (used only if DeviceToken is empty)
	UserToken  string `mapstructure:"user_token"`
	DeviceName string `mapstructure:"device_name"`
	DeviceType string `mapstructure:"device_type"`
}

// WatchConfig holds file watching settings.
type WatchConfig struct {
	Paths        []string      `mapstructure:"paths"`
	Patterns     PatternConfig `mapstructure:"patterns"`
	ScanInterval time.Duration `mapstructure:"scan_interval"`
	MinFileAge   time.Duration `mapstructure:"min_file_age"`
}

// PatternConfig holds include/exclude patterns.
type PatternConfig struct {
	Include []string `mapstructure:"include"`
	Exclude []string `mapstructure:"exclude"`
}

// UploadConfig holds upload settings.
type UploadConfig struct {
	Enabled        bool          `mapstructure:"enabled"`
	Concurrent     int           `mapstructure:"concurrent"`
	ChunkSize      int64         `mapstructure:"chunk_size"`
	RetryAttempts  int           `mapstructure:"retry_attempts"`
	RetryDelay     time.Duration `mapstructure:"retry_delay"`
	BandwidthLimit int64         `mapstructure:"bandwidth_limit"`
}

// StorageConfig holds local storage settings.
type StorageConfig struct {
	DBPath string `mapstructure:"db_path"`
}

// MetricsConfig holds Prometheus metrics settings.
type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Port    int    `mapstructure:"port"`
	Path    string `mapstructure:"path"`
}

// LogConfig holds logging settings.
type LogConfig struct {
	Level    string `mapstructure:"level"`
	Format   string `mapstructure:"format"`
	Output   string `mapstructure:"output"`
	FilePath string `mapstructure:"file_path"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			URL: "https://api.flora.fan",
		},
		Watch: WatchConfig{
			Paths: []string{},
			Patterns: PatternConfig{
				Include: []string{"*.mcap", "*.bag", "*.db3"},
				Exclude: []string{"*.active", "*.tmp", "*~"},
			},
			ScanInterval: 30 * time.Second,
			MinFileAge:   5 * time.Second,
		},
		Upload: UploadConfig{
			Enabled:       true,
			Concurrent:    2,
			ChunkSize:     10 * 1024 * 1024, // 10MB
			RetryAttempts: 3,
			RetryDelay:    5 * time.Second,
		},
		Storage: StorageConfig{
			DBPath: "/var/lib/flora-agent/agent.db",
		},
		Metrics: MetricsConfig{
			Enabled: false,
			Port:    9090,
			Path:    "/metrics",
		},
		Health: HealthConfig{
			Enabled: true,
			Port:    8080,
			Path:    "/health",
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
			Output: "stdout",
		},
	}
}
