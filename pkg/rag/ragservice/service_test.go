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

package ragservice

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

type backends struct {
	ollama service.OllamaService
	vector service.VectorService
}

func (b backends) Ollama() service.OllamaService { return b.ollama }
func (b backends) Vector() service.VectorService { return b.vector }

// hits is what the fake vector store returns: two passages with their
// metadata, as the indexer writes it.
func hits() []*service.VectorSearchResult {
	return []*service.VectorSearchResult{
		{ID: 7, Score: 0.94, Metadata: map[string]interface{}{
			"content": "type Pool struct{}", "file_path": "db/pool.go", "file_name": "pool.go",
			"language": "go", "start_line": float64(23), "end_line": "45",
		}},
		{ID: 9, Score: 0.81, Metadata: map[string]interface{}{
			"content": "func Open() {}", "file_path": "db/open.go", "start_line": 3, "end_line": int64(8),
		}},
	}
}

func working(search *service.SearchSimilarRequest, model *string) backends {
	return backends{
		ollama: &mocks.MockOllamaService{
			EmbedFunc: func(context.Context, *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
				return &service.OllamaEmbedResponse{Embedding: []float32{0.1, 0.2}}, nil
			},
			EmbedWithModelFunc: func(_ context.Context, r *service.OllamaEmbedWithModelRequest) (*service.OllamaEmbedResponse, error) {
				*model = r.Model
				return &service.OllamaEmbedResponse{Embedding: []float32{0.3, 0.4}}, nil
			},
		},
		vector: &mocks.MockVectorService{
			SearchSimilarFunc: func(_ context.Context, r *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
				*search = *r
				return &service.SearchSimilarResponse{Results: hits()}, nil
			},
		},
	}
}

func TestQuery_EmbedsSearchesAndAssembles(t *testing.T) {
	var search service.SearchSimilarRequest
	var model string
	resp, err := Query(context.Background(), working(&search, &model), QueryRequest{
		Query: "connection pooling", Namespace: "code", IncludeCitations: true,
	})
	require.NoError(t, err)

	// Zero-valued knobs take the defaults.
	assert.Equal(t, "code", search.Namespace)
	assert.Equal(t, []float32{0.1, 0.2}, search.QueryVector)
	assert.Equal(t, DefaultTopK, search.K)
	assert.Equal(t, DefaultMinScore, search.MinScore)
	assert.True(t, search.IncludeMetadata)
	assert.False(t, search.IncludeVectors)
	assert.Empty(t, model, "no model asked for, so the default embedder is used")

	assert.Equal(t, 2, resp.ResultCount)
	assert.Contains(t, resp.Context, "type Pool struct{}")
	assert.Contains(t, resp.Context, "func Open() {}")
	assert.Positive(t, resp.Tokens)
	require.Len(t, resp.Citations, 2)
	assert.Equal(t, Citation{Index: 1, FilePath: "db/pool.go", StartLine: 23, EndLine: 45, Score: 0.94}, resp.Citations[0])
	assert.Equal(t, Citation{Index: 2, FilePath: "db/open.go", StartLine: 3, EndLine: 8, Score: 0.81}, resp.Citations[1])
}

func TestQuery_PassesKnobsAndModel(t *testing.T) {
	var search service.SearchSimilarRequest
	var model string
	resp, err := Query(context.Background(), working(&search, &model), QueryRequest{
		Query: "q", Namespace: "n", TopK: 3, MinScore: 0.5, Model: "mxbai", Template: "json",
	})
	require.NoError(t, err)
	assert.Equal(t, 3, search.K)
	assert.Equal(t, float32(0.5), search.MinScore)
	assert.Equal(t, "mxbai", model)
	assert.Equal(t, []float32{0.3, 0.4}, search.QueryVector)
	assert.Empty(t, resp.Citations, "citations were not asked for")
}

func TestQuery_Errors(t *testing.T) {
	var search service.SearchSimilarRequest
	var model string
	ok := working(&search, &model)
	cause := stderrors.New("boom")

	tests := []struct {
		name string
		b    backends
		req  QueryRequest
		code dberrors.ErrorCode
		msg  string
	}{
		{"no query", ok, QueryRequest{Namespace: "n"}, dberrors.ErrInvalidInput, "query is required"},
		{"no namespace", ok, QueryRequest{Query: "q"}, dberrors.ErrInvalidInput, "namespace is required"},
		{"no ollama", backends{vector: ok.vector}, QueryRequest{Query: "q", Namespace: "n"}, dberrors.ErrUnavailable, "ollama service not configured"},
		{"no vector", backends{ollama: ok.ollama}, QueryRequest{Query: "q", Namespace: "n"}, dberrors.ErrUnavailable, "vector service not configured"},
		{"embed fails", backends{
			ollama: &mocks.MockOllamaService{EmbedFunc: func(context.Context, *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
				return nil, cause
			}},
			vector: ok.vector,
		}, QueryRequest{Query: "q", Namespace: "n"}, dberrors.ErrInternal, "failed to generate query embedding"},
		{"search fails", backends{
			ollama: ok.ollama,
			vector: &mocks.MockVectorService{SearchSimilarFunc: func(context.Context, *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
				return nil, dberrors.New(dberrors.ErrNotFound, "vector.search", "no such namespace")
			}},
		}, QueryRequest{Query: "q", Namespace: "n"}, dberrors.ErrNotFound, "vector search"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := Query(context.Background(), tt.b, tt.req)
			require.Error(t, err)
			assert.Nil(t, resp)
			var qb *dberrors.QBError
			require.True(t, stderrors.As(err, &qb))
			assert.Equal(t, tt.code, qb.Code)
			assert.Contains(t, err.Error(), tt.msg)
		})
	}
}

func TestBuildContext_Assembles(t *testing.T) {
	resp, err := BuildContext(context.Background(), BuildContextRequest{
		Results: []ResultInput{
			{Content: "func main() {}", Score: 0.92, Metadata: map[string]string{"file_path": "main.go", "start_line": "1", "end_line": "20"}},
			{Content: "package util", Score: 0.5},
		},
		IncludeCitations: true,
		Template:         "plain",
	})
	require.NoError(t, err)
	assert.Equal(t, 2, resp.ResultCount)
	assert.Contains(t, resp.Context, "func main() {}")
	require.NotEmpty(t, resp.Citations)
	assert.Equal(t, Citation{Index: 1, FilePath: "main.go", StartLine: 1, EndLine: 20, Score: 0.92}, resp.Citations[0])
}

func TestBuildContext_EmptyResultsIsInvalidInput(t *testing.T) {
	resp, err := BuildContext(context.Background(), BuildContextRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qb *dberrors.QBError
	require.True(t, stderrors.As(err, &qb))
	assert.Equal(t, dberrors.ErrInvalidInput, qb.Code)
	assert.Contains(t, err.Error(), "results array cannot be empty")
}

func TestParse(t *testing.T) {
	assert.Equal(t, "json", string(ParseTemplate("json")))
	assert.Equal(t, "xml", string(ParseTemplate("xml")))
	assert.Equal(t, "plain", string(ParseTemplate("plain")))
	assert.Equal(t, "markdown", string(ParseTemplate("")))
	assert.Equal(t, "markdown", string(ParseTemplate("html")))
	assert.Equal(t, "hard", string(ParseTruncationStrategy("hard")))
	assert.Equal(t, "none", string(ParseTruncationStrategy("none")))
	assert.Equal(t, "smart", string(ParseTruncationStrategy("")))
}
