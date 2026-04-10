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

package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// convertProtoJob Tests
// =============================================================================

func TestConvertProtoJob_AllCases(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		result := convertProtoJob(nil)
		assert.Nil(t, result)
	})

	t.Run("valid_job_with_all_fields", func(t *testing.T) {
		pj := &pb.Job{
			Id:             123,
			Name:           "test-job",
			Description:    "A test job",
			OwnerType:      pb.OwnerType_OWNER_TYPE_TENANT,
			OwnerId:        "tenant-456",
			TargetTenantId: "target-789",
			CreatedBy:      "admin",
			Type:           pb.JobType_JOB_TYPE_RECURRING,
			Schedule:       "0 * * * *",
			Handler:        "backup-handler",
			Payload:        []byte(`{"key":"value"}`),
			Priority:       10,
			TimeoutSeconds: 300,
			MaxRetries:     3,
			Status:         pb.JobStatus_JOB_STATUS_RUNNING,
			RunCount:       5,
			FailCount:      1,
			LastError:      "previous error",
			Labels:         map[string]string{"env": "test"},
			CreatedAt:      1700000000,
			UpdatedAt:      1700001000,
			ExecuteAt:      1700002000,
			DelaySeconds:   60,
			LastRun:        1700000500,
			NextRun:        1700003600,
		}

		result := convertProtoJob(pj)

		require.NotNil(t, result)
		assert.Equal(t, uint64(123), result.ID)
		assert.Equal(t, "test-job", result.Name)
		assert.Equal(t, "A test job", result.Description)
		assert.Equal(t, service.SchedulerOwnerTypeTenant, result.OwnerType)
		assert.Equal(t, "tenant-456", result.OwnerID)
		assert.Equal(t, "target-789", result.TargetTenantID)
		assert.Equal(t, "admin", result.CreatedBy)
		assert.Equal(t, service.SchedulerJobTypeRecurring, result.Type)
		assert.Equal(t, "0 * * * *", result.Schedule)
		assert.Equal(t, "backup-handler", result.Handler)
		assert.Equal(t, []byte(`{"key":"value"}`), result.Payload)
		assert.Equal(t, int32(10), result.Priority)
		assert.Equal(t, int32(3), result.MaxRetries)
		assert.Equal(t, service.SchedulerJobStatusRunning, result.Status)
		assert.Equal(t, uint64(5), result.RunCount)
		assert.Equal(t, uint64(1), result.FailCount)
		assert.Equal(t, "previous error", result.LastError)
		assert.Equal(t, "test", result.Labels["env"])
		assert.False(t, result.ExecuteAt.IsZero())
		assert.False(t, result.LastRun.IsZero())
		assert.False(t, result.NextRun.IsZero())
	})

	t.Run("valid_job_without_optional_fields", func(t *testing.T) {
		pj := &pb.Job{
			Id:           456,
			Name:         "minimal-job",
			Type:         pb.JobType_JOB_TYPE_ONE_SHOT,
			Status:       pb.JobStatus_JOB_STATUS_PENDING,
			CreatedAt:    1700000000,
			UpdatedAt:    1700000000,
			ExecuteAt:    0, // No execute time
			DelaySeconds: 0, // No delay
			LastRun:      0, // Never run
			NextRun:      0, // No next run
		}

		result := convertProtoJob(pj)

		require.NotNil(t, result)
		assert.Equal(t, uint64(456), result.ID)
		assert.True(t, result.ExecuteAt.IsZero())
		assert.Zero(t, result.Delay)
		assert.True(t, result.LastRun.IsZero())
		assert.True(t, result.NextRun.IsZero())
	})
}

// =============================================================================
// convertProtoJobExecution Tests
// =============================================================================

