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

package iam

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"
)

// TestBeginAddCredential_TargetsTheCallersAccount pins that the ceremony is
// opened for the account the user id names: the creation options carry that
// account's user handle and a session id the finish leg can redeem.
func TestBeginAddCredential_TargetsTheCallersAccount(t *testing.T) {
	mgr, _ := newCredentialAdminManager(t)

	creation, sessionID, err := mgr.Factory().BeginAddCredential(context.Background(), testWriteThroughTenant, 42)
	if err != nil {
		t.Fatalf("BeginAddCredential: %v", err)
	}
	if sessionID == "" {
		t.Fatal("BeginAddCredential returned an empty session id")
	}
	if creation == nil {
		t.Fatal("BeginAddCredential returned nil creation options")
	}
	want := xkmswebauthn.HandleFromUserID(42)
	got, ok := creation.Response.User.ID.(protocol.URLEncodedBase64)
	if !ok {
		t.Fatalf("creation user handle has type %T, want protocol.URLEncodedBase64", creation.Response.User.ID)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("creation user handle = %x, want %x", []byte(got), want)
	}
}

// TestBeginAddCredential_UnknownAccountIsTypedMismatch pins that a user with
// no WebAuthn account in the tenant is refused with the typed mismatch error,
// not an untyped lookup failure.
func TestBeginAddCredential_UnknownAccountIsTypedMismatch(t *testing.T) {
	mgr, _ := newCredentialAdminManager(t)

	_, _, err := mgr.Factory().BeginAddCredential(context.Background(), testWriteThroughTenant, 7)
	var mismatch *WebAuthnAccountMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("BeginAddCredential error = %v, want *WebAuthnAccountMismatchError", err)
	}
	if mismatch.UserID != 7 || mismatch.TenantID != testWriteThroughTenant {
		t.Fatalf("mismatch = %+v, want user 7 in %q", mismatch, testWriteThroughTenant)
	}
	if mismatch.Error() == "" {
		t.Fatal("mismatch error renders empty")
	}
}

// TestBeginAddCredential_EmptyTenantIsRefused pins that the tenant scope is
// mandatory: an empty tenant never resolves a relying party.
func TestBeginAddCredential_EmptyTenantIsRefused(t *testing.T) {
	mgr, _ := newCredentialAdminManager(t)

	_, _, err := mgr.Factory().BeginAddCredential(context.Background(), "", 42)
	var cfgErr *WebAuthnFactoryConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("BeginAddCredential error = %v, want *WebAuthnFactoryConfigError", err)
	}
}
