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

package embedded

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
)

// errVectorNamespaceNotFound is the double's unknown-namespace error.
type errVectorNamespaceNotFound struct{ namespace string }

func (e *errVectorNamespaceNotFound) Error() string {
	return "NOT_FOUND: vector namespace " + e.namespace
}

// memVectorService is an in-memory VectorService double holding real state,
// so a test observes an upsert through a later get or search. Verbs the
// tests do not exercise fall through to the nil embedded interface.
type memVectorService struct {
	service.VectorService

	mu         sync.Mutex
	namespaces map[string]*service.NamespaceInfo
	vectors    map[string]map[uint64]*service.VectorData
}

func newMemVectorService() *memVectorService {
	return &memVectorService{
		namespaces: map[string]*service.NamespaceInfo{},
		vectors:    map[string]map[uint64]*service.VectorData{},
	}
}

func (m *memVectorService) store(namespace string) (map[uint64]*service.VectorData, error) {
	s, ok := m.vectors[namespace]
	if !ok {
		return nil, &errVectorNamespaceNotFound{namespace: namespace}
	}
	return s, nil
}

func (m *memVectorService) CreateNamespace(_ context.Context, req *service.CreateNamespaceRequest) (*service.CreateNamespaceResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.namespaces[req.Namespace] = &service.NamespaceInfo{
		Namespace: req.Namespace, Dimension: req.Dimension, IndexType: req.IndexType, CreatedAt: 1,
	}
	m.vectors[req.Namespace] = map[uint64]*service.VectorData{}
	return &service.CreateNamespaceResponse{Namespace: req.Namespace, Dimension: req.Dimension, Success: true, Timestamp: 1}, nil
}

