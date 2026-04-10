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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// EmbeddingGenerate generates an embedding vector for a single text.
func (s *Server) EmbeddingGenerate(ctx context.Context, req *pb.EmbeddingGenerateRequest) (*pb.EmbeddingGenerateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Text == "" {
		return nil, status.Error(codes.InvalidArgument, "text is required")
	}

	// Get embedding service
	embeddingService := s.service.Embedding()
	if embeddingService == nil {
		return nil, status.Error(codes.Unimplemented, "embedding service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.GenerateEmbeddingRequest{
		Text:        req.Text,
		Provider:    protoToServiceProvider(req.Provider),
		InputType:   protoToServiceInputType(req.InputType),
		Metadata:    req.Metadata,
		Source:      req.Source,
		Language:    req.Language,
		CacheBypass: req.CacheBypass,
	}

	// Call service
	resp, err := embeddingService.GenerateEmbedding(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	// Convert service response to proto response
	return &pb.EmbeddingGenerateResponse{
		Embedding: resp.Embedding,
		Metadata:  serviceMetadataToProto(resp.Metadata),
		Timestamp: timestamppb.New(resp.Timestamp),
	}, nil
}

// EmbeddingBatchGenerate generates embeddings for multiple texts in a single request.
func (s *Server) EmbeddingBatchGenerate(ctx context.Context, req *pb.EmbeddingBatchGenerateRequest) (*pb.EmbeddingBatchGenerateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if len(req.Items) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no items provided")
	}

	// Get embedding service
	embeddingService := s.service.Embedding()
	if embeddingService == nil {
		return nil, status.Error(codes.Unimplemented, "embedding service not available")
	}

	// Convert proto items to service items
	serviceItems := make([]*service.BatchEmbeddingItem, len(req.Items))
	for i, item := range req.Items {
		serviceItems[i] = &service.BatchEmbeddingItem{
			ID:        item.Id,
			Text:      item.Text,
			InputType: protoToServiceInputType(item.InputType),
			Metadata:  item.Metadata,
			Source:    item.Source,
			Language:  item.Language,
		}
	}

	// Convert proto request to service request
	serviceReq := &service.BatchGenerateEmbeddingsRequest{
		Items:       serviceItems,
		Provider:    protoToServiceProvider(req.Provider),
		CacheBypass: req.CacheBypass,
	}

	// Call service
	resp, err := embeddingService.BatchGenerateEmbeddings(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	// Convert service results to proto results
	protoResults := make([]*pb.EmbeddingBatchResult, len(resp.Results))
	for i, result := range resp.Results {
		protoResults[i] = &pb.EmbeddingBatchResult{
			Id:        result.ID,
			Embedding: result.Embedding,
			Metadata:  serviceMetadataToProto(result.Metadata),
			Error:     result.Error,
		}
	}

	// Clamp counts to valid int32 range
	successCount := resp.SuccessCount
	if successCount > 0x7FFFFFFF {
		successCount = 0x7FFFFFFF
	}
	errorCount := resp.ErrorCount
	if errorCount > 0x7FFFFFFF {
		errorCount = 0x7FFFFFFF
	}
	totalTokens := resp.TotalTokens
	if totalTokens > 0x7FFFFFFF {
		totalTokens = 0x7FFFFFFF
	}

	return &pb.EmbeddingBatchGenerateResponse{
		Results:        protoResults,
		SuccessCount:   int32(successCount), // #nosec G115 - safe after bounds check
		ErrorCount:     int32(errorCount),   // #nosec G115 - safe after bounds check
		TotalTokens:    int32(totalTokens),  // #nosec G115 - safe after bounds check
		ProcessingTime: durationpb.New(resp.ProcessingTime),
		Timestamp:      timestamppb.New(resp.Timestamp),
	}, nil
}

// EmbeddingListProviders returns all registered embedding providers with their capabilities.
func (s *Server) EmbeddingListProviders(ctx context.Context, req *pb.EmbeddingListProvidersRequest) (*pb.EmbeddingListProvidersResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Get embedding service
	embeddingService := s.service.Embedding()
	if embeddingService == nil {
		return nil, status.Error(codes.Unimplemented, "embedding service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.ListProvidersRequest{
		IncludeUnavailable: req.IncludeUnavailable,
	}

	// Call service
	resp, err := embeddingService.ListProviders(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	// Convert service providers to proto providers
	protoProviders := make([]*pb.EmbeddingProviderInfo, len(resp.Providers))
	for i, provider := range resp.Providers {
		protoProviders[i] = serviceProviderInfoToProto(provider)
	}

	return &pb.EmbeddingListProvidersResponse{
		Providers:       protoProviders,
		DefaultProvider: resp.DefaultProvider,
		Timestamp:       timestamppb.New(resp.Timestamp),
	}, nil
}

// EmbeddingGetProviderInfo returns detailed information about a specific provider.
func (s *Server) EmbeddingGetProviderInfo(ctx context.Context, req *pb.EmbeddingGetProviderInfoRequest) (*pb.EmbeddingGetProviderInfoResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Provider == pb.EmbeddingProvider_EMBEDDING_PROVIDER_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "provider is required")
	}

	// Get embedding service
	embeddingService := s.service.Embedding()
	if embeddingService == nil {
		return nil, status.Error(codes.Unimplemented, "embedding service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.GetProviderInfoRequest{
		Provider: protoToServiceProvider(req.Provider),
	}

	// Call service
	resp, err := embeddingService.GetProviderInfo(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.EmbeddingGetProviderInfoResponse{
		Info:      serviceProviderInfoToProto(resp.Info),
		Timestamp: timestamppb.New(resp.Timestamp),
	}, nil
}

// EmbeddingSetDefaultProvider sets the default embedding provider.
func (s *Server) EmbeddingSetDefaultProvider(ctx context.Context, req *pb.EmbeddingSetDefaultProviderRequest) (*pb.EmbeddingSetDefaultProviderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Provider == pb.EmbeddingProvider_EMBEDDING_PROVIDER_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "provider is required")
	}

	// Get embedding service
	embeddingService := s.service.Embedding()
	if embeddingService == nil {
		return nil, status.Error(codes.Unimplemented, "embedding service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.SetDefaultProviderRequest{
		Provider: protoToServiceProvider(req.Provider),
	}

	// Call service
	resp, err := embeddingService.SetDefaultProvider(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.EmbeddingSetDefaultProviderResponse{
		Provider:         serviceToProtoProvider(resp.Provider),
		PreviousProvider: serviceToProtoProvider(resp.PreviousProvider),
		Timestamp:        timestamppb.New(resp.Timestamp),
	}, nil
}

// EmbeddingGetCacheStats returns statistics about the embedding cache.
func (s *Server) EmbeddingGetCacheStats(ctx context.Context, req *pb.EmbeddingGetCacheStatsRequest) (*pb.EmbeddingGetCacheStatsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Get embedding service
	embeddingService := s.service.Embedding()
	if embeddingService == nil {
		return nil, status.Error(codes.Unimplemented, "embedding service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.GetEmbeddingCacheStatsRequest{}

	// Call service
	resp, err := embeddingService.GetCacheStats(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	// Clamp size and capacity to valid int32 range
	size := resp.Size
	if size > 0x7FFFFFFF {
		size = 0x7FFFFFFF
	}
	capacity := resp.Capacity
	if capacity > 0x7FFFFFFF {
		capacity = 0x7FFFFFFF
	}

	return &pb.EmbeddingGetCacheStatsResponse{
		Enabled:            resp.Enabled,
		Size:               int32(size),     // #nosec G115 - safe after bounds check
		Capacity:           int32(capacity), // #nosec G115 - safe after bounds check
		Hits:               resp.Hits,
		Misses:             resp.Misses,
		HitRate:            resp.HitRate,
		Evictions:          resp.Evictions,
		MemoryBytes:        resp.MemoryBytes,
		CompressionEnabled: resp.CompressionEnabled,
		CompressionRatio:   resp.CompressionRatio,
		Timestamp:          timestamppb.New(resp.Timestamp),
	}, nil
}

// EmbeddingClearCache invalidates all or specific embedding cache entries.
func (s *Server) EmbeddingClearCache(ctx context.Context, req *pb.EmbeddingClearCacheRequest) (*pb.EmbeddingClearCacheResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Get embedding service
	embeddingService := s.service.Embedding()
	if embeddingService == nil {
		return nil, status.Error(codes.Unimplemented, "embedding service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.ClearEmbeddingCacheRequest{
		Provider: protoToServiceProvider(req.Provider),
		All:      req.All,
	}

	// Call service
	resp, err := embeddingService.ClearCache(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	// Clamp entries cleared to valid int32 range
	entriesCleared := resp.EntriesCleared
	if entriesCleared > 0x7FFFFFFF {
		entriesCleared = 0x7FFFFFFF
	}

	return &pb.EmbeddingClearCacheResponse{
		EntriesCleared: int32(entriesCleared), // #nosec G115 - safe after bounds check
		Provider:       serviceToProtoProvider(resp.Provider),
		Timestamp:      timestamppb.New(resp.Timestamp),
	}, nil
}

// =============================================================================
// Conversion Helpers
// =============================================================================

// protoToServiceProvider converts proto EmbeddingProvider to service EmbeddingProvider.
func protoToServiceProvider(p pb.EmbeddingProvider) service.EmbeddingProvider {
	switch p {
	case pb.EmbeddingProvider_EMBEDDING_PROVIDER_VOYAGE:
		return service.EmbeddingProviderVoyage
	case pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI:
		return service.EmbeddingProviderOpenAI
	case pb.EmbeddingProvider_EMBEDDING_PROVIDER_OLLAMA:
		return service.EmbeddingProviderOllama
	case pb.EmbeddingProvider_EMBEDDING_PROVIDER_LMSTUDIO:
		return service.EmbeddingProviderLMStudio
	default:
		return ""
	}
}

// serviceToProtoProvider converts service EmbeddingProvider to proto EmbeddingProvider.
func serviceToProtoProvider(p service.EmbeddingProvider) pb.EmbeddingProvider {
	switch p {
	case service.EmbeddingProviderVoyage:
		return pb.EmbeddingProvider_EMBEDDING_PROVIDER_VOYAGE
	case service.EmbeddingProviderOpenAI:
		return pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI
	case service.EmbeddingProviderOllama:
		return pb.EmbeddingProvider_EMBEDDING_PROVIDER_OLLAMA
	case service.EmbeddingProviderLMStudio:
		return pb.EmbeddingProvider_EMBEDDING_PROVIDER_LMSTUDIO
	default:
		return pb.EmbeddingProvider_EMBEDDING_PROVIDER_UNSPECIFIED
	}
}

// protoToServiceInputType converts proto EmbeddingInputType to service EmbeddingInputType.
func protoToServiceInputType(t pb.EmbeddingInputType) service.EmbeddingInputType {
	switch t {
	case pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_DOCUMENT:
		return service.EmbeddingInputTypeDocument
	case pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_QUERY:
		return service.EmbeddingInputTypeQuery
	case pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_CODE:
		return service.EmbeddingInputTypeCode
	default:
		return ""
	}
}

// serviceToProtoInputType converts service EmbeddingInputType to proto EmbeddingInputType.
func serviceToProtoInputType(t service.EmbeddingInputType) pb.EmbeddingInputType {
	switch t {
	case service.EmbeddingInputTypeDocument:
		return pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_DOCUMENT
	case service.EmbeddingInputTypeQuery:
		return pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_QUERY
	case service.EmbeddingInputTypeCode:
		return pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_CODE
	default:
		return pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_UNSPECIFIED
	}
}

// serviceToProtoStatus converts service EmbeddingStatus to proto EmbeddingStatus.
func serviceToProtoStatus(s service.EmbeddingStatus) pb.EmbeddingStatus {
	switch s {
	case service.EmbeddingStatusAvailable:
		return pb.EmbeddingStatus_EMBEDDING_STATUS_AVAILABLE
	case service.EmbeddingStatusUnavailable:
		return pb.EmbeddingStatus_EMBEDDING_STATUS_UNAVAILABLE
	case service.EmbeddingStatusRateLimited:
		return pb.EmbeddingStatus_EMBEDDING_STATUS_RATE_LIMITED
	case service.EmbeddingStatusError:
		return pb.EmbeddingStatus_EMBEDDING_STATUS_ERROR
	default:
		return pb.EmbeddingStatus_EMBEDDING_STATUS_UNSPECIFIED
	}
}

// serviceMetadataToProto converts service EmbeddingMetadata to proto EmbeddingMetadata.
func serviceMetadataToProto(m *service.EmbeddingMetadata) *pb.EmbeddingMetadata {
	if m == nil {
		return nil
	}

	return &pb.EmbeddingMetadata{
		Model:          m.Model,
		Provider:       serviceToProtoProvider(m.Provider),
		Dimensions:     int32(m.Dimensions), // #nosec G115 - dimensions are small positive integers
		Tokens:         int32(m.Tokens),     // #nosec G115 - tokens are small positive integers
		Source:         m.Source,
		Language:       m.Language,
		InputType:      serviceToProtoInputType(m.InputType),
		Tags:           m.Tags,
		CacheHit:       m.CacheHit,
		ProcessingTime: durationpb.New(m.ProcessingTime),
	}
}

// serviceProviderInfoToProto converts service ProviderInfo to proto EmbeddingProviderInfo.
func serviceProviderInfoToProto(p *service.ProviderInfo) *pb.EmbeddingProviderInfo {
	if p == nil {
		return nil
	}

	protoInfo := &pb.EmbeddingProviderInfo{
		Name:               p.Name,
		Type:               serviceToProtoProvider(p.Type),
		Model:              p.Model,
		Dimensions:         int32(p.Dimensions),   // #nosec G115 - dimensions are small positive integers
		MaxTokens:          int32(p.MaxTokens),    // #nosec G115 - max tokens are small positive integers
		MaxBatchSize:       int32(p.MaxBatchSize), // #nosec G115 - batch size is small positive integer
		Status:             serviceToProtoStatus(p.Status),
		ApiUrl:             p.APIURL,
		RateLimitPerMinute: int32(p.RateLimitPerMinute), // #nosec G115 - rate limit is small positive integer
		Timeout:            durationpb.New(p.Timeout),
		IsDefault:          p.IsDefault,
		LastError:          p.LastError,
		SuccessCount:       p.SuccessCount,
		ErrorCount:         p.ErrorCount,
		AvgLatency:         durationpb.New(p.AvgLatency),
	}

	if p.LastErrorTime != nil {
		protoInfo.LastErrorTime = timestamppb.New(*p.LastErrorTime)
	}

	return protoInfo
}
