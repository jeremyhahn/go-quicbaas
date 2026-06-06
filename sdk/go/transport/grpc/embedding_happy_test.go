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
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
)

// fakeEmbeddingServer is a configurable in-memory EmbeddingService backend.
type fakeEmbeddingServer struct {
	pb.UnimplementedEmbeddingServiceServer
	fail bool
}

func (f *fakeEmbeddingServer) errOr() error {
	if f.fail {
		return status.Error(codes.Internal, "boom")
	}
	return nil
}

func (f *fakeEmbeddingServer) EmbeddingGenerate(_ context.Context, _ *pb.EmbeddingGenerateRequest) (*pb.EmbeddingGenerateResponse, error) {
	if err := f.errOr(); err != nil {
		return nil, err
	}
	return &pb.EmbeddingGenerateResponse{
		Embedding: []float32{0.1, 0.2},
		Metadata: &pb.EmbeddingMetadata{
			Model:          "m",
			Provider:       pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI,
			Dimensions:     2,
			Tokens:         3,
			ProcessingTime: durationpb.New(time.Second),
		},
		Timestamp: timestamppb.Now(),
	}, nil
}

func (f *fakeEmbeddingServer) EmbeddingBatchGenerate(_ context.Context, _ *pb.EmbeddingBatchGenerateRequest) (*pb.EmbeddingBatchGenerateResponse, error) {
	if err := f.errOr(); err != nil {
		return nil, err
	}
	return &pb.EmbeddingBatchGenerateResponse{
		Results: []*pb.EmbeddingBatchResult{
			{Id: "a", Embedding: []float32{0.1}, Metadata: &pb.EmbeddingMetadata{Model: "m"}},
		},
		SuccessCount:   1,
		ErrorCount:     0,
		TotalTokens:    7,
		ProcessingTime: durationpb.New(time.Second),
		Timestamp:      timestamppb.Now(),
	}, nil
}

func (f *fakeEmbeddingServer) EmbeddingListProviders(_ context.Context, _ *pb.EmbeddingListProvidersRequest) (*pb.EmbeddingListProvidersResponse, error) {
	if err := f.errOr(); err != nil {
		return nil, err
	}
	return &pb.EmbeddingListProvidersResponse{
		Providers: []*pb.EmbeddingProviderInfo{
			{Name: "openai", Type: pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI, Status: pb.EmbeddingStatus_EMBEDDING_STATUS_AVAILABLE},
		},
		DefaultProvider: "openai",
		Timestamp:       timestamppb.Now(),
	}, nil
}

func (f *fakeEmbeddingServer) EmbeddingGetProviderInfo(_ context.Context, _ *pb.EmbeddingGetProviderInfoRequest) (*pb.EmbeddingGetProviderInfoResponse, error) {
	if err := f.errOr(); err != nil {
		return nil, err
	}
	return &pb.EmbeddingGetProviderInfoResponse{
		Info: &pb.EmbeddingProviderInfo{
			Name:          "openai",
			Type:          pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI,
			Status:        pb.EmbeddingStatus_EMBEDDING_STATUS_AVAILABLE,
			Timeout:       durationpb.New(time.Second),
			AvgLatency:    durationpb.New(time.Millisecond),
			LastErrorTime: timestamppb.Now(),
		},
		Timestamp: timestamppb.Now(),
	}, nil
}

func (f *fakeEmbeddingServer) EmbeddingSetDefaultProvider(_ context.Context, _ *pb.EmbeddingSetDefaultProviderRequest) (*pb.EmbeddingSetDefaultProviderResponse, error) {
	if err := f.errOr(); err != nil {
		return nil, err
	}
	return &pb.EmbeddingSetDefaultProviderResponse{
		Provider:         pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI,
		PreviousProvider: pb.EmbeddingProvider_EMBEDDING_PROVIDER_OLLAMA,
		Timestamp:        timestamppb.Now(),
	}, nil
}

func (f *fakeEmbeddingServer) EmbeddingGetCacheStats(_ context.Context, _ *pb.EmbeddingGetCacheStatsRequest) (*pb.EmbeddingGetCacheStatsResponse, error) {
	if err := f.errOr(); err != nil {
		return nil, err
	}
	return &pb.EmbeddingGetCacheStatsResponse{
		Enabled:   true,
		Size:      1,
		Capacity:  10,
		Hits:      5,
		Misses:    2,
		HitRate:   0.7,
		Timestamp: timestamppb.Now(),
	}, nil
}

func (f *fakeEmbeddingServer) EmbeddingClearCache(_ context.Context, _ *pb.EmbeddingClearCacheRequest) (*pb.EmbeddingClearCacheResponse, error) {
	if err := f.errOr(); err != nil {
		return nil, err
	}
	return &pb.EmbeddingClearCacheResponse{
		EntriesCleared: 3,
		Provider:       pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI,
		Timestamp:      timestamppb.Now(),
	}, nil
}

func newEmbeddingHandlerWithServer(t *testing.T, fail bool) *embeddingHandler {
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterEmbeddingServiceServer(s, &fakeEmbeddingServer{fail: fail})
	})
	return newEmbeddingHandler(srv.dial(t))
}