func (m *memVectorService) ListNamespaces(_ context.Context, req *service.ListNamespacesRequest) (*service.ListNamespacesResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*service.NamespaceInfo
	for name, info := range m.namespaces {
		if strings.HasPrefix(name, req.Prefix) {
			n := *info
			n.VectorCount = uint64(len(m.vectors[name]))
			out = append(out, &n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Namespace < out[j].Namespace })
	total := len(out)
	out = out[min(req.Offset, total):]
	if req.Limit > 0 && len(out) > req.Limit {
		out = out[:req.Limit]
	}
	return &service.ListNamespacesResponse{Namespaces: out, TotalCount: total}, nil
}

func (m *memVectorService) UpsertVector(_ context.Context, req *service.UpsertVectorRequest) (*service.UpsertVectorResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.store(req.Namespace)
	if err != nil {
		return nil, err
	}
	version := uint64(1)
	if prev, ok := s[req.ID]; ok {
		version = prev.Version + 1
	}
	s[req.ID] = &service.VectorData{ID: req.ID, Vector: req.Vector, Metadata: req.Metadata, Version: version}
	return &service.UpsertVectorResponse{ID: req.ID, Version: version, Success: true}, nil
}

func (m *memVectorService) GetVector(_ context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.store(req.Namespace)
	if err != nil {
		return nil, err
	}
	d, ok := s[req.ID]
	if !ok {
		return &service.GetVectorResponse{ID: req.ID}, nil
	}
	return &service.GetVectorResponse{ID: d.ID, Vector: d.Vector, Metadata: d.Metadata, Version: d.Version, Found: true}, nil
}

func (m *memVectorService) DeleteVector(_ context.Context, req *service.DeleteVectorRequest) (*service.DeleteVectorResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.store(req.Namespace)
	if err != nil {
		return nil, err
	}
	_, ok := s[req.ID]
	delete(s, req.ID)
	return &service.DeleteVectorResponse{Success: ok}, nil
}

// SearchSimilar ranks by dot product, highest first.
func (m *memVectorService) SearchSimilar(_ context.Context, req *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.store(req.Namespace)
	if err != nil {
		return nil, err
	}
	var results []*service.VectorSearchResult
	for _, d := range s {
		var score float32
		for i := range min(len(d.Vector), len(req.QueryVector)) {
			score += d.Vector[i] * req.QueryVector[i]
		}
		results = append(results, &service.VectorSearchResult{ID: d.ID, Score: score, Metadata: d.Metadata})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > req.K {
		results = results[:req.K]
	}
	for i, r := range results {
		r.Rank = i + 1
	}
	return &service.SearchSimilarResponse{Results: results}, nil
}

type vectorMock struct {
	*mockQuicBaaS
	vector service.VectorService
}

func (m *vectorMock) Vector() service.VectorService { return m.vector }

func newVectorClient(t *testing.T, vs service.VectorService) vector.Client {
	t.Helper()
	tr, err := New(&vectorMock{mockQuicBaaS: &mockQuicBaaS{}, vector: vs})
	require.NoError(t, err)
	return vector.NewClient(tr)
}

func TestEmbeddedVector_NamespaceCreateAndList(t *testing.T) {
	ctx := context.Background()
	c := newVectorClient(t, newMemVectorService())

	for _, name := range []string{"docs", "docs-archive", "images"} {
		resp, err := c.CreateNamespace(ctx, &vector.CreateNamespaceRequest{
			Config: &vector.NamespaceConfig{Namespace: name, Dimensions: 3, IndexType: vector.IndexTypeHNSW},
		})
		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Equal(t, name, resp.Namespace)
	}

	page1, err := c.ListNamespaces(ctx, &vector.ListNamespacesRequest{Prefix: "docs", PageSize: 1})
	require.NoError(t, err)
	require.Len(t, page1.Namespaces, 1)
	assert.Equal(t, "docs", page1.Namespaces[0].Config.Namespace)
	assert.Equal(t, int32(3), page1.Namespaces[0].Config.Dimensions)
	assert.Equal(t, vector.IndexTypeHNSW, page1.Namespaces[0].Config.IndexType)
	assert.Equal(t, int64(2), page1.TotalCount)
	require.Equal(t, "1", page1.NextPageToken)

	page2, err := c.ListNamespaces(ctx, &vector.ListNamespacesRequest{Prefix: "docs", PageSize: 1, PageToken: page1.NextPageToken})
	require.NoError(t, err)
	require.Len(t, page2.Namespaces, 1)
	assert.Equal(t, "docs-archive", page2.Namespaces[0].Config.Namespace)
	assert.Empty(t, page2.NextPageToken)
}

func TestEmbeddedVector_NamespaceBadPageToken(t *testing.T) {
	c := newVectorClient(t, newMemVectorService())
	_, err := c.ListNamespaces(context.Background(), &vector.ListNamespacesRequest{PageToken: "next"})
	var tokErr *InvalidPageTokenError
	require.ErrorAs(t, err, &tokErr)
	assert.Equal(t, "next", tokErr.Value)
}

func TestEmbeddedVector_UpsertGetSearchDelete(t *testing.T) {
	ctx := context.Background()
	c := newVectorClient(t, newMemVectorService())
	_, err := c.CreateNamespace(ctx, &vector.CreateNamespaceRequest{
		Config: &vector.NamespaceConfig{Namespace: "docs", Dimensions: 2},
	})
	require.NoError(t, err)

	up, err := c.Upsert(ctx, &vector.UpsertRequest{Vector: &vector.VectorEntity{
		ID: "7", Namespace: "docs", Vector: []float32{1, 0}, Metadata: map[string]string{"lang": "go"},
	}})
	require.NoError(t, err)
	assert.Equal(t, "7", up.ID)
	assert.Equal(t, int64(1), up.Version)
	_, err = c.Upsert(ctx, &vector.UpsertRequest{Vector: &vector.VectorEntity{ID: "8", Namespace: "docs", Vector: []float32{0, 1}}})
	require.NoError(t, err)

	got, err := c.Get(ctx, &vector.GetRequest{Namespace: "docs", ID: "7", IncludeVector: true, IncludeMetadata: true})
	require.NoError(t, err)
	require.True(t, got.Found)
	assert.Equal(t, "7", got.Vector.ID)
	assert.Equal(t, "docs", got.Vector.Namespace)
	assert.Equal(t, []float32{1, 0}, got.Vector.Vector)
	assert.Equal(t, map[string]string{"lang": "go"}, got.Vector.Metadata)

	found, err := c.SearchSimilar(ctx, &vector.SearchSimilarRequest{Namespace: "docs", QueryVector: []float32{0.9, 0.1}, K: 1})
	require.NoError(t, err)
	require.Len(t, found.Results, 1)
	assert.Equal(t, "7", found.Results[0].Vector.ID)
	assert.Equal(t, int32(1), found.Results[0].Rank)

	del, err := c.Delete(ctx, &vector.DeleteRequest{Namespace: "docs", ID: "7"})
	require.NoError(t, err)
	assert.True(t, del.Deleted)

	gone, err := c.Get(ctx, &vector.GetRequest{Namespace: "docs", ID: "7"})
	require.NoError(t, err)
	assert.False(t, gone.Found)
	assert.Nil(t, gone.Vector)
}

func TestEmbeddedVector_UnknownNamespace(t *testing.T) {
	ctx := context.Background()
	c := newVectorClient(t, newMemVectorService())
	var nsErr *errVectorNamespaceNotFound

	_, err := c.Upsert(ctx, &vector.UpsertRequest{Vector: &vector.VectorEntity{ID: "1", Namespace: "missing", Vector: []float32{1}}})
	require.ErrorAs(t, err, &nsErr)
	_, err = c.Get(ctx, &vector.GetRequest{Namespace: "missing", ID: "1"})
	require.ErrorAs(t, err, &nsErr)
	_, err = c.SearchSimilar(ctx, &vector.SearchSimilarRequest{Namespace: "missing", QueryVector: []float32{1}, K: 1})
	require.ErrorAs(t, err, &nsErr)
	_, err = c.Delete(ctx, &vector.DeleteRequest{Namespace: "missing", ID: "1"})
	require.ErrorAs(t, err, &nsErr)
}

func TestEmbeddedVector_NonNumericID(t *testing.T) {
	c := newVectorClient(t, newMemVectorService())
	_, err := c.Get(context.Background(), &vector.GetRequest{Namespace: "docs", ID: "doc-1"})
	var idErr *InvalidVectorIDError
	require.ErrorAs(t, err, &idErr)
	assert.Equal(t, "Get", idErr.Method)
	assert.Equal(t, "doc-1", idErr.Value)
	assert.NotNil(t, errors.Unwrap(idErr))
}

func TestEmbeddedVector_UnsupportedFilterOperator(t *testing.T) {
	c := newVectorClient(t, newMemVectorService())
	_, err := c.SearchByMetadata(context.Background(), &vector.SearchByMetadataRequest{
		Namespace:       "docs",
		MetadataFilters: []*vector.MetadataFilter{{Key: "year", Operator: vector.FilterOperatorGreaterThan, Value: "2020"}},
	})
	var filterErr *UnsupportedVectorFilterError
	require.ErrorAs(t, err, &filterErr)
	assert.Equal(t, "gt", filterErr.Operator)
}

func TestEmbeddedVector_BatchUpsertMixedNamespaces(t *testing.T) {
	c := newVectorClient(t, newMemVectorService())
	_, err := c.BatchUpsert(context.Background(), &vector.BatchUpsertRequest{Vectors: []*vector.VectorEntity{
		{ID: "1", Namespace: "a", Vector: []float32{1}},
		{ID: "2", Namespace: "b", Vector: []float32{1}},
	}})
	var mixErr *MixedVectorNamespaceError
	require.ErrorAs(t, err, &mixErr)
	assert.Equal(t, "a", mixErr.First)
	assert.Equal(t, "b", mixErr.Other)
}

func TestEmbeddedVector_ServiceNotConfigured(t *testing.T) {
	c := newVectorClient(t, nil)
	_, err := c.GetStats(context.Background(), &vector.GetStatsRequest{Namespace: "docs"})
	var notCfg *ServiceNotConfiguredError
	require.ErrorAs(t, err, &notCfg)
	assert.Equal(t, "Vector", notCfg.Service)
}

func TestEmbeddedVector_InvalidRequestType(t *testing.T) {
	tr, err := New(&vectorMock{mockQuicBaaS: &mockQuicBaaS{}, vector: newMemVectorService()})
	require.NoError(t, err)
	_, err = tr.Request(context.Background(), "quicbaas.Vector", "Upsert", &vector.GetRequest{})
	var typeErr *InvalidRequestTypeError
	require.ErrorAs(t, err, &typeErr)
	assert.Equal(t, "*vector.UpsertRequest", typeErr.ExpectedType)
}

func TestEmbeddedVector_EveryServedMethodIsRegistered(t *testing.T) {
	methods := []string{
		"CreateNamespace", "DeleteNamespace", "ListNamespaces", "GetNamespaceInfo",
		"Upsert", "BatchUpsert", "Get", "Delete", "BatchDelete", "List", "UpdateMetadata",
		"SearchSimilar", "SearchByID", "HybridSearch", "SearchByMetadata",
		"RebuildIndex", "GetIndexProgress", "Compact", "GetCompactionProgress", "GetStats",
	}
	for _, m := range methods {
		_, ok := unaryHandlers["quicbaas.Vector/"+m]
		assert.True(t, ok, "quicbaas.Vector/%s not registered", m)
	}
}
