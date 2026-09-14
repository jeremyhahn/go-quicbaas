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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// GetSpendReport over REST (and, by embedding, over QUIC)
// =============================================================================

// spendReportBody is a tenant report with one paying account that carries no
// cap — the shape a deployment that has never set a spend policy produces.
const spendReportBody = `{"level":"tenant","tenant_id":"t1","entries":3,
	"input_tokens":120,"output_tokens":80,"total_tokens":200,
	"cost_micros":0,"cost_measured":false,"successes":2,"failures":1,
	"child_level":"project",
	"children":[{"id":"p1","level":"project","total_tokens":150},
	            {"level":"project","unattributed":true,"total_tokens":50}],
	"accounts":[{"account_id":"a1","display_name":"alice-work","total_tokens":200,
	             "policy_status":"none"}],
	"models":[{"engine":"acp","model":"claude-opus-5","total_tokens":200}]}`

// TestOrchestratorGetSpendReport_GetsTheCostRoute pins the URL the SDK builds.
// The server registers GET /api/v1/orchestrator/cost and the PEP authorizes on
// that exact pattern, so a path drift silently changes which permission the
// call is checked against.
func TestOrchestratorGetSpendReport_GetsTheCostRoute(t *testing.T) {
	var gotMethod, gotPath, gotQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(spendReportBody))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	resp, err := tr.Request(context.Background(), "Orchestrator", "GetSpendReport",
		&orchestrator.GetSpendReportRequest{
			TenantID:  "t1",
			ProjectID: "p1",
			TaskID:    "w1",
			JobID:     "j1",
			SessionID: "s1",
		})
	if err != nil {
		t.Fatalf("GetSpendReport: %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Errorf("method = %s, want GET", gotMethod)
	}
	if gotPath != "/api/v1/orchestrator/cost" {
		t.Errorf("path = %s, want /api/v1/orchestrator/cost", gotPath)
	}
	for _, want := range []string{"tenant_id=t1", "project_id=p1", "task_id=w1", "job_id=j1", "session_id=s1"} {
		if !containsQuery(gotQuery, want) {
			t.Errorf("query %q dropped %q", gotQuery, want)
		}
	}

	report, ok := resp.(*orchestrator.GetSpendReportResponse)
	if !ok {
		t.Fatalf("response is %T, want *orchestrator.GetSpendReportResponse", resp)
	}
	if report.Level != orchestrator.SpendLevelTenant || report.TotalTokens != 200 {
		t.Errorf("report = %+v, want tenant/200", report)
	}
	if report.CostMeasured {
		t.Error("an unpriced report must not claim a measured cost")
	}
	if len(report.Children) != 2 || !report.Children[1].Unattributed {
		t.Errorf("children = %+v, want the unattributed row kept", report.Children)
	}
	if len(report.Accounts) != 1 || report.Accounts[0].PolicyStatus != orchestrator.SpendPolicyNone {
		t.Errorf("accounts = %+v, want acct a1 with no spend policy", report.Accounts)
	}
}

// TestOrchestratorGetSpendReport_RoundIsSentOnlyWhenSet asserts round 0 — a
// real level — survives, and that an unset round widens the scope rather than
// silently narrowing it to round 0.
func TestOrchestratorGetSpendReport_RoundIsSentOnlyWhenSet(t *testing.T) {
	queries := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries <- r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(spendReportBody))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()
	if _, err := tr.Request(ctx, "Orchestrator", "GetSpendReport",
		&orchestrator.GetSpendReportRequest{TenantID: "t1", Round: 0, RoundSet: true}); err != nil {
		t.Fatalf("GetSpendReport(round 0): %v", err)
	}
	if got := <-queries; !containsQuery(got, "round=0") {
		t.Errorf("query %q dropped round=0; round 0 is a level, not an absence", got)
	}

	if _, err := tr.Request(ctx, "Orchestrator", "GetSpendReport",
		&orchestrator.GetSpendReportRequest{TenantID: "t1", Round: 7}); err != nil {
		t.Fatalf("GetSpendReport(no round): %v", err)
	}
	if got := <-queries; containsQuery(got, "round=") {
		t.Errorf("query %q sent a round the caller did not select", got)
	}
}

// TestOrchestratorGetSpendReport_RefusalIsAnError asserts a server refusal
// surfaces as an error and never as a decoded report reading as a tenant that
// has spent nothing.
func TestOrchestratorGetSpendReport_RefusalIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"consumption ledger not available"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	if _, err := tr.Request(context.Background(), "Orchestrator", "GetSpendReport",
		&orchestrator.GetSpendReportRequest{TenantID: "t1"}); err == nil {
		t.Fatal("a refused read must be an error, not an empty report")
	}
}

// TestOrchestratorGetSpendReport_RejectsTheWrongRequestType asserts the
// dispatcher refuses a request it cannot address rather than sending a bare
// tenant-wide query on the caller's behalf.
func TestOrchestratorGetSpendReport_RejectsTheWrongRequestType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(spendReportBody))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	if _, err := tr.Request(context.Background(), "Orchestrator", "GetSpendReport",
		&orchestrator.GetSessionConversationRequest{SessionID: "s1"}); err == nil {
		t.Fatal("a mistyped request must be refused")
	}
}
