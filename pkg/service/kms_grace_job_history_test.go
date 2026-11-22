package service

import (
	"context"
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

// After a tenant's grace job runs, its history is readable by that tenant,
// and only by that tenant.
func TestGetJobHistory_TenantGraceJobAfterExecution(t *testing.T) {
	engine := scheduler.New(rotationSchedulerConfig(), schedulerstorage.NewStorage(kvstore.NewMemory()))

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

	// Registered as production registers it.
	graceHandler := schedulerhandlers.NewKMSGracePeriodHandler(&kmsGracePeriodAdapter{kms: svc})
	require.NoError(t, engine.RegisterHandler(graceHandler.Name(), graceHandler.Handler()))
	require.NoError(t, engine.Start(context.Background()))
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	adapter := NewSchedulerAdapter(engine)
	svc.SetScheduler(adapter)

	ctxA := tenantctx.WithTenant(context.Background(), "tenant-a")
	resp, err := svc.RotateKey(ctxA, &RotateKeyRequest{KeyID: "grace-key", GracePeriodSeconds: 1})
	require.NoError(t, err)
	require.NotNil(t, resp)
	jobID, err := strconv.ParseUint(resp.GracePeriodJobID, 10, 64)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		hist, err := adapter.GetJobHistory(ctxA, &GetSchedulerJobHistoryRequest{JobID: jobID, Limit: 10})
		if err != nil {
			return false
		}
		for _, exec := range hist.Executions {
			if exec.Status == SchedulerJobStatusCompleted {
				return true
			}
		}
		return false
	}, 15*time.Second, 50*time.Millisecond, "tenant-a reads a completed execution of its grace job")

	status, _ := svc.getVersionStatus(ctxA, scoped, resp.OldVersion)
	assert.Equal(t, KeyStatusDisabled, status, "the old version is disabled")

	ctxB := tenantctx.WithTenant(context.Background(), "tenant-b")
	other, err := adapter.GetJobHistory(ctxB, &GetSchedulerJobHistoryRequest{JobID: jobID, Limit: 10})
	if err == nil {
		assert.Empty(t, other.Executions, "another tenant reads no execution of the job")
	}
}
