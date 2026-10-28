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
	"encoding/json"
	"errors"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// SchedulerAdapter wraps an interfaces.SchedulerEngine to implement both the
// service.KMSScheduler and service.SchedulerService interfaces. This adapter
// converts between service-level types and scheduler package types to avoid
// circular dependencies between the service and scheduler packages.
type SchedulerAdapter struct {
	scheduler interfaces.SchedulerEngine
}

// NewSchedulerAdapter creates a new adapter for the given scheduler engine.
func NewSchedulerAdapter(s interfaces.SchedulerEngine) *SchedulerAdapter {
	return &SchedulerAdapter{scheduler: s}
}

// ScheduleJob converts the service-level job to a scheduler job and schedules it.
// This implements the KMSScheduler interface.
//
// The job is scheduled for the tenant schedulerTenantResolveID resolves from
// the caller and job.TargetTenantID: a tenant caller schedules for itself, and
// naming another tenant is refused for every caller, the system tenant
// included (tenantctx.Target). Every other outcome is an error;
// a request is never answered with success without scheduling.
func (a *SchedulerAdapter) ScheduleJob(ctx context.Context, job *SchedulerJob) error {
	tenantID, err := schedulerTenantResolveID(ctx, "ScheduleJob", job.TargetTenantID)
	if err != nil {
		return err
	}
	job.TargetTenantID = tenantID

	// Convert service.SchedulerJob to scheduler.Job
	schedulerJob := a.convertToSchedulerJob(job)

	if err := a.scheduler.ScheduleJob(ctx, schedulerJob); err != nil {
		return err
	}

	// Update the job ID from the scheduled job
	job.ID = schedulerJob.ID
	return nil
}

// CreateJob creates a new scheduled job.
func (a *SchedulerAdapter) CreateJob(ctx context.Context, req *CreateSchedulerJobRequest) (*CreateSchedulerJobResponse, error) {
	// The job's target tenant: the caller's own; naming another is refused.
	tenantID, err := schedulerTenantResolveID(ctx, "CreateJob", req.TargetTenantID)
	if err != nil {
		return nil, err
	}
	// A tenant-owned job is filed in its owner's namespace, so naming an
	// owner is naming a tenant and is held to the same rule.
	ownerID := req.OwnerID
	if req.OwnerType == SchedulerOwnerTypeTenant {
		if ownerID, err = schedulerTenantResolveID(ctx, "CreateJob", req.OwnerID); err != nil {
			return nil, err
		}
	} else if tenantID != "" {
		// A system or delegated job lives in the system namespace, so a
		// tenant caller creating one names the system tenant: refused by the
		// same rule.
		_, err := tenantctx.Target(ctx, "scheduler.CreateJob", tenantctx.SystemTenantID)
		return nil, err
	}

	// Convert request to scheduler.Job
	job := &scheduler.Job{
		Name:           req.Name,
		Description:    req.Description,
		OwnerType:      scheduler.OwnerType(req.OwnerType),
		OwnerID:        ownerID,
		TargetTenantID: tenantID,
		Type:           scheduler.JobType(req.Type),
		Schedule:       req.Schedule,
		ExecuteAt:      req.ExecuteAt,
		Delay:          req.Delay,
		Handler:        req.Handler,
		Payload:        json.RawMessage(req.Payload),
		Priority:       req.Priority,
		Timeout:        req.Timeout,
		MaxRetries:     req.MaxRetries,
		NodeAffinity:   req.NodeAffinity,
		Labels:         req.Labels,
	}

	if err := a.scheduler.ScheduleJob(ctx, job); err != nil {
		return nil, err
	}

	return &CreateSchedulerJobResponse{
		Job:       a.convertToServiceJob(job),
		Timestamp: time.Now(),
	}, nil
}

