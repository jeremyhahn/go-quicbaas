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

package scheduler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultConfig tests that DefaultConfig returns sensible values.
func TestDefaultConfig(t *testing.T) {
	t.Run("ReturnsNonNilConfig", func(t *testing.T) {
		cfg := DefaultConfig()
		assert.NotNil(t, cfg)
	})

	t.Run("TickIntervalIsPositive", func(t *testing.T) {
		cfg := DefaultConfig()
		assert.Equal(t, time.Second, cfg.TickInterval)
		assert.Greater(t, cfg.TickInterval, time.Duration(0))
	})

	t.Run("MaxConcurrentJobsIsPositive", func(t *testing.T) {
		cfg := DefaultConfig()
		assert.Equal(t, 10, cfg.MaxConcurrentJobs)
		assert.Greater(t, cfg.MaxConcurrentJobs, 0)
	})

	t.Run("DefaultTimeoutIsPositive", func(t *testing.T) {
		cfg := DefaultConfig()
		assert.Equal(t, 5*time.Minute, cfg.DefaultTimeout)
		assert.Greater(t, cfg.DefaultTimeout, time.Duration(0))
	})

	t.Run("DefaultRetriesIsPositive", func(t *testing.T) {
		cfg := DefaultConfig()
		assert.Equal(t, 3, cfg.DefaultRetries)
		assert.Greater(t, cfg.DefaultRetries, 0)
	})

	t.Run("RetryBackoffIsPositive", func(t *testing.T) {
		cfg := DefaultConfig()
		assert.Equal(t, 5*time.Second, cfg.RetryBackoff)
		assert.Greater(t, cfg.RetryBackoff, time.Duration(0))
	})

	t.Run("MultipleCallsReturnIndependentConfigs", func(t *testing.T) {
		cfg1 := DefaultConfig()
		cfg2 := DefaultConfig()
		assert.NotSame(t, cfg1, cfg2)
		cfg1.MaxConcurrentJobs = 100
		assert.NotEqual(t, cfg1.MaxConcurrentJobs, cfg2.MaxConcurrentJobs)
	})
}

// TestJobTypesConstants tests JobType constant values.
func TestJobTypesConstants(t *testing.T) {
	t.Run("JobTypeValues", func(t *testing.T) {
		assert.Equal(t, JobType("one-shot"), JobTypeOneShot)
		assert.Equal(t, JobType("recurring"), JobTypeRecurring)
		assert.Equal(t, JobType("delayed"), JobTypeDelayed)
	})
}

// TestJobStatusConstants tests JobStatus constant values.
func TestJobStatusConstants(t *testing.T) {
	t.Run("JobStatusValues", func(t *testing.T) {
		assert.Equal(t, JobStatus("pending"), JobStatusPending)
		assert.Equal(t, JobStatus("running"), JobStatusRunning)
		assert.Equal(t, JobStatus("completed"), JobStatusCompleted)
		assert.Equal(t, JobStatus("failed"), JobStatusFailed)
		assert.Equal(t, JobStatus("paused"), JobStatusPaused)
		assert.Equal(t, JobStatus("canceled"), JobStatusCanceled)
	})
}

// TestOwnerTypeConstants tests OwnerType constant values.
func TestOwnerTypeConstants(t *testing.T) {
	t.Run("OwnerTypeValues", func(t *testing.T) {
		assert.Equal(t, OwnerType("system"), OwnerTypeSystem)
		assert.Equal(t, OwnerType("tenant"), OwnerTypeTenant)
		assert.Equal(t, OwnerType("delegated"), OwnerTypeDelegated)
	})
}

