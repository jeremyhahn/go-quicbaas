//go:build !mcp

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

// Package main provides stub MCP scheduler client when MCP support is not enabled.
package main

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// MCPSchedulerClient is a stub type when MCP is not enabled.
type MCPSchedulerClient struct{}

// SchedulerCreateJob returns not implemented error.
func (c *MCPSchedulerClient) SchedulerCreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("MCPSchedulerClient.SchedulerCreateJob", "MCP transport requires -tags=mcp build flag")
}

// SchedulerGetJob returns not implemented error.
func (c *MCPSchedulerClient) SchedulerGetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("MCPSchedulerClient.SchedulerGetJob", "MCP transport requires -tags=mcp build flag")
}

// SchedulerUpdateJob returns not implemented error.
func (c *MCPSchedulerClient) SchedulerUpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("MCPSchedulerClient.SchedulerUpdateJob", "MCP transport requires -tags=mcp build flag")
}

// SchedulerDeleteJob returns not implemented error.
func (c *MCPSchedulerClient) SchedulerDeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("MCPSchedulerClient.SchedulerDeleteJob", "MCP transport requires -tags=mcp build flag")
}

// SchedulerListJobs returns not implemented error.
func (c *MCPSchedulerClient) SchedulerListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	return nil, errors.NotImplemented("MCPSchedulerClient.SchedulerListJobs", "MCP transport requires -tags=mcp build flag")
}

// SchedulerTriggerJob returns not implemented error.
func (c *MCPSchedulerClient) SchedulerTriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("MCPSchedulerClient.SchedulerTriggerJob", "MCP transport requires -tags=mcp build flag")
}

// SchedulerPauseJob returns not implemented error.
func (c *MCPSchedulerClient) SchedulerPauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("MCPSchedulerClient.SchedulerPauseJob", "MCP transport requires -tags=mcp build flag")
}

// SchedulerResumeJob returns not implemented error.
func (c *MCPSchedulerClient) SchedulerResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("MCPSchedulerClient.SchedulerResumeJob", "MCP transport requires -tags=mcp build flag")
}

// SchedulerCancelJob returns not implemented error.
func (c *MCPSchedulerClient) SchedulerCancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	return nil, errors.NotImplemented("MCPSchedulerClient.SchedulerCancelJob", "MCP transport requires -tags=mcp build flag")
}

// SchedulerGetJobHistory returns not implemented error.
func (c *MCPSchedulerClient) SchedulerGetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	return nil, errors.NotImplemented("MCPSchedulerClient.SchedulerGetJobHistory", "MCP transport requires -tags=mcp build flag")
}

// SchedulerGetStatus returns not implemented error.
func (c *MCPSchedulerClient) SchedulerGetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	return nil, errors.NotImplemented("MCPSchedulerClient.SchedulerGetStatus", "MCP transport requires -tags=mcp build flag")
}

// Close is a no-op for the stub.
func (c *MCPSchedulerClient) Close() error {
	return nil
}

// createSchedulerMCPClientFactory is a stub that returns an error when MCP is not enabled.
func createSchedulerMCPClientFactory() (SchedulerClient, error) {
	return nil, errors.NotImplemented("createSchedulerMCPClientFactory", "MCP transport requires -tags=mcp build flag")
}

// Compile-time interface check.
var _ SchedulerClient = (*MCPSchedulerClient)(nil)
