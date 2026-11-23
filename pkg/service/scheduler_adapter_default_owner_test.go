package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	schedulerstorage "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/storage/quicbaas"
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

// TestSchedulerAdapter_CreateJob_TenantCallerSystemOwnerRefused proves an
// explicit system owner from a tenant caller is still refused.
func TestSchedulerAdapter_CreateJob_TenantCallerSystemOwnerRefused(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")
	a := &SchedulerAdapter{scheduler: newTestMockSchedulerEngine()}

	_, err := a.CreateJob(ctx, &CreateSchedulerJobRequest{Name: "j", OwnerType: SchedulerOwnerType(scheduler.OwnerTypeSystem)})
	require.Error(t, err)
}

// TestSchedulerAdapter_TenantCallerDefaultOwner_CreateThenList: a tenant
// caller's default-owner job is listed by its default listing, against the
// real scheduler storage, and is not listed for another tenant.
func TestSchedulerAdapter_TenantCallerDefaultOwner_CreateThenList(t *testing.T) {
	engine := scheduler.New(rotationSchedulerConfig(), schedulerstorage.NewStorage(kvstore.NewMemory()))
	require.NoError(t, engine.Start(context.Background()))
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	a := NewSchedulerAdapter(engine)

	ctxA := tenantctx.WithTenant(context.Background(), "tenant-a")
	created, err := a.CreateJob(ctxA, &CreateSchedulerJobRequest{
		Name:      "j",
		Type:      SchedulerJobType(scheduler.JobTypeOneShot),
		Handler:   "noop",
		ExecuteAt: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	require.NotNil(t, created)
	require.NotNil(t, created.Job)
	id := created.Job.ID

	containsID := func(resp *ListSchedulerJobsResponse) bool {
		for _, j := range resp.Jobs {
			if j.ID == id {
				return true
			}
		}
		return false
	}

	listA, err := a.ListJobs(ctxA, &ListSchedulerJobsRequest{})
	require.NoError(t, err)
	require.True(t, containsID(listA), "tenant-a's default listing omits its default-owner job %d", id)

	ctxB := tenantctx.WithTenant(context.Background(), "tenant-b")
	listB, err := a.ListJobs(ctxB, &ListSchedulerJobsRequest{})
	require.NoError(t, err)
	require.False(t, containsID(listB), "tenant-b's listing shows tenant-a's job %d", id)
}
