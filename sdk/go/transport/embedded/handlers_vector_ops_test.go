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
	"fmt"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
)

type errVectorNotFound struct{ id uint64 }

func (e *errVectorNotFound) Error() string {
	return "NOT_FOUND: vector " + strconv.FormatUint(e.id, 10)
}

type errCompactionNotFound struct{ operationID string }

func (e *errCompactionNotFound) Error() string {
	return "NOT_FOUND: compaction " + e.operationID
}

// opsVectorService extends memVectorService with the batch-delete,
// metadata-update, search-by-ID, hybrid-search and compaction-progress
// operations, so the embedded handlers for them run against in-memory state.
type opsVectorService struct {
	*memVectorService
	compactions map[string]*service.GetCompactionProgressResponse
}

func newOpsVectorService() *opsVectorService {
	return &opsVectorService{memVectorService: newMemVectorService()}
}

// rankVectors scores every kept vector by dot product with query and returns
// the best k, ranked from 1, plus the number of vectors kept.
func rankVectors(s map[uint64]*service.VectorData, query []float32, k int, keep func(*service.VectorData) bool) ([]*service.VectorSearchResult, int) {
	var results []*service.VectorSearchResult
	for _, d := range s {
		if !keep(d) {
			continue
		}
		var score float32
		for i := range min(len(d.Vector), len(query)) {
			score += d.Vector[i] * query[i]
		}
		results = append(results, &service.VectorSearchResult{ID: d.ID, Score: score, Metadata: d.Metadata})
	}
	kept := len(results)
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > k {
		results = results[:k]
	}
	for i, r := range results {
		r.Rank = i + 1
	}
	return results, kept
}

func (m *opsVectorService) BatchDeleteVectors(_ context.Context, req *service.BatchDeleteVectorsRequest) (*service.BatchDeleteVectorsResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.store(req.Namespace)
	if err != nil {
		return nil, err
	}
	resp := &service.BatchDeleteVectorsResponse{Success: true}
	for _, id := range req.IDs {
		if _, ok := s[id]; !ok {
			resp.FailedIDs = append(resp.FailedIDs, id)
			continue
		}
		delete(s, id)
		resp.DeletedCount++
	}
	return resp, nil
}

func (m *opsVectorService) UpdateMetadata(_ context.Context, req *service.UpdateVectorMetadataRequest) (*service.UpdateVectorMetadataResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.store(req.Namespace)
	if err != nil {
		return nil, err
	}
	d, ok := s[req.ID]
	if !ok {
		return nil, &errVectorNotFound{id: req.ID}
	}
	if !req.MergeMetadata || d.Metadata == nil {
		d.Metadata = make(map[string]interface{}, len(req.Metadata))
	}
	for k, v := range req.Metadata {
		d.Metadata[k] = v
	}
	d.Version++
	return &service.UpdateVectorMetadataResponse{ID: d.ID, Version: d.Version, Success: true}, nil
}

func (m *opsVectorService) SearchByID(_ context.Context, req *service.SearchByIDRequest) (*service.SearchByIDResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.store(req.Namespace)
	if err != nil {
		return nil, err
	}
	src, ok := s[req.ID]
	if !ok {
		return nil, &errVectorNotFound{id: req.ID}
	}
	results, _ := rankVectors(s, src.Vector, req.K, func(d *service.VectorData) bool {
		return !req.ExcludeSelf || d.ID != req.ID
	})
	return &service.SearchByIDResponse{SourceID: req.ID, Results: results}, nil
}

func (m *opsVectorService) HybridSearch(_ context.Context, req *service.HybridSearchRequest) (*service.HybridSearchResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.store(req.Namespace)
	if err != nil {
		return nil, err
	}
	results, kept := rankVectors(s, req.QueryVector, req.K, func(d *service.VectorData) bool {
		for k, v := range req.MetadataFilter {
			if fmt.Sprint(d.Metadata[k]) != fmt.Sprint(v) {
				return false
			}
		}
		return true
	})
	return &service.HybridSearchResponse{Results: results, TotalCandidates: len(s), TotalFiltered: kept}, nil
}

