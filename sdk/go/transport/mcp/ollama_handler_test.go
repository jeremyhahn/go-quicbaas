// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"context"
	"reflect"
	"testing"
	"time"

	ollamaclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ollama"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

const ollamaStamp = "2026-10-01T12:00:00Z"

var ollamaWhen = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// assertNoOllamaArgs checks a no-argument tool received no arguments.
func assertNoOllamaArgs(t *testing.T, args map[string]interface{}) {
	if len(args) != 0 {
		t.Errorf("tool arguments = %v, want none", args)
	}
}

// TestOllamaMCP_ListModels drives ListModels through the SDK client to the
// quicbaas_ollama_model_list tool and asserts every model field arrives.
func TestOllamaMCP_ListModels(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:   "quicbaas_ollama_model_list",
		assertArgs: assertNoOllamaArgs,
		result: map[string]interface{}{
			"models": []interface{}{
				map[string]interface{}{"name": "llama3", "size": 4096, "digest": "sha256:abc", "modified_at": ollamaStamp},
			},
			"count": 1,
		},
	})
	defer cleanup()

	resp, err := ollamaclient.NewClient(tr).ListModels(context.Background(), &ollamaclient.ListModelsRequest{})
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if resp.Count != 1 || len(resp.Models) != 1 {
		t.Fatalf("response = %+v", resp)
	}
	m := resp.Models[0]
	if m.Name != "llama3" || m.Size != 4096 || m.Digest != "sha256:abc" || !m.ModifiedAt.Equal(ollamaWhen) {
		t.Errorf("model = %+v", m)
	}
}

// TestOllamaMCP_ListModels_MalformedResult checks a result that is not a JSON
// object surfaces as an error rather than an empty list.
func TestOllamaMCP_ListModels_MalformedResult(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_ollama_model_list",
		rawText:  "not json",
	})
	defer cleanup()

	if _, err := ollamaclient.NewClient(tr).ListModels(context.Background(), &ollamaclient.ListModelsRequest{}); err == nil {
		t.Error("ListModels returned no error for a malformed result")
	}
}

// TestOllamaMCP_GetModelInfo drives GetModelInfo to the
// quicbaas_ollama_model_info tool and asserts the model and every detail field.
func TestOllamaMCP_GetModelInfo(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_ollama_model_info",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if !reflect.DeepEqual(args, map[string]interface{}{"model": "llama3"}) {
				t.Errorf("tool arguments = %v", args)
			}
		},
		result: map[string]interface{}{
			"model": "llama3",
			"details": map[string]interface{}{
				"license": "MIT", "format": "gguf", "family": "llama",
				"parameter_size": "8B", "quantization_level": "Q4_0",
			},
		},
	})
	defer cleanup()

	resp, err := ollamaclient.NewClient(tr).GetModelInfo(context.Background(), &ollamaclient.GetModelInfoRequest{Model: "llama3"})
	if err != nil {
		t.Fatalf("GetModelInfo: %v", err)
	}
	want := ollamaclient.ModelDetails{
		License: "MIT", Format: "gguf", Family: "llama", ParameterSize: "8B", QuantizationLevel: "Q4_0",
	}
	if resp.Model != "llama3" || resp.Details != want {
		t.Errorf("response = %+v", resp)
	}
}

// TestOllamaMCP_GetModelInfo_BadRequest checks a wrong request type is
// refused with InvalidInput before the tool is called.
func TestOllamaMCP_GetModelInfo_BadRequest(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_ollama_model_info",
		assertArgs: func(t *testing.T, _ map[string]interface{}) {
			t.Error("a refused request must not reach the tool")
		},
	})
	defer cleanup()

	_, err := tr.Request(context.Background(), ollamaServiceName, "GetModelInfo", &ollamaclient.ListModelsRequest{})
	if !sdkerrors.IsInvalidInput(err) {
		t.Errorf("err = %v, want InvalidInput", err)
	}
}

