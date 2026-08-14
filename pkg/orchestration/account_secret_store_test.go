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

package orchestration

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
)

func newTestAccountSecretStore(t *testing.T) *AccountSecretStore {
	t.Helper()
	mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: secrets.NewMemoryBackend()})
	if err != nil {
		t.Fatalf("secrets.NewManager() error = %v", err)
	}
	return NewAccountSecretStore(mgr)
}

func TestAccountSecretStore_PutGetRoundTrip(t *testing.T) {
	store := newTestAccountSecretStore(t)
	ctx := context.Background()

	cred := &AccountCredential{
		AccessToken:       "at-1",
		RefreshToken:      "rt-1",
		AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
	}
	if err := store.Put(ctx, "tenant-a", "acct-1", cred); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	got, err := store.Get(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.AccessToken != "at-1" || got.RefreshToken != "rt-1" {
		t.Fatalf("Get() = %+v, want access_token=at-1 refresh_token=rt-1", got)
	}
}

// TestAccountSecretStore_TenantIsolation proves one tenant's stored
// credential is never returned for another tenant, even for the same
// accountID.
func TestAccountSecretStore_TenantIsolation(t *testing.T) {
	store := newTestAccountSecretStore(t)
	ctx := context.Background()

	if err := store.Put(ctx, "tenant-a", "acct-1", &AccountCredential{AccessToken: "tenant-a-secret"}); err != nil {
		t.Fatalf("Put(tenant-a) error = %v", err)
	}

	if _, err := store.Get(ctx, "tenant-b", "acct-1"); err == nil {
		t.Fatalf("Get(tenant-b, acct-1) unexpectedly succeeded — tenant isolation broken")
	}
}

func TestAccountSecretStore_Get_NotFound(t *testing.T) {
	store := newTestAccountSecretStore(t)
	if _, err := store.Get(context.Background(), "tenant-a", "missing"); err == nil {
		t.Fatalf("Get() of a missing credential succeeded, want AccountCredentialNotFoundError")
	}
}

func TestAccountSecretStore_Delete_IsIdempotentAndRemoves(t *testing.T) {
	store := newTestAccountSecretStore(t)
	ctx := context.Background()

	if err := store.Put(ctx, "tenant-a", "acct-1", &AccountCredential{AccessToken: "x"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := store.Delete(ctx, "tenant-a", "acct-1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Get(ctx, "tenant-a", "acct-1"); err == nil {
		t.Fatalf("Get() after Delete() succeeded")
	}
	// Idempotent: deleting again must not error.
	if err := store.Delete(ctx, "tenant-a", "acct-1"); err != nil {
		t.Fatalf("second Delete() error = %v, want nil (idempotent)", err)
	}
}