// ListVectors pages through a namespace in ID order; the cursor is the offset
// of the next page.
func (m *opsVectorService) ListVectors(_ context.Context, req *service.ListVectorsRequest) (*service.ListVectorsResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.store(req.Namespace)
	if err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(s))
	for id := range s {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	offset := 0
	if req.Cursor != "" {
		if offset, err = strconv.Atoi(req.Cursor); err != nil {
			return nil, err
		}
	}
	end := min(offset+req.Limit, len(ids))
	resp := &service.ListVectorsResponse{TotalCount: uint64(len(ids))}
	for _, id := range ids[offset:end] {
		d := &service.VectorData{ID: id, Version: s[id].Version}
		if req.IncludeVectors {
			d.Vector = s[id].Vector
		}
		if req.IncludeMetadata {
			d.Metadata = s[id].Metadata
		}
		resp.Vectors = append(resp.Vectors, d)
	}
	if end < len(ids) {
		resp.NextCursor = strconv.Itoa(end)
	}
	return resp, nil
}

func (m *opsVectorService) GetCompactionProgress(_ context.Context, req *service.GetCompactionProgressRequest) (*service.GetCompactionProgressResponse, error) {
	progress, ok := m.compactions[req.OperationID]
	if !ok {
		return nil, &errCompactionNotFound{operationID: req.OperationID}
	}
	return progress, nil
}

// newDocsVectorClient returns a vector client over vs with a "docs"
// namespace holding vectors 1 (lang go), 2 (lang go) and 3 (lang rust).
func newDocsVectorClient(t *testing.T, vs *opsVectorService) vector.Client {
	t.Helper()
	ctx := context.Background()
	c := newVectorClient(t, vs)
	_, err := c.CreateNamespace(ctx, &vector.CreateNamespaceRequest{
		Config: &vector.NamespaceConfig{Namespace: "docs", Dimensions: 2},
	})
	require.NoError(t, err)
	entities := []*vector.VectorEntity{
		{ID: "1", Namespace: "docs", Vector: []float32{1, 0}, Metadata: map[string]string{"lang": "go"}},
		{ID: "2", Namespace: "docs", Vector: []float32{0.8, 0.2}, Metadata: map[string]string{"lang": "go"}},
		{ID: "3", Namespace: "docs", Vector: []float32{0.9, 0.1}, Metadata: map[string]string{"lang": "rust"}},
	}
	for _, e := range entities {
		_, err := c.Upsert(ctx, &vector.UpsertRequest{Vector: e})
		require.NoError(t, err)
	}
	return c
}

// TestEmbeddedVector_BatchDeleteReportsMissingIDs asserts that a batch delete
// removes the vectors it names, counts them, and reports each ID the service
// could not delete.
func TestEmbeddedVector_BatchDeleteReportsMissingIDs(t *testing.T) {
	ctx := context.Background()
	c := newDocsVectorClient(t, newOpsVectorService())

	resp, err := c.BatchDelete(ctx, &vector.BatchDeleteRequest{Namespace: "docs", IDs: []string{"1", "3", "99"}})
	require.NoError(t, err)
	assert.Equal(t, int64(2), resp.DeletedCount)
	assert.Equal(t, map[string]string{"99": "delete failed"}, resp.Errors)

	for id, want := range map[string]bool{"1": false, "2": true, "3": false} {
		got, err := c.Get(ctx, &vector.GetRequest{Namespace: "docs", ID: id})
		require.NoError(t, err)
		assert.Equal(t, want, got.Found, "vector %s", id)
	}
}

