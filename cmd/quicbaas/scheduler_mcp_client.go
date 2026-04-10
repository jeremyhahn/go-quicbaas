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

// Package main provides MCP scheduler client implementation for the CLI.
package main

import (
	"context"
	"time"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mcpSchedulerCtx enriches the context with the tenant ID so the MCP transport
// can extract it and include it in the MCP tool arguments. The SDK scheduler
// client methods do not accept a TenantID parameter — tenant travels via context.
func mcpSchedulerCtx(ctx context.Context, tenantID string) context.Context {
	if tenantID != "" {
		return sdktransport.ContextWithTenant(ctx, tenantID)
	}
	return ctx
}

// MCPSchedulerClient wraps the SDK client to implement the SchedulerClient interface.
type MCPSchedulerClient struct {
	client quicbaas.Client
}

// SchedulerCreateJob creates a new scheduled job via MCP.
func (c *MCPSchedulerClient) SchedulerCreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerCreateJob",
			"MCP client not initialized",
		)
	}
	schedulerClient := c.client.Scheduler()
	if schedulerClient == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerCreateJob",
			"scheduler client not available",
		)
	}

	// Convert service request to SDK request
	sdkReq := &scheduler.CreateJobRequest{
		Name:        req.Name,
		Description: req.Description,
		JobType:     convertJobTypeToSDK(req.Type),
		Handler:     req.Handler,
		Schedule:    req.Schedule,
		ExecuteAt:   req.ExecuteAt,
		Delay:       req.Delay,
		Payload:     req.Payload,
		OwnerType:   convertOwnerTypeToSDK(req.OwnerType),
		OwnerID:     req.OwnerID,
		MaxRetries:  int(req.MaxRetries),
		Timeout:     req.Timeout,
		Tags:        req.Labels,
	}

	resp, err := schedulerClient.CreateJob(mcpSchedulerCtx(ctx, req.OwnerID), sdkReq)
	if err != nil {
		return nil, err
	}

	// Convert SDK response to service response
	return &service.CreateSchedulerJobResponse{
		Job: &service.SchedulerJob{
			ID:          resp.JobID,
			Name:        req.Name,
			Description: req.Description,
			Type:        req.Type,
			Status:      service.SchedulerJobStatusPending,
			Schedule:    req.Schedule,
			Handler:     req.Handler,
			Payload:     req.Payload,
			OwnerType:   req.OwnerType,
			OwnerID:     req.OwnerID,
			Priority:    req.Priority,
			Timeout:     req.Timeout,
			MaxRetries:  req.MaxRetries,
			Labels:      req.Labels,
			NextRun:     resp.NextRunAt,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		Timestamp: time.Now(),
	}, nil
}

// SchedulerGetJob retrieves a job by ID via MCP.
func (c *MCPSchedulerClient) SchedulerGetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerGetJob",
			"MCP client not initialized",
		)
	}
	schedulerClient := c.client.Scheduler()
	if schedulerClient == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerGetJob",
			"scheduler client not available",
		)
	}

	jobInfo, err := schedulerClient.GetJob(mcpSchedulerCtx(ctx, req.TenantID), req.JobID)
	if err != nil {
		return nil, err
	}

	return &service.GetSchedulerJobResponse{
		Job:       convertJobInfoToService(jobInfo),
		Timestamp: time.Now(),
	}, nil
}

// SchedulerUpdateJob updates an existing job via MCP.
func (c *MCPSchedulerClient) SchedulerUpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerUpdateJob",
			"MCP client not initialized",
		)
	}
	schedulerClient := c.client.Scheduler()
	if schedulerClient == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerUpdateJob",
			"scheduler client not available",
		)
	}

	sdkReq := &scheduler.UpdateJobRequest{
		JobID:       req.JobID,
		Name:        req.Name,
		Description: req.Description,
		Schedule:    req.Schedule,
		Payload:     req.Payload,
		MaxRetries:  int(req.MaxRetries),
		Timeout:     req.Timeout,
		Tags:        req.Labels,
	}

	jobInfo, err := schedulerClient.UpdateJob(mcpSchedulerCtx(ctx, req.TenantID), sdkReq)
	if err != nil {
		return nil, err
	}

	return &service.UpdateSchedulerJobResponse{
		Job:       convertJobInfoToService(jobInfo),
		Timestamp: time.Now(),
	}, nil
}