// TestJobJSONMarshalUnmarshal tests Job struct JSON marshaling and unmarshaling.
func TestJobJSONMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		job  *Job
	}{
		{
			name: "FullJob",
			job: &Job{
				ID:             12345,
				Name:           "test-job",
				Description:    "A test job for unit testing",
				OwnerType:      OwnerTypeTenant,
				OwnerID:        "tenant-123",
				TargetTenantID: "target-tenant-456",
				CreatedBy:      "user-789",
				Type:           JobTypeRecurring,
				Schedule:       "*/5 * * * *",
				ExecuteAt:      time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
				Delay:          5 * time.Minute,
				Handler:        "backup-handler",
				Payload:        json.RawMessage(`{"key":"value"}`),
				Priority:       10,
				Timeout:        30 * time.Second,
				MaxRetries:     5,
				Status:         JobStatusPending,
				LastRun:        time.Date(2025, 1, 14, 10, 0, 0, 0, time.UTC),
				NextRun:        time.Date(2025, 1, 15, 10, 5, 0, 0, time.UTC),
				RunCount:       100,
				FailCount:      2,
				LastError:      "previous error",
				Labels:         map[string]string{"env": "test", "team": "platform"},
				CreatedAt:      time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				UpdatedAt:      time.Date(2025, 1, 15, 9, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "MinimalJob",
			job: &Job{
				ID:        1,
				Name:      "minimal-job",
				OwnerType: OwnerTypeSystem,
				Type:      JobTypeOneShot,
				Handler:   "simple-handler",
				Status:    JobStatusPending,
			},
		},
		{
			name: "DelayedJob",
			job: &Job{
				ID:        2,
				Name:      "delayed-job",
				OwnerType: OwnerTypeSystem,
				Type:      JobTypeDelayed,
				Delay:     10 * time.Minute,
				Handler:   "delayed-handler",
				Status:    JobStatusPending,
			},
		},
		{
			name: "JobWithEmptyLabels",
			job: &Job{
				ID:        3,
				Name:      "labeled-job",
				OwnerType: OwnerTypeTenant,
				OwnerID:   "tenant-1",
				Type:      JobTypeOneShot,
				Handler:   "label-handler",
				Status:    JobStatusRunning,
				Labels:    map[string]string{},
			},
		},
		{
			name: "DelegatedJob",
			job: &Job{
				ID:             4,
				Name:           "delegated-job",
				OwnerType:      OwnerTypeDelegated,
				TargetTenantID: "target-tenant-999",
				Type:           JobTypeRecurring,
				Schedule:       "0 0 * * *",
				Handler:        "maintenance-handler",
				Status:         JobStatusCompleted,
				RunCount:       50,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(tc.job)
			require.NoError(t, err)
			assert.NotEmpty(t, data)

			// Unmarshal back to Job
			var unmarshaled Job
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)

			// Compare fields
			assert.Equal(t, tc.job.ID, unmarshaled.ID)
			assert.Equal(t, tc.job.Name, unmarshaled.Name)
			assert.Equal(t, tc.job.Description, unmarshaled.Description)
			assert.Equal(t, tc.job.OwnerType, unmarshaled.OwnerType)
			assert.Equal(t, tc.job.OwnerID, unmarshaled.OwnerID)
			assert.Equal(t, tc.job.TargetTenantID, unmarshaled.TargetTenantID)
			assert.Equal(t, tc.job.CreatedBy, unmarshaled.CreatedBy)
			assert.Equal(t, tc.job.Type, unmarshaled.Type)
			assert.Equal(t, tc.job.Schedule, unmarshaled.Schedule)
			assert.Equal(t, tc.job.Handler, unmarshaled.Handler)
			assert.Equal(t, tc.job.Priority, unmarshaled.Priority)
			assert.Equal(t, tc.job.Timeout, unmarshaled.Timeout)
			assert.Equal(t, tc.job.MaxRetries, unmarshaled.MaxRetries)
			assert.Equal(t, tc.job.Status, unmarshaled.Status)
			assert.Equal(t, tc.job.RunCount, unmarshaled.RunCount)
			assert.Equal(t, tc.job.FailCount, unmarshaled.FailCount)
			assert.Equal(t, tc.job.LastError, unmarshaled.LastError)
		})
	}
}

