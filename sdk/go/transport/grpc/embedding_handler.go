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

package grpc

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// embeddingServicePrefix is the service name prefix for embedding service routing.
const embeddingServicePrefix = "quicbaas.Embedding"

// isEmbeddingService checks if the service name is an Embedding service.
func isEmbeddingService(svc string) bool {
	return strings.HasPrefix(svc, embeddingServicePrefix)
}

// embeddingHandler handles Embedding service requests by converting between SDK types and protobuf types.
type embeddingHandler struct {
	conn *grpc.ClientConn
}

// newEmbeddingHandler creates a new embedding handler.
func newEmbeddingHandler(conn *grpc.ClientConn) *embeddingHandler {
	return &embeddingHandler{conn: conn}
}

// handleRequest routes Embedding requests to the appropriate handler based on method name.
func (h *embeddingHandler) handleRequest(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	client := pb.NewEmbeddingServiceClient(h.conn)

	switch method {
	case "GenerateEmbedding":
		return h.handleGenerateEmbedding(ctx, client, req)
	case "BatchGenerateEmbeddings":
		return h.handleBatchGenerateEmbeddings(ctx, client, req)
	case "ListProviders":
		return h.handleListProviders(ctx, client, req)
	case "GetProviderInfo":
		return h.handleGetProviderInfo(ctx, client, req)
	case "SetDefaultProvider":
		return h.handleSetDefaultProvider(ctx, client, req)
	case "GetCacheStats":
		return h.handleGetCacheStats(ctx, client, req)
	case "ClearCache":
		return h.handleClearCache(ctx, client, req)
	default:
		return nil, errors.NewUnimplemented("embeddingHandler.handleRequest",
			"unsupported method: "+method)
	}
}

// handleGenerateEmbedding handles a GenerateEmbedding request.
func (h *embeddingHandler) handleGenerateEmbedding(ctx context.Context, client pb.EmbeddingServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*embedding.GenerateEmbeddingRequest)
	if !ok {
		return nil, errors.NewInvalidInput("embeddingHandler.handleGenerateEmbedding", "invalid request type")
	}

	pbReq := &pb.EmbeddingGenerateRequest{
		Text:        sdkReq.Text,
		Provider:    sdkProviderToProto(sdkReq.Provider),
		InputType:   sdkInputTypeToProto(sdkReq.InputType),
		Metadata:    sdkReq.Metadata,
		Source:      sdkReq.Source,
		Language:    sdkReq.Language,
		CacheBypass: sdkReq.CacheBypass,
	}

	pbResp, err := client.EmbeddingGenerate(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "embeddingHandler.handleGenerateEmbedding")
	}

	return &embedding.GenerateEmbeddingResponse{
		Embedding: pbResp.Embedding,
		Metadata:  protoMetadataToSDK(pbResp.Metadata),
		Timestamp: pbResp.Timestamp.AsTime(),
	}, nil
}

// handleBatchGenerateEmbeddings handles a BatchGenerateEmbeddings request.
func (h *embeddingHandler) handleBatchGenerateEmbeddings(ctx context.Context, client pb.EmbeddingServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*embedding.BatchGenerateEmbeddingsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("embeddingHandler.handleBatchGenerateEmbeddings", "invalid request type")
	}

	pbItems := make([]*pb.EmbeddingBatchItem, len(sdkReq.Items))
	for i, item := range sdkReq.Items {
		pbItems[i] = &pb.EmbeddingBatchItem{
			Id:        item.ID,
			Text:      item.Text,
			InputType: sdkInputTypeToProto(item.InputType),
			Metadata:  item.Metadata,
			Source:    item.Source,
			Language:  item.Language,
		}
	}

	pbReq := &pb.EmbeddingBatchGenerateRequest{
		Items:       pbItems,
		Provider:    sdkProviderToProto(sdkReq.Provider),
		CacheBypass: sdkReq.CacheBypass,
	}

	pbResp, err := client.EmbeddingBatchGenerate(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "embeddingHandler.handleBatchGenerateEmbeddings")
	}

	results := make([]*embedding.BatchEmbeddingResult, len(pbResp.Results))
	for i, r := range pbResp.Results {
		results[i] = &embedding.BatchEmbeddingResult{
			ID:        r.Id,
			Embedding: r.Embedding,
			Metadata:  protoMetadataToSDK(r.Metadata),
			Error:     r.Error,
		}
	}

	return &embedding.BatchGenerateEmbeddingsResponse{
		Results:        results,
		SuccessCount:   int(pbResp.SuccessCount),
		ErrorCount:     int(pbResp.ErrorCount),
		TotalTokens:    int(pbResp.TotalTokens),
		ProcessingTime: pbResp.ProcessingTime.AsDuration(),
		Timestamp:      pbResp.Timestamp.AsTime(),
	}, nil
}