// SchedulerDeleteJob deletes a job via MCP.
func (c *MCPSchedulerClient) SchedulerDeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerDeleteJob",
			"MCP client not initialized",
		)
	}
	schedulerClient := c.client.Scheduler()
	if schedulerClient == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerDeleteJob",
			"scheduler client not available",
		)
	}

	err := schedulerClient.DeleteJob(mcpSchedulerCtx(ctx, req.TenantID), req.JobID)
	if err != nil {
		return nil, err
	}

	return &service.DeleteSchedulerJobResponse{
		JobID:     req.JobID,
		Deleted:   true,
		Timestamp: time.Now(),
	}, nil
}

// SchedulerListJobs lists all jobs matching filters via MCP.
func (c *MCPSchedulerClient) SchedulerListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	if c.client == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerListJobs",
			"MCP client not initialized",
		)
	}
	schedulerClient := c.client.Scheduler()
	if schedulerClient == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerListJobs",
			"scheduler client not available",
		)
	}

	opts := &scheduler.ListJobsOptions{
		OwnerID: req.OwnerID,
		Tags:    req.Labels,
		Limit:   req.Limit,
	}

	if req.OwnerType != "" {
		ownerType := convertOwnerTypeToSDK(req.OwnerType)
		opts.OwnerType = &ownerType
	}

	if req.Status != "" {
		status := convertStatusToSDK(req.Status)
		opts.Status = &status
	}

	resp, err := schedulerClient.ListJobs(mcpSchedulerCtx(ctx, req.OwnerID), opts)
	if err != nil {
		return nil, err
	}

	jobs := make([]*service.SchedulerJob, len(resp.Jobs))
	for i, job := range resp.Jobs {
		jobs[i] = convertJobInfoToService(&job)
	}

	return &service.ListSchedulerJobsResponse{
		Jobs:       jobs,
		TotalCount: resp.TotalCount,
		HasMore:    resp.HasMore,
		Timestamp:  time.Now(),
	}, nil
}

// SchedulerTriggerJob triggers immediate execution of a job via MCP.
func (c *MCPSchedulerClient) SchedulerTriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerTriggerJob",
			"MCP client not initialized",
		)
	}
	schedulerClient := c.client.Scheduler()
	if schedulerClient == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerTriggerJob",
			"scheduler client not available",
		)
	}

	resp, err := schedulerClient.TriggerJob(mcpSchedulerCtx(ctx, req.TenantID), req.JobID)
	if err != nil {
		return nil, err
	}

	return &service.TriggerSchedulerJobResponse{
		JobID:     resp.JobID,
		Status:    service.SchedulerJobStatusRunning,
		Timestamp: time.Now(),
	}, nil
}

// SchedulerPauseJob pauses a job via MCP.
func (c *MCPSchedulerClient) SchedulerPauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerPauseJob",
			"MCP client not initialized",
		)
	}
	schedulerClient := c.client.Scheduler()
	if schedulerClient == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerPauseJob",
			"scheduler client not available",
		)
	}

	resp, err := schedulerClient.PauseJob(mcpSchedulerCtx(ctx, req.TenantID), req.JobID)
	if err != nil {
		return nil, err
	}

	return &service.PauseSchedulerJobResponse{
		JobID:     resp.JobID,
		OldStatus: service.SchedulerJobStatusPending,
		NewStatus: service.SchedulerJobStatusPaused,
		Timestamp: time.Now(),
	}, nil
}

// SchedulerResumeJob resumes a paused job via MCP.
func (c *MCPSchedulerClient) SchedulerResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerResumeJob",
			"MCP client not initialized",
		)
	}
	schedulerClient := c.client.Scheduler()
	if schedulerClient == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerResumeJob",
			"scheduler client not available",
		)
	}

	resp, err := schedulerClient.ResumeJob(mcpSchedulerCtx(ctx, req.TenantID), req.JobID)
	if err != nil {
		return nil, err
	}

	return &service.ResumeSchedulerJobResponse{
		JobID:     resp.JobID,
		OldStatus: service.SchedulerJobStatusPaused,
		NewStatus: service.SchedulerJobStatusPending,
		Timestamp: time.Now(),
	}, nil
}