// TestJobJSONInvalidInput tests Job unmarshaling with invalid input.
func TestJobJSONInvalidInput(t *testing.T) {
	t.Run("InvalidJSON", func(t *testing.T) {
		var job Job
		err := json.Unmarshal([]byte("not valid json"), &job)
		assert.Error(t, err)
	})

	t.Run("EmptyJSON", func(t *testing.T) {
		var job Job
		err := json.Unmarshal([]byte("{}"), &job)
		assert.NoError(t, err)
		assert.Equal(t, uint64(0), job.ID)
		assert.Equal(t, "", job.Name)
	})

	t.Run("NullJSON", func(t *testing.T) {
		var job Job
		err := json.Unmarshal([]byte("null"), &job)
		assert.NoError(t, err)
	})

	t.Run("InvalidTypeForField", func(t *testing.T) {
		var job Job
		err := json.Unmarshal([]byte(`{"id":"not-a-number"}`), &job)
		assert.Error(t, err)
	})
}

// TestJobExecutionJSONMarshalUnmarshal tests JobExecution struct JSON marshaling/unmarshaling.
func TestJobExecutionJSONMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name      string
		execution *JobExecution
	}{
		{
			name: "CompletedExecution",
			execution: &JobExecution{
				ID:        1001,
				JobID:     12345,
				Status:    JobStatusCompleted,
				StartedAt: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
				EndedAt:   time.Date(2025, 1, 15, 10, 5, 0, 0, time.UTC),
				Duration:  5 * time.Minute,
				Output:    "Job completed successfully",
			},
		},
		{
			name: "FailedExecution",
			execution: &JobExecution{
				ID:        1002,
				JobID:     12346,
				Status:    JobStatusFailed,
				StartedAt: time.Date(2025, 1, 15, 11, 0, 0, 0, time.UTC),
				EndedAt:   time.Date(2025, 1, 15, 11, 1, 0, 0, time.UTC),
				Duration:  1 * time.Minute,
				Error:     "connection timeout",
			},
		},
		{
			name: "RunningExecution",
			execution: &JobExecution{
				ID:        1003,
				JobID:     12347,
				Status:    JobStatusRunning,
				StartedAt: time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "MinimalExecution",
			execution: &JobExecution{
				ID:     1004,
				JobID:  12348,
				Status: JobStatusPending,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(tc.execution)
			require.NoError(t, err)
			assert.NotEmpty(t, data)

			// Unmarshal back to JobExecution
			var unmarshaled JobExecution
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)

			// Compare fields
			assert.Equal(t, tc.execution.ID, unmarshaled.ID)
			assert.Equal(t, tc.execution.JobID, unmarshaled.JobID)
			assert.Equal(t, tc.execution.Status, unmarshaled.Status)
			assert.Equal(t, tc.execution.Duration, unmarshaled.Duration)
			assert.Equal(t, tc.execution.Error, unmarshaled.Error)
			assert.Equal(t, tc.execution.Output, unmarshaled.Output)
		})
	}
}

// TestJobExecutionJSONInvalidInput tests JobExecution unmarshaling with invalid input.
func TestJobExecutionJSONInvalidInput(t *testing.T) {
	t.Run("InvalidJSON", func(t *testing.T) {
		var exec JobExecution
		err := json.Unmarshal([]byte("invalid json"), &exec)
		assert.Error(t, err)
	})

	t.Run("EmptyJSON", func(t *testing.T) {
		var exec JobExecution
		err := json.Unmarshal([]byte("{}"), &exec)
		assert.NoError(t, err)
		assert.Equal(t, uint64(0), exec.ID)
	})
}

// TestJobResultJSONMarshalUnmarshal tests JobResult struct JSON marshaling/unmarshaling.
func TestJobResultJSONMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		result *JobResult
	}{
		{
			name: "SuccessfulResult",
			result: &JobResult{
				Success: true,
				Message: "Job completed successfully",
				Output:  []byte(`{"processed":100}`),
			},
		},
		{
			name: "FailedResult",
			result: &JobResult{
				Success: false,
				Message: "Job failed due to timeout",
				Output:  nil,
			},
		},
		{
			name: "ResultWithLargeOutput",
			result: &JobResult{
				Success: true,
				Message: "Batch processing complete",
				Output:  make([]byte, 1024),
			},
		},
		{
			name: "MinimalResult",
			result: &JobResult{
				Success: true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(tc.result)
			require.NoError(t, err)
			assert.NotEmpty(t, data)

			// Unmarshal back to JobResult
			var unmarshaled JobResult
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)

			// Compare fields
			assert.Equal(t, tc.result.Success, unmarshaled.Success)
			assert.Equal(t, tc.result.Message, unmarshaled.Message)
			assert.Equal(t, tc.result.Output, unmarshaled.Output)
		})
	}
}

