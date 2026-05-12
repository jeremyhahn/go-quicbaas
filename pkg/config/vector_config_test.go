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

package config

import (
	"path/filepath"
	"testing"
)

// TestVectorConfig_ApplyDefaults_FillsMissingHNSW locks in the canonical HNSW
// defaults applied on the QUICBAAS_VECTOR_ENABLED=true fast path. Removing or
// weakening the defaults will fail this test, restoring the original UX bug
// where the daemon crashed at boot with "hnsw configuration is required".
func TestVectorConfig_ApplyDefaults_FillsMissingHNSW(t *testing.T) {
	cfg := &VectorConfig{Enabled: true}

	cfg.ApplyDefaults("/var/lib/quicbaas/data")

	if cfg.HNSW == nil {
		t.Fatal("ApplyDefaults: HNSW must be non-nil after enabling vector")
	}
	if got, want := cfg.HNSW.M, 16; got != want {
		t.Errorf("HNSW.M = %d, want %d", got, want)
	}
	if got, want := cfg.HNSW.EfConstruction, 200; got != want {
		t.Errorf("HNSW.EfConstruction = %d, want %d", got, want)
	}
	if got, want := cfg.HNSW.MaxElements, 10000; got != want {
		t.Errorf("HNSW.MaxElements = %d, want %d", got, want)
	}
	if got, want := cfg.HNSW.Ml, 1.0; got != want {
		t.Errorf("HNSW.Ml = %v, want %v", got, want)
	}
	if got, want := cfg.ShardID, uint64(200); got != want {
		t.Errorf("ShardID = %d, want %d", got, want)
	}
	if got, want := cfg.DefaultIndexType, "hnsw"; got != want {
		t.Errorf("DefaultIndexType = %q, want %q", got, want)
	}
	want := filepath.Join("/var/lib/quicbaas/data", "vector")
	if cfg.StoragePath != want {
		t.Errorf("StoragePath = %q, want %q", cfg.StoragePath, want)
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate after ApplyDefaults must pass; got %v", err)
	}
}

// TestVectorConfig_ApplyDefaults_PreservesOperatorValues guarantees that
// operator-supplied values are never overwritten — defaults only fill blanks.
func TestVectorConfig_ApplyDefaults_PreservesOperatorValues(t *testing.T) {
	cfg := &VectorConfig{
		Enabled:          true,
		ShardID:          7,
		StoragePath:      "/custom/path",
		DefaultIndexType: "flat",
		HNSW: &HNSWConfig{
			M:              32,
			EfConstruction: 400,
			MaxElements:    50000,
			Ml:             0.5,
		},
	}

	cfg.ApplyDefaults("/should/not/be/used")

	if cfg.ShardID != 7 {
		t.Errorf("ShardID overwritten: got %d, want 7", cfg.ShardID)
	}
	if cfg.StoragePath != "/custom/path" {
		t.Errorf("StoragePath overwritten: got %q, want %q", cfg.StoragePath, "/custom/path")
	}
	if cfg.DefaultIndexType != "flat" {
		t.Errorf("DefaultIndexType overwritten: got %q, want %q", cfg.DefaultIndexType, "flat")
	}
	if cfg.HNSW.M != 32 || cfg.HNSW.EfConstruction != 400 || cfg.HNSW.MaxElements != 50000 || cfg.HNSW.Ml != 0.5 {
		t.Errorf("HNSW overwritten: %+v", cfg.HNSW)
	}
}

// TestVectorConfig_ApplyDefaults_DisabledIsNoOp confirms ApplyDefaults touches
// nothing when the subsystem is disabled.
func TestVectorConfig_ApplyDefaults_DisabledIsNoOp(t *testing.T) {
	cfg := &VectorConfig{Enabled: false}

	cfg.ApplyDefaults("/var/lib/quicbaas/data")

	if cfg.HNSW != nil {
		t.Errorf("HNSW must remain nil for disabled config; got %+v", cfg.HNSW)
	}
	if cfg.ShardID != 0 {
		t.Errorf("ShardID must remain 0 for disabled config; got %d", cfg.ShardID)
	}
	if cfg.StoragePath != "" {
		t.Errorf("StoragePath must remain empty for disabled config; got %q", cfg.StoragePath)
	}
}

// TestVectorConfig_ApplyDefaults_IsIdempotent confirms repeated calls produce
// the same result — important because initVectorStorage and the env-var path
// may both invoke it depending on the boot path.
func TestVectorConfig_ApplyDefaults_IsIdempotent(t *testing.T) {
	cfg := &VectorConfig{Enabled: true}

	cfg.ApplyDefaults("/data")
	first := *cfg.HNSW
	firstShard := cfg.ShardID
	firstPath := cfg.StoragePath

	cfg.ApplyDefaults("/somewhere/else")

	if *cfg.HNSW != first {
		t.Errorf("HNSW changed across calls: got %+v, want %+v", *cfg.HNSW, first)
	}
	if cfg.ShardID != firstShard {
		t.Errorf("ShardID changed across calls: got %d, want %d", cfg.ShardID, firstShard)
	}
	if cfg.StoragePath != firstPath {
		t.Errorf("StoragePath changed across calls: got %q, want %q", cfg.StoragePath, firstPath)
	}
}

// TestVectorConfig_ApplyDefaults_NoDataDirFallback confirms that when no
// storage data directory is available (e.g. very early boot), ApplyDefaults
// still produces a valid relative StoragePath rather than leaving it empty.
func TestVectorConfig_ApplyDefaults_NoDataDirFallback(t *testing.T) {
	cfg := &VectorConfig{Enabled: true}

	cfg.ApplyDefaults("")

	if cfg.StoragePath == "" {
		t.Fatal("StoragePath must be populated even with empty storageDataDir")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate must pass with fallback StoragePath; got %v", err)
	}
}

// TestVectorConfig_Validate_RejectsMissingHNSW guards the original error
// surface so callers that bypass ApplyDefaults still get a clear typed error.
func TestVectorConfig_Validate_RejectsMissingHNSW(t *testing.T) {
	cfg := &VectorConfig{
		Enabled:     true,
		ShardID:     1,
		StoragePath: "/tmp/x",
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate must reject Enabled=true with HNSW=nil")
	}
}
