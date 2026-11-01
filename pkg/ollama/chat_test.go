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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewChatService(t *testing.T) {
	client := NewClient(ClientConfig{BaseURL: "http://localhost:11434"})
	defer client.Close()

	svc := NewChatService(client, "llama3")

	if svc.Model() != "llama3" {
		t.Errorf("expected model 'llama3', got %s", svc.Model())
	}

	svc.SetModel("mistral")
	if svc.Model() != "mistral" {
		t.Errorf("expected model 'mistral' after SetModel, got %s", svc.Model())
	}
}

func TestChatService_Chat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("expected path /api/chat, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Model != "llama3" {
			t.Errorf("expected model 'llama3', got %s", req.Model)
		}
		if req.Stream {
			t.Error("expected stream to be false")
		}
		if len(req.Messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(req.Messages))
		}
		if req.Messages[0].Role != ChatRoleUser {
			t.Errorf("expected role 'user', got %s", req.Messages[0].Role)
		}
		if req.Messages[0].Content != "Hello" {
			t.Errorf("expected content 'Hello', got %s", req.Messages[0].Content)
		}

		resp := ChatResponse{
			Model:     "llama3",
			CreatedAt: time.Now().Format(time.RFC3339),
			Message: ChatMessage{
				Role:    ChatRoleAssistant,
				Content: "Hello! How can I help you?",
			},
			Done:          true,
			TotalDuration: 1000000,
			EvalCount:     10,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	defer client.Close()

	svc := NewChatService(client, "llama3")
	ctx := context.Background()

	messages := []ChatMessage{
		{Role: ChatRoleUser, Content: "Hello"},
	}

	resp, err := svc.Chat(ctx, messages, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Message.Content != "Hello! How can I help you?" {
		t.Errorf("expected response content 'Hello! How can I help you?', got %s", resp.Message.Content)
	}
	if resp.Message.Role != ChatRoleAssistant {
		t.Errorf("expected role 'assistant', got %s", resp.Message.Role)
	}
	if resp.Model != "llama3" {
		t.Errorf("expected model 'llama3', got %s", resp.Model)
	}
	if !resp.Done {
		t.Error("expected done to be true")
	}
}

// TestChatService_EmptyInput checks that every method rejects an empty model,
// prompt or message list with ErrInvalidInput before any request is sent.
func TestChatService_EmptyInput(t *testing.T) {
	client := NewClient(ClientConfig{BaseURL: "http://localhost:11434"})
	defer client.Close()

	ctx := context.Background()
	messages := []ChatMessage{{Role: ChatRoleUser, Content: "Hello"}}

	tests := []struct {
		name  string
		model string
		call  func(svc *ChatService) error
	}{
		{"Chat nil messages", "llama3", func(svc *ChatService) error {
			_, err := svc.Chat(ctx, nil, nil)
			return err
		}},
		{"Chat empty messages", "llama3", func(svc *ChatService) error {
			_, err := svc.Chat(ctx, []ChatMessage{}, nil)
			return err
		}},
		{"Chat empty model", "", func(svc *ChatService) error {
			_, err := svc.Chat(ctx, messages, nil)
			return err
		}},
		{"ChatWithModel empty model", "llama3", func(svc *ChatService) error {
			_, err := svc.ChatWithModel(ctx, "", messages, nil)
			return err
		}},
		{"ChatWithModel nil messages", "llama3", func(svc *ChatService) error {
			_, err := svc.ChatWithModel(ctx, "llama3", nil, nil)
			return err
		}},
		{"Generate empty prompt", "llama3", func(svc *ChatService) error {
			_, err := svc.Generate(ctx, "", nil)
			return err
		}},
		{"Generate empty model", "", func(svc *ChatService) error {
			_, err := svc.Generate(ctx, "Hello", nil)
			return err
		}},
		{"GenerateWithModel empty model", "llama3", func(svc *ChatService) error {
			_, err := svc.GenerateWithModel(ctx, "", "Hello", nil)
			return err
		}},
		{"GenerateWithModel empty prompt", "llama3", func(svc *ChatService) error {
			_, err := svc.GenerateWithModel(ctx, "llama3", "", nil)
			return err
		}},
		{"GenerateWithSystem empty prompt", "llama3", func(svc *ChatService) error {
			_, err := svc.GenerateWithSystem(ctx, "", "system prompt", nil)
			return err
		}},
		{"GenerateWithSystem empty model", "", func(svc *ChatService) error {
			_, err := svc.GenerateWithSystem(ctx, "Hello", "system prompt", nil)
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call(NewChatService(client, tt.model))
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("expected ErrInvalidInput, got %v", err)
			}
		})
	}
}

