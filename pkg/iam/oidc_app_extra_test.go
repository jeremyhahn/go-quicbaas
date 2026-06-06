//go:build iam

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

package iam

import (
	"context"
	"testing"
)

// RotateSecret reports not-found for an unknown client.
func TestAppManager_RotateSecret_Unknown(t *testing.T) {
	manager := createTestAppManager(t)
	if _, err := manager.RotateSecret(context.Background(), "no-such-client"); err == nil {
		t.Fatal("RotateSecret(unknown) want error")
	}
}

// A client_credentials-only app needs no redirect URIs; an invalid grant type
// is rejected.
func TestAppManager_RegisterApp_GrantTypeRules(t *testing.T) {
	manager := createTestAppManager(t)
	ctx := context.Background()

	// client_credentials only -> no redirect URIs required.
	app, err := manager.RegisterApp(ctx, "tenant-1", RegisterAppOptions{
		Name:       "Service",
		GrantTypes: []string{GrantTypeClientCredentials},
	})
	if err != nil {
		t.Fatalf("RegisterApp(client_credentials) error = %v", err)
	}
	if len(app.RedirectURIs) != 0 {
		t.Fatalf("client_credentials app should have no redirect URIs, got %v", app.RedirectURIs)
	}

	// An unknown grant type is rejected.
	if _, err := manager.RegisterApp(ctx, "tenant-1", RegisterAppOptions{
		Name:       "Bad",
		GrantTypes: []string{"totally_invalid_grant"},
	}); err == nil {
		t.Fatal("RegisterApp(invalid grant) want error")
	}
}

// appRequiresRedirectURIs: empty defaults to true; non-interactive grants false.
func TestAppRequiresRedirectURIs(t *testing.T) {
	if !appRequiresRedirectURIs(nil) {
		t.Fatal("empty grants must default to requiring redirect URIs")
	}
	if !appRequiresRedirectURIs([]string{GrantTypeAuthorizationCode, GrantTypeRefreshToken}) {
		t.Fatal("authorization_code requires redirect URIs")
	}
	if appRequiresRedirectURIs([]string{GrantTypeClientCredentials}) {
		t.Fatal("client_credentials must not require redirect URIs")
	}
}
