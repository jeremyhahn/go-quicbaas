//go:build rest

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

package rest

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// The engine provisioning knobs are creation-only inputs that never appear on
// the persisted Tenant record, so the recorded CreateTenantOptions is the only
// place a dropped field would be visible.

func TestCreateTenant_ForwardsEngineProvisioningKnobs(t *testing.T) {
	_, mockService, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	body := `{
		"id": "acme",
		"name": "Acme",
		"isolation_type": "barrier",
		"engine_quotas": {"max_databases": 4, "max_storage_bytes": 1024, "system_barrier_quota": -1},
		"engine_entitlements": {"allowed_isolation_modes": ["prefix","barrier"], "allow_dedicated_raft": true}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateTenant(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	opts := mockService.tenantManager.lastCreateOpts
	assert.Equal(t, qrdbsdk.TenantIsolationBarrier, opts.IsolationType)

	require.NotNil(t, opts.EngineQuotas, "engine_quotas must reach the tenant manager")
	assert.Equal(t, int64(4), opts.EngineQuotas.MaxDatabases)
	assert.Equal(t, int64(1024), opts.EngineQuotas.MaxStorageBytes)
	assert.Equal(t, int64(-1), opts.EngineQuotas.SystemBarrierQuota)

	require.NotNil(t, opts.EngineEntitlements, "engine_entitlements must reach the tenant manager")
	assert.Equal(t, []string{"prefix", "barrier"}, opts.EngineEntitlements.AllowedIsolationModes)
	assert.True(t, opts.EngineEntitlements.AllowDedicatedRaft)
}

// Nil, not an empty struct: an empty Quotas would be read as "use the system
// default" for every limit, which is a different instruction than "leave the
// engine's configured defaults alone".
func TestCreateTenant_OmittedKnobsStayNil(t *testing.T) {
	_, mockService, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	body := `{"id": "acme", "name": "Acme"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateTenant(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	opts := mockService.tenantManager.lastCreateOpts
	assert.Empty(t, string(opts.IsolationType), "an unset isolation type must defer to the engine default")
	assert.Nil(t, opts.EngineQuotas)
	assert.Nil(t, opts.EngineEntitlements)
}

func TestCreateTenant_MalformedKnobsRejected(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "engine_quotas is not an object", body: `{"id":"acme","name":"Acme","engine_quotas":"unlimited"}`},
		{name: "engine_entitlements is not an object", body: `{"id":"acme","name":"Acme","engine_entitlements":[1,2]}`},
		{name: "quota limit is not a number", body: `{"id":"acme","name":"Acme","engine_quotas":{"max_keys":"many"}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, handler, cleanup := setupIAMTestServer(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", bytes.NewReader([]byte(tt.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.CreateTenant(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code,
				"a malformed provisioning knob must be rejected, not silently zeroed")
		})
	}
}

// An unrecognized key must be an error, not an empty override: absence of the
// object means "keep the engine's configured defaults", so a typo that decoded
// to an empty struct would silently replace those defaults instead.
func TestCreateTenant_UnknownProvisioningFieldRejected(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "misspelled quota key", body: `{"id":"acme","name":"Acme","engine_quotas":{"max_databasses":4}}`},
		{name: "misspelled entitlement key", body: `{"id":"acme","name":"Acme","engine_entitlements":{"allow_dedicated_rafts":true}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, mockService, _, handler, cleanup := setupIAMTestServer(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", bytes.NewReader([]byte(tt.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.CreateTenant(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Empty(t, mockService.tenantManager.lastCreateOpts.Name,
				"the tenant must not be created from an unrecognized provisioning key")
		})
	}
}

// A literal JSON null is the caller spelling "not supplied"; decoding it would
// produce a non-nil zero override and cross the nil-vs-empty boundary.
func TestCreateTenant_NullProvisioningKnobsAreAbsent(t *testing.T) {
	_, mockService, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	body := `{"id":"acme","name":"Acme","engine_quotas":null,"engine_entitlements":null,"isolation_type":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateTenant(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	opts := mockService.tenantManager.lastCreateOpts
	assert.Nil(t, opts.EngineQuotas, "a null engine_quotas must be absent, not an empty override")
	assert.Nil(t, opts.EngineEntitlements, "a null engine_entitlements must be absent, not an empty override")
}

// The Current* counters are engine-owned outputs. gRPC's create request models
// limits only, so the accepted shape here is limits-only too: a counter is an
// unknown field, whatever its value.
func TestCreateTenant_RejectsEngineUsageCounters(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "non-zero counter", body: `{"id":"acme","name":"Acme","engine_quotas":{"max_databases":4,"current_databases":3}}`},
		{name: "zero counter is still a counter field", body: `{"id":"acme","name":"Acme","engine_quotas":{"max_databases":4,"current_databases":0}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, mockService, _, handler, cleanup := setupIAMTestServer(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", bytes.NewReader([]byte(tt.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.CreateTenant(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Empty(t, mockService.tenantManager.lastCreateOpts.Name,
				"the tenant must not be created from a request carrying usage counters")
		})
	}
}

// The limits still arrive as the engine's own struct, with the counters the
// engine owns left at zero.
func TestCreateTenant_LimitsWidenWithZeroedCounters(t *testing.T) {
	_, mockService, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	body := `{"id":"acme","name":"Acme","engine_quotas":{"max_databases":4,"max_ts_metrics":12}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateTenant(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	quotas := mockService.tenantManager.lastCreateOpts.EngineQuotas
	require.NotNil(t, quotas)
	assert.Equal(t, int64(4), quotas.MaxDatabases)
	assert.Equal(t, int64(12), quotas.MaxTSMetrics)
	assert.Zero(t, quotas.CurrentDatabases)
}
