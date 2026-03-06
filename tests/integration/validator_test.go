//go:build integration

// Package integration contains integration tests for flora-agent.
// Run with: go test -tags=integration ./tests/integration/...
package integration

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/foxglove/mcap/go/mcap"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flora-suite/flora-agent/internal/validator"
)

// TestValidateMCAP_RealFile tests MCAP validation with a properly generated MCAP file.
func TestValidateMCAP_RealFile(t *testing.T) {
	// Create a real MCAP file using the mcap library
	tmpDir := t.TempDir()
	mcapPath := filepath.Join(tmpDir, "test.mcap")

	err := createTestMCAPFile(mcapPath)
	require.NoError(t, err)

	// Validate the file
	log := zerolog.Nop()
	v := validator.New(log)

	result, err := v.Validate(mcapPath)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.Valid, "MCAP file should be valid")
	assert.Equal(t, "mcap", result.FileType)
	assert.NotEmpty(t, result.Checksum)
	assert.NotNil(t, result.Metadata)

	// Check extracted metadata
	if result.Metadata != nil {
		// Our test file has 2 channels
		assert.GreaterOrEqual(t, len(result.Metadata.Topics), 1)
	}
}

// TestValidateMCAP_WithTopics tests that topic extraction works correctly.
func TestValidateMCAP_WithTopics(t *testing.T) {
	tmpDir := t.TempDir()
	mcapPath := filepath.Join(tmpDir, "topics.mcap")

	err := createTestMCAPFileWithTopics(mcapPath, []string{
		"/camera/image",
		"/imu/data",
		"/lidar/points",
	})
	require.NoError(t, err)

	log := zerolog.Nop()
	v := validator.New(log)

	result, err := v.Validate(mcapPath)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Valid)

	require.NotNil(t, result.Metadata)
	assert.Len(t, result.Metadata.Topics, 3)

	topicNames := make(map[string]bool)
	for _, topic := range result.Metadata.Topics {
		topicNames[topic.Name] = true
	}

	assert.True(t, topicNames["/camera/image"])
	assert.True(t, topicNames["/imu/data"])
	assert.True(t, topicNames["/lidar/points"])
}

// TestValidateMCAP_LargeFile tests validation of a larger MCAP file.
func TestValidateMCAP_LargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large file test in short mode")
	}

	tmpDir := t.TempDir()
	mcapPath := filepath.Join(tmpDir, "large.mcap")

	// Create a file with many messages
	err := createLargeMCAPFile(mcapPath, 10000)
	require.NoError(t, err)

	log := zerolog.Nop()
	v := validator.New(log)

	start := time.Now()
	result, err := v.Validate(mcapPath)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Valid)

	t.Logf("Validated large MCAP file in %v", elapsed)

	// File info
	info, _ := os.Stat(mcapPath)
	t.Logf("File size: %d bytes", info.Size())
}

// TestValidateMCAP_Corrupted tests handling of corrupted MCAP files.
func TestValidateMCAP_Corrupted(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a valid MCAP file first
	validPath := filepath.Join(tmpDir, "valid.mcap")
	err := createTestMCAPFile(validPath)
	require.NoError(t, err)

	// Read the file and corrupt it severely
	data, err := os.ReadFile(validPath)
	require.NoError(t, err)

	// Corrupt the header magic bytes
	corruptedPath := filepath.Join(tmpDir, "corrupted.mcap")
	corrupted := make([]byte, len(data))
	copy(corrupted, data)
	// Corrupt the magic bytes at the start
	if len(corrupted) > 8 {
		for i := 0; i < 8; i++ {
			corrupted[i] = 0xFF
		}
	}
	err = os.WriteFile(corruptedPath, corrupted, 0644)
	require.NoError(t, err)

	log := zerolog.Nop()
	v := validator.New(log)

	// The validation should return invalid since magic bytes are corrupted
	result, err := v.Validate(corruptedPath)
	require.NoError(t, err) // No error, just invalid result
	require.NotNil(t, result)
	assert.False(t, result.Valid, "File with corrupted magic bytes should not be valid")
}

// Helper functions to create test MCAP files

func createTestMCAPFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	writer, err := mcap.NewWriter(f, &mcap.WriterOptions{
		Chunked: true,
	})
	if err != nil {
		return err
	}

	// Write header
	if err := writer.WriteHeader(&mcap.Header{
		Profile: "ros2",
		Library: "flora-agent-test",
	}); err != nil {
		return err
	}

	// Create a schema
	schemaID := uint16(1)
	if err := writer.WriteSchema(&mcap.Schema{
		ID:       schemaID,
		Name:     "std_msgs/String",
		Encoding: "ros2msg",
		Data:     []byte("string data"),
	}); err != nil {
		return err
	}

	// Create a channel
	channelID := uint16(1)
	if err := writer.WriteChannel(&mcap.Channel{
		ID:              channelID,
		SchemaID:        schemaID,
		Topic:           "/test/topic",
		MessageEncoding: "cdr",
	}); err != nil {
		return err
	}

	// Write some messages
	for i := 0; i < 10; i++ {
		msg := &mcap.Message{
			ChannelID:   channelID,
			Sequence:    uint32(i),
			LogTime:     uint64(i * 1000000000), // 1 second apart
			PublishTime: uint64(i * 1000000000),
			Data:        []byte("Hello, World!"),
		}
		if err := writer.WriteMessage(msg); err != nil {
			return err
		}
	}

	return writer.Close()
}

func createTestMCAPFileWithTopics(path string, topics []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	writer, err := mcap.NewWriter(f, &mcap.WriterOptions{
		Chunked: true,
	})
	if err != nil {
		return err
	}

	if err := writer.WriteHeader(&mcap.Header{
		Profile: "ros2",
		Library: "flora-agent-test",
	}); err != nil {
		return err
	}

	// Create schema
	schemaID := uint16(1)
	if err := writer.WriteSchema(&mcap.Schema{
		ID:       schemaID,
		Name:     "std_msgs/String",
		Encoding: "ros2msg",
		Data:     []byte("string data"),
	}); err != nil {
		return err
	}

	// Create channels for each topic
	for i, topic := range topics {
		channelID := uint16(i + 1)
		if err := writer.WriteChannel(&mcap.Channel{
			ID:              channelID,
			SchemaID:        schemaID,
			Topic:           topic,
			MessageEncoding: "cdr",
		}); err != nil {
			return err
		}

		// Write a few messages per channel
		for j := 0; j < 5; j++ {
			msg := &mcap.Message{
				ChannelID:   channelID,
				Sequence:    uint32(j),
				LogTime:     uint64((i*5 + j) * 1000000000),
				PublishTime: uint64((i*5 + j) * 1000000000),
				Data:        []byte("test message"),
			}
			if err := writer.WriteMessage(msg); err != nil {
				return err
			}
		}
	}

	return writer.Close()
}

func createLargeMCAPFile(path string, messageCount int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	writer, err := mcap.NewWriter(f, &mcap.WriterOptions{
		Chunked:   true,
		ChunkSize: 1024 * 1024, // 1MB chunks
	})
	if err != nil {
		return err
	}

	if err := writer.WriteHeader(&mcap.Header{
		Profile: "ros2",
		Library: "flora-agent-test",
	}); err != nil {
		return err
	}

	schemaID := uint16(1)
	if err := writer.WriteSchema(&mcap.Schema{
		ID:       schemaID,
		Name:     "sensor_msgs/Image",
		Encoding: "ros2msg",
		Data:     []byte("uint8[] data"),
	}); err != nil {
		return err
	}

	channelID := uint16(1)
	if err := writer.WriteChannel(&mcap.Channel{
		ID:              channelID,
		SchemaID:        schemaID,
		Topic:           "/camera/image",
		MessageEncoding: "cdr",
	}); err != nil {
		return err
	}

	// Create a 1KB message payload
	payload := bytes.Repeat([]byte{0xAB}, 1024)

	for i := 0; i < messageCount; i++ {
		msg := &mcap.Message{
			ChannelID:   channelID,
			Sequence:    uint32(i),
			LogTime:     uint64(i * 100000000), // 100ms apart
			PublishTime: uint64(i * 100000000),
			Data:        payload,
		}
		if err := writer.WriteMessage(msg); err != nil {
			return err
		}
	}

	return writer.Close()
}
