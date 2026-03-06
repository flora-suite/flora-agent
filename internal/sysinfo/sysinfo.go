// Package sysinfo provides system information collection.
package sysinfo

import (
	"os"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

// Stats contains system statistics.
type Stats struct {
	// CPU usage percentage (0-100)
	CPUUsage float64

	// Memory usage percentage (0-100)
	MemoryUsage float64

	// Disk usage percentage for the root filesystem (0-100)
	DiskUsage float64

	// System uptime in seconds
	Uptime int64

	// Hostname
	Hostname string

	// OS information
	OS string

	// Architecture
	Arch string

	// Number of CPUs
	NumCPU int

	// Total memory in bytes
	TotalMemory uint64

	// Available memory in bytes
	AvailableMemory uint64

	// Go runtime stats
	GoRoutines int
	GoVersion  string
}

// Collector collects system statistics.
type Collector struct {
	startTime time.Time
}

// NewCollector creates a new stats collector.
func NewCollector() *Collector {
	return &Collector{
		startTime: time.Now(),
	}
}

// Collect gathers current system statistics.
func (c *Collector) Collect() (*Stats, error) {
	stats := &Stats{
		Hostname:   getHostname(),
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		NumCPU:     runtime.NumCPU(),
		GoRoutines: runtime.NumGoroutine(),
		GoVersion:  runtime.Version(),
	}

	// CPU usage
	cpuPercent, err := cpu.Percent(100*time.Millisecond, false)
	if err == nil && len(cpuPercent) > 0 {
		stats.CPUUsage = cpuPercent[0]
	}

	// Memory usage
	memStats, err := mem.VirtualMemory()
	if err == nil {
		stats.MemoryUsage = memStats.UsedPercent
		stats.TotalMemory = memStats.Total
		stats.AvailableMemory = memStats.Available
	}

	// Disk usage for root partition
	diskStats, err := disk.Usage("/")
	if err == nil {
		stats.DiskUsage = diskStats.UsedPercent
	}

	// System uptime
	hostInfo, err := host.Info()
	if err == nil {
		stats.Uptime = int64(hostInfo.Uptime)
	}

	return stats, nil
}

// CollectFast gathers stats without blocking CPU measurement.
func (c *Collector) CollectFast() *Stats {
	stats := &Stats{
		Hostname:   getHostname(),
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		NumCPU:     runtime.NumCPU(),
		GoRoutines: runtime.NumGoroutine(),
		GoVersion:  runtime.Version(),
	}

	// Memory usage (fast)
	memStats, err := mem.VirtualMemory()
	if err == nil {
		stats.MemoryUsage = memStats.UsedPercent
		stats.TotalMemory = memStats.Total
		stats.AvailableMemory = memStats.Available
	}

	// Disk usage for root partition (fast)
	diskStats, err := disk.Usage("/")
	if err == nil {
		stats.DiskUsage = diskStats.UsedPercent
	}

	// System uptime (fast)
	hostInfo, err := host.Info()
	if err == nil {
		stats.Uptime = int64(hostInfo.Uptime)
	}

	return stats
}

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// WatchPaths contains information about watched paths.
type WatchPaths struct {
	Paths []PathInfo
}

// PathInfo contains information about a watched path.
type PathInfo struct {
	Path      string
	Available uint64
	Total     uint64
	Used      uint64
	UsedPct   float64
}

// GetWatchPathsInfo returns disk info for the given paths.
func GetWatchPathsInfo(paths []string) *WatchPaths {
	result := &WatchPaths{
		Paths: make([]PathInfo, 0, len(paths)),
	}

	for _, path := range paths {
		info := PathInfo{Path: path}
		usage, err := disk.Usage(path)
		if err == nil {
			info.Available = usage.Free
			info.Total = usage.Total
			info.Used = usage.Used
			info.UsedPct = usage.UsedPercent
		}
		result.Paths = append(result.Paths, info)
	}

	return result
}
