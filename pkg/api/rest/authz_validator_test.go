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
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// recordingValidatorAuthorizer is a test double for the ValidatorAuthorizer SEAM.
// The real offline-scp + liveness decision is exercised against the live
// Validator in sdk/go/platformtoken; this double verifies the middleware's PEP
// mapping, error -> status wiring, and principal injection.
type recordingValidatorAuthorizer struct {
	called   bool
	gotToken string
	gotPerm  string
	gotDPoP  string
	gotHTM   string
	gotHTU   string
	ret      *platformtoken.AuthorizedContext
	retErr   error
}

func (a *recordingValidatorAuthorizer) Authorize(_ context.Context, token, requiredPerm, dpopProof, htm, htu string) (*platformtoken.AuthorizedContext, error) {
	a.called = true
	a.gotToken = token
	a.gotPerm = requiredPerm
	a.gotDPoP = dpopProof
	a.gotHTM = htm
	a.gotHTU = htu
	return a.ret, a.retErr
}

func newValidatorMiddleware(authorizer ValidatorAuthorizer) *AuthzMiddleware {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Output: "stdout", Format: "text"})
	return NewAuthzMiddleware(&AuthzMiddlewareConfig{
		Logger:              log,
		ValidatorAuthorizer: authorizer,
	})
}

