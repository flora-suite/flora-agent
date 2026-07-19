package register

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/flora-suite/flora-agent/internal/api"
)

// DefaultServerURL is the public Flora API used when a registration server is not specified.
const DefaultServerURL = "https://api.flora.fan"

// Options contains configuration for the registration process.
type Options struct {
	ServerURL      string
	OutputPath     string
	NoService      bool
	InstallService bool
	ServiceType    string
}

// DefaultOptions returns default registration options.
func DefaultOptions() *Options {
	return &Options{
		ServerURL:      DefaultServerURL,
		OutputPath:     DefaultConfigPath(),
		NoService:      false,
		InstallService: false,
		ServiceType:    "",
	}
}

// Run executes the device registration flow.
func Run(opts *Options) error {
	fmt.Println()
	fmt.Println("🌸 Flora Agent Device Registration")
	fmt.Println("==================================")
	fmt.Println()

	// Collect device information
	fmt.Println("Collecting device information...")
	deviceInfo, err := CollectDeviceInfo()
	if err != nil {
		return fmt.Errorf("failed to collect device info: %w", err)
	}

	PrintDeviceInfo(deviceInfo)

	// Initialize API client
	client := api.NewClient(opts.ServerURL, "")

	// Initialize registration
	ctx := context.Background()
	initReq := &api.RegisterInitRequest{
		MachineID: deviceInfo.MachineID,
		Hostname:  deviceInfo.Hostname,
		Platform:  deviceInfo.Platform,
		IPAddress: deviceInfo.IPAddress,
		SystemInfo: &api.SystemInfo{
			CPUCores:      deviceInfo.CPUCores,
			CPUModel:      deviceInfo.CPUModel,
			MemoryGB:      deviceInfo.MemoryGB,
			DiskGB:        deviceInfo.DiskGB,
			OSName:        deviceInfo.OSName,
			KernelVersion: deviceInfo.KernelVersion,
		},
	}

	initResp, err := client.RegisterInit(ctx, initReq)
	if err != nil {
		return fmt.Errorf("failed to initialize registration: %w", err)
	}

	// Display registration URL
	fmt.Println("Please open the following URL in your browser to complete registration:")
	fmt.Println()
	fmt.Printf("    %s\n", initResp.RegistrationURL)
	fmt.Println()
	fmt.Printf("Registration code: %s (expires in %d seconds)\n", initResp.Code, initResp.ExpiresIn)
	fmt.Println()
	fmt.Println("Waiting for registration to complete... (Press Ctrl+C to cancel)")
	fmt.Println()

	// Poll for completion
	pollResp, err := pollForCompletion(ctx, client, initResp.Code, initResp.ExpiresIn)
	if err != nil {
		return err
	}

	fmt.Println("✓ Registration completed!")
	fmt.Println()

	// Display device info
	deviceName := pollResp.Device.Name
	orgName := "Personal"
	if pollResp.Device.OrganizationName != nil && *pollResp.Device.OrganizationName != "" {
		orgName = *pollResp.Device.OrganizationName
	}

	fmt.Printf("Device registered as: %s (ID: %s)\n", deviceName, pollResp.Device.ID)
	fmt.Printf("Organization: %s\n", orgName)
	fmt.Println()

	// Write configuration file
	watchPaths := pollResp.Config.WatchPaths
	if len(watchPaths) == 0 {
		// Default watch path
		watchPaths = []string{"/data/recordings"}
	}

	config := &AgentConfig{
		ServerURL:   opts.ServerURL,
		DeviceToken: pollResp.DeviceToken,
		WatchPaths:  watchPaths,
		DBPath:      DefaultDBPath(),
	}

	if err := WriteConfigFile(opts.OutputPath, config); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("Configuration saved to: %s\n", opts.OutputPath)
	fmt.Println()

	// Handle service installation
	if err := handleServiceInstallation(opts); err != nil {
		return err
	}

	return nil
}

func pollForCompletion(ctx context.Context, client *api.Client, code string, expiresIn int) (*api.RegisterPollResponse, error) {
	pollInterval := 2 * time.Second
	timeout := time.Duration(expiresIn) * time.Second
	deadline := time.Now().Add(timeout)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("registration expired")
			}

			resp, err := client.RegisterPoll(ctx, code)
			if err != nil {
				// Log error but continue polling
				fmt.Printf("\r⏳ Waiting... (poll error: %v)", err)
				continue
			}

			switch resp.Status {
			case "completed":
				fmt.Print("\r")
				return resp, nil
			case "expired":
				return nil, fmt.Errorf("registration expired")
			case "pending":
				remaining := int(time.Until(deadline).Seconds())
				fmt.Printf("\r⏳ Waiting for confirmation... (%ds remaining)", remaining)
			}
		}
	}
}

func handleServiceInstallation(opts *Options) error {
	// Skip if explicitly disabled
	if opts.NoService {
		fmt.Println("Skipping service installation (--no-service specified)")
		return nil
	}

	installer := NewServiceInstaller(opts.OutputPath)

	// Check if we can install a service
	serviceType := installer.serviceType
	if serviceType == ServiceTypeNone {
		fmt.Println("No supported init system found. Please start the agent manually:")
		fmt.Printf("    flora-agent run --config %s\n", opts.OutputPath)
		return nil
	}

	// Auto-install if flag set
	shouldInstall := opts.InstallService

	// Otherwise prompt user
	if !shouldInstall {
		fmt.Print("Install as system service? [Y/n]: ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		shouldInstall = input == "" || input == "y" || input == "yes"
	}

	if !shouldInstall {
		fmt.Println()
		fmt.Println("Skipping service installation. Start the agent manually with:")
		fmt.Printf("    flora-agent run --config %s\n", opts.OutputPath)
		return nil
	}

	// Install service
	fmt.Println()
	fmt.Printf("Installing %s service...\n", serviceType)

	if err := installer.Install(); err != nil {
		return fmt.Errorf("failed to install service: %w", err)
	}
	fmt.Printf("✓ Service file created: %s\n", installer.ServiceFilePath())

	if err := installer.Enable(); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}
	fmt.Println("✓ Service enabled")

	if err := installer.Start(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}
	fmt.Println("✓ Service started")

	fmt.Println()
	fmt.Println("Flora Agent is now running!")
	fmt.Println()
	fmt.Println("Check status with:")
	fmt.Printf("    %s\n", installer.StatusCommand())
	fmt.Println()
	fmt.Println("View logs with:")
	fmt.Printf("    %s\n", installer.LogsCommand())

	return nil
}
