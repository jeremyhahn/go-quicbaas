//go:build rag

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

package indexing

import (
	"context"
	"errors"
	"testing"
)

func TestMetadataStore_PutAndGet_Success(t *testing.T) {
	store := NewMetadataStore(newMemoryKVStore())
	ctx := context.Background()

	record := &FileRecord{
		Namespace: "test-ns",
		FilePath:  "/tmp/test.go",
		Checksum:  "abc123",
		ChunkIDs:  []string{"chunk-1", "chunk-2"},
		IndexedAt: 1000000,
		Size:      512,
		Language:  "go",
	}

	if err := store.PutFileRecord(ctx, record); err != nil {
		t.Fatalf("PutFileRecord failed: %v", err)
	}

	got, err := store.GetFileRecord(ctx, "test-ns", "/tmp/test.go")
	if err != nil {
		t.Fatalf("GetFileRecord failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected record, got nil")
	}
	if got.Checksum != "abc123" {
		t.Fatalf("expected checksum abc123, got %s", got.Checksum)
	}
	if got.Language != "go" {
		t.Fatalf("expected language go, got %s", got.Language)
	}
	if len(got.ChunkIDs) != 2 {
		t.Fatalf("expected 2 chunk IDs, got %d", len(got.ChunkIDs))
	}
	if got.Size != 512 {
		t.Fatalf("expected size 512, got %d", got.Size)
	}
}

func TestMetadataStore_Get_NotFound(t *testing.T) {
	store := NewMetadataStore(newMemoryKVStore())
	ctx := context.Background()

	got, err := store.GetFileRecord(ctx, "test-ns", "/nonexistent")
	if err != nil {
		t.Fatalf("GetFileRecord should return nil for missing key, got error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for missing record, got %+v", got)
	}
}

func TestMetadataStore_Delete_Success(t *testing.T) {
	store := NewMetadataStore(newMemoryKVStore())
	ctx := context.Background()

	record := &FileRecord{
		Namespace: "test-ns",
		FilePath:  "/tmp/deleteme.go",
		Checksum:  "del123",
		ChunkIDs:  []string{"chunk-1"},
		IndexedAt: 1000000,
		Size:      256,
		Language:  "go",
	}

	if err := store.PutFileRecord(ctx, record); err != nil {
		t.Fatalf("PutFileRecord failed: %v", err)
	}

	if err := store.DeleteFileRecord(ctx, "test-ns", "/tmp/deleteme.go"); err != nil {
		t.Fatalf("DeleteFileRecord failed: %v", err)
	}

	got, err := store.GetFileRecord(ctx, "test-ns", "/tmp/deleteme.go")
	if err != nil {
		t.Fatalf("GetFileRecord after delete failed: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after deletion")
	}
}

func TestMetadataStore_Delete_NonexistentKey(t *testing.T) {
	store := NewMetadataStore(newMemoryKVStore())
	ctx := context.Background()

	// Deleting a non-existent key should not error.
	if err := store.DeleteFileRecord(ctx, "test-ns", "/nonexistent"); err != nil {
		t.Fatalf("DeleteFileRecord on missing key should not error, got: %v", err)
	}
}

func TestMetadataStore_List_Success(t *testing.T) {
	store := NewMetadataStore(newMemoryKVStore())
	ctx := context.Background()

	records := []*FileRecord{
		{Namespace: "ns1", FilePath: "/file1.go", Checksum: "a", ChunkIDs: []string{"c1"}, Language: "go"},
		{Namespace: "ns1", FilePath: "/file2.go", Checksum: "b", ChunkIDs: []string{"c2"}, Language: "go"},
		{Namespace: "ns2", FilePath: "/file3.py", Checksum: "c", ChunkIDs: []string{"c3"}, Language: "python"},
	}

	for _, r := range records {
		if err := store.PutFileRecord(ctx, r); err != nil {
			t.Fatalf("PutFileRecord failed: %v", err)
		}
	}

	ns1Records, err := store.ListFileRecords(ctx, "ns1")
	if err != nil {
		t.Fatalf("ListFileRecords failed: %v", err)
	}
	if len(ns1Records) != 2 {
		t.Fatalf("expected 2 records for ns1, got %d", len(ns1Records))
	}

	ns2Records, err := store.ListFileRecords(ctx, "ns2")
	if err != nil {
		t.Fatalf("ListFileRecords failed: %v", err)
	}
	if len(ns2Records) != 1 {
		t.Fatalf("expected 1 record for ns2, got %d", len(ns2Records))
	}
}

