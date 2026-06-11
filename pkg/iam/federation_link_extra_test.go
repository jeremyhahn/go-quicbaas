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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// userInfoServer returns a userinfo endpoint that emits the supplied claims.
func userInfoServer(t *testing.T, claims string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(claims))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// LinkAccount succeeds when userinfo yields a subject, then rejects a second
// link of the same external identity to the same user (idempotent guard).
func TestLinkAccount_SuccessAndAlreadyLinked(t *testing.T) {
	manager, providerStorage, _, _, identityStore, _ := setupTestFederationManager(t)
	ctx := context.Background()

	srv := userInfoServer(t, `{"sub":"ext-abc","email":"u@example.com","name":"U"}`)

	provider := createTestProvider("tenant1", "okta")
	provider.ID = "provider-link"
	provider.UserInfoURL = srv.URL
	provider.ApplyDefaults() // wire ClaimMappings as RegisterProvider would
	if err := providerStorage.CreateProvider(ctx, provider); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	user, err := identityStore.CreateUser(ctx, "tenant1", CreateUserOptions{
		ID:       "user-link",
		Email:    "u@example.com",
		Username: "ulink",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := manager.LinkAccount(ctx, user.ID, "tenant1", provider.ID, "ext-token"); err != nil {
		t.Fatalf("LinkAccount: %v", err)
	}

	// The user now has the link.
	links, err := manager.GetUserLinks(ctx, user.ID, "tenant1")
	if err != nil {
		t.Fatalf("GetUserLinks: %v", err)
	}
	if len(links) != 1 || links[0].ExternalID != "ext-abc" {
		t.Fatalf("GetUserLinks = %v, want one link to ext-abc", links)
	}

	// Linking the same external identity again to the same user is rejected.
	err = manager.LinkAccount(ctx, user.ID, "tenant1", provider.ID, "ext-token")
	if err == nil {
		t.Fatal("re-linking the same external identity should fail")
	}
	if !errors.Is(err, ErrFederatedIdentityAlreadyLinked) {
		t.Fatalf("re-link err = %v, want ErrFederatedIdentityAlreadyLinked", err)
	}
}

// LinkAccount fails closed when the userinfo response carries no subject claim.
func TestLinkAccount_NoSubjectClaim(t *testing.T) {
	manager, providerStorage, _, _, identityStore, _ := setupTestFederationManager(t)
	ctx := context.Background()

	srv := userInfoServer(t, `{"email":"u@example.com"}`) // no "sub"

	provider := createTestProvider("tenant1", "okta")
	provider.ID = "provider-nosub"
	provider.UserInfoURL = srv.URL
	provider.ApplyDefaults()
	if err := providerStorage.CreateProvider(ctx, provider); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	user, err := identityStore.CreateUser(ctx, "tenant1", CreateUserOptions{
		ID:       "user-nosub",
		Email:    "u@example.com",
		Username: "unosub",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := manager.LinkAccount(ctx, user.ID, "tenant1", provider.ID, "ext-token"); err == nil {
		t.Fatal("LinkAccount with no subject claim should fail")
	}
}
