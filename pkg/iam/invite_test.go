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
	"errors"
	"testing"
	"time"
)

func TestGenerateInviteToken(t *testing.T) {
	tok1, err := GenerateInviteToken()
	if err != nil {
		t.Fatalf("GenerateInviteToken: unexpected error: %v", err)
	}
	if len(tok1) != 64 {
		t.Fatalf("expected 64 hex chars, got %d", len(tok1))
	}

	tok2, err := GenerateInviteToken()
	if err != nil {
		t.Fatalf("GenerateInviteToken second call: %v", err)
	}
	if tok1 == tok2 {
		t.Fatal("expected unique tokens, got identical values")
	}
}

func TestMemoryInviteStore_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryInviteStore()

	token, err := GenerateInviteToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	record := &InviteRecord{
		TokenHash: tokenHash(token),
		UserID:    "user-1",
		TenantID:  "tenant-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	if err := store.Create(ctx, record); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.GetByToken(ctx, token)
	if err != nil {
		t.Fatalf("GetByToken: %v", err)
	}
	if got.UserID != "user-1" {
		t.Errorf("expected UserID=user-1, got %q", got.UserID)
	}
}

func TestMemoryInviteStore_GetByToken_NotFound(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryInviteStore()

	_, err := store.GetByToken(ctx, "nonexistent-token")
	if !errors.Is(err, ErrInviteNotFound) {
		t.Fatalf("expected ErrInviteNotFound, got %v", err)
	}
}

func TestMemoryInviteStore_MarkUsed(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryInviteStore()

	token, _ := GenerateInviteToken()
	record := &InviteRecord{
		TokenHash: tokenHash(token),
		UserID:    "user-2",
		TenantID:  "tenant-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	_ = store.Create(ctx, record)

	if err := store.MarkUsed(ctx, token); err != nil {
		t.Fatalf("MarkUsed: %v", err)
	}

	// Second call should return ErrInviteUsed.
	if err := store.MarkUsed(ctx, token); !errors.Is(err, ErrInviteUsed) {
		t.Fatalf("expected ErrInviteUsed on second call, got %v", err)
	}
}

func TestMemoryInviteStore_Expiry(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryInviteStore()

	token, _ := GenerateInviteToken()
	record := &InviteRecord{
		TokenHash: tokenHash(token),
		UserID:    "user-3",
		TenantID:  "tenant-1",
		ExpiresAt: time.Now().Add(-time.Minute), // already expired
	}
	_ = store.Create(ctx, record)

	got, err := store.GetByToken(ctx, token)
	if err != nil {
		t.Fatalf("GetByToken: %v", err)
	}
	// Expiry check is the caller's responsibility (or GC); store returns the record.
	if got.ExpiresAt.After(time.Now()) {
		t.Fatal("expected an already-expired record")
	}
}

func TestMemoryInviteStore_GC(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryInviteStore()

	expiredToken, _ := GenerateInviteToken()
	activeToken, _ := GenerateInviteToken()

	_ = store.Create(ctx, &InviteRecord{
		TokenHash: tokenHash(expiredToken),
		UserID:    "user-e",
		TenantID:  "t1",
		ExpiresAt: time.Now().Add(-time.Minute),
	})
	_ = store.Create(ctx, &InviteRecord{
		TokenHash: tokenHash(activeToken),
		UserID:    "user-a",
		TenantID:  "t1",
		ExpiresAt: time.Now().Add(time.Hour),
	})

	if err := store.GC(ctx); err != nil {
		t.Fatalf("GC: %v", err)
	}

	// Expired record should be gone.
	if _, err := store.GetByToken(ctx, expiredToken); !errors.Is(err, ErrInviteNotFound) {
		t.Fatalf("expected expired record to be removed, got %v", err)
	}

	// Active record should still be present.
	if _, err := store.GetByToken(ctx, activeToken); err != nil {
		t.Fatalf("expected active record to survive GC, got %v", err)
	}
}