// TestEmbeddedVector_BatchDeleteRefusesANonNumericID asserts that one bad ID
// refuses the whole batch with a typed error, and that nothing is deleted.
func TestEmbeddedVector_BatchDeleteRefusesANonNumericID(t *testing.T) {
	ctx := context.Background()
	c := newDocsVectorClient(t, newOpsVectorService())

	_, err := c.BatchDelete(ctx, &vector.BatchDeleteRequest{Namespace: "docs", IDs: []string{"1", "doc-2"}})
	var idErr *InvalidVectorIDError
	require.ErrorAs(t, err, &idErr)
	assert.Equal(t, "BatchDelete", idErr.Method)
	assert.Equal(t, "doc-2", idErr.Value)

	got, err := c.Get(ctx, &vector.GetRequest{Namespace: "docs", ID: "1"})
	require.NoError(t, err)
	assert.True(t, got.Found, "a refused batch must delete nothing")
}

// TestEmbeddedVector_ListPagesThroughANamespace asserts that List returns
// each page's vectors with the requested fields, carries the page token to
// the next page, and reports the namespace total.
func TestEmbeddedVector_ListPagesThroughANamespace(t *testing.T) {
	ctx := context.Background()
	c := newDocsVectorClient(t, newOpsVectorService())

	page1, err := c.List(ctx, &vector.ListVectorsRequest{Namespace: "docs", PageSize: 2, IncludeMetadata: true})
	require.NoError(t, err)
	require.Len(t, page1.Vectors, 2)
	assert.Equal(t, "1", page1.Vectors[0].ID)
	assert.Equal(t, "docs", page1.Vectors[0].Namespace)
	assert.Equal(t, map[string]string{"lang": "go"}, page1.Vectors[0].Metadata)
	assert.Empty(t, page1.Vectors[0].Vector, "vectors were not requested")
	assert.Equal(t, int64(3), page1.TotalCount)
	require.Equal(t, "2", page1.NextPageToken)

	page2, err := c.List(ctx, &vector.ListVectorsRequest{Namespace: "docs", PageSize: 2, PageToken: page1.NextPageToken, IncludeVectors: true})
	require.NoError(t, err)
	require.Len(t, page2.Vectors, 1)
	assert.Equal(t, "3", page2.Vectors[0].ID)
	assert.Equal(t, []float32{0.9, 0.1}, page2.Vectors[0].Vector)
	assert.Empty(t, page2.NextPageToken)
}

// TestEmbeddedVector_ListUnknownNamespace asserts that the service's
// namespace-not-found error reaches the List caller.
func TestEmbeddedVector_ListUnknownNamespace(t *testing.T) {
	c := newVectorClient(t, newOpsVectorService())
	_, err := c.List(context.Background(), &vector.ListVectorsRequest{Namespace: "missing", PageSize: 1})
	var nsErr *errVectorNamespaceNotFound
	require.ErrorAs(t, err, &nsErr)
}

