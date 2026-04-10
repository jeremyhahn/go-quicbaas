//go:build mcp

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

// Package mcp implements Ollama MCP integration handlers.
package mcp

import (
	"context"
	"encoding/json"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// registerOllamaTools registers all Ollama tool handlers on the ToolRegistry.
func (r *ToolRegistry) registerOllamaTools() {
	ollamaTools := getAllOllamaTools()
	for _, tool := range ollamaTools {
		r.addTool(ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: convertInputSchema(tool.InputSchema),
		})
	}
}

// convertInputSchema converts the Tool InputSchema to map[string]interface{}.
func convertInputSchema(schema InputSchema) map[string]interface{} {
	properties := make(map[string]interface{})
	for name, prop := range schema.Properties {
		propMap := map[string]interface{}{
			"type":        prop.Type,
			"description": prop.Description,
		}
		if len(prop.Enum) > 0 {
			propMap["enum"] = prop.Enum
		}
		if prop.Default != "" {
			propMap["default"] = prop.Default
		}
		properties[name] = propMap
	}

	result := map[string]interface{}{
		"type":       schema.Type,
		"properties": properties,
	}
	if len(schema.Required) > 0 {
		result["required"] = schema.Required
	}
	return result
}

// executeOllamaTool executes an Ollama tool and returns the result.
func (r *ToolRegistry) executeOllamaTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	ollamaService := r.service.Ollama()
	if ollamaService == nil {
		return nil, api.NewServiceNotConfiguredError("ollama")
	}

	switch name {
	case "quicbaas_ollama_embed":
		return r.handleOllamaEmbed(ctx, ollamaService, args)
	case "quicbaas_ollama_embed_batch":
		return r.handleOllamaEmbedBatch(ctx, ollamaService, args)
	case "quicbaas_ollama_model_list":
		return r.handleOllamaModelList(ctx, ollamaService, args)
	case "quicbaas_ollama_model_info":
		return r.handleOllamaModelInfo(ctx, ollamaService, args)
	case "quicbaas_ollama_health":
		return r.handleOllamaHealth(ctx, ollamaService, args)
	default:
		return nil, api.NewUnknownToolError(name)
	}
}

// handleOllamaEmbed handles the quicbaas_ollama_embed tool.
func (r *ToolRegistry) handleOllamaEmbed(ctx context.Context, svc service.OllamaService, args map[string]interface{}) (interface{}, error) {
	text, ok := args["text"].(string)
	if !ok || text == "" {
		return nil, api.NewRequiredFieldError("text")
	}

	model, _ := args["model"].(string)

	var resp *service.OllamaEmbedResponse
	var err error

	if model != "" {
		resp, err = svc.EmbedWithModel(ctx, &service.OllamaEmbedWithModelRequest{
			Text:  text,
			Model: model,
		})
	} else {
		resp, err = svc.Embed(ctx, &service.OllamaEmbedRequest{
			Text: text,
		})
	}

	if err != nil {
		return nil, api.NewOperationFailedError("embed", err)
	}

	return map[string]interface{}{
		"embedding":       resp.Embedding,
		"model":           resp.Model,
		"dimensions":      resp.Dimensions,
		"processing_time": resp.ProcessingTime.String(),
	}, nil
}

// handleOllamaEmbedBatch handles the quicbaas_ollama_embed_batch tool.
func (r *ToolRegistry) handleOllamaEmbedBatch(ctx context.Context, svc service.OllamaService, args map[string]interface{}) (interface{}, error) {
	textsJSON, ok := args["texts"].(string)
	if !ok || textsJSON == "" {
		return nil, api.NewRequiredFieldError("texts")
	}

	var texts []string
	if err := json.Unmarshal([]byte(textsJSON), &texts); err != nil {
		return nil, api.NewInvalidFormatError("texts JSON", err)
	}

	if len(texts) == 0 {
		return nil, api.NewEmptyCollectionError("texts")
	}

	model, _ := args["model"].(string)

	resp, err := svc.EmbedBatch(ctx, &service.OllamaEmbedBatchRequest{
		Texts: texts,
		Model: model,
	})
	if err != nil {
		return nil, api.NewOperationFailedError("embed batch", err)
	}

	return map[string]interface{}{
		"embeddings":      resp.Embeddings,
		"model":           resp.Model,
		"dimensions":      resp.Dimensions,
		"count":           resp.Count,
		"processing_time": resp.ProcessingTime.String(),
	}, nil
}

// handleOllamaModelList handles the quicbaas_ollama_model_list tool.
func (r *ToolRegistry) handleOllamaModelList(ctx context.Context, svc service.OllamaService, args map[string]interface{}) (interface{}, error) {
	resp, err := svc.ListModels(ctx, &service.OllamaListModelsRequest{})
	if err != nil {
		return nil, api.NewOperationFailedError("list models", err)
	}

	models := make([]map[string]interface{}, len(resp.Models))
	for i, m := range resp.Models {
		models[i] = map[string]interface{}{
			"name":        m.Name,
			"size":        m.Size,
			"digest":      m.Digest,
			"modified_at": m.ModifiedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}

	return map[string]interface{}{
		"models": models,
		"count":  resp.Count,
	}, nil
}

// handleOllamaModelInfo handles the quicbaas_ollama_model_info tool.
func (r *ToolRegistry) handleOllamaModelInfo(ctx context.Context, svc service.OllamaService, args map[string]interface{}) (interface{}, error) {
	model, ok := args["model"].(string)
	if !ok || model == "" {
		return nil, api.NewRequiredFieldError("model")
	}

	resp, err := svc.GetModelInfo(ctx, &service.OllamaGetModelInfoRequest{
		Model: model,
	})
	if err != nil {
		return nil, api.NewOperationFailedError("get model info", err)
	}

	return map[string]interface{}{
		"model": resp.Model,
		"details": map[string]interface{}{
			"license":            resp.Details.License,
			"format":             resp.Details.Format,
			"family":             resp.Details.Family,
			"parameter_size":     resp.Details.ParameterSize,
			"quantization_level": resp.Details.QuantizationLevel,
		},
	}, nil
}

// handleOllamaHealth handles the quicbaas_ollama_health tool.
func (r *ToolRegistry) handleOllamaHealth(ctx context.Context, svc service.OllamaService, args map[string]interface{}) (interface{}, error) {
	resp, err := svc.Health(ctx, &service.OllamaHealthRequest{})
	if err != nil {
		// Health should always return a response
		return map[string]interface{}{
			"available": false,
			"error":     err.Error(),
		}, nil
	}

	result := map[string]interface{}{
		"available":    resp.Available,
		"version":      resp.Version,
		"base_url":     resp.BaseURL,
		"last_checked": resp.LastChecked.Format("2006-01-02T15:04:05Z07:00"),
	}

	if resp.Error != "" {
		result["error"] = resp.Error
	}

	return result, nil
}

// isOllamaTool returns true if the tool name is an Ollama tool.
func isOllamaTool(name string) bool {
	switch name {
	case "quicbaas_ollama_embed",
		"quicbaas_ollama_embed_batch",
		"quicbaas_ollama_model_list",
		"quicbaas_ollama_model_info",
		"quicbaas_ollama_health":
		return true
	default:
		return false
	}
}
