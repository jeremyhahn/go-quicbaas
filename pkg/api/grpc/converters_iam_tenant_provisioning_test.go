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

package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
)

// An absent message means "leave the engine's configured defaults alone",
// which is a different instruction than an all-zero struct ("use the system
// default for every limit"). The nil case is therefore load-bearing.
func TestEngineQuotasFromProto_NilKeepsDefaults(t *testing.T) {
	assert.Nil(t, engineQuotasFromProto(nil))
	assert.Nil(t, engineEntitlementsFromProto(nil))
}

func TestEngineQuotasFromProto(t *testing.T) {
	got := engineQuotasFromProto(&iampb.TenantEngineQuotas{
		MaxDatabases:          4,
		MaxRaftGroups:         2,
		MaxReplicasPerGroup:   3,
		MaxTotalReplicas:      9,
		MaxCollections:        5,
		MaxKeys:               6,
		MaxStorageBytes:       1024,
		MaxDatabaseSizeBytes:  512,
		MaxUsers:              7,
		MaxRoles:              8,
		MaxCertificates:       10,
		MaxBackups:            11,
		MaxVectorDims:         1536,
		MaxTsMetrics:          12,
		MaxDedicatedWalGroups: 13,
		SystemBarrierQuota:    -1,
	})

	require.NotNil(t, got)
	assert.Equal(t, int64(4), got.MaxDatabases)
	assert.Equal(t, int64(2), got.MaxRaftGroups)
	assert.Equal(t, int64(3), got.MaxReplicasPerGroup)
	assert.Equal(t, int64(9), got.MaxTotalReplicas)
	assert.Equal(t, int64(5), got.MaxCollections)
	assert.Equal(t, int64(6), got.MaxKeys)
	assert.Equal(t, int64(1024), got.MaxStorageBytes)
	assert.Equal(t, int64(512), got.MaxDatabaseSizeBytes)
	assert.Equal(t, int64(7), got.MaxUsers)
	assert.Equal(t, int64(8), got.MaxRoles)
	assert.Equal(t, int64(10), got.MaxCertificates)
	assert.Equal(t, int64(11), got.MaxBackups)
	assert.Equal(t, int64(1536), got.MaxVectorDims)
	// The two fields whose proto and Go spellings differ are where a
	// hand-written mapping goes wrong silently.
	assert.Equal(t, int64(12), got.MaxTSMetrics)
	assert.Equal(t, int64(13), got.MaxDedicatedWALGroups)
	assert.Equal(t, int64(-1), got.SystemBarrierQuota)

	// Usage counters are engine-owned outputs and must not be settable at
	// creation, so they stay at zero.
	assert.Zero(t, got.CurrentDatabases)
	assert.Zero(t, got.CurrentStorageBytes)
}

func TestEngineEntitlementsFromProto(t *testing.T) {
	got := engineEntitlementsFromProto(&iampb.TenantEngineEntitlements{
		AllowedIsolationModes:     []string{"prefix", "barrier"},
		AllowDedicatedRaft:        true,
		AllowVolumeStorage:        true,
		AllowSystemManagedBarrier: true,
		AllowDedicatedWal:         true,
		AllowedBarrierModes:       []string{"shamir"},
	})

	require.NotNil(t, got)
	assert.Equal(t, []string{"prefix", "barrier"}, got.AllowedIsolationModes)
	assert.True(t, got.AllowDedicatedRaft)
	assert.True(t, got.AllowVolumeStorage)
	assert.True(t, got.AllowSystemManagedBarrier)
	assert.True(t, got.AllowDedicatedWAL)
	assert.Equal(t, []string{"shamir"}, got.AllowedBarrierModes)
}

// An empty message is a real instruction (deny everything / default every
// limit) and must not be confused with an absent one.
func TestEngineProvisioningFromProto_EmptyMessageIsNotNil(t *testing.T) {
	quotas := engineQuotasFromProto(&iampb.TenantEngineQuotas{})
	require.NotNil(t, quotas)
	assert.Zero(t, quotas.MaxDatabases)

	entitlements := engineEntitlementsFromProto(&iampb.TenantEngineEntitlements{})
	require.NotNil(t, entitlements)
	assert.False(t, entitlements.AllowDedicatedRaft)
	assert.Empty(t, entitlements.AllowedIsolationModes)
}
