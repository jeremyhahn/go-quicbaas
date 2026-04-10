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
	"errors"
	"testing"
)

func TestNewVectorIndexFromConfig_AutoBackend(t *testing.T) {
	cfg := IndexingConfig{IndexType: "auto"}
	idx, err := NewVectorIndexFromConfig(cfg, 64)
	if err != nil {
		t.Fatalf("auto backend should succeed, got error: %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil vector index")
	}
	if idx.Size() != 0 {
		t.Fatalf("expected empty index, got size %d", idx.Size())
	}
}

func TestNewVectorIndexFromConfig_MemoryBackend(t *testing.T) {
	cfg := IndexingConfig{IndexType: "memory"}
	idx, err := NewVectorIndexFromConfig(cfg, 128)
	if err != nil {
		t.Fatalf("memory backend should succeed, got error: %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil vector index")
	}

	// Verify it works by adding a vector.
	vec := make([]float32, 128)
	for i := range vec {
		vec[i] = float32(i) / 128.0
	}
	if err := idx.Add("test-1", vec, nil); err != nil {
		t.Fatalf("Add to memory index failed: %v", err)
	}
	if idx.Size() != 1 {
		t.Fatalf("expected size 1, got %d", idx.Size())
	}
}

func TestNewVectorIndexFromConfig_HNSWBackend(t *testing.T) {
	cfg := IndexingConfig{IndexType: "hnsw"}
	idx, err := NewVectorIndexFromConfig(cfg, 64)
	if err != nil {
		t.Fatalf("hnsw backend should succeed (falls back to memory), got error: %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil vector index")
	}
}

func TestNewVectorIndexFromConfig_UnsupportedBackend(t *testing.T) {
	cfg := IndexingConfig{IndexType: "nonexistent"}
	_, err := NewVectorIndexFromConfig(cfg, 64)
	if err == nil {
		t.Fatal("expected error for unsupported backend")
	}
	var unsupported *UnsupportedIndexTypeError
	if !errors.As(err, &unsupported) {
		t.Fatalf("expected *UnsupportedIndexTypeError, got %T", err)
	}
	if unsupported.IndexType != "nonexistent" {
		t.Fatalf("expected index type nonexistent, got %s", unsupported.IndexType)
	}
}

func TestNewVectorIndexFromConfig_GPURequested(t *testing.T) {
	cfg := IndexingConfig{IndexType: "auto", GPUAccelerated: true}
	_, err := NewVectorIndexFromConfig(cfg, 64)
	if err == nil {
		t.Fatal("expected error for GPU-accelerated request")
	}
	var gpuErr *GPUUnavailableError
	if !errors.As(err, &gpuErr) {
		t.Fatalf("expected *GPUUnavailableError, got %T", err)
	}
}

func TestNewVectorIndexFromConfig_ZeroDimensions(t *testing.T) {
	cfg := IndexingConfig{IndexType: "memory"}
	_, err := NewVectorIndexFromConfig(cfg, 0)
	if err == nil {
		t.Fatal("expected error for zero dimensions")
	}
}

func TestNewVectorIndexFromConfig_NegativeDimensions(t *testing.T) {
	cfg := IndexingConfig{IndexType: "memory"}
	_, err := NewVectorIndexFromConfig(cfg, -1)
	if err == nil {
		t.Fatal("expected error for negative dimensions")
	}
}

func TestNewVectorIndexFromConfig_AllBackendsProduceWorkingIndex(t *testing.T) {
	backends := []string{"auto", "memory", "hnsw"}
	dimensions := 32

	for _, backend := range backends {
		cfg := IndexingConfig{IndexType: backend}
		idx, err := NewVectorIndexFromConfig(cfg, dimensions)
		if err != nil {
			t.Fatalf("backend %s should succeed, got error: %v", backend, err)
		}

		// Verify basic operations work.
		vec := make([]float32, dimensions)
		for i := range vec {
			vec[i] = 0.5
		}
		if err := idx.Add("v1", vec, nil); err != nil {
			t.Fatalf("backend %s: Add failed: %v", backend, err)
		}
		if idx.Size() != 1 {
			t.Fatalf("backend %s: expected size 1, got %d", backend, idx.Size())
		}
		if err := idx.Remove("v1"); err != nil {
			t.Fatalf("backend %s: Remove failed: %v", backend, err)
		}
		if idx.Size() != 0 {
			t.Fatalf("backend %s: expected size 0 after remove, got %d", backend, idx.Size())
		}
	}
}
