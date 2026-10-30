//go:build !integration

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

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// swapper is the guarded-edit capability consumers discover by assertion.
type swapper interface {
	CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error)
}

// recordingGuardedKV is an SDK GuardedKVClient double that records the swap
// it was asked for and answers with a scripted result.
type recordingGuardedKV struct {
	transport.GuardedKVClient

	key            string
	expected, next []byte
	swapped        bool
	err            error
}

func (k *recordingGuardedKV) CompareAndSwap(_ context.Context, key string, expected, next []byte) (bool, error) {
	k.key, k.expected, k.next = key, expected, next
	return k.swapped, k.err
}

func TestGuardedSDKKVStore_CompareAndSwapForwardsToTheSDK(t *testing.T) {
	double := &recordingGuardedKV{swapped: true}
	store := &guardedSDKKVStore{guarded: double}

	swapped, err := store.CompareAndSwap(context.Background(), "k", []byte("old"), []byte("new"))
	if err != nil || !swapped {
		t.Fatalf("CompareAndSwap = (%v, %v), want (true, nil)", swapped, err)
	}
	if double.key != "k" || string(double.expected) != "old" || string(double.next) != "new" {
		t.Errorf("forwarded (%q, %q, %q), want (k, old, new)", double.key, double.expected, double.next)
	}
}

func TestGuardedSDKKVStore_CompareAndSwapReturnsTheSDKError(t *testing.T) {
	cause := errors.New("not leader")
	store := &guardedSDKKVStore{guarded: &recordingGuardedKV{err: cause}}

	swapped, err := store.CompareAndSwap(context.Background(), "k", nil, []byte("new"))
	if !errors.Is(err, cause) || swapped {
		t.Fatalf("CompareAndSwap = (%v, %v), want (false, the SDK's error)", swapped, err)
	}
}

// TestSDKKVStore_EmbeddedNodeArbitratesASwap runs the swap through
// newSDKKVStore on a real standalone node: the embedded transport carries the
// guarded verbs, so the store must expose CompareAndSwap, and a swap against a
// value that has since changed must lose.
func TestSDKKVStore_EmbeddedNodeArbitratesASwap(t *testing.T) {
	store := createTestSDKKVStore(t)
	kv, ok := store.(swapper)
	if !ok {
		t.Fatalf("%T over the embedded transport does not expose CompareAndSwap", store)
	}
	ctx := context.Background()
	if err := store.Put(ctx, "swap-k", []byte("v1")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	swapped, err := kv.CompareAndSwap(ctx, "swap-k", []byte("v1"), []byte("v2"))
	if err != nil || !swapped {
		t.Fatalf("swap on the held value = (%v, %v), want (true, nil)", swapped, err)
	}
	swapped, err = kv.CompareAndSwap(ctx, "swap-k", []byte("v1"), []byte("v3"))
	if err != nil || swapped {
		t.Fatalf("swap on a stale value = (%v, %v), want (false, nil)", swapped, err)
	}
	got, err := store.Get(ctx, "swap-k")
	if err != nil || string(got) != "v2" {
		t.Fatalf("Get = (%q, %v), want (v2, nil)", got, err)
	}
}

// TestSDKKVStore_BaseStoreDoesNotPromoteTheSwap pins the split newSDKKVStore
// makes: a transport without the guarded verbs yields a store without them,
// rather than one that claims a swap it cannot arbitrate.
func TestSDKKVStore_BaseStoreDoesNotPromoteTheSwap(t *testing.T) {
	if _, ok := any(&sdkKVStore{}).(swapper); ok {
		t.Fatal("the unguarded sdkKVStore exposes CompareAndSwap")
	}
}

// TestGuardedSDKKVStore_SatisfiesTheGuardedKVStore pins the round 34
// Integration failure on go-quicbaas#1993: KMS refused this store as "not
// atomic" because it asserted go-qrdb's AtomicKVStore, whose embedded KVStore
// carries secondary-index methods this store has no reason to offer. The
// guarded store must satisfy the capability KMS asserts, and the unguarded one
// must not.
func TestGuardedSDKKVStore_SatisfiesTheGuardedKVStore(t *testing.T) {
	var store interfaces.KVStore = &guardedSDKKVStore{guarded: &recordingGuardedKV{}}
	if _, ok := store.(interfaces.GuardedKVStore); !ok {
		t.Fatalf("%T does not satisfy interfaces.GuardedKVStore", store)
	}
	if _, ok := any(&sdkKVStore{}).(interfaces.GuardedKVStore); ok {
		t.Fatal("the unguarded sdkKVStore satisfies interfaces.GuardedKVStore")
	}
}

// TestSDKKVStore_EmbeddedNodeArbitratesAClaim runs a KMS-style claim and
// release through newSDKKVStore on a real standalone node, asserting the store
// the same way KMS does: the first claim wins, a second loses, and a release
// against a stale value leaves the claim in place.
func TestSDKKVStore_EmbeddedNodeArbitratesAClaim(t *testing.T) {
	store := createTestSDKKVStore(t)
	guarded, ok := store.(interfaces.GuardedKVStore)
	if !ok {
		t.Fatalf("%T over the embedded transport does not satisfy interfaces.GuardedKVStore", store)
	}
	ctx := context.Background()

	stored, err := guarded.PutIfAbsent(ctx, "claim-k", []byte("mine"))
	if err != nil || !stored {
		t.Fatalf("first claim = (%v, %v), want (true, nil)", stored, err)
	}
	stored, err = guarded.PutIfAbsent(ctx, "claim-k", []byte("theirs"))
	if err != nil || stored {
		t.Fatalf("second claim = (%v, %v), want (false, nil)", stored, err)
	}
	deleted, err := guarded.CompareAndDelete(ctx, "claim-k", []byte("theirs"))
	if err != nil || deleted {
		t.Fatalf("stale release = (%v, %v), want (false, nil)", deleted, err)
	}
	deleted, err = guarded.CompareAndDelete(ctx, "claim-k", []byte("mine"))
	if err != nil || !deleted {
		t.Fatalf("owner release = (%v, %v), want (true, nil)", deleted, err)
	}
}
