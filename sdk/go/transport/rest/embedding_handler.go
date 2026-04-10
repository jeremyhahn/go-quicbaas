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
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isEmbeddingService returns true if the service is an Embedding service.
func isEmbeddingService(service string) bool {
	return strings.HasPrefix(service, "quicbaas.Embedding") || service == "Embedding"
}

// embeddingHandler handles Embedding-specific request/response conversions for REST transport.
type embeddingHandler struct {
	transport *Transport
}

// newEmbeddingHandler creates a new Embedding handler.
func newEmbeddingHandler(t *Transport) *embeddingHandler {
	return &embeddingHandler{transport: t}
}

// handleRequest routes Embedding requests to the REST transport and converts responses.
func (h *embeddingHandler) handleRequest(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	// GetProviderInfo requires a GET request with the provider name in the URL path.
	// The generic mapEmbeddingMethod cannot handle this because the provider name
	// is embedded in the request object, not available during URL construction.
	if method == "GetProviderInfo" || method == "GetModel" {
		return h.handleGetProviderInfo(ctx, req)
	}

	raw, err := h.transport.requestWithRetry(ctx, service, method, req)
	if err != nil {
		return nil, err
	}
	return h.convertResponse(method, raw)
}

// handleGetProviderInfo handles the GetProviderInfo request by constructing
// a GET request with the provider name as a URL path parameter.
func (h *embeddingHandler) handleGetProviderInfo(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*embedding.GetProviderInfoRequest)
	if !ok {
		return nil, errors.NewInvalidInput("rest.embeddingHandler.handleGetProviderInfo",
			"invalid request type, expected *embedding.GetProviderInfoRequest")
	}

	if sdkReq.Provider == "" {
		return nil, errors.NewInvalidInput("rest.embeddingHandler.handleGetProviderInfo",
			"provider is required")
	}

	url := h.transport.baseURL + h.transport.basePath +
		"/api/v1/embedding/providers/" + string(sdkReq.Provider)

	raw, err := h.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	return h.convertResponse("GetProviderInfo", raw)
}

// doRequest executes an HTTP request and returns the parsed response.
func (h *embeddingHandler) doRequest(ctx context.Context, method, url string, body interface{}) (interface{}, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, errors.Wrap(err, errors.CodeInvalidInput, "rest.embeddingHandler.doRequest",
				"failed to marshal request body")
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "rest.embeddingHandler.doRequest",
			"failed to create HTTP request")
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	h.transport.mu.RLock()
	for k, v := range h.transport.headers {
		httpReq.Header.Set(k, v)
	}
	h.transport.mu.RUnlock()

	resp, err := h.transport.client.Do(httpReq)
	if err != nil {
		return nil, h.transport.convertHTTPError(err, "rest.embeddingHandler.doRequest")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "rest.embeddingHandler.doRequest",
			"failed to read response body")
	}

	if resp.StatusCode >= 400 {
		return nil, h.transport.parseErrorResponse(resp.StatusCode, respBody)
	}

	var result interface{}
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal, "rest.embeddingHandler.doRequest",
				"failed to decode response")
		}
	}

	h.transport.lastHealth.Store(time.Now().UnixNano())

	return result, nil
}

// convertResponse converts a raw map response into a typed SDK response.
func (h *embeddingHandler) convertResponse(method string, raw interface{}) (interface{}, error) {
	if raw == nil {
		return nil, nil
	}

	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("rest.embeddingHandler", "unexpected response type")
	}

	switch method {
	case "GenerateEmbedding", "Generate":
		return convertGenerateEmbeddingResponse(m)
	case "BatchGenerateEmbeddings", "BatchGenerate":
		return convertBatchGenerateEmbeddingsResponse(m)
	case "ListProviders", "ListModels":
		return convertListProvidersResponse(m)
	case "GetProviderInfo", "GetModel":
		return convertGetProviderInfoResponse(m)
	case "SetDefaultProvider", "SetDefaultModel":
		return convertSetDefaultProviderResponse(m)
	case "GetCacheStats":
		return convertGetCacheStatsResponse(m)
	case "ClearCache":
		return convertClearCacheResponse(m)
	default:
		return raw, nil
	}
}

