//go:build mcp

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

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// MCP tool arguments arrive as decoded JSON, so the numeric fields come in as
// float64 (or as strings from a model that quoted them); the tool has to turn
// both into the engine's int64 limits.
func TestExecuteIAMTenantCreate_ForwardsEngineProvisioningKnobs(t *testing.T) {
	var captured *service.CreateTenantRequest
	reg := newIAMTestRegistry(&mockIAMService{
		createTenantFunc: func(_ context.Context, req *service.CreateTenantRequest) (*service.CreateTenantResponse, error) {
			captured = req
			return &service.CreateTenantResponse{Tenant: &iam.Tenant{ID: "acme", Name: req.Name}}, nil
		},
	})

	_, err := reg.executeIAMTenantCreate(context.Background(), map[string]interface{}{
		"id":             "acme",
		"name":           "Acme",
		"isolation_type": "barrier",
		"engine_quotas": map[string]interface{}{
			"max_databases":        float64(4),
			"system_barrier_quota": float64(-1),
		},
		"engine_entitlements": map[string]interface{}{
			"allowed_isolation_modes": []interface{}{"prefix", "barrier"},
			"allow_dedicated_raft":    true,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, captured)

	assert.Equal(t, qrdbsdk.TenantIsolationBarrier, captured.IsolationType)
	require.NotNil(t, captured.EngineQuotas)
	assert.Equal(t, int64(4), captured.EngineQuotas.MaxDatabases)
	assert.Equal(t, int64(-1), captured.EngineQuotas.SystemBarrierQuota)
	require.NotNil(t, captured.EngineEntitlements)
	assert.Equal(t, []string{"prefix", "barrier"}, captured.EngineEntitlements.AllowedIsolationModes)
	assert.True(t, captured.EngineEntitlements.AllowDedicatedRaft)
}

// A model that quotes its numbers must not silently produce a tenant with
// every limit at zero.
func TestExecuteIAMTenantCreate_QuotedQuotaNumbers(t *testing.T) {
	var captured *service.CreateTenantRequest
	reg := newIAMTestRegistry(&mockIAMService{
		createTenantFunc: func(_ context.Context, req *service.CreateTenantRequest) (*service.CreateTenantResponse, error) {
			captured = req
			return &service.CreateTenantResponse{Tenant: &iam.Tenant{ID: "acme", Name: req.Name}}, nil
		},
	})

	_, err := reg.executeIAMTenantCreate(context.Background(), map[string]interface{}{
		"name":          "Acme",
		"engine_quotas": map[string]interface{}{"max_databases": "4"},
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	require.NotNil(t, captured.EngineQuotas)
	assert.Equal(t, int64(4), captured.EngineQuotas.MaxDatabases)
}

func TestExecuteIAMTenantCreate_OmittedKnobsStayNil(t *testing.T) {
	var captured *service.CreateTenantRequest
	reg := newIAMTestRegistry(&mockIAMService{
		createTenantFunc: func(_ context.Context, req *service.CreateTenantRequest) (*service.CreateTenantResponse, error) {
			captured = req
			return &service.CreateTenantResponse{Tenant: &iam.Tenant{ID: "acme", Name: req.Name}}, nil
		},
	})

	_, err := reg.executeIAMTenantCreate(context.Background(), map[string]interface{}{"name": "Acme"})
	require.NoError(t, err)
	require.NotNil(t, captured)

	assert.Empty(t, string(captured.IsolationType))
	assert.Nil(t, captured.EngineQuotas)
	assert.Nil(t, captured.EngineEntitlements)
}

// A malformed quota object is a caller mistake, not a reason to create the
// tenant with silently zeroed limits.
func TestExecuteIAMTenantCreate_MalformedQuotasRejected(t *testing.T) {
	called := false
	reg := newIAMTestRegistry(&mockIAMService{
		createTenantFunc: func(_ context.Context, req *service.CreateTenantRequest) (*service.CreateTenantResponse, error) {
			called = true
			return &service.CreateTenantResponse{Tenant: &iam.Tenant{ID: "acme"}}, nil
		},
	})

	_, err := reg.executeIAMTenantCreate(context.Background(), map[string]interface{}{
		"name":          "Acme",
		"engine_quotas": map[string]interface{}{"max_databases": []interface{}{1, 2}},
	})
	require.Error(t, err)
	assert.False(t, called, "the tenant must not be created from a malformed quota object")
}

// The tool schema is what an MCP client reads to know a knob exists.
func TestIAMTenantCreateToolSchema_DeclaresProvisioningKnobs(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{})

	var schema map[string]interface{}
	for _, tool := range reg.ListTools() {
		if tool.Name == "quicbaas_iam_tenant_create" {
			schema = tool.InputSchema
			break
		}
	}
	require.NotNil(t, schema, "quicbaas_iam_tenant_create is not registered")

	props, ok := schema["properties"].(map[string]interface{})
	require.True(t, ok, "input schema has no properties object")
	for _, key := range []string{"isolation_type", "engine_quotas", "engine_entitlements"} {
		assert.Contains(t, props, key)
	}
}

// The MCP arg is decoded strictly for the same reason: a mistyped key would
// otherwise become an empty override that replaces the engine's defaults, and
// a non-object value would be ignored entirely.
func TestExecuteIAMTenantCreate_RejectsBadProvisioningArgs(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "engine_quotas is not an object",
			args: map[string]interface{}{"name": "Acme", "engine_quotas": "unlimited"},
		},
		{
			name: "engine_entitlements is not an object",
			args: map[string]interface{}{"name": "Acme", "engine_entitlements": []interface{}{1, 2}},
		},
		{
			name: "misspelled quota key",
			args: map[string]interface{}{"name": "Acme", "engine_quotas": map[string]interface{}{"max_databasses": float64(4)}},
		},
		{
			name: "misspelled entitlement key",
			args: map[string]interface{}{"name": "Acme", "engine_entitlements": map[string]interface{}{"allow_dedicated_rafts": true}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			reg := newIAMTestRegistry(&mockIAMService{
				createTenantFunc: func(_ context.Context, req *service.CreateTenantRequest) (*service.CreateTenantResponse, error) {
					called = true
					return &service.CreateTenantResponse{Tenant: &iam.Tenant{ID: "acme"}}, nil
				},
			})

			_, err := reg.executeIAMTenantCreate(context.Background(), tt.args)
			require.Error(t, err)

			var provErr *TenantEngineObjectError
			assert.ErrorAs(t, err, &provErr, "expected a typed provisioning error")
			assert.False(t, called, "the tenant must not be created from a bad provisioning argument")
		})
	}
}

// isolation_type present with the wrong type must be an error, not a tenant
// quietly created on default isolation.
func TestExecuteIAMTenantCreate_RejectsWrongTypedIsolation(t *testing.T) {
	called := false
	reg := newIAMTestRegistry(&mockIAMService{
		createTenantFunc: func(_ context.Context, req *service.CreateTenantRequest) (*service.CreateTenantResponse, error) {
			called = true
			return &service.CreateTenantResponse{Tenant: &iam.Tenant{ID: "acme"}}, nil
		},
	})

	_, err := reg.executeIAMTenantCreate(context.Background(), map[string]interface{}{
		"name":           "Acme",
		"isolation_type": float64(123),
	})
	require.Error(t, err)
	var provErr *TenantEngineObjectError
	assert.ErrorAs(t, err, &provErr)
	assert.False(t, called, "the tenant must not be created from a wrong-typed isolation_type")
}

// Usage counters are engine-owned; gRPC cannot express them, so the accepted
// shape here is limits-only too — a counter is an unknown field whatever its
// value.
func TestExecuteIAMTenantCreate_RejectsEngineUsageCounters(t *testing.T) {
	tests := []struct {
		name   string
		quotas map[string]interface{}
	}{
		{name: "non-zero counter", quotas: map[string]interface{}{"max_databases": float64(4), "current_databases": float64(3)}},
		{name: "zero counter is still a counter field", quotas: map[string]interface{}{"max_databases": float64(4), "current_databases": float64(0)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			reg := newIAMTestRegistry(&mockIAMService{
				createTenantFunc: func(_ context.Context, req *service.CreateTenantRequest) (*service.CreateTenantResponse, error) {
					called = true
					return &service.CreateTenantResponse{Tenant: &iam.Tenant{ID: "acme"}}, nil
				},
			})

			_, err := reg.executeIAMTenantCreate(context.Background(), map[string]interface{}{
				"name":          "Acme",
				"engine_quotas": tt.quotas,
			})
			require.Error(t, err)
			assert.False(t, called, "the tenant must not be created from a request carrying usage counters")
		})
	}
}
