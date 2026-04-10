//go:build !rest

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

// Package main provides stub REST scheduler client when REST support is not enabled.
package main

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// RESTSchedulerClient is a stub type when REST is not enabled.
type RESTSchedulerClient struct{}

// SchedulerCreateJob returns not implemented error.
func (c *RESTSchedulerClient) SchedulerCreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("RESTSchedulerClient.SchedulerCreateJob", "REST transport requires -tags=rest build flag")
}

// SchedulerGetJob returns not implemented error.
func (c *RESTSchedulerClient) SchedulerGetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("RESTSchedulerClient.SchedulerGetJob", "REST transport requires -tags=rest build flag")
}

// SchedulerUpdateJob returns not implemented error.
func (c *RESTSchedulerClient) SchedulerUpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("RESTSchedulerClient.SchedulerUpdateJob", "REST transport requires -tags=rest build flag")
}

// SchedulerDeleteJob returns not implemented error.
func (c *RESTSchedulerClient) SchedulerDeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("RESTSchedulerClient.SchedulerDeleteJob", "REST transport requires -tags=rest build flag")
}

// SchedulerListJobs returns not implemented error.
func (c *RESTSchedulerClient) SchedulerListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	return nil, errors.NotImplemented("RESTSchedulerClient.SchedulerListJobs", "REST transport requires -tags=rest build flag")
}

// SchedulerTriggerJob returns not implemented error.
func (c *RESTSchedulerClient) SchedulerTriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("RESTSchedulerClient.SchedulerTriggerJob", "REST transport requires -tags=rest build flag")
}

// SchedulerPauseJob returns not implemented error.
func (c *RESTSchedulerClient) SchedulerPauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("RESTSchedulerClient.SchedulerPauseJob", "REST transport requires -tags=rest build flag")
}

// SchedulerResumeJob returns not implemented error.
func (c *RESTSchedulerClient) SchedulerResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("RESTSchedulerClient.SchedulerResumeJob", "REST transport requires -tags=rest build flag")
}

// SchedulerCancelJob returns not implemented error.
func (c *RESTSchedulerClient) SchedulerCancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("RESTSchedulerClient.SchedulerCancelJob", "REST transport requires -tags=rest build flag")
}

// SchedulerGetJobHistory returns not implemented error.
func (c *RESTSchedulerClient) SchedulerGetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	return nil, errors.NotImplemented("RESTSchedulerClient.SchedulerGetJobHistory", "REST transport requires -tags=rest build flag")
}

// SchedulerGetStatus returns not implemented error.
func (c *RESTSchedulerClient) SchedulerGetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	return nil, errors.NotImplemented("RESTSchedulerClient.SchedulerGetStatus", "REST transport requires -tags=rest build flag")
}

// Close is a no-op for the stub.
func (c *RESTSchedulerClient) Close() error {
	return nil
}

// createSchedulerRESTClientFactory is a stub that returns an error when REST is not enabled.
func createSchedulerRESTClientFactory() (SchedulerClient, error) {
	return nil, errors.NotImplemented("createSchedulerRESTClientFactory", "REST transport requires -tags=rest build flag")
}

// Compile-time interface check.
var _ SchedulerClient = (*RESTSchedulerClient)(nil)
