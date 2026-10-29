// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration

package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// Note: Scheduler gRPC client methods are not yet implemented.
// Tests use REST for scheduler operations and gRPC for KMS operations.

// =============================================================================
// Endpoint Helpers
// =============================================================================

// getGRPCEndpoints returns all gRPC endpoints for testing.
func getGRPCEndpoints() []string {
	node1 := os.Getenv("QUICBAAS_NODE1_GRPC")
	if node1 == "" {
		node1 = os.Getenv("QUICBAAS_NODE1")
	}
	if node1 == "" {
		node1 = "localhost:50051"
	}
	return []string{node1}
}

// getRESTEndpoints returns all REST API endpoints for testing.
func getRESTEndpoints() []string {
	node1 := os.Getenv("QUICBAAS_NODE1_URL")
	if node1 == "" {
		node1 = "https://test-node1:8080"
	}
	return []string{node1}
}

// =============================================================================
// Service Availability Helpers
// =============================================================================

// requireKMSAvailable checks if the KMS service is available and fails the test if not.
func requireKMSAvailable(t *testing.T, client *grpcclient.Client, ctx context.Context) {
	t.Helper()
	req := &service.ListKeysRequest{
		Limit: 1,
	}
	_, err := client.KMSListKeys(ctx, req)
	if err != nil && (strings.Contains(err.Error(), "Unimplemented") ||
		strings.Contains(err.Error(), "unknown service")) {
		require.NoError(t, err, "KMS service must be enabled on the server")
	}
}

// =============================================================================
// Retry Helpers
// =============================================================================

// RetryConfig configures retry behavior for operations.
type RetryConfig struct {
	MaxAttempts int
	InitialWait time.Duration
	MaxWait     time.Duration
	Multiplier  float64
}

// DefaultRetryConfig returns sensible defaults for scheduler operations.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 30,
		InitialWait: 500 * time.Millisecond,
		MaxWait:     5 * time.Second,
		Multiplier:  1.2,
	}
}

