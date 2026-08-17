//go:build !quic || !rest

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

// Package main provides stub QUIC scheduler client when QUIC support is not enabled.
package main

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// QUICSchedulerClient is a stub type when QUIC is not enabled.
type QUICSchedulerClient struct{}

// SchedulerCreateJob returns not implemented error.
func (c *QUICSchedulerClient) SchedulerCreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("QUICSchedulerClient.SchedulerCreateJob", "QUIC transport requires -tags=quic build flag")
}

// SchedulerGetJob returns not implemented error.
func (c *QUICSchedulerClient) SchedulerGetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("QUICSchedulerClient.SchedulerGetJob", "QUIC transport requires -tags=quic build flag")
}

// SchedulerUpdateJob returns not implemented error.
func (c *QUICSchedulerClient) SchedulerUpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("QUICSchedulerClient.SchedulerUpdateJob", "QUIC transport requires -tags=quic build flag")
}

// SchedulerDeleteJob returns not implemented error.
func (c *QUICSchedulerClient) SchedulerDeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("QUICSchedulerClient.SchedulerDeleteJob", "QUIC transport requires -tags=quic build flag")
}

// SchedulerListJobs returns not implemented error.
func (c *QUICSchedulerClient) SchedulerListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	return nil, errors.NotImplemented("QUICSchedulerClient.SchedulerListJobs", "QUIC transport requires -tags=quic build flag")
}

// SchedulerTriggerJob returns not implemented error.
func (c *QUICSchedulerClient) SchedulerTriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("QUICSchedulerClient.SchedulerTriggerJob", "QUIC transport requires -tags=quic build flag")
}

// SchedulerPauseJob returns not implemented error.
func (c *QUICSchedulerClient) SchedulerPauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("QUICSchedulerClient.SchedulerPauseJob", "QUIC transport requires -tags=quic build flag")
}

// SchedulerResumeJob returns not implemented error.
func (c *QUICSchedulerClient) SchedulerResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("QUICSchedulerClient.SchedulerResumeJob", "QUIC transport requires -tags=quic build flag")
}

// SchedulerCancelJob returns not implemented error.
func (c *QUICSchedulerClient) SchedulerCancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("QUICSchedulerClient.SchedulerCancelJob", "QUIC transport requires -tags=quic build flag")
}

// SchedulerGetJobHistory returns not implemented error.
func (c *QUICSchedulerClient) SchedulerGetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	return nil, errors.NotImplemented("QUICSchedulerClient.SchedulerGetJobHistory", "QUIC transport requires -tags=quic build flag")
}

// SchedulerGetStatus returns not implemented error.
func (c *QUICSchedulerClient) SchedulerGetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	return nil, errors.NotImplemented("QUICSchedulerClient.SchedulerGetStatus", "QUIC transport requires -tags=quic build flag")
}

// Close is a no-op for the stub.
func (c *QUICSchedulerClient) Close() error {
	return nil
}

// createSchedulerQUICClientFactory is a stub that returns an error when QUIC is not enabled.
func createSchedulerQUICClientFactory() (SchedulerClient, error) {
	return nil, errors.NotImplemented("createSchedulerQUICClientFactory", "QUIC transport requires -tags=quic build flag")
}

// Compile-time interface check.
var _ SchedulerClient = (*QUICSchedulerClient)(nil)
