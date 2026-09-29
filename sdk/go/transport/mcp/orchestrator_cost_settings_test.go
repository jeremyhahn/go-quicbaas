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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestCostSettingsToolNames pins the SDK methods to the tools the server
// registers.
func TestCostSettingsToolNames(t *testing.T) {
	tr := &Transport{}
	for method, want := range map[string]string{
		"GetCostSettings": "quicbaas_orchestrator_cost_settings_get",
		"SetCostSettings": "quicbaas_orchestrator_cost_settings_set",
	} {
		if got := tr.mapToToolName("quicbaas.Orchestrator", method); got != want {
			t.Fatalf("mapToToolName(%s) = %q, want %q", method, got, want)
		}
	}
}

// TestDecodeMCPCostSettings decodes the report the server's tools marshal.
func TestDecodeMCPCostSettings(t *testing.T) {
	got, err := decodeMCPCostSettings([]byte(`{"settings":{"tenant_id":"t1"},
		"electricity":{"price_per_kwh":0.12,"source":"default"},"engine_power_watts":{"ollama":250}}`))
	if err != nil {
		t.Fatalf("decodeMCPCostSettings: %v", err)
	}
	settings, ok := got.(*orchestrator.CostSettings)
	if !ok || settings.Electricity.PricePerKWh != 0.12 || settings.EngineDraws["ollama"] != 250 {
		t.Fatalf("settings = %+v", got)
	}
	if _, err := decodeMCPCostSettings([]byte(`{`)); err == nil {
		t.Fatal("an undecodable result must be a failure, not empty settings")
	}
}
