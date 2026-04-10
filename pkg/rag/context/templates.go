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
	"strings"
)

// TemplateRenderer renders results in different formats.
type TemplateRenderer interface {
	Render(results []Result, citations []Citation, truncated bool) string
}

// MarkdownRenderer renders results as markdown.
type MarkdownRenderer struct{}

// Render creates markdown formatted context.
func (r *MarkdownRenderer) Render(results []Result, citations []Citation, truncated bool) string {
	var sb strings.Builder

	// Header
	sb.WriteString("# Relevant Code and Documentation\n\n")
	sb.WriteString("The following code snippets and documentation are relevant to your query.\n\n")

	// Results
	for i, result := range results {
		sb.WriteString(fmt.Sprintf("## Result %d (Score: %.2f)\n\n", i+1, result.FusionScore))

		// Metadata
		sb.WriteString(fmt.Sprintf("**File:** %s", result.Metadata.FilePath))
		if result.Metadata.StartLine > 0 {
			sb.WriteString(fmt.Sprintf(":%d-%d", result.Metadata.StartLine, result.Metadata.EndLine))
		}
		sb.WriteString("\n")

		if result.Metadata.Language != "" {
			sb.WriteString(fmt.Sprintf("**Language:** %s\n", result.Metadata.Language))
		}

		if result.Metadata.FunctionName != "" {
			sb.WriteString(fmt.Sprintf("**Function:** %s\n", result.Metadata.FunctionName))
		}

		if result.Metadata.ClassName != "" {
			sb.WriteString(fmt.Sprintf("**Class:** %s\n", result.Metadata.ClassName))
		}

		sb.WriteString("\n")

		// Code block
		lang := result.Metadata.Language
		if lang == "" {
			lang = "text"
		}
		sb.WriteString(fmt.Sprintf("```%s\n%s\n```\n\n", lang, result.Content))
	}

	// Footer
	sb.WriteString("---\n\n")
	sb.WriteString(fmt.Sprintf("**Total Results:** %d", len(results)))
	if truncated {
		sb.WriteString(" (truncated to fit token limit)")
	}
	sb.WriteString("\n")

	return sb.String()
}

// JSONRenderer renders results as JSON.
type JSONRenderer struct{}

// JSONOutput represents JSON output structure.
type JSONOutput struct {
	Results   []JSONResult `json:"results"`
	Truncated bool         `json:"truncated"`
}

// JSONResult represents a single result in JSON.
type JSONResult struct {
	Index    int            `json:"index"`
	Score    float32        `json:"score"`
	Content  string         `json:"content"`
	Metadata JSONResultMeta `json:"metadata"`
}

// JSONResultMeta represents result metadata in JSON.
type JSONResultMeta struct {
	FilePath     string   `json:"file_path"`
	StartLine    int      `json:"start_line,omitempty"`
	EndLine      int      `json:"end_line,omitempty"`
	Language     string   `json:"language,omitempty"`
	FunctionName string   `json:"function_name,omitempty"`
	ClassName    string   `json:"class_name,omitempty"`
	Keywords     []string `json:"keywords,omitempty"`
}

