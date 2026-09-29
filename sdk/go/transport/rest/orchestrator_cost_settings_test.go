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
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

const costSettingsBody = `{"settings":{"tenant_id":"t1","electricity_price_per_kwh":0.2,
	"engine_power_watts":{"openvino":12},"updated_at":"2026-09-23T06:00:00Z"},
	"electricity":{"price_per_kwh":0.2,"source":"tenant_settings"},
	"engine_power_watts":{"openvino":12,"ollama":250}}`

// TestOrchestratorCostSettings_Routes pins the routes and bodies the SDK sends
// for a tenant's cost settings (go-quicbaas#1058).
func TestOrchestratorCostSettings_Routes(t *testing.T) {
	type call struct{ method, path, query, body string }
	calls := make(chan call, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		calls <- call{r.Method, r.URL.Path, r.URL.RawQuery, string(body)}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(costSettingsBody)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	ctx := context.Background()

	resp, err := tr.Request(ctx, "Orchestrator", "GetCostSettings", &orchestrator.GetCostSettingsRequest{TenantID: "t1"})
	if err != nil {
		t.Fatalf("GetCostSettings: %v", err)
	}
	got := <-calls
	if got.method != http.MethodGet || got.path != "/api/v1/orchestrator/cost-settings" || got.query != "tenant_id=t1" {
		t.Fatalf("get = %+v", got)
	}
	settings, ok := resp.(*orchestrator.CostSettings)
	if !ok || settings.Electricity.PricePerKWh != 0.2 || settings.EngineDraws["ollama"] != 250 ||
		settings.Settings.UpdatedAt == nil || *settings.Settings.ElectricityPricePerKWh != 0.2 {
		t.Fatalf("settings = %+v", resp)
	}

	rate := 0.2
	if _, err := tr.Request(ctx, "Orchestrator", "SetCostSettings", &orchestrator.SetCostSettingsRequest{
		ElectricityPricePerKWh: &rate, EnginePowerWatts: map[string]float64{"openvino": 12},
	}); err != nil {
		t.Fatalf("SetCostSettings: %v", err)
	}
	got = <-calls
	if got.method != http.MethodPut || got.path != "/api/v1/orchestrator/cost-settings" || got.query != "" ||
		got.body != `{"electricity_price_per_kwh":0.2,"engine_power_watts":{"openvino":12}}` {
		t.Fatalf("set = %+v", got)
	}

	if _, err := tr.Request(ctx, "Orchestrator", "SetCostSettings", &orchestrator.GetCostSettingsRequest{}); err == nil {
		t.Fatal("a request of the wrong type must be refused")
	}
	if _, err := tr.Request(ctx, "Orchestrator", "GetCostSettings", &orchestrator.SetCostSettingsRequest{}); err == nil {
		t.Fatal("a request of the wrong type must be refused")
	}
}
