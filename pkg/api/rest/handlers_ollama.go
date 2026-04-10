//go:build rest

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

package rest

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ========================================
// Ollama Route Registration
// ========================================

// registerOllamaRoutes registers all Ollama-related routes.
func registerOllamaRoutes(s *Server, r chi.Router) {
	r.Route("/ollama", func(r chi.Router) {
		// Health check
		r.Get("/health", s.HandleOllamaHealth)

		// Model management
		r.Get("/models", s.HandleOllamaModelList)
		r.Get("/models/{model}", s.HandleOllamaModelInfo)

		// Embedding operations
		r.Post("/embed", s.HandleOllamaEmbed)
		r.Post("/embed/batch", s.HandleOllamaEmbedBatch)
	})
}

// ========================================
// Request/Response Types
// ========================================

// OllamaEmbedRequest is the request body for embedding generation.
type OllamaEmbedRequest struct {
	Text  string `json:"text"`
	Model string `json:"model,omitempty"`
}

// OllamaEmbedResponse is the response for embedding generation.
type OllamaEmbedResponse struct {
	Embedding      []float32 `json:"embedding"`
	Model          string    `json:"model"`
	Dimensions     int       `json:"dimensions"`
	ProcessingTime string    `json:"processing_time"`
}

// OllamaEmbedBatchRequest is the request body for batch embedding generation.
type OllamaEmbedBatchRequest struct {
	Texts []string `json:"texts"`
	Model string   `json:"model,omitempty"`
}

// OllamaEmbedBatchResponse is the response for batch embedding generation.
type OllamaEmbedBatchResponse struct {
	Embeddings     [][]float32 `json:"embeddings"`
	Model          string      `json:"model"`
	Dimensions     int         `json:"dimensions"`
	Count          int         `json:"count"`
	ProcessingTime string      `json:"processing_time"`
}

// OllamaModelListResponse is the response for listing models.
type OllamaModelListResponse struct {
	Models []OllamaModelInfo `json:"models"`
	Count  int               `json:"count"`
}

// OllamaModelInfo contains basic model information.
type OllamaModelInfo struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Digest     string `json:"digest"`
	ModifiedAt string `json:"modified_at"`
}

// OllamaModelDetailsResponse is the response for model details.
type OllamaModelDetailsResponse struct {
	Model   string                 `json:"model"`
	Details OllamaModelDetailsInfo `json:"details"`
}

// OllamaModelDetailsInfo contains detailed model information.
type OllamaModelDetailsInfo struct {
	License           string `json:"license"`
	Format            string `json:"format"`
	Family            string `json:"family"`
	ParameterSize     string `json:"parameter_size"`
	QuantizationLevel string `json:"quantization_level"`
}

// OllamaHealthResponse is the response for health check.
type OllamaHealthResponse struct {
	Available   bool   `json:"available"`
	Version     string `json:"version,omitempty"`
	BaseURL     string `json:"base_url"`
	LastChecked string `json:"last_checked"`
	Error       string `json:"error,omitempty"`
}

// ========================================
// Health Handler
// ========================================

