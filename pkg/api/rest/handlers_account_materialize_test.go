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
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// =============================================================================
// POST /api/v1/agent-accounts/{id}/materialize (go-quicbaas#734)
//
// The route's whole security property is that TENANCY COMES FROM THE
// PRINCIPAL: an enrolled agent daemon gets its own tenant's pool and nothing
// else, whatever the request says. These tests drive the real handler through
// the real chi mount with a principal injected exactly the way the authz
// middleware injects one.
// =============================================================================

// mountMaterializeRoute mounts the release route on a bare router.
func mountMaterializeRoute(srv *Server, r chi.Router) {
	r.Route("/agent-accounts", func(r chi.Router) {
		r.Post("/{id}/materialize", srv.HandleMaterializeAccountCredential)
	})
}

// newMaterializeTestServer builds a Server whose only wired sub-service is the
// supplied AccountService, with the release route mounted under /api/v1 and
// principal injection driven by subject.
//
// A zero Subject means "no principal at all", which is how an unauthenticated
// request reaches a handler on this surface.
func newMaterializeTestServer(accountSvc service.AccountService, subject interfaces.Subject) *Server {
	mock := mocks.NewMockQuicBaaS()
	mock.AccountServiceFunc = func() service.AccountService { return accountSvc }

	router := chi.NewRouter()
	srv := &Server{service: mock, router: router}
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !subject.Authenticated {
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(injectPrincipal(r.Context(), subject, nil)))
		})
	})
	router.Route("/api/v1", func(r chi.Router) {
		mountMaterializeRoute(srv, r)
	})
	return srv
}

// daemonSubject is the principal an enrolled agent daemon presents.
func daemonSubject(tenantID string) interfaces.Subject {
	return interfaces.Subject{
		ID:            "agent-vm-host-4",
		Username:      "agent-vm-host-4",
		TenantID:      tenantID,
		Roles:         []string{"role-agent-runtime-" + tenantID},
		Authenticated: true,
	}
}

// releasingAccountService returns an AccountService that releases a
// recognizable envelope and records the request it was asked to release.
func releasingAccountService(captured **service.MaterializeAccountCredentialRequest) *mocks.MockAccountService {
	return &mocks.MockAccountService{
		MaterializeAccountCredentialFunc: func(
			_ context.Context, req *service.MaterializeAccountCredentialRequest,
		) (*service.MaterializeAccountCredentialResponse, error) {
			if captured != nil {
				*captured = req
			}
			return &service.MaterializeAccountCredentialResponse{
				AccountID:     req.AccountID,
				Provider:      orchestration.AccountProviderClaude,
				DisplayName:   "alice-work",
				RefreshPolicy: orchestration.AccountCredentialModeStaticToken,
				Credential: &orchestration.AccountCredential{
					AccessToken:       "released-token",
					AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
					// A refresh grant the handler must NOT project onto the
					// wire: the daemon has no business renewing.
					RefreshToken:  "refresh-must-not-travel",
					TokenEndpoint: "https://example.invalid/token",
					ClientID:      "client-must-not-travel",
				},
				ExpiresAt: time.Now().UTC().Add(time.Hour),
				UpdatedAt: time.Now().UTC(),
				Timestamp: time.Now().UTC(),
			}, nil
		},
	}
}

// doMaterialize issues a release request against the mounted route.
func doMaterialize(srv *Server, target string, body interface{}) *httptest.ResponseRecorder {
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
	r := httptest.NewRequest(http.MethodPost, target, reader)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, r)
	return w
}

// TestMaterializeREST_ReleasesToTheCallersOwnTenant is the happy path.
func TestMaterializeREST_ReleasesToTheCallersOwnTenant(t *testing.T) {
	var captured *service.MaterializeAccountCredentialRequest
	srv := newMaterializeTestServer(releasingAccountService(&captured), daemonSubject("tenant-1"))

	w := doMaterialize(srv, "/api/v1/agent-accounts/acct-1/materialize?tenant_id=tenant-1",
		map[string]interface{}{"project_id": "project-x"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var resp MaterializeAccountCredentialRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Credential == nil || resp.Credential.AccessToken == "" {
		t.Fatal("response carried no credential envelope")
	}
	if resp.Credential.AccessTokenEnvKey != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Errorf("envelope projects into %q, want CLAUDE_CODE_OAUTH_TOKEN", resp.Credential.AccessTokenEnvKey)
	}

	if captured == nil {
		t.Fatal("service was not called")
	}
	if captured.TenantID != "tenant-1" {
		t.Errorf("service asked for tenant %q, want the principal's tenant-1", captured.TenantID)
	}
	if captured.AgentID != "agent-vm-host-4" {
		t.Errorf("service attributed the release to %q, want the principal", captured.AgentID)
	}
	if captured.ProjectID != "project-x" {
		t.Errorf("service asked for project %q, want project-x", captured.ProjectID)
	}
}

// TestMaterializeREST_DoesNotShipTheRefreshGrant is the structural leak guard
// for the release path: the daemon receives the short-lived half of the
// credential and nothing that would let it renew on its own.
func TestMaterializeREST_DoesNotShipTheRefreshGrant(t *testing.T) {
	srv := newMaterializeTestServer(releasingAccountService(nil), daemonSubject("tenant-1"))

	w := doMaterialize(srv, "/api/v1/agent-accounts/acct-1/materialize?tenant_id=tenant-1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	for _, forbidden := range []string{
		"refresh_token", "refresh-must-not-travel",
		"token_endpoint", "client_id", "client-must-not-travel",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("release response carries %q, which a daemon must never receive", forbidden)
		}
	}
}

