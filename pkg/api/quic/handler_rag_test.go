//go:build quic && rag

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

package quic

import (
	"context"
	"testing"
)

// --- Content moved from handler_rag_coverage_test.go ---

// =============================================================================
// RAG Handler Tests - Service Unavailable and Validation
// =============================================================================

func TestRAGQuery_OllamaUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&RAGQueryRequest{
		Query:     "test query",
		Namespace: "test-ns",
	})

	resp := h.handleRAGQuery(ctx, &Request{Payload: payload})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable Ollama service")
	}
}

func TestRAGQuery_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleRAGQuery(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestRAGBuildContext_EmptyResults(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&RAGBuildContextRequest{
		Results: []RAGResultInput{}, // Empty results should fail validation
	})

	resp := h.handleRAGBuildContext(ctx, &Request{Payload: payload})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for empty results")
	}
}

func TestRAGBuildContext_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleRAGBuildContext(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

// =============================================================================
// RAG Helper Function Tests
// =============================================================================

func TestParseTemplate(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"json", "json"},
		{"xml", "xml"},
		{"plain", "plain"},
		{"markdown", "markdown"},
		{"unknown", "markdown"}, // default is markdown
		{"", "markdown"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseTemplate(tt.input)
			// Template is an alias for string in ragctx, so compare string value
			if string(result) != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, string(result))
			}
		})
	}
}

func TestParseTruncationStrategy(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hard", "hard"},
		{"none", "none"},
		{"smart", "smart"},
		{"unknown", "smart"}, // default is smart
		{"", "smart"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseTruncationStrategy(tt.input)
			if string(result) != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, string(result))
			}
		})
	}
}

func TestGetStringFromInterfaceMap(t *testing.T) {
	tests := []struct {
		name     string
		m        map[string]interface{}
		key      string
		expected string
	}{
		{"nil map", nil, "key", ""},
		{"empty map", map[string]interface{}{}, "key", ""},
		{"key not found", map[string]interface{}{"other": "value"}, "key", ""},
		{"key found string", map[string]interface{}{"key": "value"}, "key", "value"},
		{"key found non-string", map[string]interface{}{"key": 123}, "key", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getStringFromInterfaceMap(tt.m, tt.key)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestGetIntFromInterfaceMap(t *testing.T) {
	tests := []struct {
		name     string
		m        map[string]interface{}
		key      string
		expected int
	}{
		{"nil map", nil, "key", 0},
		{"empty map", map[string]interface{}{}, "key", 0},
		{"key not found", map[string]interface{}{"other": 123}, "key", 0},
		{"key found int", map[string]interface{}{"key": 42}, "key", 42},
		{"key found int64", map[string]interface{}{"key": int64(42)}, "key", 42},
		{"key found float64", map[string]interface{}{"key": float64(42.0)}, "key", 42},
		{"key found string int", map[string]interface{}{"key": "42"}, "key", 42},
		{"key found string non-int", map[string]interface{}{"key": "not-a-number"}, "key", 0},
		{"key found non-numeric", map[string]interface{}{"key": []int{1, 2}}, "key", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getIntFromInterfaceMap(tt.m, tt.key)
			if result != tt.expected {
				t.Fatalf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestGetIntFromStringMetadata(t *testing.T) {
	tests := []struct {
		name     string
		m        map[string]string
		key      string
		expected int
	}{
		{"nil map", nil, "key", 0},
		{"empty map", map[string]string{}, "key", 0},
		{"key not found", map[string]string{"other": "123"}, "key", 0},
		{"key found valid", map[string]string{"key": "42"}, "key", 42},
		{"key found invalid", map[string]string{"key": "not-a-number"}, "key", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getIntFromStringMetadata(tt.m, tt.key)
			if result != tt.expected {
				t.Fatalf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}