// TestOllamaMCP_IsAvailable checks IsAvailable is served by the health tool
// and that the typed health result carries every field.
func TestOllamaMCP_IsAvailable(t *testing.T) {
	exp := mcpToolExpectation{
		wantTool:   "quicbaas_ollama_health",
		assertArgs: assertNoOllamaArgs,
		result: map[string]interface{}{
			"available": true, "version": "0.5.1", "base_url": "http://ollama.example:11434",
			"last_checked": ollamaStamp,
		},
	}

	t.Run("client", func(t *testing.T) {
		tr, cleanup := newFakeMCPServer(t, exp)
		defer cleanup()
		if !ollamaclient.NewClient(tr).IsAvailable() {
			t.Error("IsAvailable = false, want true")
		}
	})

	t.Run("fields", func(t *testing.T) {
		tr, cleanup := newFakeMCPServer(t, exp)
		defer cleanup()
		got, err := tr.Request(context.Background(), ollamaServiceName, "IsAvailable", nil)
		if err != nil {
			t.Fatalf("Request: %v", err)
		}
		h, ok := got.(*ollamaclient.HealthResponse)
		if !ok {
			t.Fatalf("result type = %T, want *HealthResponse", got)
		}
		if !h.Available || h.Version != "0.5.1" || h.BaseURL != "http://ollama.example:11434" ||
			h.Error != "" || !h.LastChecked.Equal(ollamaWhen) {
			t.Errorf("health = %+v", h)
		}
	})
}

// TestOllamaMCP_IsAvailable_ToolError checks a failing health tool reports
// Ollama as unavailable.
func TestOllamaMCP_IsAvailable_ToolError(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:  "quicbaas_ollama_health",
		result:    map[string]interface{}{"error": "connection refused"},
		toolError: true,
	})
	defer cleanup()

	if ollamaclient.NewClient(tr).IsAvailable() {
		t.Error("IsAvailable = true for a failing health tool")
	}
}

// TestOllamaMCP_Health drives Health through the SDK client to the
// quicbaas_ollama_health tool and asserts every field the tool returns
// arrives.
func TestOllamaMCP_Health(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:   "quicbaas_ollama_health",
		assertArgs: assertNoOllamaArgs,
		result: map[string]interface{}{
			"available": false, "version": "0.5.1", "base_url": "http://ollama.example:11434",
			"last_checked": ollamaStamp, "error": "model not loaded",
		},
	})
	defer cleanup()

	got, err := ollamaclient.NewClient(tr).Health(context.Background(), &ollamaclient.HealthRequest{})
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	want := &ollamaclient.HealthResponse{
		Available:   false,
		Version:     "0.5.1",
		BaseURL:     "http://ollama.example:11434",
		Error:       "model not loaded",
		LastChecked: ollamaWhen,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("health = %+v, want %+v", got, want)
	}
}

// TestOllamaMCP_Health_ToolError checks a failing health tool surfaces as
// an error, not as a zero-valued health response.
func TestOllamaMCP_Health_ToolError(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:  "quicbaas_ollama_health",
		result:    map[string]interface{}{"error": "connection refused"},
		toolError: true,
	})
	defer cleanup()

	if _, err := ollamaclient.NewClient(tr).Health(context.Background(), &ollamaclient.HealthRequest{}); err == nil {
		t.Error("Health returned no error for a failing tool")
	}
}

