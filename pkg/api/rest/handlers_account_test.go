//go:build rest

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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// mountAccountTestRoutes mounts the /agent-accounts surface on r exactly as
// the routes.go table (Routes(), GroupAPI) registers it in production; the
// standalone registerAccountRoutes chi helper was removed once the
// table-driven registration became the single source of truth, so tests
// that need a real chi mux (path params, method routing) mount the
// equivalent routes locally instead of reaching for a shared prod helper.
func mountAccountTestRoutes(srv *Server, r chi.Router) {
	r.Route("/agent-accounts", func(r chi.Router) {
		r.Post("/", srv.HandleCreateAccount)
		r.Get("/", srv.HandleListAccounts)
		r.Get("/{id}", srv.HandleGetAccount)
		r.Put("/{id}", srv.HandleUpdateAccount)
		r.Delete("/{id}", srv.HandleDeleteAccount)

		r.Post("/{id}/rotate", srv.HandleRotateAccountCredential)
		r.Post("/{id}/revoke", srv.HandleRevokeAccount)
		r.Post("/{id}/default", srv.HandleSetDefaultAccount)
		r.Get("/{id}/usage", srv.HandleGetAccountUsage)
	})
}

// newAccountTestServer builds a REST Server whose only wired sub-service is
// the supplied AccountService, with the real account routes mounted under
// /api/v1 so path parameters and method routing are exercised end to end.
func newAccountTestServer(accountSvc service.AccountService) *Server {
	mock := mocks.NewMockQuicBaaS()
	mock.AccountServiceFunc = func() service.AccountService { return accountSvc }

	router := chi.NewRouter()
	srv := &Server{service: mock, router: router}
	router.Route("/api/v1", func(r chi.Router) {
		mountAccountTestRoutes(srv, r)
	})
	return srv
}

// doAccountRequest issues a request against the mounted account routes.
func doAccountRequest(srv *Server, method, target string, body interface{}) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			panic(err)
		}
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}
	r := httptest.NewRequest(method, target, reader)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, r)
	return w
}

// sampleAccount is the metadata record the success-path fakes return.
func sampleAccount() *orchestration.Account {
	return &orchestration.Account{
		ID:            "acct-1",
		TenantID:      "tenant-1",
		Provider:      orchestration.AccountProviderClaude,
		DisplayName:   "alice-work",
		Status:        orchestration.AccountStatusActive,
		RefreshPolicy: orchestration.AccountRefreshOAuth2,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
}

// sampleCredentialBody is a fully populated inbound credential envelope. Every
// field carries a value a leak test can search the response body for.
func sampleCredentialBody() map[string]interface{} {
	return map[string]interface{}{
		"access_token":         "SECRET-ACCESS-TOKEN",
		"refresh_token":        "SECRET-REFRESH-TOKEN",
		"token_endpoint":       "https://auth.example.test/token",
		"client_id":            "client-abc",
		"env":                  map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "SECRET-ENV-VALUE"},
		"access_token_env_key": "CLAUDE_CODE_OAUTH_TOKEN",
	}
}

// =============================================================================
// Create
// =============================================================================

