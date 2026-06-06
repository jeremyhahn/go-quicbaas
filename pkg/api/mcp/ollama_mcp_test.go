//go:build mcp

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

package mcp

import (
	"testing"
)

func TestGetAllOllamaTools(t *testing.T) {
	tools := getAllOllamaTools()
	if len(tools) != 5 {
		t.Errorf("expected 5 ollama tools, got %d", len(tools))
	}

	expectedTools := map[string]bool{
		"quicbaas_ollama_embed":       false,
		"quicbaas_ollama_embed_batch": false,
		"quicbaas_ollama_model_list":  false,
		"quicbaas_ollama_model_info":  false,
		"quicbaas_ollama_health":      false,
	}

	for _, tool := range tools {
		if _, ok := expectedTools[tool.Name]; !ok {
			t.Errorf("unexpected tool name: %s", tool.Name)
		}
		expectedTools[tool.Name] = true
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("missing expected tool: %s", name)
		}
	}
}

func TestIsOllamaTool(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		expected bool
	}{
		{"embed tool", "quicbaas_ollama_embed", true},
		{"embed batch tool", "quicbaas_ollama_embed_batch", true},
		{"model list tool", "quicbaas_ollama_model_list", true},
		{"model info tool", "quicbaas_ollama_model_info", true},
		{"health tool", "quicbaas_ollama_health", true},
		{"unknown tool", "quicbaas_unknown", false},
		{"vector tool", "qrdb_vector_search_similar", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isOllamaTool(tt.toolName)
			if result != tt.expected {
				t.Errorf("isOllamaTool(%s) = %v, want %v", tt.toolName, result, tt.expected)
			}
		})
	}
}

func TestOllamaEmbedTool_Schema(t *testing.T) {
	tool := OllamaEmbedTool

	if tool.Name != "quicbaas_ollama_embed" {
		t.Errorf("expected name 'quicbaas_ollama_embed', got %s", tool.Name)
	}

	if tool.InputSchema.Type != "object" {
		t.Errorf("expected type 'object', got %s", tool.InputSchema.Type)
	}

	// Check required fields
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "text" {
		t.Errorf("expected required field 'text', got %v", tool.InputSchema.Required)
	}

	// Check properties exist
	props := tool.InputSchema.Properties
	if _, ok := props["text"]; !ok {
		t.Error("missing 'text' property")
	}
	if _, ok := props["model"]; !ok {
		t.Error("missing 'model' property")
	}
}

func TestOllamaEmbedBatchTool_Schema(t *testing.T) {
	tool := OllamaEmbedBatchTool

	if tool.Name != "quicbaas_ollama_embed_batch" {
		t.Errorf("expected name 'quicbaas_ollama_embed_batch', got %s", tool.Name)
	}

	// Check required fields
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "texts" {
		t.Errorf("expected required field 'texts', got %v", tool.InputSchema.Required)
	}

	// Check properties exist
	props := tool.InputSchema.Properties
	if _, ok := props["texts"]; !ok {
		t.Error("missing 'texts' property")
	}
	if _, ok := props["model"]; !ok {
		t.Error("missing 'model' property")
	}
}

func TestOllamaModelListTool_Schema(t *testing.T) {
	tool := OllamaModelListTool

	if tool.Name != "quicbaas_ollama_model_list" {
		t.Errorf("expected name 'quicbaas_ollama_model_list', got %s", tool.Name)
	}

	// No required fields
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("expected no required fields, got %v", tool.InputSchema.Required)
	}
}

func TestOllamaModelInfoTool_Schema(t *testing.T) {
	tool := OllamaModelInfoTool

	if tool.Name != "quicbaas_ollama_model_info" {
		t.Errorf("expected name 'quicbaas_ollama_model_info', got %s", tool.Name)
	}

	// Check required fields
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "model" {
		t.Errorf("expected required field 'model', got %v", tool.InputSchema.Required)
	}

	// Check properties exist
	props := tool.InputSchema.Properties
	if _, ok := props["model"]; !ok {
		t.Error("missing 'model' property")
	}
}

func TestOllamaHealthTool_Schema(t *testing.T) {
	tool := OllamaHealthTool

	if tool.Name != "quicbaas_ollama_health" {
		t.Errorf("expected name 'quicbaas_ollama_health', got %s", tool.Name)
	}

	// No required fields
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("expected no required fields, got %v", tool.InputSchema.Required)
	}
}

func TestConvertInputSchema(t *testing.T) {
	schema := InputSchema{
		Type: "object",
		Properties: map[string]Property{
			"text": {
				Type:        "string",
				Description: "Test description",
			},
			"count": {
				Type:        "integer",
				Description: "Count value",
				Default:     "10",
			},
			"format": {
				Type:        "string",
				Description: "Format option",
				Enum:        []string{"json", "xml", "plain"},
			},
		},
		Required: []string{"text"},
	}

	result := convertInputSchema(schema)

	if result["type"] != "object" {
		t.Errorf("expected type 'object', got %v", result["type"])
	}

	props, ok := result["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties to be map[string]interface{}")
	}

	// Check text property
	textProp, ok := props["text"].(map[string]interface{})
	if !ok {
		t.Fatal("expected text property to be map[string]interface{}")
	}
	if textProp["type"] != "string" {
		t.Errorf("expected text type 'string', got %v", textProp["type"])
	}

	// Check count property with default
	countProp, ok := props["count"].(map[string]interface{})
	if !ok {
		t.Fatal("expected count property to be map[string]interface{}")
	}
	if countProp["default"] != "10" {
		t.Errorf("expected count default '10', got %v", countProp["default"])
	}

	// Check format property with enum
	formatProp, ok := props["format"].(map[string]interface{})
	if !ok {
		t.Fatal("expected format property to be map[string]interface{}")
	}
	enumVal, ok := formatProp["enum"].([]string)
	if !ok || len(enumVal) != 3 {
		t.Errorf("expected format enum with 3 values, got %v", formatProp["enum"])
	}

	// Check required
	required, ok := result["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "text" {
		t.Errorf("expected required ['text'], got %v", result["required"])
	}
}
