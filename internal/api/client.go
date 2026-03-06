// Package api provides the flora-server API client.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is the flora-server API client.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// Error represents an API error with status code.
type Error struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *Error) Error() string {
	return fmt.Sprintf("API error: %s - %s", e.Status, e.Body)
}

// NewClient creates a new API client.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetToken updates the client's authentication token.
func (c *Client) SetToken(token string) {
	c.token = token
}

// ============= Device Registration =============

// RegisterRequest represents the device registration request payload.
type RegisterRequest struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	IPAddress string `json:"ipAddress,omitempty"`
	MachineID string `json:"machineId"`
}

// Device represents a registered device.
type Device struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Type            string  `json:"type"`
	Status          string  `json:"status"`
	IPAddress       *string `json:"ipAddress,omitempty"`
	Model           *string `json:"model,omitempty"`
	SerialNumber    *string `json:"serialNumber,omitempty"`
	FirmwareVersion *string `json:"firmwareVersion,omitempty"`
	Location        *string `json:"location,omitempty"`
	LastSeen        *string `json:"lastSeen,omitempty"`
	AgentVersion    *string `json:"agentVersion,omitempty"`
	AgentStatus     *string `json:"agentStatus,omitempty"`
	AgentUptime     *int64  `json:"agentUptime,omitempty"`
	CPUUsage        *float64 `json:"cpuUsage,omitempty"`
	MemoryUsage     *float64 `json:"memoryUsage,omitempty"`
	DiskUsage       *float64 `json:"diskUsage,omitempty"`
	RosDistro       *string `json:"rosDistro,omitempty"`
	RosNodeCount    *int    `json:"rosNodeCount,omitempty"`
	RosTopicCount   *int    `json:"rosTopicCount,omitempty"`
	Enabled         bool    `json:"enabled"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
}

// RegisterResponse is the response from device registration.
type RegisterResponse struct {
	Device Device `json:"device"`
	Token  string `json:"token"`
	IsNew  bool   `json:"isNew"`
}

// Register registers a device with the server using user token.
// This should be called once during initial setup.
// Returns device info and device token for subsequent API calls.
func (c *Client) Register(ctx context.Context, userToken string, req *RegisterRequest) (*RegisterResponse, error) {
	body, err := c.postWithToken(ctx, "/api/agent/register", req, userToken)
	if err != nil {
		return nil, err
	}

	// Response is wrapped in { data: ... }
	var wrapper struct {
		Data RegisterResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse register response: %w", err)
	}

	return &wrapper.Data, nil
}

// ============= Heartbeat =============

// HeartbeatRequest represents the heartbeat request payload.
type HeartbeatRequest struct {
	Status HeartbeatStatus `json:"status"`
	Topics []TopicStatus   `json:"topics,omitempty"`
}

// HeartbeatStatus contains device status information.
type HeartbeatStatus struct {
	Online bool         `json:"online"`
	System SystemStatus `json:"system"`
	Agent  AgentStatus  `json:"agent"`
	ROS    *ROSStatus   `json:"ros,omitempty"`
}

// SystemStatus contains system resource information.
type SystemStatus struct {
	CPUUsage    float64 `json:"cpuUsage"`
	MemoryUsage float64 `json:"memoryUsage"`
	DiskUsage   float64 `json:"diskUsage"`
	Uptime      int64   `json:"uptime"`
}

// AgentStatus contains agent-specific status.
type AgentStatus struct {
	Version        string   `json:"version"`
	WatchedPaths   []string `json:"watchedPaths"`
	PendingUploads int      `json:"pendingUploads"`
	UploadingCount int      `json:"uploadingCount"`
}

// ROSStatus contains optional ROS-specific information.
type ROSStatus struct {
	Distro     string `json:"distro,omitempty"`
	NodeCount  int    `json:"nodeCount,omitempty"`
	TopicCount int    `json:"topicCount,omitempty"`
}

// TopicStatus represents a ROS topic for heartbeat updates.
type TopicStatus struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Frequency *float64 `json:"frequency,omitempty"`
}

// HeartbeatResponse is the response from heartbeat API.
type HeartbeatResponse struct {
	Ack        bool   `json:"ack"`
	ServerTime string `json:"serverTime"`
}

// Heartbeat sends a heartbeat to the server with system status.
func (c *Client) Heartbeat(ctx context.Context, status *HeartbeatStatus) (*HeartbeatResponse, error) {
	req := HeartbeatRequest{
		Status: *status,
	}

	body, err := c.post(ctx, "/api/agent/heartbeat", req)
	if err != nil {
		return nil, err
	}

	// Response is wrapped in { data: ... }
	var wrapper struct {
		Data HeartbeatResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse heartbeat response: %w", err)
	}

	return &wrapper.Data, nil
}

// ============= Agent Config =============

// AgentConfig represents the configuration returned by the server.
type AgentConfig struct {
	UploadChunkSize      int64    `json:"uploadChunkSize"`
	MaxConcurrentUploads int      `json:"maxConcurrentUploads"`
	HeartbeatInterval    int      `json:"heartbeatInterval"`
	AllowedFileTypes     []string `json:"allowedFileTypes"`
	MaxFileSize          int64    `json:"maxFileSize"`
}

// GetConfig retrieves agent configuration from the server.
func (c *Client) GetConfig(ctx context.Context) (*AgentConfig, error) {
	body, err := c.get(ctx, "/api/agent/config")
	if err != nil {
		return nil, err
	}

	// Response is wrapped in { data: ... }
	var wrapper struct {
		Data AgentConfig `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse config response: %w", err)
	}

	return &wrapper.Data, nil
}

