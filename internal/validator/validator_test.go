package validator

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/foxglove/mcap/go/mcap"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestNew(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)
	require.NotNil(t, v)
}

func TestFileValidator_Validate_UnsupportedType(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	err := os.WriteFile(tmpFile, []byte("hello"), 0644)
	require.NoError(t, err)

	result, err := v.Validate(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported file type")
	assert.Nil(t, result)
}

func TestFileValidator_Validate_NonexistentFile(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	result, err := v.Validate("/nonexistent/file.mcap")
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestFileValidator_ValidateMCAP_InvalidMagic(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	// Create file with invalid magic bytes
	tmpFile := filepath.Join(t.TempDir(), "invalid.mcap")
	err := os.WriteFile(tmpFile, []byte("not a valid mcap file content here"), 0644)
	require.NoError(t, err)

	result, err := v.Validate(tmpFile)
	require.NoError(t, err) // No error, but result indicates invalid
	require.NotNil(t, result)
	assert.False(t, result.Valid)
	assert.Contains(t, result.Error.Error(), "invalid MCAP magic")
}

func TestFileValidator_ValidateMCAP_ValidFile(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	// Create a minimal valid MCAP file with proper structure
	// MCAP magic: 0x89, 'M', 'C', 'A', 'P', 0x30, '\r', '\n'
	magic := []byte{0x89, 'M', 'C', 'A', 'P', 0x30, '\r', '\n'}

	// We can't easily create a fully valid MCAP file in tests without
	// the mcap writer, so we test that validation properly detects
	// files with valid header magic but incomplete structure.
	// This is expected behavior - the MCAP library will fail to parse
	// an incomplete file, which our validator handles.

	// For a proper test, we'd need to use the mcap writer to create a file.
	// For now, skip this test or mark as integration test.
	t.Skip("Requires a properly structured MCAP file - use integration tests")

	content := make([]byte, 0)
	content = append(content, magic...)
	content = append(content, magic...) // Just magic at start and end

	tmpFile := filepath.Join(t.TempDir(), "valid.mcap")
	err := os.WriteFile(tmpFile, content, 0644)
	require.NoError(t, err)

	result, err := v.Validate(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Valid)
	assert.Equal(t, "mcap", result.FileType)
	assert.NotEmpty(t, result.Checksum)
}

func TestFileValidator_ValidateMCAP_TruncatedFile(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	// Create file with valid header magic but no footer
	magic := []byte{0x89, 'M', 'C', 'A', 'P', 0x30, '\r', '\n'}
	content := append(magic, []byte("some data without proper footer")...)

	tmpFile := filepath.Join(t.TempDir(), "truncated.mcap")
	err := os.WriteFile(tmpFile, content, 0644)
	require.NoError(t, err)

	result, err := v.Validate(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Valid)
	assert.Contains(t, result.Error.Error(), "incomplete")
}

func TestFileValidator_ValidateBag_ValidFile(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	// ROS bag magic: "#ROSBAG V2.0\n"
	content := []byte("#ROSBAG V2.0\nsome bag data here")

	tmpFile := filepath.Join(t.TempDir(), "valid.bag")
	err := os.WriteFile(tmpFile, content, 0644)
	require.NoError(t, err)

	result, err := v.Validate(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Valid)
	assert.Equal(t, "bag", result.FileType)
	assert.NotEmpty(t, result.Checksum)
}

func TestFileValidator_ValidateBag_InvalidMagic(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	content := []byte("not a valid ros bag file")

	tmpFile := filepath.Join(t.TempDir(), "invalid.bag")
	err := os.WriteFile(tmpFile, content, 0644)
	require.NoError(t, err)

	result, err := v.Validate(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Valid)
	assert.Contains(t, result.Error.Error(), "invalid ROS bag magic")
}

func TestFileValidator_ValidateDB3_ValidFile(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	// SQLite magic: "SQLite format 3\x00"
	content := []byte("SQLite format 3\x00some database content here")

	tmpFile := filepath.Join(t.TempDir(), "valid.db3")
	err := os.WriteFile(tmpFile, content, 0644)
	require.NoError(t, err)

	result, err := v.Validate(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Valid)
	assert.Equal(t, "db3", result.FileType)
	assert.NotEmpty(t, result.Checksum)
}

func TestFileValidator_ValidateDB3_InvalidMagic(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	content := []byte("not a valid sqlite database file")

	tmpFile := filepath.Join(t.TempDir(), "invalid.db3")
	err := os.WriteFile(tmpFile, content, 0644)
	require.NoError(t, err)

	result, err := v.Validate(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Valid)
	assert.Contains(t, result.Error.Error(), "invalid SQLite")
}

func TestFileValidator_Checksum_Consistent(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	// Create a valid bag file
	content := []byte("#ROSBAG V2.0\ntest content for checksum")

	tmpFile := filepath.Join(t.TempDir(), "test.bag")
	err := os.WriteFile(tmpFile, content, 0644)
	require.NoError(t, err)

	// Validate twice
	result1, err := v.Validate(tmpFile)
	require.NoError(t, err)

	result2, err := v.Validate(tmpFile)
	require.NoError(t, err)

	// Checksums should be identical
	assert.Equal(t, result1.Checksum, result2.Checksum)
	assert.NotEmpty(t, result1.Checksum)
}

func TestFileValidator_Checksum_DifferentForDifferentContent(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	// Create two bag files with different content
	tmpFile1 := filepath.Join(t.TempDir(), "test1.bag")
	err := os.WriteFile(tmpFile1, []byte("#ROSBAG V2.0\ncontent one"), 0644)
	require.NoError(t, err)

	tmpFile2 := filepath.Join(t.TempDir(), "test2.bag")
	err = os.WriteFile(tmpFile2, []byte("#ROSBAG V2.0\ncontent two"), 0644)
	require.NoError(t, err)

	result1, err := v.Validate(tmpFile1)
	require.NoError(t, err)

	result2, err := v.Validate(tmpFile2)
	require.NoError(t, err)

	// Checksums should be different
	assert.NotEqual(t, result1.Checksum, result2.Checksum)
}

func TestCalculateChecksum(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	content := []byte("hello world")
	err := os.WriteFile(tmpFile, content, 0644)
	require.NoError(t, err)

	f, err := os.Open(tmpFile)
	require.NoError(t, err)
	defer f.Close()

	checksum, err := calculateChecksum(f)
	require.NoError(t, err)

	// SHA256 of "hello world"
	expected := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	assert.Equal(t, expected, checksum)
}

func TestFileValidator_ValidateDB3_WithMetadata(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	// Create a real SQLite database with ROS 2 bag schema
	tmpFile := filepath.Join(t.TempDir(), "test.db3")

	db, err := createTestDB3(tmpFile)
	require.NoError(t, err)
	db.Close()

	result, err := v.Validate(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Valid)
	assert.Equal(t, "db3", result.FileType)
	assert.NotEmpty(t, result.Checksum)

	// Check metadata was extracted
	require.NotNil(t, result.Metadata)
	assert.Len(t, result.Metadata.Topics, 2)
	assert.Equal(t, "/camera/image", result.Metadata.Topics[0].Name)
	assert.Equal(t, "sensor_msgs/msg/Image", result.Metadata.Topics[0].Type)
	assert.Equal(t, "/lidar/scan", result.Metadata.Topics[1].Name)
	assert.Equal(t, "sensor_msgs/msg/LaserScan", result.Metadata.Topics[1].Type)

	// Check message count
	assert.Equal(t, int64(150), result.Metadata.MessageCount) // 100 + 50

	// Check time range
	assert.NotNil(t, result.Metadata.StartTime)
	assert.NotNil(t, result.Metadata.EndTime)
	assert.Greater(t, result.Metadata.Duration, 0.0)
}

func TestParseBagFields(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected map[string]string
	}{
		{
			name: "single field",
			data: buildBagFields(map[string]string{"op": "\x07"}),
			expected: map[string]string{
				"op": "\x07",
			},
		},
		{
			name: "multiple fields",
			data: buildBagFields(map[string]string{
				"topic": "/camera/image",
				"type":  "sensor_msgs/Image",
			}),
			expected: map[string]string{
				"topic": "/camera/image",
				"type":  "sensor_msgs/Image",
			},
		},
		{
			name:     "empty data",
			data:     []byte{},
			expected: map[string]string{},
		},
		{
			name:     "truncated field length",
			data:     []byte{0x01}, // Only 1 byte, need 4 for length
			expected: map[string]string{},
		},
		{
			name:     "field length exceeds data",
			data:     []byte{0xFF, 0xFF, 0x00, 0x00}, // Length 65535 but no data
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseBagFields(tt.data)
			assert.Len(t, result, len(tt.expected))
			for k, v := range tt.expected {
				assert.Equal(t, []byte(v), result[k])
			}
		})
	}
}

