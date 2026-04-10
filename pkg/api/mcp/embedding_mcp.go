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

// Package mcp implements Embedding MCP integration handlers.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// registerEmbeddingTools registers all Embedding tool handlers on the ToolRegistry.
func (r *ToolRegistry) registerEmbeddingTools() {
	embeddingTools := getAllEmbeddingTools()
	for _, tool := range embeddingTools {
		r.addTool(ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: convertInputSchema(tool.InputSchema),
		})
	}
}

// executeEmbeddingTool executes an Embedding tool and returns the result.
func (r *ToolRegistry) executeEmbeddingTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	embeddingService := r.service.Embedding()
	if embeddingService == nil {
		return nil, api.NewServiceNotConfiguredError("embedding")
	}

	switch name {
	case "quicbaas_embedding_generate":
		return r.handleEmbeddingGenerate(ctx, embeddingService, args)
	case "quicbaas_embedding_batch":
		return r.handleEmbeddingBatch(ctx, embeddingService, args)
	case "quicbaas_embedding_list_providers":
		return r.handleEmbeddingListProviders(ctx, embeddingService, args)
	case "quicbaas_embedding_get_provider":
		return r.handleEmbeddingGetProvider(ctx, embeddingService, args)
	case "quicbaas_embedding_set_default":
		return r.handleEmbeddingSetDefault(ctx, embeddingService, args)
	case "quicbaas_embedding_cache_stats":
		return r.handleEmbeddingCacheStats(ctx, embeddingService, args)
	case "quicbaas_embedding_cache_clear":
		return r.handleEmbeddingCacheClear(ctx, embeddingService, args)
	default:
		return nil, api.NewUnknownToolError(name)
	}
}

// handleEmbeddingGenerate handles the quicbaas_embedding_generate tool.
func (r *ToolRegistry) handleEmbeddingGenerate(ctx context.Context, svc service.EmbeddingService, args map[string]interface{}) (interface{}, error) {
	text, ok := args["text"].(string)
	if !ok || text == "" {
		return nil, api.NewRequiredFieldError("text")
	}

	req := &service.GenerateEmbeddingRequest{
		Text: text,
	}

	// Set optional provider
	if provider, ok := args["provider"].(string); ok && provider != "" {
		req.Provider = service.EmbeddingProvider(provider)
	}

	// Set optional input type
	if inputType, ok := args["input_type"].(string); ok && inputType != "" {
		req.InputType = service.EmbeddingInputType(inputType)
	}

	// Set optional source
	if source, ok := args["source"].(string); ok {
		req.Source = source
	}

	// Set optional language
	if language, ok := args["language"].(string); ok {
		req.Language = language
	}

	// Set cache bypass
	if cacheBypass, ok := args["cache_bypass"].(string); ok && cacheBypass == "true" {
		req.CacheBypass = true
	}

	resp, err := svc.GenerateEmbedding(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("generate embedding", err)
	}

	result := map[string]interface{}{
		"embedding":  resp.Embedding,
		"dimensions": len(resp.Embedding),
		"timestamp":  resp.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	}

	if resp.Metadata != nil {
		result["metadata"] = map[string]interface{}{
			"model":           resp.Metadata.Model,
			"provider":        string(resp.Metadata.Provider),
			"dimensions":      resp.Metadata.Dimensions,
			"tokens":          resp.Metadata.Tokens,
			"cache_hit":       resp.Metadata.CacheHit,
			"processing_time": resp.Metadata.ProcessingTime.String(),
		}
	}

	return result, nil
}