// ============= Multipart Upload =============

// InitMultipartUploadRequest is the request to start a multipart upload.
type InitMultipartUploadRequest struct {
	Filename string             `json:"filename"`
	Size     int64              `json:"size"`
	Checksum string             `json:"checksum"`
	FileType string             `json:"fileType"`
	Metadata *RecordingMetadata `json:"metadata,omitempty"`
}

// RecordingMetadata contains metadata about a recording file.
type RecordingMetadata struct {
	Topics       []TopicInfo `json:"topics,omitempty"`
	StartTime    string      `json:"startTime,omitempty"`
	EndTime      string      `json:"endTime,omitempty"`
	Duration     float64     `json:"duration,omitempty"`
	MessageCount int64       `json:"messageCount,omitempty"`
}

// TopicInfo describes a topic in a recording.
type TopicInfo struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	MessageCount int64  `json:"messageCount"`
}

// InitMultipartUploadResponse is the response from initiating a multipart upload.
type InitMultipartUploadResponse struct {
	UploadID    string `json:"uploadId"`
	ChunkSize   int64  `json:"chunkSize"`
	TotalChunks int    `json:"totalChunks"`
}

// InitMultipartUpload starts a new multipart upload.
func (c *Client) InitMultipartUpload(ctx context.Context, req *InitMultipartUploadRequest) (*InitMultipartUploadResponse, error) {
	body, err := c.post(ctx, "/api/recordings/upload/multipart", req)
	if err != nil {
		return nil, err
	}

	// Response is wrapped in { data: ... }
	var wrapper struct {
		Data InitMultipartUploadResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse init upload response: %w", err)
	}

	return &wrapper.Data, nil
}

// UploadChunkResponse is the response from uploading a chunk.
type UploadChunkResponse struct {
	ETag  string `json:"etag"`
	Index int    `json:"index"`
}

// UploadChunk uploads a single chunk.
func (c *Client) UploadChunk(ctx context.Context, uploadID string, index int, data []byte) (*UploadChunkResponse, error) {
	url := fmt.Sprintf("%s/api/recordings/upload/multipart/%s/chunks/%d", c.baseURL, uploadID, index)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &Error{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       string(body),
		}
	}

	// Response is wrapped in { data: ... }
	var wrapper struct {
		Data UploadChunkResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse chunk upload response: %w", err)
	}

	return &wrapper.Data, nil
}

