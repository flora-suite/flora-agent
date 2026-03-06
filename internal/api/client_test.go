package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	client := NewClient("https://api.flora.fan", "test-token")
	require.NotNil(t, client)

	assert.Equal(t, "https://api.flora.fan", client.baseURL)
	assert.Equal(t, "test-token", client.token)
	assert.NotNil(t, client.httpClient)
}

func TestClient_Heartbeat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/agent/heartbeat", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Verify body
		var req HeartbeatRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.True(t, req.Status.Online)

		// Send response wrapped in { data: ... }
		resp := map[string]interface{}{
			"data": HeartbeatResponse{
				Ack:        true,
				ServerTime: "2024-01-15T10:00:00Z",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	status := &HeartbeatStatus{
		Online: true,
		System: SystemStatus{
			CPUUsage:    50.0,
			MemoryUsage: 75.0,
			DiskUsage:   60.0,
			Uptime:      3600,
		},
		Agent: AgentStatus{
			Version:        "1.0.0",
			WatchedPaths:   []string{"/data"},
			PendingUploads: 5,
			UploadingCount: 2,
		},
	}
	resp, err := client.Heartbeat(context.Background(), status)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Ack)
	assert.Equal(t, "2024-01-15T10:00:00Z", resp.ServerTime)
}

func TestClient_Heartbeat_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid token"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "bad-token")
	status := &HeartbeatStatus{
		Online: true,
		System: SystemStatus{},
		Agent:  AgentStatus{WatchedPaths: []string{}},
	}
	_, err := client.Heartbeat(context.Background(), status)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestClient_GetConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/agent/config", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		resp := map[string]interface{}{
			"data": AgentConfig{
				UploadChunkSize:      10 * 1024 * 1024,
				MaxConcurrentUploads: 2,
				HeartbeatInterval:    30,
				AllowedFileTypes:     []string{".mcap", ".bag", ".db3"},
				MaxFileSize:          1024 * 1024 * 1024,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	config, err := client.GetConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, config)

	assert.Equal(t, int64(10*1024*1024), config.UploadChunkSize)
	assert.Equal(t, 2, config.MaxConcurrentUploads)
	assert.Equal(t, 30, config.HeartbeatInterval)
	assert.Len(t, config.AllowedFileTypes, 3)
}