func TestParseBagConnection(t *testing.T) {
	tests := []struct {
		name         string
		data         []byte
		wantTopic    string
		wantMsgType  string
	}{
		{
			name:         "valid connection",
			data:         buildBagFields(map[string]string{"topic": "/camera/image", "type": "sensor_msgs/Image"}),
			wantTopic:    "/camera/image",
			wantMsgType:  "sensor_msgs/Image",
		},
		{
			name:         "only topic",
			data:         buildBagFields(map[string]string{"topic": "/lidar/scan"}),
			wantTopic:    "/lidar/scan",
			wantMsgType:  "",
		},
		{
			name:         "only type",
			data:         buildBagFields(map[string]string{"type": "std_msgs/String"}),
			wantTopic:    "",
			wantMsgType:  "std_msgs/String",
		},
		{
			name:         "empty data",
			data:         []byte{},
			wantTopic:    "",
			wantMsgType:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			topic, msgType := parseBagConnection(tt.data)
			assert.Equal(t, tt.wantTopic, topic)
			assert.Equal(t, tt.wantMsgType, msgType)
		})
	}
}

func TestParseBagChunkInfo(t *testing.T) {
	t.Run("valid chunk info", func(t *testing.T) {
		// Build a valid chunk info record
		// Format: version (4) + chunk_pos (8) + start_time (8) + end_time (8) + conn_count (4) + conn entries
		data := make([]byte, 0)
		// Version = 1
		data = appendUint32(data, 1)
		// Chunk position = 0
		data = appendUint64(data, 0)
		// Start time: 1704067200 seconds, 0 nanoseconds (2024-01-01)
		data = appendUint32(data, 1704067200)
		data = appendUint32(data, 0)
		// End time: 1704067260 seconds, 0 nanoseconds
		data = appendUint32(data, 1704067260)
		data = appendUint32(data, 0)
		// Connection count = 2
		data = appendUint32(data, 2)
		// Connection 0: ID=1, count=100
		data = appendUint32(data, 1)
		data = appendUint32(data, 100)
		// Connection 1: ID=2, count=50
		data = appendUint32(data, 2)
		data = appendUint32(data, 50)

		startTime, endTime, connCounts := parseBagChunkInfo(data)

		require.NotNil(t, startTime)
		require.NotNil(t, endTime)
		assert.Equal(t, int64(1704067200), startTime.Unix())
		assert.Equal(t, int64(1704067260), endTime.Unix())
		assert.Len(t, connCounts, 2)
		assert.Equal(t, uint32(100), connCounts[1])
		assert.Equal(t, uint32(50), connCounts[2])
	})

	t.Run("truncated data", func(t *testing.T) {
		// Data less than 16 bytes
		data := make([]byte, 10)
		startTime, endTime, connCounts := parseBagChunkInfo(data)

		assert.Nil(t, startTime)
		assert.Nil(t, endTime)
		assert.Empty(t, connCounts)
	})

	t.Run("zero start time", func(t *testing.T) {
		data := make([]byte, 0)
		data = appendUint32(data, 1) // version
		data = appendUint64(data, 0) // chunk_pos
		data = appendUint32(data, 0) // start_sec = 0 (should result in nil)
		data = appendUint32(data, 0) // start_nsec
		data = appendUint32(data, 1704067260) // end_sec
		data = appendUint32(data, 0) // end_nsec
		data = appendUint32(data, 0) // conn_count

		startTime, endTime, connCounts := parseBagChunkInfo(data)

		assert.Nil(t, startTime)
		require.NotNil(t, endTime)
		assert.Equal(t, int64(1704067260), endTime.Unix())
		assert.Empty(t, connCounts)
	})
}

