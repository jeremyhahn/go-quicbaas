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

// A session the session manager writes (SessionExt: expires_at, last_active)
// is read by the gRPC session store (Session: expires, last_activity) with its
// real expiry, so it is listed and fetched rather than dropped as expired.
func TestBackendSessionStorage_ReadsSessionManagerRecords(t *testing.T) {
	kv := newRoutingTestKV()
	ext := NewBackendSessionExtStorage(kv, nil)
	store := NewBackendSessionStorage(kv, nil)
	ctx := tenantctx.WithTenant(context.Background(), "acme")

	live := extSession("acme", 7, "s1", "h1")
	live.ExpiresAt = time.Now().Add(time.Hour).Truncate(time.Second)
	live.LastActive = time.Now().Add(-time.Minute).Truncate(time.Second)
	if err := ext.Create(ctx, live); err != nil {
		t.Fatalf("Create(live): %v", err)
	}

	stale := extSession("acme", 7, "s2", "h2")
	stale.ExpiresAt = time.Now().Add(-time.Hour)
	if err := ext.Create(ctx, stale); err != nil {
		t.Fatalf("Create(stale): %v", err)
	}

	sessions, err := store.GetSessionsByUser(ctx, "acme", 7)
	if err != nil {
		t.Fatalf("GetSessionsByUser: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("GetSessionsByUser returned %d sessions, want 1 (the unexpired one)", len(sessions))
	}
	got := sessions[0]
	if got.ID != live.ID {
		t.Fatalf("listed session %q, want %q", got.ID, live.ID)
	}
	if !got.Expires.Equal(live.ExpiresAt) {
		t.Fatalf("Expires = %v, want %v", got.Expires, live.ExpiresAt)
	}
	if !got.LastActivity.Equal(live.LastActive) {
		t.Fatalf("LastActivity = %v, want %v", got.LastActivity, live.LastActive)
	}
	if got.IsExpired() {
		t.Fatal("listed session reads as expired")
	}

	byID, err := store.GetSession(context.Background(), live.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if byID.IsExpired() {
		t.Fatalf("GetSession returned an expired session: Expires = %v", byID.Expires)
	}
	if !byID.Expires.Equal(live.ExpiresAt) {
		t.Fatalf("GetSession Expires = %v, want %v", byID.Expires, live.ExpiresAt)
	}
}
