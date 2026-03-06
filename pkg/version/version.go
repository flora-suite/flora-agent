// Package version provides build version information.
package version

import (
	"fmt"
	"runtime"
)

// Build-time variables (set via -ldflags)
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// Info returns formatted version information.
func Info() string {
	return fmt.Sprintf("flora-agent %s (commit: %s, built: %s, go: %s)",
		Version, Commit, BuildDate, runtime.Version())
}

// Short returns just the version string.
func Short() string {
	return Version
}
