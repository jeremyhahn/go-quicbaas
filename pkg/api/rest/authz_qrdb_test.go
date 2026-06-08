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
)

// recordingQRDBAuthorizer is a test double for the QRDBAuthorizer SEAM. The real
// qrdb decision is exercised against the live qrdb PDP in pkg/auth/qrdbauthz;
// this double verifies the middleware's PEP mapping and allow/deny wiring.
type recordingQRDBAuthorizer struct {
	called   bool
	subject  interfaces.Subject
	resource string
	action   string
	ret      error
}

func (a *recordingQRDBAuthorizer) AuthorizeSubject(_ context.Context, subject interfaces.Subject, resource, action string) error {
	a.called = true
	a.subject = subject
	a.resource = resource
	a.action = action
	return a.ret
}

func newQRDBMiddleware(validator *mockTokenValidator, authorizer QRDBAuthorizer) *AuthzMiddleware {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Output: "stdout", Format: "text"})
	return NewAuthzMiddleware(&AuthzMiddlewareConfig{
		Logger:         log,
		TokenValidator: validator,
		QRDBAuthorizer: authorizer,
	})
}

func validResult() *auth.TokenValidationResult {
	return &auth.TokenValidationResult{
		Valid:    true,
		UserID:   "alice",
		Username: "alice",
		TenantID: "system",
		Roles:    []string{"admin"},
	}
}

// TestQRDBAuthorize_AllowMapsCanonical asserts the qrdb path maps the request to
// the qrdb-canonical (resource, action) and forwards the verified subject, then
// admits the request when the authorizer allows.
func TestQRDBAuthorize_AllowMapsCanonical(t *testing.T) {
	validator := &mockTokenValidator{result: validResult()}
	authorizer := &recordingQRDBAuthorizer{ret: nil}
	mw := newQRDBMiddleware(validator, authorizer)

	handler := &testHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/foo", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()

	mw.Authorize(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !handler.called {
		t.Fatal("handler must be called on allow")
	}
	if !authorizer.called {
		t.Fatal("qrdb authorizer must be consulted")
	}
	if authorizer.resource != "kv" || authorizer.action != "get" {
		t.Errorf("mapped (%q,%q), want (kv,get)", authorizer.resource, authorizer.action)
	}
	if authorizer.subject.TenantID != "system" {
		t.Errorf("subject tenant = %q, want system", authorizer.subject.TenantID)
	}
}

// TestQRDBAuthorize_Deny asserts a qrdb denial produces 403 and the handler is
// not invoked.
func TestQRDBAuthorize_Deny(t *testing.T) {
	validator := &mockTokenValidator{result: validResult()}
	authorizer := &recordingQRDBAuthorizer{ret: errors.New("permission denied")}
	mw := newQRDBMiddleware(validator, authorizer)

	handler := &testHandler{}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/keys/foo", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()

	mw.Authorize(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
	if handler.called {
		t.Fatal("handler must not be called on deny")
	}
	if authorizer.action != "delete" {
		t.Errorf("mapped action = %q, want delete", authorizer.action)
	}
}

// TestQRDBAuthorize_UnmappedPathDenied asserts that under the qrdb posture an
// unmapped protected path is default-denied (403) without consulting the
// authorizer — there is no registered qrdb permission to admit it.
func TestQRDBAuthorize_UnmappedPathDenied(t *testing.T) {
	validator := &mockTokenValidator{result: validResult()}
	authorizer := &recordingQRDBAuthorizer{ret: nil}
	mw := newQRDBMiddleware(validator, authorizer)

	handler := &testHandler{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/embedding/generate", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()

	mw.Authorize(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
	if handler.called {
		t.Fatal("handler must not be called for an unmapped protected path")
	}
	if authorizer.called {
		t.Fatal("authorizer must not be consulted for an unmapped path (default-deny)")
	}
}

// TestQRDBAuthorize_PublicEndpointBypass asserts the qrdb path still bypasses
// public endpoints without consulting the authorizer.
func TestQRDBAuthorize_PublicEndpointBypass(t *testing.T) {
	validator := &mockTokenValidator{err: errors.New("must not be called")}
	authorizer := &recordingQRDBAuthorizer{ret: errors.New("must not be called")}
	mw := newQRDBMiddleware(validator, authorizer)

	handler := &testHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()

	mw.Authorize(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !handler.called {
		t.Fatal("handler must be called for a public endpoint")
	}
	if authorizer.called {
		t.Fatal("authorizer must not be consulted for a public endpoint")
	}
}

// TestUnmappedResourceError_Message asserts the typed error names the offending
// path.
func TestUnmappedResourceError_Message(t *testing.T) {
	err := &UnmappedResourceError{Path: "/api/v1/embedding/generate"}
	if got := err.Error(); got != "no qrdb permission mapping for path: /api/v1/embedding/generate" {
		t.Errorf("Error() = %q", got)
	}
}
