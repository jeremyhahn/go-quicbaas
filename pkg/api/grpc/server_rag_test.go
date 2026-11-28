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
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/ragservice"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

type ragTestBackends struct {
	ollama service.OllamaService
	vector service.VectorService
}

func (b ragTestBackends) Ollama() service.OllamaService { return b.ollama }
func (b ragTestBackends) Vector() service.VectorService { return b.vector }

// ragWorking embeds every query and returns two indexed passages.
func ragWorking(search *service.SearchSimilarRequest) ragTestBackends {
	return ragTestBackends{
		ollama: &mocks.MockOllamaService{
			EmbedFunc: func(context.Context, *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
				return &service.OllamaEmbedResponse{Embedding: []float32{0.1, 0.2}}, nil
			},
		},
		vector: &mocks.MockVectorService{
			// The vector service returns the cosine distance as Distance and
			// Score; ragservice reads the metric to turn it into a similarity.
			GetNamespaceInfoFunc: func(context.Context, *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error) {
				return &service.GetNamespaceInfoResponse{DistanceMetric: "COSINE"}, nil
			},
			SearchSimilarFunc: func(_ context.Context, r *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
				*search = *r
				return &service.SearchSimilarResponse{Results: []*service.VectorSearchResult{
					{ID: 7, Distance: 0.06, Score: 0.06, Metadata: map[string]interface{}{
						"content": "type Pool struct{}", "file_path": "db/pool.go", "start_line": float64(23), "end_line": float64(45),
					}},
					{ID: 9, Distance: 0.19, Score: 0.19, Metadata: map[string]interface{}{
						"content": "func Open() {}", "file_path": "db/open.go", "start_line": 3, "end_line": 8,
					}},
				}}, nil
			},
		},
	}
}

func newTestRAGServer(t *testing.T, b ragservice.Backends) *RAGServer {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	return NewRAGServer(b, log)
}

// TestRAGServer_QueryIsTheSharedQuery proves gRPC Query returns what
// ragservice.Query returns for the same request: no pipeline of its own.
func TestRAGServer_QueryIsTheSharedQuery(t *testing.T) {
	var search service.SearchSimilarRequest
	b := ragWorking(&search)
	srv := newTestRAGServer(t, b)

	minScore := float32(0.5)
	got, err := srv.Query(context.Background(), &pb.RAGQueryRequest{
		Query: "connection pooling", Namespace: "code", TopK: 4, MinScore: &minScore,
		Template: pb.RAGTemplate_RAG_TEMPLATE_JSON, IncludeCitations: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 4, search.K)
	assert.Equal(t, float32(0), search.MinScore, "ragservice applies the similarity threshold, not the backend")

	want, err := ragservice.Query(context.Background(), b, ragservice.QueryRequest{
		Query: "connection pooling", Namespace: "code", TopK: 4, MinScore: 0.5,
		Template: "json", IncludeCitations: true,
	})
	require.NoError(t, err)
	assert.Equal(t, want.Context, got.GetContext())
	assert.Equal(t, int32(want.Tokens), got.GetTokens())
	assert.Equal(t, want.Truncated, got.GetTruncated())
	assert.Equal(t, int32(2), got.GetResultCount())
	require.Len(t, got.GetCitations(), 2)
	assert.Equal(t, "db/pool.go", got.GetCitations()[0].GetFilePath())
	assert.Equal(t, int32(23), got.GetCitations()[0].GetStartLine())
	assert.Equal(t, int32(45), got.GetCitations()[0].GetEndLine())
}

func TestRAGServer_BuildContextIsTheSharedBuildContext(t *testing.T) {
	srv := newTestRAGServer(t, ragTestBackends{})
	got, err := srv.BuildContext(context.Background(), &pb.RAGBuildContextRequest{
		Results: []*pb.RAGResultInput{
			{Content: "func main() {}", Score: 0.92, Metadata: map[string]string{"file_path": "main.go", "start_line": "1", "end_line": "20"}},
		},
		Template:           pb.RAGTemplate_RAG_TEMPLATE_PLAIN,
		TruncationStrategy: pb.RAGTruncationStrategy_RAG_TRUNCATION_HARD,
		IncludeCitations:   true,
	})
	require.NoError(t, err)

	want, err := ragservice.BuildContext(context.Background(), ragservice.BuildContextRequest{
		Results: []ragservice.ResultInput{
			{Content: "func main() {}", Score: 0.92, Metadata: map[string]string{"file_path": "main.go", "start_line": "1", "end_line": "20"}},
		},
		Template: "plain", TruncationStrategy: "hard", IncludeCitations: true,
	})
	require.NoError(t, err)
	assert.Equal(t, want.Context, got.GetContext())
	assert.Equal(t, int32(1), got.GetResultCount())
	require.Len(t, got.GetCitations(), 1)
	assert.Equal(t, "main.go", got.GetCitations()[0].GetFilePath())
}

// TestRAGServer_ErrorCodes proves the shared service's typed errors reach
// the gRPC caller as the matching status codes.
func TestRAGServer_ErrorCodes(t *testing.T) {
	var search service.SearchSimilarRequest
	ok := ragWorking(&search)

	tests := []struct {
		name string
		call func(*RAGServer) error
		code codes.Code
	}{
		{"no query", func(s *RAGServer) error {
			_, err := s.Query(context.Background(), &pb.RAGQueryRequest{Namespace: "n"})
			return err
		}, codes.InvalidArgument},
		{"no namespace", func(s *RAGServer) error {
			_, err := s.Query(context.Background(), &pb.RAGQueryRequest{Query: "q"})
			return err
		}, codes.InvalidArgument},
		{"empty results", func(s *RAGServer) error {
			_, err := s.BuildContext(context.Background(), &pb.RAGBuildContextRequest{})
			return err
		}, codes.InvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call(newTestRAGServer(t, ok))
			require.Error(t, err)
			assert.Equal(t, tt.code, status.Code(err))
		})
	}

	t.Run("no ollama", func(t *testing.T) {
		_, err := newTestRAGServer(t, ragTestBackends{vector: ok.vector}).
			Query(context.Background(), &pb.RAGQueryRequest{Query: "q", Namespace: "n"})
		require.Error(t, err)
		assert.Equal(t, codes.Unavailable, status.Code(err))
	})
}

