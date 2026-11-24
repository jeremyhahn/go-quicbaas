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
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// The "qrdb" half of this suite runs against a real go-qrdb node, under the
// Unit Test Contract's consensus-arbitration carve-out: the property under
// test is that the STORE applies a catalog entry and its index keys as one
// transaction, and an in-memory double that agrees with this package would
// prove nothing about that. The adapter's request shape is tested with a
// double in kv_qrdb_test.go. The "memory" half runs the in-memory KV that
// this package's data-shaped tests use; this suite is what proves that double
// agrees with go-qrdb.

// contractNamespaces hands each qrdb-backed test a namespace no other test
// uses, so no two tests see each other's keys on the shared node.
var contractNamespaces atomic.Uint64

func TestStorageContract(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		runStorageContract(t, newMemoryStorage)
	})

	t.Run("qrdb", func(t *testing.T) {
		runStorageContract(t, func(t *testing.T) Storage {
			t.Helper()
			n := qrdbnode.Shared(t)
			ns := "__sys:catalog-test/" + strconv.FormatUint(contractNamespaces.Add(1), 10) + "/"
			kv, err := NewQRDBKV(n.KV, n.Transaction, ns)
			if err != nil {
				t.Fatalf("NewQRDBKV() error = %v", err)
			}
			s, err := NewKVStorage(kv)
			if err != nil {
				t.Fatalf("NewKVStorage() error = %v", err)
			}
			t.Cleanup(func() { _ = s.Close() })
			return s
		})
	})
}

