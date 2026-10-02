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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// costSettingsAgent is an agent service holding a cost settings store.
type costSettingsAgent struct {
	*mockAgentService
	store orchestration.CostSettingsStore
}

func (a *costSettingsAgent) CostSettingsStore() orchestration.CostSettingsStore { return a.store }
func (a *costSettingsAgent) PriceBook() *orchestration.PriceBook                { return nil }

func costSettingsServer(t *testing.T, wired bool) *Server {
	t.Helper()
	server, _, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	var agent orchestration.AgentService = &mockAgentService{}
	if wired {
		agent = &costSettingsAgent{mockAgentService: &mockAgentService{}, store: orchestration.NewInMemoryCostSettingsStore()}
	}
	server.service = &MockQuicBaaSWithAgent{MockQuicBaaS: testutil.NewMockQuicBaaS(), agentService: agent}
	return server
}

// TestCostSettingsOverREST is go-quicbaas#1058: GET reads the default $0.12
// rate, PUT sets a rate and an engine draw, GET reads them back in force.
func TestCostSettingsOverREST(t *testing.T) {
	server := costSettingsServer(t, true)
	get := func() orchestration.CostSettingsReport {
		w := httptest.NewRecorder()
		server.HandleGetCostSettings(w, systemRequest(http.MethodGet, "/api/v1/orchestrator/cost-settings", nil))
		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
		var report orchestration.CostSettingsReport
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &report))
		return report
	}

	before := get()
	assert.Equal(t, 0.12, before.Electricity.PricePerKWh)
	assert.Equal(t, orchestration.ElectricityFromDefault, before.Electricity.Source)

	w := httptest.NewRecorder()
	server.HandleSetCostSettings(w, systemRequest(http.MethodPut, "/api/v1/orchestrator/cost-settings",
		bytes.NewBufferString(`{"electricity_price_per_kwh":0.2,"engine_power_watts":{"openvino":12}}`)))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	after := get()
	assert.Equal(t, 0.2, after.Electricity.PricePerKWh)
	assert.Equal(t, orchestration.ElectricityFromTenantSettings, after.Electricity.Source)
	assert.Equal(t, 12.0, after.EngineDraws["openvino"])
}

func TestCostSettingsOverRESTRefuses(t *testing.T) {
	tests := []struct {
		name  string
		wired bool
		body  string
		want  int
	}{
		{"an unwired deployment", false, `{}`, http.StatusServiceUnavailable},
		{"a body that is not JSON", true, `{`, http.StatusBadRequest},
		{"a negative rate", true, `{"electricity_price_per_kwh":-1}`, http.StatusBadRequest},
		{"a negative draw", true, `{"engine_power_watts":{"ollama":-5}}`, http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := costSettingsServer(t, tc.wired)
			w := httptest.NewRecorder()
			server.HandleSetCostSettings(w, systemRequest(http.MethodPut, "/api/v1/orchestrator/cost-settings", bytes.NewBufferString(tc.body)))
			assert.Equal(t, tc.want, w.Code, "body: %s", w.Body.String())
		})
	}
	w := httptest.NewRecorder()
	costSettingsServer(t, false).HandleGetCostSettings(w, systemRequest(http.MethodGet, "/api/v1/orchestrator/cost-settings", nil))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}