// CompleteMultipartUploadRequest is the request to complete a multipart upload.
type CompleteMultipartUploadRequest struct {
	Parts []UploadPart `json:"parts"`
}

// UploadPart represents a completed upload part.
type UploadPart struct {
	Index int    `json:"index"`
	ETag  string `json:"etag"`
}

// CompleteMultipartUploadResponse is the response from completing a multipart upload.
type CompleteMultipartUploadResponse struct {
	RecordingID string `json:"recordingId"`
	URL         string `json:"url"`
	Size        int64  `json:"size"`
	CreatedAt   string `json:"createdAt"`
}

// CompleteMultipartUpload completes a multipart upload.
func (c *Client) CompleteMultipartUpload(ctx context.Context, uploadID string, parts []UploadPart) (*CompleteMultipartUploadResponse, error) {
	req := CompleteMultipartUploadRequest{Parts: parts}
	body, err := c.post(ctx, fmt.Sprintf("/api/recordings/upload/multipart/%s/complete", uploadID), req)
	if err != nil {
		return nil, err
	}

	// Response is wrapped in { data: ... }
	var wrapper struct {
		Data CompleteMultipartUploadResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse complete upload response: %w", err)
	}

	return &wrapper.Data, nil
}

// UploadStatusResponse is the response from checking upload status.
type UploadStatusResponse struct {
	UploadID       string `json:"uploadId"`
	Filename       string `json:"filename"`
	Size           int64  `json:"size"`
	ChunkSize      int64  `json:"chunkSize"`
	TotalChunks    int    `json:"totalChunks"`
	UploadedChunks []int  `json:"uploadedChunks"`
	CreatedAt      string `json:"createdAt"`
	ExpiresAt      string `json:"expiresAt"`
}

// GetUploadStatus checks the status of an upload (for resuming).
func (c *Client) GetUploadStatus(ctx context.Context, uploadID string) (*UploadStatusResponse, error) {
	body, err := c.get(ctx, fmt.Sprintf("/api/recordings/upload/multipart/%s/status", uploadID))
	if err != nil {
		return nil, err
	}

	// Response is wrapped in { data: ... }
	var wrapper struct {
		Data UploadStatusResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse upload status response: %w", err)
	}

	return &wrapper.Data, nil
}

// AbortMultipartUpload aborts a multipart upload.
func (c *Client) AbortMultipartUpload(ctx context.Context, uploadID string) error {
	return c.delete(ctx, fmt.Sprintf("/api/recordings/upload/multipart/%s", uploadID))
}

// ============= Simple Upload =============

// SimpleUploadResponse is the response from simple upload.
type SimpleUploadResponse struct {
	RecordingID string `json:"recordingId"`
	URL         string `json:"url"`
	Size        int64  `json:"size"`
	CreatedAt   string `json:"createdAt"`
}

// SimpleUpload uploads a small file in a single request.
func (c *Client) SimpleUpload(ctx context.Context, filename string, data []byte, checksum string) (*SimpleUploadResponse, error) {
	url := fmt.Sprintf("%s/api/recordings/upload", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Filename", filename)
	if checksum != "" {
		req.Header.Set("X-Checksum", checksum)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, &Error{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       string(body),
		}
	}

	// Response is wrapped in { data: ... }
	var wrapper struct {
		Data SimpleUploadResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse simple upload response: %w", err)
	}

	return &wrapper.Data, nil
}

// ============= Helper methods =============

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	url := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &Error{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       string(body),
		}
	}

	return body, nil
}

func (c *Client) post(ctx context.Context, path string, payload interface{}) ([]byte, error) {
	return c.postWithToken(ctx, path, payload, c.token)
}

func (c *Client) postWithToken(ctx context.Context, path string, payload interface{}, token string) ([]byte, error) {
	url := c.baseURL + path

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, &Error{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       string(body),
		}
	}

	return body, nil
}

func (c *Client) delete(ctx context.Context, path string) error {
	url := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return &Error{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       string(body),
		}
	}

	return nil
}