// ragLeakMarker stands for what a backend error can carry: the backend's URL
// and the body it answered with.
const ragLeakMarker = "http://10.9.9.9:11434 secret-body"

// ragFailing returns backends whose embedder fails with an error carrying
// ragLeakMarker.
func ragFailing() ragTestBackends {
	var search service.SearchSimilarRequest
	ok := ragWorking(&search)
	return ragTestBackends{
		ollama: &mocks.MockOllamaService{
			EmbedFunc: func(context.Context, *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
				return nil, stderrors.New("POST " + ragLeakMarker)
			},
		},
		vector: ok.vector,
	}
}

// TestRAGServer_BackendErrorIsNotReturned checks that a backend failure keeps
// the code ToGRPCError picks but its message does not reach the caller, while
// a client error keeps its message.
func TestRAGServer_BackendErrorIsNotReturned(t *testing.T) {
	_, err := newTestRAGServer(t, ragFailing()).
		Query(context.Background(), &pb.RAGQueryRequest{Query: "q", Namespace: "n"})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.NotContains(t, status.Convert(err).Message(), ragLeakMarker)
	assert.NotContains(t, status.Convert(err).Message(), "secret-body")
	assert.Equal(t, "rag query failed", status.Convert(err).Message())

	var search service.SearchSimilarRequest
	_, err = newTestRAGServer(t, ragWorking(&search)).
		Query(context.Background(), &pb.RAGQueryRequest{Namespace: "n"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Contains(t, status.Convert(err).Message(), "query")
}

func TestRAGEnumNames(t *testing.T) {
	assert.Equal(t, "", ragTemplateName(pb.RAGTemplate_RAG_TEMPLATE_UNSPECIFIED))
	assert.Equal(t, "json", ragTemplateName(pb.RAGTemplate_RAG_TEMPLATE_JSON))
	assert.Equal(t, "xml", ragTemplateName(pb.RAGTemplate_RAG_TEMPLATE_XML))
	assert.Equal(t, "plain", ragTemplateName(pb.RAGTemplate_RAG_TEMPLATE_PLAIN))
	assert.Equal(t, "markdown", ragTemplateName(pb.RAGTemplate_RAG_TEMPLATE_MARKDOWN))
	assert.Equal(t, "", ragTruncationName(pb.RAGTruncationStrategy_RAG_TRUNCATION_UNSPECIFIED))
	assert.Equal(t, "hard", ragTruncationName(pb.RAGTruncationStrategy_RAG_TRUNCATION_HARD))
	assert.Equal(t, "none", ragTruncationName(pb.RAGTruncationStrategy_RAG_TRUNCATION_NONE))
	assert.Equal(t, "smart", ragTruncationName(pb.RAGTruncationStrategy_RAG_TRUNCATION_SMART))
}
