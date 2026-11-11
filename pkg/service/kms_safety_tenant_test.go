// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// recordSafetyUsageForTwoTenants records one key and one DEK operation in
// the shared safety tracker for each of tenant-a and tenant-b, under the
// tenant-scoped IDs the crypto service records them with.
func recordSafetyUsageForTwoTenants(t *testing.T, svc *kmsServiceImpl) {
	t.Helper()
	ctx := context.Background()
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		require.NoError(t, svc.safetyTracker.RecordKeyEncrypt(ctx, tenantScopedKeyID(tenant, "shared-key"), 100))
		require.NoError(t, svc.safetyTracker.RecordDEKEncrypt(ctx, tenantScopedKeyID(tenant, "/shared-dek"), 50))
	}
}

func TestKMSService_SafetyReport_ShowsOnlyTheCallersTenant(t *testing.T) {
	svc := newKMSService(newMockKMSManager(), newMockLogger())
	recordSafetyUsageForTwoTenants(t, svc)

	resp, err := svc.SafetyReport(tenantctx.WithTenant(context.Background(), "tenant-b"), &SafetyReportRequest{})
	require.NoError(t, err)

	require.Len(t, resp.Items, 2)
	for _, item := range resp.Items {
		assert.NotContains(t, item.ID, "tenant-a", "tenant-b's report must not show tenant-a's %s", item.Type)
		assert.NotContains(t, item.ID, "tenant/", "IDs are reported in the caller's own namespace")
	}
	assert.Equal(t, uint64(2), resp.TotalOperations)
	assert.Equal(t, uint64(150), resp.TotalBytesProcessed)
}

func TestKMSService_SafetyCheck_ShowsOnlyTheCallersTenant(t *testing.T) {
	svc := newKMSService(newMockKMSManager(), newMockLogger())
	recordSafetyUsageForTwoTenants(t, svc)
	// Put every tenant's key at its limit so the check reports it.
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		svc.safetyTracker.SetKeyConfig(tenantScopedKeyID(tenant, "shared-key"), AEADSafetyConfig{MaxOperations: 1, MaxBytes: 1 << 20, WarningPercent: 50})
	}

	resp, err := svc.SafetyCheck(tenantctx.WithTenant(context.Background(), "tenant-b"), &SafetyCheckRequest{ThresholdPercent: 50})
	require.NoError(t, err)

	require.Len(t, resp.Items, 1)
	assert.Equal(t, "shared-key", resp.Items[0].ID)
	assert.Equal(t, 1, resp.TotalKeysChecked)
}

func TestKMSService_SafetyReport_SystemCallerSeesEveryTenant(t *testing.T) {
	svc := newKMSService(newMockKMSManager(), newMockLogger())
	recordSafetyUsageForTwoTenants(t, svc)

	resp, err := svc.SafetyReport(systemTenantCtx(), &SafetyReportRequest{})
	require.NoError(t, err)

	assert.Len(t, resp.Items, 4)
	assert.Equal(t, uint64(4), resp.TotalOperations)
}

func TestKMSService_SafetyReport_RefusesACallerWithoutATenant(t *testing.T) {
	svc := newKMSService(newMockKMSManager(), newMockLogger())
	recordSafetyUsageForTwoTenants(t, svc)

	_, err := svc.SafetyReport(context.Background(), &SafetyReportRequest{})
	require.Error(t, err)

	_, err = svc.SafetyCheck(context.Background(), &SafetyCheckRequest{})
	require.Error(t, err)
}