// GetJob retrieves a job by ID.
func (a *SchedulerAdapter) GetJob(ctx context.Context, req *GetSchedulerJobRequest) (*GetSchedulerJobResponse, error) {
	tenantID, err := schedulerTenantResolveID(ctx, "GetJob", req.TenantID)
	if err != nil {
		return nil, err
	}

	job, err := a.lookupJob(ctx, req.JobID, tenantID)
	if err != nil {
		return nil, err
	}

	// Enforce tenant access on the returned job
	if err := schedulerTenantEnforceAccess(tenantID, job.TargetTenantID, "GetJob"); err != nil {
		return nil, err
	}

	return &GetSchedulerJobResponse{
		Job:       a.convertToServiceJob(job),
		Timestamp: time.Now(),
	}, nil
}

// lookupJob reads jobID in tenantID's namespace. A delegated job, a system
// job acting on one tenant's data such as the KMS grace-period disable a
// tenant's RotateKey schedules, is filed in the system namespace. So a tenant
// caller whose own namespace has no such job reads the system one, and gets
// the job only when it is delegated to that tenant.
func (a *SchedulerAdapter) lookupJob(ctx context.Context, jobID uint64, tenantID string) (*scheduler.Job, error) {
	job, err := a.scheduler.GetJobWithTenant(ctx, jobID, tenantID)
	if tenantID == "" || !isSchedulerJobNotFound(err) {
		return job, err
	}

	delegated, sysErr := a.scheduler.GetJobWithTenant(ctx, jobID, "")
	if sysErr != nil {
		if isSchedulerJobNotFound(sysErr) {
			return nil, err
		}
		return nil, sysErr
	}
	if delegated.OwnerType != scheduler.OwnerTypeDelegated || delegated.TargetTenantID != tenantID {
		return nil, err
	}
	return delegated, nil
}

// isSchedulerJobNotFound reports whether err is the scheduler's missing-job
// error, which storage returns by value or by pointer.
func isSchedulerJobNotFound(err error) bool {
	var v scheduler.JobNotFoundError
	if errors.As(err, &v) {
		return true
	}
	var p *scheduler.JobNotFoundError
	return errors.As(err, &p)
}

// UpdateJob updates an existing job.
func (a *SchedulerAdapter) UpdateJob(ctx context.Context, req *UpdateSchedulerJobRequest) (*UpdateSchedulerJobResponse, error) {
	tenantID, err := schedulerTenantResolveID(ctx, "UpdateJob", req.TenantID)
	if err != nil {
		return nil, err
	}

	// Convert request to scheduler.JobUpdateParams
	updates := &scheduler.JobUpdateParams{
		Name:        req.Name,
		Description: req.Description,
		Schedule:    req.Schedule,
		ExecuteAt:   req.ExecuteAt,
		Handler:     req.Handler,
		Payload:     req.Payload,
		Priority:    req.Priority,
		Timeout:     req.Timeout,
		MaxRetries:  req.MaxRetries,
		Labels:      req.Labels,
	}

	job, err := a.scheduler.UpdateJob(ctx, req.JobID, tenantID, updates)
	if err != nil {
		return nil, err
	}

	// Enforce tenant access on the returned job
	if err := schedulerTenantEnforceAccess(tenantID, job.TargetTenantID, "UpdateJob"); err != nil {
		return nil, err
	}

	return &UpdateSchedulerJobResponse{
		Job:       a.convertToServiceJob(job),
		Timestamp: time.Now(),
	}, nil
}

// DeleteJob removes a scheduled job.
func (a *SchedulerAdapter) DeleteJob(ctx context.Context, req *DeleteSchedulerJobRequest) (*DeleteSchedulerJobResponse, error) {
	tenantID, err := schedulerTenantResolveID(ctx, "DeleteJob", req.TenantID)
	if err != nil {
		return nil, err
	}

	if err := a.scheduler.DeleteJob(ctx, req.JobID, tenantID, req.Force); err != nil {
		return nil, err
	}

	return &DeleteSchedulerJobResponse{
		JobID:     req.JobID,
		Deleted:   true,
		Timestamp: time.Now(),
	}, nil
}

