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

package service

import (
	"context"
	"time"
)

// =============================================================================
// Scheduler Service - Job Scheduling and Execution
// =============================================================================

// SchedulerService handles job scheduling and execution.
// This service provides operations for creating, managing, and executing
// scheduled jobs with multi-tenant support and access control.
type SchedulerService interface {
	// CreateJob creates a new scheduled job.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Create job request with scheduling parameters
	// Returns:
	//   - CreateSchedulerJobResponse: Contains created job and timestamp
	//   - error: ErrInvalidInput, ErrUnauthorized, ErrTimeout
	CreateJob(ctx context.Context, req *CreateSchedulerJobRequest) (*CreateSchedulerJobResponse, error)

	// GetJob retrieves a job by ID.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get job request with job ID
	// Returns:
	//   - GetSchedulerJobResponse: Contains job details and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	GetJob(ctx context.Context, req *GetSchedulerJobRequest) (*GetSchedulerJobResponse, error)

	// UpdateJob updates an existing job.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Update job request with job ID and updated fields
	// Returns:
	//   - UpdateSchedulerJobResponse: Contains updated job and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrInvalidInput, ErrTimeout
	UpdateJob(ctx context.Context, req *UpdateSchedulerJobRequest) (*UpdateSchedulerJobResponse, error)

	// DeleteJob removes a scheduled job.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Delete job request with job ID
	// Returns:
	//   - DeleteSchedulerJobResponse: Contains deletion status and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	DeleteJob(ctx context.Context, req *DeleteSchedulerJobRequest) (*DeleteSchedulerJobResponse, error)

	// ListJobs returns jobs matching the query criteria.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List jobs request with optional filters and pagination
	// Returns:
	//   - ListSchedulerJobsResponse: Contains job list and pagination info
	//   - error: ErrUnauthorized, ErrTimeout
	ListJobs(ctx context.Context, req *ListSchedulerJobsRequest) (*ListSchedulerJobsResponse, error)

	// TriggerJob immediately executes a job.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Trigger job request with job ID
	// Returns:
	//   - TriggerSchedulerJobResponse: Contains execution status and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrInvalidInput, ErrTimeout
	TriggerJob(ctx context.Context, req *TriggerSchedulerJobRequest) (*TriggerSchedulerJobResponse, error)

	// PauseJob pauses a scheduled job.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Pause job request with job ID
	// Returns:
	//   - PauseSchedulerJobResponse: Contains old and new status with timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrInvalidInput, ErrTimeout
	PauseJob(ctx context.Context, req *PauseSchedulerJobRequest) (*PauseSchedulerJobResponse, error)

	// ResumeJob resumes a paused job.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Resume job request with job ID
	// Returns:
	//   - ResumeSchedulerJobResponse: Contains old and new status with timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrInvalidInput, ErrTimeout
	ResumeJob(ctx context.Context, req *ResumeSchedulerJobRequest) (*ResumeSchedulerJobResponse, error)

	// CancelJob cancels a scheduled job.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Cancel job request with job ID and optional reason
	// Returns:
	//   - CancelSchedulerJobResponse: Contains old and new status with timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrInvalidInput, ErrTimeout
	CancelJob(ctx context.Context, req *CancelSchedulerJobRequest) (*CancelSchedulerJobResponse, error)

	// GetJobHistory returns execution history for a job.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get job history request with job ID and pagination
	// Returns:
	//   - GetSchedulerJobHistoryResponse: Contains execution records and pagination info
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	GetJobHistory(ctx context.Context, req *GetSchedulerJobHistoryRequest) (*GetSchedulerJobHistoryResponse, error)

	// GetStatus returns scheduler status and statistics.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	// Returns:
	//   - GetSchedulerStatusResponse: Contains running state, stats, and timestamp
	//   - error: ErrTimeout
	GetStatus(ctx context.Context) (*GetSchedulerStatusResponse, error)
}

// =============================================================================
// Scheduler Types
// =============================================================================

// SchedulerJobType defines the scheduling type.
type SchedulerJobType string

