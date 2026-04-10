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

//go:build grpc

package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
)

// =============================================================================
// Replication Policy Conversion Tests (with non-nil inputs)
// =============================================================================

func TestReplicationPolicyToProto_WithValues(t *testing.T) {
	policy := &catalog.ReplicationPolicy{
		Enabled:      true,
		MinReplicas:  3,
		TargetStores: []string{"s3", "gcs"},
		Mode:         catalog.ReplicationModeSync,
	}

	result := replicationPolicyToProto(policy)

	assert.NotNil(t, result)
	assert.True(t, result.Enabled)
	assert.Equal(t, int32(3), result.MinReplicas)
	assert.Equal(t, []string{"s3", "gcs"}, result.TargetStores)
	assert.Equal(t, pb.ReplicationMode_REPLICATION_MODE_SYNC, result.Mode)
}

// =============================================================================
// Archive Policy Conversion Tests (with non-nil inputs)
// =============================================================================

func TestArchivePolicyToProto_WithValues(t *testing.T) {
	policy := &catalog.ArchivePolicy{
		Enabled:           true,
		TargetStore:       "glacier",
		ArchiveAfterDays:  90,
		DeleteFromPrimary: true,
	}

	result := archivePolicyToProto(policy)

	assert.NotNil(t, result)
	assert.True(t, result.Enabled)
	assert.Equal(t, "glacier", result.TargetStore)
	assert.Equal(t, int32(90), result.ArchiveAfterDays)
	assert.True(t, result.DeleteFromPrimary)
}

// =============================================================================
// Retention Policy Conversion Tests (with non-nil inputs)
// =============================================================================

func TestRetentionPolicyToProto_WithValues(t *testing.T) {
	policy := &catalog.RetentionPolicy{
		Enabled:       true,
		RetentionDays: 90,
		MaxVersions:   10,
	}

	result := retentionPolicyToProto(policy)

	assert.NotNil(t, result)
	assert.True(t, result.Enabled)
	assert.Equal(t, int32(90), result.RetentionDays)
	assert.Equal(t, int32(10), result.MaxVersions)
}
