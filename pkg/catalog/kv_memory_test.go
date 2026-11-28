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

package catalog

import (
	"context"
	"testing"
)

func TestMemoryKV_GetMissingKeyIsNotFound(t *testing.T) {
	kv := NewMemoryKV()
	t.Cleanup(func() { _ = kv.Close() })

	_, err := kv.Get(context.Background(), []byte("missing"))
	if _, ok := err.(*KVKeyNotFoundError); !ok {
		t.Fatalf("Get(missing) error = %v (%T), want *KVKeyNotFoundError", err, err)
	}
}

func TestMemoryKV_ScanOrderedUnderPrefixAndStops(t *testing.T) {
	ctx := context.Background()
	kv := NewMemoryKV()
	t.Cleanup(func() { _ = kv.Close() })

	batch := &KVBatch{}
	for _, k := range []string{"p/c", "q/a", "p/a", "o/z", "p/b"} {
		batch.Set([]byte(k), []byte("v-"+k))
	}
	if err := kv.Apply(ctx, batch); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	var got []string
	if err := kv.Scan(ctx, []byte("p/"), func(key, value []byte) bool {
		if string(value) != "v-"+string(key) {
			t.Fatalf("Scan value for %q = %q", key, value)
		}
		got = append(got, string(key))
		return true
	}); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	assertKeys(t, "Scan", got, []string{"p/a", "p/b", "p/c"})

	var first []string
	if err := kv.Scan(ctx, []byte("p/"), func(key, _ []byte) bool {
		first = append(first, string(key))
		return len(first) < 2
	}); err != nil {
		t.Fatalf("Scan() stop error = %v", err)
	}
	assertKeys(t, "Scan stop", first, []string{"p/a", "p/b"})
}

func TestMemoryKV_ApplyPutsAndDeletesInOrder(t *testing.T) {
	ctx := context.Background()
	kv := NewMemoryKV()
	t.Cleanup(func() { _ = kv.Close() })

	batch := &KVBatch{}
	batch.Set([]byte("a"), []byte("1"))
	batch.Delete([]byte("a"))
	batch.Set([]byte("b"), []byte("1"))
	batch.Set([]byte("b"), []byte("2"))
	batch.Delete([]byte("c"))
	batch.Set([]byte("c"), []byte("3"))
	if err := kv.Apply(ctx, batch); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if _, err := kv.Get(ctx, []byte("a")); !isKVKeyNotFound(err) {
		t.Fatalf("Get(a) error = %v, want not found (put then delete)", err)
	}
	for k, want := range map[string]string{"b": "2", "c": "3"} {
		got, err := kv.Get(ctx, []byte(k))
		if err != nil {
			t.Fatalf("Get(%s) error = %v", k, err)
		}
		if string(got) != want {
			t.Fatalf("Get(%s) = %q, want %q", k, got, want)
		}
	}
}

func TestMemoryKV_ValuesAreCopies(t *testing.T) {
	ctx := context.Background()
	kv := NewMemoryKV()
	t.Cleanup(func() { _ = kv.Close() })

	value := []byte("orig")
	batch := &KVBatch{}
	batch.Set([]byte("k"), value)
	// KVBatch.Set copies, so write the op in place to check the store copies
	// too.
	batch.ops[0].Value = value
	if err := kv.Apply(ctx, batch); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	value[0] = 'X'

	got, err := kv.Get(ctx, []byte("k"))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(got) != "orig" {
		t.Fatalf("Get() after mutating the applied slice = %q, want %q", got, "orig")
	}
	got[0] = 'Y'

	if err := kv.Scan(ctx, []byte("k"), func(_, v []byte) bool {
		v[1] = 'Z'
		return true
	}); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	again, err := kv.Get(ctx, []byte("k"))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(again) != "orig" {
		t.Fatalf("Get() after mutating returned slices = %q, want %q", again, "orig")
	}
}

func TestMemoryKV_ClosedRefuses(t *testing.T) {
	ctx := context.Background()
	kv := NewMemoryKV()

	batch := &KVBatch{}
	batch.Set([]byte("k"), []byte("v"))
	if err := kv.Apply(ctx, batch); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if err := kv.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if _, err := kv.Get(ctx, []byte("k")); err == nil || isKVKeyNotFound(err) {
		t.Fatalf("Get() after Close error = %v, want a storage error", err)
	}
	if err := kv.Scan(ctx, []byte("k"), func(_, _ []byte) bool { return true }); err == nil {
		t.Fatal("Scan() after Close error = nil, want a storage error")
	}
	if err := kv.Apply(ctx, batch); err == nil {
		t.Fatal("Apply() after Close error = nil, want a storage error")
	}
}
