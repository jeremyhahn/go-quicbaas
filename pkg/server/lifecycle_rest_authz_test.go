// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

//go:build rest && iam

package server

import (
	"context"
	"errors"
	"testing"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// fakeOIDCGetter is a test double for oidcProviderGetter. It returns the
// configured provider and error without standing up a real OIDC provider, so
// the validator's error-handling paths can be exercised in a fast unit test.
type fakeOIDCGetter struct {
	provider *iam.OIDCProvider
	err      error
}

func (f *fakeOIDCGetter) GetOIDCProvider(_ context.Context) (*iam.OIDCProvider, error) {
	return f.provider, f.err
}

// errProviderUnavailable is a sentinel used to assert error wrapping.
var errProviderUnavailable = errors.New("provider boom")

// TestOIDCTokenValidator_GetterError verifies that a failure obtaining the OIDC
// provider surfaces as a typed RESTAuthzError wrapping the underlying cause,
// rather than a nil result the middleware could mistake for a valid token.
func TestOIDCTokenValidator_GetterError(t *testing.T) {
	v := &oidcTokenValidator{getter: &fakeOIDCGetter{err: errProviderUnavailable}}

	result, err := v.ValidateToken(context.Background(), "any-token")
	if result != nil {
		t.Fatalf("expected nil result on getter error, got %+v", result)
	}
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	var authzErr *RESTAuthzError
	if !errors.As(err, &authzErr) {
		t.Fatalf("expected *RESTAuthzError, got %T: %v", err, err)
	}
	if authzErr.Op != "get oidc provider" {
		t.Errorf("expected Op 'get oidc provider', got %q", authzErr.Op)
	}
	if !errors.Is(err, errProviderUnavailable) {
		t.Errorf("expected wrapped cause to be errProviderUnavailable, got %v", err)
	}
}

// TestOIDCTokenValidator_NilProvider verifies that a nil provider (IAM not yet
// initialized) yields a RESTAuthzError carrying ErrOIDCProviderUnavailable so
// the middleware responds 401 instead of panicking on a nil dereference.
func TestOIDCTokenValidator_NilProvider(t *testing.T) {
	v := &oidcTokenValidator{getter: &fakeOIDCGetter{provider: nil, err: nil}}

	result, err := v.ValidateToken(context.Background(), "any-token")
	if result != nil {
		t.Fatalf("expected nil result on nil provider, got %+v", result)
	}
	var authzErr *RESTAuthzError
	if !errors.As(err, &authzErr) {
		t.Fatalf("expected *RESTAuthzError, got %T: %v", err, err)
	}
	if !errors.Is(err, ErrOIDCProviderUnavailable) {
		t.Errorf("expected ErrOIDCProviderUnavailable, got %v", err)
	}
}

// TestRESTAuthzError_Error verifies the error string renders both with and
// without a wrapped cause and that Unwrap exposes the cause for errors.Is.
func TestRESTAuthzError_Error(t *testing.T) {
	cause := errors.New("underlying")
	withCause := &RESTAuthzError{Op: "validate access token", Cause: cause}
	if got, want := withCause.Error(), "rest authz: validate access token: underlying"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(withCause, cause) {
		t.Error("expected Unwrap to expose the cause")
	}

	noCause := &RESTAuthzError{Op: "get oidc provider"}
	if got, want := noCause.Error(), "rest authz: get oidc provider"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestConfigureRESTAuthz_NoOIDCProvider verifies that configureRESTAuthz is a
// no-op (does not panic, does not attempt to wire authz) when the service does
// not expose an OIDC provider — the storage-less / embedded boot path. The REST
// server argument is nil precisely to prove no method is invoked on it in this
// branch: a nil quicBaaSService interface fails the oidcProviderGetter type
// assertion and the function returns before touching restServer.
func TestConfigureRESTAuthz_NoOIDCProvider(t *testing.T) {
	s := &Server{
		logger:          buildTestLogger(t),
		quicBaaSService: nil, // nil does not implement oidcProviderGetter.
	}

	s.configureRESTAuthz(nil)
}

// ValidateToken returns a populated TokenValidationResult for a valid bearer
// minted by the bound OIDC provider.
func TestOIDCTokenValidator_ValidToken(t *testing.T) {
	provider := newTestOIDCProvider(t)
	issuer := newOIDCSessionIssuer()
	issuer.SetProvider(provider)

	token, _, err := issuer.Issue(context.Background(), &qrdbauth.Identity{
		Subject:  "u-1",
		TenantID: "acme",
		Roles:    []string{"admin"},
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	v := &oidcTokenValidator{getter: &fakeOIDCGetter{provider: provider}}
	result, err := v.ValidateToken(context.Background(), token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if !result.Valid || result.UserID != "u-1" || result.TenantID != "acme" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

// ValidateToken surfaces a typed error for a malformed token.
func TestOIDCTokenValidator_InvalidToken(t *testing.T) {
	provider := newTestOIDCProvider(t)
	v := &oidcTokenValidator{getter: &fakeOIDCGetter{provider: provider}}

	result, err := v.ValidateToken(context.Background(), "garbage")
	if result != nil {
		t.Fatalf("expected nil result, got %+v", result)
	}
	var authzErr *RESTAuthzError
	if !errors.As(err, &authzErr) || authzErr.Op != "validate access token" {
		t.Fatalf("expected validate-token RESTAuthzError, got %v", err)
	}
}