// Render creates JSON formatted context.
func (r *JSONRenderer) Render(results []Result, citations []Citation, truncated bool) string {
	output := JSONOutput{
		Results:   make([]JSONResult, len(results)),
		Truncated: truncated,
	}

	for i, result := range results {
		output.Results[i] = JSONResult{
			Index:   i + 1,
			Score:   result.FusionScore,
			Content: result.Content,
			Metadata: JSONResultMeta{
				FilePath:     result.Metadata.FilePath,
				StartLine:    result.Metadata.StartLine,
				EndLine:      result.Metadata.EndLine,
				Language:     result.Metadata.Language,
				FunctionName: result.Metadata.FunctionName,
				ClassName:    result.Metadata.ClassName,
				Keywords:     result.Metadata.Keywords,
			},
		}
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"error": "failed to marshal JSON: %s"}`, err)
	}

	return string(data)
}

// XMLRenderer renders results as XML.
type XMLRenderer struct{}

// XMLOutput represents XML output structure.
type XMLOutput struct {
	XMLName   xml.Name    `xml:"context"`
	Results   []XMLResult `xml:"result"`
	Truncated bool        `xml:"truncated,attr"`
}

// XMLResult represents a single result in XML.
type XMLResult struct {
	Index    int           `xml:"index,attr"`
	Score    float32       `xml:"score,attr"`
	Content  string        `xml:"content"`
	Metadata XMLResultMeta `xml:"metadata"`
}

// XMLResultMeta represents result metadata in XML.
type XMLResultMeta struct {
	FilePath     string   `xml:"file_path"`
	StartLine    int      `xml:"start_line,omitempty"`
	EndLine      int      `xml:"end_line,omitempty"`
	Language     string   `xml:"language,omitempty"`
	FunctionName string   `xml:"function_name,omitempty"`
	ClassName    string   `xml:"class_name,omitempty"`
	Keywords     []string `xml:"keywords>keyword,omitempty"`
}

// Render creates XML formatted context.
func (r *XMLRenderer) Render(results []Result, citations []Citation, truncated bool) string {
	output := XMLOutput{
		Results:   make([]XMLResult, len(results)),
		Truncated: truncated,
	}

	for i, result := range results {
		output.Results[i] = XMLResult{
			Index:   i + 1,
			Score:   result.FusionScore,
			Content: result.Content,
			Metadata: XMLResultMeta{
				FilePath:     result.Metadata.FilePath,
				StartLine:    result.Metadata.StartLine,
				EndLine:      result.Metadata.EndLine,
				Language:     result.Metadata.Language,
				FunctionName: result.Metadata.FunctionName,
				ClassName:    result.Metadata.ClassName,
				Keywords:     result.Metadata.Keywords,
			},
		}
	}

	data, err := xml.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Sprintf(`<error>failed to marshal XML: %s</error>`, err)
	}

	return xml.Header + string(data)
}

// PlainRenderer renders results as plain text.
type PlainRenderer struct{}

// Render creates plain text formatted context.
func (r *PlainRenderer) Render(results []Result, citations []Citation, truncated bool) string {
	var sb strings.Builder

	sb.WriteString("RELEVANT CODE AND DOCUMENTATION\n")
	sb.WriteString(strings.Repeat("=", 80) + "\n\n")

	for i, result := range results {
		sb.WriteString(fmt.Sprintf("Result %d (Score: %.2f)\n", i+1, result.FusionScore))
		sb.WriteString(strings.Repeat("-", 80) + "\n")

		// Metadata
		sb.WriteString(fmt.Sprintf("File: %s", result.Metadata.FilePath))
		if result.Metadata.StartLine > 0 {
			sb.WriteString(fmt.Sprintf(":%d-%d", result.Metadata.StartLine, result.Metadata.EndLine))
		}
		sb.WriteString("\n")

		if result.Metadata.Language != "" {
			sb.WriteString(fmt.Sprintf("Language: %s\n", result.Metadata.Language))
		}

		if result.Metadata.FunctionName != "" {
			sb.WriteString(fmt.Sprintf("Function: %s\n", result.Metadata.FunctionName))
		}

		sb.WriteString("\n")
		sb.WriteString(result.Content)
		sb.WriteString("\n\n")
	}

	sb.WriteString(strings.Repeat("=", 80) + "\n")
	sb.WriteString(fmt.Sprintf("Total Results: %d", len(results)))
	if truncated {
		sb.WriteString(" (truncated to fit token limit)")
	}
	sb.WriteString("\n")

	return sb.String()
}

// GetRenderer returns the appropriate renderer for the template.
func GetRenderer(template Template) TemplateRenderer {
	switch template {
	case TemplateJSON:
		return &JSONRenderer{}
	case TemplateXML:
		return &XMLRenderer{}
	case TemplatePlain:
		return &PlainRenderer{}
	default:
		return &MarkdownRenderer{}
	}
}