func TestReadBagRecord(t *testing.T) {
	t.Run("valid connection record", func(t *testing.T) {
		// Build a connection record
		// Header: op=0x07 (connection)
		header := buildBagFields(map[string]string{"op": string([]byte{bagOpConnection})})
		// Data: topic and type
		data := buildBagFields(map[string]string{"topic": "/test", "type": "std_msgs/String"})

		buf := make([]byte, 0)
		buf = appendUint32(buf, uint32(len(header)))
		buf = append(buf, header...)
		buf = appendUint32(buf, uint32(len(data)))
		buf = append(buf, data...)

		reader := bytes.NewReader(buf)
		record, err := readBagRecord(reader)

		require.NoError(t, err)
		require.NotNil(t, record)
		assert.Equal(t, bagOpConnection, record.op)
		assert.NotNil(t, record.data)
	})

	t.Run("chunk record skips data", func(t *testing.T) {
		// Build a chunk record
		header := buildBagFields(map[string]string{"op": string([]byte{bagOpChunk})})
		data := []byte("large chunk data that should be skipped")

		buf := make([]byte, 0)
		buf = appendUint32(buf, uint32(len(header)))
		buf = append(buf, header...)
		buf = appendUint32(buf, uint32(len(data)))
		buf = append(buf, data...)

		reader := bytes.NewReader(buf)
		record, err := readBagRecord(reader)

		require.NoError(t, err)
		require.NotNil(t, record)
		assert.Equal(t, bagOpChunk, record.op)
		assert.Nil(t, record.data) // Data should be skipped
	})

	t.Run("header too large", func(t *testing.T) {
		buf := make([]byte, 0)
		buf = appendUint32(buf, 200*1024*1024) // 200MB header - too large

		reader := bytes.NewReader(buf)
		_, err := readBagRecord(reader)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "header length too large")
	})

	t.Run("data too large", func(t *testing.T) {
		header := buildBagFields(map[string]string{"op": string([]byte{bagOpConnection})})

		buf := make([]byte, 0)
		buf = appendUint32(buf, uint32(len(header)))
		buf = append(buf, header...)
		buf = appendUint32(buf, 2*1024*1024*1024) // 2GB data - too large

		reader := bytes.NewReader(buf)
		_, err := readBagRecord(reader)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "data length too large")
	})

	t.Run("EOF reading header length", func(t *testing.T) {
		reader := bytes.NewReader([]byte{})
		_, err := readBagRecord(reader)

		require.Error(t, err)
	})

	t.Run("EOF reading header data", func(t *testing.T) {
		buf := make([]byte, 0)
		buf = appendUint32(buf, 100) // Header length 100 but no data

		reader := bytes.NewReader(buf)
		_, err := readBagRecord(reader)

		require.Error(t, err)
	})
}