// SchedulerCancelJob cancels a job via MCP.
func (c *MCPSchedulerClient) SchedulerCancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerCancelJob",
			"MCP client not initialized",
		)
	}
	schedulerClient := c.client.Scheduler()
	if schedulerClient == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerCancelJob",
			"scheduler client not available",
		)
	}

	resp, err := schedulerClient.CancelJob(mcpSchedulerCtx(ctx, req.TenantID), req.JobID, req.Reason)
	if err != nil {
		return nil, err
	}

	return &service.CancelSchedulerJobResponse{
		JobID:     resp.JobID,
		OldStatus: service.SchedulerJobStatusPending,
		NewStatus: service.SchedulerJobStatusCanceled,
		Timestamp: time.Now(),
	}, nil
}

// SchedulerGetJobHistory retrieves job execution history via MCP.
func (c *MCPSchedulerClient) SchedulerGetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	if c.client == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerGetJobHistory",
			"MCP client not initialized",
		)
	}
	schedulerClient := c.client.Scheduler()
	if schedulerClient == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerGetJobHistory",
			"scheduler client not available",
		)
	}

	opts := &scheduler.HistoryOptions{
		Limit: req.Limit,
	}

	resp, err := schedulerClient.GetJobHistory(mcpSchedulerCtx(ctx, req.TenantID), req.JobID, opts)
	if err != nil {
		return nil, err
	}

	executions := make([]*service.SchedulerJobExecution, len(resp.Executions))
	for i, exec := range resp.Executions {
		executions[i] = &service.SchedulerJobExecution{
			ID:        exec.ExecutionID,
			JobID:     exec.JobID,
			Status:    convertStatusToService(exec.Status),
			StartedAt: exec.StartedAt,
			EndedAt:   exec.CompletedAt,
			Duration:  exec.Duration,
			Error:     exec.Error,
			Output:    string(exec.Result),
		}
	}

	return &service.GetSchedulerJobHistoryResponse{
		Executions: executions,
		HasMore:    resp.HasMore,
		Timestamp:  time.Now(),
	}, nil
}

// SchedulerGetStatus retrieves scheduler status via MCP.
func (c *MCPSchedulerClient) SchedulerGetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	if c.client == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerGetStatus",
			"MCP client not initialized",
		)
	}
	schedulerClient := c.client.Scheduler()
	if schedulerClient == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPSchedulerClient.SchedulerGetStatus",
			"scheduler client not available",
		)
	}

	status, err := schedulerClient.GetStatus(ctx)
	if err != nil {
		return nil, err
	}

	return &service.GetSchedulerStatusResponse{
		Running: status.Healthy,
		Stats: &service.SchedulerStats{
			JobCount:     status.TotalJobs,
			RunningJobs:  status.RunningJobs,
			SuccessCount: status.CompletedJobs,
			FailureCount: status.FailedJobs,
			StartedAt:    status.LastHeartbeat,
			LastTickTime: status.LastHeartbeat,
		},
		Timestamp: time.Now(),
	}, nil
}

// Close closes the MCP client connection.
func (c *MCPSchedulerClient) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// createSchedulerMCPClientFactory creates an MCP client for scheduler operations.
// MCP transport for scheduler commands uses JSON-RPC 2.0 over HTTP.
func createSchedulerMCPClientFactory() (SchedulerClient, error) {
	// Create the SDK client with MCP transport
	sdkClient, err := createSDKClient()
	if err != nil {
		return nil, errors.Wrapf(
			err,
			errors.ErrUnavailable,
			"createSchedulerMCPClientFactory",
			"failed to create MCP SDK client for server %s", address,
		)
	}

	return &MCPSchedulerClient{client: sdkClient}, nil
}

// Conversion helper functions

// convertJobTypeToSDK converts service.SchedulerJobType to scheduler.JobType.
func convertJobTypeToSDK(t service.SchedulerJobType) scheduler.JobType {
	switch t {
	case service.SchedulerJobTypeOneShot:
		return scheduler.JobTypeOneShot
	case service.SchedulerJobTypeRecurring:
		return scheduler.JobTypeRecurring
	case service.SchedulerJobTypeDelayed:
		return scheduler.JobTypeDelayed
	default:
		return scheduler.JobTypeOneShot
	}
}