// handleEmbeddingBatch handles the quicbaas_embedding_batch tool.
func (r *ToolRegistry) handleEmbeddingBatch(ctx context.Context, svc service.EmbeddingService, args map[string]interface{}) (interface{}, error) {
	itemsJSON, ok := args["items"].(string)
	if !ok || itemsJSON == "" {
		return nil, api.NewRequiredFieldError("items")
	}

	// Parse items JSON
	var inputItems []struct {
		ID        string `json:"id"`
		Text      string `json:"text"`
		InputType string `json:"input_type,omitempty"`
		Source    string `json:"source,omitempty"`
		Language  string `json:"language,omitempty"`
	}
	if err := json.Unmarshal([]byte(itemsJSON), &inputItems); err != nil {
		return nil, api.NewInvalidInputCauseError("invalid items JSON", err)
	}

	if len(inputItems) == 0 {
		return nil, api.NewEmptyCollectionError("items array")
	}

	// Build batch request
	items := make([]*service.BatchEmbeddingItem, len(inputItems))
	for i, item := range inputItems {
		if item.Text == "" {
			return nil, api.NewBatchItemValidationError(i, "has empty text")
		}
		items[i] = &service.BatchEmbeddingItem{
			ID:        item.ID,
			Text:      item.Text,
			InputType: service.EmbeddingInputType(item.InputType),
			Source:    item.Source,
			Language:  item.Language,
		}
		// Use index as ID if not provided
		if items[i].ID == "" {
			items[i].ID = fmt.Sprintf("item_%d", i)
		}
	}

	req := &service.BatchGenerateEmbeddingsRequest{
		Items: items,
	}

	// Set optional provider
	if provider, ok := args["provider"].(string); ok && provider != "" {
		req.Provider = service.EmbeddingProvider(provider)
	}

	// Set cache bypass
	if cacheBypass, ok := args["cache_bypass"].(string); ok && cacheBypass == "true" {
		req.CacheBypass = true
	}

	resp, err := svc.BatchGenerateEmbeddings(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("batch generate embeddings", err)
	}

	// Build results
	results := make([]map[string]interface{}, len(resp.Results))
	for i, result := range resp.Results {
		resultMap := map[string]interface{}{
			"id":         result.ID,
			"embedding":  result.Embedding,
			"dimensions": len(result.Embedding),
		}
		if result.Error != "" {
			resultMap["error"] = result.Error
		}
		if result.Metadata != nil {
			resultMap["metadata"] = map[string]interface{}{
				"model":           result.Metadata.Model,
				"provider":        string(result.Metadata.Provider),
				"tokens":          result.Metadata.Tokens,
				"cache_hit":       result.Metadata.CacheHit,
				"processing_time": result.Metadata.ProcessingTime.String(),
			}
		}
		results[i] = resultMap
	}

	return map[string]interface{}{
		"results":         results,
		"success_count":   resp.SuccessCount,
		"error_count":     resp.ErrorCount,
		"total_tokens":    resp.TotalTokens,
		"processing_time": resp.ProcessingTime.String(),
		"timestamp":       resp.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

// handleEmbeddingListProviders handles the quicbaas_embedding_list_providers tool.
func (r *ToolRegistry) handleEmbeddingListProviders(ctx context.Context, svc service.EmbeddingService, args map[string]interface{}) (interface{}, error) {
	req := &service.ListProvidersRequest{}

	// Set include unavailable
	if includeUnavailable, ok := args["include_unavailable"].(string); ok && includeUnavailable == "true" {
		req.IncludeUnavailable = true
	}

	resp, err := svc.ListProviders(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("list providers", err)
	}

	// Build provider list
	providers := make([]map[string]interface{}, len(resp.Providers))
	for i, p := range resp.Providers {
		providers[i] = map[string]interface{}{
			"name":           p.Name,
			"type":           string(p.Type),
			"model":          p.Model,
			"dimensions":     p.Dimensions,
			"max_tokens":     p.MaxTokens,
			"max_batch_size": p.MaxBatchSize,
			"status":         string(p.Status),
			"is_default":     p.IsDefault,
			"success_count":  p.SuccessCount,
			"error_count":    p.ErrorCount,
			"avg_latency":    p.AvgLatency.String(),
		}
		if p.RateLimitPerMinute > 0 {
			providers[i]["rate_limit_per_minute"] = p.RateLimitPerMinute
		}
		if p.LastError != "" {
			providers[i]["last_error"] = p.LastError
		}
	}

	return map[string]interface{}{
		"providers":        providers,
		"default_provider": resp.DefaultProvider,
		"count":            len(providers),
		"timestamp":        resp.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

// handleEmbeddingGetProvider handles the quicbaas_embedding_get_provider tool.
func (r *ToolRegistry) handleEmbeddingGetProvider(ctx context.Context, svc service.EmbeddingService, args map[string]interface{}) (interface{}, error) {
	provider, ok := args["provider"].(string)
	if !ok || provider == "" {
		return nil, api.NewRequiredFieldError("provider")
	}

	req := &service.GetProviderInfoRequest{
		Provider: service.EmbeddingProvider(provider),
	}

	resp, err := svc.GetProviderInfo(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("get provider info", err)
	}

	info := resp.Info
	result := map[string]interface{}{
		"name":           info.Name,
		"type":           string(info.Type),
		"model":          info.Model,
		"dimensions":     info.Dimensions,
		"max_tokens":     info.MaxTokens,
		"max_batch_size": info.MaxBatchSize,
		"status":         string(info.Status),
		"api_url":        info.APIURL,
		"timeout":        info.Timeout.String(),
		"is_default":     info.IsDefault,
		"success_count":  info.SuccessCount,
		"error_count":    info.ErrorCount,
		"avg_latency":    info.AvgLatency.String(),
		"timestamp":      resp.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	}

	if info.RateLimitPerMinute > 0 {
		result["rate_limit_per_minute"] = info.RateLimitPerMinute
	}
	if info.LastError != "" {
		result["last_error"] = info.LastError
	}
	if info.LastErrorTime != nil {
		result["last_error_time"] = info.LastErrorTime.Format("2006-01-02T15:04:05Z07:00")
	}

	return result, nil
}

// handleEmbeddingSetDefault handles the quicbaas_embedding_set_default tool.
func (r *ToolRegistry) handleEmbeddingSetDefault(ctx context.Context, svc service.EmbeddingService, args map[string]interface{}) (interface{}, error) {
	provider, ok := args["provider"].(string)
	if !ok || provider == "" {
		return nil, api.NewRequiredFieldError("provider")
	}

	req := &service.SetDefaultProviderRequest{
		Provider: service.EmbeddingProvider(provider),
	}

	resp, err := svc.SetDefaultProvider(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("set default provider", err)
	}

	return map[string]interface{}{
		"provider":          string(resp.Provider),
		"previous_provider": string(resp.PreviousProvider),
		"timestamp":         resp.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

// handleEmbeddingCacheStats handles the quicbaas_embedding_cache_stats tool.
func (r *ToolRegistry) handleEmbeddingCacheStats(ctx context.Context, svc service.EmbeddingService, args map[string]interface{}) (interface{}, error) {
	req := &service.GetEmbeddingCacheStatsRequest{}

	resp, err := svc.GetCacheStats(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("get cache stats", err)
	}

	result := map[string]interface{}{
		"enabled":             resp.Enabled,
		"size":                resp.Size,
		"capacity":            resp.Capacity,
		"hits":                resp.Hits,
		"misses":              resp.Misses,
		"hit_rate":            resp.HitRate,
		"evictions":           resp.Evictions,
		"compression_enabled": resp.CompressionEnabled,
		"timestamp":           resp.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	}

	if resp.MemoryBytes > 0 {
		result["memory_bytes"] = resp.MemoryBytes
	}
	if resp.CompressionRatio > 0 {
		result["compression_ratio"] = resp.CompressionRatio
	}

	return result, nil
}

// handleEmbeddingCacheClear handles the quicbaas_embedding_cache_clear tool.
func (r *ToolRegistry) handleEmbeddingCacheClear(ctx context.Context, svc service.EmbeddingService, args map[string]interface{}) (interface{}, error) {
	req := &service.ClearEmbeddingCacheRequest{}

	// Set optional provider filter
	if provider, ok := args["provider"].(string); ok && provider != "" {
		req.Provider = service.EmbeddingProvider(provider)
	}

	// Set all flag
	if all, ok := args["all"].(string); ok && all == "true" {
		req.All = true
	}

	resp, err := svc.ClearCache(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("clear cache", err)
	}

	result := map[string]interface{}{
		"entries_cleared": resp.EntriesCleared,
		"timestamp":       resp.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	}

	if resp.Provider != "" {
		result["provider"] = string(resp.Provider)
	}

	return result, nil
}

// isEmbeddingTool returns true if the tool name is an Embedding tool.
func isEmbeddingTool(name string) bool {
	switch name {
	case "quicbaas_embedding_generate",
		"quicbaas_embedding_batch",
		"quicbaas_embedding_list_providers",
		"quicbaas_embedding_get_provider",
		"quicbaas_embedding_set_default",
		"quicbaas_embedding_cache_stats",
		"quicbaas_embedding_cache_clear":
		return true
	default:
		return false
	}
}
