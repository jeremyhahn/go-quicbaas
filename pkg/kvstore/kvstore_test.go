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

package kvstore

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	})

	if err := store.Put(ctx, "alpha", []byte("one")); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := store.Get(ctx, "alpha")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "one" {
		t.Fatalf("got %q, want %q", got, "one")
	}

	exists, err := store.Exists(ctx, "alpha")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Fatal("exists must report true for a stored key")
	}

	if err := store.Delete(ctx, "alpha"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Get(ctx, "alpha"); !qberrors.IsNotFound(err) {
		t.Fatalf("get after delete must be not-found, got %v", err)
	}
}

// A missing key must surface as the canonical quicbaas not-found error so that
// every store built on this adapter can rely on errors.IsNotFound.
func TestStoreGetMissingKeyIsNotFound(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()

	tests := []struct {
		name string
		key  string
	}{
		{name: "absent key", key: "nope"},
		{name: "empty key", key: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.Get(ctx, tc.key)
			if !qberrors.IsNotFound(err) {
				t.Fatalf("want not-found, got %v", err)
			}
		})
	}
}

// Scan and List must be bounded by the prefix and ordered, because the
// orchestration stores rely on a tenant-first prefix to guarantee that a scan
// cannot observe another tenant's records.
func TestStoreScanIsPrefixBoundedAndOrdered(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()

	seed := map[string]string{
		"orch/project/tenant-a/p2": "a2",
		"orch/project/tenant-a/p1": "a1",
		"orch/project/tenant-b/p1": "b1",
		"orch/project/tenant-ab/x": "ab",
		"orch/task/tenant-a/t1":    "t1",
	}
	for k, v := range seed {
		if err := store.Put(ctx, k, []byte(v)); err != nil {
			t.Fatalf("put %s: %v", k, err)
		}
	}

	tests := []struct {
		name     string
		prefix   string
		wantKeys []string
	}{
		{
			name:   "tenant scoped prefix excludes sibling tenants",
			prefix: "orch/project/tenant-a/",
			wantKeys: []string{
				"orch/project/tenant-a/p1",
				"orch/project/tenant-a/p2",
			},
		},
		{
			name:     "prefix does not bleed into lexically adjacent tenant",
			prefix:   "orch/project/tenant-b/",
			wantKeys: []string{"orch/project/tenant-b/p1"},
		},
		{
			name:     "no matches yields empty result",
			prefix:   "orch/project/tenant-zz/",
			wantKeys: nil,
		},
		{
			name:   "empty prefix scans everything in order",
			prefix: "",
			wantKeys: []string{
				"orch/project/tenant-a/p1",
				"orch/project/tenant-a/p2",
				"orch/project/tenant-ab/x",
				"orch/project/tenant-b/p1",
				"orch/task/tenant-a/t1",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var scanned []string
			if err := store.Scan(ctx, tc.prefix, func(key string, _ []byte) error {
				scanned = append(scanned, key)
				return nil
			}); err != nil {
				t.Fatalf("scan: %v", err)
			}
			assertKeys(t, "scan", scanned, tc.wantKeys)

			listed, err := store.List(ctx, tc.prefix)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			assertKeys(t, "list", listed, tc.wantKeys)
		})
	}
}

// Scan must stop and propagate when the callback fails, and must never hand
// the callback a slice that the engine may reuse.
func TestStoreScanCallbackErrorAndValueIsolation(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()

	for _, k := range []string{"k1", "k2", "k3"} {
		if err := store.Put(ctx, k, []byte(k+"-value")); err != nil {
			t.Fatalf("put: %v", err)
		}
	}

	sentinel := errors.New("stop")
	seen := 0
	err := store.Scan(ctx, "k", func(_ string, _ []byte) error {
		seen++
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want callback error propagated, got %v", err)
	}
	if seen != 1 {
		t.Fatalf("scan must stop on first callback error, visited %d entries", seen)
	}

	// Retain and mutate the value handed to the callback; the store must not
	// be affected because Scan copies before yielding.
	var retained []byte
	if err := store.Scan(ctx, "k1", func(_ string, value []byte) error {
		retained = value
		return nil
	}); err != nil {
		t.Fatalf("scan: %v", err)
	}
	retained[0] = 'X'

	got, err := store.Get(ctx, "k1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "k1-value" {
		t.Fatalf("mutating a scanned value corrupted the store: %q", got)
	}
}

// A cancelled context must abort iteration rather than walking the keyspace.
func TestStoreScanRespectsContextCancellation(t *testing.T) {
	store := NewMemory()
	if err := store.Put(context.Background(), "a", []byte("1")); err != nil {
		t.Fatalf("put: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := store.Scan(ctx, "", func(string, []byte) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("scan must honour cancellation, got %v", err)
	}
	if _, err := store.List(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("list must honour cancellation, got %v", err)
	}
}

// Invalid input handling: an empty key is a programming error on write,
// a no-op on delete, and never reports existence.
func TestStoreEmptyKeyHandling(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()

	err := store.Put(ctx, "", []byte("value"))
	var emptyKey *EmptyKeyError
	if !errors.As(err, &emptyKey) {
		t.Fatalf("put with empty key must return EmptyKeyError, got %v", err)
	}
	if !errors.Is(err, ErrEmptyKey) {
		t.Fatal("EmptyKeyError must unwrap to ErrEmptyKey")
	}

	if err := store.Delete(ctx, ""); err != nil {
		t.Fatalf("delete with empty key must be a no-op, got %v", err)
	}

	exists, err := store.Exists(ctx, "")
	if err != nil {
		t.Fatalf("exists with empty key: %v", err)
	}
	if exists {
		t.Fatal("empty key must never report as existing")
	}
}

// Operations against a closed engine must report a typed OpError rather than
// panicking or silently succeeding.
func TestStoreOperationsOnClosedEngine(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	var opErr *OpError
	if err := store.Put(ctx, "k", []byte("v")); !errors.As(err, &opErr) {
		t.Fatalf("put on closed engine must return OpError, got %v", err)
	}
	if _, err := store.Get(ctx, "k"); !errors.As(err, &opErr) {
		t.Fatalf("get on closed engine must return OpError, got %v", err)
	}
	if _, err := store.Exists(ctx, "k"); !errors.As(err, &opErr) {
		t.Fatalf("exists on closed engine must return OpError, got %v", err)
	}
}

// An empty value is a legitimate value, distinct from a missing key. Stores
// that use a valueless key as a marker (peer-messaging dedup keys, for one)
// depend on this.
func TestStoreEmptyValueIsDistinctFromMissingKey(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()

	if err := store.Put(ctx, "empty", []byte{}); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := store.Get(ctx, "empty")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty value, got %q", got)
	}

	exists, err := store.Exists(ctx, "empty")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Fatal("a key holding an empty value must still exist")
	}
}

