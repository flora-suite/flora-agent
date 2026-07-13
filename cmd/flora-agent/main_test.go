package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/flora-suite/flora-agent/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
