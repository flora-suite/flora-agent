package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/flora-suite/flora-agent/internal/agent"
	"github.com/flora-suite/flora-agent/internal/register"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistrationServerURLUsesDefaultAndWarns(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("server", "", "")
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	assert.Equal(t, register.DefaultServerURL, registrationServerURL(cmd))
	assert.Contains(t, stderr.String(), "WARNING")
	assert.Contains(t, stderr.String(), "--server")
	assert.Contains(t, stderr.String(), register.DefaultServerURL)
}

func TestRegistrationServerURLUsesExplicitValueWithoutWarning(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("server", " https://agent.example.com ", "")
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	assert.Equal(t, "https://agent.example.com", registrationServerURL(cmd))
	assert.Empty(t, stderr.String())
}

func TestSetupLoggerWritesToConfiguredFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "agent.log")
	log, closer, err := setupLogger(&agent.Config{Log: agent.LogConfig{Output: "file", FilePath: path, Format: "json"}})
	require.NoError(t, err)
	require.NotNil(t, closer)
	log.Info().Msg("file logger test")
	require.NoError(t, closer.Close())

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(content), "file logger test")
}

func TestSetupLoggerRejectsFileOutputWithoutPath(t *testing.T) {
	_, _, err := setupLogger(&agent.Config{Log: agent.LogConfig{Output: "file"}})
	assert.Error(t, err)
}
