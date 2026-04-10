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
	"runtime"
	"testing"
)

func TestIndexingConfig_Validate_ValidConfig(t *testing.T) {
	cfg := DefaultIndexingConfig()
	cfg.ProjectPath = "/tmp/testproject"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestIndexingConfig_Validate_DisabledAlwaysValid(t *testing.T) {
	cfg := IndexingConfig{Enabled: false}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled config should always be valid, got error: %v", err)
	}
}

func TestIndexingConfig_Validate_InvalidIndexType(t *testing.T) {
	cfg := DefaultIndexingConfig()
	cfg.ProjectPath = "/tmp/testproject"
	cfg.IndexType = "invalid_type"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid index type")
	}
	configErr, ok := err.(*ConfigValidationError)
	if !ok {
		t.Fatalf("expected *ConfigValidationError, got %T", err)
	}
	if configErr.Field != "IndexType" {
		t.Fatalf("expected field IndexType, got %s", configErr.Field)
	}
}

func TestIndexingConfig_Validate_InvalidBatchSize(t *testing.T) {
	cfg := DefaultIndexingConfig()
	cfg.ProjectPath = "/tmp/testproject"
	cfg.BatchSize = -1

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative batch size")
	}
	configErr, ok := err.(*ConfigValidationError)
	if !ok {
		t.Fatalf("expected *ConfigValidationError, got %T", err)
	}
	if configErr.Field != "BatchSize" {
		t.Fatalf("expected field BatchSize, got %s", configErr.Field)
	}
}

func TestIndexingConfig_Validate_InvalidMaxConcurrency(t *testing.T) {
	cfg := DefaultIndexingConfig()
	cfg.ProjectPath = "/tmp/testproject"
	cfg.MaxConcurrency = 0

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for zero max concurrency")
	}
	configErr, ok := err.(*ConfigValidationError)
	if !ok {
		t.Fatalf("expected *ConfigValidationError, got %T", err)
	}
	if configErr.Field != "MaxConcurrency" {
		t.Fatalf("expected field MaxConcurrency, got %s", configErr.Field)
	}
}

func TestIndexingConfig_Validate_InvalidChunkMaxTokens(t *testing.T) {
	cfg := DefaultIndexingConfig()
	cfg.ProjectPath = "/tmp/testproject"
	cfg.ChunkMaxTokens = 0

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for zero chunk max tokens")
	}
	configErr, ok := err.(*ConfigValidationError)
	if !ok {
		t.Fatalf("expected *ConfigValidationError, got %T", err)
	}
	if configErr.Field != "ChunkMaxTokens" {
		t.Fatalf("expected field ChunkMaxTokens, got %s", configErr.Field)
	}
}

func TestIndexingConfig_Validate_NegativeChunkOverlap(t *testing.T) {
	cfg := DefaultIndexingConfig()
	cfg.ProjectPath = "/tmp/testproject"
	cfg.ChunkOverlap = -1

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative chunk overlap")
	}
	configErr, ok := err.(*ConfigValidationError)
	if !ok {
		t.Fatalf("expected *ConfigValidationError, got %T", err)
	}
	if configErr.Field != "ChunkOverlap" {
		t.Fatalf("expected field ChunkOverlap, got %s", configErr.Field)
	}
}

func TestIndexingConfig_Validate_ChunkOverlapExceedsMaxTokens(t *testing.T) {
	cfg := DefaultIndexingConfig()
	cfg.ProjectPath = "/tmp/testproject"
	cfg.ChunkMaxTokens = 100
	cfg.ChunkOverlap = 100

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error when chunk overlap equals chunk max tokens")
	}
	configErr, ok := err.(*ConfigValidationError)
	if !ok {
		t.Fatalf("expected *ConfigValidationError, got %T", err)
	}
	if configErr.Field != "ChunkOverlap" {
		t.Fatalf("expected field ChunkOverlap, got %s", configErr.Field)
	}
}

func TestIndexingConfig_Validate_InvalidTrigger(t *testing.T) {
	cfg := DefaultIndexingConfig()
	cfg.ProjectPath = "/tmp/testproject"
	cfg.Triggers = []string{"nonexistent_trigger"}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid trigger")
	}
	configErr, ok := err.(*ConfigValidationError)
	if !ok {
		t.Fatalf("expected *ConfigValidationError, got %T", err)
	}
	if configErr.Field != "Triggers" {
		t.Fatalf("expected field Triggers, got %s", configErr.Field)
	}
}

func TestIndexingConfig_Validate_EmptyProjectPath(t *testing.T) {
	cfg := DefaultIndexingConfig()
	cfg.ProjectPath = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty project path")
	}
	configErr, ok := err.(*ConfigValidationError)
	if !ok {
		t.Fatalf("expected *ConfigValidationError, got %T", err)
	}
	if configErr.Field != "ProjectPath" {
		t.Fatalf("expected field ProjectPath, got %s", configErr.Field)
	}
}

