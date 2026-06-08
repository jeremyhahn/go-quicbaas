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
		return &PlatformTokenResult{Subject: "svc", TenantID: "t1", Scopes: []string{"keys.read"}, JKT: "jkt"}, nil
	}
	return &PlatformTokenResult{Subject: "human", TenantID: "t1", Scopes: []string{"keys.read"}}, nil
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

// SetTokenBindingEnforcement with a nil validator is a guarded no-op.
func TestServer_TokenBindingEnforcement_NilValidator(t *testing.T) {
	s, _ := newTestServer(t)
	s.SetTokenBindingEnforcement(nil)
	if s.HasAuthorizationMiddleware() {
		t.Fatal("expected no middleware installed for a nil validator")
	}
}
