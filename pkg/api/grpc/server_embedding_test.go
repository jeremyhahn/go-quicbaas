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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// =============================================================================
// EmbeddingGenerate Tests
// =============================================================================

func TestServer_EmbeddingGenerate(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.EmbeddingGenerate(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty_text", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.EmbeddingGenerate(context.Background(), &pb.EmbeddingGenerateRequest{
			Text: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		// MockQuicBaaS.Embedding() returns nil
		_, err := srv.EmbeddingGenerate(context.Background(), &pb.EmbeddingGenerateRequest{
			Text:     "hello world",
			Provider: pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI,
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		now := time.Now()
		mockSvc.EmbeddingService = &mocks.MockEmbeddingService{
			GenerateEmbeddingFunc: func(ctx context.Context, req *service.GenerateEmbeddingRequest) (*service.GenerateEmbeddingResponse, error) {
				assert.Equal(t, "hello world", req.Text)
				assert.Equal(t, service.EmbeddingProviderOpenAI, req.Provider)
				assert.Equal(t, service.EmbeddingInputTypeDocument, req.InputType)
				return &service.GenerateEmbeddingResponse{
					Embedding: []float32{0.1, 0.2, 0.3},
					Metadata: &service.EmbeddingMetadata{
						Model:      "text-embedding-3-small",
						Provider:   service.EmbeddingProviderOpenAI,
						Dimensions: 3,
						Tokens:     2,
					},
					Timestamp: now,
				}, nil
			},
		}

		resp, err := srv.EmbeddingGenerate(context.Background(), &pb.EmbeddingGenerateRequest{
			Text:      "hello world",
			Provider:  pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI,
			InputType: pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_DOCUMENT,
			Source:    "test",
			Language:  "en",
			Metadata:  map[string]string{"key": "val"},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, []float32{0.1, 0.2, 0.3}, resp.Embedding)
		require.NotNil(t, resp.Metadata)
		assert.Equal(t, "text-embedding-3-small", resp.Metadata.Model)
		assert.Equal(t, int32(3), resp.Metadata.Dimensions)
		assert.NotNil(t, resp.Timestamp)
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.EmbeddingService = &mocks.MockEmbeddingService{
			GenerateEmbeddingFunc: func(ctx context.Context, req *service.GenerateEmbeddingRequest) (*service.GenerateEmbeddingResponse, error) {
				return nil, errors.New("provider unavailable")
			},
		}

		_, err := srv.EmbeddingGenerate(context.Background(), &pb.EmbeddingGenerateRequest{
			Text: "hello",
		})
		require.Error(t, err)
	})
}

// =============================================================================
// EmbeddingBatchGenerate Tests
// =============================================================================

func TestServer_EmbeddingBatchGenerate(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.EmbeddingBatchGenerate(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty_items", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.EmbeddingBatchGenerate(context.Background(), &pb.EmbeddingBatchGenerateRequest{
			Items: nil,
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.EmbeddingBatchGenerate(context.Background(), &pb.EmbeddingBatchGenerateRequest{
			Items: []*pb.EmbeddingBatchItem{
				{Id: "1", Text: "hello"},
			},
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		now := time.Now()
		mockSvc.EmbeddingService = &mocks.MockEmbeddingService{
			BatchGenerateEmbeddingsFunc: func(ctx context.Context, req *service.BatchGenerateEmbeddingsRequest) (*service.BatchGenerateEmbeddingsResponse, error) {
				assert.Len(t, req.Items, 2)
				assert.Equal(t, "item-1", req.Items[0].ID)
				assert.Equal(t, "hello", req.Items[0].Text)
				return &service.BatchGenerateEmbeddingsResponse{
					Results: []*service.BatchEmbeddingResult{
						{ID: "item-1", Embedding: []float32{0.1, 0.2}, Metadata: &service.EmbeddingMetadata{Model: "m1"}},
						{ID: "item-2", Embedding: []float32{0.3, 0.4}, Error: "rate limited"},
					},
					SuccessCount:   1,
					ErrorCount:     1,
					TotalTokens:    10,
					ProcessingTime: 50 * time.Millisecond,
					Timestamp:      now,
				}, nil
			},
		}

		resp, err := srv.EmbeddingBatchGenerate(context.Background(), &pb.EmbeddingBatchGenerateRequest{
			Items: []*pb.EmbeddingBatchItem{
				{Id: "item-1", Text: "hello", InputType: pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_QUERY, Metadata: map[string]string{"k": "v"}, Source: "src", Language: "en"},
				{Id: "item-2", Text: "world"},
			},
			Provider:    pb.EmbeddingProvider_EMBEDDING_PROVIDER_VOYAGE,
			CacheBypass: true,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Results, 2)
		assert.Equal(t, "item-1", resp.Results[0].Id)
		assert.Equal(t, []float32{0.1, 0.2}, resp.Results[0].Embedding)
		assert.Equal(t, "rate limited", resp.Results[1].Error)
		assert.Equal(t, int32(1), resp.SuccessCount)
		assert.Equal(t, int32(1), resp.ErrorCount)
		assert.Equal(t, int32(10), resp.TotalTokens)
		assert.NotNil(t, resp.ProcessingTime)
		assert.NotNil(t, resp.Timestamp)
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.EmbeddingService = &mocks.MockEmbeddingService{
			BatchGenerateEmbeddingsFunc: func(ctx context.Context, req *service.BatchGenerateEmbeddingsRequest) (*service.BatchGenerateEmbeddingsResponse, error) {
				return nil, errors.New("batch failed")
			},
		}

		_, err := srv.EmbeddingBatchGenerate(context.Background(), &pb.EmbeddingBatchGenerateRequest{
			Items: []*pb.EmbeddingBatchItem{{Id: "1", Text: "hello"}},
		})
		require.Error(t, err)
	})
}

// =============================================================================
// EmbeddingListProviders Tests
// =============================================================================

func TestServer_EmbeddingListProviders(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.EmbeddingListProviders(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.EmbeddingListProviders(context.Background(), &pb.EmbeddingListProvidersRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		now := time.Now()
		mockSvc.EmbeddingService = &mocks.MockEmbeddingService{
			ListProvidersFunc: func(ctx context.Context, req *service.ListProvidersRequest) (*service.ListProvidersResponse, error) {
				assert.True(t, req.IncludeUnavailable)
				return &service.ListProvidersResponse{
					Providers: []*service.ProviderInfo{
						{Name: "openai", Type: service.EmbeddingProviderOpenAI, Status: service.EmbeddingStatusAvailable, Dimensions: 1536},
						{Name: "voyage", Type: service.EmbeddingProviderVoyage, Status: service.EmbeddingStatusUnavailable},
					},
					DefaultProvider: "openai",
					Timestamp:       now,
				}, nil
			},
		}

		resp, err := srv.EmbeddingListProviders(context.Background(), &pb.EmbeddingListProvidersRequest{
			IncludeUnavailable: true,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Providers, 2)
		assert.Equal(t, "openai", resp.Providers[0].Name)
		assert.Equal(t, "openai", resp.DefaultProvider)
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.EmbeddingService = &mocks.MockEmbeddingService{
			ListProvidersFunc: func(ctx context.Context, req *service.ListProvidersRequest) (*service.ListProvidersResponse, error) {
				return nil, errors.New("internal error")
			},
		}

		_, err := srv.EmbeddingListProviders(context.Background(), &pb.EmbeddingListProvidersRequest{})
		require.Error(t, err)
	})
}

// =============================================================================
// EmbeddingGetProviderInfo Tests
// =============================================================================

func TestServer_EmbeddingGetProviderInfo(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.EmbeddingGetProviderInfo(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("unspecified_provider", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.EmbeddingGetProviderInfo(context.Background(), &pb.EmbeddingGetProviderInfoRequest{
			Provider: pb.EmbeddingProvider_EMBEDDING_PROVIDER_UNSPECIFIED,
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.EmbeddingGetProviderInfo(context.Background(), &pb.EmbeddingGetProviderInfoRequest{
			Provider: pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI,
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		now := time.Now()
		mockSvc.EmbeddingService = &mocks.MockEmbeddingService{
			GetProviderInfoFunc: func(ctx context.Context, req *service.GetProviderInfoRequest) (*service.GetProviderInfoResponse, error) {
				assert.Equal(t, service.EmbeddingProviderOpenAI, req.Provider)
				return &service.GetProviderInfoResponse{
					Info: &service.ProviderInfo{
						Name:       "openai",
						Type:       service.EmbeddingProviderOpenAI,
						Dimensions: 1536,
						Status:     service.EmbeddingStatusAvailable,
					},
					Timestamp: now,
				}, nil
			},
		}

		resp, err := srv.EmbeddingGetProviderInfo(context.Background(), &pb.EmbeddingGetProviderInfoRequest{
			Provider: pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Info)
		assert.Equal(t, "openai", resp.Info.Name)
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.EmbeddingService = &mocks.MockEmbeddingService{
			GetProviderInfoFunc: func(ctx context.Context, req *service.GetProviderInfoRequest) (*service.GetProviderInfoResponse, error) {
				return nil, errors.New("not found")
			},
		}

		_, err := srv.EmbeddingGetProviderInfo(context.Background(), &pb.EmbeddingGetProviderInfoRequest{
			Provider: pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI,
		})
		require.Error(t, err)
	})
}

// =============================================================================
// EmbeddingSetDefaultProvider Tests
// =============================================================================

func TestServer_EmbeddingSetDefaultProvider(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.EmbeddingSetDefaultProvider(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("unspecified_provider", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.EmbeddingSetDefaultProvider(context.Background(), &pb.EmbeddingSetDefaultProviderRequest{
			Provider: pb.EmbeddingProvider_EMBEDDING_PROVIDER_UNSPECIFIED,
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.EmbeddingSetDefaultProvider(context.Background(), &pb.EmbeddingSetDefaultProviderRequest{
			Provider: pb.EmbeddingProvider_EMBEDDING_PROVIDER_VOYAGE,
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.EmbeddingService = &mocks.MockEmbeddingService{
			SetDefaultProviderFunc: func(ctx context.Context, req *service.SetDefaultProviderRequest) (*service.SetDefaultProviderResponse, error) {
				assert.Equal(t, service.EmbeddingProviderVoyage, req.Provider)
				return &service.SetDefaultProviderResponse{
					Provider:         service.EmbeddingProviderVoyage,
					PreviousProvider: service.EmbeddingProviderOpenAI,
					Timestamp:        time.Now(),
				}, nil
			},
		}

		resp, err := srv.EmbeddingSetDefaultProvider(context.Background(), &pb.EmbeddingSetDefaultProviderRequest{
			Provider: pb.EmbeddingProvider_EMBEDDING_PROVIDER_VOYAGE,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, pb.EmbeddingProvider_EMBEDDING_PROVIDER_VOYAGE, resp.Provider)
		assert.Equal(t, pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI, resp.PreviousProvider)
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.EmbeddingService = &mocks.MockEmbeddingService{
			SetDefaultProviderFunc: func(ctx context.Context, req *service.SetDefaultProviderRequest) (*service.SetDefaultProviderResponse, error) {
				return nil, errors.New("provider not registered")
			},
		}

		_, err := srv.EmbeddingSetDefaultProvider(context.Background(), &pb.EmbeddingSetDefaultProviderRequest{
			Provider: pb.EmbeddingProvider_EMBEDDING_PROVIDER_VOYAGE,
		})
		require.Error(t, err)
	})
}

// =============================================================================
// EmbeddingGetCacheStats Tests
// =============================================================================

func TestServer_EmbeddingGetCacheStats(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.EmbeddingGetCacheStats(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.EmbeddingGetCacheStats(context.Background(), &pb.EmbeddingGetCacheStatsRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.EmbeddingService = &mocks.MockEmbeddingService{
			GetCacheStatsFunc: func(ctx context.Context, req *service.GetEmbeddingCacheStatsRequest) (*service.GetEmbeddingCacheStatsResponse, error) {
				return &service.GetEmbeddingCacheStatsResponse{
					Enabled:            true,
					Size:               150,
					Capacity:           1000,
					Hits:               500,
					Misses:             100,
					HitRate:            0.833,
					Evictions:          10,
					MemoryBytes:        1048576,
					CompressionEnabled: true,
					CompressionRatio:   0.65,
					Timestamp:          time.Now(),
				}, nil
			},
		}

		resp, err := srv.EmbeddingGetCacheStats(context.Background(), &pb.EmbeddingGetCacheStatsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, resp.Enabled)
		assert.Equal(t, int32(150), resp.Size)
		assert.Equal(t, int32(1000), resp.Capacity)
		assert.Equal(t, uint64(500), resp.Hits)
		assert.Equal(t, uint64(100), resp.Misses)
		assert.InDelta(t, 0.833, resp.HitRate, 0.001)
		assert.True(t, resp.CompressionEnabled)
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.EmbeddingService = &mocks.MockEmbeddingService{
			GetCacheStatsFunc: func(ctx context.Context, req *service.GetEmbeddingCacheStatsRequest) (*service.GetEmbeddingCacheStatsResponse, error) {
				return nil, errors.New("cache not initialized")
			},
		}

		_, err := srv.EmbeddingGetCacheStats(context.Background(), &pb.EmbeddingGetCacheStatsRequest{})
		require.Error(t, err)
	})
}

// =============================================================================
// EmbeddingClearCache Tests
// =============================================================================

func TestServer_EmbeddingClearCache(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.EmbeddingClearCache(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.EmbeddingClearCache(context.Background(), &pb.EmbeddingClearCacheRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("success_clear_all", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.EmbeddingService = &mocks.MockEmbeddingService{
			ClearCacheFunc: func(ctx context.Context, req *service.ClearEmbeddingCacheRequest) (*service.ClearEmbeddingCacheResponse, error) {
				assert.True(t, req.All)
				return &service.ClearEmbeddingCacheResponse{
					EntriesCleared: 42,
					Provider:       service.EmbeddingProviderOpenAI,
					Timestamp:      time.Now(),
				}, nil
			},
		}

		resp, err := srv.EmbeddingClearCache(context.Background(), &pb.EmbeddingClearCacheRequest{
			All: true,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, int32(42), resp.EntriesCleared)
		assert.Equal(t, pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI, resp.Provider)
	})

	t.Run("success_clear_by_provider", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.EmbeddingService = &mocks.MockEmbeddingService{
			ClearCacheFunc: func(ctx context.Context, req *service.ClearEmbeddingCacheRequest) (*service.ClearEmbeddingCacheResponse, error) {
				assert.Equal(t, service.EmbeddingProviderVoyage, req.Provider)
				return &service.ClearEmbeddingCacheResponse{
					EntriesCleared: 5,
					Provider:       service.EmbeddingProviderVoyage,
					Timestamp:      time.Now(),
				}, nil
			},
		}

		resp, err := srv.EmbeddingClearCache(context.Background(), &pb.EmbeddingClearCacheRequest{
			Provider: pb.EmbeddingProvider_EMBEDDING_PROVIDER_VOYAGE,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, int32(5), resp.EntriesCleared)
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.EmbeddingService = &mocks.MockEmbeddingService{
			ClearCacheFunc: func(ctx context.Context, req *service.ClearEmbeddingCacheRequest) (*service.ClearEmbeddingCacheResponse, error) {
				return nil, errors.New("cache error")
			},
		}

		_, err := srv.EmbeddingClearCache(context.Background(), &pb.EmbeddingClearCacheRequest{})
		require.Error(t, err)
	})
}

// =============================================================================
// Embedding Conversion Helper Tests
// =============================================================================

func TestProtoToServiceProvider(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.EmbeddingProvider
		expected service.EmbeddingProvider
	}{
		{name: "voyage", input: pb.EmbeddingProvider_EMBEDDING_PROVIDER_VOYAGE, expected: service.EmbeddingProviderVoyage},
		{name: "openai", input: pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI, expected: service.EmbeddingProviderOpenAI},
		{name: "ollama", input: pb.EmbeddingProvider_EMBEDDING_PROVIDER_OLLAMA, expected: service.EmbeddingProviderOllama},
		{name: "lmstudio", input: pb.EmbeddingProvider_EMBEDDING_PROVIDER_LMSTUDIO, expected: service.EmbeddingProviderLMStudio},
		{name: "npu", input: pb.EmbeddingProvider_EMBEDDING_PROVIDER_NPU, expected: service.EmbeddingProviderNPU},
		{name: "unspecified", input: pb.EmbeddingProvider_EMBEDDING_PROVIDER_UNSPECIFIED, expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoToServiceProvider(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestServiceToProtoProvider(t *testing.T) {
	tests := []struct {
		name     string
		input    service.EmbeddingProvider
		expected pb.EmbeddingProvider
	}{
		{name: "voyage", input: service.EmbeddingProviderVoyage, expected: pb.EmbeddingProvider_EMBEDDING_PROVIDER_VOYAGE},
		{name: "openai", input: service.EmbeddingProviderOpenAI, expected: pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI},
		{name: "ollama", input: service.EmbeddingProviderOllama, expected: pb.EmbeddingProvider_EMBEDDING_PROVIDER_OLLAMA},
		{name: "lmstudio", input: service.EmbeddingProviderLMStudio, expected: pb.EmbeddingProvider_EMBEDDING_PROVIDER_LMSTUDIO},
		{name: "npu", input: service.EmbeddingProviderNPU, expected: pb.EmbeddingProvider_EMBEDDING_PROVIDER_NPU},
		{name: "empty_default", input: "", expected: pb.EmbeddingProvider_EMBEDDING_PROVIDER_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := serviceToProtoProvider(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProtoToServiceInputType(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.EmbeddingInputType
		expected service.EmbeddingInputType
	}{
		{name: "document", input: pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_DOCUMENT, expected: service.EmbeddingInputTypeDocument},
		{name: "query", input: pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_QUERY, expected: service.EmbeddingInputTypeQuery},
		{name: "code", input: pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_CODE, expected: service.EmbeddingInputTypeCode},
		{name: "unspecified", input: pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_UNSPECIFIED, expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoToServiceInputType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestServiceToProtoInputType(t *testing.T) {
	tests := []struct {
		name     string
		input    service.EmbeddingInputType
		expected pb.EmbeddingInputType
	}{
		{name: "document", input: service.EmbeddingInputTypeDocument, expected: pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_DOCUMENT},
		{name: "query", input: service.EmbeddingInputTypeQuery, expected: pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_QUERY},
		{name: "code", input: service.EmbeddingInputTypeCode, expected: pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_CODE},
		{name: "empty_default", input: "", expected: pb.EmbeddingInputType_EMBEDDING_INPUT_TYPE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := serviceToProtoInputType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestServiceToProtoStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    service.EmbeddingStatus
		expected pb.EmbeddingStatus
	}{
		{name: "available", input: service.EmbeddingStatusAvailable, expected: pb.EmbeddingStatus_EMBEDDING_STATUS_AVAILABLE},
		{name: "unavailable", input: service.EmbeddingStatusUnavailable, expected: pb.EmbeddingStatus_EMBEDDING_STATUS_UNAVAILABLE},
		{name: "rate_limited", input: service.EmbeddingStatusRateLimited, expected: pb.EmbeddingStatus_EMBEDDING_STATUS_RATE_LIMITED},
		{name: "error", input: service.EmbeddingStatusError, expected: pb.EmbeddingStatus_EMBEDDING_STATUS_ERROR},
		{name: "empty_default", input: "", expected: pb.EmbeddingStatus_EMBEDDING_STATUS_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := serviceToProtoStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestServiceMetadataToProto(t *testing.T) {
	t.Run("nil_metadata", func(t *testing.T) {
		result := serviceMetadataToProto(nil)
		assert.Nil(t, result)
	})

	t.Run("valid_metadata", func(t *testing.T) {
		meta := &service.EmbeddingMetadata{
			Model:      "text-embedding-3-small",
			Provider:   service.EmbeddingProviderOpenAI,
			Dimensions: 1536,
			Tokens:     10,
			Source:     "test",
			Language:   "en",
			InputType:  service.EmbeddingInputTypeDocument,
			Tags:       map[string]string{"env": "test"},
			CacheHit:   true,
		}
		result := serviceMetadataToProto(meta)
		require.NotNil(t, result)
		assert.Equal(t, "text-embedding-3-small", result.Model)
		assert.Equal(t, pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI, result.Provider)
		assert.Equal(t, int32(1536), result.Dimensions)
		assert.Equal(t, int32(10), result.Tokens)
		assert.True(t, result.CacheHit)
	})
}

func TestServiceProviderInfoToProto(t *testing.T) {
	t.Run("nil_provider_info", func(t *testing.T) {
		result := serviceProviderInfoToProto(nil)
		assert.Nil(t, result)
	})

	t.Run("valid_provider_info_without_error_time", func(t *testing.T) {
		info := &service.ProviderInfo{
			Name:               "openai",
			Type:               service.EmbeddingProviderOpenAI,
			Model:              "text-embedding-3-small",
			Dimensions:         1536,
			MaxTokens:          8191,
			MaxBatchSize:       100,
			Status:             service.EmbeddingStatusAvailable,
			APIURL:             "https://api.openai.com",
			RateLimitPerMinute: 3000,
			IsDefault:          true,
			Timeout:            30 * time.Second,
			SuccessCount:       100,
			ErrorCount:         5,
			AvgLatency:         50 * time.Millisecond,
		}
		result := serviceProviderInfoToProto(info)
		require.NotNil(t, result)
		assert.Equal(t, "openai", result.Name)
		assert.Equal(t, pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI, result.Type)
		assert.Equal(t, int32(1536), result.Dimensions)
		assert.True(t, result.IsDefault)
		assert.Nil(t, result.LastErrorTime)
	})

	t.Run("valid_provider_info_with_error_time", func(t *testing.T) {
		errorTime := time.Now().Add(-1 * time.Hour)
		info := &service.ProviderInfo{
			Name:               "voyage",
			Type:               service.EmbeddingProviderVoyage,
			Model:              "voyage-code-2",
			Dimensions:         1024,
			MaxTokens:          16000,
			MaxBatchSize:       128,
			Status:             service.EmbeddingStatusError,
			APIURL:             "https://api.voyageai.com",
			RateLimitPerMinute: 1000,
			IsDefault:          false,
			Timeout:            60 * time.Second,
			LastError:          "rate limit exceeded",
			LastErrorTime:      &errorTime,
			SuccessCount:       500,
			ErrorCount:         10,
			AvgLatency:         100 * time.Millisecond,
		}
		result := serviceProviderInfoToProto(info)
		require.NotNil(t, result)
		assert.Equal(t, "voyage", result.Name)
		assert.Equal(t, pb.EmbeddingProvider_EMBEDDING_PROVIDER_VOYAGE, result.Type)
		assert.Equal(t, pb.EmbeddingStatus_EMBEDDING_STATUS_ERROR, result.Status)
		assert.Equal(t, "rate limit exceeded", result.LastError)
		require.NotNil(t, result.LastErrorTime)
		assert.Equal(t, errorTime.Unix(), result.LastErrorTime.GetSeconds())
	})

	t.Run("provider_info_rate_limited_status", func(t *testing.T) {
		info := &service.ProviderInfo{
			Name:               "ollama",
			Type:               service.EmbeddingProviderOllama,
			Model:              "nomic-embed-text",
			Dimensions:         768,
			MaxTokens:          8192,
			MaxBatchSize:       50,
			Status:             service.EmbeddingStatusRateLimited,
			APIURL:             "http://localhost:11434",
			RateLimitPerMinute: 100,
			IsDefault:          false,
		}
		result := serviceProviderInfoToProto(info)
		require.NotNil(t, result)
		assert.Equal(t, pb.EmbeddingStatus_EMBEDDING_STATUS_RATE_LIMITED, result.Status)
	})

	t.Run("provider_info_unavailable_status", func(t *testing.T) {
		info := &service.ProviderInfo{
			Name:   "lmstudio",
			Type:   service.EmbeddingProviderLMStudio,
			Model:  "local-model",
			Status: service.EmbeddingStatusUnavailable,
		}
		result := serviceProviderInfoToProto(info)
		require.NotNil(t, result)
		assert.Equal(t, pb.EmbeddingStatus_EMBEDDING_STATUS_UNAVAILABLE, result.Status)
	})
}

// TestProviderEnumRoundTripsEveryProvider guards the pair of conversion tables
// against a provider added to one and forgotten in the other.
//
// A gap here does not fail loudly: an unmapped provider becomes UNSPECIFIED,
// which the service reads as "use the default". A caller asking for the NPU
// would silently be served by Ollama -- exactly the kind of quiet wrong-provider
// substitution that poisons a vector index.
func TestProviderEnumRoundTripsEveryProvider(t *testing.T) {
	providers := []service.EmbeddingProvider{
		service.EmbeddingProviderVoyage,
		service.EmbeddingProviderOpenAI,
		service.EmbeddingProviderOllama,
		service.EmbeddingProviderLMStudio,
		service.EmbeddingProviderNPU,
	}

	for _, provider := range providers {
		t.Run(string(provider), func(t *testing.T) {
			encoded := serviceToProtoProvider(provider)
			require.NotEqual(t, pb.EmbeddingProvider_EMBEDDING_PROVIDER_UNSPECIFIED, encoded,
				"provider %q has no proto enum value, so a request for it would silently become the default", provider)

			decoded := protoToServiceProvider(encoded)
			assert.Equal(t, provider, decoded, "provider %q does not survive a round trip", provider)
		})
	}
}
