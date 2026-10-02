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

package grpc

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// SchedulerJobsClient presents the client's Scheduler* job RPCs as a
// service.SchedulerService, so a remote caller drives the job half exactly
// as the in-process service — the same shape the placement half has in
// PlacementClient.
type SchedulerJobsClient struct {
	c *Client
}

// Compile-time proof SchedulerJobsClient is a service.SchedulerService.
var _ service.SchedulerService = (*SchedulerJobsClient)(nil)

// Jobs returns the scheduler job client over this connection.
func (c *Client) Jobs() *SchedulerJobsClient {
	return &SchedulerJobsClient{c: c}
}

// CreateJob creates a job.
func (j *SchedulerJobsClient) CreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	return j.c.SchedulerCreateJob(ctx, req)
}

// GetJob reads a job.
func (j *SchedulerJobsClient) GetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	return j.c.SchedulerGetJob(ctx, req)
}

// UpdateJob updates a job.
func (j *SchedulerJobsClient) UpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	return j.c.SchedulerUpdateJob(ctx, req)
}

// DeleteJob deletes a job.
func (j *SchedulerJobsClient) DeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	return j.c.SchedulerDeleteJob(ctx, req)
}

// ListJobs lists jobs.
func (j *SchedulerJobsClient) ListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	return j.c.SchedulerListJobs(ctx, req)
}

// TriggerJob runs a job now.
func (j *SchedulerJobsClient) TriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	return j.c.SchedulerTriggerJob(ctx, req)
}

// PauseJob pauses a job.
func (j *SchedulerJobsClient) PauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	return j.c.SchedulerPauseJob(ctx, req)
}

// ResumeJob resumes a job.
func (j *SchedulerJobsClient) ResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	return j.c.SchedulerResumeJob(ctx, req)
}

// CancelJob cancels a job.
func (j *SchedulerJobsClient) CancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	return j.c.SchedulerCancelJob(ctx, req)
}

// GetJobHistory reads a job's executions.
func (j *SchedulerJobsClient) GetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	return j.c.SchedulerGetJobHistory(ctx, req)
}

// GetStatus reads the scheduler status.
func (j *SchedulerJobsClient) GetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	return j.c.SchedulerGetStatus(ctx)
}
