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

package context

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestBuilder_Build(t *testing.T) {
	builder := NewBuilder()

	results := []Result{
		{
			Content:     "test content",
			FusionScore: 0.95,
			Metadata: Metadata{
				FilePath:  "/path/to/file.go",
				StartLine: 10,
				EndLine:   20,
				Language:  "go",
			},
		},
	}

	context, err := builder.Build(results, 8000)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	if context == nil {
		t.Fatal("Build() returned nil context")
	}

	if context.Content == "" {
		t.Error("Build() returned empty content")
	}

	if context.Tokens <= 0 {
		t.Error("Build() returned invalid token count")
	}

	if len(context.Results) != 1 {
		t.Errorf("Build() returned %d results, want 1", len(context.Results))
	}
}

func TestBuilder_BuildWithTemplate(t *testing.T) {
	builder := NewBuilder()

	results := []Result{
		{
			Content:     "test",
			FusionScore: 0.9,
			Metadata:    Metadata{FilePath: "test.go"},
		},
	}

	templates := []Template{
		TemplateMarkdown,
		TemplateJSON,
		TemplateXML,
		TemplatePlain,
	}

	for _, tmpl := range templates {
		t.Run(string(tmpl), func(t *testing.T) {
			context, err := builder.BuildWithTemplate(results, tmpl, 8000)
			if err != nil {
				t.Fatalf("BuildWithTemplate(%s) returned error: %v", tmpl, err)
			}

			if context.Content == "" {
				t.Error("BuildWithTemplate() returned empty content")
			}
		})
	}
}

func TestBuilder_EstimateTokens(t *testing.T) {
	builder := NewBuilder()

	tests := []struct {
		name      string
		results   []Result
		minTokens int
	}{
		{
			name: "results include formatting overhead",
			results: []Result{
				{Content: "short"},
				{Content: "another short text"},
				{Content: "and one more"},
			},
			minTokens: 200,
		},
		{
			name:      "empty results keep header and footer overhead",
			results:   []Result{},
			minTokens: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := builder.EstimateTokens(tt.results)
			if tokens < tt.minTokens {
				t.Errorf("EstimateTokens() = %d, want at least %d", tokens, tt.minTokens)
			}
		})
	}
}

func TestBuilder_Build_MaxTokens(t *testing.T) {
	tests := []struct {
		name      string
		strategy  TruncationStrategy
		count     int
		words     int
		maxTokens int
		limit     int // maxTokens plus the allowed variance
	}{
		{name: "default strategy, few large results", strategy: DefaultConfig().TruncationStrategy, count: 3, words: 500, maxTokens: 500, limit: 600},
		{name: "smart strategy, many results", strategy: TruncationSmart, count: 20, words: 200, maxTokens: 1000, limit: 1200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewBuilderWithConfig(DefaultConfig().WithTruncationStrategy(tt.strategy))

			results := make([]Result, tt.count)
			for i := range results {
				results[i] = Result{
					Content:     strings.Repeat("word ", tt.words),
					FusionScore: float32(0.9 - float32(i)*0.01),
					Metadata:    Metadata{FilePath: "test.go"},
				}
			}

			context, err := builder.Build(results, tt.maxTokens)
			if err != nil {
				t.Fatalf("Build() returned error: %v", err)
			}

			if context.Tokens > tt.limit {
				t.Errorf("Build() resulted in %d tokens, exceeds limit of %d", context.Tokens, tt.maxTokens)
			}
		})
	}
}

func TestBuilder_Build_Citations(t *testing.T) {
	results := []Result{
		{
			Content:     "content 1",
			FusionScore: 0.9,
			Metadata: Metadata{
				FilePath:  "file1.go",
				StartLine: 10,
				EndLine:   20,
			},
		},
		{
			Content:     "content 2",
			FusionScore: 0.8,
			Metadata: Metadata{
				FilePath:  "file2.go",
				StartLine: 30,
				EndLine:   40,
			},
		},
	}

	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("citations=%t", enabled), func(t *testing.T) {
			builder := NewBuilderWithConfig(DefaultConfig().WithCitations(enabled))

			context, err := builder.Build(results, 8000)
			if err != nil {
				t.Fatalf("Build() returned error: %v", err)
			}

			if !enabled {
				if len(context.Citations) != 0 {
					t.Errorf("Got %d citations, want 0", len(context.Citations))
				}
				return
			}

			if len(context.Citations) != len(context.Results) {
				t.Errorf("Got %d citations, want %d (one per result)",
					len(context.Citations), len(context.Results))
			}

			for i, citation := range context.Citations {
				if citation.Index != i+1 {
					t.Errorf("Citation %d has index %d", i, citation.Index)
				}

				if citation.FilePath != context.Results[i].Metadata.FilePath {
					t.Errorf("Citation %d has file path %q, want %q", i, citation.FilePath, context.Results[i].Metadata.FilePath)
				}

				if citation.Score != context.Results[i].FusionScore {
					t.Errorf("Citation %d has score %v, want %v", i, citation.Score, context.Results[i].FusionScore)
				}
			}
		})
	}
}

