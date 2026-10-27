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

package watcher

import (
	"time"
)

// Config contains configuration for the file watcher.
type Config struct {
	// Paths to watch for changes.
	Paths []string

	// Exclude contains .gitignore-style patterns to exclude.
	// Examples: "*.tmp", "node_modules/", ".git/"
	Exclude []string

	// DebounceDelay is how long a file must go unchanged before its event is
	// emitted. Rapid changes to the same file are deduplicated, and each
	// change restarts the wait, so a file still being written is held back.
	// Held events are emitted by the first batch after the delay has passed,
	// and all pending events are emitted when the watcher stops.
	// Zero emits every pending event at the next batch.
	// Default: 100ms
	DebounceDelay time.Duration

	// BatchInterval is the interval at which batched events are emitted.
	// Setting this to 0 disables batching.
	// Default: 1s
	BatchInterval time.Duration

	// IgnoreHidden determines whether to ignore hidden files (starting with .).
	// Default: true
	IgnoreHidden bool

	// FollowSymlinks determines whether to follow symbolic links.
	// Default: false (for safety)
	FollowSymlinks bool

	// MaxDepth limits recursive directory watching depth.
	// 0 means unlimited depth.
	// Default: 0
	MaxDepth int

	// BufferSize is the size of the event channel buffer.
	// Default: 1000
	BufferSize int

	// Recursive determines whether to watch subdirectories.
	// Default: true
	Recursive bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Paths:          []string{},
		Exclude:        DefaultExclusions(),
		DebounceDelay:  100 * time.Millisecond,
		BatchInterval:  1 * time.Second,
		IgnoreHidden:   true,
		FollowSymlinks: false,
		MaxDepth:       0,
		BufferSize:     1000,
		Recursive:      true,
	}
}

// DefaultExclusions returns commonly excluded patterns.
func DefaultExclusions() []string {
	return []string{
		".git/",
		".git/*",
		"node_modules/",
		"node_modules/*",
		"*.pyc",
		"__pycache__/",
		".DS_Store",
		"*.swp",
		"*.swo",
		"*~",
		".tmp",
		"*.tmp",
		".idea/",
		".vscode/",
		"*.log",
	}
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if len(c.Paths) == 0 {
		return ErrNoPaths
	}

	if c.DebounceDelay < 0 {
		return ErrInvalidDebounceDelay
	}

	if c.BatchInterval < 0 {
		return ErrInvalidBatchInterval
	}

	if c.BufferSize <= 0 {
		c.BufferSize = 1000
	}

	if c.MaxDepth < 0 {
		return ErrInvalidMaxDepth
	}

	return nil
}
