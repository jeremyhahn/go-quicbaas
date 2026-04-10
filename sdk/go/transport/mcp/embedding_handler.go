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

package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// embeddingHandler handles Embedding-specific request/response transformations for MCP transport.
type embeddingHandler struct{}

// newEmbeddingHandler creates a new Embedding handler.
func newEmbeddingHandler() *embeddingHandler {
	return &embeddingHandler{}
}

// transformRequest transforms SDK embedding requests to MCP tool arguments.
// The MCP server expects snake_case parameter names with specific types:
//   - "items" for BatchGenerateEmbeddings must be a JSON string, not an array
//   - boolean fields like "cache_bypass" must be string "true"/"false"
//   - string fields like "provider", "text", "input_type" are passed directly
func (h *embeddingHandler) transformRequest(method string, req interface{}) (map[string]interface{}, error) {
	requestHandlers := map[string]func(interface{}) (map[string]interface{}, error){
		"GenerateEmbedding":       h.transformGenerateEmbeddingRequest,
		"BatchGenerateEmbeddings": h.transformBatchGenerateEmbeddingsRequest,
		"ListProviders":           h.transformListProvidersRequest,
		"GetProviderInfo":         h.transformGetProviderInfoRequest,
		"SetDefaultProvider":      h.transformSetDefaultProviderRequest,
		"GetCacheStats":           h.transformGetCacheStatsRequest,
		"ClearCache":              h.transformClearCacheRequest,
	}

	handler, ok := requestHandlers[method]
	if !ok {
		return h.genericTransform(req)
	}
	return handler(req)
}

// genericTransform converts a request via JSON round-trip with snake_case keys.
func (h *embeddingHandler) genericTransform(req interface{}) (map[string]interface{}, error) {
	if req == nil {
		return map[string]interface{}{}, nil
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, errors.NewInternal("embeddingHandler.genericTransform",
			"failed to marshal request: "+err.Error())
	}
	var args map[string]interface{}
	if err := json.Unmarshal(data, &args); err != nil {
		return nil, errors.NewInternal("embeddingHandler.genericTransform",
			"failed to unmarshal request: "+err.Error())
	}
	return convertKeysToSnakeCase(args), nil
}

// transformGenerateEmbeddingRequest transforms GenerateEmbedding SDK request to MCP arguments.
func (h *embeddingHandler) transformGenerateEmbeddingRequest(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*embedding.GenerateEmbeddingRequest)
	if !ok {
		return h.genericTransform(req)
	}

	args := map[string]interface{}{
		"text": sdkReq.Text,
	}

	if sdkReq.Provider != "" {
		args["provider"] = string(sdkReq.Provider)
	}
	if sdkReq.InputType != "" {
		args["input_type"] = string(sdkReq.InputType)
	}
	if sdkReq.Source != "" {
		args["source"] = sdkReq.Source
	}
	if sdkReq.Language != "" {
		args["language"] = sdkReq.Language
	}
	if sdkReq.CacheBypass {
		args["cache_bypass"] = "true"
	}

	return args, nil
}

// transformBatchGenerateEmbeddingsRequest transforms BatchGenerateEmbeddings SDK request to MCP arguments.
// The MCP server expects "items" as a JSON string containing an array of item objects,
// not as a native array. Each item should have "id", "text", and optional "input_type",
// "source", "language" fields.
func (h *embeddingHandler) transformBatchGenerateEmbeddingsRequest(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*embedding.BatchGenerateEmbeddingsRequest)
	if !ok {
		return h.genericTransform(req)
	}

	// Build items as a slice of maps with snake_case keys
	items := make([]map[string]interface{}, 0, len(sdkReq.Items))
	for _, item := range sdkReq.Items {
		if item == nil {
			continue
		}
		itemMap := map[string]interface{}{
			"id":   item.ID,
			"text": item.Text,
		}
		if item.InputType != "" {
			itemMap["input_type"] = string(item.InputType)
		}
		if item.Source != "" {
			itemMap["source"] = item.Source
		}
		if item.Language != "" {
			itemMap["language"] = item.Language
		}
		items = append(items, itemMap)
	}

	// MCP server handler expects items as a JSON string
	itemsJSON, err := json.Marshal(items)
	if err != nil {
		return nil, errors.NewInternal("embeddingHandler.transformBatchGenerateEmbeddingsRequest",
			fmt.Sprintf("failed to marshal items: %s", err.Error()))
	}

	args := map[string]interface{}{
		"items": string(itemsJSON),
	}

	if sdkReq.Provider != "" {
		args["provider"] = string(sdkReq.Provider)
	}
	if sdkReq.CacheBypass {
		args["cache_bypass"] = "true"
	}

	return args, nil
}

