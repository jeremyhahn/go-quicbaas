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

//go:build rest && iam

package server

import (
	"context"
	"errors"
	"testing"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// ValidateTokenWithDPoP wraps a getter failure as a typed RESTAuthzError.
func TestOIDCTokenValidator_DPoP_GetterError(t *testing.T) {
	v := &oidcTokenValidator{getter: &fakeOIDCGetter{err: errProviderUnavailable}}

	result, err := v.ValidateTokenWithDPoP(context.Background(), "tok", "", "GET", "https://h/p")
	if result != nil {
		t.Fatalf("expected nil result, got %+v", result)
	}
	var authzErr *RESTAuthzError
	if !errors.As(err, &authzErr) || authzErr.Op != "get oidc provider" {
		t.Fatalf("expected get-provider RESTAuthzError, got %v", err)
	}
	if !errors.Is(err, errProviderUnavailable) {
		t.Errorf("expected wrapped cause, got %v", err)
	}
}

// ValidateTokenWithDPoP surfaces ErrOIDCProviderUnavailable on a nil provider.
func TestOIDCTokenValidator_DPoP_NilProvider(t *testing.T) {
	v := &oidcTokenValidator{getter: &fakeOIDCGetter{provider: nil}}

	_, err := v.ValidateTokenWithDPoP(context.Background(), "tok", "", "GET", "https://h/p")
	if !errors.Is(err, ErrOIDCProviderUnavailable) {
		t.Fatalf("expected ErrOIDCProviderUnavailable, got %v", err)
	}
}

// ValidateTokenWithDPoP returns a populated PlatformTokenResult for an unbound
// (plain bearer) token: no proof required, no JKT on the result.
func TestOIDCTokenValidator_DPoP_PlainBearer(t *testing.T) {
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
	result, err := v.ValidateTokenWithDPoP(context.Background(), token, "", "GET", "https://h/api/v1/keys")
	if err != nil {
		t.Fatalf("ValidateTokenWithDPoP: %v", err)
	}
	if result.Subject != "u-1" || result.TenantID != "acme" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.JKT != "" {
		t.Errorf("plain bearer must carry no JKT, got %q", result.JKT)
	}
}

// ValidateTokenWithDPoP wraps a malformed token as a typed RESTAuthzError.
func TestOIDCTokenValidator_DPoP_BadToken(t *testing.T) {
	provider := newTestOIDCProvider(t)
	v := &oidcTokenValidator{getter: &fakeOIDCGetter{provider: provider}}

	_, err := v.ValidateTokenWithDPoP(context.Background(), "garbage", "", "GET", "https://h/p")
	var authzErr *RESTAuthzError
	if !errors.As(err, &authzErr) || authzErr.Op != "validate access token (dpop)" {
		t.Fatalf("expected validate-token-dpop RESTAuthzError, got %v", err)
	}
}

// tokenScopes prefers scp over the space-separated scope string and handles the
// empty case.
func TestTokenScopes(t *testing.T) {
	if got := tokenScopes(&iam.Claims{SCP: []string{"a", "b"}, Scope: "x y"}); len(got) != 2 || got[0] != "a" {
		t.Errorf("expected scp to win, got %v", got)
	}
	if got := tokenScopes(&iam.Claims{Scope: "read write"}); len(got) != 2 || got[1] != "write" {
		t.Errorf("expected scope split, got %v", got)
	}
	if got := tokenScopes(&iam.Claims{}); got != nil {
		t.Errorf("expected nil for no scopes, got %v", got)
	}
}