func TestConvertProtoJobExecution_AllCases(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		result := convertProtoJobExecution(nil)
		assert.Nil(t, result)
	})

	t.Run("valid_execution_with_ended_at", func(t *testing.T) {
		pe := &pb.JobExecution{
			Id:         123,
			JobId:      456,
			Status:     pb.JobStatus_JOB_STATUS_COMPLETED,
			StartedAt:  1700000000,
			EndedAt:    1700000060,
			DurationMs: 60000,
			Error:      "",
			Output:     "success output",
		}

		result := convertProtoJobExecution(pe)

		require.NotNil(t, result)
		assert.Equal(t, uint64(123), result.ID)
		assert.Equal(t, uint64(456), result.JobID)
		assert.Equal(t, service.SchedulerJobStatusCompleted, result.Status)
		assert.False(t, result.StartedAt.IsZero())
		assert.False(t, result.EndedAt.IsZero())
		assert.Equal(t, "success output", result.Output)
	})

	t.Run("valid_execution_without_ended_at", func(t *testing.T) {
		pe := &pb.JobExecution{
			Id:         789,
			JobId:      456,
			Status:     pb.JobStatus_JOB_STATUS_RUNNING,
			StartedAt:  1700000000,
			EndedAt:    0, // Still running
			DurationMs: 5000,
			Error:      "",
		}

		result := convertProtoJobExecution(pe)

		require.NotNil(t, result)
		assert.Equal(t, uint64(789), result.ID)
		assert.True(t, result.EndedAt.IsZero())
	})

	t.Run("failed_execution_with_error", func(t *testing.T) {
		pe := &pb.JobExecution{
			Id:         999,
			JobId:      100,
			Status:     pb.JobStatus_JOB_STATUS_FAILED,
			StartedAt:  1700000000,
			EndedAt:    1700000010,
			DurationMs: 10000,
			Error:      "connection timeout",
			Output:     "partial output",
		}

		result := convertProtoJobExecution(pe)

		require.NotNil(t, result)
		assert.Equal(t, service.SchedulerJobStatusFailed, result.Status)
		assert.Equal(t, "connection timeout", result.Error)
	})
}

// =============================================================================
// convertProtoSchedulerStats Tests
// =============================================================================

func TestConvertProtoSchedulerStats_AllCases(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		result := convertProtoSchedulerStats(nil)
		assert.Nil(t, result)
	})

	t.Run("valid_stats_with_last_tick", func(t *testing.T) {
		ps := &pb.SchedulerStats{
			JobCount:     100,
			RunningJobs:  5,
			SuccessCount: 1000,
			FailureCount: 50,
			StartedAt:    1700000000,
			LastTickTime: 1700001000,
		}

		result := convertProtoSchedulerStats(ps)

		require.NotNil(t, result)
		assert.Equal(t, uint64(100), result.JobCount)
		assert.Equal(t, uint64(5), result.RunningJobs)
		assert.Equal(t, uint64(1000), result.SuccessCount)
		assert.Equal(t, uint64(50), result.FailureCount)
		assert.False(t, result.StartedAt.IsZero())
		assert.False(t, result.LastTickTime.IsZero())
	})

	t.Run("valid_stats_without_last_tick", func(t *testing.T) {
		ps := &pb.SchedulerStats{
			JobCount:     10,
			RunningJobs:  0,
			SuccessCount: 0,
			FailureCount: 0,
			StartedAt:    1700000000,
			LastTickTime: 0, // Never ticked
		}

		result := convertProtoSchedulerStats(ps)

		require.NotNil(t, result)
		assert.Equal(t, uint64(10), result.JobCount)
		assert.True(t, result.LastTickTime.IsZero())
	})
}

// =============================================================================
// convertProtoToServiceJobType Tests
// =============================================================================