// ListJobs returns jobs matching the query criteria.
func (a *SchedulerAdapter) ListJobs(ctx context.Context, req *ListSchedulerJobsRequest) (*ListSchedulerJobsResponse, error) {
	// The listing's tenant: the caller's own; naming another as owner is
	// refused.
	tenantID, err := schedulerTenantResolveID(ctx, "ListJobs", req.OwnerID)
	if err != nil {
		return nil, err
	}
	// A named target tenant is held to the same rule.
	targetTenantID, err := schedulerTenantResolveID(ctx, "ListJobs", req.TargetTenantID)
	if err != nil {
		return nil, err
	}

	// Convert request to scheduler.JobQuery
	query := &scheduler.JobQuery{
		OwnerType:      scheduler.OwnerType(req.OwnerType),
		OwnerID:        req.OwnerID,
		TargetTenantID: targetTenantID,
		Status:         scheduler.JobStatus(req.Status),
		Handler:        req.Handler,
		Labels:         req.Labels,
		Limit:          req.Limit,
		Cursor:         req.Cursor,
	}

	// Scope query to tenant in multi-tenant mode
	if tenantID != "" && query.TargetTenantID == "" {
		query.TargetTenantID = tenantID
	}

	jobs, cursor, err := a.scheduler.ListJobsWithCursor(ctx, query)
	if err != nil {
		return nil, err
	}

	// Filter jobs to only those belonging to the requesting tenant
	serviceJobs := make([]*SchedulerJob, 0, len(jobs))
	for _, job := range jobs {
		if schedulerTenantJobBelongsTo(tenantID, job.TargetTenantID) {
			serviceJobs = append(serviceJobs, a.convertToServiceJob(job))
		}
	}

	return &ListSchedulerJobsResponse{
		Jobs:       serviceJobs,
		NextCursor: cursor,
		HasMore:    cursor != "",
		TotalCount: len(serviceJobs),
		Timestamp:  time.Now(),
	}, nil
}

// TriggerJob immediately executes a job.
func (a *SchedulerAdapter) TriggerJob(ctx context.Context, req *TriggerSchedulerJobRequest) (*TriggerSchedulerJobResponse, error) {
	tenantID, err := schedulerTenantResolveID(ctx, "TriggerJob", req.TenantID)
	if err != nil {
		return nil, err
	}

	// Get the job first to verify it exists and get its current status
	job, err := a.scheduler.GetJobWithTenant(ctx, req.JobID, tenantID)
	if err != nil {
		return nil, err
	}

	// Enforce tenant access on the returned job
	if err := schedulerTenantEnforceAccess(tenantID, job.TargetTenantID, "TriggerJob"); err != nil {
		return nil, err
	}

	if err := a.scheduler.TriggerJob(ctx, req.JobID, tenantID); err != nil {
		return nil, err
	}

	// The job status will change to running once the trigger is processed
	return &TriggerSchedulerJobResponse{
		JobID:     req.JobID,
		Status:    SchedulerJobStatus(job.Status),
		Timestamp: time.Now(),
	}, nil
}

// PauseJob pauses a scheduled job.
func (a *SchedulerAdapter) PauseJob(ctx context.Context, req *PauseSchedulerJobRequest) (*PauseSchedulerJobResponse, error) {
	tenantID, err := schedulerTenantResolveID(ctx, "PauseJob", req.TenantID)
	if err != nil {
		return nil, err
	}

	oldStatus, err := a.scheduler.PauseJobWithStatus(ctx, req.JobID, tenantID)
	if err != nil {
		return nil, err
	}

	return &PauseSchedulerJobResponse{
		JobID:     req.JobID,
		OldStatus: SchedulerJobStatus(oldStatus),
		NewStatus: SchedulerJobStatusPaused,
		Timestamp: time.Now(),
	}, nil
}