func TestChatService_Chat_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:      server.URL,
		MaxRetries:   0,
		RetryBackoff: 10 * time.Millisecond,
	})
	defer client.Close()

	svc := NewChatService(client, "llama3")
	ctx := context.Background()

	messages := []ChatMessage{
		{Role: ChatRoleUser, Content: "Hello"},
	}

	_, err := svc.Chat(ctx, messages, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrOllamaUnavailable) {
		t.Errorf("expected ErrOllamaUnavailable, got %v", err)
	}
}

func TestChatService_Chat_ModelNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "model 'nonexistent' not found"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 0,
	})
	defer client.Close()

	svc := NewChatService(client, "nonexistent")
	ctx := context.Background()

	messages := []ChatMessage{
		{Role: ChatRoleUser, Content: "Hello"},
	}

	_, err := svc.Chat(ctx, messages, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrModelNotFound) {
		t.Errorf("expected ErrModelNotFound, got %v", err)
	}
}

func TestChatService_Chat_InvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not valid json`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	defer client.Close()

	svc := NewChatService(client, "llama3")
	ctx := context.Background()

	messages := []ChatMessage{
		{Role: ChatRoleUser, Content: "Hello"},
	}

	_, err := svc.Chat(ctx, messages, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got %v", err)
	}
}

func TestChatService_Chat_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ChatResponse{
			Model:     "llama3",
			CreatedAt: time.Now().Format(time.RFC3339),
			Message: ChatMessage{
				Role:    ChatRoleAssistant,
				Content: "",
			},
			Done: true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	defer client.Close()

	svc := NewChatService(client, "llama3")
	ctx := context.Background()

	messages := []ChatMessage{
		{Role: ChatRoleUser, Content: "Hello"},
	}

	_, err := svc.Chat(ctx, messages, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrEmptyResponse) {
		t.Errorf("expected ErrEmptyResponse, got %v", err)
	}
}

func TestChatService_ChatWithModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Model != "mistral" {
			t.Errorf("expected model 'mistral', got %s", req.Model)
		}

		resp := ChatResponse{
			Model:     "mistral",
			CreatedAt: time.Now().Format(time.RFC3339),
			Message: ChatMessage{
				Role:    ChatRoleAssistant,
				Content: "I am Mistral.",
			},
			Done: true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	defer client.Close()

	svc := NewChatService(client, "llama3")
	ctx := context.Background()

	messages := []ChatMessage{
		{Role: ChatRoleUser, Content: "Who are you?"},
	}

	resp, err := svc.ChatWithModel(ctx, "mistral", messages, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Model != "mistral" {
		t.Errorf("expected model 'mistral', got %s", resp.Model)
	}
	if resp.Message.Content != "I am Mistral." {
		t.Errorf("expected 'I am Mistral.', got %s", resp.Message.Content)
	}
}

func TestChatService_Chat_WithOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Options == nil {
			t.Fatal("expected options to be set")
		}
		if req.Options.Temperature != 0.7 {
			t.Errorf("expected temperature 0.7, got %f", req.Options.Temperature)
		}
		if req.Options.TopP != 0.9 {
			t.Errorf("expected top_p 0.9, got %f", req.Options.TopP)
		}

		resp := ChatResponse{
			Model:     "llama3",
			CreatedAt: time.Now().Format(time.RFC3339),
			Message: ChatMessage{
				Role:    ChatRoleAssistant,
				Content: "Response with options.",
			},
			Done: true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	defer client.Close()

	svc := NewChatService(client, "llama3")
	ctx := context.Background()

	messages := []ChatMessage{
		{Role: ChatRoleUser, Content: "Hello"},
	}
	opts := &ModelOptions{
		Temperature: 0.7,
		TopP:        0.9,
	}

	resp, err := svc.Chat(ctx, messages, opts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Message.Content != "Response with options." {
		t.Errorf("expected 'Response with options.', got %s", resp.Message.Content)
	}
}

func TestChatService_Generate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("expected path /api/generate, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var req GenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Model != "llama3" {
			t.Errorf("expected model 'llama3', got %s", req.Model)
		}
		if req.Stream {
			t.Error("expected stream to be false")
		}
		if req.Prompt != "Tell me a joke" {
			t.Errorf("expected prompt 'Tell me a joke', got %s", req.Prompt)
		}
		if req.System != "" {
			t.Errorf("expected empty system prompt, got %s", req.System)
		}

		resp := GenerateResponse{
			Model:         "llama3",
			CreatedAt:     time.Now().Format(time.RFC3339),
			Response:      "Why did the gopher cross the road?",
			Done:          true,
			TotalDuration: 500000,
			EvalCount:     8,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	defer client.Close()

	svc := NewChatService(client, "llama3")
	ctx := context.Background()

	resp, err := svc.Generate(ctx, "Tell me a joke", nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Response != "Why did the gopher cross the road?" {
		t.Errorf("expected 'Why did the gopher cross the road?', got %s", resp.Response)
	}
	if resp.Model != "llama3" {
		t.Errorf("expected model 'llama3', got %s", resp.Model)
	}
	if !resp.Done {
		t.Error("expected done to be true")
	}
}

func TestChatService_Generate_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:      server.URL,
		MaxRetries:   0,
		RetryBackoff: 10 * time.Millisecond,
	})
	defer client.Close()

	svc := NewChatService(client, "llama3")
	ctx := context.Background()

	_, err := svc.Generate(ctx, "Hello", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrOllamaUnavailable) {
		t.Errorf("expected ErrOllamaUnavailable, got %v", err)
	}
}

func TestChatService_GenerateWithModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Model != "codellama" {
			t.Errorf("expected model 'codellama', got %s", req.Model)
		}

		resp := GenerateResponse{
			Model:     "codellama",
			CreatedAt: time.Now().Format(time.RFC3339),
			Response:  "func main() {}",
			Done:      true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	defer client.Close()

	svc := NewChatService(client, "llama3")
	ctx := context.Background()

	resp, err := svc.GenerateWithModel(ctx, "codellama", "Write a main function", nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Model != "codellama" {
		t.Errorf("expected model 'codellama', got %s", resp.Model)
	}
	if resp.Response != "func main() {}" {
		t.Errorf("expected 'func main() {}', got %s", resp.Response)
	}
}

func TestChatService_GenerateWithSystem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Model != "llama3" {
			t.Errorf("expected model 'llama3', got %s", req.Model)
		}
		if req.System != "You are a helpful assistant." {
			t.Errorf("expected system 'You are a helpful assistant.', got %s", req.System)
		}
		if req.Prompt != "What is Go?" {
			t.Errorf("expected prompt 'What is Go?', got %s", req.Prompt)
		}
		if req.Stream {
			t.Error("expected stream to be false")
		}

		resp := GenerateResponse{
			Model:     "llama3",
			CreatedAt: time.Now().Format(time.RFC3339),
			Response:  "Go is a programming language.",
			Done:      true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	defer client.Close()

	svc := NewChatService(client, "llama3")
	ctx := context.Background()

	resp, err := svc.GenerateWithSystem(ctx, "What is Go?", "You are a helpful assistant.", nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Response != "Go is a programming language." {
		t.Errorf("expected 'Go is a programming language.', got %s", resp.Response)
	}
}

func TestChatService_Generate_InvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not valid json`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	defer client.Close()

	svc := NewChatService(client, "llama3")
	ctx := context.Background()

	_, err := svc.Generate(ctx, "Hello", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got %v", err)
	}
}

