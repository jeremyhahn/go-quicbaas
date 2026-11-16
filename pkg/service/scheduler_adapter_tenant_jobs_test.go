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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	schedulerhandlers "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/handlers"
	schedulerstorage "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/storage/quicbaas"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// realStorageSchedulerAdapter is the adapter over a real engine and the
// quicbaas scheduler storage, so namespace selection is exercised.
func realStorageSchedulerAdapter(t *testing.T) *SchedulerAdapter {
	t.Helper()
	engine := scheduler.New(rotationSchedulerConfig(), schedulerstorage.NewStorage(kvstore.NewMemory()))
	require.NoError(t, engine.Start(context.Background()))
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	return NewSchedulerAdapter(engine)
}

// A tenant's job, created with no owner type, is filed in the tenant's own
// namespace; listing with no owner_id must read that namespace.
func TestSchedulerAdapter_TenantCreateThenListThroughStorage(t *testing.T) {
	a := realStorageSchedulerAdapter(t)
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

	created, err := a.CreateJob(ctx, &CreateSchedulerJobRequest{
		Name:      "nightly",
		Type:      SchedulerJobType(scheduler.JobTypeOneShot),
		ExecuteAt: time.Now().Add(time.Hour),
		Handler:   "noop",
	})
	require.NoError(t, err)

	listed, err := a.ListJobs(ctx, &ListSchedulerJobsRequest{})
	require.NoError(t, err)
	var ids []uint64
	for _, j := range listed.Jobs {
		ids = append(ids, j.ID)
	}
	require.Contains(t, ids, created.Job.ID, "the tenant lists the job it created")

	other, err := a.ListJobs(tenantctx.WithTenant(context.Background(), "tenant-b"), &ListSchedulerJobsRequest{})
	require.NoError(t, err)
	require.Empty(t, other.Jobs, "another tenant lists none of tenant-a's jobs")
}

// A tenant's KMS grace job is the tenant's own job, but its handler and
// payload are written only by the KMS service: the tenant cannot point it
// at another tenant's key.
func TestSchedulerAdapter_TenantCannotEditGraceJobPayload(t *testing.T) {
	a := realStorageSchedulerAdapter(t)
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

	original := []byte(fmt.Sprintf(`{"key_id":%q,"version":1}`, tenantScopedKeyID("tenant-a", "k")))
	job := &SchedulerJob{
		Name:           "kms-grace",
		OwnerType:      SchedulerOwnerTypeTenant,
		OwnerID:        "tenant-a",
		TargetTenantID: "tenant-a",
		Type:           SchedulerJobTypeOneShot,
		ExecuteAt:      time.Now().Add(time.Hour),
		Handler:        "kms.grace_period",
		Payload:        original,
		Status:         SchedulerJobStatusPending,
	}
	require.NoError(t, a.ScheduleJob(ctx, job))

	_, err := a.UpdateJob(ctx, &UpdateSchedulerJobRequest{
		JobID:   job.ID,
		Payload: []byte(`{"key_id":"tenant/other/key","version":1}`),
	})
	require.Error(t, err, "the grace job's payload is not tenant-editable")

	_, err = a.UpdateJob(ctx, &UpdateSchedulerJobRequest{JobID: job.ID, Handler: "noop"})
	require.Error(t, err, "the grace job's handler is not tenant-editable")

	got, err := a.GetJob(ctx, &GetSchedulerJobRequest{JobID: job.ID})
	require.NoError(t, err)
	require.JSONEq(t, string(original), string(got.Job.Payload), "the payload is unchanged")

	_, err = a.CreateJob(ctx, &CreateSchedulerJobRequest{
		Name:      "forged",
		Type:      SchedulerJobType(scheduler.JobTypeOneShot),
		ExecuteAt: time.Now().Add(time.Hour),
		Handler:   "kms.grace_period",
		Payload:   []byte(`{"key_id":"tenant/other/key","version":1}`),
	})
	require.Error(t, err, "no caller creates a job of an internal handler")
}

// A tenant caller cannot create, or turn a job into, a job of a platform
// sweep's handler: those run with platform authority across every tenant.
func TestSchedulerAdapter_TenantCannotUsePlatformSweepHandlers(t *testing.T) {
	a := realStorageSchedulerAdapter(t)
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

	for _, handler := range []string{
		schedulerhandlers.HandlerKMSRotation,
		schedulerhandlers.HandlerLedgerPricing,
	} {
		_, err := a.CreateJob(ctx, &CreateSchedulerJobRequest{
			Name:      "forged-" + handler,
			Type:      SchedulerJobType(scheduler.JobTypeOneShot),
			ExecuteAt: time.Now().Add(time.Hour),
			Handler:   handler,
		})
		require.Error(t, err, "no tenant creates a %s job", handler)
		require.Contains(t, err.Error(), "written only by the service that owns it")
	}

	created, err := a.CreateJob(ctx, &CreateSchedulerJobRequest{
		Name:      "report",
		Type:      SchedulerJobType(scheduler.JobTypeOneShot),
		ExecuteAt: time.Now().Add(time.Hour),
		Handler:   "noop",
	})
	require.NoError(t, err)

	_, err = a.UpdateJob(ctx, &UpdateSchedulerJobRequest{
		JobID:   created.Job.ID,
		Handler: schedulerhandlers.HandlerKMSRotation,
	})
	require.Error(t, err, "no tenant turns a job into a kms.rotation job")

	got, err := a.GetJob(ctx, &GetSchedulerJobRequest{JobID: created.Job.ID})
	require.NoError(t, err)
	require.Equal(t, "noop", got.Job.Handler, "the job's handler is unchanged")
}

// The grace handler binds a tenant job's key to the tenant by the same
// prefix the KMS service scopes the tenant's keys under.
func TestGraceHandlerTenantKeyPrefix_MatchesServiceScope(t *testing.T) {
	require.Equal(t, tenantScopedKeyID("tenant-a", ""), schedulerhandlers.TenantKeyPrefix("tenant-a"))
}
