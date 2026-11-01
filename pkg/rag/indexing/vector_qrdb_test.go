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

package indexing

import (
	"context"
	"errors"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag"
)

// ---------------------------------------------------------------------------
// Mock VectorClient
// ---------------------------------------------------------------------------

// mockVectorClient provides a fully-controllable in-memory VectorClient.
type mockVectorClient struct {
	namespaces map[string]*mockNamespace

	// Injected error hooks — set to force specific paths.
	createNamespaceErr error
	upsertErr          error
	deleteErr          error
	searchErr          error
	nsInfoErr          error
}

type mockNamespace struct {
	dimension uint32
	vectors   map[uint64]*qrdbtransport.VectorData
}

func newMockVectorClient() *mockVectorClient {
	return &mockVectorClient{
		namespaces: make(map[string]*mockNamespace),
	}
}

func (m *mockVectorClient) getOrCreateNS(ns string, dim uint32) *mockNamespace {
	if n, ok := m.namespaces[ns]; ok {
		return n
	}
	n := &mockNamespace{dimension: dim, vectors: make(map[uint64]*qrdbtransport.VectorData)}
	m.namespaces[ns] = n
	return n
}

func (m *mockVectorClient) CreateNamespace(_ context.Context, req *qrdbtransport.CreateNamespaceRequest) (*qrdbtransport.CreateNamespaceResponse, error) {
	if m.createNamespaceErr != nil {
		return nil, m.createNamespaceErr
	}
	if _, exists := m.namespaces[req.Namespace]; exists {
		return nil, errors.New("vector namespace already exists: " + req.Namespace)
	}
	m.namespaces[req.Namespace] = &mockNamespace{
		dimension: req.Dimension,
		vectors:   make(map[uint64]*qrdbtransport.VectorData),
	}
	return &qrdbtransport.CreateNamespaceResponse{
		Namespace: req.Namespace,
		Dimension: req.Dimension,
		Timestamp: time.Now().UnixNano(),
		Success:   true,
	}, nil
}

func (m *mockVectorClient) DeleteNamespace(_ context.Context, req *qrdbtransport.DeleteNamespaceRequest) (*qrdbtransport.DeleteNamespaceResponse, error) {
	deleted := uint64(0)
	if ns, ok := m.namespaces[req.Namespace]; ok {
		deleted = uint64(len(ns.vectors))
	}
	delete(m.namespaces, req.Namespace)
	return &qrdbtransport.DeleteNamespaceResponse{
		Success:        true,
		VectorsDeleted: deleted,
	}, nil
}

func (m *mockVectorClient) ListNamespaces(_ context.Context, _ *qrdbtransport.ListNamespacesRequest) (*qrdbtransport.ListNamespacesResponse, error) {
	infos := make([]*qrdbtransport.NamespaceInfo, 0, len(m.namespaces))
	for ns, n := range m.namespaces {
		infos = append(infos, &qrdbtransport.NamespaceInfo{
			Namespace:   ns,
			Dimension:   n.dimension,
			VectorCount: uint64(len(n.vectors)),
		})
	}
	return &qrdbtransport.ListNamespacesResponse{
		Namespaces: infos,
		TotalCount: len(infos),
	}, nil
}

func (m *mockVectorClient) GetNamespaceInfo(_ context.Context, req *qrdbtransport.GetNamespaceInfoRequest) (*qrdbtransport.GetNamespaceInfoResponse, error) {
	if m.nsInfoErr != nil {
		return nil, m.nsInfoErr
	}
	ns, ok := m.namespaces[req.Namespace]
	if !ok {
		return nil, errors.New("namespace not found: " + req.Namespace)
	}
	return &qrdbtransport.GetNamespaceInfoResponse{
		Namespace:   req.Namespace,
		Dimension:   ns.dimension,
		VectorCount: uint64(len(ns.vectors)),
	}, nil
}

