package version

import (
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInfo(t *testing.T) {
	info := Info()

	// Check it contains expected components
	assert.Contains(t, info, "flora-agent")
	assert.Contains(t, info, Version)
	assert.Contains(t, info, Commit)
	assert.Contains(t, info, BuildDate)
	assert.Contains(t, info, runtime.Version())
}

func TestInfo_DefaultValues(t *testing.T) {
	// By default, Version should be "dev"
	info := Info()
	assert.Contains(t, info, "dev")
	assert.Contains(t, info, "unknown") // Commit and BuildDate
}

func TestShort(t *testing.T) {
	short := Short()
	assert.Equal(t, Version, short)
}

func TestInfo_Format(t *testing.T) {
	info := Info()

	// Should have proper format
	parts := strings.Split(info, " ")
	assert.GreaterOrEqual(t, len(parts), 2)
	assert.Equal(t, "flora-agent", parts[0])
}

func TestVariablesModifiable(t *testing.T) {
	// Save original values
	origVersion := Version
	origCommit := Commit
	origBuildDate := BuildDate

	// Restore after test
	defer func() {
		Version = origVersion
		Commit = origCommit
		BuildDate = origBuildDate
	}()

	// Modify variables (as -ldflags would)
	Version = "1.2.3"
	Commit = "abc123"
	BuildDate = "2024-01-15T10:00:00Z"

	info := Info()
	assert.Contains(t, info, "1.2.3")
	assert.Contains(t, info, "abc123")
	assert.Contains(t, info, "2024-01-15T10:00:00Z")

	short := Short()
	assert.Equal(t, "1.2.3", short)
}