func TestEmbeddingHandler_GenerateEmbedding_Success(t *testing.T) {
	h := newEmbeddingHandlerWithServer(t, false)
	resp, err := h.handleRequest(context.Background(), "quicbaas.Embedding", "GenerateEmbedding",
		&embedding.GenerateEmbeddingRequest{Text: "hi", Provider: embedding.ProviderOpenAI})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, ok := resp.(*embedding.GenerateEmbeddingResponse)
	if !ok {
		t.Fatalf("wrong response type: %T", resp)
	}
	if len(out.Embedding) != 2 || out.Metadata == nil || out.Metadata.Dimensions != 2 {
		t.Fatalf("unexpected response: %+v", out)
	}
}

func TestEmbeddingHandler_GenerateEmbedding_ServerError(t *testing.T) {
	h := newEmbeddingHandlerWithServer(t, true)
	_, err := h.handleRequest(context.Background(), "quicbaas.Embedding", "GenerateEmbedding",
		&embedding.GenerateEmbeddingRequest{Text: "hi"})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestEmbeddingHandler_BatchGenerate_Success(t *testing.T) {
	h := newEmbeddingHandlerWithServer(t, false)
	resp, err := h.handleRequest(context.Background(), "quicbaas.Embedding", "BatchGenerateEmbeddings",
		&embedding.BatchGenerateEmbeddingsRequest{
			Items: []*embedding.BatchEmbeddingItem{{ID: "a", Text: "t"}},
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := resp.(*embedding.BatchGenerateEmbeddingsResponse)
	if out.SuccessCount != 1 || len(out.Results) != 1 {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestEmbeddingHandler_BatchGenerate_ServerError(t *testing.T) {
	h := newEmbeddingHandlerWithServer(t, true)
	_, err := h.handleRequest(context.Background(), "quicbaas.Embedding", "BatchGenerateEmbeddings",
		&embedding.BatchGenerateEmbeddingsRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEmbeddingHandler_ListProviders_Success(t *testing.T) {
	h := newEmbeddingHandlerWithServer(t, false)
	resp, err := h.handleRequest(context.Background(), "quicbaas.Embedding", "ListProviders",
		&embedding.ListProvidersRequest{IncludeUnavailable: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := resp.(*embedding.ListProvidersResponse)
	if out.DefaultProvider != "openai" || len(out.Providers) != 1 {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestEmbeddingHandler_ListProviders_ServerError(t *testing.T) {
	h := newEmbeddingHandlerWithServer(t, true)
	_, err := h.handleRequest(context.Background(), "quicbaas.Embedding", "ListProviders",
		&embedding.ListProvidersRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEmbeddingHandler_GetProviderInfo_Success(t *testing.T) {
	h := newEmbeddingHandlerWithServer(t, false)
	resp, err := h.handleRequest(context.Background(), "quicbaas.Embedding", "GetProviderInfo",
		&embedding.GetProviderInfoRequest{Provider: embedding.ProviderOpenAI})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := resp.(*embedding.GetProviderInfoResponse)
	if out.Info == nil || out.Info.Name != "openai" {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestEmbeddingHandler_GetProviderInfo_ServerError(t *testing.T) {
	h := newEmbeddingHandlerWithServer(t, true)
	_, err := h.handleRequest(context.Background(), "quicbaas.Embedding", "GetProviderInfo",
		&embedding.GetProviderInfoRequest{Provider: embedding.ProviderOpenAI})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEmbeddingHandler_SetDefaultProvider_Success(t *testing.T) {
	h := newEmbeddingHandlerWithServer(t, false)
	resp, err := h.handleRequest(context.Background(), "quicbaas.Embedding", "SetDefaultProvider",
		&embedding.SetDefaultProviderRequest{Provider: embedding.ProviderOpenAI})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := resp.(*embedding.SetDefaultProviderResponse)
	if out.Provider != embedding.ProviderOpenAI {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestEmbeddingHandler_SetDefaultProvider_ServerError(t *testing.T) {
	h := newEmbeddingHandlerWithServer(t, true)
	_, err := h.handleRequest(context.Background(), "quicbaas.Embedding", "SetDefaultProvider",
		&embedding.SetDefaultProviderRequest{Provider: embedding.ProviderOpenAI})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEmbeddingHandler_GetCacheStats_Success(t *testing.T) {
	h := newEmbeddingHandlerWithServer(t, false)
	resp, err := h.handleRequest(context.Background(), "quicbaas.Embedding", "GetCacheStats",
		&embedding.GetEmbeddingCacheStatsRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := resp.(*embedding.GetEmbeddingCacheStatsResponse)
	if !out.Enabled || out.Capacity != 10 {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestEmbeddingHandler_GetCacheStats_ServerError(t *testing.T) {
	h := newEmbeddingHandlerWithServer(t, true)
	_, err := h.handleRequest(context.Background(), "quicbaas.Embedding", "GetCacheStats",
		&embedding.GetEmbeddingCacheStatsRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEmbeddingHandler_ClearCache_Success(t *testing.T) {
	h := newEmbeddingHandlerWithServer(t, false)
	resp, err := h.handleRequest(context.Background(), "quicbaas.Embedding", "ClearCache",
		&embedding.ClearEmbeddingCacheRequest{All: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := resp.(*embedding.ClearEmbeddingCacheResponse)
	if out.EntriesCleared != 3 {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestEmbeddingHandler_ClearCache_ServerError(t *testing.T) {
	h := newEmbeddingHandlerWithServer(t, true)
	_, err := h.handleRequest(context.Background(), "quicbaas.Embedding", "ClearCache",
		&embedding.ClearEmbeddingCacheRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}
