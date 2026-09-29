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
	"strconv"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// tenantSpendServer builds a server whose agent service holds a tenant
// spend-policy store and a ledger.
func tenantSpendServer(t *testing.T, wired bool) *Server {
	t.Helper()
	server, _, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	var agent orchestration.AgentService = &mockAgentService{}
	if wired {
		agent = &agentWithLedger{
			mockAgentService: &mockAgentService{},
			ledger:           orchestration.NewInMemoryConsumptionLedger(),
			tenant:           orchestration.NewInMemoryTenantSpendPolicyStore(),
		}
	}
	server.service = &MockQuicBaaSWithAgent{MockQuicBaaS: testutil.NewMockQuicBaaS(), agentService: agent}
	return server
}

// TestTenantSpendPolicyOverREST is go-quicbaas#1058: PUT sets a tenant's own
// limits and GET reads them back with the usage against each window.
func TestTenantSpendPolicyOverREST(t *testing.T) {
	server := tenantSpendServer(t, true)
	body := `{"limits":{"windows":[{"name":"monthly","size":` +
		jsonNumber(720*time.Hour) + `,"max_equivalent_micros":2000000000}]}}`

	w := httptest.NewRecorder()
	server.HandleSetTenantSpendPolicy(w, httptest.NewRequest(http.MethodPut, "/api/v1/orchestrator/spend-policy", bytes.NewBufferString(body)))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	w = httptest.NewRecorder()
	server.HandleGetTenantSpendPolicy(w, httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/spend-policy", nil))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var report orchestration.TenantSpendPolicyReport
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &report))
	assert.Equal(t, qrdbsdk.SystemTenantID, report.TenantID)
	require.Len(t, report.Limits.Windows, 1)
	assert.Equal(t, int64(2_000_000_000), report.Limits.Windows[0].MaxEquivalentMicros)
	require.Len(t, report.Windows, 1, "the report carries the usage against each window")
	assert.False(t, report.UpdatedAt.IsZero())
}

func TestTenantSpendPolicyOverRESTRefuses(t *testing.T) {
	tests := []struct {
		name  string
		wired bool
		body  string
		code  int
	}{
		{"a body that is not JSON", true, "{bad", http.StatusBadRequest},
		{"a window that meters nothing", true, `{"limits":{"windows":[{"name":"w","size":3600000000000}]}}`, http.StatusBadRequest},
		{"a deployment with no store", false, `{"limits":{}}`, http.StatusServiceUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := tenantSpendServer(t, tc.wired)
			w := httptest.NewRecorder()
			server.HandleSetTenantSpendPolicy(w, httptest.NewRequest(http.MethodPut, "/api/v1/orchestrator/spend-policy", bytes.NewBufferString(tc.body)))
			assert.Equal(t, tc.code, w.Code, "body: %s", w.Body.String())
		})
	}
	server := tenantSpendServer(t, false)
	w := httptest.NewRecorder()
	server.HandleGetTenantSpendPolicy(w, httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/spend-policy", nil))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// jsonNumber renders a duration as the JSON number time.Duration encodes as.
func jsonNumber(d time.Duration) string {
	return strconv.FormatInt(int64(d), 10)
}
