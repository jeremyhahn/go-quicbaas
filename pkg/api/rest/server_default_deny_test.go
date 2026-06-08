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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/stretchr/testify/mock"
)

// SetDefaultDenyEnforcement installs the path-predicate gate on the TOP router so
// it covers root-mounted protected surfaces a /api/v1-scoped gate would miss. A
// token-less request to a protected path is DENIED (the point of default-deny),
// while a public probe and a valid-token+scope request pass.
func TestServer_DefaultDenyEnforcement(t *testing.T) {
	s, mockSvc := newTestServer(t)
	s.config.SPIFFETrustDomain = testTrustDomain
	s.SetDefaultDenyEnforcement(bindingValidator{})
	// The public liveness probe reaches the real handler (the gate allows it);
	// give the mock a response so it returns 200 rather than panicking.
	mockSvc.On("Liveness", mock.Anything, mock.Anything).Return(&service.LivenessResponse{Alive: true}, nil)

	if !s.HasAuthorizationMiddleware() {
		t.Fatal("expected default-deny middleware installed")
	}
	if !s.defaultDenyEnforcement {
		t.Fatal("expected defaultDenyEnforcement flag set")
	}

	tests := []struct {
		name       string
		method     string
		path       string
		authz      string
		wantDenied bool // 401/403 expected
	}{
		{"public liveness probe", http.MethodGet, "/api/v1/health/live", "", false},
		{"token-less protected denied", http.MethodGet, "/api/v1/keys/k1", "", true},
		{"token-less root-mounted credential denied", http.MethodGet, "/v1/credentials/strategy", "", true},
		{"valid token with scope passes", http.MethodGet, "/api/v1/keys/k1", "Bearer human", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.authz != "" {
				req.Header.Set("Authorization", tc.authz)
			}
			w := httptest.NewRecorder()
			s.router.ServeHTTP(w, req)

			denied := w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden
			if denied != tc.wantDenied {
				t.Fatalf("%s %s: denied=%v (code %d), want denied=%v", tc.method, tc.path, denied, w.Code, tc.wantDenied)
			}
		})
	}
}

// SetDefaultDenyEnforcement with a nil validator is a guarded no-op.
func TestServer_DefaultDenyEnforcement_NilValidator(t *testing.T) {
	s, _ := newTestServer(t)
	s.SetDefaultDenyEnforcement(nil)
	if s.HasAuthorizationMiddleware() {
		t.Fatal("expected no middleware for a nil validator")
	}
	if s.defaultDenyEnforcement {
		t.Fatal("expected defaultDenyEnforcement to remain false")
	}
}

// Flag-off path: with token-binding enforcement (the default posture), a
// token-less request to a protected path PASSES THROUGH unchanged (anonymous
// access preserved). This guards that the default-deny work did not alter the
// shipped behavior.
func TestServer_FlagOff_AnonymousPassesThrough(t *testing.T) {
	s, _ := newTestServer(t)
	s.SetTokenBindingEnforcement(bindingValidator{})

	if s.defaultDenyEnforcement {
		t.Fatal("default-deny must be off after SetTokenBindingEnforcement")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/mykey", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
		t.Fatalf("flag-off: anonymous request must pass through, got %d", w.Code)
	}
}