// handleListProviders handles a ListProviders request.
func (h *embeddingHandler) handleListProviders(ctx context.Context, client pb.EmbeddingServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*embedding.ListProvidersRequest)
	if !ok {
		return nil, errors.NewInvalidInput("embeddingHandler.handleListProviders", "invalid request type")
	}

	pbReq := &pb.EmbeddingListProvidersRequest{
		IncludeUnavailable: sdkReq.IncludeUnavailable,
	}

	pbResp, err := client.EmbeddingListProviders(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "embeddingHandler.handleListProviders")
	}

	providers := make([]*embedding.ProviderInfo, len(pbResp.Providers))
	for i, p := range pbResp.Providers {
		providers[i] = protoProviderInfoToSDK(p)
	}

	return &embedding.ListProvidersResponse{
		Providers:       providers,
		DefaultProvider: pbResp.DefaultProvider,
		Timestamp:       pbResp.Timestamp.AsTime(),
	}, nil
}

// handleGetProviderInfo handles a GetProviderInfo request.
func (h *embeddingHandler) handleGetProviderInfo(ctx context.Context, client pb.EmbeddingServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*embedding.GetProviderInfoRequest)
	if !ok {
		return nil, errors.NewInvalidInput("embeddingHandler.handleGetProviderInfo", "invalid request type")
	}

	pbProvider := sdkProviderToProto(sdkReq.Provider)

	// A non-empty provider string that maps to UNSPECIFIED is unrecognized.
	// Return early to avoid sending an invalid request to the server.
	if sdkReq.Provider != "" && pbProvider == pb.EmbeddingProvider_EMBEDDING_PROVIDER_UNSPECIFIED {
		return nil, errors.NewNotFound("embeddingHandler.handleGetProviderInfo",
			"unknown embedding provider: "+string(sdkReq.Provider))
	}

	pbReq := &pb.EmbeddingGetProviderInfoRequest{
		Provider: pbProvider,
	}

	pbResp, err := client.EmbeddingGetProviderInfo(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "embeddingHandler.handleGetProviderInfo")
	}

	return &embedding.GetProviderInfoResponse{
		Info:      protoProviderInfoToSDK(pbResp.Info),
		Timestamp: pbResp.Timestamp.AsTime(),
	}, nil
}

// handleSetDefaultProvider handles a SetDefaultProvider request.
func (h *embeddingHandler) handleSetDefaultProvider(ctx context.Context, client pb.EmbeddingServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*embedding.SetDefaultProviderRequest)
	if !ok {
		return nil, errors.NewInvalidInput("embeddingHandler.handleSetDefaultProvider", "invalid request type")
	}

	pbProvider := sdkProviderToProto(sdkReq.Provider)

	// A non-empty provider string that maps to UNSPECIFIED is unrecognized.
	// Return early to avoid sending an invalid request to the server.
	if sdkReq.Provider != "" && pbProvider == pb.EmbeddingProvider_EMBEDDING_PROVIDER_UNSPECIFIED {
		return nil, errors.NewNotFound("embeddingHandler.handleSetDefaultProvider",
			"unknown embedding provider: "+string(sdkReq.Provider))
	}

	pbReq := &pb.EmbeddingSetDefaultProviderRequest{
		Provider: pbProvider,
	}

	pbResp, err := client.EmbeddingSetDefaultProvider(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "embeddingHandler.handleSetDefaultProvider")
	}

	return &embedding.SetDefaultProviderResponse{
		Provider:         protoProviderToSDK(pbResp.Provider),
		PreviousProvider: protoProviderToSDK(pbResp.PreviousProvider),
		Timestamp:        pbResp.Timestamp.AsTime(),
	}, nil
}

