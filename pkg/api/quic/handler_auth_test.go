//go:build quic

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

package quic

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Opcode classification + permission mapping tests.
//
// Request-time authorization is enforced exclusively through the unified
// platform-token validator (see handler_authz_validator_test.go and
// handler_authz_qrdb_test.go). The mapping helpers below stay independently
// unit-tested because the validator path consumes getQRDBPermission and the
// broker-name mapper (getResourceAndAction) is still exposed for callers via
// GetResourceAndActionForTest.
// =============================================================================

func TestIsPublicOperation(t *testing.T) {
	tests := []struct {
		name     string
		opcode   byte
		expected bool
	}{
		{"Health is public", OpHealth, true},
		{"Liveness is public", OpLiveness, true},
		{"Readiness is public", OpReadiness, true},
		{"Get is not public", OpGet, false},
		{"Put is not public", OpPut, false},
		{"Delete is not public", OpDelete, false},
		{"ClusterStatus is not public", OpClusterStatus, false},
		{"KMSGenerateKey is not public", OpKMSGenerateKey, false},
		{"SecretsGet is not public", OpSecretsGet, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPublicOperation(tt.opcode)
			if result != tt.expected {
				t.Errorf("isPublicOperation(%d) = %v, want %v", tt.opcode, result, tt.expected)
			}
		})
	}
}

func TestGetResourceAndAction(t *testing.T) {
	tests := []struct {
		name         string
		opcode       byte
		wantResource string
		wantAction   string
	}{
		// KV Operations
		{"Get", OpGet, interfaces.ResourceKeys, interfaces.ActionRead},
		{"List", OpList, interfaces.ResourceKeys, interfaces.ActionRead},
		{"Put", OpPut, interfaces.ResourceKeys, interfaces.ActionWrite},
		{"Delete", OpDelete, interfaces.ResourceKeys, interfaces.ActionDelete},
		{"BatchGet", OpBatchGet, interfaces.ResourceKeys, interfaces.ActionRead},
		{"BatchPut", OpBatchPut, interfaces.ResourceKeys, interfaces.ActionWrite},
		{"BatchDelete", OpBatchDelete, interfaces.ResourceKeys, interfaces.ActionDelete},

		// Transaction Operations
		{"Transaction", OpTransaction, interfaces.ResourceTransactions, interfaces.ActionWrite},

		// Query Operations
		{"Query", OpQuery, interfaces.ResourceKeys, interfaces.ActionRead},
		{"ScanIndex", OpScanIndex, interfaces.ResourceKeys, interfaces.ActionRead},

		// Cluster Operations - Read
		{"ClusterStatus", OpClusterStatus, interfaces.ResourceCluster, interfaces.ActionRead},
		{"GetLeader", OpGetLeader, interfaces.ResourceCluster, interfaces.ActionRead},
		{"ListNodes", OpListNodes, interfaces.ResourceCluster, interfaces.ActionRead},

		// Cluster Operations - Admin
		{"AddNode", OpAddNode, interfaces.ResourceCluster, interfaces.ActionAdmin},
		{"RemoveNode", OpRemoveNode, interfaces.ResourceCluster, interfaces.ActionAdmin},
		{"RebalanceCluster", OpRebalanceCluster, interfaces.ResourceCluster, interfaces.ActionAdmin},

		// Barrier Operations (single dispatch opcode; sub-ops validated by handler)
		{"Barrier", OpBarrier, interfaces.ResourceBarrier, interfaces.ActionAdmin},

		// Ceremony Operations (single dispatch opcode)
		{"Ceremony", OpCeremony, interfaces.ResourceCeremony, interfaces.ActionAdmin},

		// KMS Operations
		{"KMSGetKey", OpKMSGetKey, interfaces.ResourceKMS, interfaces.ActionRead},
		{"KMSListKeys", OpKMSListKeys, interfaces.ResourceKMS, interfaces.ActionRead},
		{"KMSGenerateKey", OpKMSGenerateKey, interfaces.ResourceKMS, interfaces.ActionWrite},
		{"KMSDeleteKey", OpKMSDeleteKey, interfaces.ResourceKMS, interfaces.ActionDelete},

		// Secrets Operations
		{"SecretsGet", OpSecretsGet, interfaces.ResourceSecrets, interfaces.ActionRead},
		{"SecretsList", OpSecretsList, interfaces.ResourceSecrets, interfaces.ActionRead},
		{"SecretsStore", OpSecretsStore, interfaces.ResourceSecrets, interfaces.ActionWrite},
		{"SecretsDelete", OpSecretsDelete, interfaces.ResourceSecrets, interfaces.ActionDelete},

		// Crypto Operations
		{"CryptoEncrypt", OpCryptoEncrypt, interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"CryptoDecrypt", OpCryptoDecrypt, interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"CryptoSign", OpCryptoSign, interfaces.ResourceCrypto, interfaces.ActionWrite},

		// Time-Series Operations
		{"TSQuery", OpTSQuery, interfaces.ResourceTimeseries, interfaces.ActionRead},
		{"TSWritePoint", OpTSWritePoint, interfaces.ResourceTimeseries, interfaces.ActionWrite},
		{"TSDeleteSeries", OpTSDeleteSeries, interfaces.ResourceTimeseries, interfaces.ActionDelete},

		// ObjectStorage Admin (0xCF) - sub-op dispatch
		{"OSAdmin", OpOSAdmin, interfaces.ResourceObjectStore, interfaces.ActionAdmin},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := getResourceAndAction(tt.opcode)
			if resource != tt.wantResource {
				t.Errorf("getResourceAndAction(%d) resource = %q, want %q", tt.opcode, resource, tt.wantResource)
			}
			if action != tt.wantAction {
				t.Errorf("getResourceAndAction(%d) action = %q, want %q", tt.opcode, action, tt.wantAction)
			}
		})
	}
}