func TestBuilder_Build_MaxResultsPerChunk(t *testing.T) {
	config := DefaultConfig()
	config.MaxResultsPerChunk = 3
	builder := NewBuilderWithConfig(config)

	results := make([]Result, 10)
	for i := range results {
		results[i] = Result{
			Content:     "test content",
			FusionScore: float32(0.9 - float32(i)*0.05),
			Metadata:    Metadata{FilePath: "test.go"},
		}
	}

	context, err := builder.Build(results, 8000)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	if len(context.Results) > 3 {
		t.Errorf("Got %d results, want at most 3", len(context.Results))
	}
}

func TestBuilder_Build_TruncationStrategies(t *testing.T) {
	results := []Result{
		{Content: strings.Repeat("word ", 300), FusionScore: 0.9, Metadata: Metadata{FilePath: "1.go"}},
		{Content: strings.Repeat("word ", 300), FusionScore: 0.8, Metadata: Metadata{FilePath: "2.go"}},
	}

	strategies := []TruncationStrategy{
		TruncationSmart,
		TruncationHard,
		TruncationNone,
	}

	for _, strategy := range strategies {
		t.Run(string(strategy), func(t *testing.T) {
			config := DefaultConfig().WithTruncationStrategy(strategy)
			builder := NewBuilderWithConfig(config)

			context, err := builder.Build(results, 500)
			if err != nil {
				t.Fatalf("Build() with %s returned error: %v", strategy, err)
			}

			if context.Content == "" {
				t.Error("Build() returned empty content")
			}
		})
	}
}

func TestBuilder_Build_EdgeInputs(t *testing.T) {
	builder := NewBuilder()
	oneResult := []Result{
		{Content: "test", FusionScore: 0.9, Metadata: Metadata{FilePath: "test.go"}},
	}

	tests := []struct {
		name        string
		results     []Result
		maxTokens   int
		wantContent bool
	}{
		{name: "empty results", results: []Result{}, maxTokens: 8000},
		// Zero and negative maxTokens fall back to the default limit.
		{name: "zero maxTokens", results: oneResult, maxTokens: 0, wantContent: true},
		{name: "negative maxTokens", results: oneResult, maxTokens: -100, wantContent: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			context, err := builder.Build(tt.results, tt.maxTokens)
			if err != nil {
				t.Fatalf("Build() returned error: %v", err)
			}

			if context == nil {
				t.Fatal("Build() returned nil context")
			}

			if tt.wantContent && context.Content == "" {
				t.Error("Build() returned empty content")
			}
		})
	}
}

