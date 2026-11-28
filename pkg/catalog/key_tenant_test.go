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
	"errors"
	"testing"
)

// TestTenantScopedKey_ParsesAndValidates checks that the namespace the object
// storage service writes for a tenant, tenant/{tenant}/{namespace}, is a valid
// catalog key.
func TestTenantScopedKey_ParsesAndValidates(t *testing.T) {
	const raw = "tenant/tenant-premium/premium:data/important.bin"

	key, err := ParseKey(raw)
	if err != nil {
		t.Fatalf("ParseKey(%q) error = %v", raw, err)
	}
	if key.Namespace != "tenant/tenant-premium/premium" {
		t.Errorf("Namespace = %q, want %q", key.Namespace, "tenant/tenant-premium/premium")
	}
	if key.Path != "data/important.bin" {
		t.Errorf("Path = %q, want %q", key.Path, "data/important.bin")
	}
	if got := key.String(); got != raw {
		t.Errorf("String() = %q, want %q", got, raw)
	}

	built, err := NewObjectKey("tenant/tenant-premium/premium", "data/important.bin")
	if err != nil {
		t.Fatalf("NewObjectKey error = %v", err)
	}
	if got := built.String(); got != raw {
		t.Errorf("NewObjectKey String() = %q, want %q", got, raw)
	}
}

// TestTenantScopedKey_RejectsMalformedScopes checks that only the exact form
// tenant/{segment}/{segment} is accepted; every other slash stays invalid.
func TestTenantScopedKey_RejectsMalformedScopes(t *testing.T) {
	keys := []string{
		"tenant/:a/b",
		"tenant/acme:a/b",
		"tenant/acme/ns/extra:a/b",
		"tenant//ns:a/b",
		"tenant/-acme/ns:a/b",
		"tenant/acme/ns!:a/b",
		"tenants/acme/ns:a/b",
		"other/acme/ns:a/b",
		"/tenant/acme/ns:a/b",
	}
	for _, raw := range keys {
		t.Run(raw, func(t *testing.T) {
			_, err := ParseKey(raw)
			if err == nil {
				t.Fatalf("ParseKey(%q) succeeded, want an invalid namespace error", raw)
			}
			var ce *CatalogError
			if !errors.As(err, &ce) || ce.Code != ErrCodeNamespaceInvalid {
				t.Errorf("ParseKey(%q) error = %v, want code %v", raw, err, ErrCodeNamespaceInvalid)
			}
		})
	}
}

// TestTenantScopedKey_StorageRoundTrip stores, reads, lists and deletes a
// tenant-scoped entry on the KV-backed catalog.
func TestTenantScopedKey_StorageRoundTrip(t *testing.T) {
	ctx := context.Background()
	storage, err := NewKVStorage(NewMemoryKV())
	if err != nil {
		t.Fatalf("NewKVStorage error = %v", err)
	}
	mgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	if err != nil {
		t.Fatalf("NewCatalogManager error = %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	newEntry := func(key, tenant string) *CatalogEntry {
		return &CatalogEntry{
			Key:      key,
			ObjectID: 1,
			Size:     3,
			TenantID: tenant,
			Status:   StatusActive,
			PrimaryStore: StoreLocation{
				Store:     "local",
				ObjectRef: key,
				Status:    LocationStatusActive,
			},
		}
	}

	const keyA = "tenant/tenant-a/docs:report.pdf"
	const keyB = "tenant/tenant-b/docs:report.pdf"
	if err := storage.PutEntry(ctx, newEntry(keyA, "tenant-a")); err != nil {
		t.Fatalf("PutEntry(%q) error = %v", keyA, err)
	}
	if err := storage.PutEntry(ctx, newEntry(keyB, "tenant-b")); err != nil {
		t.Fatalf("PutEntry(%q) error = %v", keyB, err)
	}

	got, err := storage.GetEntry(ctx, keyA)
	if err != nil {
		t.Fatalf("GetEntry(%q) error = %v", keyA, err)
	}
	if got.Key != keyA || got.TenantID != "tenant-a" || got.PrimaryStore.Store != "local" {
		t.Errorf("GetEntry = {Key:%q TenantID:%q Store:%q}", got.Key, got.TenantID, got.PrimaryStore.Store)
	}

	list, err := mgr.ListByPrefix(ctx, "tenant/tenant-a/", nil)
	if err != nil {
		t.Fatalf("ListByPrefix error = %v", err)
	}
	if len(list.Keys) != 1 || list.Keys[0] != keyA {
		t.Errorf("ListByPrefix(tenant/tenant-a/) = %v, want [%s]", list.Keys, keyA)
	}

	if err := storage.DeleteEntry(ctx, keyA); err != nil {
		t.Fatalf("DeleteEntry(%q) error = %v", keyA, err)
	}
	if _, err := storage.GetEntry(ctx, keyA); !IsNotFound(err) {
		t.Errorf("GetEntry after delete error = %v, want not found", err)
	}
	if _, err := storage.GetEntry(ctx, keyB); err != nil {
		t.Errorf("GetEntry(%q) after deleting tenant-a's entry error = %v", keyB, err)
	}
}