// handleGetCacheStats handles a GetCacheStats request.
func (h *embeddingHandler) handleGetCacheStats(ctx context.Context, client pb.EmbeddingServiceClient, req interface{}) (interface{}, error) {
	_, ok := req.(*embedding.GetEmbeddingCacheStatsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("embeddingHandler.handleGetCacheStats", "invalid request type")
	}

	pbReq := &pb.EmbeddingGetCacheStatsRequest{}

	pbResp, err := client.EmbeddingGetCacheStats(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "embeddingHandler.handleGetCacheStats")
	}

	return &embedding.GetEmbeddingCacheStatsResponse{
		Enabled:            pbResp.Enabled,
		Size:               int(pbResp.Size),
		Capacity:           int(pbResp.Capacity),
		Hits:               pbResp.Hits,
		Misses:             pbResp.Misses,
		HitRate:            pbResp.HitRate,
		Evictions:          pbResp.Evictions,
		MemoryBytes:        pbResp.MemoryBytes,
		CompressionEnabled: pbResp.CompressionEnabled,
		CompressionRatio:   pbResp.CompressionRatio,
		Timestamp:          pbResp.Timestamp.AsTime(),
	}, nil
}

// handleClearCache handles a ClearCache request.
func (h *embeddingHandler) handleClearCache(ctx context.Context, client pb.EmbeddingServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*embedding.ClearEmbeddingCacheRequest)
	if !ok {
		return nil, errors.NewInvalidInput("embeddingHandler.handleClearCache", "invalid request type")
	}

	pbReq := &pb.EmbeddingClearCacheRequest{
		Provider: sdkProviderToProto(sdkReq.Provider),
		All:      sdkReq.All,
	}

	pbResp, err := client.EmbeddingClearCache(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "embeddingHandler.handleClearCache")
	}

	return &embedding.ClearEmbeddingCacheResponse{
		EntriesCleared: int(pbResp.EntriesCleared),
		Provider:       protoProviderToSDK(pbResp.Provider),
		Timestamp:      pbResp.Timestamp.AsTime(),
	}, nil
}

// sdkProviderToProto converts SDK Provider to proto EmbeddingProvider.
func sdkProviderToProto(p embedding.Provider) pb.EmbeddingProvider {
	switch p {
	case embedding.ProviderVoyage:
		return pb.EmbeddingProvider_EMBEDDING_PROVIDER_VOYAGE
	case embedding.ProviderOpenAI:
		return pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI
	case embedding.ProviderOllama:
		return pb.EmbeddingProvider_EMBEDDING_PROVIDER_OLLAMA
	case embedding.ProviderLMStudio:
		return pb.EmbeddingProvider_EMBEDDING_PROVIDER_LMSTUDIO
	default:
		return pb.EmbeddingProvider_EMBEDDING_PROVIDER_UNSPECIFIED
	}
}

// protoProviderToSDK converts proto EmbeddingProvider to SDK Provider.
func protoProviderToSDK(p pb.EmbeddingProvider) embedding.Provider {
	switch p {
	case pb.EmbeddingProvider_EMBEDDING_PROVIDER_VOYAGE:
		return embedding.ProviderVoyage
	case pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI:
		return embedding.ProviderOpenAI
	case pb.EmbeddingProvider_EMBEDDING_PROVIDER_OLLAMA:
		return embedding.ProviderOllama
	case pb.EmbeddingProvider_EMBEDDING_PROVIDER_LMSTUDIO:
		return embedding.ProviderLMStudio
	default:
		return ""
	}
}

