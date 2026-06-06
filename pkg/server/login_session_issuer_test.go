//go:build iam

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

package server

import (
	"errors"
	"testing"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
)

// TestOIDCSessionIssuer_NilIdentity proves the issuer fails closed when the
// login resolver yields no identity, rather than minting an anonymous bearer.
// The nil-identity guard runs before the provider is touched, so an unbound
// issuer is safe here.
func TestOIDCSessionIssuer_NilIdentity(t *testing.T) {
	issuer := newOIDCSessionIssuer()

	_, _, err := issuer.Issue(t.Context(), nil)
	if err == nil {
		t.Fatal("expected error for nil identity")
	}
	var typed *NilLoginIdentityError
	if !errors.As(err, &typed) {
		t.Errorf("expected *NilLoginIdentityError, got %T: %v", err, err)
	}
}

// TestOIDCSessionIssuer_UnboundProvider proves the issuer fails closed (rather
// than minting an unsigned/anonymous bearer) when invoked before its OIDC
// provider has been bound.
func TestOIDCSessionIssuer_UnboundProvider(t *testing.T) {
	issuer := newOIDCSessionIssuer()

	_, _, err := issuer.Issue(t.Context(), &qrdbauth.Identity{Subject: "u", TenantID: "system"})
	if err == nil {
		t.Fatal("expected error when provider unbound")
	}
	var typed *LoginCeremonyUnavailableError
	if !errors.As(err, &typed) {
		t.Errorf("expected *LoginCeremonyUnavailableError, got %T: %v", err, err)
	}

	if _, err := issuer.Resolve(t.Context(), "bearer"); err == nil {
		t.Error("Resolve must fail closed when provider unbound")
	}
	if err := issuer.Revoke(t.Context(), "bearer"); err == nil {
		t.Error("Revoke must fail closed when provider unbound")
	}
}