// transformListProvidersRequest transforms ListProviders SDK request to MCP arguments.
func (h *embeddingHandler) transformListProvidersRequest(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*embedding.ListProvidersRequest)
	if !ok {
		return h.genericTransform(req)
	}

	args := map[string]interface{}{}

	if sdkReq.IncludeUnavailable {
		args["include_unavailable"] = "true"
	}

	return args, nil
}

// transformGetProviderInfoRequest transforms GetProviderInfo SDK request to MCP arguments.
func (h *embeddingHandler) transformGetProviderInfoRequest(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*embedding.GetProviderInfoRequest)
	if !ok {
		return h.genericTransform(req)
	}

	return map[string]interface{}{
		"provider": string(sdkReq.Provider),
	}, nil
}

// transformSetDefaultProviderRequest transforms SetDefaultProvider SDK request to MCP arguments.
func (h *embeddingHandler) transformSetDefaultProviderRequest(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*embedding.SetDefaultProviderRequest)
	if !ok {
		return h.genericTransform(req)
	}

	return map[string]interface{}{
		"provider": string(sdkReq.Provider),
	}, nil
}

// transformGetCacheStatsRequest transforms GetCacheStats SDK request to MCP arguments.
// This is a no-argument tool, so we return an empty map.
func (h *embeddingHandler) transformGetCacheStatsRequest(req interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

// transformClearCacheRequest transforms ClearCache SDK request to MCP arguments.
func (h *embeddingHandler) transformClearCacheRequest(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*embedding.ClearEmbeddingCacheRequest)
	if !ok {
		return h.genericTransform(req)
	}

	args := map[string]interface{}{}

	if sdkReq.Provider != "" {
		args["provider"] = string(sdkReq.Provider)
	}
	if sdkReq.All {
		args["all"] = "true"
	}

	return args, nil
}

// transformResponse transforms MCP tool response to SDK embedding types.
func (h *embeddingHandler) transformResponse(method string, resp interface{}) (interface{}, error) {
	responseHandlers := map[string]func(interface{}) (interface{}, error){
		"GenerateEmbedding":       h.transformGenerateEmbeddingResponse,
		"BatchGenerateEmbeddings": h.transformBatchGenerateEmbeddingsResponse,
		"ListProviders":           h.transformListProvidersResponse,
		"GetProviderInfo":         h.transformGetProviderInfoResponse,
		"SetDefaultProvider":      h.transformSetDefaultProviderResponse,
		"GetCacheStats":           h.transformGetCacheStatsResponse,
		"ClearCache":              h.transformClearCacheResponse,
	}

	handler, ok := responseHandlers[method]
	if !ok {
		return resp, nil
	}
	return handler(resp)
}

// toMap converts a response value to map[string]interface{}.
func (h *embeddingHandler) toMap(resp interface{}) (map[string]interface{}, error) {
	if m, ok := resp.(map[string]interface{}); ok {
		return m, nil
	}
	jsonBytes, err := json.Marshal(resp)
	if err != nil {
		return nil, errors.NewInternal("embeddingHandler.toMap", "failed to marshal response: "+err.Error())
	}
	var m map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &m); err != nil {
		return nil, errors.NewInternal("embeddingHandler.toMap", "failed to unmarshal response: "+err.Error())
	}
	return m, nil
}

// transformGenerateEmbeddingResponse transforms GenerateEmbedding response.
func (h *embeddingHandler) transformGenerateEmbeddingResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, errors.NewInternal("embeddingHandler.transformGenerateEmbeddingResponse",
			"nil response from server")
	}

	data, err := h.toMap(resp)
	if err != nil {
		return nil, err
	}

	result := &embedding.GenerateEmbeddingResponse{
		Embedding: h.extractFloat32Slice(data, "embedding"),
		Timestamp: h.extractTime(data, "timestamp"),
	}

	if metaRaw, ok := data["metadata"]; ok {
		result.Metadata = h.parseMetadata(metaRaw)
	}

	return result, nil
}

