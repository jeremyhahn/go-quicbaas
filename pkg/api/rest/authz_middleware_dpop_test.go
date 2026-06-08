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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// mockDPoPValidator implements both TokenValidator and DPoPTokenValidator so the
// authz middleware routes through the sender-constraint-enforcing path. It echoes
// the request method + htu it was called with so tests can assert the htm/htu the
// middleware derives are forwarded to the validator.
type mockDPoPValidator struct {
	result *PlatformTokenResult
	err    error

	gotToken string
	gotProof string
	gotHTM   string
	gotHTU   string
}

func (m *mockDPoPValidator) ValidateToken(ctx context.Context, token string) (*auth.TokenValidationResult, error) {
	// Should never be invoked while the DPoP path is active; fail loudly if it is.
	return nil, errors.New("ValidateToken must not be called on the DPoP path")
}

func (m *mockDPoPValidator) ValidateTokenWithDPoP(ctx context.Context, token, dpopProof, htm, htu string) (*PlatformTokenResult, error) {
	m.gotToken = token
	m.gotProof = dpopProof
	m.gotHTM = htm
	m.gotHTU = htu
	return m.result, m.err
}

// authzCapturingHandler records the AuthorizedContext the middleware attached.
type authzCapturingHandler struct {
	called bool
	authz  *platformtoken.AuthorizedContext
}

func (h *authzCapturingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.called = true
	h.authz = AuthorizedContextFromRequest(r.Context())
	w.WriteHeader(http.StatusOK)
}

func newDPoPMiddleware(t *testing.T, validator *mockDPoPValidator) *AuthzMiddleware {
	t.Helper()
	log, _ := logger.New(logger.LogConfig{Level: "debug", Output: "stdout", Format: "text"})
	return NewAuthzMiddleware(&AuthzMiddlewareConfig{
		Logger:         log,
		TokenValidator: validator,
		AuthzService: &mockAuthzService{
			decision: &interfaces.AuthorizationDecision{Allowed: true},
		},
	})
}

