//go:build mcp

package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func newSchedulerRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	svc := &extendedMockService{
		schedulerSvc: &mockSchedulerService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(svc, log)
}

func TestSchedulerCreateJob_Success(t *testing.T) {
	registry := newSchedulerRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "minimal",
			args: map[string]interface{}{
				"name": "test-job", "handler": "backup", "type": "one-shot",
			},
		},
		{
			name: "with all options",
			args: map[string]interface{}{
				"name": "full-job", "handler": "cleanup", "type": "recurring",
				"description": "full test", "owner_type": "tenant",
				"schedule": "*/5 * * * *", "priority": float64(5),
				"max_retries":       float64(3),
				"execute_at":        time.Now().Add(time.Hour).Format(time.RFC3339),
				"delay":             "30s",
				"timeout":           "5m",
				"payload":           base64.StdEncoding.EncodeToString([]byte(`{"key":"value"}`)),
				"labels":            map[string]interface{}{"env": "test"},
				"owner_id":          "admin-001",
				"target_tenant_id":  "tenant-789",
				"idempotency_key":   "backup-2025-02-01",
				"dedup_key":         "backup-daily",
				"dedup_window_mins": float64(1440),
			},
		},
		{
			name: "non-string labels are dropped",
			args: map[string]interface{}{
				"name": "test-job", "handler": "test-handler", "type": "recurring",
				"labels": map[string]interface{}{"valid": "value", "invalid": float64(42), "bool": true},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_scheduler_create_job", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestSchedulerCreateJob_Errors(t *testing.T) {
	registry := newSchedulerRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{
			name:        "missing name",
			args:        map[string]interface{}{"handler": "backup", "type": "one-shot"},
			errContains: "name is required",
		},
		{
			name:        "missing handler",
			args:        map[string]interface{}{"name": "test", "type": "one-shot"},
			errContains: "handler is required",
		},
		{
			name:        "missing type",
			args:        map[string]interface{}{"name": "test", "handler": "backup"},
			errContains: "type is required",
		},
		{
			name: "invalid execute_at",
			args: map[string]interface{}{
				"name": "test", "handler": "backup", "type": "one-shot",
				"execute_at": "not-a-date",
			},
			errContains: "invalid execute_at format",
		},
		{
			name: "invalid delay",
			args: map[string]interface{}{
				"name": "test", "handler": "backup", "type": "delayed",
				"delay": "not-a-duration",
			},
			errContains: "invalid delay",
		},
		{
			name: "invalid timeout",
			args: map[string]interface{}{
				"name": "test", "handler": "backup", "type": "one-shot",
				"timeout": "bad-timeout",
			},
			errContains: "invalid timeout format",
		},
		{
			name: "invalid payload encoding",
			args: map[string]interface{}{
				"name": "test", "handler": "backup", "type": "one-shot",
				"payload": "not-base64!!!",
			},
			errContains: "invalid payload encoding",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_scheduler_create_job", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestSchedulerCreateJob_ServiceNotAvailable(t *testing.T) {
	svc := &extendedMockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_scheduler_create_job", map[string]interface{}{
		"name": "test", "handler": "backup", "type": "one-shot",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not available")
}

// TestSchedulerTools_Success checks that each scheduler tool taking a job_id,
// and the status tool, returns a result when the service succeeds.
func TestSchedulerTools_Success(t *testing.T) {
	registry := newSchedulerRegistry(t)
	ctx := context.Background()
	jobID := map[string]interface{}{"job_id": float64(1)}

	tests := []struct {
		tool string
		args map[string]interface{}
	}{
		{"quicbaas_scheduler_get_job", map[string]interface{}{"job_id": float64(42)}},
		{"quicbaas_scheduler_update_job", map[string]interface{}{
			"job_id": float64(1), "name": "updated-job", "schedule": "*/10 * * * *",
		}},
		{"quicbaas_scheduler_delete_job", jobID},
		{"quicbaas_scheduler_trigger_job", jobID},
		{"quicbaas_scheduler_pause_job", jobID},
		{"quicbaas_scheduler_resume_job", jobID},
		{"quicbaas_scheduler_cancel_job", jobID},
		{"quicbaas_scheduler_job_history", jobID},
		{"quicbaas_scheduler_status", map[string]interface{}{}},
	}

	for _, tc := range tests {
		t.Run(tc.tool, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tc.tool, tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

// TestSchedulerTools_MissingID checks that every job-scoped scheduler tool
// rejects a call without job_id before it reaches the scheduler service.
func TestSchedulerTools_MissingID(t *testing.T) {
	registry := newSchedulerRegistry(t)
	ctx := context.Background()

	tests := []struct {
		tool string
		args map[string]interface{}
	}{
		{"quicbaas_scheduler_get_job", map[string]interface{}{}},
		{"quicbaas_scheduler_update_job", map[string]interface{}{"name": "test"}},
		{"quicbaas_scheduler_delete_job", map[string]interface{}{}},
		{"quicbaas_scheduler_trigger_job", map[string]interface{}{}},
		{"quicbaas_scheduler_pause_job", map[string]interface{}{}},
		{"quicbaas_scheduler_resume_job", map[string]interface{}{}},
		{"quicbaas_scheduler_cancel_job", map[string]interface{}{}},
		{"quicbaas_scheduler_job_history", map[string]interface{}{}},
	}

	for _, tc := range tests {
		t.Run(tc.tool, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tc.tool, tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "job_id is required")
		})
	}
}

func TestSchedulerListJobs_Success(t *testing.T) {
	registry := newSchedulerRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{"no filters", map[string]interface{}{}},
		{"with status filter", map[string]interface{}{"status": "pending"}},
		{"with limit", map[string]interface{}{"limit": float64(10)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_scheduler_list_jobs", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestIsSchedulerTool(t *testing.T) {
	schedulerTools := []string{
		"quicbaas_scheduler_create_job", "quicbaas_scheduler_get_job",
		"quicbaas_scheduler_update_job", "quicbaas_scheduler_list_jobs",
		"quicbaas_scheduler_delete_job", "quicbaas_scheduler_trigger_job",
		"quicbaas_scheduler_pause_job", "quicbaas_scheduler_resume_job",
		"quicbaas_scheduler_cancel_job", "quicbaas_scheduler_job_history",
		"quicbaas_scheduler_status",
	}

	for _, name := range schedulerTools {
		t.Run(name+"_true", func(t *testing.T) {
			assert.True(t, isSchedulerTool(name))
		})
	}

	t.Run("non scheduler tool", func(t *testing.T) {
		assert.False(t, isSchedulerTool("qrdb_put"))
	})
}

func TestSchedulerToolDispatch_UnknownTool(t *testing.T) {
	registry := newSchedulerRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_scheduler_nonexistent", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
}

// =============================================================================
// Dynamic Secrets Tool Error Path Tests
// =============================================================================

func TestDynamicSecretsServiceNotConfigured(t *testing.T) {
	svc := &extendedMockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_dynamic_secrets_create_role", map[string]interface{}{
		"name": "test-role",
		"type": "database",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "dynamic secrets service not configured")
}

func TestIsDynamicSecretsTool_Exhaustive(t *testing.T) {
	validTools := []string{
		"quicbaas_dynamic_secrets_create_role",
		"quicbaas_dynamic_secrets_get_role",
		"quicbaas_dynamic_secrets_list_roles",
		"quicbaas_dynamic_secrets_delete_role",
		"quicbaas_dynamic_secrets_generate",
		"quicbaas_dynamic_secrets_renew_lease",
		"quicbaas_dynamic_secrets_revoke_lease",
		"quicbaas_dynamic_secrets_list_leases",
	}

	for _, tool := range validTools {
		assert.True(t, isDynamicSecretsTool(tool), "expected %s to be dynamic secrets tool", tool)
	}

	invalidTools := []string{
		"quicbaas_dynamic_secrets_unknown",
		"quicbaas_kms_encrypt",
		"qrdb_put",
		"",
		"quicbaas_dynamic_secrets",
	}

	for _, tool := range invalidTools {
		assert.False(t, isDynamicSecretsTool(tool), "expected %s to NOT be dynamic secrets tool", tool)
	}
}

// =============================================================================
// Certificate Tool Error Path Tests
// =============================================================================

func TestCertificateServiceNotConfigured(t *testing.T) {
	svc := &extendedMockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_certificate_issue", map[string]interface{}{
		"common_name": "test.example.com",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "certificate service not configured")
}

func TestIsCertificateTool_Exhaustive(t *testing.T) {
	validTools := []string{
		"quicbaas_certificate_issue",
		"quicbaas_certificate_sign_csr",
		"quicbaas_certificate_revoke",
		"quicbaas_certificate_verify",
		"quicbaas_certificate_get_ca",
		"quicbaas_certificate_get_tls_config",
	}

	for _, tool := range validTools {
		assert.True(t, isCertificateTool(tool), "expected %s to be certificate tool", tool)
	}

	invalidTools := []string{
		"quicbaas_certificate_unknown",
		"quicbaas_kms_encrypt",
		"qrdb_put",
		"",
		"quicbaas_certificate",
	}

	for _, tool := range invalidTools {
		assert.False(t, isCertificateTool(tool), "expected %s to NOT be certificate tool", tool)
	}
}

// =============================================================================
// Scheduler Tool Service Error Path Tests
// =============================================================================

// TestSchedulerTools_ServiceError checks that every scheduler tool returns the
// scheduler service's error to the caller with no result.
func TestSchedulerTools_ServiceError(t *testing.T) {
	ctx := context.Background()
	jobID := map[string]interface{}{"job_id": float64(123)}

	tests := []struct {
		tool string
		args map[string]interface{}
		fail func(m *mockSchedulerService, err error)
	}{
		{
			tool: "quicbaas_scheduler_create_job",
			args: map[string]interface{}{"name": "test-job", "type": "one-shot", "handler": "test-handler"},
			fail: func(m *mockSchedulerService, err error) {
				m.createJobFunc = func(context.Context, *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
					return nil, err
				}
			},
		},
		{
			tool: "quicbaas_scheduler_get_job",
			args: jobID,
			fail: func(m *mockSchedulerService, err error) {
				m.getJobFunc = func(context.Context, *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
					return nil, err
				}
			},
		},
		{
			tool: "quicbaas_scheduler_update_job",
			args: map[string]interface{}{"job_id": float64(123), "name": "updated-job"},
			fail: func(m *mockSchedulerService, err error) {
				m.updateJobFunc = func(context.Context, *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
					return nil, err
				}
			},
		},
		{
			tool: "quicbaas_scheduler_delete_job",
			args: jobID,
			fail: func(m *mockSchedulerService, err error) {
				m.deleteJobFunc = func(context.Context, *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
					return nil, err
				}
			},
		},
		{
			tool: "quicbaas_scheduler_list_jobs",
			args: map[string]interface{}{},
			fail: func(m *mockSchedulerService, err error) {
				m.listJobsFunc = func(context.Context, *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
					return nil, err
				}
			},
		},
		{
			tool: "quicbaas_scheduler_trigger_job",
			args: jobID,
			fail: func(m *mockSchedulerService, err error) {
				m.triggerJobFunc = func(context.Context, *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
					return nil, err
				}
			},
		},
		{
			tool: "quicbaas_scheduler_pause_job",
			args: jobID,
			fail: func(m *mockSchedulerService, err error) {
				m.pauseJobFunc = func(context.Context, *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
					return nil, err
				}
			},
		},
		{
			tool: "quicbaas_scheduler_resume_job",
			args: jobID,
			fail: func(m *mockSchedulerService, err error) {
				m.resumeJobFunc = func(context.Context, *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
					return nil, err
				}
			},
		},
		{
			tool: "quicbaas_scheduler_cancel_job",
			args: jobID,
			fail: func(m *mockSchedulerService, err error) {
				m.cancelJobFunc = func(context.Context, *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
					return nil, err
				}
			},
		},
		{
			tool: "quicbaas_scheduler_job_history",
			args: jobID,
			fail: func(m *mockSchedulerService, err error) {
				m.historyFunc = func(context.Context, *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
					return nil, err
				}
			},
		},
		{
			tool: "quicbaas_scheduler_status",
			args: map[string]interface{}{},
			fail: func(m *mockSchedulerService, err error) {
				m.statusFunc = func(context.Context) (*service.GetSchedulerStatusResponse, error) {
					return nil, err
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.tool, func(t *testing.T) {
			scheduler := &mockSchedulerService{}
			tc.fail(scheduler, errors.New(tc.tool+" failed"))
			registry := NewToolRegistry(&extendedMockService{schedulerSvc: scheduler}, newTestLogger(t))

			result, err := registry.ExecuteTool(ctx, tc.tool, tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.tool+" failed")
		})
	}
}

func TestExecuteSchedulerListJobs_WithPagination(t *testing.T) {
	svc := &extendedMockService{
		schedulerSvc: &mockSchedulerService{
			listJobsFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
				return &service.ListSchedulerJobsResponse{
					Jobs: []*service.SchedulerJob{
						{
							ID:        1,
							Name:      "test-job",
							Type:      service.SchedulerJobTypeRecurring,
							Schedule:  "*/5 * * * *",
							Status:    service.SchedulerJobStatusPending,
							CreatedAt: time.Now(),
						},
					},
					HasMore:    true,
					NextCursor: "cursor-123",
					TotalCount: 10,
					Timestamp:  time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_scheduler_list_jobs", map[string]interface{}{
		"cursor": "prev-cursor",
		"limit":  float64(10),
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	assert.Equal(t, true, resultMap["has_more"])
	assert.Equal(t, "cursor-123", resultMap["next_cursor"])
}

// =============================================================================
// Scheduler Tool Validation Tests
// =============================================================================

// =============================================================================
// Scheduler Update Job Tests (coverage at 40.6%)
// =============================================================================

func TestExecuteSchedulerUpdateJob_AllParameters(t *testing.T) {
	svc := &extendedMockService{
		schedulerSvc: &mockSchedulerService{
			updateJobFunc: func(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
				return &service.UpdateSchedulerJobResponse{
					Job: &service.SchedulerJob{
						ID:          req.JobID,
						Name:        req.Name,
						Description: req.Description,
						Handler:     req.Handler,
						Schedule:    req.Schedule,
						Priority:    req.Priority,
						Status:      service.SchedulerJobStatusPending,
						UpdatedAt:   time.Now(),
					},
					Timestamp: time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	// Encode a test payload
	testPayload := base64.StdEncoding.EncodeToString([]byte(`{"action":"test"}`))

	result, err := registry.ExecuteTool(ctx, "quicbaas_scheduler_update_job", map[string]interface{}{
		"job_id":      float64(123),
		"name":        "updated-job",
		"description": "Updated description",
		"handler":     "new-handler",
		"schedule":    "0 * * * *",
		"priority":    float64(5),
		"max_retries": float64(3),
		"execute_at":  time.Now().Add(time.Hour).Format(time.RFC3339),
		"timeout":     "30m",
		"payload":     testPayload,
		"tenant_id":   "tenant-456",
		"labels": map[string]interface{}{
			"env":     "production",
			"team":    "backend",
			"version": "1.0",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteSchedulerUpdateJob_InvalidExecuteAt(t *testing.T) {
	svc := &extendedMockService{
		schedulerSvc: &mockSchedulerService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_scheduler_update_job", map[string]interface{}{
		"job_id":     float64(123),
		"execute_at": "invalid-timestamp",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid execute_at format")
}

func TestExecuteSchedulerUpdateJob_InvalidTimeout(t *testing.T) {
	svc := &extendedMockService{
		schedulerSvc: &mockSchedulerService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_scheduler_update_job", map[string]interface{}{
		"job_id":  float64(123),
		"timeout": "not-a-duration",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid timeout format")
}

func TestExecuteSchedulerUpdateJob_InvalidPayloadEncoding(t *testing.T) {
	svc := &extendedMockService{
		schedulerSvc: &mockSchedulerService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_scheduler_update_job", map[string]interface{}{
		"job_id":  float64(123),
		"payload": "not-valid-base64!!!",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid payload encoding")
}

func TestExecuteSchedulerUpdateJob_WithLabelsNonString(t *testing.T) {
	svc := &extendedMockService{
		schedulerSvc: &mockSchedulerService{
			updateJobFunc: func(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
				return &service.UpdateSchedulerJobResponse{
					Job: &service.SchedulerJob{
						ID:        req.JobID,
						Name:      req.Name,
						Status:    service.SchedulerJobStatusPending,
						UpdatedAt: time.Now(),
					},
					Timestamp: time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	// Labels with non-string values (should be ignored)
	result, err := registry.ExecuteTool(ctx, "quicbaas_scheduler_update_job", map[string]interface{}{
		"job_id": float64(123),
		"name":   "test-job",
		"labels": map[string]interface{}{
			"valid":   "label-value",
			"invalid": float64(123), // Non-string, should be skipped
			"another": true,         // Non-string, should be skipped
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// Scheduler List Jobs Tests (coverage at 71.4%)
// =============================================================================

func TestExecuteSchedulerListJobs_AllFilters(t *testing.T) {
	svc := &extendedMockService{
		schedulerSvc: &mockSchedulerService{
			listJobsFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
				return &service.ListSchedulerJobsResponse{
					Jobs: []*service.SchedulerJob{
						{
							ID:        1,
							Name:      "job-1",
							Handler:   "handler-1",
							Status:    service.SchedulerJobStatusPending,
							CreatedAt: time.Now(),
						},
						{
							ID:        2,
							Name:      "job-2",
							Handler:   "handler-1",
							Status:    service.SchedulerJobStatusRunning,
							CreatedAt: time.Now(),
						},
					},
					HasMore:    true,
					NextCursor: "cursor-xyz",
					TotalCount: 50,
					Timestamp:  time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_scheduler_list_jobs", map[string]interface{}{
		"owner_id":         "owner-123",
		"target_tenant_id": "tenant-456",
		"handler":          "backup-handler",
		"status":           "pending",
		"limit":            float64(50),
		"cursor":           "prev-cursor",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["has_more"])
	assert.Equal(t, "cursor-xyz", resultMap["next_cursor"])
}

func TestExecuteSchedulerListJobs_DefaultLimit(t *testing.T) {
	svc := &extendedMockService{
		schedulerSvc: &mockSchedulerService{
			listJobsFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
				// Default limit should be 100
				return &service.ListSchedulerJobsResponse{
					Jobs:       []*service.SchedulerJob{},
					HasMore:    false,
					TotalCount: 0,
					Timestamp:  time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_scheduler_list_jobs", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteSchedulerListJobs_WithOwnerTypeAndLabels(t *testing.T) {
	svc := &extendedMockService{
		schedulerSvc: &mockSchedulerService{
			listJobsFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
				return &service.ListSchedulerJobsResponse{
					Jobs:       []*service.SchedulerJob{},
					HasMore:    false,
					TotalCount: 0,
					Timestamp:  time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_scheduler_list_jobs", map[string]interface{}{
		"owner_type": "system",
		"labels": map[string]interface{}{
			"env":     "production",
			"team":    "backend",
			"invalid": float64(123), // Non-string values should be skipped
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// Scheduler Create Job Tests (additional coverage)
// =============================================================================

// =============================================================================
// Helper Function Tests (additional coverage)
// =============================================================================

func TestGetInt_AllBranches(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		key      string
		expected int
	}{
		{"float64 value", map[string]interface{}{"key": float64(100)}, "key", 100},
		{"missing key", map[string]interface{}{}, "key", 0},
		{"zero value", map[string]interface{}{"key": float64(0)}, "key", 0},
		{"large value", map[string]interface{}{"key": float64(999999)}, "key", 999999},
		{"negative value", map[string]interface{}{"key": float64(-10)}, "key", -10},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := getInt(tc.args, tc.key)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// Object Admin SetBackendStatus Tests - coverage for enabled param
// =============================================================================

func TestObjectAdminSetBackendStatus_MissingEnabled(t *testing.T) {
	adminSvc := &mockObjectStorageAdminService{}

	svc := &objStorageAdminMockService{
		objAdminSvc: adminSvc,
	}

	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_set_backend_status", map[string]interface{}{
		"name": "test-backend",
		// "enabled" is missing
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "enabled is required")
}

func TestObjectAdminSetBackendStatus_ServiceError(t *testing.T) {
	expectedErr := errors.New("backend status change failed")
	adminSvc := &mockObjectStorageAdminService{
		setBackendStatusFunc: func(ctx context.Context, req *service.SetBackendStatusRequest) (*service.SetBackendStatusResponse, error) {
			return nil, expectedErr
		},
	}

	svc := &objStorageAdminMockService{
		objAdminSvc: adminSvc,
	}

	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_set_backend_status", map[string]interface{}{
		"name":    "test-backend",
		"enabled": false,
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "backend status change failed")
}
