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

package ollama

import (
	"context"
	"encoding/json"
)

// ChatService provides chat and generation capabilities using the Ollama API.
type ChatService struct {
	client *Client
	model  string
}

// NewChatService creates a new chat service.
func NewChatService(client *Client, model string) *ChatService {
	return &ChatService{
		client: client,
		model:  model,
	}
}

// Model returns the configured chat model.
func (s *ChatService) Model() string {
	return s.model
}

// SetModel changes the chat model.
func (s *ChatService) SetModel(model string) {
	s.model = model
}

// Chat sends a chat completion request using the configured model.
func (s *ChatService) Chat(ctx context.Context, messages []ChatMessage, opts *ModelOptions) (*ChatResponse, error) {
	if len(messages) == 0 {
		return nil, &OllamaSentinelMessageError{Sentinel: ErrInvalidInput, Message: "messages cannot be empty"}
	}

	return s.ChatWithModel(ctx, s.model, messages, opts)
}

// ChatWithModel sends a chat completion request using a specific model.
func (s *ChatService) ChatWithModel(ctx context.Context, model string, messages []ChatMessage, opts *ModelOptions) (*ChatResponse, error) {
	if model == "" {
		return nil, &OllamaSentinelMessageError{Sentinel: ErrInvalidInput, Message: "model cannot be empty"}
	}
	if len(messages) == 0 {
		return nil, &OllamaSentinelMessageError{Sentinel: ErrInvalidInput, Message: "messages cannot be empty"}
	}

	req := ChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   false,
		Options:  opts,
	}

	respBody, err := s.client.doRequest(ctx, "POST", "/api/chat", req)
	if err != nil {
		return nil, err
	}

	var resp ChatResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, &OllamaSentinelCauseError{Sentinel: ErrInvalidResponse, Cause: err}
	}

	if resp.Message.Content == "" {
		return nil, ErrEmptyResponse
	}

	return &resp, nil
}

// Generate sends a text generation request using the configured model.
func (s *ChatService) Generate(ctx context.Context, prompt string, opts *ModelOptions) (*GenerateResponse, error) {
	if prompt == "" {
		return nil, &OllamaSentinelMessageError{Sentinel: ErrInvalidInput, Message: "prompt cannot be empty"}
	}

	return s.GenerateWithModel(ctx, s.model, prompt, opts)
}

// GenerateWithModel sends a text generation request using a specific model.
func (s *ChatService) GenerateWithModel(ctx context.Context, model, prompt string, opts *ModelOptions) (*GenerateResponse, error) {
	if model == "" {
		return nil, &OllamaSentinelMessageError{Sentinel: ErrInvalidInput, Message: "model cannot be empty"}
	}
	if prompt == "" {
		return nil, &OllamaSentinelMessageError{Sentinel: ErrInvalidInput, Message: "prompt cannot be empty"}
	}

	req := GenerateRequest{
		Model:   model,
		Prompt:  prompt,
		Stream:  false,
		Options: opts,
	}

	respBody, err := s.client.doRequest(ctx, "POST", "/api/generate", req)
	if err != nil {
		return nil, err
	}

	var resp GenerateResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, &OllamaSentinelCauseError{Sentinel: ErrInvalidResponse, Cause: err}
	}

	if resp.Response == "" {
		return nil, ErrEmptyResponse
	}

	return &resp, nil
}

// GenerateWithSystem sends a text generation request with a system prompt.
func (s *ChatService) GenerateWithSystem(ctx context.Context, prompt, system string, opts *ModelOptions) (*GenerateResponse, error) {
	if prompt == "" {
		return nil, &OllamaSentinelMessageError{Sentinel: ErrInvalidInput, Message: "prompt cannot be empty"}
	}

	model := s.model
	if model == "" {
		return nil, &OllamaSentinelMessageError{Sentinel: ErrInvalidInput, Message: "model cannot be empty"}
	}

	req := GenerateRequest{
		Model:   model,
		Prompt:  prompt,
		System:  system,
		Stream:  false,
		Options: opts,
	}

	respBody, err := s.client.doRequest(ctx, "POST", "/api/generate", req)
	if err != nil {
		return nil, err
	}

	var resp GenerateResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, &OllamaSentinelCauseError{Sentinel: ErrInvalidResponse, Cause: err}
	}

	if resp.Response == "" {
		return nil, ErrEmptyResponse
	}

	return &resp, nil
}
