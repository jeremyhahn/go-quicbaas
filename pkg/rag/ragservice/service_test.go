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
	"fmt"
	"strings"
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
		// The vector service reports the cosine distance (1 - cos) as both
		// Distance and Score; ragservice turns it into the similarity.
		{ID: 7, Distance: 0.06, Score: 0.06, Metadata: map[string]interface{}{
			"content": "type Pool struct{}", "file_path": "db/pool.go", "file_name": "pool.go",
			"language": "go", "start_line": float64(23), "end_line": "45",
		}},
		{ID: 9, Distance: 0.19, Score: 0.19, Metadata: map[string]interface{}{
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
			GetNamespaceInfoFunc: func(context.Context, *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error) {
				return &service.GetNamespaceInfoResponse{DistanceMetric: "COSINE"}, nil
			},
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
	assert.Equal(t, float32(0), search.MinScore, "the threshold is a similarity, applied by ragservice, not a backend distance")
	assert.True(t, search.IncludeMetadata)
	assert.False(t, search.IncludeVectors)
	assert.Empty(t, model, "no model asked for, so the default embedder is used")

	assert.Equal(t, 2, resp.ResultCount)
	assert.Contains(t, resp.Context, "type Pool struct{}")
	assert.Contains(t, resp.Context, "func Open() {}")
	assert.Positive(t, resp.Tokens)
	require.Len(t, resp.Citations, 2)
	assert.InDelta(t, 0.94, resp.Citations[0].Score, 1e-6)
	assert.InDelta(t, 0.81, resp.Citations[1].Score, 1e-6)
	resp.Citations[0].Score, resp.Citations[1].Score = 0, 0
	assert.Equal(t, Citation{Index: 1, FilePath: "db/pool.go", StartLine: 23, EndLine: 45}, resp.Citations[0])
	assert.Equal(t, Citation{Index: 2, FilePath: "db/open.go", StartLine: 3, EndLine: 8}, resp.Citations[1])
}

// TestQuery_ExplicitZeroMinScoreDisablesTheFilter proves only an omitted
// threshold takes DefaultMinScore: a caller that sends 0 searches with 0.
func TestQuery_ExplicitZeroMinScoreDisablesTheFilter(t *testing.T) {
	var search service.SearchSimilarRequest
	var model string
	_, err := Query(context.Background(), working(&search, &model), QueryRequest{
		Query: "q", Namespace: "n", MinScore: 0, MinScoreSet: true,
	})
	require.NoError(t, err)
	assert.Equal(t, float32(0), search.MinScore)
}

func TestQuery_PassesKnobsAndModel(t *testing.T) {
	var search service.SearchSimilarRequest
	var model string
	resp, err := Query(context.Background(), working(&search, &model), QueryRequest{
		Query: "q", Namespace: "n", TopK: 3, MinScore: 0.5, Model: "mxbai", Template: "json",
	})
	require.NoError(t, err)
	assert.Equal(t, 3, search.K)
	assert.Equal(t, float32(0), search.MinScore, "the threshold is a similarity, applied by ragservice, not a backend distance")
	assert.Equal(t, "mxbai", model)
	assert.Equal(t, []float32{0.3, 0.4}, search.QueryVector)
	assert.Empty(t, resp.Citations, "citations were not asked for")
}

func TestQuery_Errors(t *testing.T) {
	var search service.SearchSimilarRequest
	var model string
	ok := working(&search, &model)
	cause := stderrors.New("boom")
	searchedAfterInfoError := false

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
		{"namespace info fails", backends{
			ollama: ok.ollama,
			vector: &mocks.MockVectorService{
				GetNamespaceInfoFunc: func(context.Context, *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error) {
					return nil, dberrors.New(dberrors.ErrNotFound, "vector.namespace", "no such namespace")
				},
				SearchSimilarFunc: func(context.Context, *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
					searchedAfterInfoError = true
					return &service.SearchSimilarResponse{Results: hits()}, nil
				},
			},
		}, QueryRequest{Query: "q", Namespace: "n"}, dberrors.ErrNotFound, "vector namespace info"},
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
	assert.False(t, searchedAfterInfoError, "a namespace info error stops Query before the search")
}

// TestQuery_ThresholdIsSimilarityPerMetric feeds hits the way the vector
// service returns them (the HNSW distance as Distance and Score, ascending)
// and checks MinScore is applied to the similarity for the namespace's
// metric and the citations report that similarity.
func TestQuery_ThresholdIsSimilarityPerMetric(t *testing.T) {
	hit := func(id uint64, d float32) *service.VectorSearchResult {
		return &service.VectorSearchResult{ID: id, Distance: d, Score: d, Metadata: map[string]interface{}{
			"content": fmt.Sprintf("chunk %d", id), "file_path": fmt.Sprintf("f%d.go", id),
		}}
	}
	tests := []struct {
		name     string
		metric   string
		found    []*service.VectorSearchResult
		minScore float32
		want     []float32
	}{
		{"cosine keeps similarity above threshold", "COSINE",
			[]*service.VectorSearchResult{hit(1, 0.06), hit(2, 0.19), hit(3, 0.6)}, 0.8, []float32{0.94, 0.81}},
		{"cosine lower case", "cosine",
			[]*service.VectorSearchResult{hit(1, 0.06), hit(3, 0.6)}, 0.9, []float32{0.94}},
		{"dot product kept", "DOT_PRODUCT",
			[]*service.VectorSearchResult{hit(1, -0.9)}, 0.85, []float32{0.9}},
		{"dot product dropped", "dot_product",
			[]*service.VectorSearchResult{hit(1, -0.9), hit(2, -0.99)}, 0.95, []float32{0.99}},
		{"l2", "L2",
			[]*service.VectorSearchResult{hit(1, 0.25), hit(2, 1)}, 0.8, []float32{0.8}},
		{"empty metric is l2", "",
			[]*service.VectorSearchResult{hit(1, 0.25), hit(2, 1)}, 0.75, []float32{0.8}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var search service.SearchSimilarRequest
			b := backends{
				ollama: &mocks.MockOllamaService{EmbedFunc: func(context.Context, *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
					return &service.OllamaEmbedResponse{Embedding: []float32{1, 0}}, nil
				}},
				vector: &mocks.MockVectorService{
					GetNamespaceInfoFunc: func(_ context.Context, r *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error) {
						assert.Equal(t, "n", r.Namespace)
						return &service.GetNamespaceInfoResponse{DistanceMetric: tt.metric}, nil
					},
					SearchSimilarFunc: func(_ context.Context, r *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
						search = *r
						return &service.SearchSimilarResponse{Results: tt.found}, nil
					},
				},
			}
			resp, err := Query(context.Background(), b, QueryRequest{
				Query: "q", Namespace: "n", MinScore: tt.minScore, MinScoreSet: true, IncludeCitations: true,
			})
			require.NoError(t, err)
			assert.Equal(t, float32(0), search.MinScore)
			assert.Equal(t, len(tt.want), resp.ResultCount)
			require.Len(t, resp.Citations, len(tt.want))
			for i, w := range tt.want {
				assert.InDelta(t, w, resp.Citations[i].Score, 1e-6, "citation %d", i)
			}
		})
	}

	t.Run("explicit zero keeps every hit", func(t *testing.T) {
		b := backends{
			ollama: &mocks.MockOllamaService{EmbedFunc: func(context.Context, *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
				return &service.OllamaEmbedResponse{Embedding: []float32{1, 0}}, nil
			}},
			vector: &mocks.MockVectorService{
				GetNamespaceInfoFunc: func(context.Context, *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error) {
					return &service.GetNamespaceInfoResponse{DistanceMetric: "COSINE"}, nil
				},
				SearchSimilarFunc: func(context.Context, *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
					return &service.SearchSimilarResponse{Results: []*service.VectorSearchResult{hit(1, 0.06), hit(2, 0.6), hit(3, 1.5)}}, nil
				},
			},
		}
		resp, err := Query(context.Background(), b, QueryRequest{
			Query: "q", Namespace: "n", MinScore: 0, MinScoreSet: true, IncludeCitations: true,
		})
		require.NoError(t, err)
		assert.Equal(t, 3, resp.ResultCount)
		require.Len(t, resp.Citations, 3)
		assert.InDelta(t, 0.4, resp.Citations[1].Score, 1e-6)
	})
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

func countedResults(n int) []ResultInput {
	in := make([]ResultInput, n)
	for i := range in {
		in[i] = ResultInput{
			Content: fmt.Sprintf("item-%02d holds a few short words of content", i),
			Score:   0.9 - float32(i)*0.01,
		}
	}
	return in
}

// ResultCount is the number of results the context kept: the builder
// caps it at MaxResultsPerChunk (20) however large the budget.
func TestBuildContext_ResultCountCapsAtMaxResultsPerChunk(t *testing.T) {
	resp, err := BuildContext(context.Background(), BuildContextRequest{
		Results:   countedResults(21),
		MaxTokens: 100000,
		Template:  "plain",
	})
	require.NoError(t, err)
	assert.Equal(t, 20, resp.ResultCount)
	assert.Contains(t, resp.Context, "item-19")
	assert.NotContains(t, resp.Context, "item-20")
}

// A small token budget drops results: ResultCount counts the ones kept.
func TestBuildContext_ResultCountAfterTruncation(t *testing.T) {
	resp, err := BuildContext(context.Background(), BuildContextRequest{
		Results:            countedResults(10),
		MaxTokens:          60,
		Template:           "plain",
		TruncationStrategy: "hard",
	})
	require.NoError(t, err)
	require.True(t, resp.Truncated)
	kept := 0
	for i := 0; i < 10; i++ {
		if strings.Contains(resp.Context, fmt.Sprintf("item-%02d", i)) {
			kept++
		}
	}
	assert.Less(t, resp.ResultCount, 10)
	assert.Positive(t, resp.ResultCount)
	assert.Equal(t, kept, resp.ResultCount)
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