// sdkInputTypeToProto converts SDK InputType to proto EmbeddingInputType.
func sdkInputTypeToProto(t embedding.InputType) pb.EmbeddingInputType {
	switch t {
	case embedding.InputTypeDocument:
		return pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_DOCUMENT
	case embedding.InputTypeQuery:
		return pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_QUERY
	case embedding.InputTypeCode:
		return pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_CODE
	default:
		return pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_UNSPECIFIED
	}
}

// protoInputTypeToSDK converts proto EmbeddingInputType to SDK InputType.
func protoInputTypeToSDK(t pb.EmbeddingInputType) embedding.InputType {
	switch t {
	case pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_DOCUMENT:
		return embedding.InputTypeDocument
	case pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_QUERY:
		return embedding.InputTypeQuery
	case pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_CODE:
		return embedding.InputTypeCode
	default:
		return ""
	}
}

// protoStatusToSDK converts proto EmbeddingStatus to SDK Status.
func protoStatusToSDK(s pb.EmbeddingStatus) embedding.Status {
	switch s {
	case pb.EmbeddingStatus_EMBEDDING_STATUS_AVAILABLE:
		return embedding.StatusAvailable
	case pb.EmbeddingStatus_EMBEDDING_STATUS_UNAVAILABLE:
		return embedding.StatusUnavailable
	case pb.EmbeddingStatus_EMBEDDING_STATUS_RATE_LIMITED:
		return embedding.StatusRateLimited
	case pb.EmbeddingStatus_EMBEDDING_STATUS_ERROR:
		return embedding.StatusError
	default:
		return ""
	}
}

// protoMetadataToSDK converts proto EmbeddingMetadata to SDK Metadata.
func protoMetadataToSDK(m *pb.EmbeddingMetadata) *embedding.Metadata {
	if m == nil {
		return nil
	}

	var processingTime time.Duration
	if m.ProcessingTime != nil {
		processingTime = m.ProcessingTime.AsDuration()
	}

	return &embedding.Metadata{
		Model:          m.Model,
		Provider:       protoProviderToSDK(m.Provider),
		Dimensions:     int(m.Dimensions),
		Tokens:         int(m.Tokens),
		Source:         m.Source,
		Language:       m.Language,
		InputType:      protoInputTypeToSDK(m.InputType),
		Tags:           m.Tags,
		CacheHit:       m.CacheHit,
		ProcessingTime: processingTime,
	}
}

// protoProviderInfoToSDK converts proto EmbeddingProviderInfo to SDK ProviderInfo.
func protoProviderInfoToSDK(p *pb.EmbeddingProviderInfo) *embedding.ProviderInfo {
	if p == nil {
		return nil
	}

	var timeout time.Duration
	if p.Timeout != nil {
		timeout = p.Timeout.AsDuration()
	}

	var avgLatency time.Duration
	if p.AvgLatency != nil {
		avgLatency = p.AvgLatency.AsDuration()
	}

	var lastErrorTime *time.Time
	if p.LastErrorTime != nil {
		t := p.LastErrorTime.AsTime()
		lastErrorTime = &t
	}

	return &embedding.ProviderInfo{
		Name:               p.Name,
		Type:               protoProviderToSDK(p.Type),
		Model:              p.Model,
		Dimensions:         int(p.Dimensions),
		MaxTokens:          int(p.MaxTokens),
		MaxBatchSize:       int(p.MaxBatchSize),
		Status:             protoStatusToSDK(p.Status),
		APIURL:             p.ApiUrl,
		RateLimitPerMinute: int(p.RateLimitPerMinute),
		Timeout:            timeout,
		IsDefault:          p.IsDefault,
		LastError:          p.LastError,
		LastErrorTime:      lastErrorTime,
		SuccessCount:       p.SuccessCount,
		ErrorCount:         p.ErrorCount,
		AvgLatency:         avgLatency,
	}
}