func TestFileValidator_ValidateMCAP_EmptyFile(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	tmpFile := filepath.Join(t.TempDir(), "empty.mcap")
	err := os.WriteFile(tmpFile, []byte{}, 0644)
	require.NoError(t, err)

	_, err = v.Validate(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "magic bytes")
}

func TestFileValidator_ValidateBag_EmptyFile(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	tmpFile := filepath.Join(t.TempDir(), "empty.bag")
	err := os.WriteFile(tmpFile, []byte{}, 0644)
	require.NoError(t, err)

	_, err = v.Validate(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "magic bytes")
}

func TestFileValidator_ValidateDB3_EmptyFile(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	tmpFile := filepath.Join(t.TempDir(), "empty.db3")
	err := os.WriteFile(tmpFile, []byte{}, 0644)
	require.NoError(t, err)

	_, err = v.Validate(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "magic bytes")
}

func TestResultStruct(t *testing.T) {
	result := &Result{
		Valid:    true,
		FileType: "mcap",
		Checksum: "abc123",
		Error:    nil,
	}

	assert.True(t, result.Valid)
	assert.Equal(t, "mcap", result.FileType)
	assert.Equal(t, "abc123", result.Checksum)
	assert.Nil(t, result.Error)
}

func TestFileValidator_ValidateMCAP_RealFile(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	// Create a proper MCAP file using the mcap writer
	tmpFile := filepath.Join(t.TempDir(), "test.mcap")
	f, err := os.Create(tmpFile)
	require.NoError(t, err)

	writer, err := mcap.NewWriter(f, &mcap.WriterOptions{
		Chunked: true,
	})
	require.NoError(t, err)

	// Write header
	err = writer.WriteHeader(&mcap.Header{
		Profile: "ros2",
	})
	require.NoError(t, err)

	// Write a schema
	err = writer.WriteSchema(&mcap.Schema{
		ID:       1,
		Name:     "std_msgs/String",
		Encoding: "ros2msg",
		Data:     []byte("string data"),
	})
	require.NoError(t, err)

	// Write a channel
	err = writer.WriteChannel(&mcap.Channel{
		ID:              1,
		SchemaID:        1,
		Topic:           "/chatter",
		MessageEncoding: "cdr",
	})
	require.NoError(t, err)

	// Write a message
	err = writer.WriteMessage(&mcap.Message{
		ChannelID:   1,
		Sequence:    1,
		LogTime:     1000000000, // 1 second in nanoseconds
		PublishTime: 1000000000,
		Data:        []byte("hello world"),
	})
	require.NoError(t, err)

	// Close the writer (writes footer)
	err = writer.Close()
	require.NoError(t, err)
	f.Close()

	// Now validate the file
	result, err := v.Validate(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Valid)
	assert.Equal(t, "mcap", result.FileType)
	assert.NotEmpty(t, result.Checksum)
	require.NotNil(t, result.Metadata)
	assert.Len(t, result.Metadata.Topics, 1)
	assert.Equal(t, "/chatter", result.Metadata.Topics[0].Name)
	assert.Equal(t, "std_msgs/String", result.Metadata.Topics[0].Type)
}

