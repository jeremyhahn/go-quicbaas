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
	"context"
	stderrors "errors"
	"time"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// SchedulerCreateJob creates a new scheduled job.
func (c *Client) SchedulerCreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.SchedulerCreateJob",
			Err:  ErrRequestNil,
		}
	}

	schedulerClient := pb.NewSchedulerServiceClient(c.conn)

	pbReq := &pb.CreateJobRequest{
		Name:           req.Name,
		Description:    req.Description,
		OwnerType:      convertServiceToProtoOwnerType(req.OwnerType),
		OwnerId:        req.OwnerID,
		TargetTenantId: req.TargetTenantID,
		Type:           convertServiceToProtoJobType(req.Type),
		Schedule:       req.Schedule,
		ExecuteAt:      req.ExecuteAt.Unix(),
		DelaySeconds:   int64(req.Delay.Seconds()),
		Handler:        req.Handler,
		Payload:        req.Payload,
		Priority:       req.Priority,
		TimeoutSeconds: int64(req.Timeout.Seconds()),
		MaxRetries:     req.MaxRetries,
		Labels:         req.Labels,
		NodeAffinity:   req.NodeAffinity,
	}

	pbResp, err := schedulerClient.CreateJob(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.CreateSchedulerJobResponse{
		Job:       convertProtoJob(pbResp.Job),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// SchedulerGetJob retrieves a job by ID.
func (c *Client) SchedulerGetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.SchedulerGetJob",
			Err:  ErrRequestNil,
		}
	}

	scope, err := serviceJobScopeToProto("Client.SchedulerGetJob", req.Scope)
	if err != nil {
		return nil, err
	}

	schedulerClient := pb.NewSchedulerServiceClient(c.conn)

	pbReq := &pb.GetJobRequest{
		JobId:    req.JobID,
		TenantId: req.TenantID,
		Scope:    scope,
	}

	pbResp, err := schedulerClient.GetJob(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.GetSchedulerJobResponse{
		Job:       convertProtoJob(pbResp.Job),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// SchedulerUpdateJob updates an existing job.
func (c *Client) SchedulerUpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.SchedulerUpdateJob",
			Err:  ErrRequestNil,
		}
	}

	scope, err := serviceJobScopeToProto("Client.SchedulerUpdateJob", req.Scope)
	if err != nil {
		return nil, err
	}

	schedulerClient := pb.NewSchedulerServiceClient(c.conn)

	pbReq := &pb.UpdateJobRequest{
		JobId:          req.JobID,
		TenantId:       req.TenantID,
		Scope:          scope,
		Name:           req.Name,
		Description:    req.Description,
		Schedule:       req.Schedule,
		ExecuteAt:      req.ExecuteAt.Unix(),
		Handler:        req.Handler,
		Payload:        req.Payload,
		Priority:       req.Priority,
		TimeoutSeconds: int64(req.Timeout.Seconds()),
		MaxRetries:     req.MaxRetries,
		Labels:         req.Labels,
	}

	pbResp, err := schedulerClient.UpdateJob(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.UpdateSchedulerJobResponse{
		Job:       convertProtoJob(pbResp.Job),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// SchedulerDeleteJob removes a scheduled job.
func (c *Client) SchedulerDeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.SchedulerDeleteJob",
			Err:  ErrRequestNil,
		}
	}

	scope, err := serviceJobScopeToProto("Client.SchedulerDeleteJob", req.Scope)
	if err != nil {
		return nil, err
	}

	schedulerClient := pb.NewSchedulerServiceClient(c.conn)

	pbReq := &pb.DeleteJobRequest{
		JobId:    req.JobID,
		TenantId: req.TenantID,
		Scope:    scope,
		Force:    req.Force,
	}

	pbResp, err := schedulerClient.DeleteJob(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.DeleteSchedulerJobResponse{
		JobID:     pbResp.JobId,
		Deleted:   pbResp.Deleted,
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// SchedulerListJobs returns jobs matching the query criteria.
func (c *Client) SchedulerListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	if req == nil {
		req = &service.ListSchedulerJobsRequest{}
	}

	limit, err := clientInt32("limit", req.Limit)
	if err != nil {
		return nil, err
	}

	schedulerClient := pb.NewSchedulerServiceClient(c.conn)

	pbReq := &pb.ListJobsRequest{
		OwnerType:      convertServiceToProtoOwnerType(req.OwnerType),
		OwnerId:        req.OwnerID,
		TargetTenantId: req.TargetTenantID,
		Status:         convertServiceToProtoJobStatus(req.Status),
		Handler:        req.Handler,
		Labels:         req.Labels,
		Limit:          limit,
		Cursor:         req.Cursor,
	}

	pbResp, err := schedulerClient.ListJobs(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	jobs := make([]*service.SchedulerJob, len(pbResp.Jobs))
	for i, job := range pbResp.Jobs {
		jobs[i] = convertProtoJob(job)
	}

	return &service.ListSchedulerJobsResponse{
		Jobs:       jobs,
		NextCursor: pbResp.NextCursor,
		HasMore:    pbResp.HasMore,
		TotalCount: int(pbResp.TotalCount),
		Timestamp:  time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// SchedulerTriggerJob immediately executes a job.
func (c *Client) SchedulerTriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.SchedulerTriggerJob",
			Err:  ErrRequestNil,
		}
	}

	scope, err := serviceJobScopeToProto("Client.SchedulerTriggerJob", req.Scope)
	if err != nil {
		return nil, err
	}

	schedulerClient := pb.NewSchedulerServiceClient(c.conn)

	pbReq := &pb.TriggerJobRequest{
		JobId:    req.JobID,
		TenantId: req.TenantID,
		Scope:    scope,
	}

	pbResp, err := schedulerClient.TriggerJob(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.TriggerSchedulerJobResponse{
		JobID:     pbResp.JobId,
		Status:    convertProtoToServiceJobStatus(pbResp.Status),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// SchedulerPauseJob pauses a scheduled job.
func (c *Client) SchedulerPauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.SchedulerPauseJob",
			Err:  ErrRequestNil,
		}
	}

	scope, err := serviceJobScopeToProto("Client.SchedulerPauseJob", req.Scope)
	if err != nil {
		return nil, err
	}

	schedulerClient := pb.NewSchedulerServiceClient(c.conn)

	pbReq := &pb.PauseJobRequest{
		JobId:    req.JobID,
		TenantId: req.TenantID,
		Scope:    scope,
	}

	pbResp, err := schedulerClient.PauseJob(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.PauseSchedulerJobResponse{
		JobID:     pbResp.JobId,
		OldStatus: convertProtoToServiceJobStatus(pbResp.OldStatus),
		NewStatus: convertProtoToServiceJobStatus(pbResp.NewStatus),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// SchedulerResumeJob resumes a paused job.
func (c *Client) SchedulerResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.SchedulerResumeJob",
			Err:  ErrRequestNil,
		}
	}

	scope, err := serviceJobScopeToProto("Client.SchedulerResumeJob", req.Scope)
	if err != nil {
		return nil, err
	}

	schedulerClient := pb.NewSchedulerServiceClient(c.conn)

	pbReq := &pb.ResumeJobRequest{
		JobId:    req.JobID,
		TenantId: req.TenantID,
		Scope:    scope,
	}

	pbResp, err := schedulerClient.ResumeJob(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.ResumeSchedulerJobResponse{
		JobID:     pbResp.JobId,
		OldStatus: convertProtoToServiceJobStatus(pbResp.OldStatus),
		NewStatus: convertProtoToServiceJobStatus(pbResp.NewStatus),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// SchedulerCancelJob cancels a scheduled job.
func (c *Client) SchedulerCancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.SchedulerCancelJob",
			Err:  ErrRequestNil,
		}
	}

	scope, err := serviceJobScopeToProto("Client.SchedulerCancelJob", req.Scope)
	if err != nil {
		return nil, err
	}

	schedulerClient := pb.NewSchedulerServiceClient(c.conn)

	pbReq := &pb.CancelJobRequest{
		JobId:    req.JobID,
		TenantId: req.TenantID,
		Scope:    scope,
		Reason:   req.Reason,
	}

	pbResp, err := schedulerClient.CancelJob(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.CancelSchedulerJobResponse{
		JobID:     pbResp.JobId,
		OldStatus: convertProtoToServiceJobStatus(pbResp.OldStatus),
		NewStatus: convertProtoToServiceJobStatus(pbResp.NewStatus),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// SchedulerGetJobHistory returns execution history for a job.
func (c *Client) SchedulerGetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.SchedulerGetJobHistory",
			Err:  ErrRequestNil,
		}
	}

	limit, err := clientInt32("limit", req.Limit)
	if err != nil {
		return nil, err
	}

	scope, err := serviceJobScopeToProto("Client.SchedulerGetJobHistory", req.Scope)
	if err != nil {
		return nil, err
	}

	schedulerClient := pb.NewSchedulerServiceClient(c.conn)

	pbReq := &pb.GetJobHistoryRequest{
		JobId:    req.JobID,
		TenantId: req.TenantID,
		Scope:    scope,
		Limit:    limit,
		Cursor:   req.Cursor,
	}

	pbResp, err := schedulerClient.GetJobHistory(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	executions := make([]*service.SchedulerJobExecution, len(pbResp.Executions))
	for i, exec := range pbResp.Executions {
		executions[i] = convertProtoJobExecution(exec)
	}

	return &service.GetSchedulerJobHistoryResponse{
		Executions: executions,
		NextCursor: pbResp.NextCursor,
		HasMore:    pbResp.HasMore,
		Timestamp:  time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// SchedulerGetStatus returns scheduler status and statistics.
func (c *Client) SchedulerGetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	schedulerClient := pb.NewSchedulerServiceClient(c.conn)

	pbResp, err := schedulerClient.GetSchedulerStatus(ctx, &pb.GetSchedulerStatusRequest{})
	if err != nil {
		return nil, convertError(err)
	}

	return &service.GetSchedulerStatusResponse{
		Running:   pbResp.Running,
		Stats:     convertProtoSchedulerStats(pbResp.Stats),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// Helper functions to convert between service and proto types

// convertProtoJob converts proto Job to service SchedulerJob.
func convertProtoJob(pj *pb.Job) *service.SchedulerJob {
	if pj == nil {
		return nil
	}

	job := &service.SchedulerJob{
		ID:             pj.Id,
		Name:           pj.Name,
		Description:    pj.Description,
		OwnerType:      convertProtoToServiceOwnerType(pj.OwnerType),
		OwnerID:        pj.OwnerId,
		TargetTenantID: pj.TargetTenantId,
		CreatedBy:      pj.CreatedBy,
		Type:           convertProtoToServiceJobType(pj.Type),
		Schedule:       pj.Schedule,
		Handler:        pj.Handler,
		Payload:        pj.Payload,
		Priority:       pj.Priority,
		Timeout:        time.Duration(pj.TimeoutSeconds) * time.Second,
		MaxRetries:     pj.MaxRetries,
		Status:         convertProtoToServiceJobStatus(pj.Status),
		RunCount:       pj.RunCount,
		FailCount:      pj.FailCount,
		LastError:      pj.LastError,
		Labels:         pj.Labels,
		CreatedAt:      time.Unix(pj.CreatedAt, 0),
		UpdatedAt:      time.Unix(pj.UpdatedAt, 0),
	}

	if pj.ExecuteAt > 0 {
		job.ExecuteAt = time.Unix(pj.ExecuteAt, 0)
	}

	if pj.DelaySeconds > 0 {
		job.Delay = time.Duration(pj.DelaySeconds) * time.Second
	}

	if pj.LastRun > 0 {
		job.LastRun = time.Unix(pj.LastRun, 0)
	}

	if pj.NextRun > 0 {
		job.NextRun = time.Unix(pj.NextRun, 0)
	}

	return job
}

// convertProtoJobExecution converts proto JobExecution to service SchedulerJobExecution.
func convertProtoJobExecution(pe *pb.JobExecution) *service.SchedulerJobExecution {
	if pe == nil {
		return nil
	}

	exec := &service.SchedulerJobExecution{
		ID:        pe.Id,
		JobID:     pe.JobId,
		Status:    convertProtoToServiceJobStatus(pe.Status),
		StartedAt: time.Unix(pe.StartedAt, 0),
		Duration:  time.Duration(pe.DurationMs) * time.Millisecond,
		Error:     pe.Error,
		Output:    pe.Output,
	}

	if pe.EndedAt > 0 {
		exec.EndedAt = time.Unix(pe.EndedAt, 0)
	}

	return exec
}

// convertProtoSchedulerStats converts proto SchedulerStats to service SchedulerStats.
func convertProtoSchedulerStats(ps *pb.SchedulerStats) *service.SchedulerStats {
	if ps == nil {
		return nil
	}

	stats := &service.SchedulerStats{
		JobCount:     ps.JobCount,
		RunningJobs:  ps.RunningJobs,
		SuccessCount: ps.SuccessCount,
		FailureCount: ps.FailureCount,
		StartedAt:    time.Unix(ps.StartedAt, 0),
	}

	if ps.LastTickTime > 0 {
		stats.LastTickTime = time.Unix(ps.LastTickTime, 0)
	}

	return stats
}

// convertProtoToServiceJobType converts proto JobType to service SchedulerJobType.
func convertProtoToServiceJobType(jt pb.JobType) service.SchedulerJobType {
	switch jt {
	case pb.JobType_JOB_TYPE_ONE_SHOT:
		return service.SchedulerJobTypeOneShot
	case pb.JobType_JOB_TYPE_RECURRING:
		return service.SchedulerJobTypeRecurring
	case pb.JobType_JOB_TYPE_DELAYED:
		return service.SchedulerJobTypeDelayed
	default:
		return ""
	}
}

// convertServiceToProtoJobType converts service SchedulerJobType to proto JobType.
func convertServiceToProtoJobType(jt service.SchedulerJobType) pb.JobType {
	switch jt {
	case service.SchedulerJobTypeOneShot:
		return pb.JobType_JOB_TYPE_ONE_SHOT
	case service.SchedulerJobTypeRecurring:
		return pb.JobType_JOB_TYPE_RECURRING
	case service.SchedulerJobTypeDelayed:
		return pb.JobType_JOB_TYPE_DELAYED
	default:
		return pb.JobType_JOB_TYPE_UNSPECIFIED
	}
}

// convertProtoToServiceJobStatus converts proto JobStatus to service SchedulerJobStatus.
func convertProtoToServiceJobStatus(js pb.JobStatus) service.SchedulerJobStatus {
	switch js {
	case pb.JobStatus_JOB_STATUS_PENDING:
		return service.SchedulerJobStatusPending
	case pb.JobStatus_JOB_STATUS_RUNNING:
		return service.SchedulerJobStatusRunning
	case pb.JobStatus_JOB_STATUS_COMPLETED:
		return service.SchedulerJobStatusCompleted
	case pb.JobStatus_JOB_STATUS_FAILED:
		return service.SchedulerJobStatusFailed
	case pb.JobStatus_JOB_STATUS_PAUSED:
		return service.SchedulerJobStatusPaused
	case pb.JobStatus_JOB_STATUS_CANCELED:
		return service.SchedulerJobStatusCanceled
	default:
		return ""
	}
}

// convertServiceToProtoJobStatus converts service SchedulerJobStatus to proto JobStatus.
func convertServiceToProtoJobStatus(js service.SchedulerJobStatus) pb.JobStatus {
	switch js {
	case service.SchedulerJobStatusPending:
		return pb.JobStatus_JOB_STATUS_PENDING
	case service.SchedulerJobStatusRunning:
		return pb.JobStatus_JOB_STATUS_RUNNING
	case service.SchedulerJobStatusCompleted:
		return pb.JobStatus_JOB_STATUS_COMPLETED
	case service.SchedulerJobStatusFailed:
		return pb.JobStatus_JOB_STATUS_FAILED
	case service.SchedulerJobStatusPaused:
		return pb.JobStatus_JOB_STATUS_PAUSED
	case service.SchedulerJobStatusCanceled:
		return pb.JobStatus_JOB_STATUS_CANCELED
	default:
		return pb.JobStatus_JOB_STATUS_UNSPECIFIED
	}
}

// convertProtoToServiceOwnerType converts proto OwnerType to service SchedulerOwnerType.
func convertProtoToServiceOwnerType(ot pb.OwnerType) service.SchedulerOwnerType {
	switch ot {
	case pb.OwnerType_OWNER_TYPE_SYSTEM:
		return service.SchedulerOwnerTypeSystem
	case pb.OwnerType_OWNER_TYPE_TENANT:
		return service.SchedulerOwnerTypeTenant
	case pb.OwnerType_OWNER_TYPE_DELEGATED:
		return service.SchedulerOwnerTypeDelegated
	default:
		return ""
	}
}

// convertServiceToProtoOwnerType converts service SchedulerOwnerType to proto OwnerType.
func convertServiceToProtoOwnerType(ot service.SchedulerOwnerType) pb.OwnerType {
	switch ot {
	case service.SchedulerOwnerTypeSystem:
		return pb.OwnerType_OWNER_TYPE_SYSTEM
	case service.SchedulerOwnerTypeTenant:
		return pb.OwnerType_OWNER_TYPE_TENANT
	case service.SchedulerOwnerTypeDelegated:
		return pb.OwnerType_OWNER_TYPE_DELEGATED
	default:
		return pb.OwnerType_OWNER_TYPE_UNSPECIFIED
	}
}

// ErrSchedulerJobScopeInvalid indicates a by-ID request named a job scope
// that is not tenant, delegated or system.
var ErrSchedulerJobScopeInvalid = stderrors.New("job scope must be tenant, delegated or system")

// serviceJobScopes maps a by-ID request's job scope onto the wire enum. An
// empty scope names the caller's own jobs and leaves the field unspecified.
var serviceJobScopes = map[service.SchedulerOwnerType]pb.OwnerType{
	"":                                  pb.OwnerType_OWNER_TYPE_UNSPECIFIED,
	service.SchedulerOwnerTypeSystem:    pb.OwnerType_OWNER_TYPE_SYSTEM,
	service.SchedulerOwnerTypeTenant:    pb.OwnerType_OWNER_TYPE_TENANT,
	service.SchedulerOwnerTypeDelegated: pb.OwnerType_OWNER_TYPE_DELEGATED,
}

// serviceJobScopeToProto converts a by-ID request's job scope. A scope the
// wire enum cannot carry is refused rather than sent as the caller's own.
func serviceJobScopeToProto(op string, scope service.SchedulerOwnerType) (pb.OwnerType, error) {
	ot, ok := serviceJobScopes[scope]
	if !ok {
		return pb.OwnerType_OWNER_TYPE_UNSPECIFIED, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   op,
			Err:  ErrSchedulerJobScopeInvalid,
		}
	}
	return ot, nil
}