// ResumeJob resumes a paused job.
func (a *SchedulerAdapter) ResumeJob(ctx context.Context, req *ResumeSchedulerJobRequest) (*ResumeSchedulerJobResponse, error) {
	tenantID, err := schedulerTenantResolveID(ctx, "ResumeJob", req.TenantID)
	if err != nil {
		return nil, err
	}

	oldStatus, err := a.scheduler.ResumeJobWithStatus(ctx, req.JobID, tenantID)
	if err != nil {
		return nil, err
	}

	return &ResumeSchedulerJobResponse{
		JobID:     req.JobID,
		OldStatus: SchedulerJobStatus(oldStatus),
		NewStatus: SchedulerJobStatusPending,
		Timestamp: time.Now(),
	}, nil
}

// CancelJob cancels a scheduled job.
func (a *SchedulerAdapter) CancelJob(ctx context.Context, req *CancelSchedulerJobRequest) (*CancelSchedulerJobResponse, error) {
	tenantID, err := schedulerTenantResolveID(ctx, "CancelJob", req.TenantID)
	if err != nil {
		return nil, err
	}

	oldStatus, err := a.scheduler.CancelJobWithTenant(ctx, req.JobID, tenantID, req.Reason)
	if err != nil {
		return nil, err
	}

	return &CancelSchedulerJobResponse{
		JobID:     req.JobID,
		OldStatus: SchedulerJobStatus(oldStatus),
		NewStatus: SchedulerJobStatusCanceled,
		Timestamp: time.Now(),
	}, nil
}

