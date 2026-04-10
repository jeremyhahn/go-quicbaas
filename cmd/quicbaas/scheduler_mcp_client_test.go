//go:build mcp

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

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestConvertJobTypeToSDK(t *testing.T) {
	tests := []struct {
		name     string
		input    service.SchedulerJobType
		expected scheduler.JobType
	}{
		{"OneShot", service.SchedulerJobTypeOneShot, scheduler.JobTypeOneShot},
		{"Recurring", service.SchedulerJobTypeRecurring, scheduler.JobTypeRecurring},
		{"Delayed", service.SchedulerJobTypeDelayed, scheduler.JobTypeDelayed},
		{"Default", service.SchedulerJobType("unknown"), scheduler.JobTypeOneShot},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertJobTypeToSDK(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --------------------------------------------------------------------------
// convertJobTypeToService tests
// --------------------------------------------------------------------------

func TestConvertJobTypeToService(t *testing.T) {
	tests := []struct {
		name     string
		input    scheduler.JobType
		expected service.SchedulerJobType
	}{
		{"OneShot", scheduler.JobTypeOneShot, service.SchedulerJobTypeOneShot},
		{"Recurring", scheduler.JobTypeRecurring, service.SchedulerJobTypeRecurring},
		{"Delayed", scheduler.JobTypeDelayed, service.SchedulerJobTypeDelayed},
		{"Default", scheduler.JobType(99), service.SchedulerJobTypeOneShot},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertJobTypeToService(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --------------------------------------------------------------------------
// convertOwnerTypeToSDK tests
// --------------------------------------------------------------------------

func TestConvertOwnerTypeToSDK(t *testing.T) {
	tests := []struct {
		name     string
		input    service.SchedulerOwnerType
		expected scheduler.OwnerType
	}{
		{"System", service.SchedulerOwnerTypeSystem, scheduler.OwnerTypeSystem},
		{"Tenant", service.SchedulerOwnerTypeTenant, scheduler.OwnerTypeTenant},
		{"Delegated", service.SchedulerOwnerTypeDelegated, scheduler.OwnerTypeDelegated},
		{"Default", service.SchedulerOwnerType("unknown"), scheduler.OwnerTypeSystem},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertOwnerTypeToSDK(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --------------------------------------------------------------------------
// convertOwnerTypeToService tests
// --------------------------------------------------------------------------

func TestConvertOwnerTypeToService(t *testing.T) {
	tests := []struct {
		name     string
		input    scheduler.OwnerType
		expected service.SchedulerOwnerType
	}{
		{"System", scheduler.OwnerTypeSystem, service.SchedulerOwnerTypeSystem},
		{"Tenant", scheduler.OwnerTypeTenant, service.SchedulerOwnerTypeTenant},
		{"Delegated", scheduler.OwnerTypeDelegated, service.SchedulerOwnerTypeDelegated},
		{"Default", scheduler.OwnerType(99), service.SchedulerOwnerTypeSystem},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertOwnerTypeToService(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --------------------------------------------------------------------------
// convertStatusToSDK tests
// --------------------------------------------------------------------------

func TestConvertStatusToSDK(t *testing.T) {
	tests := []struct {
		name     string
		input    service.SchedulerJobStatus
		expected scheduler.JobStatus
	}{
		{"Pending", service.SchedulerJobStatusPending, scheduler.JobStatusPending},
		{"Running", service.SchedulerJobStatusRunning, scheduler.JobStatusRunning},
		{"Completed", service.SchedulerJobStatusCompleted, scheduler.JobStatusCompleted},
		{"Failed", service.SchedulerJobStatusFailed, scheduler.JobStatusFailed},
		{"Paused", service.SchedulerJobStatusPaused, scheduler.JobStatusPaused},
		{"Canceled", service.SchedulerJobStatusCanceled, scheduler.JobStatusCanceled},
		{"Default", service.SchedulerJobStatus("unknown"), scheduler.JobStatusPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertStatusToSDK(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --------------------------------------------------------------------------
// convertStatusToService tests
// --------------------------------------------------------------------------

func TestConvertStatusToService(t *testing.T) {
	tests := []struct {
		name     string
		input    scheduler.JobStatus
		expected service.SchedulerJobStatus
	}{
		{"Pending", scheduler.JobStatusPending, service.SchedulerJobStatusPending},
		{"Running", scheduler.JobStatusRunning, service.SchedulerJobStatusRunning},
		{"Completed", scheduler.JobStatusCompleted, service.SchedulerJobStatusCompleted},
		{"Failed", scheduler.JobStatusFailed, service.SchedulerJobStatusFailed},
		{"Paused", scheduler.JobStatusPaused, service.SchedulerJobStatusPaused},
		{"Canceled", scheduler.JobStatusCanceled, service.SchedulerJobStatusCanceled},
		{"Default", scheduler.JobStatus(99), service.SchedulerJobStatusPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertStatusToService(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --------------------------------------------------------------------------
// convertJobInfoToService tests
// --------------------------------------------------------------------------

func TestConvertJobInfoToService_Nil(t *testing.T) {
	result := convertJobInfoToService(nil)
	assert.Nil(t, result)
}

func TestConvertJobInfoToService_Valid(t *testing.T) {
	info := &scheduler.JobInfo{
		JobID:       uint64(123),
		Name:        "test-job",
		Description: "A test job",
		JobType:     scheduler.JobTypeRecurring,
		Status:      scheduler.JobStatusRunning,
		Schedule:    "*/5 * * * *",
		OwnerType:   scheduler.OwnerTypeTenant,
		OwnerID:     "tenant-abc",
		MaxRetries:  3,
	}

	result := convertJobInfoToService(info)
	assert.NotNil(t, result)
	assert.Equal(t, uint64(123), result.ID)
	assert.Equal(t, "test-job", result.Name)
	assert.Equal(t, "A test job", result.Description)
	assert.Equal(t, service.SchedulerJobTypeRecurring, result.Type)
	assert.Equal(t, service.SchedulerJobStatusRunning, result.Status)
	assert.Equal(t, "*/5 * * * *", result.Schedule)
	assert.Equal(t, service.SchedulerOwnerTypeTenant, result.OwnerType)
	assert.Equal(t, "tenant-abc", result.OwnerID)
	assert.Equal(t, int32(3), result.MaxRetries)
}
func TestConvertJobTypeToSDK_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    service.SchedulerJobType
		expected scheduler.JobType
	}{
		{
			name:     "OneShot type",
			input:    service.SchedulerJobTypeOneShot,
			expected: scheduler.JobTypeOneShot,
		},
		{
			name:     "Recurring type",
			input:    service.SchedulerJobTypeRecurring,
			expected: scheduler.JobTypeRecurring,
		},
		{
			name:     "Delayed type",
			input:    service.SchedulerJobTypeDelayed,
			expected: scheduler.JobTypeDelayed,
		},
		{
			name:     "Unknown type defaults to OneShot",
			input:    "unknown_type",
			expected: scheduler.JobTypeOneShot,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertJobTypeToSDK(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertJobTypeToService_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    scheduler.JobType
		expected service.SchedulerJobType
	}{
		{
			name:     "OneShot type",
			input:    scheduler.JobTypeOneShot,
			expected: service.SchedulerJobTypeOneShot,
		},
		{
			name:     "Recurring type",
			input:    scheduler.JobTypeRecurring,
			expected: service.SchedulerJobTypeRecurring,
		},
		{
			name:     "Delayed type",
			input:    scheduler.JobTypeDelayed,
			expected: service.SchedulerJobTypeDelayed,
		},
		{
			name:     "Unknown type defaults to OneShot",
			input:    scheduler.JobType(999),
			expected: service.SchedulerJobTypeOneShot,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertJobTypeToService(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertOwnerTypeToSDK_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    service.SchedulerOwnerType
		expected scheduler.OwnerType
	}{
		{
			name:     "System owner",
			input:    service.SchedulerOwnerTypeSystem,
			expected: scheduler.OwnerTypeSystem,
		},
		{
			name:     "Tenant owner",
			input:    service.SchedulerOwnerTypeTenant,
			expected: scheduler.OwnerTypeTenant,
		},
		{
			name:     "Delegated owner",
			input:    service.SchedulerOwnerTypeDelegated,
			expected: scheduler.OwnerTypeDelegated,
		},
		{
			name:     "Unknown owner defaults to System",
			input:    "unknown_owner",
			expected: scheduler.OwnerTypeSystem,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertOwnerTypeToSDK(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertOwnerTypeToService_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    scheduler.OwnerType
		expected service.SchedulerOwnerType
	}{
		{
			name:     "System owner",
			input:    scheduler.OwnerTypeSystem,
			expected: service.SchedulerOwnerTypeSystem,
		},
		{
			name:     "Tenant owner",
			input:    scheduler.OwnerTypeTenant,
			expected: service.SchedulerOwnerTypeTenant,
		},
		{
			name:     "Delegated owner",
			input:    scheduler.OwnerTypeDelegated,
			expected: service.SchedulerOwnerTypeDelegated,
		},
		{
			name:     "Unknown owner defaults to System",
			input:    scheduler.OwnerType(999),
			expected: service.SchedulerOwnerTypeSystem,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertOwnerTypeToService(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertStatusToSDK_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    service.SchedulerJobStatus
		expected scheduler.JobStatus
	}{
		{
			name:     "Pending status",
			input:    service.SchedulerJobStatusPending,
			expected: scheduler.JobStatusPending,
		},
		{
			name:     "Running status",
			input:    service.SchedulerJobStatusRunning,
			expected: scheduler.JobStatusRunning,
		},
		{
			name:     "Completed status",
			input:    service.SchedulerJobStatusCompleted,
			expected: scheduler.JobStatusCompleted,
		},
		{
			name:     "Failed status",
			input:    service.SchedulerJobStatusFailed,
			expected: scheduler.JobStatusFailed,
		},
		{
			name:     "Paused status",
			input:    service.SchedulerJobStatusPaused,
			expected: scheduler.JobStatusPaused,
		},
		{
			name:     "Canceled status",
			input:    service.SchedulerJobStatusCanceled,
			expected: scheduler.JobStatusCanceled,
		},
		{
			name:     "Unknown status defaults to Pending",
			input:    "unknown_status",
			expected: scheduler.JobStatusPending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertStatusToSDK(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertStatusToService_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    scheduler.JobStatus
		expected service.SchedulerJobStatus
	}{
		{
			name:     "Pending status",
			input:    scheduler.JobStatusPending,
			expected: service.SchedulerJobStatusPending,
		},
		{
			name:     "Running status",
			input:    scheduler.JobStatusRunning,
			expected: service.SchedulerJobStatusRunning,
		},
		{
			name:     "Completed status",
			input:    scheduler.JobStatusCompleted,
			expected: service.SchedulerJobStatusCompleted,
		},
		{
			name:     "Failed status",
			input:    scheduler.JobStatusFailed,
			expected: service.SchedulerJobStatusFailed,
		},
		{
			name:     "Paused status",
			input:    scheduler.JobStatusPaused,
			expected: service.SchedulerJobStatusPaused,
		},
		{
			name:     "Canceled status",
			input:    scheduler.JobStatusCanceled,
			expected: service.SchedulerJobStatusCanceled,
		},
		{
			name:     "Unknown status defaults to Pending",
			input:    scheduler.JobStatus(999),
			expected: service.SchedulerJobStatusPending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertStatusToService(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertJobInfoToService_AllCases(t *testing.T) {
	t.Run("Nil input returns nil", func(t *testing.T) {
		result := convertJobInfoToService(nil)
		assert.Nil(t, result)
	})

	t.Run("Valid job info conversion", func(t *testing.T) {
		now := time.Now()
		info := &scheduler.JobInfo{
			JobID:          123,
			Name:           "test-job",
			Description:    "Test job description",
			JobType:        scheduler.JobTypeRecurring,
			Status:         scheduler.JobStatusRunning,
			Schedule:       "0 * * * *",
			Payload:        []byte(`{"key": "value"}`),
			OwnerType:      scheduler.OwnerTypeTenant,
			OwnerID:        "tenant-456",
			Timeout:        time.Minute,
			MaxRetries:     3,
			Tags:           map[string]string{"env": "test"},
			NextRunAt:      now.Add(time.Hour),
			LastRunAt:      now.Add(-time.Hour),
			ExecutionCount: 10,
			FailureCount:   2,
			CreatedAt:      now.Add(-24 * time.Hour),
			UpdatedAt:      now,
		}

		result := convertJobInfoToService(info)
		require.NotNil(t, result)
		assert.Equal(t, "test-job", result.Name)
		assert.Equal(t, "Test job description", result.Description)
		assert.Equal(t, service.SchedulerJobTypeRecurring, result.Type)
		assert.Equal(t, service.SchedulerJobStatusRunning, result.Status)
		assert.Equal(t, "0 * * * *", result.Schedule)
		assert.Equal(t, service.SchedulerOwnerTypeTenant, result.OwnerType)
		assert.Equal(t, "tenant-456", result.OwnerID)
		assert.Equal(t, time.Minute, result.Timeout)
		assert.Equal(t, int32(3), result.MaxRetries)
		// Use uint64 for comparison since those are the actual types
		assert.Equal(t, uint64(10), uint64(result.RunCount))
		assert.Equal(t, uint64(2), uint64(result.FailCount))
	})
}

// =============================================================================
// MCP Scheduler Client Tests - Testing client methods
// =============================================================================

func TestMCPSchedulerClient_NilClientMethods_NinetyBoost(t *testing.T) {
	client := &MCPSchedulerClient{client: nil}
	ctx := context.Background()

	t.Run("SchedulerCreateJob nil", func(t *testing.T) {
		_, err := client.SchedulerCreateJob(ctx, &service.CreateSchedulerJobRequest{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "MCP client not initialized")
	})

	t.Run("SchedulerGetJob nil", func(t *testing.T) {
		_, err := client.SchedulerGetJob(ctx, &service.GetSchedulerJobRequest{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "MCP client not initialized")
	})

	t.Run("SchedulerUpdateJob nil", func(t *testing.T) {
		_, err := client.SchedulerUpdateJob(ctx, &service.UpdateSchedulerJobRequest{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "MCP client not initialized")
	})

	t.Run("SchedulerDeleteJob nil", func(t *testing.T) {
		_, err := client.SchedulerDeleteJob(ctx, &service.DeleteSchedulerJobRequest{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "MCP client not initialized")
	})

	t.Run("SchedulerListJobs nil", func(t *testing.T) {
		_, err := client.SchedulerListJobs(ctx, &service.ListSchedulerJobsRequest{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "MCP client not initialized")
	})

	t.Run("SchedulerTriggerJob nil", func(t *testing.T) {
		_, err := client.SchedulerTriggerJob(ctx, &service.TriggerSchedulerJobRequest{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "MCP client not initialized")
	})

	t.Run("SchedulerPauseJob nil", func(t *testing.T) {
		_, err := client.SchedulerPauseJob(ctx, &service.PauseSchedulerJobRequest{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "MCP client not initialized")
	})

	t.Run("SchedulerResumeJob nil", func(t *testing.T) {
		_, err := client.SchedulerResumeJob(ctx, &service.ResumeSchedulerJobRequest{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "MCP client not initialized")
	})

	t.Run("SchedulerCancelJob nil", func(t *testing.T) {
		_, err := client.SchedulerCancelJob(ctx, &service.CancelSchedulerJobRequest{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "MCP client not initialized")
	})

	t.Run("SchedulerGetJobHistory nil", func(t *testing.T) {
		_, err := client.SchedulerGetJobHistory(ctx, &service.GetSchedulerJobHistoryRequest{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "MCP client not initialized")
	})

	t.Run("SchedulerGetStatus nil", func(t *testing.T) {
		_, err := client.SchedulerGetStatus(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "MCP client not initialized")
	})
}
