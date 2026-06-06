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

import "runtime"

// IndexTrigger represents an event that triggers indexing.
type IndexTrigger string

const (
	// TriggerFileChange triggers indexing on file system changes.
	TriggerFileChange IndexTrigger = "file_change"

	// TriggerIterationComplete triggers indexing at the end of an iteration cycle.
	TriggerIterationComplete IndexTrigger = "iteration_complete"

	// TriggerManual triggers indexing only on explicit request.
	TriggerManual IndexTrigger = "manual"
)

// validTriggers provides O(1) lookup for trigger validation.
var validTriggers = map[IndexTrigger]struct{}{
	TriggerFileChange:        {},
	TriggerIterationComplete: {},
	TriggerManual:            {},
}

// validIndexTypes provides O(1) lookup for index type validation.
var validIndexTypes = map[string]struct{}{
	"auto":   {},
	"memory": {},
	"hnsw":   {},
	"qrdb":   {},
}

// IndexingConfig holds configuration for the auto-indexing service.
type IndexingConfig struct {
	// Enabled controls whether indexing is active.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// AutoIndex enables automatic indexing on file changes.
	AutoIndex bool `yaml:"auto_index" json:"auto_index"`

	// IndexType selects the vector index backend: "auto", "memory", or "hnsw".
	IndexType string `yaml:"index_type" json:"index_type"`

	// GPUAccelerated requests GPU-accelerated indexing (Phase 10).
	GPUAccelerated bool `yaml:"gpu_accelerated" json:"gpu_accelerated"`

	// Triggers lists the events that trigger indexing.
	Triggers []string `yaml:"triggers" json:"triggers"`

	// BatchSize is the number of files to process per embedding batch.
	BatchSize int `yaml:"batch_size" json:"batch_size"`

	// MaxConcurrency limits the number of concurrent indexing operations.
	MaxConcurrency int `yaml:"max_concurrency" json:"max_concurrency"`

	// WatchExclusions lists glob patterns to exclude from file watching.
	WatchExclusions []string `yaml:"watch_exclusions" json:"watch_exclusions"`

	// ChunkMaxTokens is the maximum number of tokens per chunk.
	ChunkMaxTokens int `yaml:"chunk_max_tokens" json:"chunk_max_tokens"`

	// ChunkOverlap is the number of overlapping tokens between adjacent chunks.
	// A value of 0 means no overlap, which is valid.
	ChunkOverlap int `yaml:"chunk_overlap" json:"chunk_overlap"`

	// ProjectID identifies the project for namespacing.
	ProjectID string `yaml:"project_id" json:"project_id"`

	// ProjectPath is the root directory of the project to index.
	ProjectPath string `yaml:"project_path" json:"project_path"`

	// Namespace scopes the metadata and vector storage.
	Namespace string `yaml:"namespace" json:"namespace"`
}

// DefaultIndexingConfig returns an IndexingConfig with sensible defaults.
func DefaultIndexingConfig() IndexingConfig {
	return IndexingConfig{
		Enabled:        true,
		AutoIndex:      true,
		IndexType:      "auto",
		GPUAccelerated: false,
		Triggers:       []string{string(TriggerFileChange)},
		BatchSize:      32,
		MaxConcurrency: runtime.NumCPU(),
		WatchExclusions: []string{
			".git/", "node_modules/", "vendor/", "*.tmp",
			"*.log", ".DS_Store", "*.swp",
		},
		ChunkMaxTokens: 2000,
		ChunkOverlap:   200,
	}
}

// ApplyDefaults fills in zero-value fields with sensible defaults.
// For ChunkOverlap, only negative values are corrected since 0 is a
// valid setting meaning no overlap between chunks.
func (c *IndexingConfig) ApplyDefaults() {
	if c.IndexType == "" {
		c.IndexType = "auto"
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 32
	}
	if c.MaxConcurrency <= 0 {
		c.MaxConcurrency = runtime.NumCPU()
	}
	if c.ChunkMaxTokens <= 0 {
		c.ChunkMaxTokens = 2000
	}
	if c.ChunkOverlap < 0 {
		c.ChunkOverlap = 200
	}
	if len(c.Triggers) == 0 {
		c.Triggers = []string{string(TriggerFileChange)}
	}
	if len(c.WatchExclusions) == 0 {
		c.WatchExclusions = []string{
			".git/", "node_modules/", "vendor/", "*.tmp",
			"*.log", ".DS_Store", "*.swp",
		}
	}
	if c.Namespace == "" && c.ProjectID != "" {
		c.Namespace = c.ProjectID
	}
}

// Validate checks the configuration for errors and returns a typed error if invalid.
func (c *IndexingConfig) Validate() error {
	if !c.Enabled {
		return nil // Disabled config is always valid.
	}

	if _, ok := validIndexTypes[c.IndexType]; !ok {
		return &ConfigValidationError{
			Field:  "IndexType",
			Reason: "must be one of: auto, memory, hnsw",
		}
	}

	if c.BatchSize <= 0 {
		return &ConfigValidationError{
			Field:  "BatchSize",
			Reason: "must be positive",
		}
	}

	if c.MaxConcurrency <= 0 {
		return &ConfigValidationError{
			Field:  "MaxConcurrency",
			Reason: "must be positive",
		}
	}

	if c.ChunkMaxTokens <= 0 {
		return &ConfigValidationError{
			Field:  "ChunkMaxTokens",
			Reason: "must be positive",
		}
	}

	if c.ChunkOverlap < 0 {
		return &ConfigValidationError{
			Field:  "ChunkOverlap",
			Reason: "must be non-negative",
		}
	}

	if c.ChunkOverlap >= c.ChunkMaxTokens {
		return &ConfigValidationError{
			Field:  "ChunkOverlap",
			Reason: "must be less than ChunkMaxTokens",
		}
	}

	for _, trigger := range c.Triggers {
		if _, ok := validTriggers[IndexTrigger(trigger)]; !ok {
			return &ConfigValidationError{
				Field:  "Triggers",
				Reason: "invalid trigger: " + trigger,
			}
		}
	}

	if c.ProjectPath == "" {
		return &ConfigValidationError{
			Field:  "ProjectPath",
			Reason: "must not be empty when indexing is enabled",
		}
	}

	return nil
}
