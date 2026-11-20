//go:build rag

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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/ragservice"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rag"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// ragMock is a QuicBaaS double whose Ollama and Vector providers are set
// per test; a nil field is the "not configured" facade.
type ragMock struct {
	*mockQuicBaaS
	ollama service.OllamaService
	vector service.VectorService
}

func (m *ragMock) Ollama() service.OllamaService { return m.ollama }
func (m *ragMock) Vector() service.VectorService { return m.vector }

func ragBackends(search *service.SearchSimilarRequest) *ragMock {
	return &ragMock{
		mockQuicBaaS: &mockQuicBaaS{},
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

func newRAGTransport(t *testing.T, m *ragMock) *Transport {
	t.Helper()
	tr, err := New(m)
	require.NoError(t, err)
	return tr
}

// TestEmbeddedRAG_QueryIsTheSharedQuery proves the embedded Query returns
// what ragservice.Query returns for the same request (#1611).
func TestEmbeddedRAG_QueryIsTheSharedQuery(t *testing.T) {
	var search service.SearchSimilarRequest
	m := ragBackends(&search)

	out, err := newRAGTransport(t, m).Request(context.Background(), ragServiceName, "Query", &rag.QueryRequest{
		Query: "connection pooling", Namespace: "code", TopK: 4, MinScore: 0.5,
		Template: "json", IncludeCitations: true,
	})
	require.NoError(t, err)
	got, ok := out.(*rag.QueryResponse)
	require.True(t, ok)
	assert.Equal(t, 4, search.K)
	assert.Equal(t, float32(0), search.MinScore, "ragservice applies the similarity threshold, not the backend")

	want, err := ragservice.Query(context.Background(), m, ragservice.QueryRequest{
		Query: "connection pooling", Namespace: "code", TopK: 4, MinScore: 0.5,
		Template: "json", IncludeCitations: true,
	})
	require.NoError(t, err)
	assert.Equal(t, want.Context, got.Context)
	assert.Equal(t, want.Tokens, got.Tokens)
	assert.Equal(t, want.Truncated, got.Truncated)
	assert.Equal(t, 2, got.ResultCount)
	require.Len(t, got.Citations, 2)
	assert.Equal(t, "db/pool.go", got.Citations[0].FilePath)
	assert.Equal(t, 23, got.Citations[0].StartLine)
	assert.Equal(t, 45, got.Citations[0].EndLine)
}

func TestEmbeddedRAG_BuildContextIsTheSharedBuildContext(t *testing.T) {
	in := []rag.ResultInput{
		{Content: "func main() {}", Score: 0.92, Metadata: map[string]string{"file_path": "main.go", "start_line": "1", "end_line": "20"}},
	}
	out, err := newRAGTransport(t, &ragMock{mockQuicBaaS: &mockQuicBaaS{}}).Request(context.Background(), ragServiceName, "BuildContext", &rag.BuildContextRequest{
		Results: in, Template: "plain", TruncationStrategy: "hard", IncludeCitations: true,
	})
	require.NoError(t, err)
	got, ok := out.(*rag.BuildContextResponse)
	require.True(t, ok)

	want, err := ragservice.BuildContext(context.Background(), ragservice.BuildContextRequest{
		Results: []ragservice.ResultInput{
			{Content: "func main() {}", Score: 0.92, Metadata: map[string]string{"file_path": "main.go", "start_line": "1", "end_line": "20"}},
		},
		Template: "plain", TruncationStrategy: "hard", IncludeCitations: true,
	})
	require.NoError(t, err)
	assert.Equal(t, want.Context, got.Context)
	assert.Equal(t, 1, got.ResultCount)
	require.Len(t, got.Citations, 1)
	assert.Equal(t, "main.go", got.Citations[0].FilePath)
}

// TestEmbeddedRAG_ErrorCodes proves the shared service's typed errors reach
// the embedded caller with the codes gRPC returns for them: invalid input
// and an unconfigured Ollama.
func TestEmbeddedRAG_ErrorCodes(t *testing.T) {
	var search service.SearchSimilarRequest
	ok := ragBackends(&search)
	noOllama := &ragMock{mockQuicBaaS: &mockQuicBaaS{}, vector: ok.vector}

	tests := []struct {
		name   string
		m      *ragMock
		method string
		req    interface{}
		code   sdkerrors.ErrorCode
	}{
		{"no query", ok, "Query", &rag.QueryRequest{Namespace: "n"}, sdkerrors.CodeInvalidInput},
		{"no namespace", ok, "Query", &rag.QueryRequest{Query: "q"}, sdkerrors.CodeInvalidInput},
		{"empty results", ok, "BuildContext", &rag.BuildContextRequest{}, sdkerrors.CodeInvalidInput},
		{"no ollama", noOllama, "Query", &rag.QueryRequest{Query: "q", Namespace: "n"}, sdkerrors.CodeUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newRAGTransport(t, tt.m).Request(context.Background(), ragServiceName, tt.method, tt.req)
			require.Error(t, err)
			assert.Equal(t, tt.code, sdkerrors.Code(err), err.Error())
		})
	}
}

func TestEmbeddedRAG_InvalidRequestType(t *testing.T) {
	tr := newRAGTransport(t, &ragMock{mockQuicBaaS: &mockQuicBaaS{}})
	var typeErr *InvalidRequestTypeError

	_, err := tr.Request(context.Background(), ragServiceName, "Query", &rag.BuildContextRequest{})
	require.ErrorAs(t, err, &typeErr)
	assert.Equal(t, "*rag.QueryRequest", typeErr.ExpectedType)

	_, err = tr.Request(context.Background(), ragServiceName, "BuildContext", (*rag.BuildContextRequest)(nil))
	require.ErrorAs(t, err, &typeErr)
	assert.Equal(t, "*rag.BuildContextRequest", typeErr.ExpectedType)
}