func (m *mockVectorClient) UpsertVector(_ context.Context, req *qrdbtransport.UpsertVectorRequest) (*qrdbtransport.UpsertVectorResponse, error) {
	if m.upsertErr != nil {
		return nil, m.upsertErr
	}
	ns := m.getOrCreateNS(req.Namespace, uint32(len(req.Vector)))
	ns.vectors[req.ID] = &qrdbtransport.VectorData{
		ID:       req.ID,
		Vector:   req.Vector,
		Metadata: req.Metadata,
	}
	return &qrdbtransport.UpsertVectorResponse{ID: req.ID, Success: true}, nil
}

func (m *mockVectorClient) GetVector(_ context.Context, req *qrdbtransport.GetVectorRequest) (*qrdbtransport.GetVectorResponse, error) {
	ns, ok := m.namespaces[req.Namespace]
	if !ok {
		return &qrdbtransport.GetVectorResponse{Found: false}, nil
	}
	v, ok := ns.vectors[req.ID]
	if !ok {
		return &qrdbtransport.GetVectorResponse{Found: false}, nil
	}
	return &qrdbtransport.GetVectorResponse{
		ID:       v.ID,
		Vector:   v.Vector,
		Metadata: v.Metadata,
		Found:    true,
	}, nil
}

func (m *mockVectorClient) DeleteVector(_ context.Context, req *qrdbtransport.DeleteVectorRequest) (*qrdbtransport.DeleteVectorResponse, error) {
	if m.deleteErr != nil {
		return nil, m.deleteErr
	}
	if ns, ok := m.namespaces[req.Namespace]; ok {
		delete(ns.vectors, req.ID)
	}
	return &qrdbtransport.DeleteVectorResponse{Success: true}, nil
}

func (m *mockVectorClient) BatchUpsert(_ context.Context, req *qrdbtransport.BatchUpsertRequest) (*qrdbtransport.BatchUpsertResponse, error) {
	ns := m.getOrCreateNS(req.Namespace, 0)
	ids := make([]uint64, 0, len(req.Vectors))
	for _, v := range req.Vectors {
		ns.vectors[v.ID] = v
		ids = append(ids, v.ID)
	}
	return &qrdbtransport.BatchUpsertResponse{IDs: ids, Success: true, InsertedCount: len(req.Vectors)}, nil
}

func (m *mockVectorClient) BatchDeleteVectors(_ context.Context, req *qrdbtransport.BatchDeleteVectorsRequest) (*qrdbtransport.BatchDeleteVectorsResponse, error) {
	if ns, ok := m.namespaces[req.Namespace]; ok {
		for _, id := range req.IDs {
			delete(ns.vectors, id)
		}
	}
	return &qrdbtransport.BatchDeleteVectorsResponse{Success: true, DeletedCount: len(req.IDs)}, nil
}

func (m *mockVectorClient) ListVectors(_ context.Context, req *qrdbtransport.ListVectorsRequest) (*qrdbtransport.ListVectorsResponse, error) {
	ns, ok := m.namespaces[req.Namespace]
	if !ok {
		return &qrdbtransport.ListVectorsResponse{}, nil
	}
	vecs := make([]*qrdbtransport.VectorData, 0, len(ns.vectors))
	for _, v := range ns.vectors {
		vecs = append(vecs, v)
	}
	return &qrdbtransport.ListVectorsResponse{Vectors: vecs, TotalCount: uint64(len(vecs))}, nil
}

func (m *mockVectorClient) UpdateMetadata(_ context.Context, req *qrdbtransport.UpdateVectorMetadataRequest) (*qrdbtransport.UpdateVectorMetadataResponse, error) {
	return &qrdbtransport.UpdateVectorMetadataResponse{ID: req.ID, Success: true}, nil
}