func TestClient_InitMultipartUpload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/recordings/upload/multipart", r.URL.Path)

		var req InitMultipartUploadRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		assert.Equal(t, "recording.mcap", req.Filename)
		assert.Equal(t, int64(100*1024*1024), req.Size)
		assert.Equal(t, "sha256:abc123", req.Checksum)
		assert.Equal(t, "mcap", req.FileType)

		resp := map[string]interface{}{
			"data": InitMultipartUploadResponse{
				UploadID:    "upload-123",
				ChunkSize:   10 * 1024 * 1024,
				TotalChunks: 10,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	req := &InitMultipartUploadRequest{
		Filename: "recording.mcap",
		Size:     100 * 1024 * 1024,
		Checksum: "sha256:abc123",
		FileType: "mcap",
	}

	resp, err := client.InitMultipartUpload(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "upload-123", resp.UploadID)
	assert.Equal(t, int64(10*1024*1024), resp.ChunkSize)
	assert.Equal(t, 10, resp.TotalChunks)
}

func TestClient_UploadChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/recordings/upload/multipart/upload-123/chunks/0", r.URL.Path)
		assert.Equal(t, "application/octet-stream", r.Header.Get("Content-Type"))

		resp := map[string]interface{}{
			"data": UploadChunkResponse{
				ETag:  "etag-chunk-0",
				Index: 0,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	chunkData := make([]byte, 1024)

	resp, err := client.UploadChunk(context.Background(), "upload-123", 0, chunkData)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "etag-chunk-0", resp.ETag)
	assert.Equal(t, 0, resp.Index)
}

func TestClient_CompleteMultipartUpload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/recordings/upload/multipart/upload-123/complete", r.URL.Path)

		var req CompleteMultipartUploadRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		assert.Len(t, req.Parts, 2)
		assert.Equal(t, 0, req.Parts[0].Index)
		assert.Equal(t, "etag-0", req.Parts[0].ETag)

		resp := map[string]interface{}{
			"data": CompleteMultipartUploadResponse{
				RecordingID: "rec-456",
				URL:         "https://storage.flora.fan/recordings/rec-456.mcap",
				Size:        100 * 1024 * 1024,
				CreatedAt:   "2024-01-15T10:00:00Z",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	parts := []UploadPart{
		{Index: 0, ETag: "etag-0"},
		{Index: 1, ETag: "etag-1"},
	}

	resp, err := client.CompleteMultipartUpload(context.Background(), "upload-123", parts)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "rec-456", resp.RecordingID)
	assert.Contains(t, resp.URL, "rec-456")
}

func TestClient_GetUploadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/recordings/upload/multipart/upload-123/status", r.URL.Path)

		resp := map[string]interface{}{
			"data": UploadStatusResponse{
				UploadID:       "upload-123",
				Filename:       "recording.mcap",
				Size:           100 * 1024 * 1024,
				ChunkSize:      10 * 1024 * 1024,
				TotalChunks:    10,
				UploadedChunks: []int{0, 1, 2, 3},
				CreatedAt:      "2024-01-15T09:00:00Z",
				ExpiresAt:      "2024-01-16T09:00:00Z",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	status, err := client.GetUploadStatus(context.Background(), "upload-123")
	require.NoError(t, err)
	require.NotNil(t, status)

	assert.Equal(t, "upload-123", status.UploadID)
	assert.Equal(t, 10, status.TotalChunks)
	assert.Equal(t, []int{0, 1, 2, 3}, status.UploadedChunks)
}

func TestClient_AbortMultipartUpload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/recordings/upload/multipart/upload-123", r.URL.Path)

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	err := client.AbortMultipartUpload(context.Background(), "upload-123")
	require.NoError(t, err)
}

func TestClient_SimpleUpload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/recordings/upload", r.URL.Path)
		assert.Equal(t, "application/octet-stream", r.Header.Get("Content-Type"))
		assert.Equal(t, "small.mcap", r.Header.Get("X-Filename"))

		resp := struct {
			Data SimpleUploadResponse `json:"data"`
		}{
			Data: SimpleUploadResponse{
				RecordingID: "rec-789",
				URL:         "https://storage.flora.fan/rec-789.mcap",
				Size:        1024,
				CreatedAt:   "2024-01-15T10:00:00Z",
			},
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	data := make([]byte, 1024)

	uploadResp, err := client.SimpleUpload(context.Background(), "small.mcap", data, "")
	require.NoError(t, err)
	assert.Equal(t, "rec-789", uploadResp.RecordingID)
}

func TestClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay response
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	status := &HeartbeatStatus{
		Online: true,
		System: SystemStatus{},
		Agent:  AgentStatus{WatchedPaths: []string{}},
	}
	_, err := client.Heartbeat(ctx, status)
	require.Error(t, err)
}

func TestClient_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": {"code": "INTERNAL_ERROR", "message": "Something went wrong"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")

	_, err := client.GetConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestClient_SetToken(t *testing.T) {
	client := NewClient("https://api.flora.fan", "initial-token")
	assert.Equal(t, "initial-token", client.token)

	client.SetToken("new-token")
	assert.Equal(t, "new-token", client.token)
}

