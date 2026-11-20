package service

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	schedulerhandlers "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/handlers"
	schedulerstorage "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/storage/quicbaas"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// newGraceEditFixture starts a real engine, rotates tenant-a's key with a
// grace period through the KMS service, and returns the adapter and the
// grace job's ID.
func newGraceEditFixture(t *testing.T) (*SchedulerAdapter, uint64) {
	t.Helper()
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

	resp, err := svc.RotateKey(tenantctx.WithTenant(context.Background(), "tenant-a"),
		&RotateKeyRequest{KeyID: "grace-key", GracePeriodSeconds: 3600})
	require.NoError(t, err)
	require.NotNil(t, resp)
	jobID, err := strconv.ParseUint(resp.GracePeriodJobID, 10, 64)
	require.NoError(t, err)
	return adapter, jobID
}

// A tenant cannot repoint, re-target or reschedule the grace job the KMS
// service filed in its namespace; other fields stay editable.
func TestUpdateJob_TenantCannotEditGraceJob(t *testing.T) {
	adapter, jobID := newGraceEditFixture(t)
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

	before, err := adapter.GetJob(ctx, &GetSchedulerJobRequest{JobID: jobID})
	require.NoError(t, err)
	require.Equal(t, schedulerhandlers.HandlerKMSGracePeriod, before.Job.Handler)

	scoped := tenantScopedKeyID("tenant-a", "grace-key")
	edits := map[string]*UpdateSchedulerJobRequest{
		"handler":    {JobID: jobID, Handler: schedulerhandlers.HandlerKMSRotation},
		"payload":    {JobID: jobID, Payload: []byte(fmt.Sprintf(`{"key_id":%q,"version":2}`, scoped))},
		"execute_at": {JobID: jobID, ExecuteAt: time.Now().Add(time.Second)},
	}
	for name, req := range edits {
		_, err := adapter.UpdateJob(ctx, req)
		assert.Error(t, err, "a tenant edit of the grace job's %s is refused", name)
	}

	after, err := adapter.GetJob(ctx, &GetSchedulerJobRequest{JobID: jobID})
	require.NoError(t, err)
	assert.Equal(t, schedulerhandlers.HandlerKMSGracePeriod, after.Job.Handler)
	assert.Equal(t, string(before.Job.Payload), string(after.Job.Payload))
	assert.True(t, before.Job.ExecuteAt.Equal(after.Job.ExecuteAt),
		"execute_at unchanged: before %v after %v", before.Job.ExecuteAt, after.Job.ExecuteAt)

	renamed, err := adapter.UpdateJob(ctx, &UpdateSchedulerJobRequest{JobID: jobID, Name: "renamed-grace"})
	require.NoError(t, err, "the name stays editable")
	assert.Equal(t, "renamed-grace", renamed.Job.Name)
}

// A tenant cannot create a job that names a system handler.
func TestCreateJob_TenantCannotNameSystemHandler(t *testing.T) {
	engine := scheduler.New(rotationSchedulerConfig(), schedulerstorage.NewStorage(kvstore.NewMemory()))
	require.NoError(t, engine.Start(context.Background()))
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	adapter := NewSchedulerAdapter(engine)
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

	systemHandlers := []string{
		schedulerhandlers.HandlerKMSRotation,
		schedulerhandlers.HandlerKMSGracePeriod,
		schedulerhandlers.HandlerAuditCleanup,
		schedulerhandlers.HandlerDBCompaction,
		schedulerhandlers.HandlerLedgerPricing,
	}
	for _, handler := range systemHandlers {
		_, err := adapter.CreateJob(ctx, &CreateSchedulerJobRequest{
			Name:      "tenant-" + handler,
			OwnerType: SchedulerOwnerTypeTenant,
			OwnerID:   "tenant-a",
			Type:      SchedulerJobTypeOneShot,
			ExecuteAt: time.Now().Add(time.Hour),
			Handler:   handler,
		})
		assert.Error(t, err, "a tenant job naming %s is refused", handler)
	}

	list, err := adapter.ListJobs(ctx, &ListSchedulerJobsRequest{})
	require.NoError(t, err)
	for _, job := range list.Jobs {
		assert.NotContains(t, systemHandlers, job.Handler, "job %d names a system handler", job.ID)
	}
}