// convertGenerateEmbeddingResponse converts a raw map to a GenerateEmbeddingResponse.
func convertGenerateEmbeddingResponse(m map[string]interface{}) (*embedding.GenerateEmbeddingResponse, error) {
	resp := &embedding.GenerateEmbeddingResponse{}

	if v, ok := m["embedding"]; ok {
		resp.Embedding = toFloat32Slice(v)
	}

	if v, ok := m["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			resp.Timestamp = t
		}
	}

	// Build metadata from flat response fields
	resp.Metadata = &embedding.Metadata{}

	if v, ok := m["model"].(string); ok {
		resp.Metadata.Model = v
	}
	if v, ok := m["provider"].(string); ok {
		resp.Metadata.Provider = embedding.Provider(v)
	}
	if v, ok := m["dimensions"].(float64); ok {
		resp.Metadata.Dimensions = int(v)
	}
	if v, ok := m["tokens"].(float64); ok {
		resp.Metadata.Tokens = int(v)
	}
	if v, ok := m["cache_hit"].(bool); ok {
		resp.Metadata.CacheHit = v
	}
	if v, ok := m["processing_time"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			resp.Metadata.ProcessingTime = d
		}
	}

	return resp, nil
}

// convertBatchGenerateEmbeddingsResponse converts a raw map to a BatchGenerateEmbeddingsResponse.
func convertBatchGenerateEmbeddingsResponse(m map[string]interface{}) (*embedding.BatchGenerateEmbeddingsResponse, error) {
	resp := &embedding.BatchGenerateEmbeddingsResponse{}

	if v, ok := m["success_count"].(float64); ok {
		resp.SuccessCount = int(v)
	}
	if v, ok := m["error_count"].(float64); ok {
		resp.ErrorCount = int(v)
	}
	if v, ok := m["total_tokens"].(float64); ok {
		resp.TotalTokens = int(v)
	}
	if v, ok := m["processing_time"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			resp.ProcessingTime = d
		}
	}
	if v, ok := m["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			resp.Timestamp = t
		}
	}

	if results, ok := m["results"].([]interface{}); ok {
		resp.Results = make([]*embedding.BatchEmbeddingResult, len(results))
		for i, r := range results {
			if rm, ok := r.(map[string]interface{}); ok {
				resp.Results[i] = convertBatchEmbeddingResult(rm)
			}
		}
	}

	return resp, nil
}

// convertBatchEmbeddingResult converts a raw map to a BatchEmbeddingResult.
func convertBatchEmbeddingResult(m map[string]interface{}) *embedding.BatchEmbeddingResult {
	result := &embedding.BatchEmbeddingResult{}

	if v, ok := m["id"].(string); ok {
		result.ID = v
	}
	if v, ok := m["embedding"]; ok {
		result.Embedding = toFloat32Slice(v)
	}
	if v, ok := m["error"].(string); ok {
		result.Error = v
	}

	// Build metadata from flat response fields
	result.Metadata = &embedding.Metadata{}

	if v, ok := m["model"].(string); ok {
		result.Metadata.Model = v
	}
	if v, ok := m["provider"].(string); ok {
		result.Metadata.Provider = embedding.Provider(v)
	}
	if v, ok := m["dimensions"].(float64); ok {
		result.Metadata.Dimensions = int(v)
	}
	if v, ok := m["tokens"].(float64); ok {
		result.Metadata.Tokens = int(v)
	}
	if v, ok := m["cache_hit"].(bool); ok {
		result.Metadata.CacheHit = v
	}
	if v, ok := m["processing_time"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			result.Metadata.ProcessingTime = d
		}
	}

	return result
}

// convertListProvidersResponse converts a raw map to a ListProvidersResponse.
func convertListProvidersResponse(m map[string]interface{}) (*embedding.ListProvidersResponse, error) {
	resp := &embedding.ListProvidersResponse{}

	if v, ok := m["default_provider"].(string); ok {
		resp.DefaultProvider = v
	}
	if v, ok := m["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			resp.Timestamp = t
		}
	}

	if providers, ok := m["providers"].([]interface{}); ok {
		resp.Providers = make([]*embedding.ProviderInfo, len(providers))
		for i, p := range providers {
			if pm, ok := p.(map[string]interface{}); ok {
				resp.Providers[i] = convertProviderInfoFromMap(pm)
			}
		}
	}

	return resp, nil
}

// convertGetProviderInfoResponse converts a raw map to a GetProviderInfoResponse.
func convertGetProviderInfoResponse(m map[string]interface{}) (*embedding.GetProviderInfoResponse, error) {
	resp := &embedding.GetProviderInfoResponse{
		Info: convertProviderInfoFromMap(m),
	}

	if v, ok := m["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			resp.Timestamp = t
		}
	}

	return resp, nil
}

