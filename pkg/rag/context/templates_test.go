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
	"strings"
	"testing"
)

func TestMarkdownRenderer_Render(t *testing.T) {
	renderer := &MarkdownRenderer{}

	results := []Result{
		{
			Content:     "func main() {}",
			FusionScore: 0.95,
			Metadata: Metadata{
				FilePath:     "/path/to/file.go",
				StartLine:    10,
				EndLine:      20,
				Language:     "go",
				FunctionName: "main",
			},
		},
	}

	citations := []Citation{
		{Index: 1, FilePath: "/path/to/file.go", StartLine: 10, EndLine: 20, Score: 0.95},
	}

	output := renderer.Render(results, citations, false)

	// Verify key components
	if !strings.Contains(output, "# Relevant Code and Documentation") {
		t.Error("Missing header")
	}

	if !strings.Contains(output, "## Result 1") {
		t.Error("Missing result header")
	}

	if !strings.Contains(output, "func main()") {
		t.Error("Missing content")
	}

	if !strings.Contains(output, "**File:**") {
		t.Error("Missing file metadata")
	}

	if !strings.Contains(output, "```go") {
		t.Error("Missing code block with language")
	}
}

func TestMarkdownRenderer_MultipeResults(t *testing.T) {
	renderer := &MarkdownRenderer{}

	results := []Result{
		{Content: "content 1", FusionScore: 0.9, Metadata: Metadata{FilePath: "file1.go"}},
		{Content: "content 2", FusionScore: 0.8, Metadata: Metadata{FilePath: "file2.go"}},
		{Content: "content 3", FusionScore: 0.7, Metadata: Metadata{FilePath: "file3.go"}},
	}

	output := renderer.Render(results, nil, false)

	// Should have 3 results
	if strings.Count(output, "## Result") != 3 {
		t.Errorf("Expected 3 results, got different count")
	}

	// Should have all content
	for i, result := range results {
		if !strings.Contains(output, result.Content) {
			t.Errorf("Missing content for result %d", i)
		}
	}
}

func TestMarkdownRenderer_Truncated(t *testing.T) {
	renderer := &MarkdownRenderer{}

	results := []Result{
		{Content: "test", FusionScore: 0.9, Metadata: Metadata{FilePath: "test.go"}},
	}

	output := renderer.Render(results, nil, true)

	if !strings.Contains(output, "truncated to fit token limit") {
		t.Error("Missing truncation notice")
	}
}

func TestJSONRenderer_Render(t *testing.T) {
	renderer := &JSONRenderer{}

	results := []Result{
		{
			Content:     "test content",
			FusionScore: 0.95,
			Metadata: Metadata{
				FilePath:     "/path/to/file.go",
				StartLine:    10,
				EndLine:      20,
				Language:     "go",
				FunctionName: "TestFunc",
				Keywords:     []string{"test", "func"},
			},
		},
	}

	output := renderer.Render(results, nil, false)

	// Verify it's valid JSON
	var parsed JSONOutput
	err := json.Unmarshal([]byte(output), &parsed)
	if err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	// Verify structure
	if len(parsed.Results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(parsed.Results))
	}

	if parsed.Results[0].Content != "test content" {
		t.Errorf("Content mismatch: got %s", parsed.Results[0].Content)
	}

	if parsed.Results[0].Score != 0.95 {
		t.Errorf("Score mismatch: got %f", parsed.Results[0].Score)
	}

	if parsed.Results[0].Metadata.FilePath != "/path/to/file.go" {
		t.Errorf("FilePath mismatch")
	}
}

func TestJSONRenderer_MultipleResults(t *testing.T) {
	renderer := &JSONRenderer{}

	results := []Result{
		{Content: "content 1", FusionScore: 0.9, Metadata: Metadata{FilePath: "file1.go"}},
		{Content: "content 2", FusionScore: 0.8, Metadata: Metadata{FilePath: "file2.go"}},
	}

	output := renderer.Render(results, nil, false)

	var parsed JSONOutput
	err := json.Unmarshal([]byte(output), &parsed)
	if err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	if len(parsed.Results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(parsed.Results))
	}
}

func TestXMLRenderer_Render(t *testing.T) {
	renderer := &XMLRenderer{}

	results := []Result{
		{
			Content:     "test content",
			FusionScore: 0.95,
			Metadata: Metadata{
				FilePath:  "/path/to/file.go",
				StartLine: 10,
				EndLine:   20,
				Language:  "go",
				Keywords:  []string{"test", "func"},
			},
		},
	}

	output := renderer.Render(results, nil, false)

	// Verify it's valid XML
	var parsed XMLOutput
	err := xml.Unmarshal([]byte(output), &parsed)
	if err != nil {
		t.Fatalf("Invalid XML: %v", err)
	}

	// Verify structure
	if len(parsed.Results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(parsed.Results))
	}

	if parsed.Results[0].Content != "test content" {
		t.Errorf("Content mismatch")
	}

	// Should include XML header
	if !strings.HasPrefix(output, "<?xml") {
		t.Error("Missing XML header")
	}
}

func TestXMLRenderer_TruncatedFlag(t *testing.T) {
	renderer := &XMLRenderer{}

	results := []Result{
		{Content: "test", FusionScore: 0.9, Metadata: Metadata{FilePath: "test.go"}},
	}

	output := renderer.Render(results, nil, true)

	var parsed XMLOutput
	err := xml.Unmarshal([]byte(output), &parsed)
	if err != nil {
		t.Fatalf("Invalid XML: %v", err)
	}

	if !parsed.Truncated {
		t.Error("Truncated flag not set")
	}
}

