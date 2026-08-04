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

package orchestration

import (
	"encoding/json"
	"testing"
)

// TestInjectMCPServersEnv_AddsJSONEnvVar asserts that a request carrying MCP
// servers gets a QUICBAAS_MCP_SERVERS env var whose JSON round-trips back to
// the original config — including the per-server Env map that carries the
// agent's cert/key paths. This is the contract the spawned agent depends on.
func TestInjectMCPServersEnv_AddsJSONEnvVar(t *testing.T) {
	req := &SpawnRequest{
		MCPServers: []MCPServerConfig{{
			Name: "quicbaas",
			URL:  "https://localhost:8444/mcp",
			Env: map[string]string{
				"QUICBAAS_MCP_CERT": "/data/agent.crt",
				"QUICBAAS_MCP_KEY":  "/data/agent.key",
			},
		}},
	}

	injectMCPServersEnv(req)

	encoded, ok := req.Env[MCPServersEnvVar]
	if !ok {
		t.Fatalf("expected %s to be set", MCPServersEnvVar)
	}
	var got []MCPServerConfig
	if err := json.Unmarshal([]byte(encoded), &got); err != nil {
		t.Fatalf("env value is not valid JSON: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1", len(got))
	}
	if got[0].Name != "quicbaas" || got[0].URL != "https://localhost:8444/mcp" {
		t.Errorf("name/url mismatch: %+v", got[0])
	}
	if got[0].Env["QUICBAAS_MCP_CERT"] != "/data/agent.crt" {
		t.Errorf("cert path dropped: %+v", got[0].Env)
	}
	if got[0].Env["QUICBAAS_MCP_KEY"] != "/data/agent.key" {
		t.Errorf("key path dropped: %+v", got[0].Env)
	}
}

// TestInjectMCPServersEnv_NoServersIsNoOp asserts a request without MCP servers
// gets no extra environment — opting out costs nothing.
func TestInjectMCPServersEnv_NoServersIsNoOp(t *testing.T) {
	req := &SpawnRequest{Env: map[string]string{"EXISTING": "1"}}
	injectMCPServersEnv(req)
	if _, ok := req.Env[MCPServersEnvVar]; ok {
		t.Errorf("%s must not be set when no MCP servers are configured", MCPServersEnvVar)
	}
	if req.Env["EXISTING"] != "1" {
		t.Errorf("existing env must be preserved")
	}
}

// TestInjectMCPServersEnv_AllocatesNilEnv asserts the map is created when the
// request arrived with a nil Env.
func TestInjectMCPServersEnv_AllocatesNilEnv(t *testing.T) {
	req := &SpawnRequest{MCPServers: []MCPServerConfig{{Name: "quicbaas"}}}
	injectMCPServersEnv(req)
	if req.Env == nil {
		t.Fatalf("Env must be allocated")
	}
	if _, ok := req.Env[MCPServersEnvVar]; !ok {
		t.Errorf("%s must be set", MCPServersEnvVar)
	}
}

// TestInjectMCPServersEnv_RespectsExplicitEnv asserts an operator-supplied
// QUICBAAS_MCP_SERVERS value is never silently overwritten by the structured
// field — explicit env wins.
func TestInjectMCPServersEnv_RespectsExplicitEnv(t *testing.T) {
	req := &SpawnRequest{
		Env:        map[string]string{MCPServersEnvVar: "[]"},
		MCPServers: []MCPServerConfig{{Name: "quicbaas"}},
	}
	injectMCPServersEnv(req)
	if req.Env[MCPServersEnvVar] != "[]" {
		t.Errorf("explicit env must win, got %q", req.Env[MCPServersEnvVar])
	}
}

// TestInjectMCPServersEnv_NilRequestSafe asserts the helper is total — a nil
// request must not panic (keeps the spawn entry path safe).
func TestInjectMCPServersEnv_NilRequestSafe(t *testing.T) {
	injectMCPServersEnv(nil) // must not panic
}
