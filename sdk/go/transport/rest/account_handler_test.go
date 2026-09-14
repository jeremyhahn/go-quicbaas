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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
)

// TestIsAccountService pins the routing predicate, including the package-
// qualified form the SDK client actually sends.
func TestIsAccountService(t *testing.T) {
	for _, name := range []string{"Account", "quicbaas.Account"} {
		if !isAccountService(name) {
			t.Errorf("isAccountService(%q) = false", name)
		}
	}
	for _, name := range []string{"Project", "quicbaas.Orchestrator", "quicbaas.Auth"} {
		if isAccountService(name) {
			t.Errorf("isAccountService(%q) = true", name)
		}
	}
}

// TestAccountHandlerRoutes drives every account method through the REST
// handler and asserts the HTTP method, path, and tenant query parameter each
// builds. A handler that hits the wrong route is a silent 404 in production.
func TestAccountHandlerRoutes(t *testing.T) {
	var gotMethod, gotPath, gotTenant string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotTenant = r.Method, r.URL.Path, r.URL.Query().Get("tenant_id")
		w.Header().Set("Content-Type", "application/json")
		// One envelope satisfying every shape the handlers decode.
		_, _ = w.Write([]byte(`{"account":{"id":"acct-1"},"accounts":[{"id":"acct-1"}],` +
			`"total":1,"account_id":"acct-1","deleted":true,"usage":{"total_tokens":42},` +
			`"credential":{"access_token":"RELEASED","access_token_env_key":"K"}}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	cases := []struct {
		name       string
		method     string
		req        interface{}
		wantMethod string
		wantPath   string
		// wantTenantQuery is false for Create, whose tenant travels in the
		// JSON body (the server reads the body first, then falls back to the
		// query parameter) rather than the query string.
		wantTenantQuery bool
	}{
		{"Create", "Create",
			&account.CreateAccountRequest{TenantID: "t1", Provider: account.ProviderClaude,
				Credential: &account.Credential{AccessToken: "SECRET-ACCESS-TOKEN"}},
			http.MethodPost, "/api/v1/agent-accounts", false},
		{"Get", "Get",
			&account.GetAccountRequest{TenantID: "t1", AccountID: "acct-1"},
			http.MethodGet, "/api/v1/agent-accounts/acct-1", true},
		{"List", "List",
			&account.ListAccountsRequest{TenantID: "t1", Provider: account.ProviderClaude, ProjectID: "p1"},
			http.MethodGet, "/api/v1/agent-accounts", true},
		{"Update", "Update",
			&account.UpdateAccountRequest{TenantID: "t1", AccountID: "acct-1", DisplayName: "renamed"},
			http.MethodPut, "/api/v1/agent-accounts/acct-1", true},
		{"Delete", "Delete",
			&account.DeleteAccountRequest{TenantID: "t1", AccountID: "acct-1"},
			http.MethodDelete, "/api/v1/agent-accounts/acct-1", true},
		{"RotateCredential", "RotateCredential",
			&account.RotateCredentialRequest{TenantID: "t1", AccountID: "acct-1",
				Credential: &account.Credential{AccessToken: "SECRET-ACCESS-TOKEN"}},
			http.MethodPost, "/api/v1/agent-accounts/acct-1/rotate", true},
		{"Revoke", "Revoke",
			&account.RevokeAccountRequest{TenantID: "t1", AccountID: "acct-1", Reason: "leaked"},
			http.MethodPost, "/api/v1/agent-accounts/acct-1/revoke", true},
		{"SetDefault", "SetDefault",
			&account.SetDefaultAccountRequest{TenantID: "t1", AccountID: "acct-1"},
			http.MethodPost, "/api/v1/agent-accounts/acct-1/default", true},
		{"Usage", "Usage",
			&account.GetAccountUsageRequest{TenantID: "t1", AccountID: "acct-1"},
			http.MethodGet, "/api/v1/agent-accounts/acct-1/usage", true},
		{"Materialize", "Materialize",
			&account.MaterializeCredentialRequest{TenantID: "t1", AccountID: "acct-1"},
			http.MethodPost, "/api/v1/agent-accounts/acct-1/materialize", true},
	}
	handler := newAccountRESTHandler(tr)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := handler.handleRequest(ctx, tc.method, tc.req)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if resp == nil {
				t.Fatalf("%s returned a nil response", tc.name)
			}
			if gotMethod != tc.wantMethod {
				t.Errorf("%s HTTP method = %s, want %s", tc.name, gotMethod, tc.wantMethod)
			}
			if gotPath != tc.wantPath {
				t.Errorf("%s path = %s, want %s", tc.name, gotPath, tc.wantPath)
			}
			if tc.wantTenantQuery && gotTenant != "t1" {
				t.Errorf("%s tenant_id = %q, want t1", tc.name, gotTenant)
			}
		})
	}
}

// TestAccountHandlerListEncodesFilters proves the provider and project filters
// reach the query string; dropping them would silently return every account.
func TestAccountHandlerListEncodesFilters(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"accounts":[],"total":0}`))
	}))
	defer server.Close()

	handler := newAccountRESTHandler(newTestTransport(t, server))
	if _, err := handler.handleRequest(context.Background(), "List", &account.ListAccountsRequest{
		TenantID: "t1", Provider: account.ProviderCodex, ProjectID: "proj 1",
	}); err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, want := range []string{"tenant_id=t1", "provider=codex", "project_id=proj+1"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
}

