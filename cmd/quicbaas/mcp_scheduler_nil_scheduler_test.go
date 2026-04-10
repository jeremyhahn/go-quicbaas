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

// Package main provides tests for MCP scheduler client when the underlying
// quicbaas.Client returns nil from Scheduler() -- the "scheduler client not
// available" error path.
package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// All tests in this file use mockClient from client_test.go, which returns
// nil from Scheduler(). This covers the second nil check in each method:
//   schedulerClient := c.client.Scheduler()
//   if schedulerClient == nil { ... }

func TestMCPSchedulerClient_CreateJob_NilScheduler(t *testing.T) {
	client := &MCPSchedulerClient{client: &mockClient{}}
	resp, err := client.SchedulerCreateJob(context.Background(), &service.CreateSchedulerJobRequest{
		Name: "test-job",
	})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scheduler client not available")
}

func TestMCPSchedulerClient_GetJob_NilScheduler(t *testing.T) {
	client := &MCPSchedulerClient{client: &mockClient{}}
	resp, err := client.SchedulerGetJob(context.Background(), &service.GetSchedulerJobRequest{
		JobID: 1,
	})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scheduler client not available")
}

func TestMCPSchedulerClient_UpdateJob_NilScheduler(t *testing.T) {
	client := &MCPSchedulerClient{client: &mockClient{}}
	resp, err := client.SchedulerUpdateJob(context.Background(), &service.UpdateSchedulerJobRequest{
		JobID: 1,
	})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scheduler client not available")
}

func TestMCPSchedulerClient_DeleteJob_NilScheduler(t *testing.T) {
	client := &MCPSchedulerClient{client: &mockClient{}}
	resp, err := client.SchedulerDeleteJob(context.Background(), &service.DeleteSchedulerJobRequest{
		JobID: 1,
	})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scheduler client not available")
}

func TestMCPSchedulerClient_ListJobs_NilScheduler(t *testing.T) {
	client := &MCPSchedulerClient{client: &mockClient{}}
	resp, err := client.SchedulerListJobs(context.Background(), &service.ListSchedulerJobsRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scheduler client not available")
}

func TestMCPSchedulerClient_TriggerJob_NilScheduler(t *testing.T) {
	client := &MCPSchedulerClient{client: &mockClient{}}
	resp, err := client.SchedulerTriggerJob(context.Background(), &service.TriggerSchedulerJobRequest{
		JobID: 1,
	})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scheduler client not available")
}

func TestMCPSchedulerClient_PauseJob_NilScheduler(t *testing.T) {
	client := &MCPSchedulerClient{client: &mockClient{}}
	resp, err := client.SchedulerPauseJob(context.Background(), &service.PauseSchedulerJobRequest{
		JobID: 1,
	})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scheduler client not available")
}

func TestMCPSchedulerClient_ResumeJob_NilScheduler(t *testing.T) {
	client := &MCPSchedulerClient{client: &mockClient{}}
	resp, err := client.SchedulerResumeJob(context.Background(), &service.ResumeSchedulerJobRequest{
		JobID: 1,
	})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scheduler client not available")
}

func TestMCPSchedulerClient_CancelJob_NilScheduler(t *testing.T) {
	client := &MCPSchedulerClient{client: &mockClient{}}
	resp, err := client.SchedulerCancelJob(context.Background(), &service.CancelSchedulerJobRequest{
		JobID: 1,
	})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scheduler client not available")
}

func TestMCPSchedulerClient_GetJobHistory_NilScheduler(t *testing.T) {
	client := &MCPSchedulerClient{client: &mockClient{}}
	resp, err := client.SchedulerGetJobHistory(context.Background(), &service.GetSchedulerJobHistoryRequest{
		JobID: 1,
	})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scheduler client not available")
}

func TestMCPSchedulerClient_GetStatus_NilScheduler(t *testing.T) {
	client := &MCPSchedulerClient{client: &mockClient{}}
	resp, err := client.SchedulerGetStatus(context.Background())
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scheduler client not available")
}

func TestMCPSchedulerClient_Close_NonNilClient(t *testing.T) {
	client := &MCPSchedulerClient{client: &mockClient{}}
	err := client.Close()
	assert.NoError(t, err)
}
