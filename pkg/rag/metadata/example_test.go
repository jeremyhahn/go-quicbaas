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
	"fmt"
	"os"
	"path/filepath"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/chunking"
)

// ExampleExtractor demonstrates basic metadata extraction
func ExampleExtractor() {
	// Create extractor with default config
	extractor := NewExtractor(DefaultConfig())

	// Create a chunk (normally from a chunker)
	chunk := chunking.Chunk{
		Content: `package main

import "fmt"

func Hello(name string) {
	fmt.Printf("Hello, %s!\n", name)
}`,
		Metadata: chunking.Metadata{
			FunctionName: "Hello",
			StartLine:    5,
			EndLine:      7,
		},
		Tokens: 50,
		Index:  0,
	}

	// Extract file info
	tmpFile := "/tmp/example.go"
	os.WriteFile(tmpFile, []byte(chunk.Content), 0644)
	defer os.Remove(tmpFile)

	fileInfo, _ := extractor.ExtractFileInfo(tmpFile)

	// Extract metadata
	metadata := extractor.Extract(chunk, fileInfo)

	fmt.Printf("File: %s\n", metadata.FileName)
	fmt.Printf("Language: %s\n", metadata.Language)
	fmt.Printf("Function: %s\n", metadata.FunctionName)
	fmt.Printf("Scope: %s\n", metadata.Scope)
	fmt.Printf("Lines: %d-%d\n", metadata.StartLine, metadata.EndLine)

	// Output:
	// File: example.go
	// Language: Go
	// Function: Hello
	// Scope: function
	// Lines: 5-7
}

// ExampleExtractor_withKeywords demonstrates keyword extraction
func ExampleExtractor_withKeywords() {
	extractor := NewExtractor(ExtractorConfig{
		EnableGit:       false,
		ExtractKeywords: true,
		MaxKeywords:     5,
	})

	chunk := chunking.Chunk{
		Content: `The database connection pool efficiently manages database connections,
ensuring optimal performance and resource utilization. Connection pooling
reduces overhead by reusing established connections.`,
		Tokens: 50,
	}

	fileInfo := FileInfo{
		Path:     "/path/to/pool.go",
		Language: "Go",
	}

	metadata := extractor.Extract(chunk, fileInfo)

	fmt.Printf("Keywords: %v\n", metadata.Keywords)
	// Output will vary based on word frequency
}

// ExampleExtractor_gitInfo demonstrates git integration
func ExampleExtractor_gitInfo() {
	extractor := NewExtractor(ExtractorConfig{
		EnableGit:    true,
		CacheGitInfo: true,
	})

	// This will only work in a git repository
	// In production, check for errors
	cwd, _ := os.Getwd()
	gitInfo, err := extractor.ExtractGitInfo(cwd)

	if err == nil {
		fmt.Printf("Branch: %s\n", gitInfo.Branch)
		fmt.Printf("Commit: %s (short)\n", gitInfo.Commit)
		// Output will vary based on actual git repo
	} else {
		fmt.Println("Not in a git repository or git not available")
	}
}

// ExampleFileInfo demonstrates file info extraction
func ExampleExtractor_extractFileInfo() {
	extractor := NewExtractor(DefaultConfig())

	// Create a temporary file
	tmpDir := os.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	content := []byte("package main\n\nfunc main() {}\n")
	os.WriteFile(testFile, content, 0644)
	defer os.Remove(testFile)

	fileInfo, err := extractor.ExtractFileInfo(testFile)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Path: %s\n", fileInfo.Path)
	fmt.Printf("Size: %d bytes\n", fileInfo.Size)
	fmt.Printf("Language: %s\n", fileInfo.Language)
	fmt.Printf("Is Directory: %v\n", fileInfo.IsDir)

	// Output will include actual path to temp file
}

// ExampleChunkMetadata demonstrates enriching chunks with comprehensive metadata
func Example_comprehensiveMetadata() {
	extractor := NewExtractor(ExtractorConfig{
		EnableGit:       false, // Disable git for this example
		ExtractKeywords: true,
		MaxKeywords:     3,
	})

	chunk := chunking.Chunk{
		Content: `type DatabasePool struct {
	connections []*Connection
	maxSize     int
}

func (p *DatabasePool) Get() (*Connection, error) {
	// Get connection from pool
	return p.connections[0], nil
}`,
		Metadata: chunking.Metadata{
			ClassName:    "DatabasePool",
			FunctionName: "Get",
			StartLine:    6,
			EndLine:      9,
			Tags:         []string{"critical", "database"},
			Context: map[string]string{
				"module": "storage",
			},
		},
		Tokens: 150,
	}

	fileInfo := FileInfo{
		Path:     "/project/pkg/storage/pool.go",
		Size:     2048,
		Language: "Go",
	}

	metadata := extractor.Extract(chunk, fileInfo)

	fmt.Printf("File: %s\n", metadata.FileName)
	fmt.Printf("Language: %s\n", metadata.Language)
	fmt.Printf("Class: %s\n", metadata.ClassName)
	fmt.Printf("Method: %s\n", metadata.FunctionName)
	fmt.Printf("Scope: %s\n", metadata.Scope)
	fmt.Printf("Tags: %v\n", metadata.Tags)
	fmt.Printf("Module: %s\n", metadata.Context["module"])
	fmt.Printf("Too Large: %v\n", metadata.TooLarge)

	// Output:
	// File: pool.go
	// Language: Go
	// Class: DatabasePool
	// Method: Get
	// Scope: method
	// Tags: [critical database]
	// Module: storage
	// Too Large: false
}