func TestIndexingConfig_ApplyDefaults_FillsZeroValues(t *testing.T) {
	cfg := IndexingConfig{Enabled: true}
	cfg.ApplyDefaults()

	if cfg.IndexType != "auto" {
		t.Fatalf("expected IndexType auto, got %s", cfg.IndexType)
	}
	if cfg.BatchSize != 32 {
		t.Fatalf("expected BatchSize 32, got %d", cfg.BatchSize)
	}
	if cfg.MaxConcurrency != runtime.NumCPU() {
		t.Fatalf("expected MaxConcurrency %d, got %d", runtime.NumCPU(), cfg.MaxConcurrency)
	}
	if cfg.ChunkMaxTokens != 2000 {
		t.Fatalf("expected ChunkMaxTokens 2000, got %d", cfg.ChunkMaxTokens)
	}
	// ChunkOverlap of 0 is a valid value (no overlap), so ApplyDefaults
	// correctly does not override it. Only negative values are defaulted.
	if cfg.ChunkOverlap != 0 {
		t.Fatalf("expected ChunkOverlap 0 (zero is valid), got %d", cfg.ChunkOverlap)
	}
	if len(cfg.Triggers) != 1 || cfg.Triggers[0] != string(TriggerFileChange) {
		t.Fatalf("expected default trigger file_change, got %v", cfg.Triggers)
	}
	if len(cfg.WatchExclusions) == 0 {
		t.Fatal("expected default watch exclusions to be set")
	}
}

func TestIndexingConfig_ApplyDefaults_PreservesExistingValues(t *testing.T) {
	cfg := IndexingConfig{
		Enabled:         true,
		IndexType:       "memory",
		BatchSize:       64,
		MaxConcurrency:  4,
		ChunkMaxTokens:  1000,
		ChunkOverlap:    100,
		Triggers:        []string{string(TriggerManual)},
		WatchExclusions: []string{".custom/"},
	}
	cfg.ApplyDefaults()

	if cfg.IndexType != "memory" {
		t.Fatalf("expected IndexType memory, got %s", cfg.IndexType)
	}
	if cfg.BatchSize != 64 {
		t.Fatalf("expected BatchSize 64, got %d", cfg.BatchSize)
	}
	if cfg.MaxConcurrency != 4 {
		t.Fatalf("expected MaxConcurrency 4, got %d", cfg.MaxConcurrency)
	}
	if cfg.ChunkMaxTokens != 1000 {
		t.Fatalf("expected ChunkMaxTokens 1000, got %d", cfg.ChunkMaxTokens)
	}
	if cfg.ChunkOverlap != 100 {
		t.Fatalf("expected ChunkOverlap 100, got %d", cfg.ChunkOverlap)
	}
	if len(cfg.Triggers) != 1 || cfg.Triggers[0] != string(TriggerManual) {
		t.Fatalf("expected trigger manual, got %v", cfg.Triggers)
	}
	if len(cfg.WatchExclusions) != 1 || cfg.WatchExclusions[0] != ".custom/" {
		t.Fatalf("expected custom watch exclusion, got %v", cfg.WatchExclusions)
	}
}

func TestIndexingConfig_ApplyDefaults_NamespaceFromProjectID(t *testing.T) {
	cfg := IndexingConfig{
		Enabled:   true,
		ProjectID: "test-project",
	}
	cfg.ApplyDefaults()

	if cfg.Namespace != "test-project" {
		t.Fatalf("expected namespace to be set from project ID, got %s", cfg.Namespace)
	}
}

func TestDefaultIndexingConfig_IsValid(t *testing.T) {
	cfg := DefaultIndexingConfig()
	cfg.ProjectPath = "/tmp/test"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should be valid, got error: %v", err)
	}
}

func TestIndexingConfig_Validate_AllValidTriggers(t *testing.T) {
	triggers := []string{
		string(TriggerFileChange),
		string(TriggerIterationComplete),
		string(TriggerManual),
	}

	cfg := DefaultIndexingConfig()
	cfg.ProjectPath = "/tmp/testproject"
	cfg.Triggers = triggers

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected all valid triggers to pass, got error: %v", err)
	}
}

func TestIndexingConfig_Validate_AllValidIndexTypes(t *testing.T) {
	for _, indexType := range []string{"auto", "memory", "hnsw"} {
		cfg := DefaultIndexingConfig()
		cfg.ProjectPath = "/tmp/testproject"
		cfg.IndexType = indexType

		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected index type %q to be valid, got error: %v", indexType, err)
		}
	}
}