// convertJobTypeToService converts scheduler.JobType to service.SchedulerJobType.
func convertJobTypeToService(t scheduler.JobType) service.SchedulerJobType {
	switch t {
	case scheduler.JobTypeOneShot:
		return service.SchedulerJobTypeOneShot
	case scheduler.JobTypeRecurring:
		return service.SchedulerJobTypeRecurring
	case scheduler.JobTypeDelayed:
		return service.SchedulerJobTypeDelayed
	default:
		return service.SchedulerJobTypeOneShot
	}
}

// convertOwnerTypeToSDK converts service.SchedulerOwnerType to scheduler.OwnerType.
func convertOwnerTypeToSDK(t service.SchedulerOwnerType) scheduler.OwnerType {
	switch t {
	case service.SchedulerOwnerTypeSystem:
		return scheduler.OwnerTypeSystem
	case service.SchedulerOwnerTypeTenant:
		return scheduler.OwnerTypeTenant
	case service.SchedulerOwnerTypeDelegated:
		return scheduler.OwnerTypeDelegated
	default:
		return scheduler.OwnerTypeSystem
	}
}

// convertOwnerTypeToService converts scheduler.OwnerType to service.SchedulerOwnerType.
func convertOwnerTypeToService(t scheduler.OwnerType) service.SchedulerOwnerType {
	switch t {
	case scheduler.OwnerTypeSystem:
		return service.SchedulerOwnerTypeSystem
	case scheduler.OwnerTypeTenant:
		return service.SchedulerOwnerTypeTenant
	case scheduler.OwnerTypeDelegated:
		return service.SchedulerOwnerTypeDelegated
	default:
		return service.SchedulerOwnerTypeSystem
	}
}

// convertStatusToSDK converts service.SchedulerJobStatus to scheduler.JobStatus.
func convertStatusToSDK(s service.SchedulerJobStatus) scheduler.JobStatus {
	switch s {
	case service.SchedulerJobStatusPending:
		return scheduler.JobStatusPending
	case service.SchedulerJobStatusRunning:
		return scheduler.JobStatusRunning
	case service.SchedulerJobStatusCompleted:
		return scheduler.JobStatusCompleted
	case service.SchedulerJobStatusFailed:
		return scheduler.JobStatusFailed
	case service.SchedulerJobStatusPaused:
		return scheduler.JobStatusPaused
	case service.SchedulerJobStatusCanceled:
		return scheduler.JobStatusCanceled
	default:
		return scheduler.JobStatusPending
	}
}

// convertStatusToService converts scheduler.JobStatus to service.SchedulerJobStatus.
func convertStatusToService(s scheduler.JobStatus) service.SchedulerJobStatus {
	switch s {
	case scheduler.JobStatusPending:
		return service.SchedulerJobStatusPending
	case scheduler.JobStatusRunning:
		return service.SchedulerJobStatusRunning
	case scheduler.JobStatusCompleted:
		return service.SchedulerJobStatusCompleted
	case scheduler.JobStatusFailed:
		return service.SchedulerJobStatusFailed
	case scheduler.JobStatusPaused:
		return service.SchedulerJobStatusPaused
	case scheduler.JobStatusCanceled:
		return service.SchedulerJobStatusCanceled
	default:
		return service.SchedulerJobStatusPending
	}
}

// convertJobInfoToService converts scheduler.JobInfo to service.SchedulerJob.
func convertJobInfoToService(info *scheduler.JobInfo) *service.SchedulerJob {
	if info == nil {
		return nil
	}
	return &service.SchedulerJob{
		ID:          info.JobID,
		Name:        info.Name,
		Description: info.Description,
		Type:        convertJobTypeToService(info.JobType),
		Status:      convertStatusToService(info.Status),
		Schedule:    info.Schedule,
		Payload:     info.Payload,
		OwnerType:   convertOwnerTypeToService(info.OwnerType),
		OwnerID:     info.OwnerID,
		Priority:    0,
		Timeout:     info.Timeout,
		MaxRetries:  int32(info.MaxRetries),
		Labels:      info.Tags,
		NextRun:     info.NextRunAt,
		LastRun:     info.LastRunAt,
		RunCount:    info.ExecutionCount,
		FailCount:   info.FailureCount,
		CreatedAt:   info.CreatedAt,
		UpdatedAt:   info.UpdatedAt,
	}
}

// Compile-time interface check.
var _ SchedulerClient = (*MCPSchedulerClient)(nil)
