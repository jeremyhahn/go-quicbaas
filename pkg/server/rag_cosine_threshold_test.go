//go:build rag && vector && qrdb_vector

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

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/ragservice"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// cosineVectorStorage embeds interfaces.VectorStorage (nil) and overrides only
// what SearchSimilar and its lazy ensureIndex reach: one cosine namespace
// holding a fixed set of vectors. ListNamespaces is empty so the index is
// built lazily with the namespace's metric, as on a replicated node.
type cosineVectorStorage struct {
	interfaces.VectorStorage
	vectors map[uint64]*interfaces.VectorValue
}

func (s *cosineVectorStorage) ListNamespaces(context.Context) ([]interfaces.VectorNamespaceInfo, error) {
	return nil, nil
}

func (s *cosineVectorStorage) GetNamespaceInfo(_ context.Context, namespace string) (*interfaces.VectorNamespaceInfo, error) {
	return &interfaces.VectorNamespaceInfo{
		Name: namespace, Dimension: 3, VectorCount: uint64(len(s.vectors)),
		DistanceMetric: "COSINE", IndexType: "HNSW",
	}, nil
}

func (s *cosineVectorStorage) ScanNamespace(context.Context, string, *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
	out := make([]*interfaces.VectorValue, 0, len(s.vectors))
	for _, v := range s.vectors {
		out = append(out, v)
	}
	return out, nil
}

func (s *cosineVectorStorage) GetVector(_ context.Context, _ string, id uint64) (*interfaces.VectorValue, error) {
	if v, ok := s.vectors[id]; ok {
		return v, nil
	}
	return nil, errors.New("vector not found")
}

type ragTestBackends struct {
	ollama service.OllamaService
	vector service.VectorService
}

func (b ragTestBackends) Ollama() service.OllamaService { return b.ollama }
func (b ragTestBackends) Vector() service.VectorService { return b.vector }

// TestRAGQuery_RealCosineIndex runs ragservice.Query against the real vector
// service and HNSW index on a cosine namespace: MinScore is a similarity, so
// it keeps the hits at or above it, and the citations carry the similarity.
func TestRAGQuery_RealCosineIndex(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	storage := &cosineVectorStorage{vectors: map[uint64]*interfaces.VectorValue{
		1: {ID: 1, Embedding: []float32{1, 0, 0}, Metadata: map[string]string{"content": "chunk A", "file_path": "a.go"}},
		2: {ID: 2, Embedding: []float32{0.9, 0.43589, 0}, Metadata: map[string]string{"content": "chunk B", "file_path": "b.go"}},
		3: {ID: 3, Embedding: []float32{0.4, 0.91652, 0}, Metadata: map[string]string{"content": "chunk C", "file_path": "c.go"}},
	}}
	vectorSvc, err := service.NewVectorService(storage, logger.NewAdapter(log),
		service.WithVectorIndexFactory(NewVectorIndexFactory()))
	require.NoError(t, err)

	backends := ragTestBackends{
		ollama: &mocks.MockOllamaService{
			EmbedFunc: func(context.Context, *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
				return &service.OllamaEmbedResponse{Embedding: []float32{1, 0, 0}}, nil
			},
		},
		vector: vectorSvc,
	}
	ctx := tenantctx.WithSystem(context.Background())

	tests := []struct {
		name     string
		minScore float32
		keep     []string
		drop     []string
		scores   []float32
	}{
		{"0.85 keeps A and B", 0.85, []string{"chunk A", "chunk B"}, []string{"chunk C"}, []float32{1.0, 0.9}},
		{"0.95 keeps only A", 0.95, []string{"chunk A"}, []string{"chunk B", "chunk C"}, []float32{1.0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := ragservice.Query(ctx, backends, ragservice.QueryRequest{
				Query: "q", Namespace: "code", MinScore: tt.minScore, MinScoreSet: true, IncludeCitations: true,
			})
			require.NoError(t, err)
			assert.Equal(t, len(tt.keep), resp.ResultCount)
			for _, c := range tt.keep {
				assert.Contains(t, resp.Context, c)
			}
			for _, c := range tt.drop {
				assert.NotContains(t, resp.Context, c)
			}
			require.Len(t, resp.Citations, len(tt.scores))
			for i, s := range tt.scores {
				assert.InDelta(t, s, resp.Citations[i].Score, 1e-3, "citation %d", i)
			}
		})
	}
}