func TestClient_Register(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/agent/register", r.URL.Path)
		assert.Equal(t, "Bearer user-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req RegisterRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "test-device", req.Name)
		assert.Equal(t, "robot", req.Type)
		assert.Equal(t, "machine-123", req.MachineID)

		resp := map[string]interface{}{
			"data": RegisterResponse{
				Device: Device{
					ID:        "device-456",
					Name:      "test-device",
					Type:      "robot",
					Status:    "online",
					Enabled:   true,
					CreatedAt: "2024-01-15T10:00:00Z",
					UpdatedAt: "2024-01-15T10:00:00Z",
				},
				Token: "device-token-abc",
				IsNew: true,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	req := &RegisterRequest{
		Name:      "test-device",
		Type:      "robot",
		MachineID: "machine-123",
	}

	resp, err := client.Register(context.Background(), "user-token", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "device-456", resp.Device.ID)
	assert.Equal(t, "device-token-abc", resp.Token)
	assert.True(t, resp.IsNew)
}

func TestClient_Register_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid user token"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	req := &RegisterRequest{
		Name:      "test-device",
		Type:      "robot",
		MachineID: "machine-123",
	}

	_, err := client.Register(context.Background(), "bad-token", req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestClient_RegisterInit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/device/register/init", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		// Should not have Authorization header
		assert.Empty(t, r.Header.Get("Authorization"))

		var req RegisterInitRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "machine-abc", req.MachineID)
		assert.Equal(t, "my-robot", req.Hostname)

		resp := map[string]interface{}{
			"data": RegisterInitResponse{
				Code:            "ABC123",
				RegistrationURL: "https://flora.fan/register?code=ABC123",
				ExpiresAt:       "2024-01-15T10:10:00Z",
				ExpiresIn:       600,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	req := &RegisterInitRequest{
		MachineID: "machine-abc",
		Hostname:  "my-robot",
		Platform:  "linux/arm64",
		IPAddress: "192.168.1.100",
		SystemInfo: &SystemInfo{
			CPUCores: 4,
			MemoryGB: 8.0,
		},
	}

	resp, err := client.RegisterInit(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "ABC123", resp.Code)
	assert.Contains(t, resp.RegistrationURL, "ABC123")
	assert.Equal(t, 600, resp.ExpiresIn)
}

func TestClient_RegisterInit_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid request"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	req := &RegisterInitRequest{
		MachineID: "",
	}

	_, err := client.RegisterInit(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestClient_RegisterPoll_Pending(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/device/register/poll", r.URL.Path)

		var req RegisterPollRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "ABC123", req.Code)

		resp := map[string]interface{}{
			"data": RegisterPollResponse{
				Status: "pending",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	resp, err := client.RegisterPoll(context.Background(), "ABC123")
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "pending", resp.Status)
	assert.Empty(t, resp.DeviceToken)
}

func TestClient_RegisterPoll_Completed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"status":      "completed",
				"deviceToken": "device-token-xyz",
				"device": map[string]interface{}{
					"id":   "device-789",
					"name": "my-robot",
					"type": "robot",
				},
				"config": map[string]interface{}{
					"watchPaths":        []string{"/data/recordings"},
					"uploadChunkSize":   10485760,
					"heartbeatInterval": 30,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	resp, err := client.RegisterPoll(context.Background(), "ABC123")
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "completed", resp.Status)
	assert.Equal(t, "device-token-xyz", resp.DeviceToken)
	require.NotNil(t, resp.Device)
	assert.Equal(t, "device-789", resp.Device.ID)
	require.NotNil(t, resp.Config)
	assert.Equal(t, []string{"/data/recordings"}, resp.Config.WatchPaths)
}

func TestClient_RegisterPoll_Expired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": RegisterPollResponse{
				Status: "expired",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	resp, err := client.RegisterPoll(context.Background(), "ABC123")
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "expired", resp.Status)
}

func TestClient_RegisterPoll_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "code not found"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	_, err := client.RegisterPoll(context.Background(), "INVALID")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestClient_UploadChunk_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "upload not found"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	chunkData := make([]byte, 1024)

	_, err := client.UploadChunk(context.Background(), "invalid-upload", 0, chunkData)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestClient_SimpleUpload_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid file"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	data := make([]byte, 1024)

	_, err := client.SimpleUpload(context.Background(), "invalid.txt", data, "checksum")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestClient_SimpleUpload_WithChecksum(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "sha256:abc123", r.Header.Get("X-Checksum"))

		resp := struct {
			Data SimpleUploadResponse `json:"data"`
		}{
			Data: SimpleUploadResponse{
				RecordingID: "rec-123",
				URL:         "https://storage.flora.fan/rec-123.mcap",
				Size:        1024,
				CreatedAt:   "2024-01-15T10:00:00Z",
			},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	data := make([]byte, 1024)

	resp, err := client.SimpleUpload(context.Background(), "test.mcap", data, "sha256:abc123")
	require.NoError(t, err)
	assert.Equal(t, "rec-123", resp.RecordingID)
}