// GetJobHistory returns execution history for a job.
func (a *SchedulerAdapter) GetJobHistory(ctx context.Context, req *GetSchedulerJobHistoryRequest) (*GetSchedulerJobHistoryResponse, error) {
	tenantID, err := schedulerTenantResolveID(ctx, "GetJobHistory", req.TenantID)
	if err != nil {
		return nil, err
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	executions, err := a.scheduler.GetJobHistory(ctx, req.JobID, tenantID, limit)
	if err != nil {
		return nil, err
	}

	// Convert scheduler executions to service executions
	serviceExecs := make([]*SchedulerJobExecution, len(executions))
	for i, exec := range executions {
		serviceExecs[i] = &SchedulerJobExecution{
			ID:        exec.ID,
			JobID:     exec.JobID,
			Status:    SchedulerJobStatus(exec.Status),
			StartedAt: exec.StartedAt,
			EndedAt:   exec.EndedAt,
			Duration:  exec.Duration,
			Error:     exec.Error,
			Output:    exec.Output,
		}
	}

	// Generate cursor for pagination if we hit the limit
	var nextCursor string
	hasMore := false
	if len(serviceExecs) >= limit {
		hasMore = true
		if len(serviceExecs) > 0 {
			lastExec := serviceExecs[len(serviceExecs)-1]
			nextCursor = string(rune(lastExec.ID))
		}
	}

	return &GetSchedulerJobHistoryResponse{
		Executions: serviceExecs,
		NextCursor: nextCursor,
		HasMore:    hasMore,
		Timestamp:  time.Now(),
	}, nil
}

// GetStatus returns scheduler status and statistics.
func (a *SchedulerAdapter) GetStatus(ctx context.Context) (*GetSchedulerStatusResponse, error) {
	stats := a.scheduler.Stats()
	running := a.scheduler.IsRunning()

	return &GetSchedulerStatusResponse{
		Running: running,
		Stats: &SchedulerStats{
			JobCount:     stats.JobCount,
			RunningJobs:  stats.RunningJobs,
			SuccessCount: stats.SuccessCount,
			FailureCount: stats.FailureCount,
			LastTickTime: stats.LastTickTime,
			StartedAt:    stats.StartedAt,
		},
		Timestamp: time.Now(),
	}, nil
}

// convertToSchedulerJob converts a service.SchedulerJob to scheduler.Job.
func (a *SchedulerAdapter) convertToSchedulerJob(job *SchedulerJob) *scheduler.Job {
	return &scheduler.Job{
		ID:             job.ID,
		Name:           job.Name,
		Description:    job.Description,
		OwnerType:      scheduler.OwnerType(job.OwnerType),
		OwnerID:        job.OwnerID,
		TargetTenantID: job.TargetTenantID,
		CreatedBy:      job.CreatedBy,
		Type:           scheduler.JobType(job.Type),
		Schedule:       job.Schedule,
		ExecuteAt:      job.ExecuteAt,
		Delay:          job.Delay,
		Handler:        job.Handler,
		Payload:        json.RawMessage(job.Payload),
		Priority:       job.Priority,
		Timeout:        job.Timeout,
		MaxRetries:     job.MaxRetries,
		Status:         scheduler.JobStatus(job.Status),
		LastRun:        job.LastRun,
		NextRun:        job.NextRun,
		RunCount:       job.RunCount,
		FailCount:      job.FailCount,
		LastError:      job.LastError,
		Labels:         job.Labels,
		CreatedAt:      job.CreatedAt,
		UpdatedAt:      job.UpdatedAt,
	}
}

// convertToServiceJob converts a scheduler.Job to service.SchedulerJob.
func (a *SchedulerAdapter) convertToServiceJob(job *scheduler.Job) *SchedulerJob {
	return &SchedulerJob{
		ID:             job.ID,
		Name:           job.Name,
		Description:    job.Description,
		OwnerType:      SchedulerOwnerType(job.OwnerType),
		OwnerID:        job.OwnerID,
		TargetTenantID: job.TargetTenantID,
		CreatedBy:      job.CreatedBy,
		Type:           SchedulerJobType(job.Type),
		Schedule:       job.Schedule,
		ExecuteAt:      job.ExecuteAt,
		Delay:          job.Delay,
		Handler:        job.Handler,
		Payload:        []byte(job.Payload),
		Priority:       job.Priority,
		Timeout:        job.Timeout,
		MaxRetries:     job.MaxRetries,
		Status:         SchedulerJobStatus(job.Status),
		LastRun:        job.LastRun,
		NextRun:        job.NextRun,
		RunCount:       job.RunCount,
		FailCount:      job.FailCount,
		LastError:      job.LastError,
		Labels:         job.Labels,
		CreatedAt:      job.CreatedAt,
		UpdatedAt:      job.UpdatedAt,
	}
}

// gracePeriodPayload is used to unmarshal grace period job payloads.
type gracePeriodPayload struct {
	KeyID   string `json:"key_id"`
	Version uint64 `json:"version"`
}

// GetCompletedGracePeriodVersions queries the Raft-replicated scheduler storage
// for completed grace period jobs matching the given key ID. This provides
// cross-node visibility since scheduler storage is read via SyncRead.
func (a *SchedulerAdapter) GetCompletedGracePeriodVersions(ctx context.Context, keyID string) (map[uint64]time.Time, error) {
	query := &scheduler.JobQuery{
		Handler: "kms.grace_period",
		Status:  scheduler.JobStatusCompleted,
		Limit:   100,
	}

	jobs, _, err := a.scheduler.ListJobsWithCursor(ctx, query)
	if err != nil {
		return nil, err
	}

	result := make(map[uint64]time.Time)
	for _, job := range jobs {
		var payload gracePeriodPayload
		if json.Unmarshal(job.Payload, &payload) != nil {
			continue
		}
		if payload.KeyID == keyID {
			result[payload.Version] = job.LastRun
		}
	}
	return result, nil
}

// Compile-time interface satisfaction checks.
var (
	// Verify SchedulerAdapter implements KMSScheduler interface.
	_ KMSScheduler = (*SchedulerAdapter)(nil)

	// Verify SchedulerAdapter implements SchedulerService interface.
	_ SchedulerService = (*SchedulerAdapter)(nil)

	// Verify SchedulerAdapter implements KMSSchedulerQuerier interface.
	_ KMSSchedulerQuerier = (*SchedulerAdapter)(nil)
)
