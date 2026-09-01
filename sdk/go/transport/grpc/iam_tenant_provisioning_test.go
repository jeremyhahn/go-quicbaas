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

	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

func TestEngineQuotasToProto(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		wantNil bool
		wantErr bool
		verify  func(t *testing.T, got interface{})
	}{
		{
			name:    "absent field keeps engine defaults",
			input:   nil,
			wantNil: true,
		},
		{
			name: "decoded JSON object",
			input: map[string]interface{}{
				"max_databases":            float64(4),
				"max_storage_bytes":        float64(1024),
				"max_dedicated_wal_groups": float64(2),
				"system_barrier_quota":     float64(-1),
			},
		},
		{
			name:  "already-typed value from an in-process caller",
			input: &iam.EngineQuotas{MaxDatabases: 4, MaxStorageBytes: 1024, MaxDedicatedWALGroups: 2, SystemBarrierQuota: -1},
		},
		{
			name:    "wrong shape is rejected, not silently zeroed",
			input:   "not-an-object",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := engineQuotasToProto(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error for a malformed engine_quotas value")
				}
				return
			}
			if err != nil {
				t.Fatalf("engineQuotasToProto returned error: %v", err)
			}
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil for an absent value, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected a converted message")
			}
			if got.GetMaxDatabases() != 4 {
				t.Errorf("MaxDatabases = %d, want 4", got.GetMaxDatabases())
			}
			if got.GetMaxStorageBytes() != 1024 {
				t.Errorf("MaxStorageBytes = %d, want 1024", got.GetMaxStorageBytes())
			}
			// The two fields whose Go and proto spellings differ (WAL/TS) are
			// the ones a hand-written mapping gets wrong, so assert them.
			if got.GetMaxDedicatedWalGroups() != 2 {
				t.Errorf("MaxDedicatedWalGroups = %d, want 2", got.GetMaxDedicatedWalGroups())
			}
			if got.GetSystemBarrierQuota() != -1 {
				t.Errorf("SystemBarrierQuota = %d, want -1 (unlimited)", got.GetSystemBarrierQuota())
			}
		})
	}
}

func TestEngineEntitlementsToProto(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		wantNil bool
		wantErr bool
	}{
		{name: "absent field keeps engine defaults", input: nil, wantNil: true},
		{
			name: "decoded JSON object",
			input: map[string]interface{}{
				"allowed_isolation_modes": []interface{}{"prefix", "barrier"},
				"allow_dedicated_raft":    true,
				"allow_dedicated_wal":     true,
			},
		},
		{
			name:  "already-typed value from an in-process caller",
			input: &iam.EngineEntitlements{AllowedIsolationModes: []string{"prefix", "barrier"}, AllowDedicatedRaft: true, AllowDedicatedWAL: true},
		},
		{
			name:    "wrong shape is rejected, not silently zeroed",
			input:   []interface{}{1, 2, 3},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := engineEntitlementsToProto(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error for a malformed engine_entitlements value")
				}
				return
			}
			if err != nil {
				t.Fatalf("engineEntitlementsToProto returned error: %v", err)
			}
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil for an absent value, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected a converted message")
			}
			if len(got.GetAllowedIsolationModes()) != 2 {
				t.Errorf("AllowedIsolationModes = %v, want two entries", got.GetAllowedIsolationModes())
			}
			if !got.GetAllowDedicatedRaft() {
				t.Error("AllowDedicatedRaft = false, want true")
			}
			if !got.GetAllowDedicatedWal() {
				t.Error("AllowDedicatedWal = false, want true")
			}
		})
	}
}

// An unrecognized key must be an error, not an empty override. Absence of the
// object means "keep the engine's configured defaults", so a typo that decoded
// to an empty struct would silently replace those defaults instead.
func TestEngineProvisioningToProto_RejectsUnknownFields(t *testing.T) {
	if _, err := engineQuotasToProto(map[string]interface{}{"max_databasses": float64(4)}); err == nil {
		t.Error("expected a misspelled quota key to be rejected")
	}
	if _, err := engineEntitlementsToProto(map[string]interface{}{"allow_dedicated_rafts": true}); err == nil {
		t.Error("expected a misspelled entitlement key to be rejected")
	}
}

// iam.EngineQuotas names the limits only, so an engine-owned counter is an
// unknown field on the wire rather than a value the proto would drop.
func TestEngineQuotasToProto_RejectsUsageCounters(t *testing.T) {
	if _, err := engineQuotasToProto(map[string]interface{}{"current_storage_bytes": float64(9)}); err == nil {
		t.Error("expected an engine-owned usage counter to be rejected, not dropped")
	}
	if _, err := engineQuotasToProto(map[string]interface{}{"current_databases": float64(0)}); err == nil {
		t.Error("a zero-valued counter is still a counter field and must be rejected")
	}
}