// TestValidatorAuthorize_AllowMapsCanonical asserts the validator gate maps the
// request to the qrdb-canonical "resource:action", forwards the DPoP proof + htm
// + htu, admits the request on allow, and injects the reconstructed principal.
func TestValidatorAuthorize_AllowMapsCanonical(t *testing.T) {
	authorizer := &recordingValidatorAuthorizer{
		ret: &platformtoken.AuthorizedContext{
			Subject:  "alice",
			TenantID: "system",
			Scopes:   []string{"kv:get"},
			Roles:    []string{"admin"},
		},
	}
	mw := newValidatorMiddleware(authorizer)

	handler := &testHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/foo", nil)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("DPoP", "proof123")
	rr := httptest.NewRecorder()

	mw.AuthorizeViaValidator(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !handler.called {
		t.Fatal("handler must be called on allow")
	}
	if !authorizer.called {
		t.Fatal("validator authorizer must be consulted")
	}
	if authorizer.gotPerm != "kv:get" {
		t.Errorf("requiredPerm = %q, want kv:get", authorizer.gotPerm)
	}
	if authorizer.gotToken != "tok" {
		t.Errorf("token = %q, want tok", authorizer.gotToken)
	}
	if authorizer.gotDPoP != "proof123" {
		t.Errorf("dpop = %q, want proof123", authorizer.gotDPoP)
	}
	if authorizer.gotHTM != http.MethodGet {
		t.Errorf("htm = %q, want GET", authorizer.gotHTM)
	}
	if authorizer.gotHTU == "" {
		t.Error("htu must be the absolute request URI, got empty")
	}
	if handler.userID != "alice" {
		t.Errorf("injected user id = %q, want alice", handler.userID)
	}
}

// TestValidatorAuthorize_SubjectInjectedAuthenticated asserts the reconstructed
// Subject is authenticated and carries the AuthorizedContext identity so the
// service layer (auth.SubjectFromContext) sees an authenticated principal.
func TestValidatorAuthorize_SubjectInjectedAuthenticated(t *testing.T) {
	authorizer := &recordingValidatorAuthorizer{
		ret: &platformtoken.AuthorizedContext{
			Subject:  "svc-1",
			TenantID: "tenant-a",
			Scopes:   []string{"kv:get"},
		},
	}
	mw := newValidatorMiddleware(authorizer)

	var gotSubjAuthenticated bool
	var gotSubjID, gotSubjTenant string
	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subj, ok := auth.SubjectFromContext(r.Context()); ok {
			gotSubjAuthenticated = subj.Authenticated
			gotSubjID = subj.ID
			gotSubjTenant = subj.TenantID
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/foo", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()

	mw.AuthorizeViaValidator(probe).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !gotSubjAuthenticated {
		t.Error("reconstructed subject must be Authenticated")
	}
	if gotSubjID != "svc-1" || gotSubjTenant != "tenant-a" {
		t.Errorf("subject = (%q,%q), want (svc-1,tenant-a)", gotSubjID, gotSubjTenant)
	}
}

// TestValidatorAuthorize_ScopeDeniedIs403 asserts a ScopeDeniedError maps to 403
// and the handler is not invoked.
func TestValidatorAuthorize_ScopeDeniedIs403(t *testing.T) {
	authorizer := &recordingValidatorAuthorizer{retErr: &platformtoken.ScopeDeniedError{Required: "kv:delete"}}
	mw := newValidatorMiddleware(authorizer)

	handler := &testHandler{}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/keys/foo", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()

	mw.AuthorizeViaValidator(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
	if handler.called {
		t.Fatal("handler must not be called on scope denial")
	}
	if authorizer.gotPerm != "kv:delete" {
		t.Errorf("requiredPerm = %q, want kv:delete", authorizer.gotPerm)
	}
}

// TestValidatorAuthorize_TokenInactiveIs401 asserts a TokenInactiveError (revoked
// / subject disabled) maps to 401.
func TestValidatorAuthorize_TokenInactiveIs401(t *testing.T) {
	authorizer := &recordingValidatorAuthorizer{retErr: &platformtoken.TokenInactiveError{Subject: "alice", JTI: "j1"}}
	mw := newValidatorMiddleware(authorizer)

	handler := &testHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/foo", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()

	mw.AuthorizeViaValidator(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	if handler.called {
		t.Fatal("handler must not be called for an inactive token")
	}
}

// TestValidatorAuthorize_ConfigErrorIs500 asserts a ConfigError (server
// misconfiguration) fails closed with 500 and does not invoke the handler.
func TestValidatorAuthorize_ConfigErrorIs500(t *testing.T) {
	authorizer := &recordingValidatorAuthorizer{retErr: &platformtoken.ConfigError{Detail: "liveness checker required"}}
	mw := newValidatorMiddleware(authorizer)

	handler := &testHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/foo", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()

	mw.AuthorizeViaValidator(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	if handler.called {
		t.Fatal("handler must not be called on misconfiguration (fail closed)")
	}
}

// TestValidatorAuthorize_GenericValidationErrorIs401 asserts any other validation
// failure (bad signature / issuer / audience / expiry / DPoP) maps to 401.
func TestValidatorAuthorize_GenericValidationErrorIs401(t *testing.T) {
	authorizer := &recordingValidatorAuthorizer{retErr: &platformtoken.InvalidTokenError{Detail: "signature"}}
	mw := newValidatorMiddleware(authorizer)

	handler := &testHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/foo", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()

	mw.AuthorizeViaValidator(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	if handler.called {
		t.Fatal("handler must not be called on a validation error")
	}
}

// TestValidatorAuthorize_MissingBearerIs401 asserts a protected request with no
// bearer token is rejected 401 before the validator is consulted.
func TestValidatorAuthorize_MissingBearerIs401(t *testing.T) {
	authorizer := &recordingValidatorAuthorizer{}
	mw := newValidatorMiddleware(authorizer)

	handler := &testHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/foo", nil)
	rr := httptest.NewRecorder()

	mw.AuthorizeViaValidator(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	if authorizer.called {
		t.Fatal("validator must not be consulted without a bearer token")
	}
}

// TestValidatorAuthorize_UnmappedPathDenied asserts an unmapped protected path is
// default-denied (403) without consulting the validator: there is no registered
// permission to admit it.
func TestValidatorAuthorize_UnmappedPathDenied(t *testing.T) {
	authorizer := &recordingValidatorAuthorizer{}
	mw := newValidatorMiddleware(authorizer)

	handler := &testHandler{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/embedding/generate", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()

	mw.AuthorizeViaValidator(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
	if handler.called {
		t.Fatal("handler must not be called for an unmapped protected path")
	}
	if authorizer.called {
		t.Fatal("validator must not be consulted for an unmapped path (default-deny)")
	}
}

// TestValidatorAuthorize_PublicEndpointBypass asserts public endpoints bypass the
// validator gate entirely.
func TestValidatorAuthorize_PublicEndpointBypass(t *testing.T) {
	authorizer := &recordingValidatorAuthorizer{retErr: errors.New("must not be called")}
	mw := newValidatorMiddleware(authorizer)

	handler := &testHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()

	mw.AuthorizeViaValidator(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !handler.called {
		t.Fatal("handler must be called for a public endpoint")
	}
	if authorizer.called {
		t.Fatal("validator must not be consulted for a public endpoint")
	}
}

// TestValidatorErrorStatus_Mapping asserts the typed error -> HTTP status mapping
// directly, including the fail-closed default for an unrecognised error.
func TestValidatorErrorStatus_Mapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"scope-denied", &platformtoken.ScopeDeniedError{Required: "x:y"}, http.StatusForbidden},
		{"inactive", &platformtoken.TokenInactiveError{}, http.StatusUnauthorized},
		{"config", &platformtoken.ConfigError{Detail: "x"}, http.StatusInternalServerError},
		{"invalid-token", &platformtoken.InvalidTokenError{Detail: "sig"}, http.StatusUnauthorized},
		{"expired", &platformtoken.ExpiredTokenError{}, http.StatusUnauthorized},
		{"liveness-backend", &platformtoken.LivenessCheckError{Cause: errors.New("net")}, http.StatusUnauthorized},
		{"unknown", errors.New("boom"), http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validatorErrorStatus(tc.err); got != tc.want {
				t.Errorf("validatorErrorStatus(%s) = %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}

// TestAuthzMisconfiguredError_Message asserts the 500-mapped misconfig error
// carries a stable message.
func TestAuthzMisconfiguredError_Message(t *testing.T) {
	if got := (&AuthzMisconfiguredError{}).Error(); got != "authorization is misconfigured" {
		t.Errorf("Error() = %q", got)
	}
}
