//go:build rag

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
)

// TestOllamaHandlers_ResponseOutOfRange proves that an embedding dimension or
// count too large for its int32 wire field becomes codes.Internal instead of
// a wrapped integer.
func TestOllamaHandlers_ResponseOutOfRange(t *testing.T) {
	const tooBig = math.MaxInt32 + 1
	ctx := context.Background()
	embedBatch := func(dimensions, count int) func(*mockOllamaService) {
		return func(m *mockOllamaService) {
			m.embedBatchFunc = func(context.Context, *service.OllamaEmbedBatchRequest) (*service.OllamaEmbedBatchResponse, error) {
				return &service.OllamaEmbedBatchResponse{Dimensions: dimensions, Count: count}, nil
			}
		}
	}
	embedBatchCall := func(srv *OllamaServer) error {
		_, err := srv.EmbedBatch(ctx, &pb.OllamaEmbedBatchRequest{Texts: []string{"a"}})
		return err
	}

	tests := []struct {
		name  string
		field string
		setup func(m *mockOllamaService)
		call  func(srv *OllamaServer) error
	}{
		{
			name:  "ListModels count",
			field: "count",
			setup: func(m *mockOllamaService) {
				m.listModelsFunc = func(context.Context, *service.OllamaListModelsRequest) (*service.OllamaListModelsResponse, error) {
					return &service.OllamaListModelsResponse{Count: tooBig}, nil
				}
			},
			call: func(srv *OllamaServer) error {
				_, err := srv.ListModels(ctx, &pb.OllamaListModelsRequest{})
				return err
			},
		},
		{
			name:  "Embed dimensions",
			field: "dimensions",
			setup: func(m *mockOllamaService) {
				m.embedFunc = func(context.Context, *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
					return &service.OllamaEmbedResponse{Dimensions: tooBig}, nil
				}
			},
			call: func(srv *OllamaServer) error {
				_, err := srv.Embed(ctx, &pb.OllamaEmbedRequest{Text: "a"})
				return err
			},
		},
		{
			name:  "EmbedWithModel dimensions",
			field: "dimensions",
			setup: func(m *mockOllamaService) {
				m.embedWithModelFunc = func(context.Context, *service.OllamaEmbedWithModelRequest) (*service.OllamaEmbedResponse, error) {
					return &service.OllamaEmbedResponse{Dimensions: tooBig}, nil
				}
			},
			call: func(srv *OllamaServer) error {
				_, err := srv.EmbedWithModel(ctx, &pb.OllamaEmbedWithModelRequest{Text: "a", Model: "m"})
				return err
			},
		},
		{name: "EmbedBatch dimensions", field: "dimensions", setup: embedBatch(tooBig, 1), call: embedBatchCall},
		{name: "EmbedBatch count", field: "count", setup: embedBatch(8, tooBig), call: embedBatchCall},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := newTestOllamaServer(t)
			tt.setup(mockSvc)
			requireRangeStatus(t, tt.call(srv), codes.Internal, tt.field)
		})
	}
}
