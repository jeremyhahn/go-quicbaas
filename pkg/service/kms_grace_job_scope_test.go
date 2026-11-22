package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	schedulerhandlers "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/handlers"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// A tenant can edit the payload of its own grace-period job. An edited job
// that names another tenant's key must fail and leave that key's version
// enabled; the tenant's own key is still disabled.
func TestGracePeriodJob_TenantJobCannotDisableAnotherTenantsKey(t *testing.T) {
	kmsImpl := newKMSService(newMockKMSManager(), newMockLogger())
	h := schedulerhandlers.NewKMSGracePeriodHandler(&kmsGracePeriodAdapter{kms: kmsImpl, jobID: "1"})
	ctx := tenantctx.WithSystem(context.Background())

	tenantJob := func(t *testing.T, keyID string) *scheduler.Job {
		t.Helper()
		payload, err := json.Marshal(map[string]any{"key_id": keyID, "version": 1})
		require.NoError(t, err)
		return &scheduler.Job{
			ID:             1,
			OwnerType:      scheduler.OwnerTypeTenant,
			OwnerID:        "tenant-a",
			TargetTenantID: "tenant-a",
			Handler:        schedulerhandlers.HandlerKMSGracePeriod,
			Payload:        payload,
		}
	}

	victim := tenantScopedKeyID("tenant-b", "victim-key")
	_, err := h.Handle(ctx, tenantJob(t, victim))
	require.Error(t, err, "a tenant job naming another tenant's key fails")
	status, _ := kmsImpl.getVersionStatus(ctx, victim, 1)
	assert.NotEqual(t, KeyStatusDisabled, status, "the victim key's version is untouched")

	own := tenantScopedKeyID("tenant-a", "own-key")
	_, err = h.Handle(ctx, tenantJob(t, own))
	require.NoError(t, err)
	status, _ = kmsImpl.getVersionStatus(ctx, own, 1)
	assert.Equal(t, KeyStatusDisabled, status, "the owner's key version is disabled")
}
