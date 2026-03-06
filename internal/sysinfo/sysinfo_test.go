package sysinfo

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCollector(t *testing.T) {
	c := NewCollector()
	require.NotNil(t, c)
}

func TestCollect(t *testing.T) {
	c := NewCollector()

	stats, err := c.Collect()
	require.NoError(t, err)
	require.NotNil(t, stats)

	// Check basic fields are populated
	assert.NotEmpty(t, stats.Hostname)
	assert.Equal(t, runtime.GOOS, stats.OS)
	assert.Equal(t, runtime.GOARCH, stats.Arch)
	assert.Equal(t, runtime.NumCPU(), stats.NumCPU)
	assert.Greater(t, stats.GoRoutines, 0)
	assert.Equal(t, runtime.Version(), stats.GoVersion)

	// Memory should be set
	assert.Greater(t, stats.TotalMemory, uint64(0))
	assert.GreaterOrEqual(t, stats.MemoryUsage, float64(0))
	assert.LessOrEqual(t, stats.MemoryUsage, float64(100))

	// Disk usage should be valid
	assert.GreaterOrEqual(t, stats.DiskUsage, float64(0))
	assert.LessOrEqual(t, stats.DiskUsage, float64(100))

	// Uptime should be positive
	assert.GreaterOrEqual(t, stats.Uptime, int64(0))

	t.Logf("Stats: CPU=%.1f%%, Mem=%.1f%%, Disk=%.1f%%, Uptime=%ds",
		stats.CPUUsage, stats.MemoryUsage, stats.DiskUsage, stats.Uptime)
}

func TestCollectFast(t *testing.T) {
	c := NewCollector()

	stats := c.CollectFast()
	require.NotNil(t, stats)

	// Basic fields should be set
	assert.NotEmpty(t, stats.Hostname)
	assert.Equal(t, runtime.GOOS, stats.OS)

	// CPU usage won't be measured in fast mode
	assert.Equal(t, float64(0), stats.CPUUsage)

	// Memory should still be set
	assert.Greater(t, stats.TotalMemory, uint64(0))
}

func TestGetWatchPathsInfo(t *testing.T) {
	paths := []string{"/", "/tmp"}

	info := GetWatchPathsInfo(paths)
	require.NotNil(t, info)
	assert.Len(t, info.Paths, 2)

	// Root should have disk info
	rootInfo := info.Paths[0]
	assert.Equal(t, "/", rootInfo.Path)
	assert.Greater(t, rootInfo.Total, uint64(0))
	assert.GreaterOrEqual(t, rootInfo.UsedPct, float64(0))
	assert.LessOrEqual(t, rootInfo.UsedPct, float64(100))
}

func TestGetWatchPathsInfo_NonexistentPath(t *testing.T) {
	paths := []string{"/nonexistent/path/that/does/not/exist"}

	info := GetWatchPathsInfo(paths)
	require.NotNil(t, info)
	assert.Len(t, info.Paths, 1)

	// Path should be recorded but values may be zero
	assert.Equal(t, "/nonexistent/path/that/does/not/exist", info.Paths[0].Path)
}

func TestGetHostname(t *testing.T) {
	hostname := getHostname()
	assert.NotEmpty(t, hostname)
	assert.NotEqual(t, "unknown", hostname)
}

func BenchmarkCollect(b *testing.B) {
	c := NewCollector()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Collect()
	}
}

func BenchmarkCollectFast(b *testing.B) {
	c := NewCollector()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.CollectFast()
	}
}
