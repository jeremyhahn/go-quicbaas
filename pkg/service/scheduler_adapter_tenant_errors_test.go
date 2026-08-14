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

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// =============================================================================
// SchedulerAdapter tenant-resolution + cross-tenant + engine-error branches.
//
// The happy paths are covered in scheduler_adapter_test.go; this file pins the
// previously-uncovered guard branches: an invalid tenant id (resolveID error),
// a returned job belonging to another tenant (enforceAccess denial), and the
// engine returning an error.
// =============================================================================

// invalidTenantCtx yields a context whose IAM tenant fails validation, so the
// schedulerTenantResolveID guard at the top of every adapter method fires.
func invalidTenantCtx() context.Context {
	return iam.ContextWithTenant(context.Background(), "bad/tenant")
}

func crossTenantCtx() context.Context {
	return iam.ContextWithTenant(context.Background(), "tenant-a")
}

func TestSchedulerAdapter_InvalidTenant_AllMethods(t *testing.T) {
	eng := newTestMockSchedulerEngine()
	a := &SchedulerAdapter{scheduler: eng}
	ctx := invalidTenantCtx()

	cases := []struct {
		name   string
		invoke func() error
	}{
		{"ScheduleJob", func() error { return a.ScheduleJob(ctx, &SchedulerJob{Name: "j"}) }},
		{"CreateJob", func() error { _, err := a.CreateJob(ctx, &CreateSchedulerJobRequest{}); return err }},
		{"GetJob", func() error { _, err := a.GetJob(ctx, &GetSchedulerJobRequest{JobID: 1}); return err }},
		{"UpdateJob", func() error { _, err := a.UpdateJob(ctx, &UpdateSchedulerJobRequest{JobID: 1}); return err }},
		{"DeleteJob", func() error { _, err := a.DeleteJob(ctx, &DeleteSchedulerJobRequest{JobID: 1}); return err }},
		{"ListJobs", func() error { _, err := a.ListJobs(ctx, &ListSchedulerJobsRequest{}); return err }},
		{"TriggerJob", func() error { _, err := a.TriggerJob(ctx, &TriggerSchedulerJobRequest{JobID: 1}); return err }},
		{"PauseJob", func() error { _, err := a.PauseJob(ctx, &PauseSchedulerJobRequest{JobID: 1}); return err }},
		{"ResumeJob", func() error { _, err := a.ResumeJob(ctx, &ResumeSchedulerJobRequest{JobID: 1}); return err }},
		{"CancelJob", func() error { _, err := a.CancelJob(ctx, &CancelSchedulerJobRequest{JobID: 1}); return err }},
		{"GetJobHistory", func() error { _, err := a.GetJobHistory(ctx, &GetSchedulerJobHistoryRequest{JobID: 1}); return err }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, tc.invoke())
		})
	}

	// The scheduler engine must never be touched when tenant resolution fails.
	eng.AssertNotCalled(t, "ScheduleJob", mock.Anything, mock.Anything)
}

func TestSchedulerAdapter_CrossTenantJob_Denied(t *testing.T) {
	ctx := crossTenantCtx()
	// Job belongs to tenant-b; requesting tenant-a must be denied.
	foreignJob := &scheduler.Job{ID: 1, TargetTenantID: "tenant-b", Status: scheduler.JobStatusPending}

	t.Run("GetJob", func(t *testing.T) {
		eng := newTestMockSchedulerEngine()
		eng.On("GetJobWithTenant", mock.Anything, uint64(1), "tenant-a").Return(foreignJob, nil)
		a := &SchedulerAdapter{scheduler: eng}
		_, err := a.GetJob(ctx, &GetSchedulerJobRequest{JobID: 1})
		require.Error(t, err)
	})

	t.Run("UpdateJob", func(t *testing.T) {
		eng := newTestMockSchedulerEngine()
		eng.On("UpdateJob", mock.Anything, uint64(1), "tenant-a", mock.Anything).Return(foreignJob, nil)
		a := &SchedulerAdapter{scheduler: eng}
		_, err := a.UpdateJob(ctx, &UpdateSchedulerJobRequest{JobID: 1})
		require.Error(t, err)
	})

	t.Run("TriggerJob", func(t *testing.T) {
		eng := newTestMockSchedulerEngine()
		eng.On("GetJobWithTenant", mock.Anything, uint64(1), "tenant-a").Return(foreignJob, nil)
		a := &SchedulerAdapter{scheduler: eng}
		_, err := a.TriggerJob(ctx, &TriggerSchedulerJobRequest{JobID: 1})
		require.Error(t, err)
	})
}