func TestFileValidator_ValidateMCAP_NoSchemas(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	// Create an MCAP file without schemas
	tmpFile := filepath.Join(t.TempDir(), "no_schema.mcap")
	f, err := os.Create(tmpFile)
	require.NoError(t, err)

	writer, err := mcap.NewWriter(f, &mcap.WriterOptions{
		Chunked: true,
	})
	require.NoError(t, err)

	err = writer.WriteHeader(&mcap.Header{
		Profile: "ros2",
	})
	require.NoError(t, err)

	// Write a channel without schema
	err = writer.WriteChannel(&mcap.Channel{
		ID:              1,
		SchemaID:        0, // No schema
		Topic:           "/test",
		MessageEncoding: "json",
	})
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)
	f.Close()

	result, err := v.Validate(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Valid)
	assert.Equal(t, "mcap", result.FileType)
}

func TestExtractBagMetadata_WithRecords(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	// Create a bag file with proper structure
	tmpFile := filepath.Join(t.TempDir(), "test.bag")

	// Build a bag file with connection and chunk info records
	buf := bytes.NewBuffer(nil)

	// Write magic
	buf.WriteString("#ROSBAG V2.0\n")

	// Write a connection record (op=0x07)
	writeConnectionRecord(buf, "/camera/image", "sensor_msgs/Image")

	// Write chunk info record (op=0x06)
	writeChunkInfoRecord(buf, 1704067200, 1704067260, map[uint32]uint32{1: 100})

	err := os.WriteFile(tmpFile, buf.Bytes(), 0644)
	require.NoError(t, err)

	result, err := v.Validate(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Valid)
	assert.Equal(t, "bag", result.FileType)
	require.NotNil(t, result.Metadata)
	assert.Len(t, result.Metadata.Topics, 1)
	assert.Equal(t, "/camera/image", result.Metadata.Topics[0].Name)
	assert.Equal(t, "sensor_msgs/Image", result.Metadata.Topics[0].Type)
	assert.NotNil(t, result.Metadata.StartTime)
	assert.NotNil(t, result.Metadata.EndTime)
}

