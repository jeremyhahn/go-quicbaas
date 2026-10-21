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
	"slices"
	"testing"
)

// seedWebAuthnUser creates a user on the CreateUser default policy, which is
// [webauthn].
func seedWebAuthnUser(t *testing.T, store *IdentityStore) *User {
	t.Helper()
	user, err := store.CreateUser(context.Background(), "tenant1", CreateUserOptions{
		Email:    "admin@example.com",
		Username: "admin",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	return user
}

func TestPermitCertificateLogin_AddsMTLS(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()
	user := seedWebAuthnUser(t, store)

	updated, err := store.PermitCertificateLogin(ctx, "tenant1", user.ID)
	if err != nil {
		t.Fatalf("PermitCertificateLogin failed: %v", err)
	}
	want := []string{AuthMethodWebAuthn, AuthMethodMTLS}
	if !slices.Equal(updated.AuthMethods, want) {
		t.Errorf("AuthMethods = %v, want %v", updated.AuthMethods, want)
	}

	stored, err := store.GetUser(ctx, "tenant1", user.ID)
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if !slices.Equal(stored.AuthMethods, want) {
		t.Errorf("stored AuthMethods = %v, want %v", stored.AuthMethods, want)
	}
}

func TestPermitCertificateLogin_IsIdempotent(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()
	user := seedWebAuthnUser(t, store)

	if _, err := store.PermitCertificateLogin(ctx, "tenant1", user.ID); err != nil {
		t.Fatalf("first PermitCertificateLogin failed: %v", err)
	}
	updated, err := store.PermitCertificateLogin(ctx, "tenant1", user.ID)
	if err != nil {
		t.Fatalf("second PermitCertificateLogin failed: %v", err)
	}
	want := []string{AuthMethodWebAuthn, AuthMethodMTLS}
	if !slices.Equal(updated.AuthMethods, want) {
		t.Errorf("AuthMethods = %v, want %v", updated.AuthMethods, want)
	}
}

// An empty policy already permits every method, so adding mtls would narrow it.
func TestPermitCertificateLogin_LeavesEmptyPolicyAlone(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()
	user := seedWebAuthnUser(t, store)
	user.AuthMethods = nil
	if err := store.userStorage.UpdateUser(ctx, user); err != nil {
		t.Fatalf("clearing AuthMethods failed: %v", err)
	}

	updated, err := store.PermitCertificateLogin(ctx, "tenant1", user.ID)
	if err != nil {
		t.Fatalf("PermitCertificateLogin failed: %v", err)
	}
	if len(updated.AuthMethods) != 0 {
		t.Errorf("AuthMethods = %v, want empty", updated.AuthMethods)
	}
}

func TestPermitCertificateLogin_RefusesBadInput(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()
	user := seedWebAuthnUser(t, store)

	tests := []struct {
		name     string
		tenantID string
		userID   uint64
	}{
		{"empty tenant", "", user.ID},
		{"zero user id", "tenant1", 0},
		{"unknown user", "tenant1", user.ID + 1000},
		{"user in another tenant", "tenant2", user.ID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := store.PermitCertificateLogin(ctx, tt.tenantID, tt.userID); err == nil {
				t.Error("expected an error")
			}
		})
	}
}