// ============= Web-based Device Registration Flow =============

// SystemInfo contains system information collected for registration.
type SystemInfo struct {
	CPUCores      int     `json:"cpuCores,omitempty"`
	CPUModel      string  `json:"cpuModel,omitempty"`
	MemoryGB      float64 `json:"memoryGB,omitempty"`
	DiskGB        float64 `json:"diskGB,omitempty"`
	OSName        string  `json:"osName,omitempty"`
	KernelVersion string  `json:"kernelVersion,omitempty"`
}

// RegisterInitRequest is the request to initialize device registration.
type RegisterInitRequest struct {
	MachineID  string      `json:"machineId"`
	Hostname   string      `json:"hostname,omitempty"`
	Platform   string      `json:"platform,omitempty"`
	IPAddress  string      `json:"ipAddress,omitempty"`
	SystemInfo *SystemInfo `json:"systemInfo,omitempty"`
}

// RegisterInitResponse is the response from registration initialization.
type RegisterInitResponse struct {
	Code            string `json:"code"`
	RegistrationURL string `json:"registrationUrl"`
	ExpiresAt       string `json:"expiresAt"`
	ExpiresIn       int    `json:"expiresIn"` // seconds
}

// RegisterInit initializes device registration and returns a registration code.
// The user should visit the returned URL to complete registration.
func (c *Client) RegisterInit(ctx context.Context, req *RegisterInitRequest) (*RegisterInitResponse, error) {
	body, err := c.postNoAuth(ctx, "/api/device/register/init", req)
	if err != nil {
		return nil, err
	}

	var wrapper struct {
		Data RegisterInitResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse register init response: %w", err)
	}

	return &wrapper.Data, nil
}

// RegisterPollRequest is the request to poll registration status.
type RegisterPollRequest struct {
	Code string `json:"code"`
}

// RegisterPollResponseCompleted contains data when registration is completed.
type RegisterPollResponseCompleted struct {
	DeviceToken string `json:"deviceToken"`
	Device      struct {
		ID               string  `json:"id"`
		Name             string  `json:"name"`
		Type             string  `json:"type"`
		OrganizationID   *string `json:"organizationId"`
		OrganizationName *string `json:"organizationName"`
	} `json:"device"`
	Config struct {
		WatchPaths        []string `json:"watchPaths"`
		UploadChunkSize   int64    `json:"uploadChunkSize"`
		HeartbeatInterval int      `json:"heartbeatInterval"`
	} `json:"config"`
}

// RegisterPollResponse is the response from polling registration status.
type RegisterPollResponse struct {
	Status string `json:"status"` // "pending", "completed", "expired"
	// Only set when status is "completed"
	DeviceToken string `json:"deviceToken,omitempty"`
	Device      *struct {
		ID               string  `json:"id"`
		Name             string  `json:"name"`
		Type             string  `json:"type"`
		OrganizationID   *string `json:"organizationId"`
		OrganizationName *string `json:"organizationName"`
	} `json:"device,omitempty"`
	Config *struct {
		WatchPaths        []string `json:"watchPaths"`
		UploadChunkSize   int64    `json:"uploadChunkSize"`
		HeartbeatInterval int      `json:"heartbeatInterval"`
	} `json:"config,omitempty"`
}

// RegisterPoll polls for registration completion status.
func (c *Client) RegisterPoll(ctx context.Context, code string) (*RegisterPollResponse, error) {
	req := RegisterPollRequest{Code: code}
	body, err := c.postNoAuth(ctx, "/api/device/register/poll", req)
	if err != nil {
		return nil, err
	}

	var wrapper struct {
		Data RegisterPollResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse register poll response: %w", err)
	}

	return &wrapper.Data, nil
}

// postNoAuth sends a POST request without authentication.
func (c *Client) postNoAuth(ctx context.Context, path string, payload interface{}) ([]byte, error) {
	url := c.baseURL + path

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, &Error{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       string(body),
		}
	}

	return body, nil
}
