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
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/prompt"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

const promptServiceName = "quicbaas.Prompts"

var promptRecordResult = map[string]interface{}{
	"set": "s", "name": "n", "version": 2, "content": "body", "follow_parent": false,
	"updated_at": "2026-10-01T12:00:00Z",
}

// TestPromptMCP_RoutesEveryVerb drives each verb through the SDK client to
// its prompt_* tool, asserting the arguments the tool receives and the typed
// answer the client gets back.
func TestPromptMCP_RoutesEveryVerb(t *testing.T) {
	addr := prompt.Address{TenantID: "t1", Scope: "project", ProjectID: "p1", Set: "s", Name: "n"}
	addrArgs := map[string]interface{}{"tenant_id": "t1", "scope": "project", "project_id": "p1", "set": "s", "name": "n"}
	with := func(extra map[string]interface{}) map[string]interface{} {
		out := map[string]interface{}{}
		for k, v := range addrArgs {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	cases := []struct {
		tool     string
		call     func(prompt.Client) (interface{}, error)
		wantArgs map[string]interface{}
		result   map[string]interface{}
		check    func(t *testing.T, got interface{})
	}{
		{"prompt_list",
			func(c prompt.Client) (interface{}, error) {
				return c.List(context.Background(), &prompt.ListRequest{TenantID: "t1", Set: "s"})
			},
			map[string]interface{}{"tenant_id": "t1", "set": "s"},
			map[string]interface{}{"set": "s", "scope": "tenant", "prompts": []interface{}{map[string]interface{}{"name": "n", "overridden": true}}},
			func(t *testing.T, got interface{}) {
				if list := got.(*prompt.ListResponse); len(list.Prompts) != 1 || !list.Prompts[0].Overridden {
					t.Errorf("list = %+v", list)
				}
			}},
		{"prompt_get",
			func(c prompt.Client) (interface{}, error) {
				return c.Get(context.Background(), &prompt.GetRequest{Address: addr, Version: 1})
			},
			with(map[string]interface{}{"version": float64(1)}),
			promptRecordResult,
			func(t *testing.T, got interface{}) {
				record := got.(*prompt.Record)
				if record.Version != 2 || record.Content != "body" || !record.UpdatedAt.Equal(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)) {
					t.Errorf("record = %+v", record)
				}
			}},
		{"prompt_effective",
			func(c prompt.Client) (interface{}, error) { return c.Effective(context.Background(), &addr) },
			addrArgs,
			map[string]interface{}{"set": "s", "name": "n", "content": "body", "origin": map[string]interface{}{"scope": "tenant", "version": 2}},
			func(t *testing.T, got interface{}) {
				if eff := got.(*prompt.EffectivePrompt); eff.Content != "body" || eff.Origin.Version != 2 {
					t.Errorf("effective = %+v", eff)
				}
			}},
		{"prompt_diff",
			func(c prompt.Client) (interface{}, error) { return c.Diff(context.Background(), &addr) },
			addrArgs,
			map[string]interface{}{"set": "s", "name": "n", "lines": []interface{}{map[string]interface{}{"op": "insert", "text": "x"}}},
			func(t *testing.T, got interface{}) {
				if diff := got.(*prompt.Diff); len(diff.Lines) != 1 || diff.Lines[0].Op != prompt.DiffInsert {
					t.Errorf("diff = %+v", diff)
				}
			}},
		{"prompt_history",
			func(c prompt.Client) (interface{}, error) { return c.History(context.Background(), &addr) },
			addrArgs,
			map[string]interface{}{"set": "s", "name": "n", "revisions": []interface{}{map[string]interface{}{"version": 1, "author": "alice"}}},
			func(t *testing.T, got interface{}) {
				if h := got.(*prompt.History); len(h.Revisions) != 1 || h.Revisions[0].Author != "alice" {
					t.Errorf("history = %+v", h)
				}
			}},
		{"prompt_put",
			func(c prompt.Client) (interface{}, error) {
				return c.Put(context.Background(), &prompt.PutRequest{Address: addr, Content: "body", Note: "why", ExpectedVersion: 1})
			},
			with(map[string]interface{}{"content": "body", "note": "why", "expected_version": float64(1)}),
			promptRecordResult,
			func(t *testing.T, got interface{}) {
				if got.(*prompt.Record).Version != 2 {
					t.Errorf("put = %+v", got)
				}
			}},
		{"prompt_rollback",
			func(c prompt.Client) (interface{}, error) {
				return c.Rollback(context.Background(), &prompt.RollbackRequest{Address: addr, ToVersion: 1, Note: "why", ExpectedVersion: 2})
			},
			with(map[string]interface{}{"to_version": float64(1), "note": "why", "expected_version": float64(2)}),
			promptRecordResult,
			func(t *testing.T, got interface{}) {
				if got.(*prompt.Record).Version != 2 {
					t.Errorf("rollback = %+v", got)
				}
			}},
		{"prompt_reset",
			func(c prompt.Client) (interface{}, error) {
				return c.Reset(context.Background(), &prompt.ResetRequest{Address: addr, ExpectedVersion: 3})
			},
			with(map[string]interface{}{"expected_version": float64(3)}),
			promptRecordResult,
			func(t *testing.T, got interface{}) {
				if got.(*prompt.Record).Set != "s" {
					t.Errorf("reset = %+v", got)
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool: tc.tool,
				assertArgs: func(t *testing.T, args map[string]interface{}) {
					if !reflect.DeepEqual(args, tc.wantArgs) {
						t.Errorf("tool arguments = %v, want %v", args, tc.wantArgs)
					}
				},
				result: tc.result,
			})
			defer cleanup()
			got, err := tc.call(prompt.NewClient(tr))
			if err != nil {
				t.Fatalf("%s: %v", tc.tool, err)
			}
			tc.check(t, got)
		})
	}
}

