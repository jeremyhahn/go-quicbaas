// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.
package rest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
)

const tenantSpendPolicyBody = `{"tenant_id":"t1",
	"limits":{"windows":[{"name":"monthly","size":2592000000000000,"max_equivalent_micros":2000000000}]},
	"updated_at":"2026-09-23T06:00:00Z",
	"windows":[{"name":"monthly","size":2592000000000000,"max_tokens":0,"used_tokens":5,"remaining_tokens":0,
	            "max_equivalent_micros":2000000000,"used_equivalent_micros":1500000,
	            "remaining_equivalent_micros":1998500000,"exhausted":false}]}`

// TestOrchestratorTenantSpendPolicy_Routes pins the routes and bodies the SDK
// sends for a tenant's own limits (go-quicbaas#1058): GET and PUT on
// /api/v1/orchestrator/spend-policy, scoped by tenant_id.
func TestOrchestratorTenantSpendPolicy_Routes(t *testing.T) {
	type call struct{ method, path, query, body string }
	calls := make(chan call, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		calls <- call{r.Method, r.URL.Path, r.URL.RawQuery, string(body)}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(tenantSpendPolicyBody)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	ctx := context.Background()

	resp, err := tr.Request(ctx, "Orchestrator", "GetTenantSpendPolicy", &orchestrator.GetTenantSpendPolicyRequest{TenantID: "t1"})
	if err != nil {
		t.Fatalf("GetTenantSpendPolicy: %v", err)
	}
	got := <-calls
	if got.method != http.MethodGet || got.path != "/api/v1/orchestrator/spend-policy" || got.query != "tenant_id=t1" {
		t.Fatalf("get = %+v", got)
	}
	policy, ok := resp.(*orchestrator.TenantSpendPolicy)
	if !ok || len(policy.Limits.Windows) != 1 || policy.Limits.Windows[0].Size != 720*time.Hour ||
		policy.UpdatedAt == nil || len(policy.Windows) != 1 || policy.Windows[0].UsedEquivalentMicros != 1_500_000 {
		t.Fatalf("policy = %+v", resp)
	}

	limits := spend.Limits{Windows: []spend.Window{{Name: "monthly", Size: 720 * time.Hour, MaxEquivalentMicros: 2_000_000_000}}}
	if _, err := tr.Request(ctx, "Orchestrator", "SetTenantSpendPolicy",
		&orchestrator.SetTenantSpendPolicyRequest{TenantID: "t1", Limits: limits}); err != nil {
		t.Fatalf("SetTenantSpendPolicy: %v", err)
	}
	put := <-calls
	if put.method != http.MethodPut || put.path != "/api/v1/orchestrator/spend-policy" || put.query != "tenant_id=t1" {
		t.Fatalf("put = %+v", put)
	}
	var body struct {
		Limits spend.Limits `json:"limits"`
	}
	if err := json.Unmarshal([]byte(put.body), &body); err != nil || len(body.Limits.Windows) != 1 || body.Limits.Windows[0] != limits.Windows[0] {
		t.Fatalf("put body = %s, %v", put.body, err)
	}
}

func TestOrchestratorTenantSpendPolicy_RefusesAWrongRequestType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the server was reached")
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	for _, method := range []string{"GetTenantSpendPolicy", "SetTenantSpendPolicy"} {
		if _, err := tr.Request(context.Background(), "Orchestrator", method, &orchestrator.GetSpendReportRequest{}); err == nil {
			t.Fatalf("%s accepted the wrong request type", method)
		}
	}
}

func TestTenantSpendPolicyPath(t *testing.T) {
	if got := tenantSpendPolicyPath(""); got != "/api/v1/orchestrator/spend-policy" {
		t.Fatalf("unscoped = %q", got)
	}
	if got := tenantSpendPolicyPath("t 1"); got != "/api/v1/orchestrator/spend-policy?tenant_id=t+1" {
		t.Fatalf("scoped = %q", got)
	}
}
