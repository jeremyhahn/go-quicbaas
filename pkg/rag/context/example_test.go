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

package context_test

import (
	"fmt"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/context"
)

func ExampleBuilder() {
	// Create a context builder with default config
	builder := context.NewBuilder()

	// Sample search results
	results := []context.Result{
		{
			Content:     "func main() {\n    fmt.Println(\"Hello, World!\")\n}",
			FusionScore: 0.95,
			Metadata: context.Metadata{
				FilePath:     "/path/to/main.go",
				StartLine:    1,
				EndLine:      3,
				Language:     "go",
				FunctionName: "main",
			},
		},
		{
			Content:     "package main\n\nimport \"fmt\"",
			FusionScore: 0.85,
			Metadata: context.Metadata{
				FilePath:  "/path/to/main.go",
				StartLine: 1,
				EndLine:   3,
				Language:  "go",
			},
		},
	}

	// Build context for LLM consumption
	builtContext, err := builder.Build(results, 8000)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Built context with %d tokens\n", builtContext.Tokens)
	fmt.Printf("Included %d results\n", len(builtContext.Results))
	fmt.Printf("Truncated: %v\n", builtContext.Truncated)
	// Output:
	// Built context with 187 tokens
	// Included 2 results
	// Truncated: false
}

func ExampleBuilder_BuildWithTemplate() {
	builder := context.NewBuilder()

	results := []context.Result{
		{
			Content:     "test content",
			FusionScore: 0.9,
			Metadata: context.Metadata{
				FilePath: "test.go",
				Language: "go",
			},
		},
	}

	// Build with JSON template
	jsonContext, err := builder.BuildWithTemplate(results, context.TemplateJSON, 8000)
	if err != nil {
		panic(err)
	}

	fmt.Printf("JSON format, %d tokens\n", jsonContext.Tokens)

	// Build with Markdown template (default)
	mdContext, err := builder.BuildWithTemplate(results, context.TemplateMarkdown, 8000)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Markdown format, %d tokens\n", mdContext.Tokens)
	// Output:
	// JSON format, 80 tokens
	// Markdown format, 83 tokens
}

func ExampleNewBuilderWithConfig() {
	// Create custom configuration
	config := context.DefaultConfig().
		WithMaxTokens(5000).
		WithTemplate(context.TemplateJSON).
		WithCitations(true).
		WithTruncationStrategy(context.TruncationSmart)

	builder := context.NewBuilderWithConfig(config)

	results := []context.Result{
		{
			Content:     "example code",
			FusionScore: 0.95,
			Metadata: context.Metadata{
				FilePath:  "example.go",
				StartLine: 10,
				EndLine:   20,
			},
		},
	}

	builtContext, err := builder.Build(results, 0) // 0 = use default from config
	if err != nil {
		panic(err)
	}

	fmt.Printf("Citations included: %d\n", len(builtContext.Citations))
	// Output:
	// Citations included: 1
}

func ExampleSimpleTokenizer() {
	tokenizer := context.NewSimpleTokenizer()

	text := "The quick brown fox jumps over the lazy dog"
	tokens := tokenizer.Count(text)

	fmt.Printf("Text: %q\n", text)
	fmt.Printf("Estimated tokens: %d\n", tokens)
	// Output:
	// Text: "The quick brown fox jumps over the lazy dog"
	// Estimated tokens: 11
}

func ExampleSmartTruncate() {
	tokenizer := context.NewSimpleTokenizer()

	results := []context.Result{
		{Content: "First result with some content", FusionScore: 0.9},
		{Content: "Second result with more content", FusionScore: 0.8},
		{Content: "Third result", FusionScore: 0.7},
	}

	// Truncate to fit within 50 tokens (very small limit for demo)
	truncated := context.SmartTruncate(results, 50, tokenizer)

	fmt.Printf("Original results: %d\n", len(results))
	fmt.Printf("After truncation: %d\n", len(truncated))
	// Output:
	// Original results: 3
	// After truncation: 1
}

func ExampleBuilder_EstimateTokens() {
	builder := context.NewBuilder()

	results := []context.Result{
		{Content: "First result"},
		{Content: "Second result"},
		{Content: "Third result"},
	}

	estimatedTokens := builder.EstimateTokens(results)

	fmt.Printf("Estimated tokens for %d results: %d\n", len(results), estimatedTokens)
	// Output:
	// Estimated tokens for 3 results: 356
}

func ExampleGetRenderer() {
	// Get Markdown renderer
	mdRenderer := context.GetRenderer(context.TemplateMarkdown)
	fmt.Printf("Markdown renderer: %T\n", mdRenderer)

	// Get JSON renderer
	jsonRenderer := context.GetRenderer(context.TemplateJSON)
	fmt.Printf("JSON renderer: %T\n", jsonRenderer)

	// Get XML renderer
	xmlRenderer := context.GetRenderer(context.TemplateXML)
	fmt.Printf("XML renderer: %T\n", xmlRenderer)

	// Get Plain text renderer
	plainRenderer := context.GetRenderer(context.TemplatePlain)
	fmt.Printf("Plain renderer: %T\n", plainRenderer)

	// Output:
	// Markdown renderer: *context.MarkdownRenderer
	// JSON renderer: *context.JSONRenderer
	// XML renderer: *context.XMLRenderer
	// Plain renderer: *context.PlainRenderer
}

func ExampleConfig() {
	// Use default configuration
	defaultConfig := context.DefaultConfig()
	fmt.Printf("Default max tokens: %d\n", defaultConfig.DefaultMaxTokens)

	// Create custom configuration
	customConfig := context.DefaultConfig().
		WithMaxTokens(10000).
		WithTemplate(context.TemplateJSON).
		WithMetadata(false).
		WithCitations(false).
		WithTruncationStrategy(context.TruncationHard)

	fmt.Printf("Custom max tokens: %d\n", customConfig.DefaultMaxTokens)
	fmt.Printf("Custom template: %s\n", customConfig.DefaultTemplate)
	fmt.Printf("Include metadata: %v\n", customConfig.IncludeMetadata)
	// Output:
	// Default max tokens: 8000
	// Custom max tokens: 10000
	// Custom template: json
	// Include metadata: false
}