// (a) cnf-bound token + a validator that accepts the proof => allowed, and the
// middleware forwards the request method + absolute URI as htm/htu.
func TestAuthzMiddleware_DPoP_BoundValid(t *testing.T) {
	validator := &mockDPoPValidator{
		result: &PlatformTokenResult{
			Subject:  "user-1",
			Username: "alice",
			TenantID: "tenant-1",
			Roles:    []string{"admin"},
			Scopes:   []string{"keys.read"},
			JKT:      "abc123",
		},
	}
	mw := newDPoPMiddleware(t, validator)
	handler := &authzCapturingHandler{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
	req.Header.Set("Authorization", "Bearer bound-token")
	req.Header.Set("DPoP", "proof-jwt")
	rr := httptest.NewRecorder()

	mw.Authorize(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !handler.called {
		t.Fatal("expected handler to be called")
	}
	if validator.gotProof != "proof-jwt" {
		t.Errorf("expected proof forwarded, got %q", validator.gotProof)
	}
	if validator.gotHTM != http.MethodGet {
		t.Errorf("expected htm GET, got %q", validator.gotHTM)
	}
	if validator.gotHTU != "http://example.com/api/v1/keys/test" {
		t.Errorf("unexpected htu %q", validator.gotHTU)
	}
	// (g) tenant_id and authz context on the request.
	if handler.authz == nil {
		t.Fatal("expected AuthorizedContext on request context")
	}
	if handler.authz.TenantID != "tenant-1" {
		t.Errorf("expected tenant-1, got %q", handler.authz.TenantID)
	}
	if handler.authz.Confirmation == nil || handler.authz.Confirmation.Jkt != "abc123" {
		t.Errorf("expected cnf.jkt abc123, got %v", handler.authz.Confirmation)
	}
}

// (b) cnf-bound token presented as a bearer (the validator rejects it) => 401,
// handler not reached.
func TestAuthzMiddleware_DPoP_BoundMissingProofRejected(t *testing.T) {
	validator := &mockDPoPValidator{
		err: &platformtoken.NotSenderConstrainedError{},
	}
	mw := newDPoPMiddleware(t, validator)
	handler := &authzCapturingHandler{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
	req.Header.Set("Authorization", "Bearer bound-token")
	// No DPoP header — bearer replay attempt.
	rr := httptest.NewRecorder()

	mw.Authorize(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if handler.called {
		t.Fatal("handler must not be called when DPoP enforcement fails")
	}
}

// (c) the validator rejects a malformed/mismatched proof => 401.
func TestAuthzMiddleware_DPoP_InvalidProofRejected(t *testing.T) {
	validator := &mockDPoPValidator{
		err: errors.New("htu mismatch"),
	}
	mw := newDPoPMiddleware(t, validator)
	handler := &authzCapturingHandler{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
	req.Header.Set("Authorization", "Bearer bound-token")
	req.Header.Set("DPoP", "bad-proof")
	rr := httptest.NewRecorder()

	mw.Authorize(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if handler.called {
		t.Fatal("handler must not be called for an invalid proof")
	}
}

// (e) plain bearer token (no cnf): the validator returns an unbound result with
// no JKT => allowed and no Confirmation on the AuthorizedContext.
func TestAuthzMiddleware_DPoP_PlainBearerAllowed(t *testing.T) {
	validator := &mockDPoPValidator{
		result: &PlatformTokenResult{
			Subject:  "user-2",
			Username: "bob",
			Roles:    []string{"reader"},
			Scopes:   []string{"keys.read"},
			// No JKT: unbound bearer.
		},
	}
	mw := newDPoPMiddleware(t, validator)
	handler := &authzCapturingHandler{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
	req.Header.Set("Authorization", "Bearer plain-bearer")
	rr := httptest.NewRecorder()

	mw.Authorize(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !handler.called {
		t.Fatal("expected handler to be called for a plain bearer")
	}
	if handler.authz == nil {
		t.Fatal("expected AuthorizedContext on request context")
	}
	if handler.authz.Confirmation != nil {
		t.Errorf("plain bearer must have no Confirmation, got %v", handler.authz.Confirmation)
	}
}

// (f) scp -> RBAC gate: RequireScope allows a granted scope and denies an absent
// one, using the AuthorizedContext the middleware attached.
func TestAuthzMiddleware_DPoP_ScopeGate(t *testing.T) {
	validator := &mockDPoPValidator{
		result: &PlatformTokenResult{
			Subject: "user-3",
			Scopes:  []string{"keys.read"},
			Roles:   []string{"reader"},
			JKT:     "k",
		},
	}
	mw := newDPoPMiddleware(t, validator)

	var got *platformtoken.AuthorizedContext
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = AuthorizedContextFromRequest(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
	req.Header.Set("Authorization", "Bearer bound-token")
	req.Header.Set("DPoP", "proof")
	rr := httptest.NewRecorder()

	mw.Authorize(handler).ServeHTTP(rr, req)

	if got == nil {
		t.Fatal("expected AuthorizedContext")
	}
	if err := got.RequireScope("keys.read"); err != nil {
		t.Errorf("granted scope should pass: %v", err)
	}
	if err := got.RequireScope("keys.admin"); err == nil {
		t.Error("absent scope must be denied")
	}
	if !got.HasRole("reader") {
		t.Error("expected reader role")
	}
}

// A validator error on the DPoP path surfaces as 401 (fail closed) when the
// result is nil.
func TestAuthzMiddleware_DPoP_ValidatorError(t *testing.T) {
	validator := &mockDPoPValidator{err: errors.New("provider unavailable")}
	mw := newDPoPMiddleware(t, validator)
	handler := &authzCapturingHandler{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
	req.Header.Set("Authorization", "Bearer t")
	rr := httptest.NewRecorder()

	mw.Authorize(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if handler.called {
		t.Fatal("handler must not be called on validator error")
	}
}

// AuthorizedContextFromRequest returns nil when nothing was attached (the
// non-DPoP / public path), and the error helpers carry messages.
func TestAuthorizedContextFromRequest_Absent(t *testing.T) {
	if AuthorizedContextFromRequest(context.Background()) != nil {
		t.Error("expected nil AuthorizedContext on a bare context")
	}
	if (&InvalidTokenResultError{}).Error() == "" {
		t.Error("expected message")
	}
	if (&RevokedTokenError{}).Error() == "" {
		t.Error("expected message")
	}
}

// EnforceTokenBinding bypasses public endpoints without invoking the validator.
func TestEnforceTokenBinding_PublicBypass(t *testing.T) {
	validator := &mockDPoPValidator{err: errors.New("must not be called")}
	mw := newDPoPMiddleware(t, validator)
	handler := &authzCapturingHandler{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()

	mw.EnforceTokenBinding(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK || !handler.called {
		t.Fatalf("expected public bypass to reach handler, got %d called=%v", rr.Code, handler.called)
	}
	if validator.gotToken != "" {
		t.Error("validator must not be called on a public endpoint")
	}
}

// EnforceTokenBinding is a sender-constraint BINDING middleware, not an
// authentication gate: a request with NO Authorization header passes through
// unchanged (the handler is invoked). It only validates/enforces binding on
// tokens that ARE presented. This preserves the data plane's prior posture of
// accepting unauthenticated requests; the "require auth" decision lives in the
// separate Authorize gate.
func TestEnforceTokenBinding_MissingHeader_PassesThrough(t *testing.T) {
	mw := newDPoPMiddleware(t, &mockDPoPValidator{})
	handler := &authzCapturingHandler{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/x", nil)
	rr := httptest.NewRecorder()

	mw.EnforceTokenBinding(handler).ServeHTTP(rr, req)

	if !handler.called {
		t.Fatalf("expected token-less request to pass through to the handler, got code=%d called=%v", rr.Code, handler.called)
	}
}
