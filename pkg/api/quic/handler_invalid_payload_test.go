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
	"context"
	"testing"
)

// =============================================================================
// Invalid Payload Tests for handlers at 83.3% coverage
// These tests cover the DecodePayload error path
// =============================================================================

func TestHandler_BackupOperations_InvalidPayload(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, nil, createTestLogger())
	ctx := context.Background()
	invalidPayload := []byte("{invalid json")

	tests := []struct {
		name   string
		opcode byte
	}{
		{"ListBackups", OpListBackups},
		{"RestoreBackup", OpRestoreBackup},
		{"DeleteBackup", OpDeleteBackup},
		{"VerifyBackup", OpVerifyBackup},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := handler.HandleRequest(ctx, &Request{
				Opcode:  tt.opcode,
				Payload: invalidPayload,
			})
			if resp.Status == StatusOK {
				t.Errorf("Expected error for invalid payload on %s", tt.name)
			}
		})
	}
}

func TestHandler_AuthOperations_InvalidPayload(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, nil, createTestLogger())
	ctx := context.Background()
	invalidPayload := []byte("{invalid json")

	tests := []struct {
		name   string
		opcode byte
	}{
		{"Logout", OpLogout},
		{"RefreshToken", OpRefreshToken},
		{"ValidateToken", OpValidateToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := handler.HandleRequest(ctx, &Request{
				Opcode:  tt.opcode,
				Payload: invalidPayload,
			})
			if resp.Status == StatusOK {
				t.Errorf("Expected error for invalid payload on %s", tt.name)
			}
		})
	}
}

func TestHandler_RBACOperations_InvalidPayload(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, nil, createTestLogger())
	ctx := context.Background()
	invalidPayload := []byte("{invalid json")

	tests := []struct {
		name   string
		opcode byte
	}{
		{"CreateUser", OpCreateUser},
		{"DeleteUser", OpDeleteUser},
		{"ListUsers", OpListUsers},
		{"CreateRole", OpCreateRole},
		{"DeleteRole", OpDeleteRole},
		{"ListRoles", OpListRoles},
		{"AssignRole", OpAssignRole},
		{"RevokeRole", OpRevokeRole},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := handler.HandleRequest(ctx, &Request{
				Opcode:  tt.opcode,
				Payload: invalidPayload,
			})
			if resp.Status == StatusOK {
				t.Errorf("Expected error for invalid payload on %s", tt.name)
			}
		})
	}
}

func TestHandler_ClusterOperations_InvalidPayload(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, nil, createTestLogger())
	ctx := context.Background()
	invalidPayload := []byte("{invalid json")

	tests := []struct {
		name   string
		opcode byte
	}{
		{"RebalanceCluster", OpRebalanceCluster},
		{"AddNode", OpAddNode},
		{"RemoveNode", OpRemoveNode},
		{"CreateShard", OpCreateShard},
		{"ClusterStatus", OpClusterStatus},
		{"GetLeader", OpGetLeader},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := handler.HandleRequest(ctx, &Request{
				Opcode:  tt.opcode,
				Payload: invalidPayload,
			})
			if resp.Status == StatusOK {
				t.Errorf("Expected error for invalid payload on %s", tt.name)
			}
		})
	}
}

func TestHandler_OperationsManagement_InvalidPayload(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, nil, createTestLogger())
	ctx := context.Background()
	invalidPayload := []byte("{invalid json")

	tests := []struct {
		name   string
		opcode byte
	}{
		{"MigrateData", OpMigrateData},
		{"GetMigrationProgress", OpGetMigrationProgress},
		{"ReplaceNode", OpReplaceNode},
		{"UpgradeNode", OpUpgradeNode},
		{"GetUpgradeStatus", OpGetUpgradeStatus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := handler.HandleRequest(ctx, &Request{
				Opcode:  tt.opcode,
				Payload: invalidPayload,
			})
			if resp.Status == StatusOK {
				t.Errorf("Expected error for invalid payload on %s", tt.name)
			}
		})
	}
}

func TestHandler_CacheOperations_InvalidPayload(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, nil, createTestLogger())
	ctx := context.Background()
	invalidPayload := []byte("{invalid json")

	tests := []struct {
		name   string
		opcode byte
	}{
		{"GetCacheStats", OpGetCacheStats},
		{"ClearCache", OpClearCache},
		{"GetCacheConfig", OpGetCacheConfig},
		{"UpdateCacheConfig", OpUpdateCacheConfig},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := handler.HandleRequest(ctx, &Request{
				Opcode:  tt.opcode,
				Payload: invalidPayload,
			})
			if resp.Status == StatusOK {
				t.Errorf("Expected error for invalid payload on %s", tt.name)
			}
		})
	}
}

func TestHandler_TimeSeriesOperations_InvalidPayload(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, nil, createTestLogger())
	ctx := context.Background()
	invalidPayload := []byte("{invalid json")

	tests := []struct {
		name   string
		opcode byte
	}{
		{"Aggregate", OpAggregate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := handler.HandleRequest(ctx, &Request{
				Opcode:  tt.opcode,
				Payload: invalidPayload,
			})
			if resp.Status == StatusOK {
				t.Errorf("Expected error for invalid payload on %s", tt.name)
			}
		})
	}
}

func TestHandler_HealthOperations_InvalidPayload(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, nil, createTestLogger())
	ctx := context.Background()
	invalidPayload := []byte("{invalid json")

	tests := []struct {
		name   string
		opcode byte
	}{
		{"Health", OpHealth},
		{"Liveness", OpLiveness},
		{"Readiness", OpReadiness},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := handler.HandleRequest(ctx, &Request{
				Opcode:  tt.opcode,
				Payload: invalidPayload,
			})
			if resp.Status == StatusOK {
				t.Errorf("Expected error for invalid payload on %s", tt.name)
			}
		})
	}
}
