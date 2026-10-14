// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// The SDK job types predate the server's service-layer scheduler types and
// differ from them in shape (enums vs strings, Tags vs Labels, JobID vs ID).
// These conversions are the ONE mapping between the two, shared by every
// transport that reaches the server's SchedulerService through its
// service-layer types: gRPC and Unix (through the pkg/api/grpc client),
// REST and QUIC (whose JSON mirrors the service types), and the in-process
// embedded transport. A field mapped here is mapped identically on all of
// them.

// jobTypeToService maps an SDK job type to the service type.
var jobTypeToService = map[JobType]service.SchedulerJobType{
	JobTypeOneShot:   service.SchedulerJobTypeOneShot,
	JobTypeRecurring: service.SchedulerJobTypeRecurring,
	JobTypeDelayed:   service.SchedulerJobTypeDelayed,
}

// jobTypeFromService maps a service job type to the SDK type.
var jobTypeFromService = map[service.SchedulerJobType]JobType{
	service.SchedulerJobTypeOneShot:   JobTypeOneShot,
	service.SchedulerJobTypeRecurring: JobTypeRecurring,
	service.SchedulerJobTypeDelayed:   JobTypeDelayed,
}

// ownerTypeToService maps an SDK owner type to the service type.
var ownerTypeToService = map[OwnerType]service.SchedulerOwnerType{
	OwnerTypeSystem:    service.SchedulerOwnerTypeSystem,
	OwnerTypeTenant:    service.SchedulerOwnerTypeTenant,
	OwnerTypeDelegated: service.SchedulerOwnerTypeDelegated,
}

// ownerTypeFromService maps a service owner type to the SDK type.
var ownerTypeFromService = map[service.SchedulerOwnerType]OwnerType{
	service.SchedulerOwnerTypeSystem:    OwnerTypeSystem,
	service.SchedulerOwnerTypeTenant:    OwnerTypeTenant,
	service.SchedulerOwnerTypeDelegated: OwnerTypeDelegated,
}

// statusToService maps an SDK job status to the service status.
var statusToService = map[JobStatus]service.SchedulerJobStatus{
	JobStatusPending:   service.SchedulerJobStatusPending,
	JobStatusRunning:   service.SchedulerJobStatusRunning,
	JobStatusCompleted: service.SchedulerJobStatusCompleted,
	JobStatusFailed:    service.SchedulerJobStatusFailed,
	JobStatusPaused:    service.SchedulerJobStatusPaused,
	JobStatusCanceled:  service.SchedulerJobStatusCanceled,
}

// statusFromService maps a service job status to the SDK status.
var statusFromService = map[service.SchedulerJobStatus]JobStatus{
	service.SchedulerJobStatusPending:   JobStatusPending,
	service.SchedulerJobStatusRunning:   JobStatusRunning,
	service.SchedulerJobStatusCompleted: JobStatusCompleted,
	service.SchedulerJobStatusFailed:    JobStatusFailed,
	service.SchedulerJobStatusPaused:    JobStatusPaused,
	service.SchedulerJobStatusCanceled:  JobStatusCanceled,
}

// CreateJobRequestToService converts a CreateJob request.
func CreateJobRequestToService(req *CreateJobRequest) *service.CreateSchedulerJobRequest {
	return &service.CreateSchedulerJobRequest{
		Name:         req.Name,
		Description:  req.Description,
		OwnerType:    ownerTypeToService[req.OwnerType],
		OwnerID:      req.OwnerID,
		Type:         jobTypeToService[req.JobType],
		Schedule:     req.Schedule,
		ExecuteAt:    req.ExecuteAt,
		Delay:        req.Delay,
		Handler:      req.Handler,
		Payload:      req.Payload,
		Priority:     int32(req.Priority),
		Timeout:      req.Timeout,
		MaxRetries:   int32(req.MaxRetries),
		Labels:       req.Tags,
		NodeAffinity: req.NodeAffinity,
	}
}

// UpdateJobRequestToService converts an UpdateJob request. The service
// update carries no delay, so UpdateJobRequest.Delay is not sent.
func UpdateJobRequestToService(req *UpdateJobRequest) *service.UpdateSchedulerJobRequest {
	return &service.UpdateSchedulerJobRequest{
		JobID:       req.JobID,
		Name:        req.Name,
		Description: req.Description,
		Schedule:    req.Schedule,
		Payload:     req.Payload,
		Timeout:     req.Timeout,
		MaxRetries:  int32(req.MaxRetries),
		Labels:      req.Tags,
	}
}

// ListJobsOptionsToService converts list options. The service filters by
// owner, status, handler and labels and pages by limit and cursor; JobType
// and Offset have no server-side counterpart and are not sent.
func ListJobsOptionsToService(opts *ListJobsOptions) *service.ListSchedulerJobsRequest {
	out := &service.ListSchedulerJobsRequest{}
	if opts == nil {
		return out
	}
	out.OwnerID = opts.OwnerID
	out.Labels = opts.Tags
	out.Limit = opts.Limit
	out.Handler = opts.Handler
	out.Cursor = opts.Cursor
	if opts.OwnerType != nil {
		out.OwnerType = ownerTypeToService[*opts.OwnerType]
	}
	if opts.Status != nil {
		out.Status = statusToService[*opts.Status]
	}
	return out
}

// HistoryLimit returns the history page size the options ask for.
func HistoryLimit(opts *HistoryOptions) int {
	if opts == nil {
		return 0
	}
	return opts.Limit
}