func TestClient_AbortMultipartUpload_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "upload not found"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	err := client.AbortMultipartUpload(context.Background(), "invalid-upload")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestClient_InitMultipartUpload_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid request"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	req := &InitMultipartUploadRequest{
		Filename: "",
	}

	_, err := client.InitMultipartUpload(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestClient_CompleteMultipartUpload_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "missing parts"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	parts := []UploadPart{}

	_, err := client.CompleteMultipartUpload(context.Background(), "upload-123", parts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestClient_GetUploadStatus_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "upload not found"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	_, err := client.GetUploadStatus(context.Background(), "invalid-upload")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestClient_GetConfig_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "unauthorized"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "bad-token")
	_, err := client.GetConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestError_Error(t *testing.T) {
	err := &Error{
		StatusCode: 404,
		Status:     "404 Not Found",
		Body:       "resource not found",
	}

	assert.Contains(t, err.Error(), "404 Not Found")
	assert.Contains(t, err.Error(), "resource not found")
}

func TestClient_InitMultipartUpload_WithMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req InitMultipartUploadRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		// Verify metadata is included
		require.NotNil(t, req.Metadata)
		assert.Len(t, req.Metadata.Topics, 2)
		assert.Equal(t, "/camera", req.Metadata.Topics[0].Name)
		assert.Equal(t, 60.0, req.Metadata.Duration)

		resp := map[string]interface{}{
			"data": InitMultipartUploadResponse{
				UploadID:    "upload-456",
				ChunkSize:   10 * 1024 * 1024,
				TotalChunks: 5,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	req := &InitMultipartUploadRequest{
		Filename: "recording.mcap",
		Size:     50 * 1024 * 1024,
		Checksum: "sha256:xyz",
		FileType: "mcap",
		Metadata: &RecordingMetadata{
			Topics: []TopicInfo{
				{Name: "/camera", Type: "sensor_msgs/Image", MessageCount: 1000},
				{Name: "/lidar", Type: "sensor_msgs/LaserScan", MessageCount: 500},
			},
			StartTime:    "2024-01-15T09:00:00Z",
			EndTime:      "2024-01-15T09:01:00Z",
			Duration:     60.0,
			MessageCount: 1500,
		},
	}

	resp, err := client.InitMultipartUpload(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "upload-456", resp.UploadID)
}

func TestClient_Heartbeat_WithROS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req HeartbeatRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		// Verify ROS info is included
		require.NotNil(t, req.Status.ROS)
		assert.Equal(t, "humble", req.Status.ROS.Distro)
		assert.Equal(t, 5, req.Status.ROS.NodeCount)

		resp := map[string]interface{}{
			"data": HeartbeatResponse{
				Ack:        true,
				ServerTime: "2024-01-15T10:00:00Z",
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	status := &HeartbeatStatus{
		Online: true,
		System: SystemStatus{
			CPUUsage:    25.0,
			MemoryUsage: 50.0,
			DiskUsage:   40.0,
			Uptime:      7200,
		},
		Agent: AgentStatus{
			Version:        "1.0.0",
			WatchedPaths:   []string{"/data"},
			PendingUploads: 0,
			UploadingCount: 0,
		},
		ROS: &ROSStatus{
			Distro:     "humble",
			NodeCount:  5,
			TopicCount: 20,
		},
	}

	resp, err := client.Heartbeat(context.Background(), status)
	require.NoError(t, err)
	assert.True(t, resp.Ack)
}
