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
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TestRequireLoginKey pins which records can authenticate: one with an email
// login key, or one bound to a certificate. Anything else is refused.
func TestRequireLoginKey(t *testing.T) {
	cases := []struct {
		name    string
		user    *User
		refused bool
	}{
		{"email", &User{Email: "alice@acme.test"}, false},
		{"cert binding", &User{CertBindings: []qrdbsdk.CertBinding{
			{Thumbprint: "spki-thumbprint", Kind: qrdbsdk.CertBindingSPKIThumbprint},
		}}, false},
		{"neither", &User{TenantID: "acme", Username: "ghost"}, true},
		{"blank email", &User{Email: "   "}, true},
		{"binding without a thumbprint", &User{CertBindings: []qrdbsdk.CertBinding{
			{Kind: qrdbsdk.CertBindingSPKIThumbprint},
		}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := RequireLoginKey(tc.user)
			if !tc.refused {
				if err != nil {
					t.Fatalf("RequireLoginKey: %v", err)
				}
				return
			}
			var noKey *PrincipalWithoutLoginKeyError
			if !errors.As(err, &noKey) {
				t.Fatalf("err = %v, want PrincipalWithoutLoginKeyError", err)
			}
			if !isErrorCode(err, dberrors.ErrInvalidInput) {
				t.Fatalf("err = %v, want an invalid-input code", err)
			}
		})
	}
}

// TestMemoryStorage_CreateUser_RefusesAPrincipalWithoutLoginKey proves the
// refusal happens at creation and writes nothing.
func TestMemoryStorage_CreateUser_RefusesAPrincipalWithoutLoginKey(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryStorage()

	err := mem.CreateUser(ctx, &User{TenantID: "acme", Username: "ghost", Name: "Ghost"})
	var noKey *PrincipalWithoutLoginKeyError
	if !errors.As(err, &noKey) {
		t.Fatalf("CreateUser err = %v, want PrincipalWithoutLoginKeyError", err)
	}
	if noKey.Username != "ghost" || noKey.TenantID != "acme" {
		t.Fatalf("error names %q in %q, want ghost in acme", noKey.Username, noKey.TenantID)
	}
	if _, err := mem.GetUserByUsername(ctx, "acme", "ghost"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("a refused create was stored: GetUserByUsername err = %v", err)
	}

	certOnly := &User{
		TenantID: "acme", Username: "node-1", Name: "Node 1",
		CertBindings: []qrdbsdk.CertBinding{{Thumbprint: "spki-node-1", Kind: qrdbsdk.CertBindingSPKIThumbprint}},
	}
	if err := mem.CreateUser(ctx, certOnly); err != nil {
		t.Fatalf("a certificate-only principal must be accepted: %v", err)
	}
}
