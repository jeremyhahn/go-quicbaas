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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	schedulerhandlers "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/handlers"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// reservedSchedulerHandlers are the handlers no scheduler API caller may
// name: every platform handler plus the service-internal kms.grace_period.
func reservedSchedulerHandlers() []string {
	return append(scheduler.PlatformHandlers(), schedulerhandlers.HandlerKMSGracePeriod)
}

func requireSchedulerForbidden(t *testing.T, err error, msg string) {
	t.Helper()
	require.Error(t, err, msg)
	var qb *errors.QBError
	require.ErrorAs(t, err, &qb, msg)
	assert.Equal(t, errors.ErrForbidden, qb.Code, msg)
}

func requireNoJobOfHandler(t *testing.T, a *SchedulerAdapter, ctx context.Context, handler string) {
	t.Helper()
	resp, err := a.ListJobs(ctx, &ListSchedulerJobsRequest{})
	require.NoError(t, err)
	for _, job := range resp.Jobs {
		assert.NotEqual(t, handler, job.Handler, "no job of %s is stored", handler)
	}
}

// TestSchedulerAdapter_TenantCannotCreateReservedHandlerJob: a tenant cannot
// create a job of a platform handler (which acts across every tenant) or of
// the service-internal grace handler, and nothing is stored.
func TestSchedulerAdapter_TenantCannotCreateReservedHandlerJob(t *testing.T) {
	for _, handler := range reservedSchedulerHandlers() {
		t.Run(handler, func(t *testing.T) {
			a := realStorageSchedulerAdapter(t)
			ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

			_, err := a.CreateJob(ctx, &CreateSchedulerJobRequest{
				Name:      "forged-" + handler,
				Type:      SchedulerJobType(scheduler.JobTypeOneShot),
				ExecuteAt: time.Now().Add(time.Hour),
				Handler:   handler,
			})
			requireSchedulerForbidden(t, err, "a tenant job of a reserved handler is refused")
			requireNoJobOfHandler(t, a, ctx, handler)
		})
	}
}

// TestSchedulerAdapter_SystemCannotCreateReservedHandlerJob: the refusal
// holds for the system tenant too; platform jobs are installed only by their
// owning service's internal registration.
func TestSchedulerAdapter_SystemCannotCreateReservedHandlerJob(t *testing.T) {
	for _, handler := range reservedSchedulerHandlers() {
		t.Run(handler, func(t *testing.T) {
			a := realStorageSchedulerAdapter(t)
			ctx := tenantctx.WithSystem(context.Background())

			_, err := a.CreateJob(ctx, &CreateSchedulerJobRequest{
				Name:      "forged-" + handler,
				Type:      SchedulerJobType(scheduler.JobTypeOneShot),
				ExecuteAt: time.Now().Add(time.Hour),
				Handler:   handler,
			})
			requireSchedulerForbidden(t, err, "a system API job of a reserved handler is refused")
			requireNoJobOfHandler(t, a, ctx, handler)
		})
	}
}

// TestSchedulerAdapter_TenantCannotUpdateJobToReservedHandler: a tenant's
// noop job cannot be turned into a job of a reserved handler; its handler is
// unchanged.
func TestSchedulerAdapter_TenantCannotUpdateJobToReservedHandler(t *testing.T) {
	for _, handler := range reservedSchedulerHandlers() {
		t.Run(handler, func(t *testing.T) {
			a := realStorageSchedulerAdapter(t)
			ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

			created, err := a.CreateJob(ctx, &CreateSchedulerJobRequest{
				Name:      "noop-job",
				Type:      SchedulerJobType(scheduler.JobTypeOneShot),
				ExecuteAt: time.Now().Add(time.Hour),
				Handler:   "noop",
			})
			require.NoError(t, err)

			_, err = a.UpdateJob(ctx, &UpdateSchedulerJobRequest{JobID: created.Job.ID, Handler: handler})
			requireSchedulerForbidden(t, err, "turning a tenant job into a reserved handler's job is refused")

			got, err := a.GetJob(ctx, &GetSchedulerJobRequest{JobID: created.Job.ID})
			require.NoError(t, err)
			assert.Equal(t, "noop", got.Job.Handler, "the handler is unchanged")
			requireNoJobOfHandler(t, a, ctx, handler)
		})
	}
}

// TestSchedulerPlatformHandlers_PinsHandlerNames pins the scheduler's
// platform-handler list to the handler packages' own names, so a renamed
// handler cannot silently drop out of the reservation.
func TestSchedulerPlatformHandlers_PinsHandlerNames(t *testing.T) {
	want := []string{
		schedulerhandlers.HandlerKMSRotation,
		schedulerhandlers.HandlerLedgerPricing,
		sdlc.RotationHandlerName,
		schedulerhandlers.HandlerAuditCleanup,
		schedulerhandlers.HandlerDBCompaction,
	}
	got := scheduler.PlatformHandlers()
	assert.ElementsMatch(t, want, got)
	for _, name := range want {
		assert.True(t, scheduler.IsPlatformHandler(name), name)
	}
	assert.False(t, scheduler.IsPlatformHandler(schedulerhandlers.HandlerKMSGracePeriod),
		"the grace handler runs bound to its job's tenant; it is service-internal, not platform")
	assert.False(t, scheduler.IsPlatformHandler("noop"))
}
