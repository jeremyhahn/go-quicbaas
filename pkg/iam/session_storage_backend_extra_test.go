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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// UpdateSession/DeleteSession round-trip plus not-found and invalid-input paths.
func TestBackendSessionStorage_UpdateDelete(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendSessionStorage(kv, nil)
	ctx := context.Background()

	sess := basicSession("acme", "u1", "s1")
	if err := store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Update an existing session.
	sess.UserID = "u1-renamed"
	if err := store.UpdateSession(ctx, sess); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	got, err := store.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.UserID != "u1-renamed" {
		t.Fatalf("UpdateSession userID = %q, want u1-renamed", got.UserID)
	}

	// Update with empty ID is invalid input.
	if err := store.UpdateSession(ctx, &Session{}); err == nil {
		t.Fatal("UpdateSession(empty) want error")
	}

	// Update a missing session is not found.
	missing := basicSession("acme", "u1", "does-not-exist")
	missing.ID = ensureTenantEncoded("acme", "does-not-exist")
	if err := store.UpdateSession(ctx, missing); err == nil {
		t.Fatal("UpdateSession(missing) want not-found error")
	}

	// Delete missing session is not found.
	if err := store.DeleteSession(ctx, ensureTenantEncoded("acme", "nope")); err == nil {
		t.Fatal("DeleteSession(missing) want not-found error")
	}

	// Delete the existing session.
	if err := store.DeleteSession(ctx, sess.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := store.GetSession(ctx, sess.ID); err == nil {
		t.Fatal("GetSession after delete want not-found")
	}
}

// SessionExt Update reconciles token indexes; Delete is idempotent.
func TestBackendSessionExtStorage_UpdateDelete(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendSessionExtStorage(kv, nil)
	ctx := tenantctx.WithTenant(context.Background(), "acme")

	sess := extSession("acme", "u1", "s1", "th-old")
	sess.RefreshHash = "rh-old"
	if err := store.Create(ctx, sess); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Rotate both token and refresh hashes; old indexes must be reconciled.
	sess.TokenHash = "th-new"
	sess.RefreshHash = "rh-new"
	if err := store.Update(ctx, sess); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, err := store.GetByToken(ctx, "th-old"); err == nil {
		t.Fatal("old token index still resolves after Update")
	}
	got, err := store.GetByToken(ctx, "th-new")
	if err != nil {
		t.Fatalf("GetByToken(new): %v", err)
	}
	if got.RefreshHash != "rh-new" {
		t.Fatalf("RefreshHash = %q, want rh-new", got.RefreshHash)
	}

	// Update a missing session errors.
	missing := extSession("acme", "u1", "missing", "th-x")
	missing.ID = ensureTenantEncoded("acme", "missing")
	if err := store.Update(ctx, missing); err == nil {
		t.Fatal("Update(missing) want error")
	}

	// Delete with empty ID is invalid input.
	if err := store.Delete(ctx, ""); err == nil {
		t.Fatal("Delete(empty) want error")
	}

	// Delete is idempotent on a missing session.
	if err := store.Delete(ctx, ensureTenantEncoded("acme", "gone")); err != nil {
		t.Fatalf("Delete(missing) = %v, want nil (idempotent)", err)
	}

	// Delete the existing session and verify indexes are gone.
	if err := store.Delete(ctx, sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.GetByToken(ctx, "th-new"); err == nil {
		t.Fatal("token index survived Delete")
	}
}

// ListByUserActive filters expired; DeleteByUser and CleanupExpired clear records.
func TestBackendSessionExtStorage_ActiveDeleteByUserCleanup(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendSessionExtStorage(kv, staticTenantLister{ids: []string{"acme"}})
	ctx := tenantctx.WithTenant(context.Background(), "acme")

	active := extSession("acme", "u1", "active", "th-active")
	expired := extSession("acme", "u1", "expired", "th-expired")
	expired.ExpiresAt = time.Now().Add(-time.Hour)
	if err := store.Create(ctx, active); err != nil {
		t.Fatalf("Create(active): %v", err)
	}
	if err := store.Create(ctx, expired); err != nil {
		t.Fatalf("Create(expired): %v", err)
	}

	activeList, err := store.ListByUserActive(ctx, "u1", "acme")
	if err != nil || len(activeList) != 1 {
		t.Fatalf("ListByUserActive = %d, %v; want 1, nil", len(activeList), err)
	}
	if activeList[0].ID != active.ID {
		t.Fatalf("ListByUserActive returned wrong session: %q", activeList[0].ID)
	}

	// CleanupExpired removes the expired one across the tenant.
	n, err := store.CleanupExpired(ctx)
	if err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}
	if n != 1 {
		t.Fatalf("CleanupExpired removed %d, want 1", n)
	}

	// DeleteByUser clears the remaining session.
	if err := store.DeleteByUser(ctx, "u1", "acme"); err != nil {
		t.Fatalf("DeleteByUser: %v", err)
	}
	remaining, err := store.ListByUser(ctx, "u1", "acme")
	if err != nil || len(remaining) != 0 {
		t.Fatalf("after DeleteByUser ListByUser = %d, %v; want 0, nil", len(remaining), err)
	}
}