// TestJobResultJSONInvalidInput tests JobResult unmarshaling with invalid input.
func TestJobResultJSONInvalidInput(t *testing.T) {
	t.Run("InvalidJSON", func(t *testing.T) {
		var result JobResult
		err := json.Unmarshal([]byte("invalid"), &result)
		assert.Error(t, err)
	})

	t.Run("EmptyJSON", func(t *testing.T) {
		var result JobResult
		err := json.Unmarshal([]byte("{}"), &result)
		assert.NoError(t, err)
		assert.False(t, result.Success)
	})
}

// TestJobQueryJSONMarshalUnmarshal tests JobQuery struct JSON marshaling/unmarshaling.
func TestJobQueryJSONMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name  string
		query *JobQuery
	}{
		{
			name: "FullQuery",
			query: &JobQuery{
				OwnerType:      OwnerTypeTenant,
				OwnerID:        "tenant-123",
				TargetTenantID: "target-456",
				Status:         JobStatusPending,
				Handler:        "backup-handler",
				Labels:         map[string]string{"env": "prod", "priority": "high"},
				Limit:          100,
				Cursor:         "cursor-token-abc",
			},
		},
		{
			name: "MinimalQuery",
			query: &JobQuery{
				Limit: 10,
			},
		},
		{
			name: "QueryByOwner",
			query: &JobQuery{
				OwnerType: OwnerTypeSystem,
				OwnerID:   "system",
			},
		},
		{
			name: "QueryByStatus",
			query: &JobQuery{
				Status: JobStatusFailed,
				Limit:  50,
			},
		},
		{
			name: "QueryWithLabels",
			query: &JobQuery{
				Labels: map[string]string{
					"team":        "platform",
					"environment": "staging",
					"version":     "v2",
				},
			},
		},
		{
			name:  "EmptyQuery",
			query: &JobQuery{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(tc.query)
			require.NoError(t, err)
			assert.NotEmpty(t, data)

			// Unmarshal back to JobQuery
			var unmarshaled JobQuery
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)

			// Compare fields
			assert.Equal(t, tc.query.OwnerType, unmarshaled.OwnerType)
			assert.Equal(t, tc.query.OwnerID, unmarshaled.OwnerID)
			assert.Equal(t, tc.query.TargetTenantID, unmarshaled.TargetTenantID)
			assert.Equal(t, tc.query.Status, unmarshaled.Status)
			assert.Equal(t, tc.query.Handler, unmarshaled.Handler)
			assert.Equal(t, tc.query.Limit, unmarshaled.Limit)
			assert.Equal(t, tc.query.Cursor, unmarshaled.Cursor)
		})
	}
}

// TestJobQueryJSONInvalidInput tests JobQuery unmarshaling with invalid input.
func TestJobQueryJSONInvalidInput(t *testing.T) {
	t.Run("InvalidJSON", func(t *testing.T) {
		var query JobQuery
		err := json.Unmarshal([]byte("not-json"), &query)
		assert.Error(t, err)
	})

	t.Run("InvalidLimitType", func(t *testing.T) {
		var query JobQuery
		err := json.Unmarshal([]byte(`{"limit":"not-a-number"}`), &query)
		assert.Error(t, err)
	})
}