func TestPlainRenderer_Render(t *testing.T) {
	renderer := &PlainRenderer{}

	results := []Result{
		{
			Content:     "test content",
			FusionScore: 0.95,
			Metadata: Metadata{
				FilePath:     "/path/to/file.go",
				StartLine:    10,
				EndLine:      20,
				Language:     "go",
				FunctionName: "TestFunc",
			},
		},
	}

	output := renderer.Render(results, nil, false)

	// Verify key components
	if !strings.Contains(output, "RELEVANT CODE AND DOCUMENTATION") {
		t.Error("Missing header")
	}

	if !strings.Contains(output, "Result 1") {
		t.Error("Missing result header")
	}

	if !strings.Contains(output, "test content") {
		t.Error("Missing content")
	}

	if !strings.Contains(output, "File:") {
		t.Error("Missing file metadata")
	}

	// Should have separator lines
	if !strings.Contains(output, strings.Repeat("=", 80)) {
		t.Error("Missing separator")
	}
}

func TestPlainRenderer_Truncated(t *testing.T) {
	renderer := &PlainRenderer{}

	results := []Result{
		{Content: "test", FusionScore: 0.9, Metadata: Metadata{FilePath: "test.go"}},
	}

	output := renderer.Render(results, nil, true)

	if !strings.Contains(output, "truncated to fit token limit") {
		t.Error("Missing truncation notice")
	}
}

func TestGetRenderer(t *testing.T) {
	tests := []struct {
		template     Template
		expectedType string
	}{
		{TemplateMarkdown, "markdown"},
		{TemplateJSON, "json"},
		{TemplateXML, "xml"},
		{TemplatePlain, "plain"},
		{Template("unknown"), "markdown"}, // Default
	}

	for _, tt := range tests {
		t.Run(string(tt.template), func(t *testing.T) {
			renderer := GetRenderer(tt.template)

			switch tt.expectedType {
			case "markdown":
				if _, ok := renderer.(*MarkdownRenderer); !ok {
					t.Errorf("GetRenderer(%s) did not return MarkdownRenderer", tt.template)
				}
			case "json":
				if _, ok := renderer.(*JSONRenderer); !ok {
					t.Errorf("GetRenderer(%s) did not return JSONRenderer", tt.template)
				}
			case "xml":
				if _, ok := renderer.(*XMLRenderer); !ok {
					t.Errorf("GetRenderer(%s) did not return XMLRenderer", tt.template)
				}
			case "plain":
				if _, ok := renderer.(*PlainRenderer); !ok {
					t.Errorf("GetRenderer(%s) did not return PlainRenderer", tt.template)
				}
			}
		})
	}
}
func TestRenderers_EmptyResults(t *testing.T) {
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
			output := r.renderer.Render([]Result{}, nil, false)
			if output == "" {
				t.Error("Renderer returned empty string for empty results")
			}
		})
	}
}

func TestMarkdownRenderer_AllMetadata(t *testing.T) {
	renderer := &MarkdownRenderer{}

	results := []Result{
		{
			Content:     "code here",
			FusionScore: 0.95,
			Metadata: Metadata{
				FilePath:     "/path/to/file.go",
				StartLine:    10,
				EndLine:      20,
				Language:     "go",
				FunctionName: "TestFunc",
				ClassName:    "TestClass",
			},
		},
	}

	output := renderer.Render(results, nil, false)

	// All metadata should be present
	if !strings.Contains(output, "TestFunc") {
		t.Error("Missing function name")
	}

	if !strings.Contains(output, "TestClass") {
		t.Error("Missing class name")
	}

	if !strings.Contains(output, ":10-20") {
		t.Error("Missing line numbers")
	}
}

// Helper function to get type name
func getTypeName(v interface{}) string {
	return strings.TrimPrefix(strings.TrimPrefix(
		strings.Split(strings.Split(
			strings.TrimPrefix(
				strings.TrimPrefix(
					toString(v), "&"),
				"*"),
			"{")[0], " ")[0],
		"&"), "*")
}

func toString(v interface{}) string {
	switch v.(type) {
	case *MarkdownRenderer:
		return "*context.MarkdownRenderer"
	case *JSONRenderer:
		return "*context.JSONRenderer"
	case *XMLRenderer:
		return "*context.XMLRenderer"
	case *PlainRenderer:
		return "*context.PlainRenderer"
	default:
		return "unknown"
	}
}

// Benchmark tests
func BenchmarkMarkdownRenderer(b *testing.B) {
	renderer := &MarkdownRenderer{}
	results := []Result{
		{Content: "test content", FusionScore: 0.9, Metadata: Metadata{FilePath: "test.go"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = renderer.Render(results, nil, false)
	}
}

func BenchmarkJSONRenderer(b *testing.B) {
	renderer := &JSONRenderer{}
	results := []Result{
		{Content: "test content", FusionScore: 0.9, Metadata: Metadata{FilePath: "test.go"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = renderer.Render(results, nil, false)
	}
}

func BenchmarkXMLRenderer(b *testing.B) {
	renderer := &XMLRenderer{}
	results := []Result{
		{Content: "test content", FusionScore: 0.9, Metadata: Metadata{FilePath: "test.go"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = renderer.Render(results, nil, false)
	}
}
