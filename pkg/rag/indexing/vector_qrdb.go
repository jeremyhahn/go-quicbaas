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
	"encoding/binary"
	"hash/fnv"
	"sync/atomic"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag"
)

// qrdbVectorIndex implements rag.VectorIndex backed by the qrdb SDK
// VectorClient. Each instance is bound to a single physical namespace and
// an optional tenant prefix, so all operations are scoped to the correct
// tenant at construction time.
//
// Tenant isolation is achieved by embedding the tenant ID in the namespace
// name at construction: physicalNamespace = tenantID + ":" + logicalNamespace
// (or just logicalNamespace when tenantID is empty). This matches the
// quicbaas convention used by baasTransport's namespace-key-based isolation
// and avoids any reliance on the qrdb context-key-based tenant derivation
// (which uses a different context key than the quicbaas iam package).
//
// The qrdb VectorClient uses uint64 vector IDs. RAG pipeline vector IDs are
// arbitrary strings (e.g. "ns:abc123:0"). stringToUint64 converts a RAG ID
// to a stable uint64 via FNV-1a 64-bit hashing.
type qrdbVectorIndex struct {
	client    qrdbtransport.VectorClient
	namespace string
	dimension int
	trained   atomic.Bool
}

// qrdbVectorIndexConfig holds the construction parameters for a qrdbVectorIndex.
type qrdbVectorIndexConfig struct {
	// Client is the qrdb SDK VectorClient (required).
	Client qrdbtransport.VectorClient

	// Namespace is the logical namespace for this index (required).
	Namespace string

	// TenantID is the owning tenant. When non-empty the physical namespace
	// is prefixed as "{tenantID}:{namespace}". When empty the namespace is
	// used as-is (single-tenant / system-tenant operation).
	TenantID string

	// Dimension is the vector dimensionality (required, must be > 0).
	Dimension int

	// DistanceMetric is the distance metric ("cosine", "euclidean",
	// "dot_product"). Defaults to "cosine" when empty.
	DistanceMetric string
}

// Compile-time interface assertion.
var _ rag.VectorIndex = (*qrdbVectorIndex)(nil)