// newMemoryStorage returns a Storage over a fresh in-memory KV, closed when
// the test ends.
func newMemoryStorage(t *testing.T) Storage {
	t.Helper()
	s, err := NewKVStorage(NewMemoryKV())
	if err != nil {
		t.Fatalf("NewKVStorage() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// runStorageContract checks the behaviour every Storage must share, whatever
// KV it runs on.
func runStorageContract(t *testing.T, newStorage func(t *testing.T) Storage) {
	ctx := context.Background()

	t.Run("put get delete", func(t *testing.T) {
		s := newStorage(t)
		entry := testEntry("ns1:docs/a.txt", "s3", "tenant-a", StatusActive)
		if err := s.PutEntry(ctx, entry); err != nil {
			t.Fatalf("PutEntry() error = %v", err)
		}
		got, err := s.GetEntry(ctx, entry.Key)
		if err != nil {
			t.Fatalf("GetEntry() error = %v", err)
		}
		if got.Key != entry.Key || got.Size != entry.Size || got.TenantID != entry.TenantID {
			t.Fatalf("GetEntry() = %+v, want key %q size %d tenant %q", got, entry.Key, entry.Size, entry.TenantID)
		}
		if err := s.DeleteEntry(ctx, entry.Key); err != nil {
			t.Fatalf("DeleteEntry() error = %v", err)
		}
		if _, err := s.GetEntry(ctx, entry.Key); !IsNotFound(err) {
			t.Fatalf("GetEntry() after delete error = %v, want entry not found", err)
		}
	})

	t.Run("batch put get delete", func(t *testing.T) {
		s := newStorage(t)
		var entries []*CatalogEntry
		var keys []string
		for i := 0; i < 3; i++ {
			e := testEntry(fmt.Sprintf("ns1:batch/%d", i), "s3", "tenant-a", StatusActive)
			entries = append(entries, e)
			keys = append(keys, e.Key)
		}
		if err := s.PutEntries(ctx, entries); err != nil {
			t.Fatalf("PutEntries() error = %v", err)
		}
		got, err := s.GetEntries(ctx, keys)
		if err != nil {
			t.Fatalf("GetEntries() error = %v", err)
		}
		if len(got) != len(keys) {
			t.Fatalf("GetEntries() returned %d entries, want %d", len(got), len(keys))
		}
		if err := s.DeleteEntries(ctx, keys); err != nil {
			t.Fatalf("DeleteEntries() error = %v", err)
		}
		got, err = s.GetEntries(ctx, keys)
		if err != nil {
			t.Fatalf("GetEntries() after delete error = %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("GetEntries() after delete returned %d entries, want 0", len(got))
		}
	})

	t.Run("iterate in order under prefix", func(t *testing.T) {
		s := newStorage(t)
		inside := []string{"ns1:iter/a", "ns1:iter/b", "ns1:iter/c"}
		// Written out of order, plus keys outside the prefix.
		for _, k := range []string{"ns1:iter/c", "ns2:iter/a", "ns1:iter/a", "ns1:other/z", "ns1:iter/b"} {
			if err := s.PutEntry(ctx, testEntry(k, "s3", "tenant-a", StatusActive)); err != nil {
				t.Fatalf("PutEntry(%q) error = %v", k, err)
			}
		}

		var gotEntries []string
		if err := s.Iterate(ctx, "ns1:iter/", func(e *CatalogEntry) bool {
			gotEntries = append(gotEntries, e.Key)
			return true
		}); err != nil {
			t.Fatalf("Iterate() error = %v", err)
		}
		assertKeys(t, "Iterate", gotEntries, inside)

		var gotKeys []string
		if err := s.IterateKeys(ctx, "ns1:iter/", func(k string) bool {
			gotKeys = append(gotKeys, k)
			return true
		}); err != nil {
			t.Fatalf("IterateKeys() error = %v", err)
		}
		assertKeys(t, "IterateKeys", gotKeys, inside)

		var first []string
		if err := s.Iterate(ctx, "ns1:iter/", func(e *CatalogEntry) bool {
			first = append(first, e.Key)
			return false
		}); err != nil {
			t.Fatalf("Iterate() stop error = %v", err)
		}
		assertKeys(t, "Iterate stop", first, inside[:1])

		var firstKeys []string
		if err := s.IterateKeys(ctx, "ns1:iter/", func(k string) bool {
			firstKeys = append(firstKeys, k)
			return len(firstKeys) < 2
		}); err != nil {
			t.Fatalf("IterateKeys() stop error = %v", err)
		}
		assertKeys(t, "IterateKeys stop", firstKeys, inside[:2])
	})

	t.Run("indexes follow the entry", func(t *testing.T) {
		s := newStorage(t)
		idx := s.Indexes()
		entry := testEntry("ns1:idx/a", "store-old", "tenant-idx", StatusActive)
		if err := s.PutEntry(ctx, entry); err != nil {
			t.Fatalf("PutEntry() error = %v", err)
		}
		assertListed(t, "ListByStore(store-old)", mustList(t)(idx.ListByStore(ctx, "store-old", nil)), entry.Key, true)
		assertListed(t, "ListByTenant", mustList(t)(idx.ListByTenant(ctx, "tenant-idx", nil)), entry.Key, true)
		assertListed(t, "ListByStatus", mustList(t)(idx.ListByStatus(ctx, StatusActive, nil)), entry.Key, true)
		if n, err := idx.CountByStore(ctx, "store-old"); err != nil || n != 1 {
			t.Fatalf("CountByStore(store-old) = %d, %v, want 1", n, err)
		}

		// An update that moves the primary store: the store index follows.
		old, err := s.GetEntry(ctx, entry.Key)
		if err != nil {
			t.Fatalf("GetEntry() error = %v", err)
		}
		updated := testEntry(entry.Key, "store-new", "tenant-idx", StatusActive)
		batch := &KVBatch{}
		if err := idx.UpdateIndexes(batch, old, updated); err != nil {
			t.Fatalf("UpdateIndexes() error = %v", err)
		}
		if err := s.PutEntry(ctx, updated); err != nil {
			t.Fatalf("PutEntry(updated) error = %v", err)
		}
		if err := applyBatch(ctx, s, batch); err != nil {
			t.Fatalf("apply update batch: %v", err)
		}
		assertListed(t, "ListByStore(store-old) after move", mustList(t)(idx.ListByStore(ctx, "store-old", nil)), entry.Key, false)
		assertListed(t, "ListByStore(store-new)", mustList(t)(idx.ListByStore(ctx, "store-new", nil)), entry.Key, true)
		if n, err := idx.CountByStore(ctx, "store-new"); err != nil || n != 1 {
			t.Fatalf("CountByStore(store-new) = %d, %v, want 1", n, err)
		}

		if err := s.DeleteEntry(ctx, entry.Key); err != nil {
			t.Fatalf("DeleteEntry() error = %v", err)
		}
		assertListed(t, "ListByStore(store-new) after delete", mustList(t)(idx.ListByStore(ctx, "store-new", nil)), entry.Key, false)
		assertListed(t, "ListByTenant after delete", mustList(t)(idx.ListByTenant(ctx, "tenant-idx", nil)), entry.Key, false)
		assertListed(t, "ListByStatus after delete", mustList(t)(idx.ListByStatus(ctx, StatusActive, nil)), entry.Key, false)
		if n, err := idx.CountByStore(ctx, "store-new"); err != nil || n != 0 {
			t.Fatalf("CountByStore(store-new) after delete = %d, %v, want 0", n, err)
		}
	})

	t.Run("index paging", func(t *testing.T) {
		s := newStorage(t)
		idx := s.Indexes()
		var want []string
		for i := 0; i < 5; i++ {
			k := fmt.Sprintf("ns1:page/%d", i)
			want = append(want, k)
			if err := s.PutEntry(ctx, testEntry(k, "store-page", "tenant-page", StatusActive)); err != nil {
				t.Fatalf("PutEntry(%q) error = %v", k, err)
			}
		}
		var got []string
		cursor := ""
		for page := 0; page < 10; page++ {
			res, err := idx.ListByStore(ctx, "store-page", &ListOptions{Limit: 2, Cursor: cursor})
			if err != nil {
				t.Fatalf("ListByStore() page %d error = %v", page, err)
			}
			got = append(got, res.Keys...)
			if !res.HasMore {
				break
			}
			if res.NextCursor == "" {
				t.Fatalf("ListByStore() page %d has more but no cursor", page)
			}
			cursor = res.NextCursor
		}
		assertKeys(t, "ListByStore pages", got, want)
	})

	t.Run("closed store refuses", func(t *testing.T) {
		s := newStorage(t)
		if err := s.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if _, err := s.GetEntry(ctx, "ns1:closed"); !IsCatalogClosed(err) {
			t.Fatalf("GetEntry() after Close error = %v, want catalog closed", err)
		}
		if err := s.PutEntry(ctx, testEntry("ns1:closed", "s3", "t", StatusActive)); !IsCatalogClosed(err) {
			t.Fatalf("PutEntry() after Close error = %v, want catalog closed", err)
		}
	})
}

// applyBatch commits a batch built through the index manager on the store's
// KV, as a caller that composes its own batch would.
func applyBatch(ctx context.Context, s Storage, batch *KVBatch) error {
	ks, ok := s.(*kvStorage)
	if !ok {
		return fmt.Errorf("storage %T is not a kvStorage", s)
	}
	return ks.kv.Apply(ctx, batch)
}

// assertStoreRoundTrip proves a freshly opened store is usable: an entry put
// into it reads back.
func assertStoreRoundTrip(t *testing.T, s Storage) {
	t.Helper()
	ctx := context.Background()
	entry := testEntry("ns1:open/check", "s3", "tenant-open", StatusActive)
	if err := s.PutEntry(ctx, entry); err != nil {
		t.Fatalf("PutEntry() on the opened store error = %v", err)
	}
	got, err := s.GetEntry(ctx, entry.Key)
	if err != nil {
		t.Fatalf("GetEntry() on the opened store error = %v", err)
	}
	if got.Key != entry.Key {
		t.Fatalf("GetEntry() key = %q, want %q", got.Key, entry.Key)
	}
}

func mustList(t *testing.T) func(*ListResult, error) *ListResult {
	t.Helper()
	return func(res *ListResult, err error) *ListResult {
		t.Helper()
		if err != nil {
			t.Fatalf("list error = %v", err)
		}
		return res
	}
}

func assertListed(t *testing.T, what string, res *ListResult, key string, want bool) {
	t.Helper()
	found := false
	for _, k := range res.Keys {
		if k == key {
			found = true
		}
	}
	if found != want {
		t.Fatalf("%s: key %q listed = %v, want %v (keys %v)", what, key, found, want, res.Keys)
	}
}

func assertKeys(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", what, got, want)
		}
	}
}