func (m *mockVectorClient) SearchSimilar(_ context.Context, req *qrdbtransport.SearchSimilarRequest) (*qrdbtransport.SearchSimilarResponse, error) {
	if m.searchErr != nil {
		return nil, m.searchErr
	}
	ns, ok := m.namespaces[req.Namespace]
	if !ok {
		return &qrdbtransport.SearchSimilarResponse{}, nil
	}
	k := req.K
	results := make([]*qrdbtransport.VectorSearchResult, 0)
	rank := 0
	for id, v := range ns.vectors {
		if k > 0 && len(results) >= k {
			break
		}
		rank++
		results = append(results, &qrdbtransport.VectorSearchResult{
			ID:       id,
			Score:    0.9,
			Vector:   v.Vector,
			Metadata: v.Metadata,
			Rank:     rank,
		})
	}
	return &qrdbtransport.SearchSimilarResponse{Results: results}, nil
}

func (m *mockVectorClient) SearchByMetadata(_ context.Context, _ *qrdbtransport.SearchByMetadataRequest) (*qrdbtransport.SearchByMetadataResponse, error) {
	return &qrdbtransport.SearchByMetadataResponse{}, nil
}

func (m *mockVectorClient) HybridSearch(_ context.Context, _ *qrdbtransport.HybridSearchRequest) (*qrdbtransport.HybridSearchResponse, error) {
	return &qrdbtransport.HybridSearchResponse{}, nil
}

func (m *mockVectorClient) SearchByID(_ context.Context, _ *qrdbtransport.SearchByIDRequest) (*qrdbtransport.SearchByIDResponse, error) {
	return &qrdbtransport.SearchByIDResponse{}, nil
}

func (m *mockVectorClient) RebuildIndex(_ context.Context, _ *qrdbtransport.RebuildIndexRequest) (*qrdbtransport.RebuildIndexResponse, error) {
	return &qrdbtransport.RebuildIndexResponse{Success: true}, nil
}

func (m *mockVectorClient) GetRebuildProgress(_ context.Context, _ *qrdbtransport.GetRebuildProgressRequest) (*qrdbtransport.GetRebuildProgressResponse, error) {
	return &qrdbtransport.GetRebuildProgressResponse{Status: "completed", PercentComplete: 100}, nil
}

func (m *mockVectorClient) GetIndexStats(_ context.Context, req *qrdbtransport.GetIndexStatsRequest) (*qrdbtransport.GetIndexStatsResponse, error) {
	ns, ok := m.namespaces[req.Namespace]
	if !ok {
		return &qrdbtransport.GetIndexStatsResponse{}, nil
	}
	return &qrdbtransport.GetIndexStatsResponse{
		Namespace:   req.Namespace,
		VectorCount: uint64(len(ns.vectors)),
	}, nil
}

func (m *mockVectorClient) CompactNamespace(_ context.Context, _ *qrdbtransport.CompactNamespaceRequest) (*qrdbtransport.CompactNamespaceResponse, error) {
	return &qrdbtransport.CompactNamespaceResponse{Success: true}, nil
}

