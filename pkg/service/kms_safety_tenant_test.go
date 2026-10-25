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

// seedSafetyTracker records usage for one key and one DEK in each of the
// system scope, tenant t1 and tenant t2, all past a 50% threshold.
func seedSafetyTracker(t *testing.T, svc *kmsServiceImpl) {
	t.Helper()
	config := AEADSafetyConfig{MaxOperations: 10, MaxBytes: 100000, WarningPercent: 50}
	for _, id := range []string{"sys-key", "tenant/t1/t1-key", "tenant/t2/t2-key"} {
		svc.safetyTracker.SetKeyConfig(id, config)
		for i := 0; i < 8; i++ {
			require.NoError(t, svc.safetyTracker.RecordKeyEncrypt(systemTenantCtx(), id, 10))
		}
	}
	for _, path := range []string{"transit/dek/sys", "tenant/t1/transit/dek/t1", "tenant/t2/transit/dek/t2"} {
		svc.safetyTracker.SetDEKConfig(path, config)
		for i := 0; i < 8; i++ {
			require.NoError(t, svc.safetyTracker.RecordDEKEncrypt(systemTenantCtx(), path, 10))
		}
	}
}

func safetyIDs(items []SafetyCheckItem) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func TestKMSService_SafetyReport_TenantScoped(t *testing.T) {
	svc := newKMSService(newMockKMSManager(), newMockLogger())
	seedSafetyTracker(t, svc)

	tests := []struct {
		name string
		ctx  context.Context
		want []string
	}{
		{"tenant t1", tenantctx.WithTenant(context.Background(), "t1"), []string{"t1-key", "transit/dek/t1"}},
		{"tenant t2", tenantctx.WithTenant(context.Background(), "t2"), []string{"t2-key", "transit/dek/t2"}},
		{"system", systemTenantCtx(), []string{"sys-key", "transit/dek/sys"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := svc.SafetyReport(tt.ctx, &SafetyReportRequest{})
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.want, safetyIDs(resp.Items))

			var ops, bytes uint64
			for _, item := range resp.Items {
				ops += item.OperationCount
				bytes += item.BytesProcessed
			}
			assert.Equal(t, ops, resp.TotalOperations, "totals must cover only the caller's entries")
			assert.Equal(t, bytes, resp.TotalBytesProcessed)
			assert.Equal(t, uint64(16), resp.TotalOperations)
		})
	}
}

func TestKMSService_SafetyCheck_TenantScoped(t *testing.T) {
	svc := newKMSService(newMockKMSManager(), newMockLogger())
	seedSafetyTracker(t, svc)

	resp, err := svc.SafetyCheck(tenantctx.WithTenant(context.Background(), "t1"), &SafetyCheckRequest{ThresholdPercent: 50})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"t1-key", "transit/dek/t1"}, safetyIDs(resp.Items))
	assert.Equal(t, 1, resp.TotalKeysChecked)
	assert.Equal(t, 1, resp.TotalDEKsChecked)

	resp, err = svc.SafetyCheck(systemTenantCtx(), &SafetyCheckRequest{ThresholdPercent: 50})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"sys-key", "transit/dek/sys"}, safetyIDs(resp.Items))
}

func TestKMSService_Safety_RefusesUnboundTenant(t *testing.T) {
	svc := newKMSService(newMockKMSManager(), newMockLogger())
	seedSafetyTracker(t, svc)

	_, err := svc.SafetyCheck(context.Background(), &SafetyCheckRequest{})
	require.Error(t, err)
	_, err = svc.SafetyReport(context.Background(), &SafetyReportRequest{})
	require.Error(t, err)
	_, err = svc.SafetyReport(tenantctx.WithTenant(context.Background(), "bad/tenant"), &SafetyReportRequest{})
	require.Error(t, err)
}
