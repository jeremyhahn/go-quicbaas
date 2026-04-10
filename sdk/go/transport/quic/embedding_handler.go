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

package quic

import (
	"encoding/json"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// embeddingServicePrefix is the service name prefix for Embedding operations.
const embeddingServicePrefix = "quicbaas.Embedding"

// isEmbeddingService checks if the service name is an Embedding service.
func isEmbeddingService(svc string) bool {
	return strings.HasPrefix(svc, embeddingServicePrefix) || svc == "Embedding"
}

// embeddingResponseConverter defines the function signature for embedding response converters.
type embeddingResponseConverter func(data []byte) (interface{}, error)

// embeddingResponseConverters maps method names to their response converters for O(1) dispatch.
var embeddingResponseConverters = map[string]embeddingResponseConverter{
	"GenerateEmbedding":       convertGenerateEmbeddingResponse,
	"BatchGenerateEmbeddings": convertBatchGenerateEmbeddingsResponse,
	"ListProviders":           convertListProvidersResponse,
	"GetProviderInfo":         convertGetProviderInfoResponse,
	"SetDefaultProvider":      convertSetDefaultProviderResponse,
	"GetCacheStats":           convertGetEmbeddingCacheStatsResponse,
	"ClearCache":              convertClearEmbeddingCacheResponse,
}

// convertEmbeddingResponse converts a raw map response to the appropriate SDK embedding type.
// It marshals the map to JSON and unmarshals into the typed struct for reliable conversion.
func convertEmbeddingResponse(method string, decoded map[string]interface{}) (interface{}, error) {
	if decoded == nil {
		return nil, nil
	}

	converter, ok := embeddingResponseConverters[method]
	if !ok {
		return decoded, nil
	}

	data, err := json.Marshal(decoded)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertEmbeddingResponse", "failed to marshal response")
	}

	return converter(data)
}

// convertGenerateEmbeddingResponse converts JSON data to a GenerateEmbeddingResponse.
func convertGenerateEmbeddingResponse(data []byte) (interface{}, error) {
	var resp embedding.GenerateEmbeddingResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertGenerateEmbeddingResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// convertBatchGenerateEmbeddingsResponse converts JSON data to a BatchGenerateEmbeddingsResponse.
func convertBatchGenerateEmbeddingsResponse(data []byte) (interface{}, error) {
	var resp embedding.BatchGenerateEmbeddingsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertBatchGenerateEmbeddingsResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// convertListProvidersResponse converts JSON data to a ListProvidersResponse.
func convertListProvidersResponse(data []byte) (interface{}, error) {
	var resp embedding.ListProvidersResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertListProvidersResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// convertGetProviderInfoResponse converts JSON data to a GetProviderInfoResponse.
func convertGetProviderInfoResponse(data []byte) (interface{}, error) {
	var resp embedding.GetProviderInfoResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertGetProviderInfoResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// convertSetDefaultProviderResponse converts JSON data to a SetDefaultProviderResponse.
func convertSetDefaultProviderResponse(data []byte) (interface{}, error) {
	var resp embedding.SetDefaultProviderResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertSetDefaultProviderResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// convertGetEmbeddingCacheStatsResponse converts JSON data to a GetEmbeddingCacheStatsResponse.
func convertGetEmbeddingCacheStatsResponse(data []byte) (interface{}, error) {
	var resp embedding.GetEmbeddingCacheStatsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertGetEmbeddingCacheStatsResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// convertClearEmbeddingCacheResponse converts JSON data to a ClearEmbeddingCacheResponse.
func convertClearEmbeddingCacheResponse(data []byte) (interface{}, error) {
	var resp embedding.ClearEmbeddingCacheResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertClearEmbeddingCacheResponse", "failed to unmarshal response")
	}
	return &resp, nil
}
