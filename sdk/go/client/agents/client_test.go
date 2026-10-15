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

package agents

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// fakeTransport records the one request it carried and answers with resp.
type fakeTransport struct {
	capable bool
	service string
	method  string
	req     interface{}
	resp    interface{}
	err     error
}

func (f *fakeTransport) Request(_ context.Context, service, method string, req interface{}) (interface{}, error) {
	f.service, f.method, f.req = service, method, req
	return f.resp, f.err
}

func (f *fakeTransport) RequestStream(context.Context, string, string, interface{}) (transport.Stream, error) {
	return nil, nil
}
func (f *fakeTransport) Conn() interface{}    { return nil }
func (f *fakeTransport) Close() error         { return nil }
func (f *fakeTransport) Healthy() bool        { return true }
func (f *fakeTransport) SupportsAgents() bool { return f.capable }

// plainTransport carries no Agents service.
type plainTransport struct{ fakeTransport }

func (plainTransport) SupportsAgents() bool { return false }

func TestAgentRef_ParseAndRender(t *testing.T) {
	cases := map[string]AgentRef{
		"":              {},
		"claude-coder":  {AgentID: "claude-coder"},
		"agent:codex":   {AgentID: "codex"},
		" pool: coders": {PoolID: "coders"},
	}
	for text, want := range cases {
		got, err := ParseAgentRef(text)
		if err != nil || got != want {
			t.Fatalf("ParseAgentRef(%q) = %+v, %v; want %+v", text, got, err, want)
		}
	}
	if _, err := ParseAgentRef("fleet:node-1"); err == nil {
		t.Fatal("an unknown kind must be refused")
	}
	if (AgentRef{PoolID: "p"}).String() != "pool:p" || (AgentRef{AgentID: "a"}).String() != "agent:a" || (AgentRef{}).String() != "" {
		t.Fatal("String")
	}
	if !(AgentRef{AgentID: "  "}).IsZero() || (AgentRef{PoolID: "p"}).IsZero() {
		t.Fatal("IsZero")
	}
}

// TestClient_EveryVerbCarriesItsRequest proves each verb names the right
// method and request shape, and unwraps the answer.
func TestClient_EveryVerbCarriesItsRequest(t *testing.T) {
	ctx := context.Background()
	agent := &Agent{ID: "a", Backend: "codex", Model: "gpt-5.5"}
	pool := &Pool{ID: "p", Agents: []string{"a"}}
	usages := &UsageList{UsedBy: []Usage{{Kind: "binding", ID: "proj", Slot: "worker"}}}

	cases := []struct {
		method string
		resp   interface{}
		call   func(Client) error
		check  func(req interface{}) bool
	}{
		{"ListAgents", &AgentList{Agents: []*Agent{agent}}, func(c Client) error {
			list, err := c.ListAgents(ctx, "acme")
			if err == nil && len(list) != 1 {
				t.Fatal("list not unwrapped")
			}
			return err
		}, func(req interface{}) bool { return req.(*ListAgentsRequest).TenantID == "acme" }},
		{"GetAgent", agent, func(c Client) error { _, err := c.GetAgent(ctx, "acme", "a"); return err },
			func(req interface{}) bool { return req.(*AgentRequest).AgentID == "a" }},
		{"CreateAgent", agent, func(c Client) error { _, err := c.CreateAgent(ctx, "acme", agent); return err },
			func(req interface{}) bool { r := req.(*SaveAgentRequest); return r.AgentID == "" && r.Agent == agent }},
		{"UpdateAgent", agent, func(c Client) error { _, err := c.UpdateAgent(ctx, "acme", "a", agent); return err },
			func(req interface{}) bool { return req.(*SaveAgentRequest).AgentID == "a" }},
		{"DeleteAgent", &Deleted{Deleted: true}, func(c Client) error { return c.DeleteAgent(ctx, "acme", "a") },
			func(req interface{}) bool { return req.(*AgentRequest).AgentID == "a" }},
		{"AgentUsedBy", usages, func(c Client) error {
			got, err := c.AgentUsedBy(ctx, "acme", "a")
			if err == nil && len(got) != 1 {
				t.Fatal("usages not unwrapped")
			}
			return err
		}, func(req interface{}) bool { return req.(*AgentRequest).AgentID == "a" }},
		{"ListPools", &PoolList{Pools: []*Pool{pool}}, func(c Client) error { _, err := c.ListPools(ctx, "acme"); return err },
			func(req interface{}) bool { return req.(*ListPoolsRequest).TenantID == "acme" }},
		{"GetPool", pool, func(c Client) error { _, err := c.GetPool(ctx, "acme", "p"); return err },
			func(req interface{}) bool { return req.(*PoolRequest).PoolID == "p" }},
		{"CreatePool", pool, func(c Client) error { _, err := c.CreatePool(ctx, "acme", pool); return err },
			func(req interface{}) bool { return req.(*SavePoolRequest).Pool == pool }},
		{"UpdatePool", pool, func(c Client) error { _, err := c.UpdatePool(ctx, "acme", "p", pool); return err },
			func(req interface{}) bool { return req.(*SavePoolRequest).PoolID == "p" }},
		{"DeletePool", &Deleted{Deleted: true}, func(c Client) error { return c.DeletePool(ctx, "acme", "p") },
			func(req interface{}) bool { return req.(*PoolRequest).PoolID == "p" }},
		{"PoolUsedBy", usages, func(c Client) error { _, err := c.PoolUsedBy(ctx, "acme", "p"); return err },
			func(req interface{}) bool { return req.(*PoolRequest).PoolID == "p" }},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			fake := &fakeTransport{capable: true, resp: tc.resp}
			if err := tc.call(NewClient(fake)); err != nil {
				t.Fatalf("%s: %v", tc.method, err)
			}
			if fake.service != ServiceName || fake.method != tc.method || !tc.check(fake.req) {
				t.Fatalf("carried %s.%s %+v", fake.service, fake.method, fake.req)
			}
			// A response of the wrong type is an internal error, not a panic.
			wrong := &fakeTransport{capable: true, resp: struct{}{}}
			if err := tc.call(NewClient(wrong)); err == nil {
				t.Fatalf("%s accepted a mistyped response", tc.method)
			}
		})
	}
}