func (m *mockVectorClient) GetCompactionProgress(_ context.Context, _ *qrdbtransport.GetCompactionProgressRequest) (*qrdbtransport.GetCompactionProgressResponse, error) {
	return &qrdbtransport.GetCompactionProgressResponse{PercentComplete: 100}, nil
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func newTestQRDBIndex(t *testing.T, dimensions int) (*qrdbVectorIndex, *mockVectorClient) {
	t.Helper()
	client := newMockVectorClient()
	cfg := qrdbVectorIndexConfig{
		Client:    client,
		Namespace: "test-ns",
		Dimension: dimensions,
	}
	idx, err := newQRDBVectorIndex(context.Background(), cfg)
	if err != nil {
		t.Fatalf("newQRDBVectorIndex failed: %v", err)
	}
	return idx.(*qrdbVectorIndex), client
}

func makeVec(dims int, val float32) []float32 {
	v := make([]float32, dims)
	for i := range v {
		v[i] = val
	}
	return v
}

// ---------------------------------------------------------------------------
// Construction tests
// ---------------------------------------------------------------------------

func TestNewQRDBVectorIndex(t *testing.T) {
	tests := []struct {
		name      string
		nilClient bool
		namespace string
		dimension int
		// existing pre-creates the namespace so the constructor must treat
		// "already exists" as success.
		existing  bool
		createErr error
		wantErr   any
	}{
		{name: "success", namespace: "ns", dimension: 64},
		{name: "namespace already exists", namespace: "ns", dimension: 32, existing: true},
		{name: "nil client", nilClient: true, namespace: "ns", dimension: 16, wantErr: new(*QRDBVectorClientNilError)},
		{name: "empty namespace", namespace: "", dimension: 16, wantErr: new(*QRDBVectorNamespaceEmptyError)},
		{name: "zero dimension", namespace: "ns", dimension: 0, wantErr: new(*rag.VectorIndexConfigError)},
		{name: "create namespace fails", namespace: "ns", dimension: 32, createErr: errors.New("storage full"), wantErr: new(*QRDBVectorCreateNamespaceError)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := qrdbVectorIndexConfig{Namespace: tt.namespace, Dimension: tt.dimension}
			if !tt.nilClient {
				client := newMockVectorClient()
				client.createNamespaceErr = tt.createErr
				cfg.Client = client
			}
			if tt.existing {
				if _, err := newQRDBVectorIndex(context.Background(), cfg); err != nil {
					t.Fatalf("first creation should succeed: %v", err)
				}
			}
			idx, err := newQRDBVectorIndex(context.Background(), cfg)
			if tt.wantErr != nil {
				if !errors.As(err, tt.wantErr) {
					t.Fatalf("expected %T, got %T (%v)", tt.wantErr, err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !idx.IsTrained() {
				t.Fatal("newly created index should be trained")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tenant isolation tests
// ---------------------------------------------------------------------------

func TestPhysicalNamespace_WithTenant(t *testing.T) {
	got := physicalNamespace("acme", "embeddings")
	want := "acme:embeddings"
	if got != want {
		t.Fatalf("physicalNamespace(%q, %q) = %q, want %q", "acme", "embeddings", got, want)
	}
}

func TestPhysicalNamespace_NoTenant(t *testing.T) {
	got := physicalNamespace("", "embeddings")
	if got != "embeddings" {
		t.Fatalf("expected %q, got %q", "embeddings", got)
	}
}

func TestQRDBVectorIndex_TenantIsolation(t *testing.T) {
	client := newMockVectorClient()
	dims := 4

	// Create index for tenant "alice".
	aliceCfg := qrdbVectorIndexConfig{Client: client, Namespace: "rag", TenantID: "alice", Dimension: dims}
	aliceIdx, err := newQRDBVectorIndex(context.Background(), aliceCfg)
	if err != nil {
		t.Fatalf("alice index creation failed: %v", err)
	}

	// Create index for tenant "bob".
	bobCfg := qrdbVectorIndexConfig{Client: client, Namespace: "rag", TenantID: "bob", Dimension: dims}
	bobIdx, err := newQRDBVectorIndex(context.Background(), bobCfg)
	if err != nil {
		t.Fatalf("bob index creation failed: %v", err)
	}

	// Add vector to alice's index.
	if err := aliceIdx.Add("v1", makeVec(dims, 0.1), nil); err != nil {
		t.Fatalf("alice Add failed: %v", err)
	}

	// Bob's namespace must be empty.
	if bobIdx.Size() != 0 {
		t.Fatalf("bob's index should be empty, got size %d", bobIdx.Size())
	}

	// Alice's namespace must contain 1 vector.
	if aliceIdx.Size() != 1 {
		t.Fatalf("alice's index should have size 1, got %d", aliceIdx.Size())
	}
}

// ---------------------------------------------------------------------------
// Train / IsTrained
// ---------------------------------------------------------------------------

func TestQRDBVectorIndex_Train_IsNoOp(t *testing.T) {
	idx, _ := newTestQRDBIndex(t, 16)
	if !idx.IsTrained() {
		t.Fatal("should be trained from construction")
	}
	// Train should always succeed.
	if err := idx.Train([][]float32{{1, 2, 3, 4}}); err != nil {
		t.Fatalf("Train failed: %v", err)
	}
	if !idx.IsTrained() {
		t.Fatal("should remain trained after Train()")
	}
}

// ---------------------------------------------------------------------------
// Add / Size / Remove
// ---------------------------------------------------------------------------

func TestQRDBVectorIndex_AddAndSize(t *testing.T) {
	idx, _ := newTestQRDBIndex(t, 8)

	if idx.Size() != 0 {
		t.Fatalf("empty index should have size 0, got %d", idx.Size())
	}

	vec := makeVec(8, 0.5)
	if err := idx.Add("chunk-1", vec, map[string]interface{}{"lang": "go"}); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if idx.Size() != 1 {
		t.Fatalf("expected size 1, got %d", idx.Size())
	}
}

func TestQRDBVectorIndex_Add_DimensionMismatch(t *testing.T) {
	idx, _ := newTestQRDBIndex(t, 8)
	err := idx.Add("v1", makeVec(4, 0.5), nil)
	if err == nil {
		t.Fatal("expected dimension mismatch error")
	}
	var dimErr *rag.VectorDimensionError
	if !errors.As(err, &dimErr) {
		t.Fatalf("expected VectorDimensionError, got %T", err)
	}
}

func TestQRDBVectorIndex_Add_UpsertError(t *testing.T) {
	dims := 4
	client := newMockVectorClient()
	client.upsertErr = errors.New("connection refused")
	cfg := qrdbVectorIndexConfig{Client: client, Namespace: "ns", Dimension: dims}
	idx, err := newQRDBVectorIndex(context.Background(), cfg)
	if err != nil {
		t.Fatalf("construction failed: %v", err)
	}
	err = idx.Add("v1", makeVec(dims, 0.1), nil)
	if err == nil {
		t.Fatal("expected upsert error")
	}
	var upsertErr *QRDBVectorUpsertError
	if !errors.As(err, &upsertErr) {
		t.Fatalf("expected QRDBVectorUpsertError, got %T", err)
	}
}

func TestQRDBVectorIndex_Remove_Success(t *testing.T) {
	idx, _ := newTestQRDBIndex(t, 8)

	if err := idx.Add("v1", makeVec(8, 0.3), nil); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if idx.Size() != 1 {
		t.Fatalf("expected size 1 after Add")
	}

	if err := idx.Remove("v1"); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if idx.Size() != 0 {
		t.Fatalf("expected size 0 after Remove, got %d", idx.Size())
	}
}

func TestQRDBVectorIndex_Remove_Error(t *testing.T) {
	dims := 4
	client := newMockVectorClient()
	cfg := qrdbVectorIndexConfig{Client: client, Namespace: "ns", Dimension: dims}
	idx, err := newQRDBVectorIndex(context.Background(), cfg)
	if err != nil {
		t.Fatalf("construction failed: %v", err)
	}
	client.deleteErr = errors.New("disk full")
	err = idx.Remove("v1")
	if err == nil {
		t.Fatal("expected delete error")
	}
	var delErr *QRDBVectorDeleteError
	if !errors.As(err, &delErr) {
		t.Fatalf("expected QRDBVectorDeleteError, got %T", err)
	}
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

func TestQRDBVectorIndex_Search_Success(t *testing.T) {
	idx, _ := newTestQRDBIndex(t, 4)

	vecs := []struct {
		id  string
		vec []float32
	}{
		{"chunk-a", makeVec(4, 0.1)},
		{"chunk-b", makeVec(4, 0.5)},
		{"chunk-c", makeVec(4, 0.9)},
	}
	for _, v := range vecs {
		if err := idx.Add(v.id, v.vec, map[string]interface{}{"rag_id": v.id}); err != nil {
			t.Fatalf("Add %s failed: %v", v.id, err)
		}
	}

	results, err := idx.Search(makeVec(4, 0.5), 2, 0)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	// Verify results have IDs.
	for _, r := range results {
		if r.ID == "" {
			t.Fatal("result ID must not be empty")
		}
	}
}

func TestQRDBVectorIndex_Search_DimensionMismatch(t *testing.T) {
	idx, _ := newTestQRDBIndex(t, 8)
	_, err := idx.Search(makeVec(4, 0.5), 5, 0)
	if err == nil {
		t.Fatal("expected dimension mismatch error")
	}
	var dimErr *rag.VectorDimensionError
	if !errors.As(err, &dimErr) {
		t.Fatalf("expected VectorDimensionError, got %T", err)
	}
}

func TestQRDBVectorIndex_Search_Error(t *testing.T) {
	dims := 4
	client := newMockVectorClient()
	cfg := qrdbVectorIndexConfig{Client: client, Namespace: "ns", Dimension: dims}
	idx, err := newQRDBVectorIndex(context.Background(), cfg)
	if err != nil {
		t.Fatalf("construction failed: %v", err)
	}
	client.searchErr = errors.New("index unavailable")
	_, err = idx.Search(makeVec(dims, 0.5), 5, 0)
	if err == nil {
		t.Fatal("expected search error")
	}
	var searchErr *QRDBVectorSearchError
	if !errors.As(err, &searchErr) {
		t.Fatalf("expected QRDBVectorSearchError, got %T", err)
	}
}

func TestQRDBVectorIndex_Search_EmptyNamespace(t *testing.T) {
	idx, _ := newTestQRDBIndex(t, 4)
	results, err := idx.Search(makeVec(4, 0.5), 5, 0)
	if err != nil {
		t.Fatalf("Search on empty namespace failed: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// Save / Load (no-ops)
// ---------------------------------------------------------------------------

func TestQRDBVectorIndex_Save_IsNoOp(t *testing.T) {
	idx, _ := newTestQRDBIndex(t, 8)
	if err := idx.Save("/some/path"); err != nil {
		t.Fatalf("Save should be a no-op, got error: %v", err)
	}
}

func TestQRDBVectorIndex_Load_IsNoOp(t *testing.T) {
	idx, _ := newTestQRDBIndex(t, 8)
	if err := idx.Load("/some/path"); err != nil {
		t.Fatalf("Load should be a no-op, got error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Size with error from GetNamespaceInfo
// ---------------------------------------------------------------------------

func TestQRDBVectorIndex_Size_ReturnsZeroOnError(t *testing.T) {
	dims := 4
	client := newMockVectorClient()
	cfg := qrdbVectorIndexConfig{Client: client, Namespace: "ns", Dimension: dims}
	idx, err := newQRDBVectorIndex(context.Background(), cfg)
	if err != nil {
		t.Fatalf("construction failed: %v", err)
	}
	client.nsInfoErr = errors.New("service unavailable")
	size := idx.Size()
	if size != 0 {
		t.Fatalf("Size should return 0 on error, got %d", size)
	}
}

// ---------------------------------------------------------------------------
// stringToUint64 stability
// ---------------------------------------------------------------------------

func TestStringToUint64_Stability(t *testing.T) {
	// Same input always produces the same output.
	id := "rag-ns:abc123:0"
	first := stringToUint64(id)
	second := stringToUint64(id)
	if first != second {
		t.Fatalf("stringToUint64 should be deterministic: %d != %d", first, second)
	}
}

// TestStringToUint64_PinnedValue pins the FNV-1a 64 id of a known vector ID.
// The id is the persisted vector key, so the algorithm must never change.
func TestStringToUint64_PinnedValue(t *testing.T) {
	const want uint64 = 0x8a6663c975f34c65
	if got := stringToUint64("rag-ns:abc123:0"); got != want {
		t.Fatalf("stringToUint64 = %#x, want %#x", got, want)
	}
}

func TestStringToUint64_Collision(t *testing.T) {
	// Distinct inputs should produce distinct outputs (extremely high probability).
	a := stringToUint64("rag-ns:abc:0")
	b := stringToUint64("rag-ns:abc:1")
	if a == b {
		t.Fatal("stringToUint64 collision between distinct inputs — check hash function")
	}
}

// ---------------------------------------------------------------------------
// ragIDFromMetadata
// ---------------------------------------------------------------------------

func TestRagIDFromMetadata_WithRagID(t *testing.T) {
	meta := map[string]interface{}{"rag_id": "my-chunk-id"}
	got := ragIDFromMetadata(meta, 42)
	if got != "my-chunk-id" {
		t.Fatalf("expected 'my-chunk-id', got %q", got)
	}
}

func TestRagIDFromMetadata_FallbackToNumericID(t *testing.T) {
	got := ragIDFromMetadata(nil, 99)
	if got == "" {
		t.Fatal("fallback ID must not be empty")
	}
	// Fallback starts with "qrdb:" prefix.
	if len(got) < 5 || got[:5] != "qrdb:" {
		t.Fatalf("fallback ID should start with 'qrdb:', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// NewVectorIndexFromConfig with qrdb type
// ---------------------------------------------------------------------------

func TestNewVectorIndexFromConfig_QRDB(t *testing.T) {
	tests := []struct {
		name       string
		indexType  string
		withClient bool
		tenantID   string
		wantQRDB   bool
		wantErr    any
	}{
		{name: "qrdb with client", indexType: "qrdb", withClient: true, tenantID: "acme", wantQRDB: true},
		{name: "qrdb without client", indexType: "qrdb", wantErr: new(*QRDBVectorClientNilError)},
		{name: "auto without client falls back to memory", indexType: "auto"},
		{name: "auto with client uses qrdb", indexType: "auto", withClient: true, wantQRDB: true},
		{name: "hnsw with client uses qrdb", indexType: "hnsw", withClient: true, wantQRDB: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []VectorIndexOption
			if tt.withClient {
				opts = append(opts, WithVectorClient(newMockVectorClient()))
			}
			if tt.tenantID != "" {
				opts = append(opts, WithTenantID(tt.tenantID))
			}
			cfg := IndexingConfig{IndexType: tt.indexType, Namespace: "rag", Enabled: true}
			idx, err := NewVectorIndexFromConfig(cfg, 16, opts...)
			if tt.wantErr != nil {
				if !errors.As(err, tt.wantErr) {
					t.Fatalf("expected %T, got %T: %v", tt.wantErr, err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if idx == nil {
				t.Fatal("expected non-nil index")
			}
			if _, isQRDB := idx.(*qrdbVectorIndex); isQRDB != tt.wantQRDB {
				t.Fatalf("qrdb-backed = %v, want %v (got %T)", isQRDB, tt.wantQRDB, idx)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Error type messages
// ---------------------------------------------------------------------------

func TestQRDBVectorErrors(t *testing.T) {
	cause := errors.New("cause")
	tests := []struct {
		name string
		err  error
		// cause is nil for errors that wrap nothing.
		cause error
	}{
		{name: "client nil", err: &QRDBVectorClientNilError{}},
		{name: "namespace empty", err: &QRDBVectorNamespaceEmptyError{}},
		{name: "create namespace", err: &QRDBVectorCreateNamespaceError{Namespace: "ns", Cause: cause}, cause: cause},
		{name: "upsert", err: &QRDBVectorUpsertError{ID: "v1", Cause: cause}, cause: cause},
		{name: "search", err: &QRDBVectorSearchError{Cause: cause}, cause: cause},
		{name: "delete", err: &QRDBVectorDeleteError{ID: "v1", Cause: cause}, cause: cause},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() == "" {
				t.Fatal("error message must not be empty")
			}
			if tt.cause != nil && !errors.Is(tt.err, tt.cause) {
				t.Fatal("Unwrap should return cause")
			}
		})
	}
}
