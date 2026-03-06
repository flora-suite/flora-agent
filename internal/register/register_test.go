// Package register provides tests for the device registration functionality.
package register

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectDeviceInfo(t *testing.T) {
	info, err := CollectDeviceInfo()
	require.NoError(t, err)
	require.NotNil(t, info)

	// Hostname should be non-empty
	assert.NotEmpty(t, info.Hostname)

	// Platform should contain OS and arch
	assert.Contains(t, info.Platform, runtime.GOOS)
	assert.Contains(t, info.Platform, runtime.GOARCH)

	// Machine ID should be non-empty
	assert.NotEmpty(t, info.MachineID)

	// CPU cores should be at least 1
	assert.Greater(t, info.CPUCores, 0)

	// Memory should be positive
	assert.Greater(t, info.MemoryGB, float64(0))

	// Disk should be positive
	assert.Greater(t, info.DiskGB, float64(0))

	// OS name should be non-empty
	assert.NotEmpty(t, info.OSName)
}

func TestGetMachineID(t *testing.T) {
	// This function should either return a valid machine ID or an error
	id, err := getMachineID()

	// On most systems, we should get a valid ID
	if err == nil {
		assert.NotEmpty(t, id)
	}
}

func TestGetLocalIP(t *testing.T) {
	ip := getLocalIP()
	// IP might be empty on some systems (e.g., no network)
	// but if present, it should be valid
	if ip != "" {
		// Check it's a valid IPv4-like format (basic check)
		assert.Regexp(t, `^\d+\.\d+\.\d+\.\d+$`, ip)
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "shorter than max",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "equal to max",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "longer than max",
			input:    "hello world",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "empty string",
			input:    "",
			maxLen:   5,
			expected: "",
		},
		{
			name:     "zero max",
			input:    "hello",
			maxLen:   0,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateString(tt.input, tt.maxLen)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()
	require.NotNil(t, opts)

	assert.Equal(t, "https://api.flora.fan", opts.ServerURL)
	assert.NotEmpty(t, opts.OutputPath)
	assert.False(t, opts.NoService)
	assert.False(t, opts.InstallService)
	assert.Empty(t, opts.ServiceType)
}

func TestDefaultConfigPath(t *testing.T) {
	path := DefaultConfigPath()
	assert.NotEmpty(t, path)

	// Path should end with agent.yaml
	assert.True(t, filepath.Ext(path) == ".yaml")
	assert.Contains(t, filepath.Base(path), "agent")
}

func TestDefaultDBPath(t *testing.T) {
	path := DefaultDBPath()
	assert.NotEmpty(t, path)

	// Path should end with agent.db
	assert.True(t, filepath.Ext(path) == ".db")
	assert.Contains(t, filepath.Base(path), "agent")
}

func TestWriteConfigFile(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config", "agent.yaml")

	config := &AgentConfig{
		ServerURL:   "http://localhost:3000",
		DeviceToken: "test-device-token-12345",
		WatchPaths:  []string{"/data/recordings", "/mnt/external"},
		DBPath:      "/var/lib/flora-agent/agent.db",
	}

	err := WriteConfigFile(configPath, config)
	require.NoError(t, err)

	// Verify file was created
	_, err = os.Stat(configPath)
	require.NoError(t, err)

	// Read and verify content
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)

	contentStr := string(content)
	assert.Contains(t, contentStr, "http://localhost:3000")
	assert.Contains(t, contentStr, "test-device-token-12345")
	assert.Contains(t, contentStr, "/data/recordings")
	assert.Contains(t, contentStr, "/mnt/external")
	assert.Contains(t, contentStr, "/var/lib/flora-agent/agent.db")

	// Verify file permissions
	info, err := os.Stat(configPath)
	require.NoError(t, err)
	// File should have restricted permissions (0600)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestWriteConfigFile_InvalidPath(t *testing.T) {
	// Try to write to a path that can't be created
	configPath := "/nonexistent/deeply/nested/path/agent.yaml"

	config := &AgentConfig{
		ServerURL:   "http://localhost:3000",
		DeviceToken: "test-token",
		WatchPaths:  []string{"/data"},
		DBPath:      "/var/lib/flora-agent/agent.db",
	}

	err := WriteConfigFile(configPath, config)
	assert.Error(t, err)
}

func TestWriteConfigFile_EmptyWatchPaths(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "agent.yaml")

	config := &AgentConfig{
		ServerURL:   "http://localhost:3000",
		DeviceToken: "test-token",
		WatchPaths:  []string{},
		DBPath:      "/var/lib/flora-agent/agent.db",
	}

	err := WriteConfigFile(configPath, config)
	require.NoError(t, err)

	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "paths:")
}

func TestAgentConfigStruct(t *testing.T) {
	config := &AgentConfig{
		ServerURL:   "https://api.flora.fan",
		DeviceToken: "device-token-123",
		WatchPaths:  []string{"/path1", "/path2"},
		DBPath:      "/data/agent.db",
	}

	assert.Equal(t, "https://api.flora.fan", config.ServerURL)
	assert.Equal(t, "device-token-123", config.DeviceToken)
	assert.Equal(t, []string{"/path1", "/path2"}, config.WatchPaths)
	assert.Equal(t, "/data/agent.db", config.DBPath)
}

func TestDeviceInfoStruct(t *testing.T) {
	info := &DeviceInfo{
		MachineID:     "machine-id-123",
		Hostname:      "test-host",
		Platform:      "linux/amd64",
		IPAddress:     "192.168.1.100",
		CPUCores:      8,
		CPUModel:      "Intel Core i7",
		MemoryGB:      16.0,
		DiskGB:        500.0,
		OSName:        "Ubuntu 22.04",
		KernelVersion: "5.15.0",
	}

	assert.Equal(t, "machine-id-123", info.MachineID)
	assert.Equal(t, "test-host", info.Hostname)
	assert.Equal(t, "linux/amd64", info.Platform)
	assert.Equal(t, "192.168.1.100", info.IPAddress)
	assert.Equal(t, 8, info.CPUCores)
	assert.Equal(t, "Intel Core i7", info.CPUModel)
	assert.Equal(t, 16.0, info.MemoryGB)
	assert.Equal(t, 500.0, info.DiskGB)
	assert.Equal(t, "Ubuntu 22.04", info.OSName)
	assert.Equal(t, "5.15.0", info.KernelVersion)
}

func TestOptionsStruct(t *testing.T) {
	opts := &Options{
		ServerURL:      "http://localhost:3000",
		OutputPath:     "/etc/flora-agent/agent.yaml",
		NoService:      true,
		InstallService: false,
		ServiceType:    "systemd",
	}

	assert.Equal(t, "http://localhost:3000", opts.ServerURL)
	assert.Equal(t, "/etc/flora-agent/agent.yaml", opts.OutputPath)
	assert.True(t, opts.NoService)
	assert.False(t, opts.InstallService)
	assert.Equal(t, "systemd", opts.ServiceType)
}
