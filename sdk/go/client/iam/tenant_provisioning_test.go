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

package iam

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The provisioning knobs only matter if they survive the trip to the wire, so
// these tests assert on what the transport actually receives and on the JSON
// the map-based transports (REST, QUIC, MCP) marshal.

func TestCreateTenantRequest_ProvisioningKnobsReachTheTransport(t *testing.T) {
	var captured *CreateTenantRequest
	client := NewTenantClient(&mockTransport{
		requestFunc: func(_ context.Context, _, _ string, req interface{}) (interface{}, error) {
			captured, _ = req.(*CreateTenantRequest)
			return &Tenant{ID: "acme"}, nil
		},
	})

	quotas := &EngineQuotas{MaxDatabases: 12, MaxStorageBytes: 1 << 30}
	entitlements := &EngineEntitlements{
		AllowedIsolationModes: []string{string(TenantIsolationBarrier)},
		AllowDedicatedRaft:    true,
	}

	_, err := client.Create(context.Background(), &CreateTenantRequest{
		ID:                 "acme",
		Name:               "Acme",
		IsolationType:      TenantIsolationBarrier,
		EngineQuotas:       quotas,
		EngineEntitlements: entitlements,
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if captured == nil {
		t.Fatal("transport did not receive a *CreateTenantRequest")
	}
	if captured.IsolationType != TenantIsolationBarrier {
		t.Errorf("IsolationType = %q, want %q", captured.IsolationType, TenantIsolationBarrier)
	}
	if captured.EngineQuotas != quotas {
		t.Error("EngineQuotas did not reach the transport unchanged")
	}
	if captured.EngineEntitlements != entitlements {
		t.Error("EngineEntitlements did not reach the transport unchanged")
	}
}

func TestCreateTenantRequest_WireKeys(t *testing.T) {
	req := &CreateTenantRequest{
		Name:               "Acme",
		IsolationType:      TenantIsolationVolume,
		EngineQuotas:       &EngineQuotas{MaxKeys: 7},
		EngineEntitlements: &EngineEntitlements{AllowVolumeStorage: true},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	if got := decoded["isolation_type"]; got != string(TenantIsolationVolume) {
		t.Errorf("isolation_type = %v, want %q", got, TenantIsolationVolume)
	}
	quotas, ok := decoded["engine_quotas"].(map[string]interface{})
	if !ok {
		t.Fatalf("engine_quotas is %T, want an object", decoded["engine_quotas"])
	}
	if got := quotas["max_keys"]; got != float64(7) {
		t.Errorf("engine_quotas.max_keys = %v, want 7", got)
	}
	entitlements, ok := decoded["engine_entitlements"].(map[string]interface{})
	if !ok {
		t.Fatalf("engine_entitlements is %T, want an object", decoded["engine_entitlements"])
	}
	if got := entitlements["allow_volume_storage"]; got != true {
		t.Errorf("engine_entitlements.allow_volume_storage = %v, want true", got)
	}
}

// A caller that never touched the new fields must produce exactly the body it
// produced before they existed — that is what makes the change additive.
func TestCreateTenantRequest_UnsetKnobsAreOmitted(t *testing.T) {
	data, err := json.Marshal(&CreateTenantRequest{Name: "Acme"})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	for _, key := range []string{"isolation_type", "engine_quotas", "engine_entitlements"} {
		if _, present := decoded[key]; present {
			t.Errorf("%s must be omitted when unset, body was %s", key, data)
		}
	}
}

// Validation is unchanged by the new fields: name is still the only
// requirement, and setting a knob does not make an invalid request valid.
func TestValidateCreateTenantRequest_WithProvisioningKnobs(t *testing.T) {
	tests := []struct {
		name    string
		req     *CreateTenantRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "knobs set but name missing",
			req:     &CreateTenantRequest{IsolationType: TenantIsolationBarrier, EngineQuotas: &EngineQuotas{}},
			wantErr: true,
		},
		{
			name:    "name only",
			req:     &CreateTenantRequest{Name: "Acme"},
			wantErr: false,
		},
		{
			name:    "name with knobs",
			req:     &CreateTenantRequest{Name: "Acme", IsolationType: TenantIsolationRaft},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCreateTenantRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateCreateTenantRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// The engine owns the Current* counters, and EngineQuotas does not name them:
// a Go caller cannot set one (that is a compile error), and the wire form
// rejects one as an unknown field. These assertions pin the accepted shape,
// which is what has to match the gRPC create request.
func TestEngineQuotas_ShapeIsLimitsOnly(t *testing.T) {
	data, err := json.Marshal(&EngineQuotas{MaxDatabases: 4, SystemBarrierQuota: -1})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	for key := range decoded {
		if strings.HasPrefix(key, "current_") {
			t.Errorf("EngineQuotas must not carry the engine-owned counter %q", key)
		}
	}

	// Every limit the proto carries must be present, so no transport has to
	// invent a value the caller could not name.
	for _, key := range []string{
		"max_databases", "max_raft_groups", "max_replicas_per_group",
		"max_total_replicas", "max_collections", "max_keys", "max_storage_bytes",
		"max_database_size_bytes", "max_users", "max_roles", "max_certificates",
		"max_backups", "max_vector_dims", "max_ts_metrics",
		"max_dedicated_wal_groups", "system_barrier_quota",
	} {
		if _, present := decoded[key]; !present {
			t.Errorf("EngineQuotas is missing the limit %q", key)
		}
	}
}

// Widening to the engine struct must carry every limit and leave the counters
// for the engine to fill in.
func TestEngineQuotas_Quotas(t *testing.T) {
	if (*EngineQuotas)(nil).Quotas() != nil {
		t.Error("a nil EngineQuotas must widen to nil, preserving \"keep engine defaults\"")
	}

	got := (&EngineQuotas{
		MaxDatabases:          4,
		MaxTSMetrics:          12,
		MaxDedicatedWALGroups: 13,
		SystemBarrierQuota:    -1,
	}).Quotas()
	if got == nil {
		t.Fatal("expected a widened Quotas")
	}
	if got.MaxDatabases != 4 || got.MaxTSMetrics != 12 || got.MaxDedicatedWALGroups != 13 || got.SystemBarrierQuota != -1 {
		t.Errorf("limits did not survive widening: %+v", got)
	}
	if got.CurrentDatabases != 0 || got.CurrentStorageBytes != 0 || got.CurrentSystemBarrierBytes != 0 {
		t.Errorf("usage counters must stay zero for the engine to fill in: %+v", got)
	}
}