// RetryOperation executes an operation with exponential backoff retry.
func RetryOperation(ctx context.Context, cfg RetryConfig, operation func() error) error {
	var lastErr error
	wait := cfg.InitialWait

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		lastErr = operation()
		if lastErr == nil {
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if attempt < cfg.MaxAttempts {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}

			wait = time.Duration(float64(wait) * cfg.Multiplier)
			if wait > cfg.MaxWait {
				wait = cfg.MaxWait
			}
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", cfg.MaxAttempts, lastErr)
}

// =============================================================================
// Test Data Helpers
// =============================================================================

// generateUniqueJobName generates a unique job name with a prefix.
func generateUniqueJobName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// generateUniqueKeyName generates a unique key name with a prefix.
func generateUniqueKeyName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// =============================================================================
// Scheduler REST Client
// =============================================================================

// SchedulerRESTClient wraps HTTP client for scheduler REST API testing.
type SchedulerRESTClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewSchedulerRESTClient creates a new scheduler REST client that acts as the
// tenant operator: go-qrdb grants the scheduler:* job verbs to the built-in
// Operator role and to no other built-in role.
func NewSchedulerRESTClient(t *testing.T, baseURL string) *SchedulerRESTClient {
	t.Helper()
	return &SchedulerRESTClient{
		baseURL:    baseURL,
		httpClient: helpers.NewTenantOperatorHTTPClient(t, 30*time.Second),
	}
}

// RESTCreateJobRequest represents the REST API request for creating a job.
type RESTCreateJobRequest struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	OwnerType   string            `json:"owner_type,omitempty"`
	OwnerID     string            `json:"owner_id,omitempty"`
	Type        string            `json:"type"`
	Handler     string            `json:"handler"`
	Schedule    string            `json:"schedule,omitempty"`
	ExecuteAt   string            `json:"execute_at,omitempty"` // RFC3339 format string
	Delay       int64             `json:"delay_seconds,omitempty"`
	Payload     json.RawMessage   `json:"payload,omitempty"`
	Priority    int32             `json:"priority,omitempty"`
	Timeout     int64             `json:"timeout_seconds,omitempty"`
	MaxRetries  int32             `json:"max_retries,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// RESTCancelJobRequest represents the REST API request for canceling a job.
type RESTCancelJobRequest struct {
	Reason string `json:"reason,omitempty"`
}

// RESTJobResponse represents the REST API response for a job.
type RESTJobResponse struct {
	ID          uint64            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	OwnerType   string            `json:"owner_type,omitempty"`
	OwnerID     string            `json:"owner_id,omitempty"`
	Type        string            `json:"type"`
	Handler     string            `json:"handler"`
	Status      string            `json:"status"`
	Schedule    string            `json:"schedule,omitempty"`
	ExecuteAt   time.Time         `json:"execute_at,omitempty"`
	Delay       time.Duration     `json:"delay,omitempty"`
	NextRun     time.Time         `json:"next_run,omitempty"`
	LastRun     time.Time         `json:"last_run,omitempty"`
	RunCount    uint64            `json:"run_count,omitempty"`
	FailCount   uint64            `json:"fail_count,omitempty"`
	LastError   string            `json:"last_error,omitempty"`
	Payload     json.RawMessage   `json:"payload,omitempty"`
	Priority    int32             `json:"priority,omitempty"`
	Timeout     time.Duration     `json:"timeout,omitempty"`
	MaxRetries  int32             `json:"max_retries,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at,omitempty"`
}

// RESTCreateJobResponse represents the wrapped response for job creation.
type RESTCreateJobResponse struct {
	Job       *RESTJobResponse `json:"job"`
	Timestamp time.Time        `json:"timestamp"`
}

// RESTGetJobResponse represents the wrapped response for getting a job.
type RESTGetJobResponse struct {
	Job       *RESTJobResponse `json:"job"`
	Timestamp time.Time        `json:"timestamp"`
}

// RESTListJobsResponse represents the REST API response for listing jobs.
type RESTListJobsResponse struct {
	Jobs       []*RESTJobResponse `json:"jobs"`
	NextCursor string             `json:"next_cursor,omitempty"`
	HasMore    bool               `json:"has_more"`
	TotalCount int                `json:"total_count,omitempty"`
	Timestamp  time.Time          `json:"timestamp"`
}

// RESTSchedulerStatsResponse represents scheduler statistics in REST response.
type RESTSchedulerStatsResponse struct {
	JobCount     uint64    `json:"job_count"`
	RunningJobs  uint64    `json:"running_jobs"`
	SuccessCount uint64    `json:"success_count"`
	FailureCount uint64    `json:"failure_count"`
	LastTickTime time.Time `json:"last_tick_time,omitempty"`
	StartedAt    time.Time `json:"started_at"`
}

// RESTSchedulerStatusResponse represents the scheduler status response.
type RESTSchedulerStatusResponse struct {
	Running   bool                        `json:"running"`
	Stats     *RESTSchedulerStatsResponse `json:"stats,omitempty"`
	Timestamp time.Time                   `json:"timestamp"`
}

// RESTPauseJobResponse represents the REST response for pausing a job.
type RESTPauseJobResponse struct {
	JobID     uint64    `json:"job_id"`
	OldStatus string    `json:"old_status"`
	NewStatus string    `json:"new_status"`
	Timestamp time.Time `json:"timestamp"`
}

// RESTResumeJobResponse represents the REST response for resuming a job.
type RESTResumeJobResponse struct {
	JobID     uint64    `json:"job_id"`
	OldStatus string    `json:"old_status"`
	NewStatus string    `json:"new_status"`
	Timestamp time.Time `json:"timestamp"`
}

// RESTCancelJobResponse represents the REST response for canceling a job.
type RESTCancelJobResponse struct {
	JobID     uint64    `json:"job_id"`
	OldStatus string    `json:"old_status"`
	NewStatus string    `json:"new_status"`
	Timestamp time.Time `json:"timestamp"`
}

// CreateJob creates a job via REST API.
func (c *SchedulerRESTClient) CreateJob(ctx context.Context, req *RESTCreateJobRequest) (*RESTJobResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/scheduler/jobs", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("create job failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result RESTCreateJobResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w (body: %s)", err, string(respBody))
	}

	if result.Job == nil {
		return nil, fmt.Errorf("create job response missing 'job' field (body: %s)", string(respBody))
	}

	return result.Job, nil
}

// GetJob retrieves a job via REST API.
func (c *SchedulerRESTClient) GetJob(ctx context.Context, jobID uint64) (*RESTJobResponse, error) {
	url := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d", c.baseURL, jobID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get job %d failed with status %d: %s", jobID, resp.StatusCode, string(respBody))
	}

	var result RESTGetJobResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w (body: %s)", err, string(respBody))
	}

	if result.Job == nil {
		return nil, fmt.Errorf("get job response missing 'job' field (body: %s)", string(respBody))
	}

	return result.Job, nil
}

// ListJobs lists jobs via REST API.
func (c *SchedulerRESTClient) ListJobs(ctx context.Context, limit int, cursor string) (*RESTListJobsResponse, error) {
	url := fmt.Sprintf("%s/api/v1/scheduler/jobs", c.baseURL)

	if limit > 0 || cursor != "" {
		url += "?"
		params := []string{}
		if limit > 0 {
			params = append(params, fmt.Sprintf("limit=%d", limit))
		}
		if cursor != "" {
			params = append(params, fmt.Sprintf("cursor=%s", cursor))
		}
		url += strings.Join(params, "&")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list jobs failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result RESTListJobsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w (body: %s)", err, string(respBody))
	}

	return &result, nil
}

// DeleteJob deletes a job via REST API.
func (c *SchedulerRESTClient) DeleteJob(ctx context.Context, jobID uint64) error {
	url := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d", c.baseURL, jobID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete job %d failed with status %d: %s", jobID, resp.StatusCode, string(respBody))
	}

	return nil
}

// TriggerJob triggers immediate execution of a job via REST API.
func (c *SchedulerRESTClient) TriggerJob(ctx context.Context, jobID uint64) error {
	url := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d/trigger", c.baseURL, jobID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("trigger job %d failed with status %d: %s", jobID, resp.StatusCode, string(respBody))
	}

	return nil
}

// PauseJob pauses a job via REST API.
func (c *SchedulerRESTClient) PauseJob(ctx context.Context, jobID uint64) (*RESTPauseJobResponse, error) {
	url := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d/pause", c.baseURL, jobID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pause job %d failed with status %d: %s", jobID, resp.StatusCode, string(respBody))
	}

	var result RESTPauseJobResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w (body: %s)", err, string(respBody))
	}

	return &result, nil
}

// ResumeJob resumes a paused job via REST API.
func (c *SchedulerRESTClient) ResumeJob(ctx context.Context, jobID uint64) (*RESTResumeJobResponse, error) {
	url := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d/resume", c.baseURL, jobID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("resume job %d failed with status %d: %s", jobID, resp.StatusCode, string(respBody))
	}

	var result RESTResumeJobResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w (body: %s)", err, string(respBody))
	}

	return &result, nil
}

// CancelJob cancels a job via REST API.
func (c *SchedulerRESTClient) CancelJob(ctx context.Context, jobID uint64, reason string) (*RESTCancelJobResponse, error) {
	url := fmt.Sprintf("%s/api/v1/scheduler/jobs/%d/cancel", c.baseURL, jobID)

	var body io.Reader
	if reason != "" {
		bodyBytes, err := json.Marshal(&RESTCancelJobRequest{Reason: reason})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal cancel request: %w", err)
		}
		body = bytes.NewReader(bodyBytes)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	if reason != "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cancel job %d failed with status %d: %s", jobID, resp.StatusCode, string(respBody))
	}

	var result RESTCancelJobResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w (body: %s)", err, string(respBody))
	}

	return &result, nil
}

// GetStatus retrieves scheduler status via REST API.
func (c *SchedulerRESTClient) GetStatus(ctx context.Context) (*RESTSchedulerStatusResponse, error) {
	url := fmt.Sprintf("%s/api/v1/scheduler/status", c.baseURL)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get status failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result RESTSchedulerStatusResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w (body: %s)", err, string(respBody))
	}

	return &result, nil
}

// =============================================================================
// Job Waiting Helpers
// =============================================================================

// WaitForJobStatus waits for a job to reach the specified status.
func WaitForJobStatus(ctx context.Context, client *SchedulerRESTClient, jobID uint64, targetStatus string, cfg RetryConfig) (*RESTJobResponse, error) {
	if jobID == 0 {
		return nil, fmt.Errorf("invalid job ID: 0")
	}

	var lastJob *RESTJobResponse
	err := RetryOperation(ctx, cfg, func() error {
		job, err := client.GetJob(ctx, jobID)
		if err != nil {
			return err
		}
		lastJob = job
		if job.Status != targetStatus {
			return fmt.Errorf("job %d status is %s, waiting for %s", jobID, job.Status, targetStatus)
		}
		return nil
	})
	return lastJob, err
}

// WaitForJobCompletion waits for a job to complete (either successfully or with failure).
func WaitForJobCompletion(ctx context.Context, client *SchedulerRESTClient, jobID uint64, cfg RetryConfig) (*RESTJobResponse, error) {
	if jobID == 0 {
		return nil, fmt.Errorf("invalid job ID: 0")
	}

	var lastJob *RESTJobResponse
	err := RetryOperation(ctx, cfg, func() error {
		job, err := client.GetJob(ctx, jobID)
		if err != nil {
			return err
		}
		lastJob = job
		if job.Status != string(scheduler.JobStatusCompleted) && job.Status != string(scheduler.JobStatusFailed) {
			return fmt.Errorf("job %d status is %s, waiting for completion", jobID, job.Status)
		}
		return nil
	})
	return lastJob, err
}

// =============================================================================
// Cleanup Helpers
// =============================================================================

// cleanupJob attempts to delete a test job.
func cleanupJob(t *testing.T, client *SchedulerRESTClient, ctx context.Context, jobID uint64) {
	t.Helper()
	_ = client.DeleteJob(ctx, jobID)
}

// cleanupKey attempts to delete a test key via gRPC.
func cleanupKey(t *testing.T, client *grpcclient.Client, ctx context.Context, keyID string) {
	t.Helper()
	req := &service.DeleteKeyRequest{
		KeyID: keyID,
		Force: true,
	}
	_, _ = client.KMSDeleteKey(ctx, req)
}

// =============================================================================
// Assertion Helpers
// =============================================================================

// assertJobExists verifies a job exists.
func assertJobExists(t *testing.T, client *SchedulerRESTClient, ctx context.Context, jobID uint64) {
	t.Helper()
	job, err := client.GetJob(ctx, jobID)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.Equal(t, jobID, job.ID)
}

// assertKeyVersionStatus verifies a key version has the expected status.
func assertKeyVersionStatus(t *testing.T, client *grpcclient.Client, ctx context.Context, keyID string, version uint64, expectedStatus service.KeyStatus) {
	t.Helper()
	req := &service.ListKeyVersionsRequest{
		KeyID: keyID,
	}
	resp, err := client.KMSListKeyVersions(ctx, req)
	require.NoError(t, err)

	var found bool
	for _, v := range resp.Versions {
		if v.Version == version {
			require.Equal(t, expectedStatus, v.Status, "key %s version %d has unexpected status", keyID, version)
			found = true
			break
		}
	}
	require.True(t, found, "key %s version %d not found", keyID, version)
}

// waitForKeyVersionStatus polls ListKeyVersions with retries until the expected status is visible.
// In a multi-node Raft cluster, version status propagation may have a brief delay after
// the grace period job completes.
func waitForKeyVersionStatus(t *testing.T, client *grpcclient.Client, ctx context.Context, keyID string, version uint64, expectedStatus service.KeyStatus) {
	t.Helper()
	cfg := RetryConfig{
		MaxAttempts: 15,
		InitialWait: 500 * time.Millisecond,
		MaxWait:     2 * time.Second,
		Multiplier:  1.3,
	}

	req := &service.ListKeyVersionsRequest{KeyID: keyID}
	err := RetryOperation(ctx, cfg, func() error {
		resp, listErr := client.KMSListKeyVersions(ctx, req)
		if listErr != nil {
			return listErr
		}
		for _, v := range resp.Versions {
			if v.Version == version {
				if v.Status != expectedStatus {
					return fmt.Errorf("key %s version %d status is %s, waiting for %s", keyID, version, v.Status, expectedStatus)
				}
				return nil
			}
		}
		return fmt.Errorf("key %s version %d not found", keyID, version)
	})
	require.NoError(t, err, "key %s version %d should have status %s", keyID, version, expectedStatus)
}
