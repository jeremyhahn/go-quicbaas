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

func basicSession(tenantID, userID, id string) *Session {
	return &Session{
		ID:       id,
		UserID:   userID,
		TenantID: tenantID,
		Created:  time.Now(),
		Expires:  time.Now().Add(time.Hour),
	}
}

func extSession(tenantID, userID, id, tokenHash string) *SessionExt {
	return &SessionExt{
		ID:        id,
		UserID:    userID,
		TenantID:  tenantID,
		TokenHash: tokenHash,
		Created:   time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

// (a) A barrier tenant's session lands in its own store, absent from shared.
func TestBackendSessionStorage_BarrierIsolation(t *testing.T) {
	kv := newRoutingTestKV()
	tenantStore := kv.barrierTenant("acme")
	store := NewBackendSessionStorage(kv, nil)
	ctx := context.Background()

	sess := basicSession("acme", "u1", "s1")
	if err := store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if tenantStore.keyCount() == 0 {
		t.Fatal("session did not land in barrier store")
	}
	if kv.shared.keyCount() != 0 {
		t.Fatal("session leaked into shared store")
	}

	// (b) By-ID lookup with no tenant in context routes via the encoded id.
	got, err := store.GetSession(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.UserID != "u1" {
		t.Fatalf("session userID = %q, want u1", got.UserID)
	}

	byUser, err := store.GetSessionsByUser(ctx, "acme", "u1")
	if err != nil || len(byUser) != 1 {
		t.Fatalf("GetSessionsByUser = %d, %v; want 1, nil", len(byUser), err)
	}
}

// (c) Sealed tenant fails closed with no shared fallback.
func TestBackendSessionStorage_SealedFailsClosed(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.sealTenant("acme")
	store := NewBackendSessionStorage(kv, nil)
	ctx := context.Background()

	if err := store.CreateSession(ctx, basicSession("acme", "u1", "s1")); !errors.Is(err, errTenantSealedTest) {
		t.Fatalf("CreateSession(sealed) err = %v, want sealed", err)
	}
	if kv.shared.keyCount() != 0 {
		t.Fatalf("sealed op wrote to shared store: %d keys", kv.shared.keyCount())
	}
}

// DeleteExpiredSessions sweeps per tenant via the lister.
func TestBackendSessionStorage_DeleteExpiredPerTenant(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.barrierTenant("globex")
	store := NewBackendSessionStorage(kv, staticTenantLister{ids: []string{"acme", "globex"}})
	ctx := context.Background()

	expired := basicSession("acme", "u1", "s1")
	expired.Expires = time.Now().Add(-time.Hour)
	if err := store.CreateSession(ctx, expired); err != nil {
		t.Fatalf("CreateSession(expired): %v", err)
	}
	if err := store.CreateSession(ctx, basicSession("globex", "u2", "s2")); err != nil {
		t.Fatalf("CreateSession(fresh): %v", err)
	}

	n, err := store.DeleteExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if n != 1 {
		t.Fatalf("removed %d, want 1", n)
	}
}

// (a)/(b) SessionExt barrier isolation + GetByToken routes by context tenant.
func TestBackendSessionExtStorage_TokenLookup(t *testing.T) {
	kv := newRoutingTestKV()
	tenantStore := kv.barrierTenant("acme")
	store := NewBackendSessionExtStorage(kv, nil)
	ctx := tenantctx.WithTenant(context.Background(), "acme")

	sess := extSession("acme", "u1", "s1", "tokenhash-abc")
	if err := store.Create(ctx, sess); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if kv.shared.keyCount() != 0 {
		t.Fatal("ext session leaked into shared store")
	}
	if tenantStore.keyCount() == 0 {
		t.Fatal("ext session not in barrier store")
	}

	// GetByToken routes by the context tenant (token carries no tenant).
	got, err := store.GetByToken(ctx, "tokenhash-abc")
	if err != nil {
		t.Fatalf("GetByToken: %v", err)
	}
	if got.UserID != "u1" {
		t.Fatalf("token lookup userID = %q, want u1", got.UserID)
	}

	// Get by encoded id with empty context still routes.
	got2, err := store.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("Get(byID): %v", err)
	}
	if got2.ID != sess.ID {
		t.Fatalf("id round-trip mismatch: %q vs %q", got2.ID, sess.ID)
	}
}

// SessionExt sealed fail-closed.
func TestBackendSessionExtStorage_SealedFailsClosed(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.sealTenant("acme")
	store := NewBackendSessionExtStorage(kv, nil)
	ctx := tenantctx.WithTenant(context.Background(), "acme")

	if err := store.Create(ctx, extSession("acme", "u1", "s1", "th")); !errors.Is(err, errTenantSealedTest) {
		t.Fatalf("Create(sealed) err = %v, want sealed", err)
	}
	if kv.shared.keyCount() != 0 {
		t.Fatalf("sealed op wrote to shared store: %d keys", kv.shared.keyCount())
	}
}

// ListByUserActive filters expired sessions; DeleteAllExcept keeps one.
func TestBackendSessionExtStorage_ListAndDeleteAllExcept(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendSessionExtStorage(kv, nil)
	ctx := tenantctx.WithTenant(context.Background(), "acme")

	keep := extSession("acme", "u1", "keep", "th-keep")
	drop := extSession("acme", "u1", "drop", "th-drop")
	if err := store.Create(ctx, keep); err != nil {
		t.Fatalf("Create(keep): %v", err)
	}
	if err := store.Create(ctx, drop); err != nil {
		t.Fatalf("Create(drop): %v", err)
	}

	all, err := store.ListByUser(ctx, "u1", "acme")
	if err != nil || len(all) != 2 {
		t.Fatalf("ListByUser = %d, %v; want 2, nil", len(all), err)
	}

	if err := store.DeleteAllExcept(ctx, "u1", "acme", keep.ID); err != nil {
		t.Fatalf("DeleteAllExcept: %v", err)
	}
	remaining, err := store.ListByUser(ctx, "u1", "acme")
	if err != nil || len(remaining) != 1 {
		t.Fatalf("after DeleteAllExcept ListByUser = %d, %v; want 1, nil", len(remaining), err)
	}
	if remaining[0].ID != keep.ID {
		t.Fatalf("kept wrong session: %q", remaining[0].ID)
	}
}
