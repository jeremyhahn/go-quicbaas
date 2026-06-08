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
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/watcher"
)

// newTestMemoryIndex builds an in-memory brute-force vector index for tests.
func newTestMemoryIndex(t *testing.T) rag.VectorIndex {
	t.Helper()
	idx, err := rag.NewVectorIndex(&rag.VectorIndexConfig{
		Dimensions:     64,
		DistanceMetric: rag.CosineDistance,
	})
	if err != nil {
		t.Fatalf("failed to create vector index: %v", err)
	}
	return idx
}

// --- QRDB vector error Error() messages (only Unwrap was previously covered) ---

func TestQRDBVectorCreateNamespaceError_Message(t *testing.T) {
	err := &QRDBVectorCreateNamespaceError{Namespace: "tenant-ns", Cause: errors.New("disk full")}
	msg := err.Error()
	if !strings.Contains(msg, "tenant-ns") {
		t.Fatalf("error message should contain namespace, got: %s", msg)
	}
	if !strings.Contains(msg, "disk full") {
		t.Fatalf("error message should contain cause, got: %s", msg)
	}
}

func TestQRDBVectorUpsertError_Message(t *testing.T) {
	err := &QRDBVectorUpsertError{ID: "vec-7", Cause: errors.New("timeout")}
	msg := err.Error()
	if !strings.Contains(msg, "vec-7") {
		t.Fatalf("error message should contain id, got: %s", msg)
	}
	if !strings.Contains(msg, "timeout") {
		t.Fatalf("error message should contain cause, got: %s", msg)
	}
}

func TestQRDBVectorSearchError_Message(t *testing.T) {
	err := &QRDBVectorSearchError{Cause: errors.New("index closed")}
	msg := err.Error()
	if !strings.Contains(msg, "index closed") {
		t.Fatalf("error message should contain cause, got: %s", msg)
	}
}

func TestQRDBVectorDeleteError_Message(t *testing.T) {
	err := &QRDBVectorDeleteError{ID: "vec-9", Cause: errors.New("not found")}
	msg := err.Error()
	if !strings.Contains(msg, "vec-9") {
		t.Fatalf("error message should contain id, got: %s", msg)
	}
	if !strings.Contains(msg, "not found") {
		t.Fatalf("error message should contain cause, got: %s", msg)
	}
}

// --- vector_adapter WithDistanceMetric option ---

func TestWithDistanceMetric_SetsMetric(t *testing.T) {
	opts := &vectorIndexOptions{}
	WithDistanceMetric("euclidean")(opts)
	if opts.distanceMetric != "euclidean" {
		t.Fatalf("expected distanceMetric euclidean, got %q", opts.distanceMetric)
	}
}

// --- vector_qrdb uint64ToBytes round-trips through stringToUint64 ---

func TestUint64ToBytes_BigEndianEncoding(t *testing.T) {
	v := stringToUint64("some-vector-id")
	b := uint64ToBytes(v)
	if len(b) != 8 {
		t.Fatalf("expected 8 bytes, got %d", len(b))
	}
	want := uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
	if want != v {
		t.Fatalf("big-endian decode mismatch: got %d want %d", want, v)
	}
}

// --- service handleWatcherEvent unknown type (warn + return branch) ---

func TestIndexingService_HandleWatcherEvent_UnknownType(t *testing.T) {
	svc, _, _ := newTestService(t)
	// An EventType not present in the dispatch map must hit the warn-and-return branch.
	svc.handleWatcherEvent(context.Background(), watcher.Event{Type: watcher.EventType(255), Path: "/x"})
	// No panic and no event emitted means the unhandled branch executed.
	select {
	case evt := <-svc.Events():
		t.Fatalf("unhandled event should not emit, got %v", evt)
	default:
	}
}

// --- service emitEvent drops when channel is full (default branch) ---

func TestIndexingService_EmitEvent_ChannelFull(t *testing.T) {
	svc, _, _ := newTestService(t)
	// Fill the buffered event channel to capacity so the next emit takes the
	// default (drop) branch instead of blocking.
	for i := 0; i < cap(svc.eventCh); i++ {
		svc.eventCh <- IndexEvent{Type: IndexEventFileAdded}
	}
	// This must not block; it takes the drop path.
	svc.emitEvent(IndexEvent{Type: IndexEventError})
	if len(svc.eventCh) != cap(svc.eventCh) {
		t.Fatalf("expected channel to remain at capacity, got len=%d cap=%d", len(svc.eventCh), cap(svc.eventCh))
	}
}

