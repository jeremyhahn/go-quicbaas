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

func TestNewModelService(t *testing.T) {
	client := NewClient(DefaultClientConfig())
	service := NewModelService(client)

	if service == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestModelService_List_Success(t *testing.T) {
	expectedModels := []ModelInfo{
		{
			Name:       "nomic-embed-text",
			ModifiedAt: time.Now(),
			Size:       1000000,
			Digest:     "sha256:abc123",
		},
		{
			Name:       "llama3.2",
			ModifiedAt: time.Now(),
			Size:       5000000,
			Digest:     "sha256:def456",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("expected path /api/tags, got %s", r.URL.Path)
		}

		if r.Method != "GET" {
			t.Errorf("expected GET method, got %s", r.Method)
		}

		resp := ListModelsResponse{Models: expectedModels}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	service := NewModelService(client)

	models, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(models) != len(expectedModels) {
		t.Fatalf("expected %d models, got %d", len(expectedModels), len(models))
	}

	for i, m := range models {
		if m.Name != expectedModels[i].Name {
			t.Errorf("expected model name %s, got %s", expectedModels[i].Name, m.Name)
		}
	}
}

func TestModelService_List_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ListModelsResponse{Models: []ModelInfo{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	service := NewModelService(client)

	models, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(models) != 0 {
		t.Errorf("expected 0 models, got %d", len(models))
	}
}

func TestModelService_Show_Success(t *testing.T) {
	expectedDetails := ModelDetails{
		License:   "MIT",
		Modelfile: "FROM nomic-embed-text",
		Details: Details{
			Format:            "gguf",
			Family:            "nomic",
			ParameterSize:     "137M",
			QuantizationLevel: "Q4_0",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" {
			t.Errorf("expected path /api/show, got %s", r.URL.Path)
		}

		if r.Method != "POST" {
			t.Errorf("expected POST method, got %s", r.Method)
		}

		var req ShowModelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Name != "nomic-embed-text" {
			t.Errorf("expected model name 'nomic-embed-text', got %s", req.Name)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expectedDetails)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	service := NewModelService(client)

	details, err := service.Show(context.Background(), "nomic-embed-text")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if details.License != expectedDetails.License {
		t.Errorf("expected license %s, got %s", expectedDetails.License, details.License)
	}

	if details.Details.ParameterSize != expectedDetails.Details.ParameterSize {
		t.Errorf("expected parameter size %s, got %s", expectedDetails.Details.ParameterSize, details.Details.ParameterSize)
	}
}

func TestModelService_Show_EmptyModel(t *testing.T) {
	client := NewClient(DefaultClientConfig())
	service := NewModelService(client)

	_, err := service.Show(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty model name")
	}

	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestModelService_IsAvailable_True(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ListModelsResponse{
			Models: []ModelInfo{
				{Name: "nomic-embed-text"},
				{Name: "llama3.2"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	service := NewModelService(client)

	available, err := service.IsAvailable(context.Background(), "nomic-embed-text")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !available {
		t.Error("expected model to be available")
	}
}

func TestModelService_IsAvailable_False(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ListModelsResponse{
			Models: []ModelInfo{
				{Name: "other-model"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	service := NewModelService(client)

	available, err := service.IsAvailable(context.Background(), "nomic-embed-text")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if available {
		t.Error("expected model to not be available")
	}
}

func TestModelService_IsAvailable_EmptyModel(t *testing.T) {
	client := NewClient(DefaultClientConfig())
	service := NewModelService(client)

	_, err := service.IsAvailable(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty model name")
	}

	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestModelService_GetModelInfo_Success(t *testing.T) {
	expectedModel := ModelInfo{
		Name:   "nomic-embed-text",
		Size:   1000000,
		Digest: "sha256:abc123",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ListModelsResponse{
			Models: []ModelInfo{
				{Name: "other-model", Size: 500000, Digest: "sha256:000"},
				expectedModel,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	service := NewModelService(client)

	info, err := service.GetModelInfo(context.Background(), "nomic-embed-text")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if info.Name != expectedModel.Name {
		t.Errorf("expected name %s, got %s", expectedModel.Name, info.Name)
	}

	if info.Size != expectedModel.Size {
		t.Errorf("expected size %d, got %d", expectedModel.Size, info.Size)
	}
}

func TestModelService_GetModelInfo_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ListModelsResponse{
			Models: []ModelInfo{
				{Name: "other-model"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	service := NewModelService(client)

	_, err := service.GetModelInfo(context.Background(), "nomic-embed-text")
	if err == nil {
		t.Fatal("expected error for model not found")
	}

	if !errors.Is(err, ErrModelNotFound) {
		t.Errorf("expected ErrModelNotFound, got %v", err)
	}
}

func TestModelService_GetModelInfo_EmptyModel(t *testing.T) {
	client := NewClient(DefaultClientConfig())
	service := NewModelService(client)

	_, err := service.GetModelInfo(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty model name")
	}

	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}
