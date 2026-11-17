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

//go:build rag

package grpc

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	apigrpc "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/ragservice"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rag"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// roundTripBackends are the stub backends pkg/api/grpc's RAG server tests use:
// an embedder and a vector search that records the request it was given.
type roundTripBackends struct {
	ollama service.OllamaService
	vector service.VectorService
}

func (b roundTripBackends) Ollama() service.OllamaService { return b.ollama }
func (b roundTripBackends) Vector() service.VectorService { return b.vector }

// roundTripLeakMarker stands for what a backend error can carry: the
// backend's URL and the body it answered with.
const roundTripLeakMarker = "http://10.9.9.9:11434 secret-body"

func roundTripWorking(search *service.SearchSimilarRequest) roundTripBackends {
	return roundTripBackends{
		ollama: &mocks.MockOllamaService{
			EmbedFunc: func(context.Context, *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
				return &service.OllamaEmbedResponse{Embedding: []float32{0.1, 0.2}}, nil
			},
		},
		vector: &mocks.MockVectorService{
			SearchSimilarFunc: func(_ context.Context, r *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
				*search = *r
				return &service.SearchSimilarResponse{Results: []*service.VectorSearchResult{
					{ID: 7, Score: 0.94, Metadata: map[string]interface{}{
						"content": "type Pool struct{}", "file_path": "db/pool.go", "start_line": float64(23), "end_line": float64(45),
					}},
				}}, nil
			},
		},
	}
}

// newRAGRoundTrip serves the real pkg/api/grpc RAGServer over bufconn and
// returns the SDK gRPC RAG handler dialled to it, plus the last Query request
// the server received on the wire.
func newRAGRoundTrip(t *testing.T, b ragservice.Backends) (*ragHandler, **pb.RAGQueryRequest) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	var wire *pb.RAGQueryRequest
	record := grpc.UnaryInterceptor(func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (interface{}, error) {
		if q, ok := req.(*pb.RAGQueryRequest); ok {
			wire = q
		}
		return next(ctx, req)
	})
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterRAGServiceServer(s, apigrpc.NewRAGServer(b, log))
	}, record)
	return newRAGHandler(srv.dial(t)), &wire
}

// roundTripCode returns the gRPC status code an SDK error carries.
func roundTripCode(t *testing.T, err error) codes.Code {
	t.Helper()
	var withStatus interface{ GRPCStatus() *status.Status }
	require.True(t, stderrors.As(err, &withStatus), "the SDK error does not carry the server's status: %v", err)
	return withStatus.GRPCStatus().Code()
}

func TestRAGHandler_RoundTrip_QueryMinScore(t *testing.T) {
	tests := []struct {
		name    string
		req     rag.QueryRequest
		wireSet bool
		wire    float32
		search  float32
	}{
		{"set", rag.QueryRequest{MinScore: 0.5}, true, 0.5, 0.5},
		{"explicit zero", rag.QueryRequest{MinScore: 0, MinScoreSet: true}, true, 0, 0},
		{"omitted", rag.QueryRequest{}, false, 0, ragservice.DefaultMinScore},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var search service.SearchSimilarRequest
			h, wire := newRAGRoundTrip(t, roundTripWorking(&search))
			req := tt.req
			req.Query, req.Namespace, req.TopK, req.IncludeCitations = "connection pooling", "code", 4, true

			out, err := h.handleRequest(context.Background(), "quicbaas.RAG", "Query", &req)
			require.NoError(t, err)

			require.NotNil(t, *wire, "the Query RPC did not reach the server")
			if tt.wireSet {
				require.NotNil(t, (*wire).MinScore, "min_score did not arrive")
				assert.Equal(t, tt.wire, (*wire).GetMinScore())
			} else {
				assert.Nil(t, (*wire).MinScore, "an omitted min_score arrived set")
			}
			assert.Equal(t, tt.search, search.MinScore)
			assert.Equal(t, 4, search.K)

			resp := out.(*rag.QueryResponse)
			assert.Equal(t, 1, resp.ResultCount)
			assert.Contains(t, resp.Context, "type Pool struct{}")
			require.Len(t, resp.Citations, 1)
			assert.Equal(t, "db/pool.go", resp.Citations[0].FilePath)
		})
	}
}

func TestRAGHandler_RoundTrip_BuildContext(t *testing.T) {
	var search service.SearchSimilarRequest
	h, _ := newRAGRoundTrip(t, roundTripWorking(&search))

	out, err := h.handleRequest(context.Background(), "quicbaas.RAG", "BuildContext", &rag.BuildContextRequest{
		Results: []rag.ResultInput{
			{Content: "func main() {}", Score: 0.92, Metadata: map[string]string{"file_path": "main.go", "start_line": "1", "end_line": "20"}},
		},
		Template:           "plain",
		IncludeCitations:   true,
		TruncationStrategy: "hard",
	})
	require.NoError(t, err)
	resp := out.(*rag.BuildContextResponse)
	assert.Equal(t, 1, resp.ResultCount)
	assert.Contains(t, resp.Context, "func main() {}")
	require.Len(t, resp.Citations, 1)
	assert.Equal(t, "main.go", resp.Citations[0].FilePath)

	_, err = h.handleRequest(context.Background(), "quicbaas.RAG", "BuildContext", &rag.BuildContextRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, roundTripCode(t, err))
	assert.True(t, errors.IsInvalidInput(err), "got %v", err)
}

// TestRAGHandler_RoundTrip_BackendFailure checks that a backend failure
// reaches the SDK caller as an error with the server's status code, without
// the backend's message.
func TestRAGHandler_RoundTrip_BackendFailure(t *testing.T) {
	var search service.SearchSimilarRequest
	ok := roundTripWorking(&search)

	t.Run("embedder fails", func(t *testing.T) {
		h, _ := newRAGRoundTrip(t, roundTripBackends{
			ollama: &mocks.MockOllamaService{
				EmbedFunc: func(context.Context, *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
					return nil, stderrors.New("POST " + roundTripLeakMarker)
				},
			},
			vector: ok.vector,
		})
		_, err := h.handleRequest(context.Background(), "quicbaas.RAG", "Query", &rag.QueryRequest{Query: "q", Namespace: "n"})
		require.Error(t, err)
		assert.Equal(t, codes.Internal, roundTripCode(t, err))
		assert.Equal(t, errors.CodeInternal, errors.Code(err))
		assert.NotContains(t, err.Error(), "secret-body")
	})

	t.Run("no vector service", func(t *testing.T) {
		h, _ := newRAGRoundTrip(t, roundTripBackends{ollama: ok.ollama})
		_, err := h.handleRequest(context.Background(), "quicbaas.RAG", "Query", &rag.QueryRequest{Query: "q", Namespace: "n"})
		require.Error(t, err)
		assert.Equal(t, codes.Unavailable, roundTripCode(t, err))
		assert.True(t, errors.IsUnavailable(err), "got %v", err)
	})
}