// --- service handleFileDelete error path (pipeline.RemoveFile fails) ---

func TestIndexingService_HandleFileDelete_RemoveError(t *testing.T) {
	cfg := IndexingConfig{
		Enabled:        true,
		IndexType:      "memory",
		BatchSize:      32,
		MaxConcurrency: 2,
		ChunkMaxTokens: 2000,
		ChunkOverlap:   200,
		ProjectPath:    t.TempDir(),
		ProjectID:      "test-project",
		Namespace:      "test-ns",
	}
	getErr := errors.New("metadata backend down")
	// failingKVStore makes GetFileRecord (and thus RemoveFile) return an error.
	metadata := NewMetadataStore(newFailingKVStore(getErr))
	embedder := newTestEmbeddingProvider(64)
	index := newTestMemoryIndex(t)
	pipeline := NewIndexingPipeline(cfg, embedder, index, metadata, newTestLogger())
	svc := newIndexingServiceWithWatcher(cfg, pipeline, newTestWatcher(), metadata, newTestLogger())

	svc.handleFileDelete(context.Background(), watcher.Event{Type: watcher.EventDelete, Path: "/gone"})

	// The error path emits an IndexEventError.
	select {
	case evt := <-svc.Events():
		if evt.Type != IndexEventError {
			t.Fatalf("expected IndexEventError, got %v", evt.Type)
		}
		if evt.Error == nil {
			t.Fatal("expected non-nil error on event")
		}
	default:
		t.Fatal("expected an error event to be emitted")
	}
}

// --- pipeline.RemoveFile GetFileRecord error path ---

func TestIndexingPipeline_RemoveFile_GetError(t *testing.T) {
	cfg := IndexingConfig{
		Enabled:        true,
		IndexType:      "memory",
		BatchSize:      32,
		MaxConcurrency: 2,
		ChunkMaxTokens: 2000,
		ChunkOverlap:   200,
		ProjectPath:    t.TempDir(),
		Namespace:      "test-ns",
	}
	backendErr := errors.New("kv get failed")
	metadata := NewMetadataStore(newFailingKVStore(backendErr))
	index := newTestMemoryIndex(t)
	pipeline := NewIndexingPipeline(cfg, newTestEmbeddingProvider(64), index, metadata, newTestLogger())

	err := pipeline.RemoveFile(context.Background(), "/some/file.go")
	if err == nil {
		t.Fatal("expected RemoveFile to propagate the metadata get error")
	}
	var mdErr *MetadataStoreError
	if !errors.As(err, &mdErr) {
		t.Fatalf("expected MetadataStoreError, got %T", err)
	}
}

// --- metadata GetFileRecord JSON unmarshal error (corrupt stored value) ---

func TestMetadataStore_Get_CorruptJSON(t *testing.T) {
	kv := newMemoryKVStore()
	ns := "test-ns"
	path := "/corrupt/file.go"
	key := buildKey(ns, path)
	if err := kv.Put(context.Background(), key, []byte("{not valid json")); err != nil {
		t.Fatalf("seed corrupt value: %v", err)
	}
	store := NewMetadataStore(kv)

	_, err := store.GetFileRecord(context.Background(), ns, path)
	if err == nil {
		t.Fatal("expected unmarshal error for corrupt JSON")
	}
	var mdErr *MetadataStoreError
	if !errors.As(err, &mdErr) {
		t.Fatalf("expected MetadataStoreError, got %T", err)
	}
	if mdErr.Operation != "get" {
		t.Fatalf("expected operation get, got %s", mdErr.Operation)
	}
}

// --- metadata ListFileRecords JSON unmarshal error (corrupt stored value) ---

func TestMetadataStore_List_CorruptJSON(t *testing.T) {
	kv := newMemoryKVStore()
	ns := "test-ns"
	key := buildKey(ns, "/corrupt/file.go")
	if err := kv.Put(context.Background(), key, []byte("not-json")); err != nil {
		t.Fatalf("seed corrupt value: %v", err)
	}
	store := NewMetadataStore(kv)

	_, err := store.ListFileRecords(context.Background(), ns)
	if err == nil {
		t.Fatal("expected unmarshal error during list of corrupt JSON")
	}
	var mdErr *MetadataStoreError
	if !errors.As(err, &mdErr) {
		t.Fatalf("expected MetadataStoreError, got %T", err)
	}
	if mdErr.Operation != "list" {
		t.Fatalf("expected operation list, got %s", mdErr.Operation)
	}
}
