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
	"reflect"
	"testing"

	healthclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/health"
)

// assertNoHealthArgs checks the health tools are called with no arguments.
func assertNoHealthArgs(t *testing.T, args map[string]interface{}) {
	t.Helper()
	if len(args) != 0 {
		t.Errorf("health tool args = %v, want none", args)
	}
}

// The server's health responses carry no JSON tags, so the keys are the Go
// field names.
func TestHealthMCP_Readiness_Ready(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:   "quicbaas_readiness",
		assertArgs: assertNoHealthArgs,
		result:     map[string]interface{}{"Ready": true},
	})
	defer cleanup()

	got, err := healthclient.NewClient(tr).Readiness(context.Background())
	if err != nil {
		t.Fatalf("Readiness: %v", err)
	}
	want := &healthclient.HealthResponse{
		Healthy: true,
		Status:  "ready",
		Details: map[string]interface{}{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("readiness = %+v, want %+v", got, want)
	}
}

func TestHealthMCP_Readiness_NotReady(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_readiness",
		result:   map[string]interface{}{"Ready": false, "Reason": "raft has no leader"},
	})
	defer cleanup()

	got, err := healthclient.NewClient(tr).Readiness(context.Background())
	if err != nil {
		t.Fatalf("Readiness: %v", err)
	}
	want := &healthclient.HealthResponse{
		Healthy: false,
		Status:  "not ready",
		Details: map[string]interface{}{"reason": "raft has no leader"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("readiness = %+v, want %+v", got, want)
	}
}

func TestHealthMCP_Liveness(t *testing.T) {
	for _, alive := range []bool{true, false} {
		tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
			wantTool:   "quicbaas_liveness",
			assertArgs: assertNoHealthArgs,
			result:     map[string]interface{}{"Alive": alive},
		})

		got, err := healthclient.NewClient(tr).Liveness(context.Background())
		cleanup()
		if err != nil {
			t.Fatalf("Liveness(alive=%v): %v", alive, err)
		}
		status := "dead"
		if alive {
			status = "alive"
		}
		want := &healthclient.HealthResponse{Healthy: alive, Status: status}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("liveness = %+v, want %+v", got, want)
		}
	}
}

// A result without its probe field, or with a non-bool one, is an error and
// never reads as an unhealthy server.
func TestHealthMCP_MissingProbeField(t *testing.T) {
	cases := []struct {
		name   string
		tool   string
		result map[string]interface{}
		call   func(healthclient.Client) (*healthclient.HealthResponse, error)
	}{
		{"readiness missing", "quicbaas_readiness", map[string]interface{}{"Reason": "x"},
			func(c healthclient.Client) (*healthclient.HealthResponse, error) {
				return c.Readiness(context.Background())
			}},
		{"readiness lowercase key", "quicbaas_readiness", map[string]interface{}{"ready": true},
			func(c healthclient.Client) (*healthclient.HealthResponse, error) {
				return c.Readiness(context.Background())
			}},
		{"readiness not bool", "quicbaas_readiness", map[string]interface{}{"Ready": "true"},
			func(c healthclient.Client) (*healthclient.HealthResponse, error) {
				return c.Readiness(context.Background())
			}},
		{"liveness missing", "quicbaas_liveness", map[string]interface{}{},
			func(c healthclient.Client) (*healthclient.HealthResponse, error) {
				return c.Liveness(context.Background())
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{wantTool: tc.tool, result: tc.result})
			defer cleanup()
			if got, err := tc.call(healthclient.NewClient(tr)); err == nil {
				t.Errorf("no error for result %v, got %+v", tc.result, got)
			}
		})
	}
}

func TestHealthMCP_NonJSONResult(t *testing.T) {
	for _, tool := range []string{"quicbaas_readiness", "quicbaas_liveness"} {
		t.Run(tool, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{wantTool: tool, rawText: "not json"})
			defer cleanup()
			c := healthclient.NewClient(tr)
			var err error
			if tool == "quicbaas_readiness" {
				_, err = c.Readiness(context.Background())
			} else {
				_, err = c.Liveness(context.Background())
			}
			if err == nil {
				t.Error("no error for a non-JSON tool result")
			}
		})
	}
}

func TestHealthMCP_ToolError(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:  "quicbaas_readiness",
		result:    map[string]interface{}{"error": "unavailable"},
		toolError: true,
	})
	defer cleanup()

	if _, err := healthclient.NewClient(tr).Readiness(context.Background()); err == nil {
		t.Error("Readiness returned no error for a failing tool")
	}
}

func TestHealthMCP_ToolNames(t *testing.T) {
	want := map[string]string{
		"Liveness":  "quicbaas_liveness",
		"Readiness": "quicbaas_readiness",
	}
	for _, service := range []string{healthServiceName, "Health"} {
		for method, tool := range want {
			if got := ToolName(service, method); got != tool {
				t.Errorf("ToolName(%q, %q) = %q, want %q", service, method, got, tool)
			}
		}
	}
	if !isMCPHealthService(healthServiceName) || !isMCPHealthService("Health") {
		t.Error("isMCPHealthService refuses a health service name")
	}
	if isMCPHealthService("quicbaas.Ollama") {
		t.Error("isMCPHealthService accepts a non-health service name")
	}
}
