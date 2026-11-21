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

package quicbaas

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeQRDBVector is written against the re-exported names only: it compiles
// only if sdk/go carries every type a QRDBVectorClient call needs.
type fakeQRDBVector struct {
	QRDBVectorClient
	created  *QRDBCreateNamespaceRequest
	searched *QRDBSearchSimilarRequest
	rebuilt  *QRDBRebuildIndexRequest
}

func (f *fakeQRDBVector) CreateNamespace(_ context.Context, req *QRDBCreateNamespaceRequest) (*QRDBCreateNamespaceResponse, error) {
	f.created = req
	return &QRDBCreateNamespaceResponse{}, nil
}

func (f *fakeQRDBVector) SearchSimilar(_ context.Context, req *QRDBSearchSimilarRequest) (*QRDBSearchSimilarResponse, error) {
	f.searched = req
	return &QRDBSearchSimilarResponse{Results: []*QRDBVectorSearchResult{{ID: 7, Score: 0.9}}}, nil
}

func (f *fakeQRDBVector) RebuildIndex(_ context.Context, req *QRDBRebuildIndexRequest) (*QRDBRebuildIndexResponse, error) {
	f.rebuilt = req
	return &QRDBRebuildIndexResponse{}, nil
}

func TestQRDBVectorReexport_HNSWNamespaceSearchAndRebuild(t *testing.T) {
	fake := &fakeQRDBVector{}
	var vc QRDBVectorClient = fake
	ctx := context.Background()

	_, err := vc.CreateNamespace(ctx, &QRDBCreateNamespaceRequest{
		Namespace:      "docs",
		Dimension:      3,
		DistanceMetric: QRDBDistanceCosine,
		IndexType:      QRDBIndexHNSW,
		IndexConfig:    map[string]string{"m": "16", "ef_construction": "200"},
	})
	require.NoError(t, err)
	require.NotNil(t, fake.created)
	assert.Equal(t, "hnsw", fake.created.IndexType)
	assert.Equal(t, "cosine", fake.created.DistanceMetric)

	resp, err := vc.SearchSimilar(ctx, &QRDBSearchSimilarRequest{Namespace: "docs", QueryVector: []float32{1, 0, 0}, K: 1, EfSearch: 64})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, uint64(7), resp.Results[0].ID)
	assert.Equal(t, "docs", fake.searched.Namespace)

	_, err = vc.RebuildIndex(ctx, &QRDBRebuildIndexRequest{Namespace: "docs"})
	require.NoError(t, err)
	assert.Equal(t, "docs", fake.rebuilt.Namespace)
}

func TestQRDBVectorReexport_Constants(t *testing.T) {
	assert.Equal(t, "cosine", QRDBDistanceCosine)
	assert.Equal(t, "euclidean", QRDBDistanceEuclidean)
	assert.Equal(t, "dot_product", QRDBDistanceDotProduct)
	assert.Equal(t, "hnsw", QRDBIndexHNSW)
	assert.Equal(t, "flat", QRDBIndexFlat)
	assert.Equal(t, "ivf_flat", QRDBIndexIVFFlat)
}