// TestSchedulerStatsJSONMarshalUnmarshal tests SchedulerStats struct JSON marshaling/unmarshaling.
func TestSchedulerStatsJSONMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name  string
		stats *SchedulerStats
	}{
		{
			name: "FullStats",
			stats: &SchedulerStats{
				JobCount:     1000,
				RunningJobs:  10,
				SuccessCount: 950,
				FailureCount: 40,
				LastTickTime: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
				StartedAt:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "ZeroStats",
			stats: &SchedulerStats{
				JobCount:     0,
				RunningJobs:  0,
				SuccessCount: 0,
				FailureCount: 0,
				StartedAt:    time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "HighLoadStats",
			stats: &SchedulerStats{
				JobCount:     1000000,
				RunningJobs:  500,
				SuccessCount: 999000,
				FailureCount: 500,
				LastTickTime: time.Date(2025, 1, 15, 23, 59, 59, 0, time.UTC),
				StartedAt:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name:  "EmptyStats",
			stats: &SchedulerStats{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(tc.stats)
			require.NoError(t, err)
			assert.NotEmpty(t, data)

			// Unmarshal back to SchedulerStats
			var unmarshaled SchedulerStats
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)

			// Compare fields
			assert.Equal(t, tc.stats.JobCount, unmarshaled.JobCount)
			assert.Equal(t, tc.stats.RunningJobs, unmarshaled.RunningJobs)
			assert.Equal(t, tc.stats.SuccessCount, unmarshaled.SuccessCount)
			assert.Equal(t, tc.stats.FailureCount, unmarshaled.FailureCount)
		})
	}
}

// TestSchedulerStatsJSONInvalidInput tests SchedulerStats unmarshaling with invalid input.
func TestSchedulerStatsJSONInvalidInput(t *testing.T) {
	t.Run("InvalidJSON", func(t *testing.T) {
		var stats SchedulerStats
		err := json.Unmarshal([]byte("bad json"), &stats)
		assert.Error(t, err)
	})

	t.Run("InvalidFieldType", func(t *testing.T) {
		var stats SchedulerStats
		err := json.Unmarshal([]byte(`{"job_count":"not-a-number"}`), &stats)
		assert.Error(t, err)
	})
}

// TestSchedulerConfigJSONMarshalUnmarshal tests SchedulerConfig struct JSON marshaling/unmarshaling.
func TestSchedulerConfigJSONMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		config *SchedulerConfig
	}{
		{
			name:   "DefaultConfig",
			config: DefaultConfig(),
		},
		{
			name: "CustomConfig",
			config: &SchedulerConfig{
				TickInterval:      500 * time.Millisecond,
				MaxConcurrentJobs: 50,
				DefaultTimeout:    10 * time.Minute,
				DefaultRetries:    5,
				RetryBackoff:      30 * time.Second,
			},
		},
		{
			name: "MinimalConfig",
			config: &SchedulerConfig{
				TickInterval:      100 * time.Millisecond,
				MaxConcurrentJobs: 1,
				DefaultTimeout:    time.Second,
				DefaultRetries:    0,
				RetryBackoff:      0,
			},
		},
		{
			name: "HighThroughputConfig",
			config: &SchedulerConfig{
				TickInterval:      100 * time.Millisecond,
				MaxConcurrentJobs: 1000,
				DefaultTimeout:    30 * time.Minute,
				DefaultRetries:    10,
				RetryBackoff:      time.Minute,
			},
		},
		{
			name:   "ZeroConfig",
			config: &SchedulerConfig{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(tc.config)
			require.NoError(t, err)
			assert.NotEmpty(t, data)

			// Unmarshal back to SchedulerConfig
			var unmarshaled SchedulerConfig
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)

			// Compare fields
			assert.Equal(t, tc.config.TickInterval, unmarshaled.TickInterval)
			assert.Equal(t, tc.config.MaxConcurrentJobs, unmarshaled.MaxConcurrentJobs)
			assert.Equal(t, tc.config.DefaultTimeout, unmarshaled.DefaultTimeout)
			assert.Equal(t, tc.config.DefaultRetries, unmarshaled.DefaultRetries)
			assert.Equal(t, tc.config.RetryBackoff, unmarshaled.RetryBackoff)
		})
	}
}

// TestSchedulerConfigJSONInvalidInput tests SchedulerConfig unmarshaling with invalid input.
func TestSchedulerConfigJSONInvalidInput(t *testing.T) {
	t.Run("InvalidJSON", func(t *testing.T) {
		var cfg SchedulerConfig
		err := json.Unmarshal([]byte("not valid json"), &cfg)
		assert.Error(t, err)
	})

	t.Run("InvalidMaxConcurrentJobsType", func(t *testing.T) {
		var cfg SchedulerConfig
		err := json.Unmarshal([]byte(`{"max_concurrent_jobs":"not-a-number"}`), &cfg)
		assert.Error(t, err)
	})
}

