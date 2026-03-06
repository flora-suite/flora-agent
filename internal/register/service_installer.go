package register

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"text/template"
)

// ServiceType represents the type of init system.
type ServiceType string

const (
	ServiceTypeSystemd ServiceType = "systemd"
	ServiceTypeLaunchd ServiceType = "launchd"
	ServiceTypeNone    ServiceType = "none"
)

// DetectServiceType detects the available init system.
func DetectServiceType() ServiceType {
	switch runtime.GOOS {
	case "linux":
		// Check for systemd
		if _, err := exec.LookPath("systemctl"); err == nil {
			return ServiceTypeSystemd
		}
	case "darwin":
		// macOS uses launchd
		return ServiceTypeLaunchd
	}
	return ServiceTypeNone
}

// ServiceInstaller installs and manages system services.
type ServiceInstaller struct {
	configPath  string
	binaryPath  string
	serviceType ServiceType
}

// NewServiceInstaller creates a new service installer.
func NewServiceInstaller(configPath string) *ServiceInstaller {
	// Get the path to the current binary
	binaryPath, _ := os.Executable()
	if binaryPath == "" {
		binaryPath = "/usr/local/bin/flora-agent"
	}

	return &ServiceInstaller{
		configPath:  configPath,
		binaryPath:  binaryPath,
		serviceType: DetectServiceType(),
	}
}

// Install installs the system service.
func (s *ServiceInstaller) Install() error {
	switch s.serviceType {
	case ServiceTypeSystemd:
		return s.installSystemd()
	case ServiceTypeLaunchd:
		return s.installLaunchd()
	default:
		return fmt.Errorf("no supported init system found")
	}
}

// Enable enables the service to start on boot.
func (s *ServiceInstaller) Enable() error {
	switch s.serviceType {
	case ServiceTypeSystemd:
		return s.runCommand("systemctl", "enable", "flora-agent")
	case ServiceTypeLaunchd:
		// launchd services are enabled by default when loaded
		return nil
	default:
		return fmt.Errorf("no supported init system found")
	}
}

// Start starts the service.
func (s *ServiceInstaller) Start() error {
	switch s.serviceType {
	case ServiceTypeSystemd:
		return s.runCommand("systemctl", "start", "flora-agent")
	case ServiceTypeLaunchd:
		plistPath := s.launchdPlistPath()
		return s.runCommand("launchctl", "load", "-w", plistPath)
	default:
		return fmt.Errorf("no supported init system found")
	}
}

// IsRunning checks if the service is running.
func (s *ServiceInstaller) IsRunning() bool {
	switch s.serviceType {
	case ServiceTypeSystemd:
		err := s.runCommand("systemctl", "is-active", "--quiet", "flora-agent")
		return err == nil
	case ServiceTypeLaunchd:
		err := s.runCommand("launchctl", "list", "fan.flora.agent")
		return err == nil
	default:
		return false
	}
}

// StatusCommand returns the command to check service status.
func (s *ServiceInstaller) StatusCommand() string {
	switch s.serviceType {
	case ServiceTypeSystemd:
		return "systemctl status flora-agent"
	case ServiceTypeLaunchd:
		return "launchctl list | grep flora"
	default:
		return ""
	}
}

// LogsCommand returns the command to view service logs.
func (s *ServiceInstaller) LogsCommand() string {
	switch s.serviceType {
	case ServiceTypeSystemd:
		return "journalctl -u flora-agent -f"
	case ServiceTypeLaunchd:
		return "tail -f /var/log/flora-agent/agent.log"
	default:
		return ""
	}
}

// ServiceFilePath returns the path where the service file was installed.
func (s *ServiceInstaller) ServiceFilePath() string {
	switch s.serviceType {
	case ServiceTypeSystemd:
		return "/etc/systemd/system/flora-agent.service"
	case ServiceTypeLaunchd:
		return s.launchdPlistPath()
	default:
		return ""
	}
}

func (s *ServiceInstaller) runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (s *ServiceInstaller) launchdPlistPath() string {
	// User-level service
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "flora.agent.plist")
}

// ============= Systemd =============

const systemdTemplate = `[Unit]
Description=Flora Agent - Recording File Uploader
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart={{.BinaryPath}} run --config {{.ConfigPath}}
Restart=always
RestartSec=10
{{- if .RunAsUser}}
User={{.User}}
Group={{.Group}}
{{- end}}

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=/var/lib/flora-agent

[Install]
WantedBy=multi-user.target
`

type systemdConfig struct {
	BinaryPath string
	ConfigPath string
	RunAsUser  bool
	User       string
	Group      string
}

func (s *ServiceInstaller) installSystemd() error {
	// Create service file
	servicePath := "/etc/systemd/system/flora-agent.service"

	tmpl, err := template.New("systemd").Parse(systemdTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse systemd template: %w", err)
	}

	file, err := os.Create(servicePath)
	if err != nil {
		return fmt.Errorf("failed to create service file: %w", err)
	}
	defer file.Close()

	config := systemdConfig{
		BinaryPath: s.binaryPath,
		ConfigPath: s.configPath,
		RunAsUser:  false, // Run as root for now (simpler)
	}

	if err := tmpl.Execute(file, config); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}

	// Create data directory
	if err := os.MkdirAll("/var/lib/flora-agent", 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Reload systemd
	if err := s.runCommand("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	return nil
}

// ============= Launchd =============

const launchdTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>flora.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        <string>run</string>
        <string>--config</string>
        <string>{{.ConfigPath}}</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>{{.LogPath}}/agent.log</string>
    <key>StandardErrorPath</key>
    <string>{{.LogPath}}/agent.err</string>
</dict>
</plist>
`

type launchdConfig struct {
	BinaryPath string
	ConfigPath string
	LogPath    string
}

func (s *ServiceInstaller) installLaunchd() error {
	plistPath := s.launchdPlistPath()

	// Ensure directory exists
	dir := filepath.Dir(plistPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create LaunchAgents directory: %w", err)
	}

	// Determine log path
	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, "Library", "Logs", "flora-agent")
	if err := os.MkdirAll(logPath, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	tmpl, err := template.New("launchd").Parse(launchdTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse launchd template: %w", err)
	}

	file, err := os.Create(plistPath)
	if err != nil {
		return fmt.Errorf("failed to create plist file: %w", err)
	}
	defer file.Close()

	config := launchdConfig{
		BinaryPath: s.binaryPath,
		ConfigPath: s.configPath,
		LogPath:    logPath,
	}

	if err := tmpl.Execute(file, config); err != nil {
		return fmt.Errorf("failed to write plist file: %w", err)
	}

	return nil
}