// TestEmbeddedVector_UpdateMetadataMergeAndReplace asserts that Merge keeps
// the existing metadata keys, that a replace drops them, and that each update
// raises the vector's version.
func TestEmbeddedVector_UpdateMetadataMergeAndReplace(t *testing.T) {
	ctx := context.Background()
	c := newDocsVectorClient(t, newOpsVectorService())
	metadataOf := func() map[string]string {
		t.Helper()
		got, err := c.Get(ctx, &vector.GetRequest{Namespace: "docs", ID: "1", IncludeMetadata: true})
		require.NoError(t, err)
		return got.Vector.Metadata
	}

	merged, err := c.UpdateMetadata(ctx, &vector.UpdateMetadataRequest{
		Namespace: "docs", ID: "1", Metadata: map[string]string{"year": "2026"}, Merge: true,
	})
	require.NoError(t, err)
	assert.True(t, merged.Success)
	assert.Equal(t, int64(2), merged.Version)
	assert.Equal(t, map[string]string{"lang": "go", "year": "2026"}, metadataOf())

	replaced, err := c.UpdateMetadata(ctx, &vector.UpdateMetadataRequest{
		Namespace: "docs", ID: "1", Metadata: map[string]string{"owner": "ops"},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(3), replaced.Version)
	assert.Equal(t, map[string]string{"owner": "ops"}, metadataOf())
}

// TestEmbeddedVector_UpdateMetadataErrors asserts that a non-numeric ID is
// refused before the service, and that the service's not-found error reaches
// the caller.
func TestEmbeddedVector_UpdateMetadataErrors(t *testing.T) {
	ctx := context.Background()
	c := newDocsVectorClient(t, newOpsVectorService())

	metadata := map[string]string{"year": "2026"}
	_, err := c.UpdateMetadata(ctx, &vector.UpdateMetadataRequest{Namespace: "docs", ID: "doc-1", Metadata: metadata})
	var idErr *InvalidVectorIDError
	require.ErrorAs(t, err, &idErr)
	assert.Equal(t, "UpdateMetadata", idErr.Method)

	_, err = c.UpdateMetadata(ctx, &vector.UpdateMetadataRequest{Namespace: "docs", ID: "42", Metadata: metadata})
	var notFound *errVectorNotFound
	require.ErrorAs(t, err, &notFound)
	assert.Equal(t, uint64(42), notFound.id)
}

// TestEmbeddedVector_SearchByIDHonoursIncludeSelf asserts that a search from
// an existing vector leaves the source out unless IncludeSelf asks for it.
func TestEmbeddedVector_SearchByIDHonoursIncludeSelf(t *testing.T) {
	ctx := context.Background()
	c := newDocsVectorClient(t, newOpsVectorService())
	idsOf := func(results []*vector.SearchResult) []string {
		ids := make([]string, 0, len(results))
		for _, r := range results {
			ids = append(ids, r.Vector.ID)
		}
		return ids
	}

	without, err := c.SearchByID(ctx, &vector.SearchByIDRequest{Namespace: "docs", ID: "1", K: 2})
	require.NoError(t, err)
	assert.Equal(t, []string{"3", "2"}, idsOf(without.Results))
	assert.Equal(t, int64(2), without.TotalCandidates)

	with, err := c.SearchByID(ctx, &vector.SearchByIDRequest{Namespace: "docs", ID: "1", K: 2, IncludeSelf: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"1", "3"}, idsOf(with.Results))
}

// TestEmbeddedVector_SearchByIDErrors asserts that a non-numeric source ID is
// refused and that an unknown source reaches the caller as not found.
func TestEmbeddedVector_SearchByIDErrors(t *testing.T) {
	ctx := context.Background()
	c := newDocsVectorClient(t, newOpsVectorService())

	_, err := c.SearchByID(ctx, &vector.SearchByIDRequest{Namespace: "docs", ID: "doc-1", K: 1})
	var idErr *InvalidVectorIDError
	require.ErrorAs(t, err, &idErr)
	assert.Equal(t, "SearchByID", idErr.Method)

	_, err = c.SearchByID(ctx, &vector.SearchByIDRequest{Namespace: "docs", ID: "42", K: 1})
	var notFound *errVectorNotFound
	require.ErrorAs(t, err, &notFound)
}

// TestEmbeddedVector_HybridSearchFiltersOnMetadata asserts that a hybrid
// search ranks only the vectors matching its equality filter, reports the
// candidate and filtered counts, and flags reranking when one is requested.
func TestEmbeddedVector_HybridSearchFiltersOnMetadata(t *testing.T) {
	ctx := context.Background()
	c := newDocsVectorClient(t, newOpsVectorService())

	resp, err := c.HybridSearch(ctx, &vector.HybridSearchRequest{
		Namespace:          "docs",
		QueryVector:        []float32{1, 0},
		K:                  5,
		MetadataFilters:    []*vector.MetadataFilter{{Key: "lang", Operator: vector.FilterOperatorEquals, Value: "go"}},
		RerankingAlgorithm: "rrf",
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 2)
	assert.Equal(t, "1", resp.Results[0].Vector.ID)
	assert.Equal(t, "2", resp.Results[1].Vector.ID)
	assert.Equal(t, int64(3), resp.TotalCandidates)
	assert.Equal(t, int64(2), resp.TotalFiltered)
	assert.True(t, resp.RerankingApplied)

	plain, err := c.HybridSearch(ctx, &vector.HybridSearchRequest{Namespace: "docs", QueryVector: []float32{1, 0}, K: 5})
	require.NoError(t, err)
	assert.Len(t, plain.Results, 3)
	assert.False(t, plain.RerankingApplied)
}

// TestEmbeddedVector_HybridSearchRefusesUnsupportedFilter asserts that a
// filter operator the service cannot evaluate is refused with a typed error
// naming HybridSearch.
func TestEmbeddedVector_HybridSearchRefusesUnsupportedFilter(t *testing.T) {
	c := newDocsVectorClient(t, newOpsVectorService())
	_, err := c.HybridSearch(context.Background(), &vector.HybridSearchRequest{
		Namespace:       "docs",
		QueryVector:     []float32{1, 0},
		K:               1,
		MetadataFilters: []*vector.MetadataFilter{{Key: "year", Operator: vector.FilterOperatorGreaterThan, Value: "2020"}},
	})
	var filterErr *UnsupportedVectorFilterError
	require.ErrorAs(t, err, &filterErr)
	assert.Equal(t, "HybridSearch", filterErr.Method)
	assert.Equal(t, "year", filterErr.Key)
}

// TestEmbeddedVector_GetCompactionProgressStatus asserts that the service's
// progress maps to the completed, running and failed statuses, and that the
// elapsed time of a finished compaction is its completion minus its start.
func TestEmbeddedVector_GetCompactionProgressStatus(t *testing.T) {
	started := int64(1_000_000_000)
	completed := started + 250_000_000
	vs := newOpsVectorService()
	vs.compactions = map[string]*service.GetCompactionProgressResponse{
		"op-done":    {OperationID: "op-done", Namespace: "docs", BytesCompacted: 512, TotalBytes: 512, PercentComplete: 100, StartedAt: started, CompletedAt: &completed},
		"op-running": {OperationID: "op-running", Namespace: "docs", BytesCompacted: 128, TotalBytes: 512, PercentComplete: 25, InProgress: true, StartedAt: started},
		"op-failed":  {OperationID: "op-failed", Namespace: "docs", InProgress: true, Error: "disk full"},
	}
	c := newVectorClient(t, vs)

	cases := []struct {
		operationID string
		status      string
		percent     float32
	}{
		{"op-done", "completed", 100},
		{"op-running", "running", 25},
		{"op-failed", "failed", 0},
	}
	for _, tc := range cases {
		t.Run(tc.operationID, func(t *testing.T) {
			resp, err := c.GetCompactionProgress(context.Background(), &vector.GetCompactionProgressRequest{Namespace: "docs", OperationID: tc.operationID})
			require.NoError(t, err)
			assert.Equal(t, tc.operationID, resp.OperationID)
			assert.Equal(t, "docs", resp.Namespace)
			assert.Equal(t, tc.status, resp.Status)
			assert.Equal(t, tc.percent, resp.PercentComplete)
		})
	}

	done, err := c.GetCompactionProgress(context.Background(), &vector.GetCompactionProgressRequest{Namespace: "docs", OperationID: "op-done"})
	require.NoError(t, err)
	assert.Equal(t, int64(250), done.ElapsedTimeMs)
	assert.Equal(t, int64(512), done.SpaceReclaimedBytes)

	failed, err := c.GetCompactionProgress(context.Background(), &vector.GetCompactionProgressRequest{Namespace: "docs", OperationID: "op-failed"})
	require.NoError(t, err)
	assert.Zero(t, failed.ElapsedTimeMs, "a compaction that never started has no elapsed time")
}

// TestEmbeddedVector_GetCompactionProgressUnknownOperation asserts that the
// service's not-found error for an unknown operation reaches the caller.
func TestEmbeddedVector_GetCompactionProgressUnknownOperation(t *testing.T) {
	c := newVectorClient(t, newOpsVectorService())
	_, err := c.GetCompactionProgress(context.Background(), &vector.GetCompactionProgressRequest{Namespace: "docs", OperationID: "op-x"})
	var notFound *errCompactionNotFound
	require.ErrorAs(t, err, &notFound)
	assert.Equal(t, "op-x", notFound.operationID)
}