func TestValidateResults(t *testing.T) {
	tests := []struct {
		name    string
		results []Result
		wantErr bool
	}{
		{
			name:    "empty results",
			results: []Result{},
			wantErr: true,
		},
		{
			name: "valid results",
			results: []Result{
				{Content: "test"},
			},
			wantErr: false,
		},
		{
			name: "result with empty content",
			results: []Result{
				{Content: ""},
			},
			wantErr: true,
		},
		{
			name: "mixed valid and invalid",
			results: []Result{
				{Content: "valid"},
				{Content: ""},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResults(tt.results)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateResults() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFilterEmptyResults(t *testing.T) {
	tests := []struct {
		name     string
		results  []Result
		expected int
	}{
		{
			name:     "no empty results",
			results:  []Result{{Content: "a"}, {Content: "b"}},
			expected: 2,
		},
		{
			name:     "some empty results",
			results:  []Result{{Content: "a"}, {Content: ""}, {Content: "b"}},
			expected: 2,
		},
		{
			name:     "all empty results",
			results:  []Result{{Content: ""}, {Content: ""}},
			expected: 0,
		},
		{
			name:     "empty input",
			results:  []Result{},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := FilterEmptyResults(tt.results)
			if len(filtered) != tt.expected {
				t.Errorf("FilterEmptyResults() returned %d results, want %d",
					len(filtered), tt.expected)
			}

			// Verify no empty content
			for i, result := range filtered {
				if result.Content == "" {
					t.Errorf("Filtered result %d has empty content", i)
				}
			}
		})
	}
}

// Benchmark tests
func BenchmarkBuilder_Build(b *testing.B) {
	builder := NewBuilder()

	results := []Result{
		{Content: "test content", FusionScore: 0.9, Metadata: Metadata{FilePath: "test.go"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = builder.Build(results, 8000)
	}
}

func BenchmarkBuilder_BuildLarge(b *testing.B) {
	builder := NewBuilder()

	results := make([]Result, 10)
	for i := range results {
		results[i] = Result{
			Content:     strings.Repeat("test content ", 100),
			FusionScore: 0.9,
			Metadata:    Metadata{FilePath: "test.go"},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = builder.Build(results, 8000)
	}
}

func BenchmarkBuilder_EstimateTokens(b *testing.B) {
	builder := NewBuilder()

	results := make([]Result, 10)
	for i := range results {
		results[i] = Result{Content: strings.Repeat("word ", 50)}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = builder.EstimateTokens(results)
	}
}

func TestNewBuilderWithConfig_InvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name: "negative max tokens",
			config: Config{
				DefaultMaxTokens:   -1000, // Invalid
				MaxResultsPerChunk: 10,
				DefaultTemplate:    TemplateMarkdown,
				TokenMultiplier:    1.3,
				IncludeMetadata:    true,
				IncludeCitations:   false,
				TruncationStrategy: TruncationSmart,
			},
		},
		{
			name: "zero max results",
			config: Config{
				DefaultMaxTokens:   8000,
				MaxResultsPerChunk: 0, // Invalid
				DefaultTemplate:    TemplateMarkdown,
				TokenMultiplier:    1.3,
				IncludeMetadata:    true,
				IncludeCitations:   false,
				TruncationStrategy: TruncationSmart,
			},
		},
		{
			name: "invalid token multiplier",
			config: Config{
				DefaultMaxTokens:   8000,
				MaxResultsPerChunk: 10,
				DefaultTemplate:    TemplateMarkdown,
				TokenMultiplier:    0.0, // Invalid
				IncludeMetadata:    true,
				IncludeCitations:   false,
				TruncationStrategy: TruncationSmart,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewBuilderWithConfig(tt.config)

			// Should return a builder with default config instead of failing
			if builder == nil {
				t.Fatal("NewBuilderWithConfig() returned nil for invalid config")
			}

			// Verify it uses default config by testing it works
			results := []Result{
				{Content: "test", FusionScore: 0.9, Metadata: Metadata{FilePath: "test.go"}},
			}

			context, err := builder.Build(results, 1000)
			if err != nil {
				t.Fatalf("Builder with invalid config failed: %v", err)
			}

			if context == nil {
				t.Error("Builder with invalid config returned nil context")
			}
		})
	}
}

// TestJSONRenderer_ValidOutput tests JSONRenderer.Render
// This covers templates.go:122-127 where json.MarshalIndent is called
func TestJSONRenderer_ValidOutput(t *testing.T) {
	renderer := &JSONRenderer{}

	results := []Result{
		{
			Content:     "test content",
			FusionScore: 0.95,
			Metadata: Metadata{
				FilePath: "/test/file.go",
				Keywords: []string{"test"},
			},
		},
	}

	output := renderer.Render(results, nil, false)

	// Should produce valid JSON
	if output == "" {
		t.Error("JSONRenderer.Render() returned empty string")
	}

	// Verify it's valid JSON
	var parsed JSONOutput
	err := json.Unmarshal([]byte(output), &parsed)
	if err != nil {
		t.Errorf("JSONRenderer.Render() returned invalid JSON: %v", err)
	}
}

// TestXMLRenderer_ValidOutput tests XMLRenderer.Render
// This covers templates.go:183-188 where xml.MarshalIndent is called
func TestXMLRenderer_ValidOutput(t *testing.T) {
	renderer := &XMLRenderer{}

	results := []Result{
		{
			Content:     "test content",
			FusionScore: 0.95,
			Metadata: Metadata{
				FilePath: "/test/file.go",
				Keywords: []string{"test"},
			},
		},
	}

	output := renderer.Render(results, nil, false)

	// Should produce valid XML
	if output == "" {
		t.Error("XMLRenderer.Render() returned empty string")
	}

	// Verify it's valid XML
	var parsed XMLOutput
	err := xml.Unmarshal([]byte(output), &parsed)
	if err != nil {
		t.Errorf("XMLRenderer.Render() returned invalid XML: %v", err)
	}

	// Should include XML header
	if !strings.HasPrefix(output, "<?xml") {
		t.Error("Missing XML header")
	}
}

// TestSmartTruncate_FirstResultTooLarge tests when first result exceeds limit
// This covers truncation.go:49-58 (the path where first result is truncated)
func TestSmartTruncate_EdgeCases(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	tests := []struct {
		name          string
		content       string
		maxTokens     int
		wantTruncated bool
	}{
		// availableTokens = 500 - 200 = 300, so the first result's content is cut.
		{name: "first result too large is truncated", content: strings.Repeat("word ", 2000), maxTokens: 500, wantTruncated: true},
		{name: "result that fits is kept whole", content: strings.Repeat("word ", 100), maxTokens: 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			truncated := SmartTruncate([]Result{{Content: tt.content, FusionScore: 0.9}}, tt.maxTokens, tokenizer)
			if len(truncated) != 1 {
				t.Fatalf("SmartTruncate() returned %d results, want 1", len(truncated))
			}

			gotTruncated := len(truncated[0].Content) < len(tt.content)
			if gotTruncated != tt.wantTruncated {
				t.Errorf("SmartTruncate() content length %d of %d, want truncated=%t",
					len(truncated[0].Content), len(tt.content), tt.wantTruncated)
			}
		})
	}
}

func TestTruncateContent_EdgeCases(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	tests := []struct {
		name      string
		content   string
		maxTokens int
		check     func(t *testing.T, content, result string)
	}{
		{
			name:      "empty content",
			content:   "",
			maxTokens: 100,
			check: func(t *testing.T, _, result string) {
				if result != "" {
					t.Errorf("TruncateContent() = %q, want empty string", result)
				}
			},
		},
		{
			name:      "truncated content ends with ellipsis",
			content:   "one two three four five six seven eight nine ten eleven twelve",
			maxTokens: 5,
			check: func(t *testing.T, content, result string) {
				if len(result) >= len(content) {
					t.Errorf("TruncateContent() = %q, want it shorter than the input", result)
				}
				if !strings.HasSuffix(result, "...") {
					t.Errorf("TruncateContent() = %q, want a %q suffix", result, "...")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, tt.content, TruncateContent(tt.content, tt.maxTokens, tokenizer))
		})
	}
}

func TestFitResults_EdgeCases(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	tests := []struct {
		name      string
		results   []Result
		maxTokens int
		want      []int
	}{
		{name: "empty results below overhead", results: []Result{}, maxTokens: 100, want: []int{}},
		// maxTokens below the formatting overhead still keeps the first result.
		{name: "limit below overhead keeps first", results: []Result{{Content: "test"}}, maxTokens: 100, want: []int{0}},
		{name: "first result too large is kept", results: []Result{{Content: strings.Repeat("word ", 1000)}}, maxTokens: 300, want: []int{0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indices := FitResults(tt.results, tt.maxTokens, tokenizer)
			if !slices.Equal(indices, tt.want) {
				t.Errorf("FitResults() = %v, want %v", indices, tt.want)
			}
		})
	}
}

// TestRenderers_ComplexMetadata tests all renderers with full metadata
func TestRenderers_ComplexMetadata(t *testing.T) {
	results := []Result{
		{
			Content:     "complex content",
			FusionScore: 0.95,
			Metadata: Metadata{
				FilePath:     "/path/to/file.go",
				FileName:     "file.go",
				Language:     "go",
				StartLine:    10,
				EndLine:      20,
				StartByte:    100,
				EndByte:      200,
				PackageName:  "main",
				FunctionName: "TestFunc",
				ClassName:    "TestClass",
				MethodName:   "TestMethod",
				Keywords:     []string{"test", "func", "method"},
				Tags:         []string{"unit-test", "integration"},
				Category:     "testing",
			},
		},
	}

	renderers := []struct {
		name     string
		renderer TemplateRenderer
	}{
		{"markdown", &MarkdownRenderer{}},
		{"json", &JSONRenderer{}},
		{"xml", &XMLRenderer{}},
		{"plain", &PlainRenderer{}},
	}

	for _, r := range renderers {
		t.Run(r.name, func(t *testing.T) {
			output := r.renderer.Render(results, nil, false)

			if output == "" {
				t.Error("Renderer returned empty output")
			}

			// Verify some metadata is included
			if !strings.Contains(output, "file.go") && !strings.Contains(output, "path") {
				t.Error("Output doesn't contain file path")
			}
		})
	}
}

// TestMarkdownRenderer_NoLanguage tests default language fallback
// This covers templates.go:52-55
func TestMarkdownRenderer_NoLanguage(t *testing.T) {
	renderer := &MarkdownRenderer{}

	results := []Result{
		{
			Content:     "content without language",
			FusionScore: 0.9,
			Metadata: Metadata{
				FilePath: "/path/to/file.txt",
				// No Language field set
			},
		},
	}

	output := renderer.Render(results, nil, false)

	// Should default to "text" for code blocks
	if !strings.Contains(output, "```text") {
		t.Error("Expected default language 'text' in code block")
	}
}

// TestPlainRenderer_NoStartLine tests line number handling
func TestPlainRenderer_NoStartLine(t *testing.T) {
	renderer := &PlainRenderer{}

	results := []Result{
		{
			Content:     "content",
			FusionScore: 0.9,
			Metadata: Metadata{
				FilePath: "/path/to/file.txt",
				// StartLine is 0 (not set)
			},
		},
	}

	output := renderer.Render(results, nil, false)

	// Should not include ":0" for missing line numbers
	if strings.Contains(output, ":0-0") {
		t.Error("Should not show :0-0 for missing line numbers")
	}
}

// TestMarkdownRenderer_ConditionalMetadata tests conditional metadata rendering
// Covers templates.go:37-47 (conditional metadata fields)
func TestMarkdownRenderer_ConditionalMetadata(t *testing.T) {
	renderer := &MarkdownRenderer{}

	tests := []struct {
		name        string
		metadata    Metadata
		contains    []string
		notContains []string
	}{
		{
			name: "no optional fields",
			metadata: Metadata{
				FilePath: "/path/to/file.txt",
			},
			contains:    []string{"/path/to/file.txt"},
			notContains: []string{"**Language:**", "**Function:**", "**Class:**"},
		},
		{
			name: "with language",
			metadata: Metadata{
				FilePath: "/path/to/file.go",
				Language: "go",
			},
			contains:    []string{"**Language:** go"},
			notContains: []string{"**Function:**", "**Class:**"},
		},
		{
			name: "with function",
			metadata: Metadata{
				FilePath:     "/path/to/file.go",
				FunctionName: "TestFunc",
			},
			contains:    []string{"**Function:** TestFunc"},
			notContains: []string{"**Class:**"},
		},
		{
			name: "with class",
			metadata: Metadata{
				FilePath:  "/path/to/file.go",
				ClassName: "TestClass",
			},
			contains:    []string{"**Class:** TestClass"},
			notContains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := []Result{
				{
					Content:     "content",
					FusionScore: 0.9,
					Metadata:    tt.metadata,
				},
			}

			output := renderer.Render(results, nil, false)

			for _, expected := range tt.contains {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q", expected)
				}
			}

			for _, notExpected := range tt.notContains {
				if strings.Contains(output, notExpected) {
					t.Errorf("Expected output not to contain %q", notExpected)
				}
			}
		})
	}
}

// TestPlainRenderer_ConditionalMetadata tests plain renderer metadata
// Covers templates.go:212-219 (conditional metadata in plain renderer)
func TestPlainRenderer_ConditionalMetadata(t *testing.T) {
	renderer := &PlainRenderer{}

	tests := []struct {
		name     string
		metadata Metadata
		contains []string
	}{
		{
			name: "with all metadata",
			metadata: Metadata{
				FilePath:     "/path/to/file.go",
				StartLine:    10,
				EndLine:      20,
				Language:     "go",
				FunctionName: "TestFunc",
			},
			contains: []string{
				"/path/to/file.go:10-20",
				"Language: go",
				"Function: TestFunc",
			},
		},
		{
			name: "minimal metadata",
			metadata: Metadata{
				FilePath: "/path/to/file.txt",
			},
			contains: []string{
				"/path/to/file.txt",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := []Result{
				{
					Content:     "content",
					FusionScore: 0.9,
					Metadata:    tt.metadata,
				},
			}

			output := renderer.Render(results, nil, false)

			for _, expected := range tt.contains {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q", expected)
				}
			}
		})
	}
}
