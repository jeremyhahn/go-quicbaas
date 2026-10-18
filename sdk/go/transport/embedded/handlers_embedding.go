// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package embedded

import (
	"context"
	"reflect"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
)

// embeddingServiceName is the service name the Embedding client sends.
const embeddingServiceName = "quicbaas.Embedding"

func init() {
	unaryHandlers[embeddingServiceName+"/GenerateEmbedding"] = handleEmbeddingGenerate
	unaryHandlers[embeddingServiceName+"/BatchGenerateEmbeddings"] = handleEmbeddingBatchGenerate
	unaryHandlers[embeddingServiceName+"/ListProviders"] = handleEmbeddingListProviders
	unaryHandlers[embeddingServiceName+"/GetProviderInfo"] = handleEmbeddingGetProviderInfo
	unaryHandlers[embeddingServiceName+"/SetDefaultProvider"] = handleEmbeddingSetDefaultProvider
	unaryHandlers[embeddingServiceName+"/GetCacheStats"] = handleEmbeddingGetCacheStats
	unaryHandlers[embeddingServiceName+"/ClearCache"] = handleEmbeddingClearCache
}

// embeddingRequest resolves the Embedding service and type-asserts the
// request the SDK client sent.
func embeddingRequest[T any](svc service.QuicBaaS, method string, req interface{}) (service.EmbeddingService, *T, error) {
	embeddingSvc := svc.Embedding()
	if embeddingSvc == nil {
		return nil, nil, NewServiceNotConfiguredError("Embedding")
	}
	r, ok := req.(*T)
	if !ok || r == nil {
		return nil, nil, NewInvalidRequestTypeError(embeddingServiceName, method,
			reflect.TypeOf((*T)(nil)).String(), typeNameOf(req))
	}
	return embeddingSvc, r, nil
}

func embeddingMetadataToSDK(m *service.EmbeddingMetadata) *embedding.Metadata {
	if m == nil {
		return nil
	}
	return &embedding.Metadata{
		Model:          m.Model,
		Provider:       embedding.Provider(m.Provider),
		Dimensions:     m.Dimensions,
		Tokens:         m.Tokens,
		Source:         m.Source,
		Language:       m.Language,
		InputType:      embedding.InputType(m.InputType),
		Tags:           m.Tags,
		CacheHit:       m.CacheHit,
		ProcessingTime: m.ProcessingTime,
	}
}

func embeddingProviderInfoToSDK(p *service.ProviderInfo) *embedding.ProviderInfo {
	if p == nil {
		return nil
	}
	return &embedding.ProviderInfo{
		Name:               p.Name,
		Type:               embedding.Provider(p.Type),
		Model:              p.Model,
		Dimensions:         p.Dimensions,
		MaxTokens:          p.MaxTokens,
		MaxBatchSize:       p.MaxBatchSize,
		Status:             embedding.Status(p.Status),
		APIURL:             p.APIURL,
		RateLimitPerMinute: p.RateLimitPerMinute,
		Timeout:            p.Timeout,
		IsDefault:          p.IsDefault,
		LastError:          p.LastError,
		LastErrorTime:      p.LastErrorTime,
		SuccessCount:       p.SuccessCount,
		ErrorCount:         p.ErrorCount,
		AvgLatency:         p.AvgLatency,
	}
}

func handleEmbeddingGenerate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	es, r, err := embeddingRequest[embedding.GenerateEmbeddingRequest](svc, "GenerateEmbedding", req)
	if err != nil {
		return nil, err
	}
	resp, err := es.GenerateEmbedding(ctx, &service.GenerateEmbeddingRequest{
		Text:        r.Text,
		Provider:    service.EmbeddingProvider(r.Provider),
		InputType:   service.EmbeddingInputType(r.InputType),
		Metadata:    r.Metadata,
		Source:      r.Source,
		Language:    r.Language,
		CacheBypass: r.CacheBypass,
	})
	if err != nil {
		return nil, err
	}
	return &embedding.GenerateEmbeddingResponse{
		Embedding: resp.Embedding,
		Metadata:  embeddingMetadataToSDK(resp.Metadata),
		Timestamp: resp.Timestamp,
	}, nil
}

