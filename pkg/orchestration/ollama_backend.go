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

	"github.com/jeremyhahn/go-quicbaas/pkg/ollama"
)

// OllamaBackendConfig holds configuration for the Ollama backend.
type OllamaBackendConfig struct {
	ChatService  *ollama.ChatService
	ModelService *ollama.ModelService
	DefaultModel string
}

// OllamaBackend implements AgentBackend for Ollama LLMs. It is stateless
// and does not support persistent sessions.
type OllamaBackend struct {
	chatService  *ollama.ChatService
	modelService *ollama.ModelService
	defaultModel string
}

// Compile-time interface checks.
var _ AgentBackend = (*OllamaBackend)(nil)
var _ ModelLister = (*OllamaBackend)(nil)

// NewOllamaBackend creates a new Ollama backend with the given configuration.
func NewOllamaBackend(config OllamaBackendConfig) *OllamaBackend {
	return &OllamaBackend{
		chatService:  config.ChatService,
		modelService: config.ModelService,
		defaultModel: config.DefaultModel,
	}
}

// Spawn returns an UnsupportedOperationError because Ollama does not support
// persistent sessions.
func (b *OllamaBackend) Spawn(ctx context.Context, req *SpawnRequest) (*Session, error) {
	return nil, &UnsupportedOperationError{Backend: b.Type(), Operation: "spawn"}
}

// Prompt sends a chat request to Ollama. The SessionID field is ignored.
// Model resolution order: req.Model, then b.defaultModel.
func (b *OllamaBackend) Prompt(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
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

	var messages []ollama.ChatMessage

	if req.SystemPrompt != "" {
		messages = append(messages, ollama.ChatMessage{
			Role:    ollama.ChatRoleSystem,
			Content: req.SystemPrompt,
		})
	}

	messages = append(messages, ollama.ChatMessage{
		Role:    ollama.ChatRoleUser,
		Content: req.Prompt,
	})

	resp, err := b.chatService.ChatWithModel(ctx, model, messages, nil)
	if err != nil {
		return nil, &PromptError{SessionID: "", Cause: err}
	}

	// ToolUse stays nil: the chat call offers the model no tools, and this
	// backend parses no tool calls out of the reply.
	return &PromptResponse{
		Content: []ContentBlock{
			{
				Type: "text",
				Text: resp.Message.Content,
			},
		},
		Model:      resp.Model,
		StopReason: StopReasonEndTurn,
		Usage:      countedUsage(int64(resp.PromptEvalCount), int64(resp.EvalCount)),
	}, nil
}

// countedUsage is the usage of a model server that reports a prompt count and
// a generation count and nothing about caching.
//
// BOTH ZERO IS "REPORTED NOTHING", not "cost nothing". A turn that produced
// an answer generated tokens, so a response carrying no counts at all is one
// whose server left them out — Ollama omits them on some paths, and an
// OpenAI-compatible provider may omit usage entirely. Recording that as a
// measured zero would make the model look free.
func countedUsage(prompt, generated int64) *TokenUsage {
	if prompt == 0 && generated == 0 {
		return nil
	}
	usage := &TokenUsage{InputTokens: prompt, OutputTokens: generated}
	usage.normalize()
	return usage
}

// Status returns an UnsupportedOperationError because Ollama does not support
// persistent sessions.
func (b *OllamaBackend) Status(ctx context.Context, sessionID string) (*SessionStatus, error) {
	return nil, &UnsupportedOperationError{Backend: b.Type(), Operation: "status"}
}

// Close returns an UnsupportedOperationError because Ollama does not support
// persistent sessions.
func (b *OllamaBackend) Close(ctx context.Context, sessionID string) error {
	return &UnsupportedOperationError{Backend: b.Type(), Operation: "close"}
}

// Capabilities returns the capabilities of the Ollama backend.
func (b *OllamaBackend) Capabilities() BackendCapabilities {
	return BackendCapabilities{
		Kind:               BackendKindModelServer,
		PersistentSessions: false,
		ToolUse:            false,
		FileAccess:         false,
		Streaming:          false,
		ACP:                false,
	}
}

// Type returns the backend type identifier.
func (b *OllamaBackend) Type() string {
	return "ollama"
}

// ToolGuidance returns empty string. Ollama is a model server, not an agent,
// so it has no tool-specific schema requirements.
func (b *OllamaBackend) ToolGuidance() string {
	return ""
}

// ListModels returns the names of locally-pulled Ollama models by querying
// the agent's local /api/tags endpoint. Returns an empty slice (not an error)
// when no model service is configured.
func (b *OllamaBackend) ListModels(ctx context.Context) ([]string, error) {
	if b.modelService == nil {
		return []string{}, nil
	}

	infos, err := b.modelService.List(ctx)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(infos))
	for _, m := range infos {
		names = append(names, m.Name)
	}
	return names, nil
}