func TestSchedulerAdapter_EngineErrors(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("engine boom")

	t.Run("ScheduleJob", func(t *testing.T) {
		eng := newTestMockSchedulerEngine()
		eng.On("ScheduleJob", mock.Anything, mock.Anything).Return(boom)
		a := &SchedulerAdapter{scheduler: eng}
		require.Error(t, a.ScheduleJob(ctx, &SchedulerJob{Name: "j"}))
	})

	t.Run("CreateJob", func(t *testing.T) {
		eng := newTestMockSchedulerEngine()
		eng.On("ScheduleJob", mock.Anything, mock.Anything).Return(boom)
		a := &SchedulerAdapter{scheduler: eng}
		_, err := a.CreateJob(ctx, &CreateSchedulerJobRequest{Name: "j"})
		require.Error(t, err)
	})

	t.Run("GetJob", func(t *testing.T) {
		eng := newTestMockSchedulerEngine()
		eng.On("GetJobWithTenant", mock.Anything, uint64(1), "").Return(nil, boom)
		a := &SchedulerAdapter{scheduler: eng}
		_, err := a.GetJob(ctx, &GetSchedulerJobRequest{JobID: 1})
		require.Error(t, err)
	})

	t.Run("UpdateJob", func(t *testing.T) {
		eng := newTestMockSchedulerEngine()
		eng.On("UpdateJob", mock.Anything, uint64(1), "", mock.Anything).Return(nil, boom)
		a := &SchedulerAdapter{scheduler: eng}
		_, err := a.UpdateJob(ctx, &UpdateSchedulerJobRequest{JobID: 1})
		require.Error(t, err)
	})

	t.Run("DeleteJob", func(t *testing.T) {
		eng := newTestMockSchedulerEngine()
		eng.On("DeleteJob", mock.Anything, uint64(1), "", false).Return(boom)
		a := &SchedulerAdapter{scheduler: eng}
		_, err := a.DeleteJob(ctx, &DeleteSchedulerJobRequest{JobID: 1})
		require.Error(t, err)
	})

	t.Run("ListJobs", func(t *testing.T) {
		eng := newTestMockSchedulerEngine()
		eng.On("ListJobsWithCursor", mock.Anything, mock.Anything).Return(nil, "", boom)
		a := &SchedulerAdapter{scheduler: eng}
		_, err := a.ListJobs(ctx, &ListSchedulerJobsRequest{})
		require.Error(t, err)
	})

	t.Run("TriggerJob_GetFails", func(t *testing.T) {
		eng := newTestMockSchedulerEngine()
		eng.On("GetJobWithTenant", mock.Anything, uint64(1), "").Return(nil, boom)
		a := &SchedulerAdapter{scheduler: eng}
		_, err := a.TriggerJob(ctx, &TriggerSchedulerJobRequest{JobID: 1})
		require.Error(t, err)
	})

	t.Run("TriggerJob_TriggerFails", func(t *testing.T) {
		eng := newTestMockSchedulerEngine()
		job := &scheduler.Job{ID: 1, Status: scheduler.JobStatusPending}
		eng.On("GetJobWithTenant", mock.Anything, uint64(1), "").Return(job, nil)
		eng.On("TriggerJob", mock.Anything, uint64(1), "").Return(boom)
		a := &SchedulerAdapter{scheduler: eng}
		_, err := a.TriggerJob(ctx, &TriggerSchedulerJobRequest{JobID: 1})
		require.Error(t, err)
	})

	t.Run("PauseJob", func(t *testing.T) {
		eng := newTestMockSchedulerEngine()
		eng.On("PauseJobWithStatus", mock.Anything, uint64(1), "").Return(scheduler.JobStatusPending, boom)
		a := &SchedulerAdapter{scheduler: eng}
		_, err := a.PauseJob(ctx, &PauseSchedulerJobRequest{JobID: 1})
		require.Error(t, err)
	})

	t.Run("ResumeJob", func(t *testing.T) {
		eng := newTestMockSchedulerEngine()
		eng.On("ResumeJobWithStatus", mock.Anything, uint64(1), "").Return(scheduler.JobStatusPaused, boom)
		a := &SchedulerAdapter{scheduler: eng}
		_, err := a.ResumeJob(ctx, &ResumeSchedulerJobRequest{JobID: 1})
		require.Error(t, err)
	})

	t.Run("CancelJob", func(t *testing.T) {
		eng := newTestMockSchedulerEngine()
		eng.On("CancelJobWithTenant", mock.Anything, uint64(1), "", mock.Anything).Return(scheduler.JobStatusPending, boom)
		a := &SchedulerAdapter{scheduler: eng}
		_, err := a.CancelJob(ctx, &CancelSchedulerJobRequest{JobID: 1})
		require.Error(t, err)
	})

	t.Run("GetJobHistory", func(t *testing.T) {
		eng := newTestMockSchedulerEngine()
		eng.On("GetJobHistory", mock.Anything, uint64(1), "", mock.Anything).Return(nil, boom)
		a := &SchedulerAdapter{scheduler: eng}
		_, err := a.GetJobHistory(ctx, &GetSchedulerJobHistoryRequest{JobID: 1})
		require.Error(t, err)
	})
}

// ScheduleJob / CreateJob cross-tenant request enforcement (target mismatch).
func TestSchedulerAdapter_CrossTenantRequest_Denied(t *testing.T) {
	ctx := crossTenantCtx()
	eng := newTestMockSchedulerEngine()
	a := &SchedulerAdapter{scheduler: eng}

	t.Run("ScheduleJob", func(t *testing.T) {
		err := a.ScheduleJob(ctx, &SchedulerJob{Name: "j", TargetTenantID: "tenant-b"})
		require.Error(t, err)
	})

	t.Run("CreateJob", func(t *testing.T) {
		_, err := a.CreateJob(ctx, &CreateSchedulerJobRequest{Name: "j", TargetTenantID: "tenant-b"})
		require.Error(t, err)
	})
}
