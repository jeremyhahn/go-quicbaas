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

package processor_test

import (
	"context"
	"fmt"
	"log"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/language"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/processor"
)

// ExampleNew demonstrates creating a new processor
func ExampleNew() {
	config := processor.DefaultConfig()
	proc, err := processor.New(config)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Processor created with strategy: %s\n", config.DefaultStrategy)
	// Output: Processor created with strategy: auto
	_ = proc
}

// ExampleProcessor_ProcessContent demonstrates processing Go code content
func ExampleProcessor_ProcessContent() {
	// Create processor
	config := processor.DefaultConfig()
	proc, err := processor.New(config)
	if err != nil {
		log.Fatal(err)
	}

	// Sample Go code
	content := []byte(`package main

import "fmt"

// Greet prints a greeting message
func Greet(name string) {
	fmt.Printf("Hello, %s!\n", name)
}
`)

	// Process content
	ctx := context.Background()
	chunks, err := proc.ProcessContent(ctx, content, "example.go")
	if err != nil {
		log.Fatal(err)
	}

	// Display results
	for i, chunk := range chunks {
		fmt.Printf("Chunk %d:\n", i+1)
		fmt.Printf("  Language: %s\n", chunk.Metadata.Language)
		fmt.Printf("  Function: %s\n", chunk.Metadata.FunctionName)
		fmt.Printf("  Lines: %d-%d\n", chunk.Metadata.StartLine, chunk.Metadata.EndLine)
		fmt.Printf("  Has content: %v\n", len(chunk.Content) > 0)
	}
	// Output:
	// Chunk 1:
	//   Language: go
	//   Function: Greet
	//   Lines: 5-8
	//   Has content: true
}

// ExampleConfig_strategyOverride demonstrates strategy override
func ExampleConfig_strategyOverride() {
	config := processor.DefaultConfig()

	// Override Go files to use text chunker instead of code chunker
	config.StrategyOverrides = map[language.Language]string{
		language.LanguageGo: "text",
	}

	proc, err := processor.New(config)
	if err != nil {
		log.Fatal(err)
	}

	content := []byte(`package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
	fmt.Println("This is a test of the text chunker strategy override functionality.")
	fmt.Println("We need enough content to meet the minimum chunk size requirements.")
	fmt.Println("This ensures the text chunker can process the Go code as plain text.")
	fmt.Println("The strategy override feature allows flexible processing of different file types.")
}
`)

	ctx := context.Background()
	chunks, err := proc.ProcessContent(ctx, content, "test.go")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Processed with overridden strategy: %d chunks\n", len(chunks))
	// Output: Processed with overridden strategy: 1 chunks
}

// ExampleConfig_deduplication demonstrates hash-based deduplication
func ExampleConfig_deduplication() {
	config := processor.DefaultConfig()
	config.EnableDeduplication = true

	proc, err := processor.New(config)
	if err != nil {
		log.Fatal(err)
	}

	// Content with sufficient length for chunking
	content := []byte(`First paragraph with enough content to meet minimum chunk size requirements.
This paragraph contains sufficient text to ensure the chunker creates a valid chunk that exceeds
the minimum token threshold of 50 tokens as configured in the default text chunker settings.

Second paragraph with different content ensuring unique chunks throughout processing lifecycle.
More content is added here to ensure we meet the minimum requirements for proper chunking validation.

Third paragraph adds more unique content to validate deduplication works correctly with the
hash-based filtering mechanism that prevents duplicate chunks from appearing in final results.
`)

	ctx := context.Background()
	chunks, err := proc.ProcessContent(ctx, content, "test.txt")
	if err != nil {
		log.Fatal(err)
	}

	// Verify all chunks have unique hashes
	hashes := make(map[string]bool)
	for _, chunk := range chunks {
		if hashes[chunk.Hash] {
			fmt.Println("Duplicate found (should not happen)")
		}
		hashes[chunk.Hash] = true
	}

	fmt.Printf("All %d chunks have unique hashes\n", len(chunks))
	// Output: All 1 chunks have unique hashes
}

// ExampleProcessor_metadata demonstrates metadata extraction
func ExampleProcessor_metadata() {
	config := processor.DefaultConfig()
	config.MetadataConfig.EnableGit = false // Disable git for this example
	config.MetadataConfig.ExtractKeywords = true

	proc, err := processor.New(config)
	if err != nil {
		log.Fatal(err)
	}

	content := []byte(`package calculator

// Add adds two numbers
func Add(a, b int) int {
	result := a + b
	return result
}
`)

	ctx := context.Background()
	chunks, err := proc.ProcessContent(ctx, content, "calculator.go")
	if err != nil {
		log.Fatal(err)
	}

	// Display metadata
	for _, chunk := range chunks {
		fmt.Printf("File: %s\n", chunk.Metadata.FileName)
		fmt.Printf("Language: %s\n", chunk.Metadata.Language)
		fmt.Printf("Function: %s\n", chunk.Metadata.FunctionName)
		fmt.Printf("Scope: %s\n", chunk.Metadata.Scope)
		fmt.Printf("Has content: %v\n", len(chunk.Content) > 0)
	}
	// Output:
	// File: calculator.go
	// Language: go
	// Function: Add
	// Scope: function
	// Has content: true
}