// TestOllamaMCP_Embed drives Embed through the SDK client to the
// quicbaas_ollama_embed tool and asserts the argument and every field.
func TestOllamaMCP_Embed(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_ollama_embed",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			want := map[string]interface{}{"text": "hello"}
			if !reflect.DeepEqual(args, want) {
				t.Errorf("tool arguments = %v, want %v", args, want)
			}
		},
		result: map[string]interface{}{
			"embedding":       []interface{}{0.25, -0.5, 1.5},
			"model":           "nomic-embed-text",
			"dimensions":      3,
			"processing_time": "125ms",
		},
	})
	defer cleanup()

	got, err := ollamaclient.NewClient(tr).Embed(context.Background(), &ollamaclient.EmbedRequest{Text: "hello"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	want := &ollamaclient.EmbedResponse{
		Embedding:      []float32{0.25, -0.5, 1.5},
		Model:          "nomic-embed-text",
		Dimensions:     3,
		ProcessingTime: 125 * time.Millisecond,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("embed = %+v, want %+v", got, want)
	}
}

// TestOllamaMCP_Embed_BadRequest checks a request of the wrong type is
// refused before it reaches the tool.
func TestOllamaMCP_Embed_BadRequest(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_ollama_embed",
		assertArgs: func(t *testing.T, _ map[string]interface{}) {
			t.Error("a refused request must not reach the tool")
		},
	})
	defer cleanup()

	_, err := tr.Request(context.Background(), ollamaServiceName, "Embed", &ollamaclient.EmbedBatchRequest{})
	if !sdkerrors.IsInvalidInput(err) {
		t.Errorf("err = %v, want InvalidInput", err)
	}
}

// TestOllamaMCP_EmbedBatch drives EmbedBatch through the SDK client to the
// quicbaas_ollama_embed_batch tool and asserts the JSON-encoded texts
// argument and every field.
func TestOllamaMCP_EmbedBatch(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_ollama_embed_batch",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			want := map[string]interface{}{"texts": `["first","second"]`, "model": "mxbai-embed-large"}
			if !reflect.DeepEqual(args, want) {
				t.Errorf("tool arguments = %v, want %v", args, want)
			}
		},
		result: map[string]interface{}{
			"embeddings": []interface{}{
				[]interface{}{0.25, -0.5},
				[]interface{}{1.5, 2},
			},
			"model":           "mxbai-embed-large",
			"dimensions":      2,
			"count":           2,
			"processing_time": "1.5s",
		},
	})
	defer cleanup()

	got, err := ollamaclient.NewClient(tr).EmbedBatch(context.Background(), &ollamaclient.EmbedBatchRequest{
		Texts: []string{"first", "second"},
		Model: "mxbai-embed-large",
	})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	want := &ollamaclient.EmbedBatchResponse{
		Embeddings:     [][]float32{{0.25, -0.5}, {1.5, 2}},
		Model:          "mxbai-embed-large",
		Dimensions:     2,
		Count:          2,
		ProcessingTime: 1500 * time.Millisecond,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("embed batch = %+v, want %+v", got, want)
	}
}

// TestOllamaMCP_EmbedBatch_BadRequest checks a request of the wrong type is
// refused before it reaches the tool.
func TestOllamaMCP_EmbedBatch_BadRequest(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_ollama_embed_batch",
		assertArgs: func(t *testing.T, _ map[string]interface{}) {
			t.Error("a refused request must not reach the tool")
		},
	})
	defer cleanup()

	_, err := tr.Request(context.Background(), ollamaServiceName, "EmbedBatch", &ollamaclient.EmbedRequest{})
	if !sdkerrors.IsInvalidInput(err) {
		t.Errorf("err = %v, want InvalidInput", err)
	}
}

// TestOllamaMCP_ToolNames pins the method-to-tool map the contract test in
// pkg/api/mcp checks against the server registry.
func TestOllamaMCP_ToolNames(t *testing.T) {
	want := map[string]string{
		"ListModels":   "quicbaas_ollama_model_list",
		"GetModelInfo": "quicbaas_ollama_model_info",
		"IsAvailable":  "quicbaas_ollama_health",
		"Health":       "quicbaas_ollama_health",
		"Embed":        "quicbaas_ollama_embed",
		"EmbedBatch":   "quicbaas_ollama_embed_batch",
	}
	for method, tool := range want {
		if got := ToolName(ollamaServiceName, method); got != tool {
			t.Errorf("ToolName(%q) = %q, want %q", method, got, tool)
		}
	}
}