// transformBatchGenerateEmbeddingsResponse transforms BatchGenerateEmbeddings response.
func (h *embeddingHandler) transformBatchGenerateEmbeddingsResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, errors.NewInternal("embeddingHandler.transformBatchGenerateEmbeddingsResponse",
			"nil response from server")
	}

	data, err := h.toMap(resp)
	if err != nil {
		return nil, err
	}

	result := &embedding.BatchGenerateEmbeddingsResponse{
		SuccessCount:   int(getInt64FromMap(data, "success_count")),
		ErrorCount:     int(getInt64FromMap(data, "error_count")),
		TotalTokens:    int(getInt64FromMap(data, "total_tokens")),
		ProcessingTime: time.Duration(getInt64FromMap(data, "processing_time")),
		Timestamp:      h.extractTime(data, "timestamp"),
	}

	if resultsRaw, ok := data["results"].([]interface{}); ok {
		result.Results = make([]*embedding.BatchEmbeddingResult, 0, len(resultsRaw))
		for _, itemRaw := range resultsRaw {
			item := h.parseBatchEmbeddingResult(itemRaw)
			if item != nil {
				result.Results = append(result.Results, item)
			}
		}
	}

	return result, nil
}

// transformListProvidersResponse transforms ListProviders response.
func (h *embeddingHandler) transformListProvidersResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return &embedding.ListProvidersResponse{Providers: []*embedding.ProviderInfo{}}, nil
	}

	data, err := h.toMap(resp)
	if err != nil {
		return nil, err
	}

	result := &embedding.ListProvidersResponse{
		DefaultProvider: getStringFromMap(data, "default_provider"),
		Timestamp:       h.extractTime(data, "timestamp"),
	}

	if providersRaw, ok := data["providers"].([]interface{}); ok {
		result.Providers = make([]*embedding.ProviderInfo, 0, len(providersRaw))
		for _, pRaw := range providersRaw {
			info := h.parseProviderInfo(pRaw)
			if info != nil {
				result.Providers = append(result.Providers, info)
			}
		}
	} else {
		result.Providers = []*embedding.ProviderInfo{}
	}

	return result, nil
}

// transformGetProviderInfoResponse transforms GetProviderInfo response.
func (h *embeddingHandler) transformGetProviderInfoResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, errors.NewInternal("embeddingHandler.transformGetProviderInfoResponse",
			"nil response from server")
	}

	data, err := h.toMap(resp)
	if err != nil {
		return nil, err
	}

	result := &embedding.GetProviderInfoResponse{
		Timestamp: h.extractTime(data, "timestamp"),
	}

	// The provider info may be nested under "info" or at the top level
	if infoRaw, ok := data["info"]; ok {
		result.Info = h.parseProviderInfo(infoRaw)
	} else {
		result.Info = h.parseProviderInfoFromMap(data)
	}

	return result, nil
}

// transformSetDefaultProviderResponse transforms SetDefaultProvider response.
func (h *embeddingHandler) transformSetDefaultProviderResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, errors.NewInternal("embeddingHandler.transformSetDefaultProviderResponse",
			"nil response from server")
	}

	data, err := h.toMap(resp)
	if err != nil {
		return nil, err
	}

	return &embedding.SetDefaultProviderResponse{
		Provider:         embedding.Provider(getStringFromMap(data, "provider")),
		PreviousProvider: embedding.Provider(getStringFromMap(data, "previous_provider")),
		Timestamp:        h.extractTime(data, "timestamp"),
	}, nil
}

// transformGetCacheStatsResponse transforms GetCacheStats response.
func (h *embeddingHandler) transformGetCacheStatsResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, errors.NewInternal("embeddingHandler.transformGetCacheStatsResponse",
			"nil response from server")
	}

	data, err := h.toMap(resp)
	if err != nil {
		return nil, err
	}

	return &embedding.GetEmbeddingCacheStatsResponse{
		Enabled:            getBoolFromMap(data, "enabled"),
		Size:               int(getInt64FromMap(data, "size")),
		Capacity:           int(getInt64FromMap(data, "capacity")),
		Hits:               getUint64FromMap(data, "hits"),
		Misses:             getUint64FromMap(data, "misses"),
		HitRate:            h.extractFloat64(data, "hit_rate"),
		Evictions:          getUint64FromMap(data, "evictions"),
		MemoryBytes:        getUint64FromMap(data, "memory_bytes"),
		CompressionEnabled: getBoolFromMap(data, "compression_enabled"),
		CompressionRatio:   h.extractFloat64(data, "compression_ratio"),
		Timestamp:          h.extractTime(data, "timestamp"),
	}, nil
}

// transformClearCacheResponse transforms ClearCache response.
func (h *embeddingHandler) transformClearCacheResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, errors.NewInternal("embeddingHandler.transformClearCacheResponse",
			"nil response from server")
	}

	data, err := h.toMap(resp)
	if err != nil {
		return nil, err
	}

	return &embedding.ClearEmbeddingCacheResponse{
		EntriesCleared: int(getInt64FromMap(data, "entries_cleared")),
		Provider:       embedding.Provider(getStringFromMap(data, "provider")),
		Timestamp:      h.extractTime(data, "timestamp"),
	}, nil
}