// Concurrent access must be safe: every store built on this adapter is shared
// across request goroutines.
func TestStoreConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()

	const writers = 64
	var wg sync.WaitGroup

	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(n int) {
			defer wg.Done()
			key := "key-" + strconv.Itoa(n)
			if err := store.Put(ctx, key, []byte(key)); err != nil {
				t.Errorf("put %s: %v", key, err)
			}
		}(i)
	}
	wg.Wait()

	// Contended writes to a single key plus concurrent reads and scans.
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(n int) {
			defer wg.Done()
			if err := store.Put(ctx, "hot", []byte(strconv.Itoa(n))); err != nil {
				t.Errorf("put hot: %v", err)
			}
			if _, err := store.Get(ctx, "key-0"); err != nil {
				t.Errorf("get: %v", err)
			}
			if err := store.Scan(ctx, "key-", func(string, []byte) error { return nil }); err != nil {
				t.Errorf("scan: %v", err)
			}
		}(i)
	}
	wg.Wait()

	keys, err := store.List(ctx, "key-")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != writers {
		t.Fatalf("got %d keys, want %d", len(keys), writers)
	}
}

// Delete and Close must report engine failures as typed errors too, and the
// error messages must name the operation and key so an operator can act on
// them.
func TestStoreErrorReporting(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	var opErr *OpError
	if err := store.Delete(ctx, "k"); !errors.As(err, &opErr) {
		t.Fatalf("delete on closed engine must return OpError, got %v", err)
	}
	if err := store.Scan(ctx, "k", func(string, []byte) error { return nil }); !errors.As(err, &opErr) {
		t.Fatalf("scan on closed engine must return OpError, got %v", err)
	}
	if _, err := store.List(ctx, "k"); !errors.As(err, &opErr) {
		t.Fatalf("list on closed engine must return OpError, got %v", err)
	}
	// Close is idempotent: a second call must not manufacture an error.
	if err := store.Close(); err != nil {
		t.Fatalf("second close must be a no-op, got %v", err)
	}
}

func TestErrorMessages(t *testing.T) {
	engineFailure := errors.New("engine exploded")

	tests := []struct {
		name     string
		err      error
		contains []string
		unwraps  error
	}{
		{
			name:     "empty key names the operation",
			err:      &EmptyKeyError{Op: "put"},
			contains: []string{"empty key", "put"},
			unwraps:  ErrEmptyKey,
		},
		{
			name:     "op error names operation, key and cause",
			err:      &OpError{Op: "get", Key: "orch/task/t1/x", Cause: engineFailure},
			contains: []string{"get", "orch/task/t1/x", "engine exploded"},
			unwraps:  engineFailure,
		},
		{
			name:     "op error without a key or cause still identifies itself",
			err:      &OpError{Op: "close"},
			contains: []string{"kvstore", "close"},
			unwraps:  ErrKVStore,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			for _, want := range tc.contains {
				if !strings.Contains(msg, want) {
					t.Fatalf("message %q must contain %q", msg, want)
				}
			}
			if !errors.Is(tc.err, tc.unwraps) {
				t.Fatalf("%v must unwrap to %v", tc.err, tc.unwraps)
			}
		})
	}
}

func TestPrefixUpperBound(t *testing.T) {
	tests := []struct {
		name   string
		prefix []byte
		want   []byte
	}{
		{name: "ascii increments last byte", prefix: []byte("ab"), want: []byte("ac")},
		{name: "carries past trailing 0xFF", prefix: []byte{'a', 0xFF}, want: []byte{'b'}},
		{name: "all 0xFF has no successor", prefix: []byte{0xFF, 0xFF}, want: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := prefixUpperBound(tc.prefix)
			if string(got) != string(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHasPrefix(t *testing.T) {
	if !HasPrefix("orch/project/t1/p1", "orch/project/t1/") {
		t.Fatal("expected prefix match")
	}
	if HasPrefix("orch/project/t10/p1", "orch/project/t1/") {
		t.Fatal("unexpected prefix match across tenant boundary")
	}
}

func assertKeys(t *testing.T, op string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s returned %v, want %v", op, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s returned %v, want %v", op, got, want)
		}
	}
}