// HandleOllamaHealth returns the health status of the Ollama service.
func (s *Server) HandleOllamaHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ollamaSvc := s.service.Ollama()
	if ollamaSvc == nil {
		RespondJSON(w, r, http.StatusOK, OllamaHealthResponse{
			Available: false,
			Error:     "ollama service not configured",
		})
		return
	}

	healthResp, err := ollamaSvc.Health(ctx, &service.OllamaHealthRequest{})
	if err != nil {
		RespondJSON(w, r, http.StatusOK, OllamaHealthResponse{
			Available: false,
			BaseURL:   ollamaSvc.BaseURL(),
			Error:     err.Error(),
		})
		return
	}

	resp := OllamaHealthResponse{
		Available:   healthResp.Available,
		Version:     healthResp.Version,
		BaseURL:     healthResp.BaseURL,
		LastChecked: healthResp.LastChecked.Format("2006-01-02T15:04:05Z07:00"),
	}

	if healthResp.Error != "" {
		resp.Error = healthResp.Error
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// ========================================
// Model Handlers
// ========================================

// HandleOllamaModelList returns the list of available models.
func (s *Server) HandleOllamaModelList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ollamaSvc := s.service.Ollama()
	if ollamaSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ValidationError{
			Message: "ollama service not configured",
		})
		return
	}

	listResp, err := ollamaSvc.ListModels(ctx, &service.OllamaListModelsRequest{})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	models := make([]OllamaModelInfo, len(listResp.Models))
	for i, m := range listResp.Models {
		models[i] = OllamaModelInfo{
			Name:       m.Name,
			Size:       m.Size,
			Digest:     m.Digest,
			ModifiedAt: m.ModifiedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}

	RespondJSON(w, r, http.StatusOK, OllamaModelListResponse{
		Models: models,
		Count:  listResp.Count,
	})
}

// HandleOllamaModelInfo returns detailed information about a specific model.
func (s *Server) HandleOllamaModelInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	model := chi.URLParam(r, "model")
	if model == "" {
		RespondBadRequest(w, r, "model is required")
		return
	}

	ollamaSvc := s.service.Ollama()
	if ollamaSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ValidationError{
			Message: "ollama service not configured",
		})
		return
	}

	infoResp, err := ollamaSvc.GetModelInfo(ctx, &service.OllamaGetModelInfoRequest{
		Model: model,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, OllamaModelDetailsResponse{
		Model: infoResp.Model,
		Details: OllamaModelDetailsInfo{
			License:           infoResp.Details.License,
			Format:            infoResp.Details.Format,
			Family:            infoResp.Details.Family,
			ParameterSize:     infoResp.Details.ParameterSize,
			QuantizationLevel: infoResp.Details.QuantizationLevel,
		},
	})
}

// ========================================
// Embedding Handlers
// ========================================

// HandleOllamaEmbed generates an embedding for a single text.
func (s *Server) HandleOllamaEmbed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request body
	var req OllamaEmbedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if req.Text == "" {
		RespondBadRequest(w, r, "text is required")
		return
	}

	ollamaSvc := s.service.Ollama()
	if ollamaSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ValidationError{
			Message: "ollama service not configured",
		})
		return
	}

	var embedResp *service.OllamaEmbedResponse
	var err error

	if req.Model != "" {
		embedResp, err = ollamaSvc.EmbedWithModel(ctx, &service.OllamaEmbedWithModelRequest{
			Text:  req.Text,
			Model: req.Model,
		})
	} else {
		embedResp, err = ollamaSvc.Embed(ctx, &service.OllamaEmbedRequest{
			Text: req.Text,
		})
	}

	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, OllamaEmbedResponse{
		Embedding:      embedResp.Embedding,
		Model:          embedResp.Model,
		Dimensions:     embedResp.Dimensions,
		ProcessingTime: embedResp.ProcessingTime.String(),
	})
}

// HandleOllamaEmbedBatch generates embeddings for multiple texts.
func (s *Server) HandleOllamaEmbedBatch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request body
	var req OllamaEmbedBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if len(req.Texts) == 0 {
		RespondBadRequest(w, r, "texts is required and cannot be empty")
		return
	}

	ollamaSvc := s.service.Ollama()
	if ollamaSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ValidationError{
			Message: "ollama service not configured",
		})
		return
	}

	embedResp, err := ollamaSvc.EmbedBatch(ctx, &service.OllamaEmbedBatchRequest{
		Texts: req.Texts,
		Model: req.Model,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, OllamaEmbedBatchResponse{
		Embeddings:     embedResp.Embeddings,
		Model:          embedResp.Model,
		Dimensions:     embedResp.Dimensions,
		Count:          embedResp.Count,
		ProcessingTime: embedResp.ProcessingTime.String(),
	})
}