// parseMetadata converts a raw metadata value to an embedding.Metadata struct.
func (h *embeddingHandler) parseMetadata(raw interface{}) *embedding.Metadata {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}

	meta := &embedding.Metadata{
		Model:          getStringFromMap(m, "model"),
		Provider:       embedding.Provider(getStringFromMap(m, "provider")),
		Dimensions:     int(getInt64FromMap(m, "dimensions")),
		Tokens:         int(getInt64FromMap(m, "tokens")),
		Source:         getStringFromMap(m, "source"),
		Language:       getStringFromMap(m, "language"),
		InputType:      embedding.InputType(getStringFromMap(m, "input_type")),
		CacheHit:       getBoolFromMap(m, "cache_hit"),
		ProcessingTime: time.Duration(getInt64FromMap(m, "processing_time")),
	}

	if tagsRaw, ok := m["tags"].(map[string]interface{}); ok {
		meta.Tags = make(map[string]string, len(tagsRaw))
		for k, v := range tagsRaw {
			if s, ok := v.(string); ok {
				meta.Tags[k] = s
			}
		}
	}

	return meta
}

// parseBatchEmbeddingResult converts a raw batch result item to an embedding.BatchEmbeddingResult.
func (h *embeddingHandler) parseBatchEmbeddingResult(raw interface{}) *embedding.BatchEmbeddingResult {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}

	result := &embedding.BatchEmbeddingResult{
		ID:        getStringFromMap(m, "id"),
		Embedding: h.extractFloat32Slice(m, "embedding"),
		Error:     getStringFromMap(m, "error"),
	}

	if metaRaw, ok := m["metadata"]; ok {
		result.Metadata = h.parseMetadata(metaRaw)
	}

	return result
}

// parseProviderInfo converts a raw value to an embedding.ProviderInfo.
func (h *embeddingHandler) parseProviderInfo(raw interface{}) *embedding.ProviderInfo {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}
	return h.parseProviderInfoFromMap(m)
}

// parseProviderInfoFromMap converts a map to an embedding.ProviderInfo.
func (h *embeddingHandler) parseProviderInfoFromMap(m map[string]interface{}) *embedding.ProviderInfo {
	info := &embedding.ProviderInfo{
		Name:               getStringFromMap(m, "name"),
		Type:               embedding.Provider(getStringFromMap(m, "type")),
		Model:              getStringFromMap(m, "model"),
		Dimensions:         int(getInt64FromMap(m, "dimensions")),
		MaxTokens:          int(getInt64FromMap(m, "max_tokens")),
		MaxBatchSize:       int(getInt64FromMap(m, "max_batch_size")),
		Status:             embedding.Status(getStringFromMap(m, "status")),
		APIURL:             getStringFromMap(m, "api_url"),
		RateLimitPerMinute: int(getInt64FromMap(m, "rate_limit_per_minute")),
		Timeout:            time.Duration(getInt64FromMap(m, "timeout")),
		IsDefault:          getBoolFromMap(m, "is_default"),
		LastError:          getStringFromMap(m, "last_error"),
		SuccessCount:       getUint64FromMap(m, "success_count"),
		ErrorCount:         getUint64FromMap(m, "error_count"),
		AvgLatency:         time.Duration(getInt64FromMap(m, "avg_latency")),
	}

	lastErrorTime := parseTimeFromMap(m, "last_error_time")
	if !lastErrorTime.IsZero() {
		info.LastErrorTime = &lastErrorTime
	}

	return info
}

// extractFloat32Slice extracts a []float32 from a map field.
func (h *embeddingHandler) extractFloat32Slice(m map[string]interface{}, key string) []float32 {
	raw, ok := m[key]
	if !ok {
		return nil
	}

	switch v := raw.(type) {
	case []interface{}:
		result := make([]float32, 0, len(v))
		for _, item := range v {
			if f, ok := item.(float64); ok {
				result = append(result, float32(f))
			}
		}
		return result
	case []float64:
		result := make([]float32, len(v))
		for i, f := range v {
			result[i] = float32(f)
		}
		return result
	case []float32:
		return v
	default:
		return nil
	}
}

// extractFloat64 extracts a float64 from a map field.
func (h *embeddingHandler) extractFloat64(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}

// extractTime extracts a time.Time from a map field, supporting multiple formats.
func (h *embeddingHandler) extractTime(m map[string]interface{}, key string) time.Time {
	return parseTimeFromMap(m, key)
}

// isMCPEmbeddingService returns true if the service is an Embedding service.
func isMCPEmbeddingService(service string) bool {
	serviceName := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		serviceName = service[idx+1:]
	}
	return serviceName == "Embedding"
}
