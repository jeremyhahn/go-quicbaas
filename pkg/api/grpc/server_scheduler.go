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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// SchedulerServer wraps the scheduler service for gRPC endpoints.
type SchedulerServer struct {
	pb.UnimplementedSchedulerServiceServer
	schedulerSvc service.SchedulerService
	logger       *logger.Logger
}

// NewSchedulerServer creates a new gRPC server for scheduler operations.
func NewSchedulerServer(schedulerSvc service.SchedulerService, log *logger.Logger) *SchedulerServer {
	return &SchedulerServer{
		schedulerSvc: schedulerSvc,
		logger:       log.WithField("component", "scheduler-grpc-server"),
	}
}

// CreateJob creates a new scheduled job.
func (s *SchedulerServer) CreateJob(ctx context.Context, req *pb.CreateJobRequest) (*pb.CreateJobResponse, error) {
	s.logger.WithField("name", req.Name).Debug("Processing CreateJob request")

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "job name is required")
	}

	if req.Handler == "" {
		return nil, status.Error(codes.InvalidArgument, "handler is required")
	}

	svcReq := &service.CreateSchedulerJobRequest{
		Name:           req.Name,
		Description:    req.Description,
		OwnerType:      protoOwnerTypeToService(req.OwnerType),
		OwnerID:        req.OwnerId,
		TargetTenantID: req.TargetTenantId,
		Type:           protoJobTypeToService(req.Type),
		Schedule:       req.Schedule,
		ExecuteAt:      time.Unix(req.ExecuteAt, 0),
		Delay:          time.Duration(req.DelaySeconds) * time.Second,
		Handler:        req.Handler,
		Payload:        req.Payload,
		Priority:       req.Priority,
		Timeout:        time.Duration(req.TimeoutSeconds) * time.Second,
		MaxRetries:     req.MaxRetries,
		NodeAffinity:   req.NodeAffinity,
		Labels:         req.Labels,
	}

	resp, err := s.schedulerSvc.CreateJob(ctx, svcReq)
	if err != nil {
		return nil, convertSchedulerError(err)
	}

	return &pb.CreateJobResponse{
		Job:       serviceJobToProto(resp.Job),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// GetJob retrieves a job by ID.
func (s *SchedulerServer) GetJob(ctx context.Context, req *pb.GetJobRequest) (*pb.GetJobResponse, error) {
	s.logger.WithField("job_id", req.JobId).Debug("Processing GetJob request")

	if req.JobId == 0 {
		return nil, status.Error(codes.InvalidArgument, "job_id is required")
	}

	svcReq := &service.GetSchedulerJobRequest{
		JobID:    req.JobId,
		TenantID: req.TenantId,
	}

	resp, err := s.schedulerSvc.GetJob(ctx, svcReq)
	if err != nil {
		return nil, convertSchedulerError(err)
	}

	return &pb.GetJobResponse{
		Job:       serviceJobToProto(resp.Job),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// UpdateJob updates an existing job.
func (s *SchedulerServer) UpdateJob(ctx context.Context, req *pb.UpdateJobRequest) (*pb.UpdateJobResponse, error) {
	s.logger.WithField("job_id", req.JobId).Debug("Processing UpdateJob request")

	if req.JobId == 0 {
		return nil, status.Error(codes.InvalidArgument, "job_id is required")
	}

	svcReq := &service.UpdateSchedulerJobRequest{
		JobID:       req.JobId,
		TenantID:    req.TenantId,
		Name:        req.Name,
		Description: req.Description,
		Schedule:    req.Schedule,
		ExecuteAt:   time.Unix(req.ExecuteAt, 0),
		Handler:     req.Handler,
		Payload:     req.Payload,
		Priority:    req.Priority,
		Timeout:     time.Duration(req.TimeoutSeconds) * time.Second,
		MaxRetries:  req.MaxRetries,
		Labels:      req.Labels,
	}

	resp, err := s.schedulerSvc.UpdateJob(ctx, svcReq)
	if err != nil {
		return nil, convertSchedulerError(err)
	}

	return &pb.UpdateJobResponse{
		Job:       serviceJobToProto(resp.Job),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// DeleteJob removes a scheduled job.
func (s *SchedulerServer) DeleteJob(ctx context.Context, req *pb.DeleteJobRequest) (*pb.DeleteJobResponse, error) {
	s.logger.WithField("job_id", req.JobId).Debug("Processing DeleteJob request")

	if req.JobId == 0 {
		return nil, status.Error(codes.InvalidArgument, "job_id is required")
	}

	svcReq := &service.DeleteSchedulerJobRequest{
		JobID:    req.JobId,
		TenantID: req.TenantId,
		Force:    req.Force,
	}

	resp, err := s.schedulerSvc.DeleteJob(ctx, svcReq)
	if err != nil {
		return nil, convertSchedulerError(err)
	}

	return &pb.DeleteJobResponse{
		JobId:     resp.JobID,
		Deleted:   resp.Deleted,
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// ListJobs returns jobs matching the query criteria.
func (s *SchedulerServer) ListJobs(ctx context.Context, req *pb.ListJobsRequest) (*pb.ListJobsResponse, error) {
	s.logger.Debug("Processing ListJobs request")

	svcReq := &service.ListSchedulerJobsRequest{
		OwnerType:      protoOwnerTypeToService(req.OwnerType),
		OwnerID:        req.OwnerId,
		TargetTenantID: req.TargetTenantId,
		Status:         protoJobStatusToService(req.Status),
		Handler:        req.Handler,
		Labels:         req.Labels,
		Limit:          int(req.Limit),
		Cursor:         req.Cursor,
	}

	resp, err := s.schedulerSvc.ListJobs(ctx, svcReq)
	if err != nil {
		return nil, convertSchedulerError(err)
	}

	jobs := make([]*pb.Job, len(resp.Jobs))
	for i, job := range resp.Jobs {
		jobs[i] = serviceJobToProto(job)
	}

	return &pb.ListJobsResponse{
		Jobs:       jobs,
		NextCursor: resp.NextCursor,
		HasMore:    resp.HasMore,
		TotalCount: int32(resp.TotalCount), //nolint:gosec // G115: bounded count
		Timestamp:  resp.Timestamp.Unix(),
	}, nil
}

// TriggerJob immediately executes a job.
func (s *SchedulerServer) TriggerJob(ctx context.Context, req *pb.TriggerJobRequest) (*pb.TriggerJobResponse, error) {
	s.logger.WithField("job_id", req.JobId).Debug("Processing TriggerJob request")

	if req.JobId == 0 {
		return nil, status.Error(codes.InvalidArgument, "job_id is required")
	}

	svcReq := &service.TriggerSchedulerJobRequest{
		JobID:    req.JobId,
		TenantID: req.TenantId,
	}

	resp, err := s.schedulerSvc.TriggerJob(ctx, svcReq)
	if err != nil {
		return nil, convertSchedulerError(err)
	}

	return &pb.TriggerJobResponse{
		JobId:     resp.JobID,
		Status:    serviceJobStatusToProto(resp.Status),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// PauseJob pauses a scheduled job.
func (s *SchedulerServer) PauseJob(ctx context.Context, req *pb.PauseJobRequest) (*pb.PauseJobResponse, error) {
	s.logger.WithField("job_id", req.JobId).Debug("Processing PauseJob request")

	if req.JobId == 0 {
		return nil, status.Error(codes.InvalidArgument, "job_id is required")
	}

	svcReq := &service.PauseSchedulerJobRequest{
		JobID:    req.JobId,
		TenantID: req.TenantId,
	}

	resp, err := s.schedulerSvc.PauseJob(ctx, svcReq)
	if err != nil {
		return nil, convertSchedulerError(err)
	}

	return &pb.PauseJobResponse{
		JobId:     resp.JobID,
		OldStatus: serviceJobStatusToProto(resp.OldStatus),
		NewStatus: serviceJobStatusToProto(resp.NewStatus),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// ResumeJob resumes a paused job.
func (s *SchedulerServer) ResumeJob(ctx context.Context, req *pb.ResumeJobRequest) (*pb.ResumeJobResponse, error) {
	s.logger.WithField("job_id", req.JobId).Debug("Processing ResumeJob request")

	if req.JobId == 0 {
		return nil, status.Error(codes.InvalidArgument, "job_id is required")
	}

	svcReq := &service.ResumeSchedulerJobRequest{
		JobID:    req.JobId,
		TenantID: req.TenantId,
	}

	resp, err := s.schedulerSvc.ResumeJob(ctx, svcReq)
	if err != nil {
		return nil, convertSchedulerError(err)
	}

	return &pb.ResumeJobResponse{
		JobId:     resp.JobID,
		OldStatus: serviceJobStatusToProto(resp.OldStatus),
		NewStatus: serviceJobStatusToProto(resp.NewStatus),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// CancelJob cancels a scheduled job.
func (s *SchedulerServer) CancelJob(ctx context.Context, req *pb.CancelJobRequest) (*pb.CancelJobResponse, error) {
	s.logger.WithField("job_id", req.JobId).Debug("Processing CancelJob request")

	if req.JobId == 0 {
		return nil, status.Error(codes.InvalidArgument, "job_id is required")
	}

	svcReq := &service.CancelSchedulerJobRequest{
		JobID:    req.JobId,
		TenantID: req.TenantId,
		Reason:   req.Reason,
	}

	resp, err := s.schedulerSvc.CancelJob(ctx, svcReq)
	if err != nil {
		return nil, convertSchedulerError(err)
	}

	return &pb.CancelJobResponse{
		JobId:     resp.JobID,
		OldStatus: serviceJobStatusToProto(resp.OldStatus),
		NewStatus: serviceJobStatusToProto(resp.NewStatus),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// GetJobHistory returns execution history for a job.
func (s *SchedulerServer) GetJobHistory(ctx context.Context, req *pb.GetJobHistoryRequest) (*pb.GetJobHistoryResponse, error) {
	s.logger.WithField("job_id", req.JobId).Debug("Processing GetJobHistory request")

	if req.JobId == 0 {
		return nil, status.Error(codes.InvalidArgument, "job_id is required")
	}

	svcReq := &service.GetSchedulerJobHistoryRequest{
		JobID:    req.JobId,
		TenantID: req.TenantId,
		Limit:    int(req.Limit),
		Cursor:   req.Cursor,
	}

	resp, err := s.schedulerSvc.GetJobHistory(ctx, svcReq)
	if err != nil {
		return nil, convertSchedulerError(err)
	}

	executions := make([]*pb.JobExecution, len(resp.Executions))
	for i, exec := range resp.Executions {
		executions[i] = serviceExecutionToProto(exec)
	}

	return &pb.GetJobHistoryResponse{
		Executions: executions,
		NextCursor: resp.NextCursor,
		HasMore:    resp.HasMore,
		Timestamp:  resp.Timestamp.Unix(),
	}, nil
}

// GetSchedulerStatus returns scheduler status and statistics.
func (s *SchedulerServer) GetSchedulerStatus(ctx context.Context, _ *pb.GetSchedulerStatusRequest) (*pb.GetSchedulerStatusResponse, error) {
	s.logger.Debug("Processing GetSchedulerStatus request")

	resp, err := s.schedulerSvc.GetStatus(ctx)
	if err != nil {
		return nil, convertSchedulerError(err)
	}

	return &pb.GetSchedulerStatusResponse{
		Running:   resp.Running,
		Stats:     serviceStatsToProto(resp.Stats),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// =============================================================================
// Type Conversion Helpers - Proto to Service
// =============================================================================

// protoOwnerTypeToService converts pb.OwnerType to service.SchedulerOwnerType.
func protoOwnerTypeToService(ot pb.OwnerType) service.SchedulerOwnerType {
	switch ot {
	case pb.OwnerType_OWNER_TYPE_SYSTEM:
		return service.SchedulerOwnerTypeSystem
	case pb.OwnerType_OWNER_TYPE_TENANT:
		return service.SchedulerOwnerTypeTenant
	case pb.OwnerType_OWNER_TYPE_DELEGATED:
		return service.SchedulerOwnerTypeDelegated
	default:
		return service.SchedulerOwnerTypeSystem
	}
}

// protoJobTypeToService converts pb.JobType to service.SchedulerJobType.
func protoJobTypeToService(jt pb.JobType) service.SchedulerJobType {
	switch jt {
	case pb.JobType_JOB_TYPE_ONE_SHOT:
		return service.SchedulerJobTypeOneShot
	case pb.JobType_JOB_TYPE_RECURRING:
		return service.SchedulerJobTypeRecurring
	case pb.JobType_JOB_TYPE_DELAYED:
		return service.SchedulerJobTypeDelayed
	default:
		return service.SchedulerJobTypeOneShot
	}
}

// protoJobStatusToService converts pb.JobStatus to service.SchedulerJobStatus.
func protoJobStatusToService(js pb.JobStatus) service.SchedulerJobStatus {
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
		return service.SchedulerJobStatusPending
	}
}

// =============================================================================
// Type Conversion Helpers - Service to Proto
// =============================================================================

// serviceOwnerTypeToProto converts service.SchedulerOwnerType to pb.OwnerType.
func serviceOwnerTypeToProto(ot service.SchedulerOwnerType) pb.OwnerType {
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

// serviceJobTypeToProto converts service.SchedulerJobType to pb.JobType.
func serviceJobTypeToProto(jt service.SchedulerJobType) pb.JobType {
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

// serviceJobStatusToProto converts service.SchedulerJobStatus to pb.JobStatus.
func serviceJobStatusToProto(js service.SchedulerJobStatus) pb.JobStatus {
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

// serviceJobToProto converts service.SchedulerJob to pb.Job.
func serviceJobToProto(job *service.SchedulerJob) *pb.Job {
	if job == nil {
		return nil
	}

	return &pb.Job{
		Id:             job.ID,
		Name:           job.Name,
		Description:    job.Description,
		OwnerType:      serviceOwnerTypeToProto(job.OwnerType),
		OwnerId:        job.OwnerID,
		TargetTenantId: job.TargetTenantID,
		CreatedBy:      job.CreatedBy,
		Type:           serviceJobTypeToProto(job.Type),
		Schedule:       job.Schedule,
		ExecuteAt:      job.ExecuteAt.Unix(),
		DelaySeconds:   int64(job.Delay.Seconds()),
		Handler:        job.Handler,
		Payload:        job.Payload,
		Priority:       job.Priority,
		TimeoutSeconds: int64(job.Timeout.Seconds()),
		MaxRetries:     job.MaxRetries,
		Status:         serviceJobStatusToProto(job.Status),
		LastRun:        job.LastRun.Unix(),
		NextRun:        job.NextRun.Unix(),
		RunCount:       job.RunCount,
		FailCount:      job.FailCount,
		LastError:      job.LastError,
		Labels:         job.Labels,
		CreatedAt:      job.CreatedAt.Unix(),
		UpdatedAt:      job.UpdatedAt.Unix(),
	}
}

// serviceExecutionToProto converts service.SchedulerJobExecution to pb.JobExecution.
func serviceExecutionToProto(exec *service.SchedulerJobExecution) *pb.JobExecution {
	if exec == nil {
		return nil
	}

	return &pb.JobExecution{
		Id:         exec.ID,
		JobId:      exec.JobID,
		Status:     serviceJobStatusToProto(exec.Status),
		StartedAt:  exec.StartedAt.Unix(),
		EndedAt:    exec.EndedAt.Unix(),
		DurationMs: exec.Duration.Milliseconds(),
		Error:      exec.Error,
		Output:     exec.Output,
	}
}

// serviceStatsToProto converts service.SchedulerStats to pb.SchedulerStats.
func serviceStatsToProto(stats *service.SchedulerStats) *pb.SchedulerStats {
	if stats == nil {
		return nil
	}

	return &pb.SchedulerStats{
		JobCount:     stats.JobCount,
		RunningJobs:  stats.RunningJobs,
		SuccessCount: stats.SuccessCount,
		FailureCount: stats.FailureCount,
		LastTickTime: stats.LastTickTime.Unix(),
		StartedAt:    stats.StartedAt.Unix(),
	}
}

// =============================================================================
// Error Handling
// =============================================================================

// convertSchedulerError converts internal errors to gRPC status errors.
func convertSchedulerError(err error) error {
	if err == nil {
		return nil
	}

	// Check scheduler-specific error types first
	var notFoundErr *scheduler.JobNotFoundError
	if stderrors.As(err, &notFoundErr) {
		return status.Error(codes.NotFound, notFoundErr.Error())
	}

	var alreadyExistsErr *scheduler.JobAlreadyExistsError
	if stderrors.As(err, &alreadyExistsErr) {
		return status.Error(codes.AlreadyExists, alreadyExistsErr.Error())
	}

	var invalidJobErr *scheduler.InvalidJobError
	if stderrors.As(err, &invalidJobErr) {
		return status.Error(codes.InvalidArgument, invalidJobErr.Error())
	}

	var invalidScheduleErr *scheduler.InvalidScheduleError
	if stderrors.As(err, &invalidScheduleErr) {
		return status.Error(codes.InvalidArgument, invalidScheduleErr.Error())
	}

	var handlerNotFoundErr *scheduler.HandlerNotFoundError
	if stderrors.As(err, &handlerNotFoundErr) {
		return status.Error(codes.NotFound, handlerNotFoundErr.Error())
	}

	var handlerFailedErr *scheduler.HandlerFailedError
	if stderrors.As(err, &handlerFailedErr) {
		return status.Error(codes.Internal, handlerFailedErr.Error())
	}

	var timeoutErr *scheduler.JobTimeoutError
	if stderrors.As(err, &timeoutErr) {
		return status.Error(codes.DeadlineExceeded, timeoutErr.Error())
	}

	var maxRetriesErr *scheduler.MaxRetriesExceededError
	if stderrors.As(err, &maxRetriesErr) {
		return status.Error(codes.ResourceExhausted, maxRetriesErr.Error())
	}

	var stoppedErr *scheduler.SchedulerStoppedError
	if stderrors.As(err, &stoppedErr) {
		return status.Error(codes.Unavailable, stoppedErr.Error())
	}

	var unauthorizedErr *scheduler.UnauthorizedError
	if stderrors.As(err, &unauthorizedErr) {
		return status.Error(codes.PermissionDenied, unauthorizedErr.Error())
	}

	var storageErr *scheduler.ErrStorageError
	if stderrors.As(err, &storageErr) {
		return status.Error(codes.Internal, storageErr.Error())
	}

	// Fall back to QBError handling
	var dErr *errors.QBError
	if !stderrors.As(err, &dErr) {
		return status.Error(codes.Internal, err.Error())
	}

	switch dErr.Code {
	case errors.ErrNotFound:
		return status.Error(codes.NotFound, dErr.Error())
	case errors.ErrAlreadyExists:
		return status.Error(codes.AlreadyExists, dErr.Error())
	case errors.ErrInvalidInput:
		return status.Error(codes.InvalidArgument, dErr.Error())
	case errors.ErrUnauthorized:
		return status.Error(codes.Unauthenticated, dErr.Error())
	case errors.ErrForbidden:
		return status.Error(codes.PermissionDenied, dErr.Error())
	case errors.ErrTimeout:
		return status.Error(codes.DeadlineExceeded, dErr.Error())
	case errors.ErrResourceExhausted:
		return status.Error(codes.ResourceExhausted, dErr.Error())
	case errors.ErrUnavailable:
		return status.Error(codes.Unavailable, dErr.Error())
	case errors.ErrCanceled:
		return status.Error(codes.Canceled, dErr.Error())
	default:
		return status.Error(codes.Internal, dErr.Error())
	}
}

// Ensure SchedulerServer implements the interface.
var _ pb.SchedulerServiceServer = (*SchedulerServer)(nil)
