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

package metadata

import (
	"time"
)

// ChunkMetadata holds all metadata for a chunk.
type ChunkMetadata struct {
	// Source information
	FilePath string
	FileName string
	FileSize int64
	Language string

	// Position information
	StartLine int
	EndLine   int
	StartByte int
	EndByte   int

	// Code context (for code files)
	PackageName  string
	Imports      []string
	FunctionName string
	ClassName    string
	MethodName   string
	ReceiverType string

	// Structural context
	NestingLevel int
	Scope        string // "global", "function", "class", "method"

	// Temporal information
	ModTime    time.Time
	CreateTime time.Time

	// Git information (optional)
	GitRepo   string
	GitBranch string
	GitCommit string
	GitAuthor string

	// Semantic information
	Keywords []string
	Tags     []string
	Category string

	// Quality indicators
	TooLarge     bool // Chunk exceeds recommended size
	Incomplete   bool // Chunk was truncated
	HasSyntaxErr bool // Code has syntax errors

	// Custom context
	Context map[string]string
}

// FileInfo holds file-level metadata.
type FileInfo struct {
	Path     string
	Size     int64
	ModTime  time.Time
	IsDir    bool
	Language string
}

// GitInfo holds git repository metadata.
type GitInfo struct {
	Repo   string
	Branch string
	Commit string
	Author string
}

// ExtractorConfig configures metadata extraction behavior.
type ExtractorConfig struct {
	EnableGit       bool
	ExtractKeywords bool
	MaxKeywords     int
	CacheGitInfo    bool
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() ExtractorConfig {
	return ExtractorConfig{
		EnableGit:       true,
		ExtractKeywords: true,
		MaxKeywords:     10,
		CacheGitInfo:    true,
	}
}