// TestPromptMCP_RefusesBadRequests proves a wrong request type and an
// unknown method are refused before any tool is called.
func TestPromptMCP_RefusesBadRequests(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "prompt_get",
		assertArgs: func(t *testing.T, _ map[string]interface{}) {
			t.Error("a refused request must not reach the tool")
		},
	})
	defer cleanup()
	ctx := context.Background()
	for method := range promptVerbs {
		if _, err := tr.Request(ctx, promptServiceName, method, "wrong"); !sdkerrors.IsInvalidInput(err) {
			t.Errorf("%s with a wrong request type: %v", method, err)
		}
	}
	var nilAddr *prompt.Address
	if _, err := tr.Request(ctx, promptServiceName, "Diff", nilAddr); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("nil request: %v", err)
	}
	if _, err := tr.Request(ctx, promptServiceName, "Delete", &prompt.Address{}); !sdkerrors.IsUnimplemented(err) {
		t.Errorf("unknown method: %v", err)
	}
}

// TestPromptMCP_MapsToolRefusals proves a coded tool error keeps its class
// and a result that is not the answer shape is an error, not a zero value.
func TestPromptMCP_MapsToolRefusals(t *testing.T) {
	get := &prompt.GetRequest{Address: prompt.Address{Set: "s", Name: "n"}}
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:      "prompt_get",
		result:        map[string]interface{}{"error": "nothing overridden"},
		toolError:     true,
		toolErrorCode: MCPResourceNotFound,
	})
	defer cleanup()
	if _, err := prompt.NewClient(tr).Get(context.Background(), get); !sdkerrors.IsNotFound(err) {
		t.Errorf("not found tool error: %v", err)
	}

	malformed, cleanupMalformed := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "prompt_get",
		result:   map[string]interface{}{"version": "two"},
	})
	defer cleanupMalformed()
	if _, err := prompt.NewClient(malformed).Get(context.Background(), get); err == nil {
		t.Error("a malformed result must be an error")
	}
}

// TestPromptMCP_CarriesEveryVerb, in both directions: every SDK verb has a
// tool, every tool is reachable from the SDK, and ToolName reports the
// prompt_* name the server registers.
func TestPromptMCP_CarriesEveryVerb(t *testing.T) {
	iface := reflect.TypeOf((*prompt.Client)(nil)).Elem()
	declared := make(map[string]bool, iface.NumMethod())
	for i := 0; i < iface.NumMethod(); i++ {
		verb := iface.Method(i).Name
		declared[verb] = true
		if _, ok := promptVerbs[verb]; !ok {
			t.Errorf("the MCP transport does not carry prompt.%s", verb)
		}
	}
	for method, verb := range promptVerbs {
		if !declared[method] {
			t.Errorf("the MCP transport carries %q, which the SDK interface does not declare", method)
		}
		if got := ToolName(promptServiceName, method); got != verb.tool {
			t.Errorf("ToolName(%q) = %q, want %q", method, got, verb.tool)
		}
	}
	if !isMCPPromptsService(promptServiceName) || isMCPPromptsService("quicbaas.SDLC") {
		t.Error("isMCPPromptsService misroutes")
	}
	if !(&Transport{}).SupportsPrompts() {
		t.Fatal("the MCP transport must declare the Prompts service")
	}
}
