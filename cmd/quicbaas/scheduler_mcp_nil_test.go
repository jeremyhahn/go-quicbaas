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

// Package main provides tests for MCP scheduler client nil-client error paths.
package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// TestMCPSchedulerClient_SchedulerCreateJob_NilClient verifies nil client error.
func TestMCPSchedulerClient_SchedulerCreateJob_NilClient(t *testing.T) {
	client := &MCPSchedulerClient{client: nil}
	resp, err := client.SchedulerCreateJob(context.Background(), &service.CreateSchedulerJobRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP client not initialized")
}

// TestMCPSchedulerClient_SchedulerGetJob_NilClient verifies nil client error.
func TestMCPSchedulerClient_SchedulerGetJob_NilClient(t *testing.T) {
	client := &MCPSchedulerClient{client: nil}
	resp, err := client.SchedulerGetJob(context.Background(), &service.GetSchedulerJobRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP client not initialized")
}

// TestMCPSchedulerClient_SchedulerUpdateJob_NilClient verifies nil client error.
func TestMCPSchedulerClient_SchedulerUpdateJob_NilClient(t *testing.T) {
	client := &MCPSchedulerClient{client: nil}
	resp, err := client.SchedulerUpdateJob(context.Background(), &service.UpdateSchedulerJobRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP client not initialized")
}

// TestMCPSchedulerClient_SchedulerDeleteJob_NilClient verifies nil client error.
func TestMCPSchedulerClient_SchedulerDeleteJob_NilClient(t *testing.T) {
	client := &MCPSchedulerClient{client: nil}
	resp, err := client.SchedulerDeleteJob(context.Background(), &service.DeleteSchedulerJobRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP client not initialized")
}

// TestMCPSchedulerClient_SchedulerListJobs_NilClient verifies nil client error.
func TestMCPSchedulerClient_SchedulerListJobs_NilClient(t *testing.T) {
	client := &MCPSchedulerClient{client: nil}
	resp, err := client.SchedulerListJobs(context.Background(), &service.ListSchedulerJobsRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP client not initialized")
}

// TestMCPSchedulerClient_SchedulerTriggerJob_NilClient verifies nil client error.
func TestMCPSchedulerClient_SchedulerTriggerJob_NilClient(t *testing.T) {
	client := &MCPSchedulerClient{client: nil}
	resp, err := client.SchedulerTriggerJob(context.Background(), &service.TriggerSchedulerJobRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP client not initialized")
}

// TestMCPSchedulerClient_SchedulerPauseJob_NilClient verifies nil client error.
func TestMCPSchedulerClient_SchedulerPauseJob_NilClient(t *testing.T) {
	client := &MCPSchedulerClient{client: nil}
	resp, err := client.SchedulerPauseJob(context.Background(), &service.PauseSchedulerJobRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP client not initialized")
}

// TestMCPSchedulerClient_SchedulerResumeJob_NilClient verifies nil client error.
func TestMCPSchedulerClient_SchedulerResumeJob_NilClient(t *testing.T) {
	client := &MCPSchedulerClient{client: nil}
	resp, err := client.SchedulerResumeJob(context.Background(), &service.ResumeSchedulerJobRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP client not initialized")
}

// TestMCPSchedulerClient_SchedulerCancelJob_NilClient verifies nil client error.
func TestMCPSchedulerClient_SchedulerCancelJob_NilClient(t *testing.T) {
	client := &MCPSchedulerClient{client: nil}
	resp, err := client.SchedulerCancelJob(context.Background(), &service.CancelSchedulerJobRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP client not initialized")
}

// TestMCPSchedulerClient_SchedulerGetJobHistory_NilClient verifies nil client error.
func TestMCPSchedulerClient_SchedulerGetJobHistory_NilClient(t *testing.T) {
	client := &MCPSchedulerClient{client: nil}
	resp, err := client.SchedulerGetJobHistory(context.Background(), &service.GetSchedulerJobHistoryRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP client not initialized")
}

// TestMCPSchedulerClient_SchedulerGetStatus_NilClient verifies nil client error.
func TestMCPSchedulerClient_SchedulerGetStatus_NilClient(t *testing.T) {
	client := &MCPSchedulerClient{client: nil}
	resp, err := client.SchedulerGetStatus(context.Background())
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP client not initialized")
}

// TestMCPSchedulerClient_Close_NilClient verifies nil client close returns nil.
func TestMCPSchedulerClient_Close_NilClient(t *testing.T) {
	client := &MCPSchedulerClient{client: nil}
	err := client.Close()
	assert.NoError(t, err)
}
