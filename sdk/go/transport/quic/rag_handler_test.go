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

package quic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rag"
)

func TestIsRAGService(t *testing.T) {
	assert.True(t, isRAGService("quicbaas.RAG"))
	assert.True(t, isRAGService("RAG"))
	assert.False(t, isRAGService("quicbaas.KV"))
}

func TestRAGMethodToOpcode(t *testing.T) {
	t.Run("known", func(t *testing.T) {
		op, ok := RAGMethodToOpcode("quicbaas.RAG", "Query")
		require.True(t, ok)
		assert.Equal(t, OpRAGQuery, op)
	})
	t.Run("unknown", func(t *testing.T) {
		_, ok := RAGMethodToOpcode("quicbaas.RAG", "Nope")
		assert.False(t, ok)
	})
}

func TestRAGOpcodeToMethod(t *testing.T) {
	t.Run("known", func(t *testing.T) {
		m, ok := RAGOpcodeToMethod(OpRAGBuildContext)
		require.True(t, ok)
		assert.Equal(t, "quicbaas.RAG/BuildContext", m)
	})
	t.Run("unknown", func(t *testing.T) {
		_, ok := RAGOpcodeToMethod(0x00)
		assert.False(t, ok)
	})
}

// TestConvertRAGResponse covers the Query, BuildContext and fallback branches.
func TestConvertRAGResponse(t *testing.T) {
	t.Run("Query", func(t *testing.T) {
		got, err := convertRAGResponse("Query", map[string]interface{}{
			"context":      "ctx",
			"tokens":       float64(12),
			"truncated":    true,
			"result_count": float64(2),
			"citations": []interface{}{
				map[string]interface{}{
					"index": float64(0), "file_path": "a.go", "start_line": float64(1),
					"end_line": float64(5), "score": float64(0.9),
				},
				"not-a-map", // skip branch
			},
		})
		require.NoError(t, err)
		resp := got.(*rag.QueryResponse)
		assert.Equal(t, "ctx", resp.Context)
		assert.Equal(t, 12, resp.Tokens)
		assert.True(t, resp.Truncated)
		require.Len(t, resp.Citations, 2)
		assert.Equal(t, "a.go", resp.Citations[0].FilePath)
		assert.Equal(t, float32(0.9), resp.Citations[0].Score)
	})

	t.Run("BuildContext", func(t *testing.T) {
		got, err := convertRAGResponse("BuildContext", map[string]interface{}{
			"context":      "c2",
			"tokens":       float64(3),
			"truncated":    false,
			"result_count": float64(1),
			"citations": []interface{}{
				map[string]interface{}{"index": float64(1), "file_path": "b.go"},
			},
		})
		require.NoError(t, err)
		resp := got.(*rag.BuildContextResponse)
		assert.Equal(t, "c2", resp.Context)
		require.Len(t, resp.Citations, 1)
		assert.Equal(t, "b.go", resp.Citations[0].FilePath)
	})

	t.Run("fallback", func(t *testing.T) {
		got, err := convertRAGResponse("Other", map[string]interface{}{"x": 1})
		require.NoError(t, err)
		m := got.(map[string]interface{})
		assert.Equal(t, 1, m["x"])
	})
}

// TestTransformRAGRequest covers both typed branches plus the pass-through.
func TestTransformRAGRequest(t *testing.T) {
	t.Run("Query typed", func(t *testing.T) {
		out, err := transformRAGRequest("Query", &rag.QueryRequest{
			Query: "q", Namespace: "ns", TopK: 5, MaxTokens: 100,
			MinScore: 0.5, Template: "t", IncludeCitations: true, Model: "m",
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "q", m["query"])
		assert.Equal(t, "ns", m["namespace"])
		assert.Equal(t, true, m["include_citations"])
	})

	t.Run("Query non-typed pass-through", func(t *testing.T) {
		in := map[string]interface{}{"raw": true}
		out, err := transformRAGRequest("Query", in)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})

	t.Run("BuildContext typed", func(t *testing.T) {
		out, err := transformRAGRequest("BuildContext", &rag.BuildContextRequest{
			Results: []rag.ResultInput{
				{Content: "doc", Score: 0.8, Metadata: map[string]string{"k": "v"}},
			},
			MaxTokens: 200, Template: "tmpl", IncludeCitations: true, TruncationStrategy: "tail",
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		results := m["results"].([]map[string]interface{})
		require.Len(t, results, 1)
		assert.Equal(t, "doc", results[0]["content"])
		assert.Equal(t, "tail", m["truncation_strategy"])
	})

	t.Run("BuildContext non-typed pass-through", func(t *testing.T) {
		in := map[string]interface{}{"raw": true}
		out, err := transformRAGRequest("BuildContext", in)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})

	t.Run("unknown method pass-through", func(t *testing.T) {
		in := map[string]interface{}{"a": 1}
		out, err := transformRAGRequest("Bogus", in)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})
}

func TestExtractMethodName(t *testing.T) {
	assert.Equal(t, "Query", extractMethodName("quicbaas.RAG/Query"))
	assert.Equal(t, "Plain", extractMethodName("Plain"))
}
