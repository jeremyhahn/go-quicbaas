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
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// SaveState rejects an empty nonce; GetState reports not-found and expired.
func TestBackendStateStorage_ValidationAndExpiry(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendStateStorage(kv, nil)
	ctx := context.Background()

	if err := store.SaveState(ctx, &LoginState{TenantID: "acme"}); !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("SaveState(no nonce) err = %v, want ErrStateInvalid", err)
	}

	// Unknown token is not-found.
	if _, err := store.GetState(ctx, encodeTenantID("acme", "ghost")); !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("GetState(missing) err = %v, want ErrStateNotFound", err)
	}

	// An expired state surfaces ErrStateExpired.
	expired := testState("acme", "expired", "p1")
	expired.ExpiresAt = time.Now().Add(-time.Hour)
	if err := store.SaveState(ctx, expired); err != nil {
		t.Fatalf("SaveState(expired): %v", err)
	}
	if _, err := store.GetState(ctx, expired.Nonce); !errors.Is(err, ErrStateExpired) {
		t.Fatalf("GetState(expired) err = %v, want ErrStateExpired", err)
	}
}

// GetLinksByUser skips a dangling index entry whose record was deleted, and
// DeleteUserLinks clears multiple links.
func TestBackendFederationStorage_LinksByUserAndDeleteAll(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendFederationStorage(kv)
	ctx := tenantctx.WithTenant(context.Background(), "acme")

	l1 := testLink("acme", "u1", "l1", "p1", "ext-1")
	l2 := testLink("acme", "u1", "l2", "p2", "ext-2")
	if err := store.CreateLink(ctx, l1); err != nil {
		t.Fatalf("CreateLink(l1): %v", err)
	}
	if err := store.CreateLink(ctx, l2); err != nil {
		t.Fatalf("CreateLink(l2): %v", err)
	}

	links, err := store.GetLinksByUser(ctx, "u1")
	if err != nil || len(links) != 2 {
		t.Fatalf("GetLinksByUser = %d, %v; want 2, nil", len(links), err)
	}

	// Delete one link's record directly to leave a dangling index entry; the
	// next GetLinksByUser must skip it without error.
	if err := store.DeleteLink(ctx, l2.ID); err != nil {
		t.Fatalf("DeleteLink(l2): %v", err)
	}
	links, err = store.GetLinksByUser(ctx, "u1")
	if err != nil || len(links) != 1 {
		t.Fatalf("GetLinksByUser after delete = %d, %v; want 1, nil", len(links), err)
	}

	// DeleteUserLinks clears the remaining link.
	if err := store.DeleteUserLinks(ctx, "u1"); err != nil {
		t.Fatalf("DeleteUserLinks: %v", err)
	}
	links, err = store.GetLinksByUser(ctx, "u1")
	if err != nil || len(links) != 0 {
		t.Fatalf("GetLinksByUser after DeleteUserLinks = %d, %v; want 0, nil", len(links), err)
	}
}

// CleanupExpired sweeps expired states across every tenant in the lister.
func TestBackendStateStorage_CleanupAcrossTenants(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.barrierTenant("globex")
	store := NewBackendStateStorage(kv, staticTenantLister{ids: []string{"acme", "globex"}})
	ctx := context.Background()

	a := testState("acme", "a-old", "p1")
	a.ExpiresAt = time.Now().Add(-time.Hour)
	g := testState("globex", "g-old", "p1")
	g.ExpiresAt = time.Now().Add(-time.Hour)
	if err := store.SaveState(ctx, a); err != nil {
		t.Fatalf("SaveState(acme): %v", err)
	}
	if err := store.SaveState(ctx, g); err != nil {
		t.Fatalf("SaveState(globex): %v", err)
	}
	if err := store.SaveState(ctx, testState("acme", "a-fresh", "p1")); err != nil {
		t.Fatalf("SaveState(fresh): %v", err)
	}

	n, err := store.CleanupExpired(ctx)
	if err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}
	if n != 2 {
		t.Fatalf("CleanupExpired removed %d, want 2", n)
	}
}