// JobInfoFromService converts a service job.
func JobInfoFromService(job *service.SchedulerJob) *JobInfo {
	if job == nil {
		return nil
	}
	return &JobInfo{
		JobID:          job.ID,
		Name:           job.Name,
		Description:    job.Description,
		JobType:        jobTypeFromService[job.Type],
		Status:         statusFromService[job.Status],
		Schedule:       job.Schedule,
		Delay:          job.Delay,
		Payload:        job.Payload,
		OwnerType:      ownerTypeFromService[job.OwnerType],
		OwnerID:        job.OwnerID,
		MaxRetries:     int(job.MaxRetries),
		Timeout:        job.Timeout,
		NextRunAt:      job.NextRun,
		LastRunAt:      job.LastRun,
		CreatedAt:      job.CreatedAt,
		UpdatedAt:      job.UpdatedAt,
		ExecutionCount: job.RunCount,
		FailureCount:   job.FailCount,
		Tags:           job.Labels,
		Handler:        job.Handler,
		Priority:       int(job.Priority),
		LastError:      job.LastError,
	}
}

// CreateJobResponseFromService converts a CreateJob response.
func CreateJobResponseFromService(resp *service.CreateSchedulerJobResponse) *CreateJobResponse {
	out := &CreateJobResponse{}
	if resp != nil && resp.Job != nil {
		out.JobID = resp.Job.ID
		out.NextRunAt = resp.Job.NextRun
	}
	return out
}

// ListJobsResponseFromService converts a ListJobs response.
func ListJobsResponseFromService(resp *service.ListSchedulerJobsResponse) *ListJobsResponse {
	out := &ListJobsResponse{Jobs: []JobInfo{}}
	if resp == nil {
		return out
	}
	for _, job := range resp.Jobs {
		if info := JobInfoFromService(job); info != nil {
			out.Jobs = append(out.Jobs, *info)
		}
	}
	out.HasMore = resp.HasMore
	out.NextCursor = resp.NextCursor
	out.TotalCount = resp.TotalCount
	if out.TotalCount == 0 {
		out.TotalCount = len(out.Jobs)
	}
	return out
}

// TriggerJobResponseFromService converts a TriggerJob response.
func TriggerJobResponseFromService(resp *service.TriggerSchedulerJobResponse) *TriggerJobResponse {
	return &TriggerJobResponse{JobID: resp.JobID, StartedAt: resp.Timestamp, Status: statusFromService[resp.Status]}
}

// PauseJobResponseFromService converts a PauseJob response.
func PauseJobResponseFromService(resp *service.PauseSchedulerJobResponse) *PauseJobResponse {
	return &PauseJobResponse{
		JobID:     resp.JobID,
		PausedAt:  resp.Timestamp,
		OldStatus: statusFromService[resp.OldStatus],
		NewStatus: statusFromService[resp.NewStatus],
	}
}

// ResumeJobResponseFromService converts a ResumeJob response.
func ResumeJobResponseFromService(resp *service.ResumeSchedulerJobResponse) *ResumeJobResponse {
	return &ResumeJobResponse{
		JobID:     resp.JobID,
		ResumedAt: resp.Timestamp,
		OldStatus: statusFromService[resp.OldStatus],
		NewStatus: statusFromService[resp.NewStatus],
	}
}

// CancelJobResponseFromService converts a CancelJob response; the reason is
// the one the caller sent, which the service does not echo.
func CancelJobResponseFromService(resp *service.CancelSchedulerJobResponse, reason string) *CancelJobResponse {
	return &CancelJobResponse{
		JobID:      resp.JobID,
		CanceledAt: resp.Timestamp,
		Reason:     reason,
		OldStatus:  statusFromService[resp.OldStatus],
		NewStatus:  statusFromService[resp.NewStatus],
	}
}

// DeleteJobResponseFromService converts a DeleteJob response.
func DeleteJobResponseFromService(resp *service.DeleteSchedulerJobResponse) *DeleteJobResponse {
	if resp == nil {
		return &DeleteJobResponse{}
	}
	return &DeleteJobResponse{JobID: resp.JobID, Deleted: resp.Deleted, DeletedAt: resp.Timestamp}
}

// JobHistoryResponseFromService converts a GetJobHistory response.
func JobHistoryResponseFromService(jobID uint64, resp *service.GetSchedulerJobHistoryResponse) *JobHistoryResponse {
	out := &JobHistoryResponse{JobID: jobID, Executions: []JobExecution{}}
	if resp == nil {
		return out
	}
	for _, exec := range resp.Executions {
		if exec == nil {
			continue
		}
		out.Executions = append(out.Executions, JobExecution{
			ExecutionID: exec.ID,
			JobID:       exec.JobID,
			Status:      statusFromService[exec.Status],
			StartedAt:   exec.StartedAt,
			CompletedAt: exec.EndedAt,
			Duration:    exec.Duration,
			Error:       exec.Error,
			Result:      []byte(exec.Output),
		})
	}
	out.TotalCount = len(out.Executions)
	out.HasMore = resp.HasMore
	return out
}

// SchedulerStatusFromService converts a GetStatus response, deriving the
// uptime from the engine's start time at now.
func SchedulerStatusFromService(resp *service.GetSchedulerStatusResponse, now time.Time) *SchedulerStatus {
	out := &SchedulerStatus{}
	if resp == nil {
		return out
	}
	out.Healthy = resp.Running
	if stats := resp.Stats; stats != nil {
		out.TotalJobs = stats.JobCount
		out.RunningJobs = stats.RunningJobs
		out.CompletedJobs = stats.SuccessCount
		out.FailedJobs = stats.FailureCount
		out.LastHeartbeat = stats.LastTickTime
		if !stats.StartedAt.IsZero() {
			out.UptimeDuration = now.Sub(stats.StartedAt)
		}
	}
	return out
}
