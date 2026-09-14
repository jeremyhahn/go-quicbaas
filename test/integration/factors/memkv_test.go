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

//go:build integration

package factors

import (
	"context"
	"testing"
)

// TestMemKV_CompareAndDelete pins the fixture's guarded-release semantics.
// memKV models the guarded half of the claim protocol production code runs
// against the real KV service (PutIfAbsent to claim, CompareAndDelete to
// release), so a fixture that deleted on mismatch — or reported the wrong
// boolean — would let a broken release path pass every test built on it.
func TestMemKV_CompareAndDelete(t *testing.T) {
	ctx := context.Background()

	t.Run("ExactMatchDeletes", func(t *testing.T) {
		kv := newMemKV()
		if err := kv.Put(ctx, "claim", []byte("holder-a")); err != nil {
			t.Fatalf("Put: %v", err)
		}
		deleted, err := kv.CompareAndDelete(ctx, "claim", []byte("holder-a"))
		if err != nil {
			t.Fatalf("CompareAndDelete: %v", err)
		}
		if !deleted {
			t.Fatal("an exact match must report deleted=true")
		}
		if _, err := kv.Get(ctx, "claim"); err == nil {
			t.Fatal("the key survived a reported deletion")
		}
	})

	t.Run("MismatchRefusesAndKeepsTheValue", func(t *testing.T) {
		kv := newMemKV()
		if err := kv.Put(ctx, "claim", []byte("holder-a")); err != nil {
			t.Fatalf("Put: %v", err)
		}
		deleted, err := kv.CompareAndDelete(ctx, "claim", []byte("holder-b"))
		if err != nil {
			t.Fatalf("CompareAndDelete: %v", err)
		}
		if deleted {
			t.Fatal("a mismatched value must not be deleted: that un-decides " +
				"what the guarded create decided")
		}
		got, err := kv.Get(ctx, "claim")
		if err != nil {
			t.Fatalf("Get after refused delete: %v", err)
		}
		if string(got) != "holder-a" {
			t.Fatalf("stored value = %q after a refused delete, want the original", got)
		}
	})

	t.Run("MissingKeyRefuses", func(t *testing.T) {
		kv := newMemKV()
		deleted, err := kv.CompareAndDelete(ctx, "absent", []byte("anything"))
		if err != nil {
			t.Fatalf("CompareAndDelete: %v", err)
		}
		if deleted {
			t.Fatal("deleting an absent key must report deleted=false")
		}
	})
}
