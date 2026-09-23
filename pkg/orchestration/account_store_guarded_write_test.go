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
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Account record writes are BACKEND-guarded (go-quicbaas#1028, round 5).
//
// The service layer and the orchestration runtime each build their own
// AccountStore over one KV, and every daemon has its own beyond that. A
// per-instance mutex orders none of them against each other, so a version
// check under that mutex followed by an unconditional Put could still land a
// stale snapshot over a record another instance had just written. These tests
// put a second STORE INSTANCE's write exactly inside the first one's window —
// after its read and version check, before its commit — which no mutex in the
// first instance can see.
// =============================================================================

// swapWindowKV runs `between` once, immediately before the first guarded edit
// it forwards: after the writer read and checked the record, before it
// commits. The hook is disarmed before it runs, because the writer it runs
// reaches this same verb through another store instance.
type swapWindowKV struct {
	interfaces.KVStore

	between func()
	armed   atomic.Bool
}

func newSwapWindowKV(inner interfaces.KVStore, between func()) *swapWindowKV {
	k := &swapWindowKV{KVStore: inner, between: between}
	k.armed.Store(true)
	return k
}

func (k *swapWindowKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

func (k *swapWindowKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	return forwardCompareAndDelete(ctx, k.KVStore, key, expected)
}

func (k *swapWindowKV) CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error) {
	if k.armed.CompareAndSwap(true, false) {
		k.between()
	}
	return forwardCompareAndSwap(ctx, k.KVStore, key, expected, next)
}

// TestAccountStore_StaleRenameCannotEraseASwitchFromAnotherStore is the round-5
// finding: a rename in store A passes its version check, a switch to a secret
// reference commits through store B over the same backend, and A's write must
// then be refused rather than erase the reference.
func TestAccountStore_StaleRenameCannotEraseASwitchFromAnotherStore(t *testing.T) {
	ctx := context.Background()
	shared := NewMemoryTaskKV()
	other := NewAccountStore(shared)
	account := mustCreateAccount(t, other, newTestAccount("tenant-cas", "acct-1", AccountProviderClaude))

	window := newSwapWindowKV(shared, func() {
		switched, err := other.Get(ctx, "tenant-cas", "acct-1")
		if err != nil {
			t.Errorf("Get() in the window error = %v", err)
			return
		}
		switched.CredentialSecretRef = "agents/claude/claude1/oauth_token"
		if err := other.UpdateIfUnchanged(ctx, switched, switched.UpdatedAt); err != nil {
			t.Errorf("the overtaking switch failed: %v", err)
		}
	})
	renamer := NewAccountStore(window)

	stale, err := renamer.Get(ctx, "tenant-cas", account.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	stale.DisplayName = "renamed"
	err = renamer.UpdateIfUnchanged(ctx, stale, stale.UpdatedAt)

	var conflict *AccountVersionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("UpdateIfUnchanged() error = %v, want an AccountVersionConflictError", err)
	}
	got, err := other.Get(ctx, "tenant-cas", account.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.CredentialSecretRef == "" {
		t.Fatal("the stale rename erased the reference switch another store committed")
	}
	if got.DisplayName == "renamed" {
		t.Error("the refused rename was written")
	}
}

// TestAccountStore_TouchReappliesOverAWriteFromAnotherStore proves Touch, a
// field-level change, loses its swap to another store's write and re-applies
// onto the newer record instead of overwriting it.
func TestAccountStore_TouchReappliesOverAWriteFromAnotherStore(t *testing.T) {
	ctx := context.Background()
	shared := NewMemoryTaskKV()
	other := NewAccountStore(shared)
	account := mustCreateAccount(t, other, newTestAccount("tenant-cas", "acct-1", AccountProviderClaude))

	window := newSwapWindowKV(shared, func() {
		renamed, err := other.Get(ctx, "tenant-cas", "acct-1")
		if err != nil {
			t.Errorf("Get() in the window error = %v", err)
			return
		}
		renamed.DisplayName = "renamed-elsewhere"
		if err := other.UpdateIfUnchanged(ctx, renamed, renamed.UpdatedAt); err != nil {
			t.Errorf("the overtaking rename failed: %v", err)
		}
	})
	toucher := NewAccountStore(window)

	if err := toucher.Touch(ctx, "tenant-cas", account.ID, TouchAccountOptions{
		SetStatusValid: true, SetStatus: AccountStatusError, SetLastError: "probe failed",
	}); err != nil {
		t.Fatalf("Touch() error = %v", err)
	}

	got, err := other.Get(ctx, "tenant-cas", account.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.DisplayName != "renamed-elsewhere" {
		t.Errorf("display name = %q: the touch overwrote another store's rename", got.DisplayName)
	}
	if got.Status != AccountStatusError || got.LastError != "probe failed" {
		t.Errorf("status = %s, last_error = %q: the touch was not re-applied", got.Status, got.LastError)
	}
}

// TestAccountStore_TouchRequiringInlineRefusesAReferenceAccount proves the
// inline-only guard is decided on the record the touch commits over: a switch
// that lands inside the window turns the touch into a refusal that writes
// nothing.
func TestAccountStore_TouchRequiringInlineRefusesAReferenceAccount(t *testing.T) {
	ctx := context.Background()
	shared := NewMemoryTaskKV()
	other := NewAccountStore(shared)
	account := mustCreateAccount(t, other, newTestAccount("tenant-cas", "acct-1", AccountProviderClaude))

	window := newSwapWindowKV(shared, func() {
		switched, err := other.Get(ctx, "tenant-cas", "acct-1")
		if err != nil {
			t.Errorf("Get() in the window error = %v", err)
			return
		}
		switched.CredentialSecretRef = "agents/claude/claude1/oauth_token"
		if err := other.UpdateIfUnchanged(ctx, switched, switched.UpdatedAt); err != nil {
			t.Errorf("the overtaking switch failed: %v", err)
		}
	})
	rotator := NewAccountStore(window)
	before, err := other.Get(ctx, "tenant-cas", account.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	err = rotator.Touch(ctx, "tenant-cas", account.ID, TouchAccountOptions{
		MarkRefreshed:            true,
		SetCredentialExpiresAt:   before.CreatedAt.Add(365 * 24 * time.Hour),
		SetCredentialExpiryValid: true,
		RequireInlineCredential:  true,
	})
	var changed *AccountCredentialSourceChangedError
	if !errors.As(err, &changed) || !errors.Is(err, ErrAccountVersionConflict) {
		t.Fatalf("Touch() error = %v, want an AccountCredentialSourceChangedError", err)
	}
	got, err := other.Get(ctx, "tenant-cas", account.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !got.CredentialExpiresAt.Equal(before.CredentialExpiresAt) || !got.LastRefreshedAt.Equal(before.LastRefreshedAt) {
		t.Error("the refused touch wrote its expiry or refresh stamp onto the reference account")
	}
}

// TestAccountStore_WritesRefuseAStoreWithoutAGuardedEdit proves a backend that
// cannot decide a guarded edit is refused, not written to unconditionally.
func TestAccountStore_WritesRefuseAStoreWithoutAGuardedEdit(t *testing.T) {
	ctx := context.Background()
	shared := NewMemoryTaskKV()
	account := mustCreateAccount(t, NewAccountStore(shared), newTestAccount("tenant-cas", "acct-1", AccountProviderClaude))

	unguarded := NewAccountStore(&noSwapKV{KVStore: shared})
	err := unguarded.Touch(ctx, "tenant-cas", account.ID, TouchAccountOptions{MarkUsed: true})
	var storeErr *AccountStoreError
	if !errors.As(err, &storeErr) {
		t.Fatalf("Touch() error = %v, want an AccountStoreError", err)
	}
	got, getErr := unguarded.Get(ctx, "tenant-cas", account.ID)
	if getErr != nil {
		t.Fatalf("Get() error = %v", getErr)
	}
	if !got.LastUsedAt.IsZero() {
		t.Error("an unguarded store was written to")
	}
}

// noSwapKV hides the guarded edit, keeping the guarded create and delete.
type noSwapKV struct {
	interfaces.KVStore
}

func (k *noSwapKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

func (k *noSwapKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	return forwardCompareAndDelete(ctx, k.KVStore, key, expected)
}
