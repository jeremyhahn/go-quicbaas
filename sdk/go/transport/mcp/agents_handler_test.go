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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
)

// TestAgentsMCP_EveryVerbCallsItsTool drives each Agents verb through a real
// in-process MCP server registering the tool the server exposes, and checks
// the arguments it sends and the answer it decodes.
func TestAgentsMCP_EveryVerbCallsItsTool(t *testing.T) {
	agent := map[string]interface{}{"id": "codex-reviewer", "backend": "codex", "model": "gpt-5.5", "roles": []interface{}{"review"}}
	pool := map[string]interface{}{"id": "reviewers", "agents": []interface{}{"codex-reviewer", "qwen-reviewer"}}
	usedBy := map[string]interface{}{"used_by": []interface{}{map[string]interface{}{"kind": "binding", "id": "proj-1", "slot": "reviewer:default"}}}
	deleted := map[string]interface{}{"deleted": true}

	cases := []struct {
		method string
		tool   string
		req    interface{}
		arg    string // an argument the tool must receive
		result map[string]interface{}
		check  func(interface{}) bool
	}{
		{"ListAgents", "quicbaas_agents_list", &agents.ListAgentsRequest{TenantID: "acme"}, "tenant_id",
			map[string]interface{}{"agents": []interface{}{agent}},
			func(r interface{}) bool { return len(r.(*agents.AgentList).Agents) == 1 }},
		{"GetAgent", "quicbaas_agents_get", &agents.AgentRequest{AgentID: "codex-reviewer"}, "agent_id", agent,
			func(r interface{}) bool { return r.(*agents.Agent).Model == "gpt-5.5" }},
		{"CreateAgent", "quicbaas_agents_create", &agents.SaveAgentRequest{Agent: &agents.Agent{ID: "codex-reviewer"}}, "agent", agent,
			func(r interface{}) bool { return r.(*agents.Agent).ID == "codex-reviewer" }},
		{"UpdateAgent", "quicbaas_agents_update", &agents.SaveAgentRequest{AgentID: "codex-reviewer", Agent: &agents.Agent{}}, "agent_id", agent,
			func(r interface{}) bool { return r.(*agents.Agent).Backend == "codex" }},
		{"DeleteAgent", "quicbaas_agents_delete", &agents.AgentRequest{AgentID: "codex-reviewer"}, "agent_id", deleted,
			func(r interface{}) bool { return r.(*agents.Deleted).Deleted }},
		{"AgentUsedBy", "quicbaas_agents_used_by", &agents.AgentRequest{AgentID: "codex-reviewer"}, "agent_id", usedBy,
			func(r interface{}) bool { return r.(*agents.UsageList).UsedBy[0].Slot == "reviewer:default" }},
		{"ListPools", "quicbaas_agent_pools_list", &agents.ListPoolsRequest{TenantID: "acme"}, "tenant_id",
			map[string]interface{}{"pools": []interface{}{pool}},
			func(r interface{}) bool { return len(r.(*agents.PoolList).Pools) == 1 }},
		{"GetPool", "quicbaas_agent_pools_get", &agents.PoolRequest{PoolID: "reviewers"}, "pool_id", pool,
			func(r interface{}) bool { return len(r.(*agents.Pool).Agents) == 2 }},
		{"CreatePool", "quicbaas_agent_pools_create", &agents.SavePoolRequest{Pool: &agents.Pool{ID: "reviewers"}}, "pool", pool,
			func(r interface{}) bool { return r.(*agents.Pool).ID == "reviewers" }},
		{"UpdatePool", "quicbaas_agent_pools_update", &agents.SavePoolRequest{PoolID: "reviewers", Pool: &agents.Pool{}}, "pool_id", pool,
			func(r interface{}) bool { return r.(*agents.Pool).Agents[1] == "qwen-reviewer" }},
		{"DeletePool", "quicbaas_agent_pools_delete", &agents.PoolRequest{PoolID: "reviewers"}, "pool_id", deleted,
			func(r interface{}) bool { return r.(*agents.Deleted).Deleted }},
		{"PoolUsedBy", "quicbaas_agent_pools_used_by", &agents.PoolRequest{PoolID: "reviewers"}, "pool_id", usedBy,
			func(r interface{}) bool { return r.(*agents.UsageList).UsedBy[0].ID == "proj-1" }},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			if name, ok := agentsToolName(tc.method); !ok || name != tc.tool {
				t.Fatalf("agentsToolName(%s) = %q", tc.method, name)
			}
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool: tc.tool,
				assertArgs: func(t *testing.T, args map[string]interface{}) {
					if _, ok := args[tc.arg]; !ok {
						t.Errorf("%s arguments %v lack %q", tc.tool, args, tc.arg)
					}
				},
				result: tc.result,
			})
			defer cleanup()
			resp, err := tr.Request(context.Background(), agents.ServiceName, tc.method, tc.req)
			if err != nil {
				t.Fatalf("%s: %v", tc.method, err)
			}
			if !tc.check(resp) {
				t.Fatalf("%s answer = %+v", tc.method, resp)
			}
		})
	}
}

func TestAgentsMCP_RefusesWhatItCannotCarry(t *testing.T) {
	if _, ok := agentsToolName("Rename"); ok {
		t.Fatal("an unknown verb has no tool")
	}
	tr := &Transport{}
	if _, err := tr.requestAgents(context.Background(), "Rename", nil); err == nil {
		t.Fatal("an unknown verb must be refused")
	}
	if !tr.SupportsAgents() || !isMCPAgentsService(agents.ServiceName) || isMCPAgentsService("quicbaas.Fleet") {
		t.Fatal("service declaration")
	}

	tool, cleanup := newFakeMCPServer(t, mcpToolExpectation{wantTool: "quicbaas_agents_get", rawText: `{"model": 7}`})
	defer cleanup()
	if _, err := tool.Request(context.Background(), agents.ServiceName, "GetAgent", &agents.AgentRequest{AgentID: "a"}); err == nil {
		t.Fatal("an answer of the wrong shape must be a failure")
	}

	refused, cleanupRefused := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_agents_delete", toolError: true,
		result: map[string]interface{}{"error": "agent codex-reviewer is in use by binding proj-1 (worker)"},
	})
	defer cleanupRefused()
	if _, err := refused.Request(context.Background(), agents.ServiceName, "DeleteAgent", &agents.AgentRequest{AgentID: "codex-reviewer"}); err == nil {
		t.Fatal("a tool error must surface")
	}
}