func TestExtractBagMetadata_MultipleConnections(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	tmpFile := filepath.Join(t.TempDir(), "multi_conn.bag")
	buf := bytes.NewBuffer(nil)
	buf.WriteString("#ROSBAG V2.0\n")

	// Write multiple connection records
	writeConnectionRecord(buf, "/camera/image", "sensor_msgs/Image")
	writeConnectionRecord(buf, "/lidar/scan", "sensor_msgs/LaserScan")
	writeConnectionRecord(buf, "/camera/image", "sensor_msgs/Image") // Duplicate topic

	err := os.WriteFile(tmpFile, buf.Bytes(), 0644)
	require.NoError(t, err)

	result, err := v.Validate(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Valid)
	require.NotNil(t, result.Metadata)
	// Should only have 2 unique topics
	assert.Len(t, result.Metadata.Topics, 2)
}

func TestExtractBagMetadata_ChunkInfoWithTime(t *testing.T) {
	log := zerolog.Nop()
	v := New(log)

	tmpFile := filepath.Join(t.TempDir(), "time_range.bag")
	buf := bytes.NewBuffer(nil)
	buf.WriteString("#ROSBAG V2.0\n")

	// Write multiple chunk info records with different time ranges
	writeChunkInfoRecord(buf, 1704067200, 1704067260, map[uint32]uint32{1: 50})
	writeChunkInfoRecord(buf, 1704067260, 1704067320, map[uint32]uint32{1: 50, 2: 25})

	err := os.WriteFile(tmpFile, buf.Bytes(), 0644)
	require.NoError(t, err)

	result, err := v.Validate(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Metadata)
	assert.NotNil(t, result.Metadata.StartTime)
	assert.NotNil(t, result.Metadata.EndTime)
	// Total messages: 50 + 50 + 25 = 125
	assert.Equal(t, int64(125), result.Metadata.MessageCount)
	// Duration should be calculated
	assert.Greater(t, result.Metadata.Duration, 0.0)
}

func TestCalculateChecksum_Error(t *testing.T) {
	// Test with a reader that fails
	reader := &errorReader{}
	_, err := calculateChecksum(reader)
	require.Error(t, err)
}

// Helper type for testing checksum errors
type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, os.ErrClosed
}

// Helper function to write a bag connection record
func writeConnectionRecord(buf *bytes.Buffer, topic, msgType string) {
	// Build header with op=0x07 (connection)
	header := buildBagFields(map[string]string{"op": string([]byte{bagOpConnection})})

	// Build data with topic and type
	data := buildBagFields(map[string]string{"topic": topic, "type": msgType})

	// Write header length + header + data length + data
	writeBagRecord(buf, header, data)
}

