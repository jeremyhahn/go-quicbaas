package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestSchedulerAdapter_CreateJob_TenantCallerNoOwnerType proves a tenant
// caller that names no owner type creates a job its own tenant owns, rather
// than being refused for naming the system namespace (the REST job body
// carries no owner type).
func TestSchedulerAdapter_CreateJob_TenantCallerNoOwnerType(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")
	eng := newTestMockSchedulerEngine()
	var got *scheduler.Job
	eng.On("ScheduleJob", mock.Anything, mock.MatchedBy(func(job *scheduler.Job) bool {
		got = job
		return true
	})).Return(nil)
	a := &SchedulerAdapter{scheduler: eng}

	_, err := a.CreateJob(ctx, &CreateSchedulerJobRequest{Name: "j", Type: SchedulerJobType(scheduler.JobTypeOneShot)})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, scheduler.OwnerTypeTenant, got.OwnerType)
	require.Equal(t, "tenant-a", got.OwnerID)
	require.Equal(t, "tenant-a", got.TargetTenantID)
}

// A system caller that names no owner type creates a
// system-owned job, as before the tenant default was added.
func TestSchedulerAdapter_CreateJob_SystemCallerNoOwnerType(t *testing.T) {
	eng := newTestMockSchedulerEngine()
	var got *scheduler.Job
	eng.On("ScheduleJob", mock.Anything, mock.MatchedBy(func(job *scheduler.Job) bool {
		got = job
		return true
	})).Return(nil)
	a := &SchedulerAdapter{scheduler: eng}

	ctx := tenantctx.WithTenant(context.Background(), tenantctx.SystemTenantID)
	_, err := a.CreateJob(ctx, &CreateSchedulerJobRequest{Name: "j", Type: SchedulerJobType(scheduler.JobTypeOneShot)})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, scheduler.OwnerTypeSystem, got.OwnerType)
	require.Empty(t, got.TargetTenantID)
}

// TestSchedulerAdapter_CreateJob_TenantCallerSystemOwnerRefused proves an
// explicit system owner from a tenant caller is still refused.
func TestSchedulerAdapter_CreateJob_TenantCallerSystemOwnerRefused(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")
	a := &SchedulerAdapter{scheduler: newTestMockSchedulerEngine()}

	_, err := a.CreateJob(ctx, &CreateSchedulerJobRequest{Name: "j", OwnerType: SchedulerOwnerType(scheduler.OwnerTypeSystem)})
	require.Error(t, err)
}
