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
	"strconv"
	"testing"
)

// The orchestration stores sit directly on this adapter, so its per-operation
// cost is the floor for every project, agent and task read. These benchmarks
// exist to catch a regression in that floor — particularly in Scan, where a
// prefix-bounded iterator must not degrade into a full-keyspace walk.

func BenchmarkStorePut(b *testing.B) {
	ctx := context.Background()
	store := NewMemory()
	value := []byte("orchestration record payload")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := store.Put(ctx, "orch/task/tenant/"+strconv.Itoa(i), value); err != nil {
			b.Fatalf("put: %v", err)
		}
	}
}

func BenchmarkStoreGet(b *testing.B) {
	ctx := context.Background()
	store := seedStore(b, 10000)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.Get(ctx, "orch/task/tenant-5/"+strconv.Itoa(i%1000)); err != nil {
			b.Fatalf("get: %v", err)
		}
	}
}

func BenchmarkStoreExists(b *testing.B) {
	ctx := context.Background()
	store := seedStore(b, 10000)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.Exists(ctx, "orch/task/tenant-5/"+strconv.Itoa(i%1000)); err != nil {
			b.Fatalf("exists: %v", err)
		}
	}
}

// BenchmarkStoreScanTenantPrefix measures a single tenant's listing against a
// store holding ten tenants. Cost must track the tenant's own record count,
// not the total keyspace — that is what makes a tenant-scoped list affordable.
func BenchmarkStoreScanTenantPrefix(b *testing.B) {
	ctx := context.Background()
	store := seedStore(b, 10000)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		count := 0
		if err := store.Scan(ctx, "orch/task/tenant-5/", func(string, []byte) error {
			count++
			return nil
		}); err != nil {
			b.Fatalf("scan: %v", err)
		}
		if count != 1000 {
			b.Fatalf("scanned %d records, want 1000", count)
		}
	}
}

func BenchmarkStoreListTenantPrefix(b *testing.B) {
	ctx := context.Background()
	store := seedStore(b, 10000)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		keys, err := store.List(ctx, "orch/task/tenant-5/")
		if err != nil {
			b.Fatalf("list: %v", err)
		}
		if len(keys) != 1000 {
			b.Fatalf("listed %d keys, want 1000", len(keys))
		}
	}
}

// seedStore fills a store with total records spread evenly across ten tenants.
func seedStore(b *testing.B, total int) *Store {
	b.Helper()
	ctx := context.Background()
	store := NewMemory()
	value := []byte("orchestration record payload")

	perTenant := total / 10
	for tenant := 0; tenant < 10; tenant++ {
		prefix := "orch/task/tenant-" + strconv.Itoa(tenant) + "/"
		for i := 0; i < perTenant; i++ {
			if err := store.Put(ctx, prefix+strconv.Itoa(i), value); err != nil {
				b.Fatalf("seed put: %v", err)
			}
		}
	}
	return store
}