func TestHandleCreateAccount_HappyPath(t *testing.T) {
	var captured *service.CreateAccountRequest
	svc := &mocks.MockAccountService{
		CreateAccountFunc: func(ctx context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			captured = req
			return &service.CreateAccountResponse{Account: sampleAccount(), Timestamp: time.Now().UTC()}, nil
		},
	}
	srv := newAccountTestServer(svc)

	w := doAccountRequest(srv, http.MethodPost, "/api/v1/agent-accounts", map[string]interface{}{
		"tenant_id":      "tenant-1",
		"provider":       "claude",
		"display_name":   "alice-work",
		"refresh_policy": "oauth2",
		"credential":     sampleCredentialBody(),
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if captured == nil {
		t.Fatal("service was not called")
	}
	if captured.TenantID != "tenant-1" || captured.Provider != orchestration.AccountProviderClaude {
		t.Errorf("unexpected service request: %+v", captured)
	}
	if captured.Credential == nil || captured.Credential.AccessToken != "SECRET-ACCESS-TOKEN" {
		t.Error("credential envelope was not forwarded to the service layer")
	}

	var resp AccountEnvelopeRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Account == nil || resp.Account.ID != "acct-1" {
		t.Fatalf("unexpected account in response: %+v", resp.Account)
	}
}

func TestHandleCreateAccount_InvalidInput(t *testing.T) {
	svc := &mocks.MockAccountService{
		CreateAccountFunc: func(ctx context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			t.Fatal("service must not be called for an invalid request")
			return nil, nil
		},
	}
	srv := newAccountTestServer(svc)

	cases := []struct {
		name string
		body interface{}
		raw  string
	}{
		{name: "missing tenant", body: map[string]interface{}{"provider": "claude", "credential": sampleCredentialBody()}},
		{name: "missing provider", body: map[string]interface{}{"tenant_id": "t1", "credential": sampleCredentialBody()}},
		{name: "malformed json", raw: "{not-json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var w *httptest.ResponseRecorder
			if tc.raw != "" {
				r := httptest.NewRequest(http.MethodPost, "/api/v1/agent-accounts", strings.NewReader(tc.raw))
				w = httptest.NewRecorder()
				srv.router.ServeHTTP(w, r)
			} else {
				w = doAccountRequest(srv, http.MethodPost, "/api/v1/agent-accounts", tc.body)
			}
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// TestHandleCreateAccount_SecretRef proves a create that names a tenant secret
// instead of an inline credential reaches the service with the reference and
// file intact, and that the response reports the reference back.
func TestHandleCreateAccount_SecretRef(t *testing.T) {
	var captured *service.CreateAccountRequest
	svc := &mocks.MockAccountService{
		CreateAccountFunc: func(ctx context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			captured = req
			account := sampleAccount()
			account.CredentialSecretRef = req.CredentialSecretRef
			account.CredentialSecretFile = req.CredentialSecretFile
			return &service.CreateAccountResponse{Account: account, Timestamp: time.Now().UTC()}, nil
		},
	}
	srv := newAccountTestServer(svc)

	w := doAccountRequest(srv, http.MethodPost, "/api/v1/agent-accounts", map[string]interface{}{
		"tenant_id":              "tenant-1",
		"provider":               "codex",
		"credential_secret_ref":  "agents/codex/reviewer/auth_json",
		"credential_secret_file": ".codex/auth.json",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if captured == nil || captured.Credential != nil ||
		captured.CredentialSecretRef != "agents/codex/reviewer/auth_json" ||
		captured.CredentialSecretFile != ".codex/auth.json" {
		t.Fatalf("secret reference not forwarded: %+v", captured)
	}
	var resp AccountEnvelopeRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Account.CredentialSecretRef != "agents/codex/reviewer/auth_json" ||
		resp.Account.CredentialSecretFile != ".codex/auth.json" {
		t.Fatalf("reference not reported back: %+v", resp.Account)
	}
}

// TestHandleCreateAccount_CredentialSourceRefusedByService proves the
// inline-vs-reference rule is answered by the service and surfaces as a 400.
func TestHandleCreateAccount_CredentialSourceRefusedByService(t *testing.T) {
	svc := &mocks.MockAccountService{
		CreateAccountFunc: func(ctx context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			return nil, &service.AccountInvalidError{Field: "credential", Message: "an account needs a credential"}
		},
	}
	srv := newAccountTestServer(svc)

	w := doAccountRequest(srv, http.MethodPost, "/api/v1/agent-accounts", map[string]interface{}{
		"tenant_id": "tenant-1", "provider": "claude",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleAccountError_SecretReference maps the two reference failures: an
// unresolvable secret is the account's state (409), a node with no resolver is
// unavailable (503).
func TestHandleAccountError_SecretReference(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"unresolved", &orchestration.AccountSecretRefUnresolvedError{
			TenantID: "t", AccountID: "a", Ref: "agents/x", Reason: "the secret does not exist in this tenant",
		}, http.StatusConflict},
		{"no resolver", &orchestration.AccountSecretResolverNotConfiguredError{AccountID: "a"}, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			handleAccountError(w, httptest.NewRequest(http.MethodPost, "/", nil), tc.err)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d", w.Code, tc.want)
			}
		})
	}
}

// =============================================================================
// Get / List
// =============================================================================

func TestHandleGetAccount_HappyPath(t *testing.T) {
	svc := &mocks.MockAccountService{
		GetAccountFunc: func(ctx context.Context, req *service.GetAccountRequest) (*service.GetAccountResponse, error) {
			if req.AccountID != "acct-1" || req.TenantID != "tenant-1" {
				t.Errorf("unexpected request: %+v", req)
			}
			return &service.GetAccountResponse{Account: sampleAccount(), Timestamp: time.Now().UTC()}, nil
		},
	}
	srv := newAccountTestServer(svc)

	w := doAccountRequest(srv, http.MethodGet, "/api/v1/agent-accounts/acct-1?tenant_id=tenant-1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestHandleGetAccount_ErrorPaths(t *testing.T) {
	svc := &mocks.MockAccountService{
		GetAccountFunc: func(ctx context.Context, req *service.GetAccountRequest) (*service.GetAccountResponse, error) {
			return nil, &service.AccountNotFoundError{TenantID: req.TenantID, AccountID: req.AccountID}
		},
	}
	srv := newAccountTestServer(svc)

	if w := doAccountRequest(srv, http.MethodGet, "/api/v1/agent-accounts/acct-1", nil); w.Code != http.StatusBadRequest {
		t.Errorf("missing tenant_id: status = %d, want 400", w.Code)
	}
	if w := doAccountRequest(srv, http.MethodGet, "/api/v1/agent-accounts/nope?tenant_id=t1", nil); w.Code != http.StatusNotFound {
		t.Errorf("unknown account: status = %d, want 404", w.Code)
	}
}

func TestHandleListAccounts_HappyPath(t *testing.T) {
	svc := &mocks.MockAccountService{
		ListAccountsFunc: func(ctx context.Context, req *service.ListAccountsRequest) (*service.ListAccountsResponse, error) {
			if req.Provider != orchestration.AccountProviderClaude {
				t.Errorf("provider filter not propagated: %+v", req)
			}
			if req.ProjectID != "proj-9" {
				t.Errorf("project filter not propagated: %+v", req)
			}
			return &service.ListAccountsResponse{
				Accounts:  []*orchestration.Account{sampleAccount()},
				Total:     1,
				Timestamp: time.Now().UTC(),
			}, nil
		},
	}
	srv := newAccountTestServer(svc)

	w := doAccountRequest(srv, http.MethodGet,
		"/api/v1/agent-accounts?tenant_id=tenant-1&provider=claude&project_id=proj-9", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp ListAccountsRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 || len(resp.Accounts) != 1 {
		t.Fatalf("unexpected list response: %+v", resp)
	}
}

func TestHandleListAccounts_MissingTenant(t *testing.T) {
	srv := newAccountTestServer(&mocks.MockAccountService{})
	if w := doAccountRequest(srv, http.MethodGet, "/api/v1/agent-accounts", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// =============================================================================
// Update / Delete
// =============================================================================

func TestHandleUpdateAccount_HappyPath(t *testing.T) {
	var captured *service.UpdateAccountRequest
	svc := &mocks.MockAccountService{
		UpdateAccountFunc: func(ctx context.Context, req *service.UpdateAccountRequest) (*service.UpdateAccountResponse, error) {
			captured = req
			return &service.UpdateAccountResponse{Account: sampleAccount(), Timestamp: time.Now().UTC()}, nil
		},
	}
	srv := newAccountTestServer(svc)

	w := doAccountRequest(srv, http.MethodPut, "/api/v1/agent-accounts/acct-1?tenant_id=tenant-1", map[string]interface{}{
		"display_name":     "renamed",
		"clear_project_id": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if captured == nil || captured.DisplayName != "renamed" || !captured.ClearProjectID {
		t.Fatalf("update fields not propagated: %+v", captured)
	}
}

// TestHandleUpdateAccount_SecretRef proves the switch-to-reference fields
// reach the service.
func TestHandleUpdateAccount_SecretRef(t *testing.T) {
	var captured *service.UpdateAccountRequest
	svc := &mocks.MockAccountService{
		UpdateAccountFunc: func(ctx context.Context, req *service.UpdateAccountRequest) (*service.UpdateAccountResponse, error) {
			captured = req
			return &service.UpdateAccountResponse{Account: sampleAccount(), Timestamp: time.Now().UTC()}, nil
		},
	}
	srv := newAccountTestServer(svc)

	w := doAccountRequest(srv, http.MethodPut, "/api/v1/agent-accounts/acct-1?tenant_id=tenant-1", map[string]interface{}{
		"credential_secret_ref": "agents/claude/claude1/oauth_token",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if captured == nil || captured.CredentialSecretRef != "agents/claude/claude1/oauth_token" {
		t.Fatalf("secret reference not propagated: %+v", captured)
	}
}

func TestHandleUpdateAccount_InvalidInput(t *testing.T) {
	srv := newAccountTestServer(&mocks.MockAccountService{})

	if w := doAccountRequest(srv, http.MethodPut, "/api/v1/agent-accounts/acct-1", map[string]interface{}{}); w.Code != http.StatusBadRequest {
		t.Errorf("missing tenant_id: status = %d, want 400", w.Code)
	}

	r := httptest.NewRequest(http.MethodPut, "/api/v1/agent-accounts/acct-1?tenant_id=t1", strings.NewReader("{oops"))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("malformed body: status = %d, want 400", w.Code)
	}
}

func TestHandleDeleteAccount_HappyPath(t *testing.T) {
	svc := &mocks.MockAccountService{
		DeleteAccountFunc: func(ctx context.Context, req *service.DeleteAccountRequest) (*service.DeleteAccountResponse, error) {
			return &service.DeleteAccountResponse{AccountID: req.AccountID, Deleted: true, Timestamp: time.Now().UTC()}, nil
		},
	}
	srv := newAccountTestServer(svc)

	w := doAccountRequest(srv, http.MethodDelete, "/api/v1/agent-accounts/acct-1?tenant_id=tenant-1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp DeleteAccountRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Deleted || resp.AccountID != "acct-1" {
		t.Fatalf("unexpected delete response: %+v", resp)
	}
}

func TestHandleDeleteAccount_NotFound(t *testing.T) {
	svc := &mocks.MockAccountService{
		DeleteAccountFunc: func(ctx context.Context, req *service.DeleteAccountRequest) (*service.DeleteAccountResponse, error) {
			return nil, &service.AccountNotFoundError{TenantID: req.TenantID, AccountID: req.AccountID}
		},
	}
	srv := newAccountTestServer(svc)
	if w := doAccountRequest(srv, http.MethodDelete, "/api/v1/agent-accounts/x?tenant_id=t1", nil); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// =============================================================================
// Rotate / Revoke / SetDefault / Usage
// =============================================================================

func TestHandleRotateAccountCredential_HappyPath(t *testing.T) {
	var captured *service.RotateAccountCredentialRequest
	svc := &mocks.MockAccountService{
		RotateAccountCredentialFunc: func(ctx context.Context, req *service.RotateAccountCredentialRequest) (*service.RotateAccountCredentialResponse, error) {
			captured = req
			return &service.RotateAccountCredentialResponse{AccountID: req.AccountID, Timestamp: time.Now().UTC()}, nil
		},
	}
	srv := newAccountTestServer(svc)

	w := doAccountRequest(srv, http.MethodPost, "/api/v1/agent-accounts/acct-1/rotate?tenant_id=tenant-1",
		map[string]interface{}{"credential": sampleCredentialBody()})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if captured == nil || captured.Credential == nil || captured.Credential.RefreshToken != "SECRET-REFRESH-TOKEN" {
		t.Fatal("rotate credential not forwarded to the service layer")
	}
}

func TestHandleRotateAccountCredential_MissingCredential(t *testing.T) {
	srv := newAccountTestServer(&mocks.MockAccountService{})
	w := doAccountRequest(srv, http.MethodPost, "/api/v1/agent-accounts/acct-1/rotate?tenant_id=t1",
		map[string]interface{}{})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestHandleRevokeAccount_HappyPath(t *testing.T) {
	var captured *service.RevokeAccountRequest
	svc := &mocks.MockAccountService{
		RevokeAccountFunc: func(ctx context.Context, req *service.RevokeAccountRequest) (*service.RevokeAccountResponse, error) {
			captured = req
			revoked := sampleAccount()
			revoked.Status = orchestration.AccountStatusRevoked
			return &service.RevokeAccountResponse{Account: revoked, Timestamp: time.Now().UTC()}, nil
		},
	}
	srv := newAccountTestServer(svc)

	w := doAccountRequest(srv, http.MethodPost, "/api/v1/agent-accounts/acct-1/revoke?tenant_id=tenant-1",
		map[string]interface{}{"reason": "compromised"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if captured == nil || captured.Reason != "compromised" {
		t.Fatalf("reason not propagated: %+v", captured)
	}
}

func TestHandleRevokeAccount_MissingTenant(t *testing.T) {
	srv := newAccountTestServer(&mocks.MockAccountService{})
	w := doAccountRequest(srv, http.MethodPost, "/api/v1/agent-accounts/acct-1/revoke", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleSetDefaultAccount_HappyPath(t *testing.T) {
	svc := &mocks.MockAccountService{
		SetDefaultAccountFunc: func(ctx context.Context, req *service.SetDefaultAccountRequest) (*service.SetDefaultAccountResponse, error) {
			return &service.SetDefaultAccountResponse{AccountID: req.AccountID, Timestamp: time.Now().UTC()}, nil
		},
	}
	srv := newAccountTestServer(svc)

	w := doAccountRequest(srv, http.MethodPost, "/api/v1/agent-accounts/acct-1/default?tenant_id=tenant-1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestHandleSetDefaultAccount_NotFound(t *testing.T) {
	svc := &mocks.MockAccountService{
		SetDefaultAccountFunc: func(ctx context.Context, req *service.SetDefaultAccountRequest) (*service.SetDefaultAccountResponse, error) {
			return nil, &service.AccountNotFoundError{TenantID: req.TenantID, AccountID: req.AccountID}
		},
	}
	srv := newAccountTestServer(svc)
	w := doAccountRequest(srv, http.MethodPost, "/api/v1/agent-accounts/x/default?tenant_id=t1", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleGetAccountUsage_HappyPath(t *testing.T) {
	svc := &mocks.MockAccountService{
		GetAccountUsageFunc: func(ctx context.Context, req *service.GetAccountUsageRequest) (*service.GetAccountUsageResponse, error) {
			return &service.GetAccountUsageResponse{
				Usage:     &orchestration.UsageReport{TotalTokens: 4242, ByAgent: map[string]int64{"a": 42}},
				Timestamp: time.Now().UTC(),
			}, nil
		},
	}
	srv := newAccountTestServer(svc)

	w := doAccountRequest(srv, http.MethodGet, "/api/v1/agent-accounts/acct-1/usage?tenant_id=tenant-1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp GetAccountUsageRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 4242 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
}

func TestHandleGetAccountUsage_TrackerNotConfigured(t *testing.T) {
	svc := &mocks.MockAccountService{
		GetAccountUsageFunc: func(ctx context.Context, req *service.GetAccountUsageRequest) (*service.GetAccountUsageResponse, error) {
			return nil, &service.AccountUsageTrackerNotConfiguredError{}
		},
	}
	srv := newAccountTestServer(svc)
	w := doAccountRequest(srv, http.MethodGet, "/api/v1/agent-accounts/acct-1/usage?tenant_id=t1", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// Service-unavailable posture
// =============================================================================

func TestAccountHandlers_ServiceUnavailable(t *testing.T) {
	mock := mocks.NewMockQuicBaaS()
	mock.AccountServiceFunc = func() service.AccountService { return nil }
	router := chi.NewRouter()
	srv := &Server{service: mock, router: router}
	router.Route("/api/v1", func(r chi.Router) { mountAccountTestRoutes(srv, r) })

	requests := [][2]string{
		{http.MethodGet, "/api/v1/agent-accounts?tenant_id=t1"},
		{http.MethodGet, "/api/v1/agent-accounts/a1?tenant_id=t1"},
		{http.MethodPut, "/api/v1/agent-accounts/a1?tenant_id=t1"},
		{http.MethodDelete, "/api/v1/agent-accounts/a1?tenant_id=t1"},
		{http.MethodPost, "/api/v1/agent-accounts/a1/revoke?tenant_id=t1"},
		{http.MethodPost, "/api/v1/agent-accounts/a1/default?tenant_id=t1"},
		{http.MethodGet, "/api/v1/agent-accounts/a1/usage?tenant_id=t1"},
	}
	for _, req := range requests {
		w := doAccountRequest(srv, req[0], req[1], nil)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status = %d, want 503", req[0], req[1], w.Code)
		}
	}
}

// =============================================================================
// Credential leak guards
// =============================================================================

// accountSecretMarkers are the distinctive values sampleCredentialBody sends
// inbound. None of them may appear in any account response body.
var accountSecretMarkers = []string{
	"SECRET-ACCESS-TOKEN",
	"SECRET-REFRESH-TOKEN",
	"SECRET-ENV-VALUE",
	"access_token",
	"refresh_token",
}

// TestAccountRESTResponsesCarryNoCredential is the behavioral leak guard: it
// drives every response-producing verb with a service that hands back a
// metadata record and asserts none of the credential markers reaches the wire.
func TestAccountRESTResponsesCarryNoCredential(t *testing.T) {
	full := sampleAccount()
	svc := &mocks.MockAccountService{
		CreateAccountFunc: func(ctx context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			// The service received the real secret; the response must not.
			if req.Credential == nil || req.Credential.AccessToken == "" {
				t.Fatal("test setup: credential did not reach the service")
			}
			return &service.CreateAccountResponse{Account: full, Timestamp: time.Now().UTC()}, nil
		},
		GetAccountFunc: func(ctx context.Context, req *service.GetAccountRequest) (*service.GetAccountResponse, error) {
			return &service.GetAccountResponse{Account: full, Timestamp: time.Now().UTC()}, nil
		},
		ListAccountsFunc: func(ctx context.Context, req *service.ListAccountsRequest) (*service.ListAccountsResponse, error) {
			return &service.ListAccountsResponse{Accounts: []*orchestration.Account{full}, Total: 1, Timestamp: time.Now().UTC()}, nil
		},
		RotateAccountCredentialFunc: func(ctx context.Context, req *service.RotateAccountCredentialRequest) (*service.RotateAccountCredentialResponse, error) {
			return &service.RotateAccountCredentialResponse{AccountID: req.AccountID, Timestamp: time.Now().UTC()}, nil
		},
	}
	srv := newAccountTestServer(svc)

	responses := []*httptest.ResponseRecorder{
		doAccountRequest(srv, http.MethodPost, "/api/v1/agent-accounts", map[string]interface{}{
			"tenant_id": "tenant-1", "provider": "claude", "credential": sampleCredentialBody(),
		}),
		doAccountRequest(srv, http.MethodGet, "/api/v1/agent-accounts/acct-1?tenant_id=tenant-1", nil),
		doAccountRequest(srv, http.MethodGet, "/api/v1/agent-accounts?tenant_id=tenant-1", nil),
		doAccountRequest(srv, http.MethodPost, "/api/v1/agent-accounts/acct-1/rotate?tenant_id=tenant-1",
			map[string]interface{}{"credential": sampleCredentialBody()}),
	}
	for i, w := range responses {
		body := w.Body.String()
		for _, marker := range accountSecretMarkers {
			if strings.Contains(body, marker) {
				t.Errorf("response %d leaked credential marker %q: %s", i, marker, body)
			}
		}
	}
}

// TestAccountRESTResponseTypesHaveNoCredentialField is the structural leak
// guard: it reflects over every account response type and fails if any field
// is (or transitively contains) the inbound credential shape. A future
// refactor that adds a credential field to a response breaks here even if no
// handler happens to populate it.
func TestAccountRESTResponseTypesHaveNoCredentialField(t *testing.T) {
	credentialType := reflect.TypeOf(AccountCredentialRESTRequest{})
	responseTypes := []reflect.Type{
		reflect.TypeOf(AccountRESTResponse{}),
		reflect.TypeOf(AccountEnvelopeRESTResponse{}),
		reflect.TypeOf(ListAccountsRESTResponse{}),
		reflect.TypeOf(AccountIDRESTResponse{}),
		reflect.TypeOf(DeleteAccountRESTResponse{}),
		reflect.TypeOf(GetAccountUsageRESTResponse{}),
		reflect.TypeOf(AccountUsageRESTResponse{}),
	}
	for _, rt := range responseTypes {
		if containsCredentialType(rt, credentialType, make(map[reflect.Type]bool)) {
			t.Errorf("response type %s reaches the credential shape %s", rt, credentialType)
		}
	}
}

// containsCredentialType reports whether rt is, or transitively contains,
// target. seen breaks recursive type graphs.
func containsCredentialType(rt, target reflect.Type, seen map[reflect.Type]bool) bool {
	for rt.Kind() == reflect.Pointer || rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array {
		rt = rt.Elem()
	}
	if rt == target {
		return true
	}
	if rt.Kind() != reflect.Struct || seen[rt] {
		return false
	}
	seen[rt] = true
	for i := 0; i < rt.NumField(); i++ {
		if containsCredentialType(rt.Field(i).Type, target, seen) {
			return true
		}
	}
	return false
}

// =============================================================================
// PEP mapping
// =============================================================================

// TestAccountRoutesResolveToRegisteredPermissions pins each account route to
// the registered permission the production gate enforces. The route-tree guard
// (TestPEPMappingCoversEveryMountedRoute) only proves a mapping EXISTS; this
// proves it is the intended one.
//
// Every verb takes QuicBaaS's own tenant-scoped agent_accounts:* family, never
// go-qrdb's ScopeCross credentials:*. The reads moved because every agent
// daemon is a tenant identity and could never satisfy credentials:*
// (go-quicbaas#734); the writes moved because no principal could write a
// non-system tenant's pool under it (go-quicbaas#1355).
func TestAccountRoutesResolveToRegisteredPermissions(t *testing.T) {
	cases := []struct {
		method, path, resource, action string
	}{
		{http.MethodPost, "/api/v1/agent-accounts", "agent_accounts", "write"},
		{http.MethodGet, "/api/v1/agent-accounts", "agent_accounts", "list"},
		{http.MethodGet, "/api/v1/agent-accounts/a1", "agent_accounts", "read"},
		{http.MethodPut, "/api/v1/agent-accounts/a1", "agent_accounts", "write"},
		{http.MethodDelete, "/api/v1/agent-accounts/a1", "agent_accounts", "delete"},
		{http.MethodPost, "/api/v1/agent-accounts/a1/rotate", "agent_accounts", "rotate"},
		{http.MethodPost, "/api/v1/agent-accounts/a1/revoke", "agent_accounts", "write"},
		{http.MethodPost, "/api/v1/agent-accounts/a1/default", "agent_accounts", "write"},
		{http.MethodGet, "/api/v1/agent-accounts/a1/usage", "agent_accounts", "read"},
		{http.MethodPost, "/api/v1/agent-accounts/a1/materialize", "agent_accounts", "materialize"},
	}
	for _, tc := range cases {
		resource, action, ok := routePermissionForPath(tc.path, tc.method)
		if !ok {
			t.Errorf("%s %s is unmapped (hard-403 under RequireAuthz)", tc.method, tc.path)
			continue
		}
		if resource != tc.resource || action != tc.action {
			t.Errorf("%s %s -> %s:%s, want %s:%s", tc.method, tc.path, resource, action, tc.resource, tc.action)
		}
	}
}

// TestAccountRoutesAreNotPreAuthExempt asserts the account surface is treated
// as protected in the frozen route table: neither route is RouteAuthPublic.
func TestAccountRoutesAreNotPreAuthExempt(t *testing.T) {
	cases := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/agent-accounts"},
		{http.MethodPost, "/api/v1/agent-accounts/a1/rotate"},
	}
	for _, tc := range cases {
		auth, ok := routeAuthForPath(tc.path, tc.method)
		if !ok {
			t.Errorf("%s %s is not in the frozen route table", tc.method, tc.path)
			continue
		}
		if auth == RouteAuthPublic {
			t.Errorf("account path %s %s is RouteAuthPublic; must stay gated", tc.method, tc.path)
		}
	}
}
