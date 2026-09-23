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

//go:build rag

package orchestration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/ollama"
)

// The Ollama backend's usage, under the rag tag its real client builds with
// (see ollama_backend_test.go). go-quicbaas#1058.

func TestOllamaBackendPromptReportsItsCounts(t *testing.T) {
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(ollama.ChatResponse{
			Model:           "review-q8:latest",
			Message:         ollama.ChatMessage{Role: ollama.ChatRoleAssistant, Content: "ok"},
			Done:            true,
			PromptEvalCount: 900,
			EvalCount:       40,
		}); err != nil {
			t.Errorf("encode: %v", err)
		}
	}
	backend, server := newMockOllamaBackend(t, handler, "review-q8:latest")
	defer server.Close()

	resp, err := backend.Prompt(context.Background(), &PromptRequest{Prompt: "review"})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if resp.Usage == nil || resp.Usage.InputTokens != 900 || resp.Usage.OutputTokens != 40 {
		t.Fatalf("Usage = %+v, want 900 in / 40 out", resp.Usage)
	}
}

func TestOllamaBackendPromptWithoutCountsReportsNothing(t *testing.T) {
	backend, server := newMockOllamaBackend(t, ollamaChatHandler("ok", "llama3"), "llama3")
	defer server.Close()

	resp, err := backend.Prompt(context.Background(), &PromptRequest{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if resp.Usage != nil {
		t.Fatalf("Usage = %+v, want nil when the server sent no counts", resp.Usage)
	}
}
