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

package server

import (
	"context"
	"errors"
	"testing"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestOIDCSessionIssuer_RefusesNonSystemTenantLogin checks that a verified
// login for a tenant other than the system tenant is refused before a bearer
// is minted, with the typed refusal and a PreconditionFailed code. The user is
// seeded, so the refusal and not an unresolvable principal is what stops it.
// go-quicbaas #1816 removes the refusal.
func TestOIDCSessionIssuer_RefusesNonSystemTenantLogin(t *testing.T) {
	provider, users := newTestOIDCProviderWithUsers(t)
	seedSessionUser(t, users, "acme", "user-42", []string{"admin"})
	issuer := newOIDCSessionIssuer(nil)
	issuer.SetProvider(provider)

	token, _, err := issuer.Issue(context.Background(), &qrdbauth.Identity{
		Subject:  "user-42",
		TenantID: "acme",
		Metadata: map[string]string{authMethodMetadataKey: "fido2"},
	})
	var refused *sdkerrors.TenantLoginRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("Issue error = %v, want TenantLoginRefusedError", err)
	}
	var coded *dberrors.QBError
	if !errors.As(err, &coded) || coded.Code != dberrors.ErrPreconditionFailed {
		t.Errorf("Issue error = %v, want a PreconditionFailed QBError", err)
	}
	if token != "" {
		t.Error("a refused login must issue no bearer")
	}
}

// TestOIDCSessionIssuer_SystemTenantLoginIssuesBearer checks that the refusal
// leaves the system tenant's login untouched: it still issues a bearer that
// resolves back to its subject.
func TestOIDCSessionIssuer_SystemTenantLoginIssuesBearer(t *testing.T) {
	provider, users := newTestOIDCProviderWithUsers(t)
	seedSessionUser(t, users, qrdbsdk.SystemTenantID, "user-42", []string{"admin"})
	issuer := newOIDCSessionIssuer(nil)
	issuer.SetProvider(provider)
	ctx := context.Background()

	token, _, err := issuer.Issue(ctx, &qrdbauth.Identity{
		Subject:  "user-42",
		TenantID: qrdbsdk.SystemTenantID,
		Metadata: map[string]string{authMethodMetadataKey: "piv"},
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	resolved, err := issuer.Resolve(ctx, token)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.TenantID != qrdbsdk.SystemTenantID {
		t.Errorf("resolved tenant = %q, want %q", resolved.TenantID, qrdbsdk.SystemTenantID)
	}
}
