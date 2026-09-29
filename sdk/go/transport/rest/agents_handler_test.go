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
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
)

// TestAgentsREST_Routes pins the method, route, query and body every Agents
// verb sends, and the answer type it decodes.
func TestAgentsREST_Routes(t *testing.T) {
	type call struct{ method, path, query, body string }
	calls := make(chan call, 1)
	answer := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		calls <- call{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, string(body)}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(<-answer)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()
	tr := newTestTransport(t, server)

	agentJSON := `{"id":"codex-reviewer","backend":"codex","model":"gpt-5.5"}`
	poolJSON := `{"id":"reviewers","agents":["codex-reviewer"]}`
	usedJSON := `{"used_by":[{"kind":"binding","id":"proj-1","slot":"worker"}]}`
	cases := []struct {
		verb   string
		req    interface{}
		answer string
		want   call
		check  func(interface{}) bool
	}{
		{"ListAgents", &agents.ListAgentsRequest{TenantID: "acme"}, `{"agents":[` + agentJSON + `]}`,
			call{http.MethodGet, "/api/v1/agents", "tenant_id=acme", ""},
			func(r interface{}) bool { return len(r.(*agents.AgentList).Agents) == 1 }},
		{"GetAgent", &agents.AgentRequest{AgentID: "a b"}, agentJSON,
			call{http.MethodGet, "/api/v1/agents/a%20b", "", ""},
			func(r interface{}) bool { return r.(*agents.Agent).Model == "gpt-5.5" }},
		{"CreateAgent", &agents.SaveAgentRequest{Agent: &agents.Agent{ID: "codex-reviewer"}}, agentJSON,
			call{http.MethodPost, "/api/v1/agents", "", `"id":"codex-reviewer"`},
			func(r interface{}) bool { return r.(*agents.Agent).ID == "codex-reviewer" }},
		{"UpdateAgent", &agents.SaveAgentRequest{TenantID: "acme", AgentID: "codex-reviewer", Agent: &agents.Agent{Model: "gpt-5.5"}}, agentJSON,
			call{http.MethodPut, "/api/v1/agents/codex-reviewer", "tenant_id=acme", `"model":"gpt-5.5"`},
			func(r interface{}) bool { return r.(*agents.Agent).Backend == "codex" }},
		{"DeleteAgent", &agents.AgentRequest{AgentID: "codex-reviewer"}, `{"deleted":true}`,
			call{http.MethodDelete, "/api/v1/agents/codex-reviewer", "", ""},
			func(r interface{}) bool { return r.(*agents.Deleted).Deleted }},
		{"AgentUsedBy", &agents.AgentRequest{AgentID: "codex-reviewer"}, usedJSON,
			call{http.MethodGet, "/api/v1/agents/codex-reviewer/used-by", "", ""},
			func(r interface{}) bool { return r.(*agents.UsageList).UsedBy[0].Slot == "worker" }},
		{"ListPools", &agents.ListPoolsRequest{}, `{"pools":[` + poolJSON + `]}`,
			call{http.MethodGet, "/api/v1/agent-pools", "", ""},
			func(r interface{}) bool { return len(r.(*agents.PoolList).Pools) == 1 }},
		{"GetPool", &agents.PoolRequest{PoolID: "reviewers"}, poolJSON,
			call{http.MethodGet, "/api/v1/agent-pools/reviewers", "", ""},
			func(r interface{}) bool { return r.(*agents.Pool).ID == "reviewers" }},
		{"CreatePool", &agents.SavePoolRequest{Pool: &agents.Pool{ID: "reviewers"}}, poolJSON,
			call{http.MethodPost, "/api/v1/agent-pools", "", `"id":"reviewers"`},
			func(r interface{}) bool { return r.(*agents.Pool).Agents[0] == "codex-reviewer" }},
		{"UpdatePool", &agents.SavePoolRequest{PoolID: "reviewers", Pool: &agents.Pool{Agents: []string{"x"}}}, poolJSON,
			call{http.MethodPut, "/api/v1/agent-pools/reviewers", "", `"agents":["x"]`},
			func(r interface{}) bool { return r.(*agents.Pool).ID == "reviewers" }},
		{"DeletePool", &agents.PoolRequest{PoolID: "reviewers"}, `{"deleted":true}`,
			call{http.MethodDelete, "/api/v1/agent-pools/reviewers", "", ""},
			func(r interface{}) bool { return r.(*agents.Deleted).Deleted }},
		{"PoolUsedBy", &agents.PoolRequest{PoolID: "reviewers"}, usedJSON,
			call{http.MethodGet, "/api/v1/agent-pools/reviewers/used-by", "", ""},
			func(r interface{}) bool { return r.(*agents.UsageList).UsedBy[0].ID == "proj-1" }},
	}
	for _, tc := range cases {
		t.Run(tc.verb, func(t *testing.T) {
			answer <- tc.answer
			resp, err := tr.Request(context.Background(), agents.ServiceName, tc.verb, tc.req)
			if err != nil {
				t.Fatalf("%s: %v", tc.verb, err)
			}
			got := <-calls
			if got.method != tc.want.method || got.path != tc.want.path || got.query != tc.want.query || !strings.Contains(got.body, tc.want.body) {
				t.Fatalf("%s sent %+v, want %+v", tc.verb, got, tc.want)
			}
			if !tc.check(resp) {
				t.Fatalf("%s answer = %+v", tc.verb, resp)
			}
		})
	}
}

func TestAgentsREST_RefusesARequestItCannotRoute(t *testing.T) {
	tr := &Transport{}
	if !tr.SupportsAgents() || !isAgentsService(agents.ServiceName) || isAgentsService("quicbaas.Fleet") {
		t.Fatal("service declaration")
	}
	if _, err := tr.handleAgentsRequest(context.Background(), "Rename", &agents.AgentRequest{}); err == nil {
		t.Fatal("an unknown verb must be refused")
	}
	for verb, route := range agentsRoutes {
		if _, err := route(&struct{}{}); err == nil {
			t.Fatalf("%s accepted a request of the wrong type", verb)
		}
		if _, err := route(nil); err == nil {
			t.Fatalf("%s accepted no request", verb)
		}
	}
	var nilAgent *agents.AgentRequest
	if _, err := agentsRoutes["GetAgent"](nilAgent); err == nil {
		t.Fatal("a typed nil request must be refused before it is read")
	}
}

func TestAgentsREST_SurfacesARefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		if _, err := w.Write([]byte(`{"error":"agent codex-reviewer is in use"}`)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	if _, err := tr.Request(context.Background(), agents.ServiceName, "DeleteAgent", &agents.AgentRequest{AgentID: "codex-reviewer"}); err == nil {
		t.Fatal("a 409 must surface as an error")
	}
}