// Helper function to write a bag chunk info record
func writeChunkInfoRecord(buf *bytes.Buffer, startSec, endSec uint32, connCounts map[uint32]uint32) {
	// Build header with op=0x06 (chunk_info)
	header := buildBagFields(map[string]string{"op": string([]byte{bagOpChunkInfo})})

	// Build chunk info data
	data := make([]byte, 0)
	data = appendUint32(data, 1)                        // version
	data = appendUint64(data, 0)                        // chunk_pos
	data = appendUint32(data, startSec)                 // start_sec
	data = appendUint32(data, 0)                        // start_nsec
	data = appendUint32(data, endSec)                   // end_sec
	data = appendUint32(data, 0)                        // end_nsec
	data = appendUint32(data, uint32(len(connCounts)))  // conn_count
	for id, count := range connCounts {
		data = appendUint32(data, id)
		data = appendUint32(data, count)
	}

	writeBagRecord(buf, header, data)
}

func writeBagRecord(buf *bytes.Buffer, header, data []byte) {
	headerLen := make([]byte, 4)
	binary.LittleEndian.PutUint32(headerLen, uint32(len(header)))
	buf.Write(headerLen)
	buf.Write(header)

	dataLen := make([]byte, 4)
	binary.LittleEndian.PutUint32(dataLen, uint32(len(data)))
	buf.Write(dataLen)
	buf.Write(data)
}

// Helper functions for building bag data structures

func buildBagFields(fields map[string]string) []byte {
	result := make([]byte, 0)
	for k, v := range fields {
		field := k + "=" + v
		result = appendUint32(result, uint32(len(field)))
		result = append(result, []byte(field)...)
	}
	return result
}

func appendUint32(buf []byte, v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return append(buf, b...)
}

func appendUint64(buf []byte, v uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)
	return append(buf, b...)
}

// createTestDB3 creates a test db3 file with ROS 2 bag schema
func createTestDB3(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	// Create ROS 2 bag schema
	schema := `
		CREATE TABLE topics (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			serialization_format TEXT NOT NULL,
			offered_qos_profiles TEXT
		);

		CREATE TABLE messages (
			id INTEGER PRIMARY KEY,
			topic_id INTEGER NOT NULL,
			timestamp INTEGER NOT NULL,
			data BLOB NOT NULL,
			FOREIGN KEY (topic_id) REFERENCES topics(id)
		);

		-- Insert test topics
		INSERT INTO topics (id, name, type, serialization_format) VALUES
			(1, '/camera/image', 'sensor_msgs/msg/Image', 'cdr'),
			(2, '/lidar/scan', 'sensor_msgs/msg/LaserScan', 'cdr');

		-- Insert test messages with timestamps in nanoseconds
		-- Start: 1704067200000000000 (2024-01-01 00:00:00 UTC)
		-- End: 1704067260000000000 (2024-01-01 00:01:00 UTC) - 60 seconds later
	`

	_, err = db.Exec(schema)
	if err != nil {
		db.Close()
		return nil, err
	}

	// Insert 100 messages for topic 1 and 50 for topic 2
	startNs := int64(1704067200000000000) // 2024-01-01 00:00:00 UTC
	endNs := int64(1704067260000000000)   // 2024-01-01 00:01:00 UTC

	stmt, err := db.Prepare("INSERT INTO messages (topic_id, timestamp, data) VALUES (?, ?, ?)")
	if err != nil {
		db.Close()
		return nil, err
	}
	defer stmt.Close()

	// Insert messages for topic 1
	for i := 0; i < 100; i++ {
		ts := startNs + int64(i)*600000000 // Every 0.6 seconds
		_, err = stmt.Exec(1, ts, []byte{0x01, 0x02})
		if err != nil {
			db.Close()
			return nil, err
		}
	}

	// Insert messages for topic 2
	for i := 0; i < 50; i++ {
		ts := startNs + int64(i)*1200000000 // Every 1.2 seconds
		if i == 49 {
			ts = endNs // Last message at end time
		}
		_, err = stmt.Exec(2, ts, []byte{0x03, 0x04})
		if err != nil {
			db.Close()
			return nil, err
		}
	}

	return db, nil
}
