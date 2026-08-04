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
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// bindingValidator routes through the DPoP-aware path: a token whose value is a
// known bound-token id requires a non-empty proof, otherwise it is treated as an
// unbound bearer.
type bindingValidator struct{}

func (bindingValidator) ValidateToken(ctx context.Context, token string) (*auth.TokenValidationResult, error) {
	return nil, &InvalidTokenResultError{}
}

func (bindingValidator) ValidateTokenWithDPoP(ctx context.Context, token, dpopProof, htm, htu string) (*PlatformTokenResult, error) {
	if token == "bound" {
		if dpopProof == "" {
			// Sender-constrained token presented as a bearer: fail closed.
			return nil, &platformtoken.NotSenderConstrainedError{}
		}
		return &PlatformTokenResult{Subject: "svc", TenantID: "t1", Scopes: []string{"kv:get"}, JKT: "jkt"}, nil
	}
	return &PlatformTokenResult{Subject: "human", TenantID: "t1", Scopes: []string{"kv:get"}}, nil
}

// SetTokenBindingEnforcement installs EnforceTokenBinding on the live router: a
// bound token without a proof is rejected 401, while a bound token with a proof,
// a plain bearer, AND a token-less (anonymous) request all reach the route layer
// (here a 404 since no entity route matches, proving the middleware passed the
// request through). The middleware enforces sender-constraint BINDING only on
// presented tokens; it is not an authentication gate, so anonymous requests are
// not blocked here.
func TestServer_TokenBindingEnforcement(t *testing.T) {
	s, _ := newTestServer(t)
	s.SetTokenBindingEnforcement(bindingValidator{})

	if !s.HasAuthorizationMiddleware() {
		t.Fatal("expected binding-enforcement middleware to be installed")
	}

	tests := []struct {
		name        string
		authz       string
		dpop        string
		wantBlocked bool
	}{
		{"bound token without proof rejected", "Bearer bound", "", true},
		{"bound token with proof passes middleware", "Bearer bound", "proof", false},
		{"plain bearer passes middleware", "Bearer human", "", false},
		{"missing auth header passes through (binding-only, not an auth gate)", "", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/mykey", nil)
			if tc.authz != "" {
				req.Header.Set("Authorization", tc.authz)
			}
			if tc.dpop != "" {
				req.Header.Set("DPoP", tc.dpop)
			}
			w := httptest.NewRecorder()
			s.router.ServeHTTP(w, req)

			if tc.wantBlocked {
				if w.Code != http.StatusUnauthorized {
					t.Fatalf("expected 401, got %d", w.Code)
				}
			} else if w.Code == http.StatusUnauthorized {
				t.Fatalf("middleware blocked a request it should have passed: %d", w.Code)
			}
		})
	}
}

// The root-mounted IAM management group (/api/v1/tenants, registered in
// iam.go) must receive the SAME authorization chain as the /api/v1 chi group
// (useAPIAuthzMiddleware). Under the binding posture (RequireAuthz=false) a
// bound token presented without its DPoP proof is rejected 401 on the IAM
// group exactly as on the /api/v1 group, and a valid bearer has its principal
// injected before the route layer — the seam the service-layer tenant-binding
// gates depend on.
func TestServer_TokenBindingEnforcement_IAMTenantsGroup(t *testing.T) {
	s, _ := newTestServer(t)
	s.SetTokenBindingEnforcement(bindingValidator{})

	tests := []struct {
		name        string
		path        string
		authz       string
		dpop        string
		wantBlocked bool
	}{
		{"bound token without proof rejected on tenants list", "/api/v1/tenants", "Bearer bound", "", true},
		{"bound token without proof rejected on tenant barrier status", "/api/v1/tenants/t1/barrier/status", "Bearer bound", "", true},
		{"bound token with proof passes middleware", "/api/v1/tenants/t1/barrier/status", "Bearer bound", "proof", false},
		{"plain bearer passes middleware", "/api/v1/tenants/t1/barrier/status", "Bearer human", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.authz != "" {
				req.Header.Set("Authorization", tc.authz)
			}
			if tc.dpop != "" {
				req.Header.Set("DPoP", tc.dpop)
			}
			w := httptest.NewRecorder()
			s.router.ServeHTTP(w, req)

			if tc.wantBlocked {
				if w.Code != http.StatusUnauthorized {
					t.Fatalf("expected 401 on %s, got %d", tc.path, w.Code)
				}
			} else if w.Code == http.StatusUnauthorized {
				t.Fatalf("middleware blocked a request it should have passed on %s: %d", tc.path, w.Code)
			}
		})
	}
}

// Under the validator posture (RequireAuthz=true) the IAM tenants group is
// fail-closed through the SAME AuthorizeViaValidator gate as the /api/v1
// group: the PEP maps the per-tenant barrier status path to
// tenant_barrier:status, a scope denial surfaces as 403, and a token-less
// request is rejected 401 — proving no unauthenticated path into the IAM
// management surface remains.
func TestServer_ValidatorEnforcement_IAMTenantsGroup(t *testing.T) {
	s, _ := newTestServer(t)
	authorizer := &recordingValidatorAuthorizer{retErr: &platformtoken.ScopeDeniedError{Required: "tenant_barrier:status"}}
	s.SetValidatorAuthzEnforcement(authorizer)

	t.Run("scope denial is 403 with canonical permission", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/barrier/status", nil)
		req.Header.Set("Authorization", "Bearer tok")
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", w.Code)
		}
		if !authorizer.called {
			t.Fatal("validator authorizer must be consulted for the IAM tenants group")
		}
		if authorizer.gotPerm != "tenant_barrier:status" {
			t.Errorf("requiredPerm = %q, want tenant_barrier:status", authorizer.gotPerm)
		}
	})

	t.Run("token-less request is 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/t1/barrier/init", nil)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for token-less request, got %d", w.Code)
		}
	})
}

// SetTokenBindingEnforcement with a nil validator is a guarded no-op.
func TestServer_TokenBindingEnforcement_NilValidator(t *testing.T) {
	s, _ := newTestServer(t)
	s.SetTokenBindingEnforcement(nil)
	if s.HasAuthorizationMiddleware() {
		t.Fatal("expected no middleware installed for a nil validator")
	}
}
