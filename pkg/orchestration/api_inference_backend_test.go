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

package orchestration

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/inference"
)

// stubInferenceBackend is an in-test implementation of
// inference.APIInferenceBackend.
type stubInferenceBackend struct {
	provider string
	baseURL  string
	models   []inference.Model
	resp     *inference.GenerateResponse
	genErr   error
}

func (s *stubInferenceBackend) Provider() string { return s.provider }
func (s *stubInferenceBackend) BaseURL() string  { return s.baseURL }

func (s *stubInferenceBackend) ListModels(ctx context.Context) ([]inference.Model, error) {
	return s.models, nil
}

func (s *stubInferenceBackend) Generate(ctx context.Context, req inference.GenerateRequest) (*inference.GenerateResponse, error) {
	if s.genErr != nil {
		return nil, s.genErr
	}
	if s.resp != nil {
		return s.resp, nil
	}
	return &inference.GenerateResponse{
		Content:      "ok",
		Model:        req.Model,
		FinishReason: inference.FinishReasonStop,
	}, nil
}

func (s *stubInferenceBackend) Stream(ctx context.Context, req inference.GenerateRequest) (<-chan inference.GenerateChunk, <-chan error) {
	chunks := make(chan inference.GenerateChunk)
	errs := make(chan error)
	close(chunks)
	close(errs)
	return chunks, errs
}

func (s *stubInferenceBackend) Close() error { return nil }

func TestNewAPIInferenceBackend_NilWrappedFails(t *testing.T) {
	t.Parallel()

	_, err := NewAPIInferenceBackend(APIInferenceBackendConfig{Wrapped: nil})
	if err == nil {
		t.Fatal("expected error for nil wrapped, got nil")
	}
	var ive *InputValidationError
	if !errors.As(err, &ive) {
		t.Errorf("expected *InputValidationError, got %T", err)
	}
}

func TestAPIInferenceBackend_TypeIsProvider(t *testing.T) {
	t.Parallel()

	be, err := NewAPIInferenceBackend(APIInferenceBackendConfig{
		Wrapped: &stubInferenceBackend{provider: "anthropic"},
	})
	if err != nil {
		t.Fatalf("NewAPIInferenceBackend failed: %v", err)
	}
	if be.Type() != "anthropic" {
		t.Errorf("Type() = %q, want %q", be.Type(), "anthropic")
	}
}

func TestAPIInferenceBackend_CapabilitiesKindIsAPIInference(t *testing.T) {
	t.Parallel()

	be, err := NewAPIInferenceBackend(APIInferenceBackendConfig{
		Wrapped: &stubInferenceBackend{provider: "openai"},
	})
	if err != nil {
		t.Fatalf("NewAPIInferenceBackend failed: %v", err)
	}
	caps := be.Capabilities()
	if caps.Kind != BackendKindAPIInference {
		t.Errorf("Kind = %q, want %q", caps.Kind, BackendKindAPIInference)
	}
	if !caps.Streaming {
		t.Error("API inference backends must report Streaming=true")
	}
	if caps.PersistentSessions {
		t.Error("API inference backends must report PersistentSessions=false")
	}
	if caps.ACP {
		t.Error("API inference backends must report ACP=false")
	}
}

func TestAPIInferenceBackend_PromptDispatches(t *testing.T) {
	t.Parallel()

	be, err := NewAPIInferenceBackend(APIInferenceBackendConfig{
		Wrapped: &stubInferenceBackend{
			provider: "anthropic",
			resp: &inference.GenerateResponse{
				Content:      "hi there",
				Model:        "claude-3-5-sonnet",
				FinishReason: inference.FinishReasonStop,
			},
		},
		DefaultModel: "claude-3-5-sonnet",
	})
	if err != nil {
		t.Fatalf("NewAPIInferenceBackend failed: %v", err)
	}

	resp, err := be.Prompt(context.Background(), &PromptRequest{Prompt: "hello"})
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "hi there" {
		t.Errorf("Content = %+v, want one text block 'hi there'", resp.Content)
	}
	if resp.Model != "claude-3-5-sonnet" {
		t.Errorf("Model = %q", resp.Model)
	}
	if resp.StopReason != "stop" {
		t.Errorf("StopReason = %q, want stop", resp.StopReason)
	}
}

func TestAPIInferenceBackend_PromptValidates(t *testing.T) {
	t.Parallel()

	be, _ := NewAPIInferenceBackend(APIInferenceBackendConfig{
		Wrapped: &stubInferenceBackend{provider: "anthropic"},
	})

	if _, err := be.Prompt(context.Background(), nil); err == nil {
		t.Error("expected error for nil request")
	}
	if _, err := be.Prompt(context.Background(), &PromptRequest{Prompt: ""}); err == nil {
		t.Error("expected error for empty prompt")
	}
	if _, err := be.Prompt(context.Background(), &PromptRequest{Prompt: "x"}); err == nil {
		t.Error("expected error when no model and no default")
	}
}

func TestAPIInferenceBackend_SpawnAndStatusUnsupported(t *testing.T) {
	t.Parallel()

	be, _ := NewAPIInferenceBackend(APIInferenceBackendConfig{
		Wrapped: &stubInferenceBackend{provider: "openai"},
	})

	if _, err := be.Spawn(context.Background(), &SpawnRequest{}); err == nil {
		t.Error("Spawn must return UnsupportedOperationError")
	} else {
		var u *UnsupportedOperationError
		if !errors.As(err, &u) {
			t.Errorf("expected *UnsupportedOperationError, got %T", err)
		}
	}

	if _, err := be.Status(context.Background(), "any"); err == nil {
		t.Error("Status must return UnsupportedOperationError")
	}
}

func TestAPIInferenceBackend_CloseClosesWrapped(t *testing.T) {
	t.Parallel()

	stub := &stubInferenceBackend{provider: "openai"}
	be, _ := NewAPIInferenceBackend(APIInferenceBackendConfig{Wrapped: stub})

	if err := be.Close(context.Background(), ""); err != nil {
		t.Errorf("Close(no session) should succeed, got %v", err)
	}
	if err := be.Close(context.Background(), "some-session"); err == nil {
		t.Error("Close with sessionID must reject — API inference is sessionless")
	}
}

func TestAPIInferenceBackend_ListModelsProjectsToStrings(t *testing.T) {
	t.Parallel()

	be, _ := NewAPIInferenceBackend(APIInferenceBackendConfig{
		Wrapped: &stubInferenceBackend{
			provider: "anthropic",
			models: []inference.Model{
				{ID: "claude-3-5-sonnet", Name: "Claude 3.5 Sonnet"},
				{ID: "claude-3-5-haiku", Name: "Claude 3.5 Haiku"},
			},
		},
	})

	got, err := be.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	want := []string{"claude-3-5-sonnet", "claude-3-5-haiku"}
	if len(got) != len(want) {
		t.Fatalf("got %d models, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("models[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAPIInferenceBackend_ListModelsEmptyMeansNoConfigured(t *testing.T) {
	t.Parallel()

	// Frontend hook: when ListModels returns empty (not error) the UI shows
	// "no provider configured" instead of greying out.
	be, _ := NewAPIInferenceBackend(APIInferenceBackendConfig{
		Wrapped: &stubInferenceBackend{provider: "openai", models: nil},
	})

	got, err := be.ListModels(context.Background())
	if err != nil {
		t.Errorf("ListModels should not error, got %v", err)
	}
	if got == nil {
		t.Error("ListModels must return non-nil slice (even when empty)")
	}
	if len(got) != 0 {
		t.Errorf("ListModels = %v, want empty", got)
	}
}
