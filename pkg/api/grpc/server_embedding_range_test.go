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
	"math"
	"testing"

	"google.golang.org/grpc/codes"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// TestEmbeddingHandlers_ResponseOutOfRange proves that embedding counts the
// service reports beyond the int32 wire range fail with Internal naming the
// field, instead of being clamped to a wrong value.
func TestEmbeddingHandlers_ResponseOutOfRange(t *testing.T) {
	const tooBig = math.MaxInt32 + 1

	tests := []struct {
		name  string
		svc   *mocks.MockEmbeddingService
		call  func(s *Server) error
		field string
	}{
		{
			name: "EmbeddingGenerate metadata.tokens",
			svc: &mocks.MockEmbeddingService{
				GenerateEmbeddingFunc: func(context.Context, *service.GenerateEmbeddingRequest) (*service.GenerateEmbeddingResponse, error) {
					return &service.GenerateEmbeddingResponse{Metadata: &service.EmbeddingMetadata{Tokens: tooBig}}, nil
				},
			},
			call: func(s *Server) error {
				_, err := s.EmbeddingGenerate(context.Background(), &pb.EmbeddingGenerateRequest{Text: "x"})
				return err
			},
			field: "metadata.tokens",
		},
		{
			name: "EmbeddingBatchGenerate metadata.dimensions",
			svc: &mocks.MockEmbeddingService{
				BatchGenerateEmbeddingsFunc: func(context.Context, *service.BatchGenerateEmbeddingsRequest) (*service.BatchGenerateEmbeddingsResponse, error) {
					return &service.BatchGenerateEmbeddingsResponse{
						Results: []*service.BatchEmbeddingResult{{Metadata: &service.EmbeddingMetadata{Dimensions: tooBig}}},
					}, nil
				},
			},
			call:  batchGenerateOne,
			field: "metadata.dimensions",
		},
		{
			name: "EmbeddingBatchGenerate total_tokens",
			svc: &mocks.MockEmbeddingService{
				BatchGenerateEmbeddingsFunc: func(context.Context, *service.BatchGenerateEmbeddingsRequest) (*service.BatchGenerateEmbeddingsResponse, error) {
					return &service.BatchGenerateEmbeddingsResponse{TotalTokens: tooBig}, nil
				},
			},
			call:  batchGenerateOne,
			field: "total_tokens",
		},
		{
			name: "EmbeddingListProviders provider.max_tokens",
			svc: &mocks.MockEmbeddingService{
				ListProvidersFunc: func(context.Context, *service.ListProvidersRequest) (*service.ListProvidersResponse, error) {
					return &service.ListProvidersResponse{Providers: []*service.ProviderInfo{{MaxTokens: tooBig}}}, nil
				},
			},
			call: func(s *Server) error {
				_, err := s.EmbeddingListProviders(context.Background(), &pb.EmbeddingListProvidersRequest{})
				return err
			},
			field: "provider.max_tokens",
		},
		{
			name: "EmbeddingGetProviderInfo provider.rate_limit_per_minute",
			svc: &mocks.MockEmbeddingService{
				GetProviderInfoFunc: func(context.Context, *service.GetProviderInfoRequest) (*service.GetProviderInfoResponse, error) {
					return &service.GetProviderInfoResponse{Info: &service.ProviderInfo{RateLimitPerMinute: tooBig}}, nil
				},
			},
			call: func(s *Server) error {
				_, err := s.EmbeddingGetProviderInfo(context.Background(), &pb.EmbeddingGetProviderInfoRequest{
					Provider: pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI,
				})
				return err
			},
			field: "provider.rate_limit_per_minute",
		},
		{
			name: "EmbeddingGetCacheStats capacity",
			svc: &mocks.MockEmbeddingService{
				GetCacheStatsFunc: func(context.Context, *service.GetEmbeddingCacheStatsRequest) (*service.GetEmbeddingCacheStatsResponse, error) {
					return &service.GetEmbeddingCacheStatsResponse{Capacity: tooBig}, nil
				},
			},
			call: func(s *Server) error {
				_, err := s.EmbeddingGetCacheStats(context.Background(), &pb.EmbeddingGetCacheStatsRequest{})
				return err
			},
			field: "capacity",
		},
		{
			name: "EmbeddingClearCache entries_cleared",
			svc: &mocks.MockEmbeddingService{
				ClearCacheFunc: func(context.Context, *service.ClearEmbeddingCacheRequest) (*service.ClearEmbeddingCacheResponse, error) {
					return &service.ClearEmbeddingCacheResponse{EntriesCleared: tooBig}, nil
				},
			},
			call: func(s *Server) error {
				_, err := s.EmbeddingClearCache(context.Background(), &pb.EmbeddingClearCacheRequest{All: true})
				return err
			},
			field: "entries_cleared",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			mockSvc.EmbeddingService = tt.svc

			requireRangeStatus(t, tt.call(srv), codes.Internal, tt.field)
		})
	}
}

func batchGenerateOne(s *Server) error {
	_, err := s.EmbeddingBatchGenerate(context.Background(), &pb.EmbeddingBatchGenerateRequest{
		Items: []*pb.EmbeddingBatchItem{{Id: "1", Text: "x"}},
	})
	return err
}