// TestJobPayloadHandling tests that Job properly handles RawMessage payload.
func TestJobPayloadHandling(t *testing.T) {
	t.Run("ValidJSONPayload", func(t *testing.T) {
		job := &Job{
			ID:      1,
			Name:    "payload-test",
			Handler: "test-handler",
			Payload: json.RawMessage(`{"database":"test","tables":["users","orders"]}`),
		}

		data, err := json.Marshal(job)
		require.NoError(t, err)

		var unmarshaled Job
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)

		// Verify payload can be parsed
		var payload map[string]interface{}
		err = json.Unmarshal(unmarshaled.Payload, &payload)
		require.NoError(t, err)
		assert.Equal(t, "test", payload["database"])
	})

	t.Run("NullPayload", func(t *testing.T) {
		job := &Job{
			ID:      2,
			Name:    "null-payload",
			Handler: "handler",
			Payload: nil,
		}

		data, err := json.Marshal(job)
		require.NoError(t, err)

		var unmarshaled Job
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)
		assert.Nil(t, unmarshaled.Payload)
	})

	t.Run("EmptyObjectPayload", func(t *testing.T) {
		job := &Job{
			ID:      3,
			Name:    "empty-payload",
			Handler: "handler",
			Payload: json.RawMessage(`{}`),
		}

		data, err := json.Marshal(job)
		require.NoError(t, err)

		var unmarshaled Job
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)
		assert.Equal(t, json.RawMessage(`{}`), unmarshaled.Payload)
	})

	t.Run("ArrayPayload", func(t *testing.T) {
		job := &Job{
			ID:      4,
			Name:    "array-payload",
			Handler: "handler",
			Payload: json.RawMessage(`[1,2,3,"test"]`),
		}

		data, err := json.Marshal(job)
		require.NoError(t, err)

		var unmarshaled Job
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)

		var payload []interface{}
		err = json.Unmarshal(unmarshaled.Payload, &payload)
		require.NoError(t, err)
		assert.Len(t, payload, 4)
	})
}

// TestJobLabelsHandling tests that Job properly handles labels map.
func TestJobLabelsHandling(t *testing.T) {
	t.Run("MultipleLabels", func(t *testing.T) {
		job := &Job{
			ID:      1,
			Name:    "labeled-job",
			Handler: "handler",
			Labels: map[string]string{
				"environment": "production",
				"team":        "platform",
				"priority":    "high",
				"version":     "v1.2.3",
			},
		}

		data, err := json.Marshal(job)
		require.NoError(t, err)

		var unmarshaled Job
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)

		assert.Equal(t, "production", unmarshaled.Labels["environment"])
		assert.Equal(t, "platform", unmarshaled.Labels["team"])
		assert.Equal(t, "high", unmarshaled.Labels["priority"])
		assert.Equal(t, "v1.2.3", unmarshaled.Labels["version"])
	})

	t.Run("NilLabels", func(t *testing.T) {
		job := &Job{
			ID:      2,
			Name:    "no-labels",
			Handler: "handler",
			Labels:  nil,
		}

		data, err := json.Marshal(job)
		require.NoError(t, err)

		var unmarshaled Job
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)
		assert.Nil(t, unmarshaled.Labels)
	})

	t.Run("EmptyLabels", func(t *testing.T) {
		job := &Job{
			ID:      3,
			Name:    "empty-labels",
			Handler: "handler",
			Labels:  map[string]string{},
		}

		data, err := json.Marshal(job)
		require.NoError(t, err)

		var unmarshaled Job
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)
		// Empty map may become nil after unmarshaling with omitempty
		assert.True(t, len(unmarshaled.Labels) == 0)
	})

	t.Run("SpecialCharactersInLabels", func(t *testing.T) {
		job := &Job{
			ID:      4,
			Name:    "special-labels",
			Handler: "handler",
			Labels: map[string]string{
				"key-with-dash":       "value",
				"key.with.dot":        "value",
				"key_with_underscore": "value",
				"key/with/slash":      "value",
			},
		}

		data, err := json.Marshal(job)
		require.NoError(t, err)

		var unmarshaled Job
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)
		assert.Len(t, unmarshaled.Labels, 4)
	})
}
