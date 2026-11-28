package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	schedulerstorage "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/storage/quicbaas"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// The store picks a listing's namespace by owner, so a tenant's listing with
// no owner filter must still name the caller's tenant as owner.
func TestListJobs_NoOwnerFilterReadsCallerTenant(t *testing.T) {
	var recorded *scheduler.JobQuery
	called := false
	engine := &covMockSchedulerEngine{
		listJobsFn: func(_ context.Context, query *scheduler.JobQuery) ([]*scheduler.Job, string, error) {
			called = true
			recorded = query
			return nil, "", nil
		},
	}
	adapter := NewSchedulerAdapter(engine)
	ctx := context.Background()

	_, err := adapter.ListJobs(tenantctx.WithTenant(ctx, "acme"), &ListSchedulerJobsRequest{})
	require.NoError(t, err)
	require.NotNil(t, recorded)
	assert.Equal(t, "acme", recorded.OwnerID, "the caller's tenant namespace is read")

	recorded, called = nil, false
	_, err = adapter.ListJobs(systemTenantCtx(), &ListSchedulerJobsRequest{})
	require.NoError(t, err)
	require.NotNil(t, recorded)
	assert.Equal(t, "", recorded.OwnerID, "the system tenant reads the system namespace")

	recorded, called = nil, false
	_, err = adapter.ListJobs(tenantctx.WithTenant(ctx, "acme"), &ListSchedulerJobsRequest{OwnerID: "other"})
	require.Error(t, err, "naming another tenant as owner is refused")
	assert.False(t, called, "the store is not read")
}

// A tenant's key-rotation grace job is listed for that tenant, without an
// owner filter, and not for another tenant.
func TestListJobs_RotatedKeyGraceJobListedForItsTenant(t *testing.T) {
	engine := scheduler.New(rotationSchedulerConfig(), schedulerstorage.NewStorage(kvstore.NewMemory()))
	require.NoError(t, engine.Start(context.Background()))
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	adapter := NewSchedulerAdapter(engine)

	mockManager := newMockKMSManager()
	scoped := tenantScopedKeyID("tenant-a", "grace-key")
	mockManager.keys[scoped] = &mockKeyData{
		keyID:     scoped,
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	svc := newKMSService(mockManager, newMockLogger())
	svc.SetScheduler(adapter)

	ctx := context.Background()
	resp, err := svc.RotateKey(tenantctx.WithTenant(ctx, "tenant-a"), &RotateKeyRequest{KeyID: "grace-key", GracePeriodSeconds: 3600})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.GracePeriodJobID)

	hasGraceJob := func(jobs []*SchedulerJob) bool {
		for _, job := range jobs {
			if job.Handler == "kms.grace_period" && fmt.Sprintf("%d", job.ID) == resp.GracePeriodJobID {
				return true
			}
		}
		return false
	}

	own, err := adapter.ListJobs(tenantctx.WithTenant(ctx, "tenant-a"), &ListSchedulerJobsRequest{})
	require.NoError(t, err)
	assert.True(t, hasGraceJob(own.Jobs), "the tenant's grace job is in its listing")

	other, err := adapter.ListJobs(tenantctx.WithTenant(ctx, "tenant-b"), &ListSchedulerJobsRequest{})
	require.NoError(t, err)
	assert.False(t, hasGraceJob(other.Jobs), "another tenant does not see the job")
}