// TestAccountHandlerRotateSendsOnlyTheCredential proves the rotate body is the
// credential envelope alone — the account/tenant IDs travel in the URL, so
// echoing them in the body would be redundant wire surface.
func TestAccountHandlerRotateSendsOnlyTheCredential(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"account_id":"acct-1"}`))
	}))
	defer server.Close()

	handler := newAccountRESTHandler(newTestTransport(t, server))
	if _, err := handler.handleRequest(context.Background(), "RotateCredential", &account.RotateCredentialRequest{
		TenantID: "t1", AccountID: "acct-1",
		Credential: &account.Credential{AccessToken: "SECRET-ACCESS-TOKEN"},
	}); err != nil {
		t.Fatalf("RotateCredential: %v", err)
	}
	if _, ok := body["credential"]; !ok {
		t.Fatalf("rotate body carried no credential: %+v", body)
	}
	if len(body) != 1 {
		t.Errorf("rotate body carried unexpected extra fields: %+v", body)
	}
}

// TestAccountHandlerInvalidRequestType proves each method rejects a wrong-typed
// request instead of panicking on the type assertion.
func TestAccountHandlerInvalidRequestType(t *testing.T) {
	handler := newAccountRESTHandler(&Transport{})
	for _, method := range []string{
		"Create", "Get", "List", "Update", "Delete",
		"RotateCredential", "Revoke", "SetDefault", "Usage",
	} {
		if _, err := handler.handleRequest(context.Background(), method, "not-a-request"); err == nil {
			t.Errorf("%s accepted a wrong-typed request", method)
		}
	}
}

// TestAccountHandlerUnknownMethod proves the dispatcher fails closed rather
// than falling through to a generic request that would 404.
func TestAccountHandlerUnknownMethod(t *testing.T) {
	handler := newAccountRESTHandler(&Transport{})
	if _, err := handler.handleRequest(context.Background(), "Teleport", nil); err == nil {
		t.Fatal("expected an error for an unknown account method")
	}
}

// TestAccountHandlerUsageRequiresAReport proves a server answering without a
// usage object surfaces an error rather than a nil report the caller would
// dereference.
func TestAccountHandlerUsageRequiresAReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	handler := newAccountRESTHandler(newTestTransport(t, server))
	if _, err := handler.handleRequest(context.Background(), "Usage",
		&account.GetAccountUsageRequest{TenantID: "t1", AccountID: "acct-1"}); err == nil {
		t.Fatal("expected an error when the server returns no usage report")
	}
}

// TestAccountHandlerMaterializeDecodesTheReleasedEnvelope proves the release
// response reaches the caller intact — the one account response in this
// transport that carries secret material.
func TestAccountHandlerMaterializeDecodesTheReleasedEnvelope(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"account_id":"acct-1","provider":"claude","refresh_policy":"none",` +
			`"credential":{"access_token":"RELEASED","access_token_env_key":"CLAUDE_CODE_OAUTH_TOKEN"},` +
			`"updated_at":"2026-09-09T12:00:00Z"}`))
	}))
	defer server.Close()

	handler := newAccountRESTHandler(newTestTransport(t, server))
	resp, err := handler.handleRequest(context.Background(), "Materialize",
		&account.MaterializeCredentialRequest{TenantID: "t1", AccountID: "acct-1", ProjectID: "proj-1"})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	released, ok := resp.(*account.MaterializedCredential)
	if !ok {
		t.Fatalf("response is %T, want *account.MaterializedCredential", resp)
	}
	if released.Credential == nil || released.Credential.AccessTokenEnvKey != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Fatal("release lost the material a spawn needs")
	}
	if released.UpdatedAt.IsZero() {
		t.Error("release lost the cache-invalidation signal")
	}
	if !strings.Contains(gotBody, "proj-1") {
		t.Errorf("request body %q did not carry the project scope", gotBody)
	}
}

// TestAccountHandlerMaterializeRejectsTheWrongRequestType covers the
// dispatcher's bad-input path.
func TestAccountHandlerMaterializeRejectsTheWrongRequestType(t *testing.T) {
	handler := newAccountRESTHandler(nil)
	if _, err := handler.handleRequest(context.Background(), "Materialize",
		&account.GetAccountRequest{TenantID: "t1", AccountID: "acct-1"}); err == nil {
		t.Fatal("expected an error for a mismatched request type")
	}
}
