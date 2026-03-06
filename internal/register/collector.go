// Package register provides the device registration functionality.
package register

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

// DeviceInfo contains information about the device for registration.
type DeviceInfo struct {
	MachineID     string
	Hostname      string
	Platform      string
	IPAddress     string
	CPUCores      int
	CPUModel      string
	MemoryGB      float64
	DiskGB        float64
	OSName        string
	KernelVersion string
}

// CollectDeviceInfo gathers device information for registration.
func CollectDeviceInfo() (*DeviceInfo, error) {
	info := &DeviceInfo{}

	// Hostname
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	info.Hostname = hostname

	// Platform
	info.Platform = fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)

	// Machine ID
	machineID, err := getMachineID()
	if err != nil {
		// Fallback to hostname-based ID
		machineID = fmt.Sprintf("host-%s", hostname)
	}
	info.MachineID = machineID

	// IP Address
	info.IPAddress = getLocalIP()

	// CPU info
	info.CPUCores = runtime.NumCPU()
	cpuInfo, err := cpu.Info()
	if err == nil && len(cpuInfo) > 0 {
		info.CPUModel = cpuInfo[0].ModelName
	}

	// Memory
	memInfo, err := mem.VirtualMemory()
	if err == nil {
		info.MemoryGB = float64(memInfo.Total) / (1024 * 1024 * 1024)
	}

	// Disk
	diskInfo, err := disk.Usage("/")
	if err == nil {
		info.DiskGB = float64(diskInfo.Total) / (1024 * 1024 * 1024)
	}

	// OS info
	hostInfo, err := host.Info()
	if err == nil {
		info.OSName = fmt.Sprintf("%s %s", hostInfo.Platform, hostInfo.PlatformVersion)
		info.KernelVersion = hostInfo.KernelVersion
	} else {
		info.OSName = runtime.GOOS
	}

	return info, nil
}

// getMachineID returns a unique machine identifier.
func getMachineID() (string, error) {
	// Try to read machine-id on Linux
	if runtime.GOOS == "linux" {
		// Try /etc/machine-id first (systemd)
		data, err := os.ReadFile("/etc/machine-id")
		if err == nil {
			return strings.TrimSpace(string(data)), nil
		}

		// Try /var/lib/dbus/machine-id
		data, err = os.ReadFile("/var/lib/dbus/machine-id")
		if err == nil {
			return strings.TrimSpace(string(data)), nil
		}
	}

	// Try to read IOPlatformUUID on macOS
	if runtime.GOOS == "darwin" {
		hostInfo, err := host.Info()
		if err == nil && hostInfo.HostID != "" {
			return hostInfo.HostID, nil
		}
	}

	// Fallback to host info
	hostInfo, err := host.Info()
	if err == nil && hostInfo.HostID != "" {
		return hostInfo.HostID, nil
	}

	return "", fmt.Errorf("could not determine machine ID")
}

// getLocalIP returns the local IP address.
func getLocalIP() string {
	// Get all network interfaces
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String()
			}
		}
	}

	return ""
}

// PrintDeviceInfo prints device information in a formatted way.
func PrintDeviceInfo(info *DeviceInfo) {
	fmt.Println()
	fmt.Println("Device Information:")
	fmt.Printf("  Hostname:     %s\n", info.Hostname)
	fmt.Printf("  Platform:     %s\n", info.Platform)
	fmt.Printf("  Machine ID:   %s\n", truncateString(info.MachineID, 16)+"...")
	if info.IPAddress != "" {
		fmt.Printf("  IP Address:   %s\n", info.IPAddress)
	}
	if info.CPUCores > 0 {
		fmt.Printf("  CPU:          %d cores", info.CPUCores)
		if info.CPUModel != "" {
			fmt.Printf(" (%s)", truncateString(info.CPUModel, 30))
		}
		fmt.Println()
	}
	if info.MemoryGB > 0 {
		fmt.Printf("  Memory:       %.0f GB\n", info.MemoryGB)
	}
	if info.DiskGB > 0 {
		fmt.Printf("  Disk:         %.0f GB\n", info.DiskGB)
	}
	if info.OSName != "" {
		fmt.Printf("  OS:           %s\n", info.OSName)
	}
	fmt.Println()
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