const (
	// SchedulerJobTypeOneShot executes exactly once at the specified time.
	SchedulerJobTypeOneShot SchedulerJobType = "one-shot"
	// SchedulerJobTypeRecurring executes repeatedly according to a schedule.
	SchedulerJobTypeRecurring SchedulerJobType = "recurring"
	// SchedulerJobTypeDelayed executes once after a specified delay.
	SchedulerJobTypeDelayed SchedulerJobType = "delayed"
)

// SchedulerJobStatus defines the job lifecycle state.
type SchedulerJobStatus string

const (
	// SchedulerJobStatusPending indicates the job is waiting to be executed.
	SchedulerJobStatusPending SchedulerJobStatus = "pending"
	// SchedulerJobStatusRunning indicates the job is currently executing.
	SchedulerJobStatusRunning SchedulerJobStatus = "running"
	// SchedulerJobStatusCompleted indicates the job has finished successfully.
	SchedulerJobStatusCompleted SchedulerJobStatus = "completed"
	// SchedulerJobStatusFailed indicates the job has failed.
	SchedulerJobStatusFailed SchedulerJobStatus = "failed"
	// SchedulerJobStatusPaused indicates the job is temporarily suspended.
	SchedulerJobStatusPaused SchedulerJobStatus = "paused"
	// SchedulerJobStatusCanceled indicates the job was canceled.
	SchedulerJobStatusCanceled SchedulerJobStatus = "canceled"
)

// SchedulerOwnerType defines job ownership.
type SchedulerOwnerType string

const (
	// SchedulerOwnerTypeSystem indicates a system-owned job.
	SchedulerOwnerTypeSystem SchedulerOwnerType = "system"
	// SchedulerOwnerTypeTenant indicates a tenant-owned job.
	SchedulerOwnerTypeTenant SchedulerOwnerType = "tenant"
	// SchedulerOwnerTypeDelegated indicates a delegated job.
	SchedulerOwnerTypeDelegated SchedulerOwnerType = "delegated"
)