func TestConvertProtoToServiceJobType_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.JobType
		expected service.SchedulerJobType
	}{
		{name: "one_shot", input: pb.JobType_JOB_TYPE_ONE_SHOT, expected: service.SchedulerJobTypeOneShot},
		{name: "recurring", input: pb.JobType_JOB_TYPE_RECURRING, expected: service.SchedulerJobTypeRecurring},
		{name: "delayed", input: pb.JobType_JOB_TYPE_DELAYED, expected: service.SchedulerJobTypeDelayed},
		{name: "unspecified_returns_empty", input: pb.JobType_JOB_TYPE_UNSPECIFIED, expected: ""},
		{name: "unknown_returns_empty", input: pb.JobType(999), expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToServiceJobType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// convertServiceToProtoJobType Tests
// =============================================================================

func TestConvertServiceToProtoJobType_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    service.SchedulerJobType
		expected pb.JobType
	}{
		{name: "one_shot", input: service.SchedulerJobTypeOneShot, expected: pb.JobType_JOB_TYPE_ONE_SHOT},
		{name: "recurring", input: service.SchedulerJobTypeRecurring, expected: pb.JobType_JOB_TYPE_RECURRING},
		{name: "delayed", input: service.SchedulerJobTypeDelayed, expected: pb.JobType_JOB_TYPE_DELAYED},
		{name: "empty_returns_unspecified", input: "", expected: pb.JobType_JOB_TYPE_UNSPECIFIED},
		{name: "unknown_returns_unspecified", input: "invalid", expected: pb.JobType_JOB_TYPE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertServiceToProtoJobType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// convertProtoToServiceJobStatus Tests
// =============================================================================

func TestConvertProtoToServiceJobStatus_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.JobStatus
		expected service.SchedulerJobStatus
	}{
		{name: "pending", input: pb.JobStatus_JOB_STATUS_PENDING, expected: service.SchedulerJobStatusPending},
		{name: "running", input: pb.JobStatus_JOB_STATUS_RUNNING, expected: service.SchedulerJobStatusRunning},
		{name: "completed", input: pb.JobStatus_JOB_STATUS_COMPLETED, expected: service.SchedulerJobStatusCompleted},
		{name: "failed", input: pb.JobStatus_JOB_STATUS_FAILED, expected: service.SchedulerJobStatusFailed},
		{name: "paused", input: pb.JobStatus_JOB_STATUS_PAUSED, expected: service.SchedulerJobStatusPaused},
		{name: "canceled", input: pb.JobStatus_JOB_STATUS_CANCELED, expected: service.SchedulerJobStatusCanceled},
		{name: "unspecified_returns_empty", input: pb.JobStatus_JOB_STATUS_UNSPECIFIED, expected: ""},
		{name: "unknown_returns_empty", input: pb.JobStatus(999), expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToServiceJobStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// convertServiceToProtoJobStatus Tests
// =============================================================================

func TestConvertServiceToProtoJobStatus_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    service.SchedulerJobStatus
		expected pb.JobStatus
	}{
		{name: "pending", input: service.SchedulerJobStatusPending, expected: pb.JobStatus_JOB_STATUS_PENDING},
		{name: "running", input: service.SchedulerJobStatusRunning, expected: pb.JobStatus_JOB_STATUS_RUNNING},
		{name: "completed", input: service.SchedulerJobStatusCompleted, expected: pb.JobStatus_JOB_STATUS_COMPLETED},
		{name: "failed", input: service.SchedulerJobStatusFailed, expected: pb.JobStatus_JOB_STATUS_FAILED},
		{name: "paused", input: service.SchedulerJobStatusPaused, expected: pb.JobStatus_JOB_STATUS_PAUSED},
		{name: "canceled", input: service.SchedulerJobStatusCanceled, expected: pb.JobStatus_JOB_STATUS_CANCELED},
		{name: "empty_returns_unspecified", input: "", expected: pb.JobStatus_JOB_STATUS_UNSPECIFIED},
		{name: "unknown_returns_unspecified", input: "invalid", expected: pb.JobStatus_JOB_STATUS_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertServiceToProtoJobStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// convertProtoToServiceOwnerType Tests
// =============================================================================

func TestConvertProtoToServiceOwnerType_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.OwnerType
		expected service.SchedulerOwnerType
	}{
		{name: "system", input: pb.OwnerType_OWNER_TYPE_SYSTEM, expected: service.SchedulerOwnerTypeSystem},
		{name: "tenant", input: pb.OwnerType_OWNER_TYPE_TENANT, expected: service.SchedulerOwnerTypeTenant},
		{name: "delegated", input: pb.OwnerType_OWNER_TYPE_DELEGATED, expected: service.SchedulerOwnerTypeDelegated},
		{name: "unspecified_returns_empty", input: pb.OwnerType_OWNER_TYPE_UNSPECIFIED, expected: ""},
		{name: "unknown_returns_empty", input: pb.OwnerType(999), expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToServiceOwnerType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// convertServiceToProtoOwnerType Tests
// =============================================================================

func TestConvertServiceToProtoOwnerType_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    service.SchedulerOwnerType
		expected pb.OwnerType
	}{
		{name: "system", input: service.SchedulerOwnerTypeSystem, expected: pb.OwnerType_OWNER_TYPE_SYSTEM},
		{name: "tenant", input: service.SchedulerOwnerTypeTenant, expected: pb.OwnerType_OWNER_TYPE_TENANT},
		{name: "delegated", input: service.SchedulerOwnerTypeDelegated, expected: pb.OwnerType_OWNER_TYPE_DELEGATED},
		{name: "empty_returns_unspecified", input: "", expected: pb.OwnerType_OWNER_TYPE_UNSPECIFIED},
		{name: "unknown_returns_unspecified", input: "invalid", expected: pb.OwnerType_OWNER_TYPE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertServiceToProtoOwnerType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
