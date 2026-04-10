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

// Package main provides tests covering the API error branch (formatError path)
// in scheduler run commands. When the scheduler service method returns an error,
// the command should call formatError and propagate the error.
package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestRunSchedulerListCommand_APIError(t *testing.T) {
	saved := schedulerClientFactory
	defer func() { schedulerClientFactory = saved }()

	schedulerClientFactory = func() (SchedulerClient, error) {
		return &MockSchedulerClient{
			SchedulerListJobsFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
				return nil, errors.Newf(errors.ErrInternal, "scheduler.list", "service unavailable")
			},
		}, nil
	}

	cmd := newSchedulerListCommand()
	err := cmd.RunE(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "service unavailable")
}

func TestRunSchedulerGetCommand_APIError(t *testing.T) {
	saved := schedulerClientFactory
	defer func() { schedulerClientFactory = saved }()

	schedulerClientFactory = func() (SchedulerClient, error) {
		return &MockSchedulerClient{
			SchedulerGetJobFunc: func(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
				return nil, errors.Newf(errors.ErrNotFound, "scheduler.get", "job not found")
			},
		}, nil
	}

	cmd := newSchedulerGetCommand()
	err := cmd.RunE(cmd, []string{"1"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "job not found")
}

func TestRunSchedulerDeleteCommand_APIError(t *testing.T) {
	saved := schedulerClientFactory
	defer func() { schedulerClientFactory = saved }()

	schedulerClientFactory = func() (SchedulerClient, error) {
		return &MockSchedulerClient{
			SchedulerDeleteJobFunc: func(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
				return nil, errors.Newf(errors.ErrNotFound, "scheduler.delete", "job not found")
			},
		}, nil
	}

	cmd := newSchedulerDeleteCommand()
	err := cmd.RunE(cmd, []string{"1"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "job not found")
}

func TestRunSchedulerTriggerCommand_APIError(t *testing.T) {
	saved := schedulerClientFactory
	defer func() { schedulerClientFactory = saved }()

	schedulerClientFactory = func() (SchedulerClient, error) {
		return &MockSchedulerClient{
			SchedulerTriggerJobFunc: func(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
				return nil, errors.Newf(errors.ErrInternal, "scheduler.trigger", "trigger failed")
			},
		}, nil
	}

	cmd := newSchedulerTriggerCommand()
	err := cmd.RunE(cmd, []string{"1"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "trigger failed")
}

func TestRunSchedulerPauseCommand_APIError(t *testing.T) {
	saved := schedulerClientFactory
	defer func() { schedulerClientFactory = saved }()

	schedulerClientFactory = func() (SchedulerClient, error) {
		return &MockSchedulerClient{
			SchedulerPauseJobFunc: func(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
				return nil, errors.Newf(errors.ErrInternal, "scheduler.pause", "pause failed")
			},
		}, nil
	}

	cmd := newSchedulerPauseCommand()
	err := cmd.RunE(cmd, []string{"1"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pause failed")
}

func TestRunSchedulerResumeCommand_APIError(t *testing.T) {
	saved := schedulerClientFactory
	defer func() { schedulerClientFactory = saved }()

	schedulerClientFactory = func() (SchedulerClient, error) {
		return &MockSchedulerClient{
			SchedulerResumeJobFunc: func(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
				return nil, errors.Newf(errors.ErrInternal, "scheduler.resume", "resume failed")
			},
		}, nil
	}

	cmd := newSchedulerResumeCommand()
	err := cmd.RunE(cmd, []string{"1"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "resume failed")
}

func TestRunSchedulerCancelCommand_APIError(t *testing.T) {
	saved := schedulerClientFactory
	defer func() { schedulerClientFactory = saved }()

	schedulerClientFactory = func() (SchedulerClient, error) {
		return &MockSchedulerClient{
			SchedulerCancelJobFunc: func(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
				return nil, errors.Newf(errors.ErrInternal, "scheduler.cancel", "cancel failed")
			},
		}, nil
	}

	cmd := newSchedulerCancelCommand()
	err := cmd.RunE(cmd, []string{"1"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cancel failed")
}

func TestRunSchedulerHistoryCommand_APIError(t *testing.T) {
	saved := schedulerClientFactory
	defer func() { schedulerClientFactory = saved }()

	schedulerClientFactory = func() (SchedulerClient, error) {
		return &MockSchedulerClient{
			SchedulerGetJobHistoryFunc: func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
				return nil, errors.Newf(errors.ErrNotFound, "scheduler.history", "history not found")
			},
		}, nil
	}

	cmd := newSchedulerHistoryCommand()
	err := cmd.RunE(cmd, []string{"1"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "history not found")
}

func TestRunSchedulerStatusCommand_APIError(t *testing.T) {
	saved := schedulerClientFactory
	defer func() { schedulerClientFactory = saved }()

	schedulerClientFactory = func() (SchedulerClient, error) {
		return &MockSchedulerClient{
			SchedulerGetStatusFunc: func(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
				return nil, errors.Newf(errors.ErrInternal, "scheduler.status", "status unavailable")
			},
		}, nil
	}

	cmd := newSchedulerStatusCommand()
	err := cmd.RunE(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status unavailable")
}