// SchedulerJob represents a scheduled job.
type SchedulerJob struct {
	ID             uint64             `json:"id"`
	Name           string             `json:"name"`
	Description    string             `json:"description,omitempty"`
	OwnerType      SchedulerOwnerType `json:"owner_type"`
	OwnerID        string             `json:"owner_id,omitempty"`
	TargetTenantID string             `json:"target_tenant_id,omitempty"`
	CreatedBy      string             `json:"created_by,omitempty"`
	Type           SchedulerJobType   `json:"type"`
	Schedule       string             `json:"schedule,omitempty"`
	ExecuteAt      time.Time          `json:"execute_at"`
	Delay          time.Duration      `json:"delay,omitempty"`
	Handler        string             `json:"handler"`
	Payload        []byte             `json:"payload,omitempty"`
	Priority       int32              `json:"priority"`
	Timeout        time.Duration      `json:"timeout"`
	MaxRetries     int32              `json:"max_retries"`
	Status         SchedulerJobStatus `json:"status"`
	LastRun        time.Time          `json:"last_run,omitempty"`
	NextRun        time.Time          `json:"next_run,omitempty"`
	RunCount       uint64             `json:"run_count"`
	FailCount      uint64             `json:"fail_count"`
	LastError      string             `json:"last_error,omitempty"`
	Labels         map[string]string  `json:"labels,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

// SchedulerJobExecution represents a job execution record.
type SchedulerJobExecution struct {
	ID        uint64             `json:"id"`
	JobID     uint64             `json:"job_id"`
	Status    SchedulerJobStatus `json:"status"`
	StartedAt time.Time          `json:"started_at"`
	EndedAt   time.Time          `json:"ended_at,omitempty"`
	Duration  time.Duration      `json:"duration,omitempty"`
	Error     string             `json:"error,omitempty"`
	Output    string             `json:"output,omitempty"`
}

// SchedulerStats contains scheduler statistics.
type SchedulerStats struct {
	JobCount     uint64    `json:"job_count"`
	RunningJobs  uint64    `json:"running_jobs"`
	SuccessCount uint64    `json:"success_count"`
	FailureCount uint64    `json:"failure_count"`
	LastTickTime time.Time `json:"last_tick_time,omitempty"`
	StartedAt    time.Time `json:"started_at"`
}

// =============================================================================
// Request/Response Types
// =============================================================================

// CreateSchedulerJobRequest contains parameters for creating a job.
type CreateSchedulerJobRequest struct {
	Name           string             `json:"name"`
	Description    string             `json:"description,omitempty"`
	OwnerType      SchedulerOwnerType `json:"owner_type"`
	OwnerID        string             `json:"owner_id,omitempty"`
	TargetTenantID string             `json:"target_tenant_id,omitempty"`
	Type           SchedulerJobType   `json:"type"`
	Schedule       string             `json:"schedule,omitempty"`
	ExecuteAt      time.Time          `json:"execute_at,omitempty"`
	Delay          time.Duration      `json:"delay,omitempty"`
	Handler        string             `json:"handler"`
	Payload        []byte             `json:"payload,omitempty"`
	Priority       int32              `json:"priority,omitempty"`
	Timeout        time.Duration      `json:"timeout,omitempty"`
	MaxRetries     int32              `json:"max_retries,omitempty"`
	Labels         map[string]string  `json:"labels,omitempty"`

	// NodeAffinity restricts execution to a specific node. Empty
	// (default) makes the job cluster-wide — executes on the Raft
	// leader only. Non-empty pins execution to the node whose
	// SchedulerEngine was constructed with a matching node ID,
	// regardless of leadership. See scheduler.Job.NodeAffinity for
	// full semantics.
	NodeAffinity string `json:"node_affinity,omitempty"`
}

// CreateSchedulerJobResponse contains the result of creating a job.
type CreateSchedulerJobResponse struct {
	Job       *SchedulerJob `json:"job"`
	Timestamp time.Time     `json:"timestamp"`
}

// GetSchedulerJobRequest contains parameters for retrieving a job.
type GetSchedulerJobRequest struct {
	JobID    uint64 `json:"job_id"`
	TenantID string `json:"tenant_id,omitempty"`
}

// GetSchedulerJobResponse contains the result of retrieving a job.
type GetSchedulerJobResponse struct {
	Job       *SchedulerJob `json:"job"`
	Timestamp time.Time     `json:"timestamp"`
}

// UpdateSchedulerJobRequest contains parameters for updating a job.
type UpdateSchedulerJobRequest struct {
	JobID       uint64            `json:"job_id"`
	TenantID    string            `json:"tenant_id,omitempty"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Schedule    string            `json:"schedule,omitempty"`
	ExecuteAt   time.Time         `json:"execute_at,omitempty"`
	Handler     string            `json:"handler,omitempty"`
	Payload     []byte            `json:"payload,omitempty"`
	Priority    int32             `json:"priority,omitempty"`
	Timeout     time.Duration     `json:"timeout,omitempty"`
	MaxRetries  int32             `json:"max_retries,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// UpdateSchedulerJobResponse contains the result of updating a job.
type UpdateSchedulerJobResponse struct {
	Job       *SchedulerJob `json:"job"`
	Timestamp time.Time     `json:"timestamp"`
}

// DeleteSchedulerJobRequest contains parameters for deleting a job.
type DeleteSchedulerJobRequest struct {
	JobID    uint64 `json:"job_id"`
	TenantID string `json:"tenant_id,omitempty"`
	Force    bool   `json:"force,omitempty"`
}

// DeleteSchedulerJobResponse contains the result of deleting a job.
type DeleteSchedulerJobResponse struct {
	JobID     uint64    `json:"job_id"`
	Deleted   bool      `json:"deleted"`
	Timestamp time.Time `json:"timestamp"`
}

// ListSchedulerJobsRequest contains parameters for listing jobs.
type ListSchedulerJobsRequest struct {
	OwnerType      SchedulerOwnerType `json:"owner_type,omitempty"`
	OwnerID        string             `json:"owner_id,omitempty"`
	TargetTenantID string             `json:"target_tenant_id,omitempty"`
	Status         SchedulerJobStatus `json:"status,omitempty"`
	Handler        string             `json:"handler,omitempty"`
	Labels         map[string]string  `json:"labels,omitempty"`
	Limit          int                `json:"limit,omitempty"`
	Cursor         string             `json:"cursor,omitempty"`
}

// ListSchedulerJobsResponse contains the result of listing jobs.
type ListSchedulerJobsResponse struct {
	Jobs       []*SchedulerJob `json:"jobs"`
	NextCursor string          `json:"next_cursor,omitempty"`
	HasMore    bool            `json:"has_more"`
	TotalCount int             `json:"total_count,omitempty"`
	Timestamp  time.Time       `json:"timestamp"`
}

// TriggerSchedulerJobRequest contains parameters for triggering a job.
type TriggerSchedulerJobRequest struct {
	JobID    uint64 `json:"job_id"`
	TenantID string `json:"tenant_id,omitempty"`
}

// TriggerSchedulerJobResponse contains the result of triggering a job.
type TriggerSchedulerJobResponse struct {
	JobID     uint64             `json:"job_id"`
	Status    SchedulerJobStatus `json:"status"`
	Timestamp time.Time          `json:"timestamp"`
}

// PauseSchedulerJobRequest contains parameters for pausing a job.
type PauseSchedulerJobRequest struct {
	JobID    uint64 `json:"job_id"`
	TenantID string `json:"tenant_id,omitempty"`
}

// PauseSchedulerJobResponse contains the result of pausing a job.
type PauseSchedulerJobResponse struct {
	JobID     uint64             `json:"job_id"`
	OldStatus SchedulerJobStatus `json:"old_status"`
	NewStatus SchedulerJobStatus `json:"new_status"`
	Timestamp time.Time          `json:"timestamp"`
}

// ResumeSchedulerJobRequest contains parameters for resuming a job.
type ResumeSchedulerJobRequest struct {
	JobID    uint64 `json:"job_id"`
	TenantID string `json:"tenant_id,omitempty"`
}

// ResumeSchedulerJobResponse contains the result of resuming a job.
type ResumeSchedulerJobResponse struct {
	JobID     uint64             `json:"job_id"`
	OldStatus SchedulerJobStatus `json:"old_status"`
	NewStatus SchedulerJobStatus `json:"new_status"`
	Timestamp time.Time          `json:"timestamp"`
}

// CancelSchedulerJobRequest contains parameters for canceling a job.
type CancelSchedulerJobRequest struct {
	JobID    uint64 `json:"job_id"`
	TenantID string `json:"tenant_id,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// CancelSchedulerJobResponse contains the result of canceling a job.
type CancelSchedulerJobResponse struct {
	JobID     uint64             `json:"job_id"`
	OldStatus SchedulerJobStatus `json:"old_status"`
	NewStatus SchedulerJobStatus `json:"new_status"`
	Timestamp time.Time          `json:"timestamp"`
}

// GetSchedulerJobHistoryRequest contains parameters for retrieving job history.
type GetSchedulerJobHistoryRequest struct {
	JobID    uint64 `json:"job_id"`
	TenantID string `json:"tenant_id,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Cursor   string `json:"cursor,omitempty"`
}

// GetSchedulerJobHistoryResponse contains the result of retrieving job history.
type GetSchedulerJobHistoryResponse struct {
	Executions []*SchedulerJobExecution `json:"executions"`
	NextCursor string                   `json:"next_cursor,omitempty"`
	HasMore    bool                     `json:"has_more"`
	Timestamp  time.Time                `json:"timestamp"`
}

// GetSchedulerStatusResponse contains the result of retrieving scheduler status.
type GetSchedulerStatusResponse struct {
	Running   bool            `json:"running"`
	Stats     *SchedulerStats `json:"stats"`
	Timestamp time.Time       `json:"timestamp"`
}