func handleEmbeddingBatchGenerate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	es, r, err := embeddingRequest[embedding.BatchGenerateEmbeddingsRequest](svc, "BatchGenerateEmbeddings", req)
	if err != nil {
		return nil, err
	}
	items := make([]*service.BatchEmbeddingItem, 0, len(r.Items))
	for _, item := range r.Items {
		if item == nil {
			continue
		}
		items = append(items, &service.BatchEmbeddingItem{
			ID:        item.ID,
			Text:      item.Text,
			InputType: service.EmbeddingInputType(item.InputType),
			Metadata:  item.Metadata,
			Source:    item.Source,
			Language:  item.Language,
		})
	}
	resp, err := es.BatchGenerateEmbeddings(ctx, &service.BatchGenerateEmbeddingsRequest{
		Items:       items,
		Provider:    service.EmbeddingProvider(r.Provider),
		CacheBypass: r.CacheBypass,
	})
	if err != nil {
		return nil, err
	}
	results := make([]*embedding.BatchEmbeddingResult, 0, len(resp.Results))
	for _, res := range resp.Results {
		if res == nil {
			continue
		}
		results = append(results, &embedding.BatchEmbeddingResult{
			ID:        res.ID,
			Embedding: res.Embedding,
			Metadata:  embeddingMetadataToSDK(res.Metadata),
			Error:     res.Error,
		})
	}
	return &embedding.BatchGenerateEmbeddingsResponse{
		Results:        results,
		SuccessCount:   resp.SuccessCount,
		ErrorCount:     resp.ErrorCount,
		TotalTokens:    resp.TotalTokens,
		ProcessingTime: resp.ProcessingTime,
		Timestamp:      resp.Timestamp,
	}, nil
}

func handleEmbeddingListProviders(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	es, r, err := embeddingRequest[embedding.ListProvidersRequest](svc, "ListProviders", req)
	if err != nil {
		return nil, err
	}
	resp, err := es.ListProviders(ctx, &service.ListProvidersRequest{IncludeUnavailable: r.IncludeUnavailable})
	if err != nil {
		return nil, err
	}
	providers := make([]*embedding.ProviderInfo, 0, len(resp.Providers))
	for _, p := range resp.Providers {
		if p == nil {
			continue
		}
		providers = append(providers, embeddingProviderInfoToSDK(p))
	}
	return &embedding.ListProvidersResponse{
		Providers:       providers,
		DefaultProvider: resp.DefaultProvider,
		Timestamp:       resp.Timestamp,
	}, nil
}

func handleEmbeddingGetProviderInfo(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	es, r, err := embeddingRequest[embedding.GetProviderInfoRequest](svc, "GetProviderInfo", req)
	if err != nil {
		return nil, err
	}
	resp, err := es.GetProviderInfo(ctx, &service.GetProviderInfoRequest{
		Provider: service.EmbeddingProvider(r.Provider),
	})
	if err != nil {
		return nil, err
	}
	return &embedding.GetProviderInfoResponse{
		Info:      embeddingProviderInfoToSDK(resp.Info),
		Timestamp: resp.Timestamp,
	}, nil
}

func handleEmbeddingSetDefaultProvider(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	es, r, err := embeddingRequest[embedding.SetDefaultProviderRequest](svc, "SetDefaultProvider", req)
	if err != nil {
		return nil, err
	}
	resp, err := es.SetDefaultProvider(ctx, &service.SetDefaultProviderRequest{
		Provider: service.EmbeddingProvider(r.Provider),
	})
	if err != nil {
		return nil, err
	}
	return &embedding.SetDefaultProviderResponse{
		Provider:         embedding.Provider(resp.Provider),
		PreviousProvider: embedding.Provider(resp.PreviousProvider),
		Timestamp:        resp.Timestamp,
	}, nil
}

func handleEmbeddingGetCacheStats(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	es, _, err := embeddingRequest[embedding.GetEmbeddingCacheStatsRequest](svc, "GetCacheStats", req)
	if err != nil {
		return nil, err
	}
	resp, err := es.GetCacheStats(ctx, &service.GetEmbeddingCacheStatsRequest{})
	if err != nil {
		return nil, err
	}
	return &embedding.GetEmbeddingCacheStatsResponse{
		Enabled:            resp.Enabled,
		Size:               resp.Size,
		Capacity:           resp.Capacity,
		Hits:               resp.Hits,
		Misses:             resp.Misses,
		HitRate:            resp.HitRate,
		Evictions:          resp.Evictions,
		MemoryBytes:        resp.MemoryBytes,
		CompressionEnabled: resp.CompressionEnabled,
		CompressionRatio:   resp.CompressionRatio,
		Timestamp:          resp.Timestamp,
	}, nil
}

func handleEmbeddingClearCache(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	es, r, err := embeddingRequest[embedding.ClearEmbeddingCacheRequest](svc, "ClearCache", req)
	if err != nil {
		return nil, err
	}
	resp, err := es.ClearCache(ctx, &service.ClearEmbeddingCacheRequest{
		Provider: service.EmbeddingProvider(r.Provider),
		All:      r.All,
	})
	if err != nil {
		return nil, err
	}
	return &embedding.ClearEmbeddingCacheResponse{
		EntriesCleared: resp.EntriesCleared,
		Provider:       embedding.Provider(resp.Provider),
		Timestamp:      resp.Timestamp,
	}, nil
}
