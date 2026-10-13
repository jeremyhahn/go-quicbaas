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
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
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
	var n responseNarrower
	out := &pb.EmbeddingGenerateResponse{
		Embedding: resp.Embedding,
		Metadata:  serviceMetadataToProto(&n, resp.Metadata),
		Timestamp: timestamppb.New(resp.Timestamp),
	}
	if err := n.err(); err != nil {
		return nil, err
	}
	return out, nil
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
	var n responseNarrower
	protoResults := make([]*pb.EmbeddingBatchResult, len(resp.Results))
	for i, result := range resp.Results {
		protoResults[i] = &pb.EmbeddingBatchResult{
			Id:        result.ID,
			Embedding: result.Embedding,
			Metadata:  serviceMetadataToProto(&n, result.Metadata),
			Error:     result.Error,
		}
	}

	out := &pb.EmbeddingBatchGenerateResponse{
		Results:        protoResults,
		SuccessCount:   n.int32("success_count", resp.SuccessCount),
		ErrorCount:     n.int32("error_count", resp.ErrorCount),
		TotalTokens:    n.int32("total_tokens", resp.TotalTokens),
		ProcessingTime: durationpb.New(resp.ProcessingTime),
		Timestamp:      timestamppb.New(resp.Timestamp),
	}
	if err := n.err(); err != nil {
		return nil, err
	}
	return out, nil
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
	var n responseNarrower
	protoProviders := make([]*pb.EmbeddingProviderInfo, len(resp.Providers))
	for i, provider := range resp.Providers {
		protoProviders[i] = serviceProviderInfoToProto(&n, provider)
	}
	if err := n.err(); err != nil {
		return nil, err
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

	var n responseNarrower
	info := serviceProviderInfoToProto(&n, resp.Info)
	if err := n.err(); err != nil {
		return nil, err
	}

	return &pb.EmbeddingGetProviderInfoResponse{
		Info:      info,
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

	var n responseNarrower
	size := n.int32("size", resp.Size)
	capacity := n.int32("capacity", resp.Capacity)
	if err := n.err(); err != nil {
		return nil, err
	}

	return &pb.EmbeddingGetCacheStatsResponse{
		Enabled:            resp.Enabled,
		Size:               size,
		Capacity:           capacity,
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

	entriesCleared, err := conv.SafeIntToInt32(resp.EntriesCleared)
	if err != nil {
		return nil, responseRangeError("entries_cleared", err)
	}

	return &pb.EmbeddingClearCacheResponse{
		EntriesCleared: entriesCleared,
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
	case pb.EmbeddingProvider_EMBEDDING_PROVIDER_NPU:
		return service.EmbeddingProviderNPU
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
	case service.EmbeddingProviderNPU:
		return pb.EmbeddingProvider_EMBEDDING_PROVIDER_NPU
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
func serviceMetadataToProto(n *responseNarrower, m *service.EmbeddingMetadata) *pb.EmbeddingMetadata {
	if m == nil {
		return nil
	}

	return &pb.EmbeddingMetadata{
		Model:          m.Model,
		Provider:       serviceToProtoProvider(m.Provider),
		Dimensions:     n.int32("metadata.dimensions", m.Dimensions),
		Tokens:         n.int32("metadata.tokens", m.Tokens),
		Source:         m.Source,
		Language:       m.Language,
		InputType:      serviceToProtoInputType(m.InputType),
		Tags:           m.Tags,
		CacheHit:       m.CacheHit,
		ProcessingTime: durationpb.New(m.ProcessingTime),
	}
}

// serviceProviderInfoToProto converts service ProviderInfo to proto EmbeddingProviderInfo.
func serviceProviderInfoToProto(n *responseNarrower, p *service.ProviderInfo) *pb.EmbeddingProviderInfo {
	if p == nil {
		return nil
	}

	protoInfo := &pb.EmbeddingProviderInfo{
		Name:               p.Name,
		Type:               serviceToProtoProvider(p.Type),
		Model:              p.Model,
		Dimensions:         n.int32("provider.dimensions", p.Dimensions),
		MaxTokens:          n.int32("provider.max_tokens", p.MaxTokens),
		MaxBatchSize:       n.int32("provider.max_batch_size", p.MaxBatchSize),
		Status:             serviceToProtoStatus(p.Status),
		ApiUrl:             p.APIURL,
		RateLimitPerMinute: n.int32("provider.rate_limit_per_minute", p.RateLimitPerMinute),
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
