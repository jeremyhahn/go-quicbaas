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

package interfaces

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// Compile-time interface satisfaction check.
// This is placed here (where the interface is defined) rather than in the
// scheduler package to avoid an import cycle.
var _ SchedulerEngine = (*scheduler.SchedulerEngine)(nil)

// SchedulerEngine defines the interface for the scheduler engine operations
// used by the service layer. This interface provides job scheduling, lifecycle
// management, and handler registration capabilities.
//
// The interface is defined where it's consumed (pkg/interfaces) following Go
// best practices. It includes only the methods actually used by the service
// layer to keep the interface minimal and focused.
type SchedulerEngine interface {
	// RegisterHandler registers a job handler function for a given handler name.
	// Returns an error if a handler with the same name already exists.
	RegisterHandler(name string, handler scheduler.JobHandler) error

	// ScheduleJob schedules a new job for execution. The job ID is assigned
	// and set on the job struct upon successful scheduling.
	ScheduleJob(ctx context.Context, job *scheduler.Job) error

	// GetJobWithTenant retrieves a job by ID with tenant isolation.
	// Returns an error if the job is not found or belongs to a different tenant.
	GetJobWithTenant(ctx context.Context, jobID uint64, tenantID string) (*scheduler.Job, error)

	// UpdateJob updates an existing job with the provided parameters.
	// Returns the updated job or an error if the job is not found.
	UpdateJob(ctx context.Context, jobID uint64, tenantID string, updates *scheduler.JobUpdateParams) (*scheduler.Job, error)

	// DeleteJob removes a scheduled job. If force is true, running jobs will
	// be canceled before deletion.
	DeleteJob(ctx context.Context, jobID uint64, tenantID string, force bool) error

	// ListJobsWithCursor returns jobs matching the query criteria with cursor-based pagination.
	// Returns the jobs, a cursor for the next page, and any error.
	ListJobsWithCursor(ctx context.Context, query *scheduler.JobQuery) ([]*scheduler.Job, string, error)

	// TriggerJob immediately triggers execution of a scheduled job.
	// The job must exist and be in a triggerable state.
	TriggerJob(ctx context.Context, jobID uint64, tenantID string) error

	// PauseJobWithStatus pauses a scheduled job and returns its previous status.
	// Returns an error if the job is not found or already paused.
	PauseJobWithStatus(ctx context.Context, jobID uint64, tenantID string) (scheduler.JobStatus, error)

	// ResumeJobWithStatus resumes a paused job and returns its previous status.
	// Returns an error if the job is not found or not paused.
	ResumeJobWithStatus(ctx context.Context, jobID uint64, tenantID string) (scheduler.JobStatus, error)

	// CancelJobWithTenant cancels a job with tenant isolation and a cancellation reason.
	// Returns the previous job status or an error if the job is not found.
	CancelJobWithTenant(ctx context.Context, jobID uint64, tenantID string, reason string) (scheduler.JobStatus, error)

	// GetJobHistory returns the execution history for a job.
	// The limit parameter controls the maximum number of executions returned.
	GetJobHistory(ctx context.Context, jobID uint64, tenantID string, limit int) ([]*scheduler.JobExecution, error)

	// Stats returns current scheduler runtime statistics.
	Stats() *scheduler.SchedulerStats

	// IsRunning returns true if the scheduler is currently running.
	IsRunning() bool

	// Start begins the scheduler's job processing loop.
	// Returns an error if the scheduler is already running.
	Start(ctx context.Context) error

	// Stop gracefully stops the scheduler, waiting for running jobs to complete.
	Stop(ctx context.Context) error
}