// TestMaterializeREST_MarksTheResponseUncacheable proves a live provider login
// is never parked in an intermediary's cache.
func TestMaterializeREST_MarksTheResponseUncacheable(t *testing.T) {
	srv := newMaterializeTestServer(releasingAccountService(nil), daemonSubject("tenant-1"))

	w := doMaterialize(srv, "/api/v1/agent-accounts/acct-1/materialize?tenant_id=tenant-1", nil)
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

// TestMaterializeREST_RefusesAnotherTenantsQuery proves the query parameter is
// not an identity source: naming another tenant is refused rather than honoured
// or silently ignored.
func TestMaterializeREST_RefusesAnotherTenantsQuery(t *testing.T) {
	var captured *service.MaterializeAccountCredentialRequest
	srv := newMaterializeTestServer(releasingAccountService(&captured), daemonSubject("tenant-1"))

	w := doMaterialize(srv, "/api/v1/agent-accounts/acct-1/materialize?tenant_id=tenant-2", nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}
	if captured != nil {
		t.Fatal("a cross-tenant request reached the service")
	}
}

// TestMaterializeREST_RefusesAnUnauthenticatedCaller proves the route is not
// reachable without a principal, which is what makes tenancy meaningful.
func TestMaterializeREST_RefusesAnUnauthenticatedCaller(t *testing.T) {
	var captured *service.MaterializeAccountCredentialRequest
	srv := newMaterializeTestServer(releasingAccountService(&captured), interfaces.Subject{})

	w := doMaterialize(srv, "/api/v1/agent-accounts/acct-1/materialize?tenant_id=tenant-1", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", w.Code, w.Body.String())
	}
	if captured != nil {
		t.Fatal("an unauthenticated request reached the service")
	}
}

// TestMaterializeREST_RefusesASystemTenantCaller proves a principal with no
// tenant binding cannot release a tenant's credential: the release must name
// the tenant that owns the pool.
func TestMaterializeREST_RefusesASystemTenantCaller(t *testing.T) {
	var captured *service.MaterializeAccountCredentialRequest
	subject := daemonSubject("")
	srv := newMaterializeTestServer(releasingAccountService(&captured), subject)

	w := doMaterialize(srv, "/api/v1/agent-accounts/acct-1/materialize", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	if captured != nil {
		t.Fatal("a tenant-less principal reached the service")
	}
}

// TestMaterializeREST_MapsRefusalsToStatuses pins the status each refusal takes.
// The distinction matters operationally: a 409 clears itself, a 403 needs a
// configuration change, a 503 is the deployment's fault and not the caller's.
func TestMaterializeREST_MapsRefusalsToStatuses(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "revoked account",
			err:  &service.AccountNotReleasableError{AccountID: "acct-1", Status: orchestration.AccountStatusRevoked},
			want: http.StatusConflict,
		},
		{
			name: "limit hold",
			err: &service.AccountNotReleasableError{
				AccountID: "acct-1", Status: orchestration.AccountStatusLimited,
				AvailableAt: time.Now().UTC().Add(time.Hour),
			},
			want: http.StatusConflict,
		},
		{
			name: "project fence",
			err:  &service.AccountProjectScopeError{AccountID: "acct-1", AccountProjectID: "project-x"},
			want: http.StatusForbidden,
		},
		{
			name: "no such account",
			err:  &service.AccountNotFoundError{TenantID: "tenant-1", AccountID: "acct-1"},
			want: http.StatusNotFound,
		},
		{
			name: "pool not wired",
			err:  &service.AccountTokenManagerNotConfiguredError{},
			want: http.StatusServiceUnavailable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &mocks.MockAccountService{
				MaterializeAccountCredentialFunc: func(
					_ context.Context, _ *service.MaterializeAccountCredentialRequest,
				) (*service.MaterializeAccountCredentialResponse, error) {
					return nil, tc.err
				},
			}
			srv := newMaterializeTestServer(svc, daemonSubject("tenant-1"))
			w := doMaterialize(srv, "/api/v1/agent-accounts/acct-1/materialize?tenant_id=tenant-1", nil)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

// TestMaterializeREST_RejectsAMalformedBody covers the bad-input path: an
// unparsable body must not be treated as "no optional fields supplied".
func TestMaterializeREST_RejectsAMalformedBody(t *testing.T) {
	var captured *service.MaterializeAccountCredentialRequest
	srv := newMaterializeTestServer(releasingAccountService(&captured), daemonSubject("tenant-1"))

	r := httptest.NewRequest(http.MethodPost,
		"/api/v1/agent-accounts/acct-1/materialize?tenant_id=tenant-1",
		bytes.NewReader([]byte("{not json")))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	if captured != nil {
		t.Fatal("a malformed request reached the service")
	}
}

// TestAccountCredentialToREST_DropsTheRefreshGrant is the unit-level guard on
// the converter itself, so the property survives a handler refactor.
func TestAccountCredentialToREST_DropsTheRefreshGrant(t *testing.T) {
	out := accountCredentialToREST(&orchestration.AccountCredential{
		AccessToken:       "released",
		RefreshToken:      "must-not-travel",
		TokenEndpoint:     "https://example.invalid/token",
		ClientID:          "client",
		AccessTokenEnvKey: "K",
		Env:               map[string]string{"A": "b"},
	})
	if out == nil {
		t.Fatal("converter returned nil for a populated envelope")
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), "must-not-travel") {
		t.Error("converted envelope carries the refresh token")
	}
	if out.AccessToken == "" || out.AccessTokenEnvKey != "K" {
		t.Error("converted envelope dropped the material a spawn needs")
	}
	if accountCredentialToREST(nil) != nil {
		t.Error("converter fabricated an envelope from nil")
	}
}