// convertSetDefaultProviderResponse converts a raw map to a SetDefaultProviderResponse.
func convertSetDefaultProviderResponse(m map[string]interface{}) (*embedding.SetDefaultProviderResponse, error) {
	resp := &embedding.SetDefaultProviderResponse{}

	if v, ok := m["provider"].(string); ok {
		resp.Provider = embedding.Provider(v)
	}
	if v, ok := m["previous_provider"].(string); ok {
		resp.PreviousProvider = embedding.Provider(v)
	}
	if v, ok := m["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			resp.Timestamp = t
		}
	}

	return resp, nil
}

// convertGetCacheStatsResponse converts a raw map to a GetEmbeddingCacheStatsResponse.
func convertGetCacheStatsResponse(m map[string]interface{}) (*embedding.GetEmbeddingCacheStatsResponse, error) {
	resp := &embedding.GetEmbeddingCacheStatsResponse{}

	if v, ok := m["enabled"].(bool); ok {
		resp.Enabled = v
	}
	if v, ok := m["size"].(float64); ok {
		resp.Size = int(v)
	}
	if v, ok := m["capacity"].(float64); ok {
		resp.Capacity = int(v)
	}
	if v, ok := m["hits"].(float64); ok {
		resp.Hits = uint64(v)
	}
	if v, ok := m["misses"].(float64); ok {
		resp.Misses = uint64(v)
	}
	if v, ok := m["hit_rate"].(float64); ok {
		resp.HitRate = v
	}
	if v, ok := m["evictions"].(float64); ok {
		resp.Evictions = uint64(v)
	}
	if v, ok := m["memory_bytes"].(float64); ok {
		resp.MemoryBytes = uint64(v)
	}
	if v, ok := m["compression_enabled"].(bool); ok {
		resp.CompressionEnabled = v
	}
	if v, ok := m["compression_ratio"].(float64); ok {
		resp.CompressionRatio = v
	}
	if v, ok := m["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			resp.Timestamp = t
		}
	}

	return resp, nil
}

// convertClearCacheResponse converts a raw map to a ClearEmbeddingCacheResponse.
func convertClearCacheResponse(m map[string]interface{}) (*embedding.ClearEmbeddingCacheResponse, error) {
	resp := &embedding.ClearEmbeddingCacheResponse{}

	if v, ok := m["entries_cleared"].(float64); ok {
		resp.EntriesCleared = int(v)
	}
	if v, ok := m["provider"].(string); ok {
		resp.Provider = embedding.Provider(v)
	}
	if v, ok := m["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			resp.Timestamp = t
		}
	}

	return resp, nil
}

// convertProviderInfoFromMap converts a raw map to a ProviderInfo.
func convertProviderInfoFromMap(m map[string]interface{}) *embedding.ProviderInfo {
	info := &embedding.ProviderInfo{}

	if v, ok := m["name"].(string); ok {
		info.Name = v
	}
	if v, ok := m["type"].(string); ok {
		info.Type = embedding.Provider(v)
	}
	if v, ok := m["model"].(string); ok {
		info.Model = v
	}
	if v, ok := m["dimensions"].(float64); ok {
		info.Dimensions = int(v)
	}
	if v, ok := m["max_tokens"].(float64); ok {
		info.MaxTokens = int(v)
	}
	if v, ok := m["max_batch_size"].(float64); ok {
		info.MaxBatchSize = int(v)
	}
	if v, ok := m["status"].(string); ok {
		info.Status = embedding.Status(v)
	}
	if v, ok := m["api_url"].(string); ok {
		info.APIURL = v
	}
	if v, ok := m["rate_limit_per_minute"].(float64); ok {
		info.RateLimitPerMinute = int(v)
	}
	if v, ok := m["timeout"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			info.Timeout = d
		}
	}
	if v, ok := m["is_default"].(bool); ok {
		info.IsDefault = v
	}
	if v, ok := m["last_error"].(string); ok {
		info.LastError = v
	}
	if v, ok := m["last_error_time"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			info.LastErrorTime = &t
		}
	}
	if v, ok := m["success_count"].(float64); ok {
		info.SuccessCount = uint64(v)
	}
	if v, ok := m["error_count"].(float64); ok {
		info.ErrorCount = uint64(v)
	}
	if v, ok := m["avg_latency"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			info.AvgLatency = d
		}
	}

	return info
}

// toFloat32Slice converts a JSON-decoded interface{} to a []float32.
func toFloat32Slice(v interface{}) []float32 {
	// JSON arrays decode as []interface{} with float64 elements
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}

	// Try fast-path: pre-encoded JSON number array
	result := make([]float32, 0, len(arr))
	for _, elem := range arr {
		switch val := elem.(type) {
		case float64:
			result = append(result, float32(val))
		case json.Number:
			if f, err := val.Float64(); err == nil {
				result = append(result, float32(f))
			}
		}
	}
	return result
}
