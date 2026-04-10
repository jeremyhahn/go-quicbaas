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

//go:build rag

package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/ollama"
)

// newMockOllamaBackend creates an OllamaBackend backed by a mock HTTP server.
// The handler func receives chat requests and returns chat responses.
func newMockOllamaBackend(t *testing.T, handler http.HandlerFunc, defaultModel string) (*OllamaBackend, *httptest.Server) {
	t.Helper()

	server := httptest.NewServer(handler)

	client := ollama.NewClient(ollama.ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 0,
	})

	chatService := ollama.NewChatService(client, defaultModel)

	backend := NewOllamaBackend(OllamaBackendConfig{
		ChatService:  chatService,
		DefaultModel: defaultModel,
	})

	return backend, server
}

// ollamaChatHandler returns an http.HandlerFunc that responds with a valid
// Ollama chat response containing the specified content.
func ollamaChatHandler(content string, model string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		var req ollama.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp := ollama.ChatResponse{
			Model: model,
			Message: ollama.ChatMessage{
				Role:    ollama.ChatRoleAssistant,
				Content: content,
			},
			Done: true,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// ollamaChatHandlerWithSystemCheck returns a handler that verifies a system
// message is present in the request.
func ollamaChatHandlerWithSystemCheck(t *testing.T, content string, model string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		var req ollama.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Verify system message is present
		if len(req.Messages) < 2 {
			t.Errorf("expected at least 2 messages (system + user), got %d", len(req.Messages))
		}
		if req.Messages[0].Role != ollama.ChatRoleSystem {
			t.Errorf("first message role = %q, want %q", req.Messages[0].Role, ollama.ChatRoleSystem)
		}

		resp := ollama.ChatResponse{
			Model: model,
			Message: ollama.ChatMessage{
				Role:    ollama.ChatRoleAssistant,
				Content: content,
			},
			Done: true,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func TestOllamaBackend_Capabilities(t *testing.T) {
	backend := NewOllamaBackend(OllamaBackendConfig{})
	caps := backend.Capabilities()

	if caps.PersistentSessions {
		t.Error("PersistentSessions should be false")
	}
	if caps.ToolUse {
		t.Error("ToolUse should be false")
	}
	if caps.FileAccess {
		t.Error("FileAccess should be false")
	}
	if caps.Streaming {
		t.Error("Streaming should be false")
	}
	if caps.ACP {
		t.Error("ACP should be false")
	}
}

func TestOllamaBackend_Type(t *testing.T) {
	backend := NewOllamaBackend(OllamaBackendConfig{})
	if got := backend.Type(); got != "ollama" {
		t.Errorf("Type() = %q, want %q", got, "ollama")
	}
}

func TestOllamaBackend_Spawn_Unsupported(t *testing.T) {
	backend := NewOllamaBackend(OllamaBackendConfig{})

	ctx := context.Background()
	_, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp"})
	if err == nil {
		t.Fatal("Spawn() should return error")
	}
	if !errors.Is(err, ErrUnsupportedOperation) {
		t.Errorf("error = %v, want ErrUnsupportedOperation", err)
	}

	var target *UnsupportedOperationError
	if !errors.As(err, &target) {
		t.Fatal("error should be UnsupportedOperationError")
	}
	if target.Backend != "ollama" {
		t.Errorf("Backend = %q, want %q", target.Backend, "ollama")
	}
	if target.Operation != "spawn" {
		t.Errorf("Operation = %q, want %q", target.Operation, "spawn")
	}
}

func TestOllamaBackend_Status_Unsupported(t *testing.T) {
	backend := NewOllamaBackend(OllamaBackendConfig{})

	ctx := context.Background()
	_, err := backend.Status(ctx, "any-session")
	if err == nil {
		t.Fatal("Status() should return error")
	}
	if !errors.Is(err, ErrUnsupportedOperation) {
		t.Errorf("error = %v, want ErrUnsupportedOperation", err)
	}

	var target *UnsupportedOperationError
	if !errors.As(err, &target) {
		t.Fatal("error should be UnsupportedOperationError")
	}
	if target.Operation != "status" {
		t.Errorf("Operation = %q, want %q", target.Operation, "status")
	}
}

func TestOllamaBackend_Close_Unsupported(t *testing.T) {
	backend := NewOllamaBackend(OllamaBackendConfig{})

	ctx := context.Background()
	err := backend.Close(ctx, "any-session")
	if err == nil {
		t.Fatal("Close() should return error")
	}
	if !errors.Is(err, ErrUnsupportedOperation) {
		t.Errorf("error = %v, want ErrUnsupportedOperation", err)
	}

	var target *UnsupportedOperationError
	if !errors.As(err, &target) {
		t.Fatal("error should be UnsupportedOperationError")
	}
	if target.Operation != "close" {
		t.Errorf("Operation = %q, want %q", target.Operation, "close")
	}
}

func TestOllamaBackend_Prompt(t *testing.T) {
	backend, server := newMockOllamaBackend(t, ollamaChatHandler("Hello from Ollama", "llama3"), "llama3")
	defer server.Close()

	ctx := context.Background()
	resp, err := backend.Prompt(ctx, &PromptRequest{
		Prompt: "Hello",
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}

	if len(resp.Content) != 1 {
		t.Fatalf("Content length = %d, want 1", len(resp.Content))
	}
	if resp.Content[0].Type != "text" {
		t.Errorf("Content[0].Type = %q, want %q", resp.Content[0].Type, "text")
	}
	if resp.Content[0].Text != "Hello from Ollama" {
		t.Errorf("Content[0].Text = %q, want %q", resp.Content[0].Text, "Hello from Ollama")
	}
	if resp.Model != "llama3" {
		t.Errorf("Model = %q, want %q", resp.Model, "llama3")
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "end_turn")
	}
}

func TestOllamaBackend_Prompt_WithSystemPrompt(t *testing.T) {
	backend, server := newMockOllamaBackend(t,
		ollamaChatHandlerWithSystemCheck(t, "System response", "llama3"),
		"llama3",
	)
	defer server.Close()

	ctx := context.Background()
	resp, err := backend.Prompt(ctx, &PromptRequest{
		Prompt:       "Hello",
		SystemPrompt: "You are a helpful assistant",
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}

	if len(resp.Content) == 0 {
		t.Fatal("Content should not be empty")
	}
	if resp.Content[0].Text != "System response" {
		t.Errorf("Content[0].Text = %q, want %q", resp.Content[0].Text, "System response")
	}
}

func TestOllamaBackend_Prompt_EmptyPrompt(t *testing.T) {
	backend := NewOllamaBackend(OllamaBackendConfig{DefaultModel: "llama3"})

	ctx := context.Background()
	_, err := backend.Prompt(ctx, &PromptRequest{Prompt: ""})
	if err == nil {
		t.Fatal("Prompt with empty prompt should return error")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("error = %v, want ErrInvalidInput", err)
	}

	var target *InputValidationError
	if !errors.As(err, &target) {
		t.Fatal("error should be InputValidationError")
	}
	if target.Field != "prompt" {
		t.Errorf("Field = %q, want %q", target.Field, "prompt")
	}
}

func TestOllamaBackend_Prompt_NilRequest(t *testing.T) {
	backend := NewOllamaBackend(OllamaBackendConfig{DefaultModel: "llama3"})

	ctx := context.Background()
	_, err := backend.Prompt(ctx, nil)
	if err == nil {
		t.Fatal("Prompt(nil) should return error")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("error = %v, want ErrInvalidInput", err)
	}
}

func TestOllamaBackend_Prompt_ModelResolution(t *testing.T) {
	t.Run("UsesRequestModel", func(t *testing.T) {
		handler := func(w http.ResponseWriter, r *http.Request) {
			var req ollama.ChatRequest
			json.NewDecoder(r.Body).Decode(&req)

			if req.Model != "custom-model" {
				t.Errorf("Model = %q, want %q", req.Model, "custom-model")
			}

			resp := ollama.ChatResponse{
				Model: req.Model,
				Message: ollama.ChatMessage{
					Role:    ollama.ChatRoleAssistant,
					Content: "response",
				},
				Done: true,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}

		backend, server := newMockOllamaBackend(t, handler, "default-model")
		defer server.Close()

		ctx := context.Background()
		resp, err := backend.Prompt(ctx, &PromptRequest{
			Prompt: "Hello",
			Model:  "custom-model",
		})
		if err != nil {
			t.Fatalf("Prompt() error = %v", err)
		}
		if resp.Model != "custom-model" {
			t.Errorf("Model = %q, want %q", resp.Model, "custom-model")
		}
	})

	t.Run("FallsBackToDefaultModel", func(t *testing.T) {
		handler := func(w http.ResponseWriter, r *http.Request) {
			var req ollama.ChatRequest
			json.NewDecoder(r.Body).Decode(&req)

			if req.Model != "default-model" {
				t.Errorf("Model = %q, want %q", req.Model, "default-model")
			}

			resp := ollama.ChatResponse{
				Model: req.Model,
				Message: ollama.ChatMessage{
					Role:    ollama.ChatRoleAssistant,
					Content: "response",
				},
				Done: true,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}

		backend, server := newMockOllamaBackend(t, handler, "default-model")
		defer server.Close()

		ctx := context.Background()
		resp, err := backend.Prompt(ctx, &PromptRequest{
			Prompt: "Hello",
		})
		if err != nil {
			t.Fatalf("Prompt() error = %v", err)
		}
		if resp.Model != "default-model" {
			t.Errorf("Model = %q, want %q", resp.Model, "default-model")
		}
	})

	t.Run("NoModelAvailable", func(t *testing.T) {
		backend := NewOllamaBackend(OllamaBackendConfig{
			DefaultModel: "",
		})

		ctx := context.Background()
		_, err := backend.Prompt(ctx, &PromptRequest{
			Prompt: "Hello",
		})
		if err == nil {
			t.Fatal("Prompt with no model should return error")
		}
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("error = %v, want ErrInvalidInput", err)
		}
	})
}

func TestOllamaBackend_Prompt_ServerError(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}

	backend, server := newMockOllamaBackend(t, handler, "llama3")
	defer server.Close()

	ctx := context.Background()
	_, err := backend.Prompt(ctx, &PromptRequest{
		Prompt: "Hello",
	})
	if err == nil {
		t.Fatal("Prompt should return error on server error")
	}
	if !errors.Is(err, ErrPromptFailed) {
		t.Errorf("error = %v, want ErrPromptFailed", err)
	}
}

func TestOllamaBackendCapabilitiesKind(t *testing.T) {
	backend := NewOllamaBackend(OllamaBackendConfig{})
	caps := backend.Capabilities()

	if caps.Kind != BackendKindModelServer {
		t.Errorf("Capabilities().Kind = %q, want %q", caps.Kind, BackendKindModelServer)
	}
}

func TestOllamaBackendCapabilitiesKind_InvalidKind(t *testing.T) {
	backend := NewOllamaBackend(OllamaBackendConfig{})
	caps := backend.Capabilities()

	if caps.Kind == BackendKindAgent {
		t.Error("Ollama backend Kind should not be BackendKindAgent")
	}
	if caps.Kind == "" {
		t.Error("Ollama backend Kind should not be empty")
	}
}

// =============================================================================
// ToolGuidance Tests
// =============================================================================

func TestOllamaBackend_ToolGuidance(t *testing.T) {
	t.Parallel()

	backend := NewOllamaBackend(OllamaBackendConfig{
		DefaultModel: "tinyllama",
	})

	guidance := backend.ToolGuidance()
	if guidance != "" {
		t.Errorf("expected empty guidance for ollama backend, got %q", guidance)
	}
}
