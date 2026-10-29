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

package kvstore

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// putAndGet stores value under key and returns what a later Get reads back.
func putAndGet(t *testing.T, store *Store, key string, value []byte) []byte {
	t.Helper()
	ctx := context.Background()
	if err := store.Put(ctx, key, value); err != nil {
		t.Fatalf("put %q: %v", key, err)
	}
	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("get %q: %v", key, err)
	}
	return got
}

func TestStorePutOverwriteReadsNewValue(t *testing.T) {
	store := NewMemory()

	if got := putAndGet(t, store, "k", []byte("original")); string(got) != "original" {
		t.Fatalf("got %q, want %q", got, "original")
	}
	if got := putAndGet(t, store, "k", []byte("updated")); string(got) != "updated" {
		t.Fatalf("overwrite: got %q, want %q", got, "updated")
	}
}

// Deleting a key that was never stored is not an error, however often it runs.
func TestStoreDeleteMissingKeyIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()

	for i := 0; i < 2; i++ {
		if err := store.Delete(ctx, "never-stored"); err != nil {
			t.Fatalf("delete %d of a missing key: %v", i+1, err)
		}
	}
	if _, err := store.Get(ctx, "never-stored"); !qberrors.IsNotFound(err) {
		t.Fatalf("want not-found, got %v", err)
	}
}

func TestStoreLargeValuesRoundTrip(t *testing.T) {
	store := NewMemory()

	for _, size := range []int{1 << 10, 10 << 10, 100 << 10, 1 << 20} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			value := make([]byte, size)
			for i := range value {
				value[i] = byte(i % 256)
			}
			if got := putAndGet(t, store, "large", value); !bytes.Equal(got, value) {
				t.Fatalf("a %d-byte value did not round-trip", size)
			}
		})
	}
}

// Keys are opaque bytes: separators, whitespace and control characters, and
// long keys, must all round-trip unchanged.
func TestStoreUnusualKeysRoundTrip(t *testing.T) {
	store := NewMemory()

	keys := []string{
		"key/with/slashes",
		"key:with:colons",
		"key with spaces",
		"key\twith\ttabs",
		"key\nwith\nnewlines",
		"key@with#special$chars%",
		"key[with]brackets{and}braces",
		"a",
		strings.Repeat("abcdefghijklmnopqrstuvwxyz", 20), // 520 bytes
	}
	for _, key := range keys {
		t.Run(strconv.Quote(key), func(t *testing.T) {
			if got := putAndGet(t, store, key, []byte("v")); string(got) != "v" {
				t.Fatalf("got %q, want %q", got, "v")
			}
		})
	}
}

// Scan hands the callback each stored value with its own key.
func TestStoreScanYieldsStoredValues(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()

	want := map[string]string{"p/key1": "value1", "p/key2": "value2", "p/key3": "value3"}
	for k, v := range want {
		if err := store.Put(ctx, k, []byte(v)); err != nil {
			t.Fatalf("put: %v", err)
		}
	}

	got := make(map[string]string)
	if err := store.Scan(ctx, "p/", func(key string, value []byte) error {
		got[key] = string(value)
		return nil
	}); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("scanned %d entries, want %d", len(got), len(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("key %q: got %q, want %q", k, got[k], v)
		}
	}
}

// Each goroutine owns its keys, so every put, get and delete must succeed
// and read back exactly what that goroutine wrote.
func TestStoreConcurrentPutGetDelete(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()

	const workers, perWorker = 10, 10
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				key := "mixed-" + strconv.Itoa(w) + "-" + strconv.Itoa(i)
				if err := store.Put(ctx, key, []byte(key)); err != nil {
					t.Errorf("put %s: %v", key, err)
					continue
				}
				got, err := store.Get(ctx, key)
				if err != nil || string(got) != key {
					t.Errorf("get %s: got %q, %v", key, got, err)
					continue
				}
				if err := store.Delete(ctx, key); err != nil {
					t.Errorf("delete %s: %v", key, err)
				}
			}
		}(w)
	}
	wg.Wait()

	keys, err := store.List(ctx, "mixed-")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("%d keys survived their deletes", len(keys))
	}
}

// Concurrent scans over a stable prefix must each see every key.
func TestStoreConcurrentScansSeeEveryKey(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()

	const numKeys, scanners = 50, 10
	for i := 0; i < numKeys; i++ {
		key := "scan-" + strconv.Itoa(i)
		if err := store.Put(ctx, key, []byte(key)); err != nil {
			t.Fatalf("put: %v", err)
		}
	}

	var wg sync.WaitGroup
	wg.Add(scanners)
	for s := 0; s < scanners; s++ {
		go func() {
			defer wg.Done()
			count := 0
			if err := store.Scan(ctx, "scan-", func(string, []byte) error {
				count++
				return nil
			}); err != nil {
				t.Errorf("scan: %v", err)
				return
			}
			if count != numKeys {
				t.Errorf("scan saw %d keys, want %d", count, numKeys)
			}
		}()
	}
	wg.Wait()
}