func TestClient_RefusesBadInputBeforeTheWire(t *testing.T) {
	ctx := context.Background()
	fake := &fakeTransport{capable: true}
	c := NewClient(fake)
	var nilCtx context.Context
	refusals := map[string]error{
		"get without id":        func() error { _, err := c.GetAgent(ctx, "t", " "); return err }(),
		"create nil":            func() error { _, err := c.CreateAgent(ctx, "t", nil); return err }(),
		"update without id":     func() error { _, err := c.UpdateAgent(ctx, "t", "", &Agent{}); return err }(),
		"update nil":            func() error { _, err := c.UpdateAgent(ctx, "t", "a", nil); return err }(),
		"delete without id":     c.DeleteAgent(ctx, "t", ""),
		"used-by without id":    func() error { _, err := c.AgentUsedBy(ctx, "t", ""); return err }(),
		"get pool without id":   func() error { _, err := c.GetPool(ctx, "t", ""); return err }(),
		"create pool nil":       func() error { _, err := c.CreatePool(ctx, "t", nil); return err }(),
		"update pool no id":     func() error { _, err := c.UpdatePool(ctx, "t", "", &Pool{}); return err }(),
		"update pool nil":       func() error { _, err := c.UpdatePool(ctx, "t", "p", nil); return err }(),
		"delete pool no id":     c.DeletePool(ctx, "t", ""),
		"pool used-by no id":    func() error { _, err := c.PoolUsedBy(ctx, "t", ""); return err }(),
		"nil context":           func() error { _, err := c.ListAgents(nilCtx, "t"); return err }(),
		"transport unsupported": func() error { _, err := NewClient(&plainTransport{}).ListPools(ctx, "t"); return err }(),
	}
	for name, err := range refusals {
		if err == nil {
			t.Errorf("%s was not refused", name)
		}
	}
	if fake.method != "" {
		t.Fatalf("a refused request reached the wire: %s", fake.method)
	}
	if NewClient(nil) != nil {
		t.Fatal("no transport, no client")
	}
}

func TestClient_PropagatesTheTransportError(t *testing.T) {
	boom := stderrors.New("agent catalog unavailable")
	c := NewClient(&fakeTransport{capable: true, err: boom})
	if _, err := c.ListAgents(context.Background(), "t"); !stderrors.Is(err, boom) {
		t.Fatalf("ListAgents = %v", err)
	}
	if err := c.DeletePool(context.Background(), "t", "p"); !stderrors.Is(err, boom) {
		t.Fatalf("DeletePool = %v", err)
	}
}
