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

// Package schedulertest provides a recording SchedulerService for transport
// tests: it keeps the last request each method received, so a test asserts
// what reached the server rather than only that a call returned.
package schedulertest

import (
	"context"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Jobs is an in-memory service.SchedulerService that records requests and
// answers with a fixed job. Err, when set, is returned by every method.
type Jobs struct {
	mu sync.Mutex

	// Job is the job every job-returning method answers with.
	Job *service.SchedulerJob

	// Err is returned by every method when non-nil.
	Err error

	// Last holds the last request each method received, keyed by method.
	Last map[string]any
}

// New returns a Jobs answering with a populated job.
func New() *Jobs {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	return &Jobs{
		Job: &service.SchedulerJob{
			ID:        42,
			Name:      "nightly",
			Type:      service.SchedulerJobTypeRecurring,
			Status:    service.SchedulerJobStatusPending,
			OwnerType: service.SchedulerOwnerTypeTenant,
			OwnerID:   "acme",
			Schedule:  "0 2 * * *",
			Handler:   "noop",
			Labels:    map[string]string{"team": "ops"},
			NextRun:   now.Add(time.Hour),
			RunCount:  3,
			CreatedAt: now,
			UpdatedAt: now,
		},
		Last: map[string]any{},
	}
}

// Received returns the last request method received.
func (j *Jobs) Received(method string) any {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Last[method]
}

// record stores a request and returns the configured error.
func (j *Jobs) record(method string, req any) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Last[method] = req
	return j.Err
}

func (j *Jobs) stamp() time.Time { return j.Job.UpdatedAt }

// CreateJob records the request.
func (j *Jobs) CreateJob(_ context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	if err := j.record("CreateJob", req); err != nil {
		return nil, err
	}
	return &service.CreateSchedulerJobResponse{Job: j.Job, Timestamp: j.stamp()}, nil
}

// GetJob records the request.
func (j *Jobs) GetJob(_ context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	if err := j.record("GetJob", req); err != nil {
		return nil, err
	}
	return &service.GetSchedulerJobResponse{Job: j.Job, Timestamp: j.stamp()}, nil
}

// UpdateJob records the request.
func (j *Jobs) UpdateJob(_ context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	if err := j.record("UpdateJob", req); err != nil {
		return nil, err
	}
	return &service.UpdateSchedulerJobResponse{Job: j.Job, Timestamp: j.stamp()}, nil
}

// DeleteJob records the request.
func (j *Jobs) DeleteJob(_ context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	if err := j.record("DeleteJob", req); err != nil {
		return nil, err
	}
	return &service.DeleteSchedulerJobResponse{JobID: req.JobID, Deleted: true, Timestamp: j.stamp()}, nil
}

// ListJobs records the request.
func (j *Jobs) ListJobs(_ context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	if err := j.record("ListJobs", req); err != nil {
		return nil, err
	}
	return &service.ListSchedulerJobsResponse{Jobs: []*service.SchedulerJob{j.Job}, TotalCount: 1, Timestamp: j.stamp()}, nil
}

// TriggerJob records the request.
func (j *Jobs) TriggerJob(_ context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	if err := j.record("TriggerJob", req); err != nil {
		return nil, err
	}
	return &service.TriggerSchedulerJobResponse{JobID: req.JobID, Status: service.SchedulerJobStatusRunning, Timestamp: j.stamp()}, nil
}

// PauseJob records the request.
func (j *Jobs) PauseJob(_ context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	if err := j.record("PauseJob", req); err != nil {
		return nil, err
	}
	return &service.PauseSchedulerJobResponse{JobID: req.JobID, OldStatus: service.SchedulerJobStatusPending, NewStatus: service.SchedulerJobStatusPaused, Timestamp: j.stamp()}, nil
}

// ResumeJob records the request.
func (j *Jobs) ResumeJob(_ context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	if err := j.record("ResumeJob", req); err != nil {
		return nil, err
	}
	return &service.ResumeSchedulerJobResponse{JobID: req.JobID, OldStatus: service.SchedulerJobStatusPaused, NewStatus: service.SchedulerJobStatusPending, Timestamp: j.stamp()}, nil
}

// CancelJob records the request.
func (j *Jobs) CancelJob(_ context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	if err := j.record("CancelJob", req); err != nil {
		return nil, err
	}
	return &service.CancelSchedulerJobResponse{JobID: req.JobID, OldStatus: service.SchedulerJobStatusPending, NewStatus: service.SchedulerJobStatusCanceled, Timestamp: j.stamp()}, nil
}

// GetJobHistory records the request.
func (j *Jobs) GetJobHistory(_ context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	if err := j.record("GetJobHistory", req); err != nil {
		return nil, err
	}
	return &service.GetSchedulerJobHistoryResponse{
		Executions: []*service.SchedulerJobExecution{{
			ID: 7, JobID: req.JobID, Status: service.SchedulerJobStatusCompleted,
			StartedAt: j.stamp(), EndedAt: j.stamp().Add(time.Second), Duration: time.Second, Output: "ok",
		}},
		Timestamp: j.stamp(),
	}, nil
}

// GetStatus records the call.
func (j *Jobs) GetStatus(_ context.Context) (*service.GetSchedulerStatusResponse, error) {
	if err := j.record("GetStatus", struct{}{}); err != nil {
		return nil, err
	}
	return &service.GetSchedulerStatusResponse{
		Running:   true,
		Stats:     &service.SchedulerStats{JobCount: 5, RunningJobs: 1, SuccessCount: 3, FailureCount: 1, StartedAt: j.stamp()},
		Timestamp: j.stamp(),
	}, nil
}
