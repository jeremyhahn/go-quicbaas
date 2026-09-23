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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/inference"
)

// APIInferenceBackend adapts an inference.APIInferenceBackend (the generic,
// provider-agnostic HTTP-inference contract) into the orchestrator's
// AgentBackend + ModelLister interfaces. This is the single seam that lets
// the orchestrator route to API-based providers (Anthropic, OpenAI, xAI,
// ...) without knowing anything about their wire formats.
//
// The wrapped backend is constructed via the inference package's Registry
// (see inference.Create) and registered with the orchestrator via
// AgentService.RegisterAPIInferenceBackend.
//
// Session-related operations (Spawn, Status, Close-by-session) return
// UnsupportedOperationError because API inference is stateless — sessions
// belong to ACP backends.
type APIInferenceBackend struct {
	wrapped      inference.APIInferenceBackend
	defaultModel string
}

// Compile-time interface checks: this adapter satisfies both AgentBackend
// (so the orchestrator can register and dispatch it) and ModelLister (so
// ListBackendModels works uniformly across all backend categories).
var _ AgentBackend = (*APIInferenceBackend)(nil)
var _ ModelLister = (*APIInferenceBackend)(nil)

// APIInferenceBackendConfig configures an APIInferenceBackend adapter.
type APIInferenceBackendConfig struct {
	// Wrapped is the underlying provider-specific inference backend.
	// Required.
	Wrapped inference.APIInferenceBackend

	// DefaultModel is used when a PromptRequest does not specify a model.
	// Optional.
	DefaultModel string
}

// NewAPIInferenceBackend wraps an inference.APIInferenceBackend so it can be
// registered with the orchestrator. Returns an InputValidationError when
// cfg.Wrapped is nil.
func NewAPIInferenceBackend(cfg APIInferenceBackendConfig) (*APIInferenceBackend, error) {
	if cfg.Wrapped == nil {
		return nil, &InputValidationError{Field: "wrapped", Message: "must not be nil"}
	}
	return &APIInferenceBackend{
		wrapped:      cfg.Wrapped,
		defaultModel: cfg.DefaultModel,
	}, nil
}

// Wrapped returns the underlying inference.APIInferenceBackend. Useful for
// callers (e.g. streaming inference paths) that need the richer streaming /
// tool-use surface that AgentBackend's Prompt does not expose.
func (b *APIInferenceBackend) Wrapped() inference.APIInferenceBackend {
	return b.wrapped
}

// Spawn returns UnsupportedOperationError. API inference is stateless and
// has no session concept.
func (b *APIInferenceBackend) Spawn(ctx context.Context, req *SpawnRequest) (*Session, error) {
	return nil, &UnsupportedOperationError{Backend: b.Type(), Operation: "spawn"}
}

// Prompt sends a single prompt to the wrapped provider via Generate and
// returns the assistant's reply. Streaming consumers should use Wrapped()
// to access the richer Stream API directly.
func (b *APIInferenceBackend) Prompt(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
	if req == nil {
		return nil, &InputValidationError{Field: "request", Message: "cannot be nil"}
	}
	if req.Prompt == "" {
		return nil, &InputValidationError{Field: "prompt", Message: "cannot be empty"}
	}

	model := req.Model
	if model == "" {
		model = b.defaultModel
	}
	if model == "" {
		return nil, &InputValidationError{Field: "model", Message: "no model specified and no default configured"}
	}

	messages := make([]inference.Message, 0, 2)
	if req.SystemPrompt != "" {
		messages = append(messages, inference.Message{
			Role:    inference.RoleSystem,
			Content: req.SystemPrompt,
		})
	}
	messages = append(messages, inference.Message{
		Role:    inference.RoleUser,
		Content: req.Prompt,
	})

	resp, err := b.wrapped.Generate(ctx, inference.GenerateRequest{
		Model:    model,
		Messages: messages,
	})
	if err != nil {
		return nil, &PromptError{SessionID: "", Cause: err}
	}

	return &PromptResponse{
		Content: []ContentBlock{{
			Type: "text",
			Text: resp.Content,
		}},
		Model:      resp.Model,
		StopReason: string(resp.FinishReason),
		Usage:      countedUsage(int64(resp.Usage.PromptTokens), int64(resp.Usage.CompletionTokens)),
	}, nil
}

// Status returns UnsupportedOperationError. API inference is stateless.
func (b *APIInferenceBackend) Status(ctx context.Context, sessionID string) (*SessionStatus, error) {
	return nil, &UnsupportedOperationError{Backend: b.Type(), Operation: "status"}
}

// Close releases the underlying provider client. It does NOT take a
// sessionID because API inference has no sessions; the AgentBackend
// signature is satisfied by ignoring the parameter and forwarding to the
// wrapped backend's Close.
func (b *APIInferenceBackend) Close(ctx context.Context, sessionID string) error {
	if sessionID != "" {
		// The orchestrator never assigns a sessionID for stateless backends;
		// if one was passed, it is a programming error in the caller.
		return &UnsupportedOperationError{Backend: b.Type(), Operation: "close-by-session"}
	}
	return b.wrapped.Close()
}

// Capabilities reports the static capability surface of an API inference
// backend: stateless, streaming-capable, no file access.
func (b *APIInferenceBackend) Capabilities() BackendCapabilities {
	return BackendCapabilities{
		Kind:               BackendKindAPIInference,
		PersistentSessions: false,
		ToolUse:            true, // future: gated by Model.Capabilities
		FileAccess:         false,
		Streaming:          true,
		ACP:                false,
	}
}

// Type returns the provider identifier from the wrapped backend (e.g.
// "anthropic", "openai"). The provider name is the canonical Type string
// the orchestrator advertises to UIs and routers.
func (b *APIInferenceBackend) Type() string {
	return b.wrapped.Provider()
}

// ToolGuidance returns empty string. Provider-specific tool schema guidance
// is delivered via the structured Tool type in inference.GenerateRequest,
// not via free-form prompt suffixes.
func (b *APIInferenceBackend) ToolGuidance() string {
	return ""
}

// ListModels returns the model IDs advertised by the wrapped provider. It
// projects inference.Model.ID into the AgentBackend ModelLister contract,
// which is plain []string. Returns an empty slice (not an error) when the
// wrapped backend reports no models — the UI uses that to show a "no
// provider configured" hint instead of greying the backend out.
func (b *APIInferenceBackend) ListModels(ctx context.Context) ([]string, error) {
	models, err := b.wrapped.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, m.ID)
	}
	return out, nil
}
