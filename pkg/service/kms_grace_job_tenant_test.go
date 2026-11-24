package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// A tenant that rotates a key with a grace period must be able to read the
// grace-period job the rotation returns. The scheduler files a job by its
// owner: a delegated job goes to the system namespace, a tenant-owned job to
// its tenant's. GetJob as tenant T reads T's namespace, so a grace job filed
// as delegated answered 404 to the tenant that created it.
func TestKMSService_RotateKey_GraceJobOwnedByCallerTenant(t *testing.T) {
	const tenant = "tenant-a"
	mockManager := newMockKMSManager()
	scoped := tenantScopedKeyID(tenant, "grace-key")
	mockManager.keys[scoped] = &mockKeyData{
		keyID:     scoped,
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	svc := newKMSService(mockManager, newMockLogger())
	sched := &rotateKeyTestScheduler{}
	svc.SetScheduler(sched)

	ctx := tenantctx.WithTenant(context.Background(), tenant)
	resp, err := svc.RotateKey(ctx, &RotateKeyRequest{KeyID: "grace-key", GracePeriodSeconds: 3600})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, sched.lastJob)

	job := sched.lastJob
	assert.Equal(t, SchedulerOwnerTypeTenant, job.OwnerType, "the job is filed in the caller's namespace")
	assert.Equal(t, tenant, job.OwnerID)
	assert.Equal(t, tenant, job.TargetTenantID)

	// The engine files and reads it under the caller's tenant.
	assert.Equal(t, tenant, scheduler.JobTenantID(&scheduler.Job{OwnerType: scheduler.OwnerType(job.OwnerType), OwnerID: job.OwnerID}))

	// Another tenant is still refused.
	require.Error(t, schedulerTenantEnforceAccess("tenant-b", job.TargetTenantID, "GetJob"))
	require.NoError(t, schedulerTenantEnforceAccess(tenant, job.TargetTenantID, "GetJob"))
}

// The system tenant's grace job stays a delegated job in the system namespace.
func TestKMSService_RotateKey_GraceJobSystemTenantDelegated(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.keys["sys-grace-key"] = &mockKeyData{
		keyID:     "sys-grace-key",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	svc := newKMSService(mockManager, newMockLogger())
	sched := &rotateKeyTestScheduler{}
	svc.SetScheduler(sched)

	_, err := svc.RotateKey(systemTenantCtx(), &RotateKeyRequest{KeyID: "sys-grace-key", GracePeriodSeconds: 3600})
	require.NoError(t, err)
	require.NotNil(t, sched.lastJob)
	assert.Equal(t, SchedulerOwnerTypeDelegated, sched.lastJob.OwnerType)
	assert.Empty(t, sched.lastJob.OwnerID)
}