func TestChatService_Generate_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := GenerateResponse{
			Model:     "llama3",
			CreatedAt: time.Now().Format(time.RFC3339),
			Response:  "",
			Done:      true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	defer client.Close()

	svc := NewChatService(client, "llama3")
	ctx := context.Background()

	_, err := svc.Generate(ctx, "Hello", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrEmptyResponse) {
		t.Errorf("expected ErrEmptyResponse, got %v", err)
	}
}

func TestChatService_Generate_ModelNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "model 'nonexistent' not found"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 0,
	})
	defer client.Close()

	svc := NewChatService(client, "nonexistent")
	ctx := context.Background()

	_, err := svc.Generate(ctx, "Hello", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrModelNotFound) {
		t.Errorf("expected ErrModelNotFound, got %v", err)
	}
}

func TestChatService_Chat_MultipleMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if len(req.Messages) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(req.Messages))
		}
		if req.Messages[0].Role != ChatRoleSystem {
			t.Errorf("expected first message role 'system', got %s", req.Messages[0].Role)
		}
		if req.Messages[1].Role != ChatRoleUser {
			t.Errorf("expected second message role 'user', got %s", req.Messages[1].Role)
		}
		if req.Messages[2].Role != ChatRoleAssistant {
			t.Errorf("expected third message role 'assistant', got %s", req.Messages[2].Role)
		}

		resp := ChatResponse{
			Model:     "llama3",
			CreatedAt: time.Now().Format(time.RFC3339),
			Message: ChatMessage{
				Role:    ChatRoleAssistant,
				Content: "Continued conversation.",
			},
			Done: true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	defer client.Close()

	svc := NewChatService(client, "llama3")
	ctx := context.Background()

	messages := []ChatMessage{
		{Role: ChatRoleSystem, Content: "You are helpful."},
		{Role: ChatRoleUser, Content: "Hello"},
		{Role: ChatRoleAssistant, Content: "Hi there!"},
	}

	resp, err := svc.Chat(ctx, messages, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Message.Content != "Continued conversation." {
		t.Errorf("expected 'Continued conversation.', got %s", resp.Message.Content)
	}
}

func TestChatService_Generate_WithOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Options == nil {
			t.Fatal("expected options to be set")
		}
		if req.Options.Temperature != 0.5 {
			t.Errorf("expected temperature 0.5, got %f", req.Options.Temperature)
		}
		if req.Options.Seed != 42 {
			t.Errorf("expected seed 42, got %d", req.Options.Seed)
		}
		if len(req.Options.Stop) != 1 || req.Options.Stop[0] != "\n" {
			t.Errorf("expected stop [\\n], got %v", req.Options.Stop)
		}

		resp := GenerateResponse{
			Model:     "llama3",
			CreatedAt: time.Now().Format(time.RFC3339),
			Response:  "Deterministic output.",
			Done:      true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	defer client.Close()

	svc := NewChatService(client, "llama3")
	ctx := context.Background()

	opts := &ModelOptions{
		Temperature: 0.5,
		Seed:        42,
		Stop:        []string{"\n"},
	}

	resp, err := svc.Generate(ctx, "Hello", opts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Response != "Deterministic output." {
		t.Errorf("expected 'Deterministic output.', got %s", resp.Response)
	}
}

func TestChatService_GenerateWithSystem_InvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{invalid json`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	defer client.Close()

	svc := NewChatService(client, "llama3")
	ctx := context.Background()

	_, err := svc.GenerateWithSystem(ctx, "Hello", "Be helpful", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got %v", err)
	}
}

func TestChatService_GenerateWithSystem_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := GenerateResponse{
			Model:     "llama3",
			CreatedAt: time.Now().Format(time.RFC3339),
			Response:  "",
			Done:      true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	defer client.Close()

	svc := NewChatService(client, "llama3")
	ctx := context.Background()

	_, err := svc.GenerateWithSystem(ctx, "Hello", "Be helpful", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrEmptyResponse) {
		t.Errorf("expected ErrEmptyResponse, got %v", err)
	}
}