// newQRDBVectorIndex constructs and initialises a qrdbVectorIndex.
// It creates the underlying namespace in qrdb if it does not already exist
// (namespace already-exists is not treated as an error to support restarts).
func newQRDBVectorIndex(ctx context.Context, cfg qrdbVectorIndexConfig) (rag.VectorIndex, error) {
	if cfg.Client == nil {
		return nil, &QRDBVectorClientNilError{}
	}
	if cfg.Namespace == "" {
		return nil, &QRDBVectorNamespaceEmptyError{}
	}
	if cfg.Dimension <= 0 {
		return nil, &rag.VectorIndexConfigError{Field: "Dimensions", Reason: "must be positive"}
	}

	physical := physicalNamespace(cfg.TenantID, cfg.Namespace)
	metric := cfg.DistanceMetric
	if metric == "" {
		metric = qrdbtransport.DistanceCosine
	}

	req := &qrdbtransport.CreateNamespaceRequest{
		Namespace:      physical,
		Dimension:      uint32(cfg.Dimension),
		DistanceMetric: metric,
		IndexType:      qrdbtransport.IndexHNSW,
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if _, err := cfg.Client.CreateNamespace(ctx, req); err != nil {
		if !isAlreadyExists(err) {
			return nil, &QRDBVectorCreateNamespaceError{Namespace: physical, Cause: err}
		}
	}

	idx := &qrdbVectorIndex{
		client:    cfg.Client,
		namespace: physical,
		dimension: cfg.Dimension,
	}
	idx.trained.Store(true)
	return idx, nil
}

// physicalNamespace builds the storage-level namespace name. When tenantID is
// empty the logical name is returned unmodified.
func physicalNamespace(tenantID, logical string) string {
	if tenantID == "" {
		return logical
	}
	return tenantID + ":" + logical
}

// stringToUint64 converts an arbitrary RAG vector ID string to a stable
// uint64 using FNV-1a 64-bit hashing.
func stringToUint64(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// uint64ToBytes serialises a uint64 to an 8-byte big-endian slice for
// storage as vector metadata so the original string ID can be recovered.
func uint64ToBytes(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// isAlreadyExists returns true for qrdb "already exists" error responses.
// The baasTransport returns *VectorNamespaceExistsError; for network
// transports the error message carries an "already exists" phrase.
func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return containsAny(msg, "already exists", "AlreadyExists")
}

// containsAny returns true when s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// rag.VectorIndex implementation
// ---------------------------------------------------------------------------

// Train is a no-op for the qrdb index: the HNSW graph builds lazily as
// vectors are inserted. IsTrained returns true immediately after construction.
func (q *qrdbVectorIndex) Train(_ [][]float32) error {
	q.trained.Store(true)
	return nil
}

// IsTrained returns true as soon as the namespace has been created.
func (q *qrdbVectorIndex) IsTrained() bool {
	return q.trained.Load()
}

// Add inserts or updates a vector in the qrdb namespace. The string id is
// hashed to a uint64 and stored as metadata under the key "rag_id" so it
// can be correlated with search results.
func (q *qrdbVectorIndex) Add(id string, vector []float32, metadata map[string]interface{}) error {
	if len(vector) != q.dimension {
		return &rag.VectorDimensionError{Expected: q.dimension, Got: len(vector)}
	}

	numericID := stringToUint64(id)
	if metadata == nil {
		metadata = make(map[string]interface{}, 1)
	}
	metadata["rag_id"] = id

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &qrdbtransport.UpsertVectorRequest{
		Namespace: q.namespace,
		ID:        numericID,
		Vector:    vector,
		Metadata:  metadata,
	}
	if _, err := q.client.UpsertVector(ctx, req); err != nil {
		return &QRDBVectorUpsertError{ID: id, Cause: err}
	}
	return nil
}

// Search performs a k-nearest-neighbour search against the qrdb namespace.
// The nprobe parameter maps to EfSearch for HNSW index traversal.
func (q *qrdbVectorIndex) Search(query []float32, k int, nprobe int) ([]rag.VectorSearchResult, error) {
	if len(query) != q.dimension {
		return nil, &rag.VectorDimensionError{Expected: q.dimension, Got: len(query)}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &qrdbtransport.SearchSimilarRequest{
		Namespace:       q.namespace,
		QueryVector:     query,
		K:               k,
		EfSearch:        nprobe,
		IncludeMetadata: true,
	}

	resp, err := q.client.SearchSimilar(ctx, req)
	if err != nil {
		return nil, &QRDBVectorSearchError{Cause: err}
	}

	results := make([]rag.VectorSearchResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		ragID := ragIDFromMetadata(r.Metadata, r.ID)
		results = append(results, rag.VectorSearchResult{
			ID:    ragID,
			Score: r.Score,
		})
	}
	return results, nil
}

// ragIDFromMetadata recovers the original RAG string ID from the vector
// metadata "rag_id" field. Falls back to stringifying the numeric ID when
// the metadata field is absent (e.g., vectors inserted by other callers).
func ragIDFromMetadata(meta map[string]interface{}, numericID uint64) string {
	if meta != nil {
		if v, ok := meta["rag_id"]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	// Fallback: hex encode the numeric ID.
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, numericID)
	return "qrdb:" + string(hexEncode(b))
}

// hexEncode converts bytes to a lowercase hex string without importing fmt.
func hexEncode(b []byte) []byte {
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = digits[v>>4]
		out[i*2+1] = digits[v&0xf]
	}
	return out
}

// Remove deletes a vector from the qrdb namespace by its RAG string ID.
func (q *qrdbVectorIndex) Remove(id string) error {
	numericID := stringToUint64(id)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &qrdbtransport.DeleteVectorRequest{
		Namespace: q.namespace,
		ID:        numericID,
	}
	if _, err := q.client.DeleteVector(ctx, req); err != nil {
		return &QRDBVectorDeleteError{ID: id, Cause: err}
	}
	return nil
}

// Save is a no-op: qrdb persists vectors automatically to PebbleDB-backed
// storage. The path parameter is accepted for interface compliance but unused.
func (q *qrdbVectorIndex) Save(_ string) error {
	return nil
}

// Load is a no-op: qrdb reloads the namespace on restart from durable storage.
// The path parameter is accepted for interface compliance but unused.
func (q *qrdbVectorIndex) Load(_ string) error {
	return nil
}

// Size returns the number of vectors currently stored in the qrdb namespace
// by requesting the namespace info from the server.
func (q *qrdbVectorIndex) Size() int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := q.client.GetNamespaceInfo(ctx, &qrdbtransport.GetNamespaceInfoRequest{
		Namespace: q.namespace,
	})
	if err != nil {
		return 0
	}
	return int(resp.VectorCount)
}
