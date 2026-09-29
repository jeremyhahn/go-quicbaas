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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
)

// TestAccountHandlerUsageCarriesLiveSessions proves the live-session count the
// server reports beside the usage figures reaches the SDK caller.
func TestAccountHandlerUsageCarriesLiveSessions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"usage":{"total_tokens":12},"live_sessions":2}`)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()

	handler := newAccountRESTHandler(newTestTransport(t, server))
	resp, err := handler.handleRequest(context.Background(), "Usage",
		&account.GetAccountUsageRequest{TenantID: "t1", AccountID: "acct-1"})
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	report, ok := resp.(*account.UsageReport)
	if !ok {
		t.Fatalf("Usage returned %T", resp)
	}
	if report.TotalTokens != 12 || report.LiveSessions != 2 {
		t.Fatalf("report = %+v, want 12 tokens and 2 live sessions", report)
	}
}

// TestAccountHandlerUsageCarriesTheSpendVerdict proves the spend gate's
// verdict reaches the SDK caller, and is nil when the server sends none.
func TestAccountHandlerUsageCarriesTheSpendVerdict(t *testing.T) {
	bodies := make(chan string, 2)
	bodies <- `{"usage":{"total_tokens":1},"live_sessions":0,"spend":{"admits":false,"refusal":"quota_exhausted","window":"daily","frees_at":"2026-09-25T00:00:00Z"}}`
	bodies <- `{"usage":{"total_tokens":1},"live_sessions":0}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(<-bodies)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()

	handler := newAccountRESTHandler(newTestTransport(t, server))
	req := &account.GetAccountUsageRequest{TenantID: "t1", AccountID: "acct-1"}
	resp, err := handler.handleRequest(context.Background(), "Usage", req)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	spent := resp.(*account.UsageReport).Spend
	if spent == nil || spent.Admits || spent.Refusal != "quota_exhausted" || spent.FreesAt == nil {
		t.Fatalf("spend = %+v, want the spent daily window", spent)
	}
	resp, err = handler.handleRequest(context.Background(), "Usage", req)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if resp.(*account.UsageReport).Spend != nil {
		t.Fatal("a server with no gate sent no verdict, yet the SDK reported one")
	}
}