func TestMetadataStore_List_EmptyNamespace(t *testing.T) {
	store := NewMetadataStore(newMemoryKVStore())
	ctx := context.Background()

	records, err := store.ListFileRecords(ctx, "empty-ns")
	if err != nil {
		t.Fatalf("ListFileRecords on empty namespace should not error, got: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records, got %d", len(records))
	}
}

func TestMetadataStore_PutOverwrite_Success(t *testing.T) {
	store := NewMetadataStore(newMemoryKVStore())
	ctx := context.Background()

	record := &FileRecord{
		Namespace: "test-ns",
		FilePath:  "/tmp/overwrite.go",
		Checksum:  "v1",
		ChunkIDs:  []string{"c1"},
		Language:  "go",
	}
	if err := store.PutFileRecord(ctx, record); err != nil {
		t.Fatalf("PutFileRecord failed: %v", err)
	}

	record.Checksum = "v2"
	record.ChunkIDs = []string{"c2", "c3"}
	if err := store.PutFileRecord(ctx, record); err != nil {
		t.Fatalf("PutFileRecord overwrite failed: %v", err)
	}

	got, err := store.GetFileRecord(ctx, "test-ns", "/tmp/overwrite.go")
	if err != nil {
		t.Fatalf("GetFileRecord failed: %v", err)
	}
	if got.Checksum != "v2" {
		t.Fatalf("expected checksum v2, got %s", got.Checksum)
	}
	if len(got.ChunkIDs) != 2 {
		t.Fatalf("expected 2 chunk IDs after overwrite, got %d", len(got.ChunkIDs))
	}
}

func TestMetadataStore_Get_FailingKVStore(t *testing.T) {
	kvErr := errors.New("kv read error")
	store := NewMetadataStore(newFailingKVStore(kvErr))
	ctx := context.Background()

	_, err := store.GetFileRecord(ctx, "ns", "/file")
	if err == nil {
		t.Fatal("expected error from failing kv store")
	}
	var metaErr *MetadataStoreError
	if !errors.As(err, &metaErr) {
		t.Fatalf("expected *MetadataStoreError, got %T", err)
	}
	if metaErr.Operation != "get" {
		t.Fatalf("expected operation get, got %s", metaErr.Operation)
	}
}

func TestMetadataStore_Put_FailingKVStore(t *testing.T) {
	kvErr := errors.New("kv write error")
	store := NewMetadataStore(newFailingKVStore(kvErr))
	ctx := context.Background()

	record := &FileRecord{
		Namespace: "ns",
		FilePath:  "/file",
		Checksum:  "x",
	}
	err := store.PutFileRecord(ctx, record)
	if err == nil {
		t.Fatal("expected error from failing kv store")
	}
	var metaErr *MetadataStoreError
	if !errors.As(err, &metaErr) {
		t.Fatalf("expected *MetadataStoreError, got %T", err)
	}
	if metaErr.Operation != "put" {
		t.Fatalf("expected operation put, got %s", metaErr.Operation)
	}
}

func TestMetadataStore_Delete_FailingKVStore(t *testing.T) {
	kvErr := errors.New("kv delete error")
	store := NewMetadataStore(newFailingKVStore(kvErr))
	ctx := context.Background()

	err := store.DeleteFileRecord(ctx, "ns", "/file")
	if err == nil {
		t.Fatal("expected error from failing kv store")
	}
	var metaErr *MetadataStoreError
	if !errors.As(err, &metaErr) {
		t.Fatalf("expected *MetadataStoreError, got %T", err)
	}
	if metaErr.Operation != "delete" {
		t.Fatalf("expected operation delete, got %s", metaErr.Operation)
	}
}

func TestMetadataStore_List_FailingKVStore(t *testing.T) {
	kvErr := errors.New("kv scan error")
	store := NewMetadataStore(newFailingKVStore(kvErr))
	ctx := context.Background()

	_, err := store.ListFileRecords(ctx, "ns")
	if err == nil {
		t.Fatal("expected error from failing kv store")
	}
}

func TestBuildKey_DeterministicAndUnique(t *testing.T) {
	key1 := buildKey("ns", "/path/to/file1.go")
	key2 := buildKey("ns", "/path/to/file2.go")
	key3 := buildKey("ns", "/path/to/file1.go")

	if key1 == key2 {
		t.Fatal("different file paths should produce different keys")
	}
	if key1 != key3 {
		t.Fatal("same inputs should produce the same key")
	}

	// Verify prefix format.
	if len(key1) < 4 || key1[:4] != "idx:" {
		t.Fatalf("key should start with 'idx:', got %s", key1)
	}
}

func TestBuildKey_DifferentNamespaces(t *testing.T) {
	key1 := buildKey("ns1", "/file.go")
	key2 := buildKey("ns2", "/file.go")

	if key1 == key2 {
		t.Fatal("different namespaces should produce different keys")
	}
}
