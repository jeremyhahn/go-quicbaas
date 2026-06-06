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

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag"
)

// VectorIndexOption configures VectorIndex creation with optional dependencies.
type VectorIndexOption func(*vectorIndexOptions)

// vectorIndexOptions holds optional parameters for VectorIndex construction.
type vectorIndexOptions struct {
	vectorClient   qrdbtransport.VectorClient
	tenantID       string
	distanceMetric string
}

// WithVectorClient injects a qrdb VectorClient so that "qrdb" and "auto" index
// types can delegate to the persistent, tenant-safe HNSW vector primitive.
func WithVectorClient(client qrdbtransport.VectorClient) VectorIndexOption {
	return func(o *vectorIndexOptions) {
		o.vectorClient = client
	}
}

// WithTenantID sets the tenant ID used to prefix the physical namespace in qrdb,
// providing per-tenant data isolation. When empty the logical namespace is used
// as-is (system/single-tenant operation).
func WithTenantID(tenantID string) VectorIndexOption {
	return func(o *vectorIndexOptions) {
		o.tenantID = tenantID
	}
}

// WithDistanceMetric overrides the distance metric for the qrdb index.
// Accepted values: "cosine", "euclidean", "dot_product". Defaults to "cosine".
func WithDistanceMetric(metric string) VectorIndexOption {
	return func(o *vectorIndexOptions) {
		o.distanceMetric = metric
	}
}

// indexTypeHandlers maps index type names to their factory functions for O(1) dispatch.
// Each handler receives the config, dimensions, a context, and the resolved options.
var indexTypeHandlers = map[string]func(ctx context.Context, cfg IndexingConfig, dimensions int, opts *vectorIndexOptions) (rag.VectorIndex, error){
	"auto":   newAutoIndex,
	"memory": newMemoryIndex,
	"hnsw":   newHNSWOrQRDBIndex,
	"qrdb":   newQRDBIndex,
}

// NewVectorIndexFromConfig creates a VectorIndex based on the provided configuration.
// It selects the appropriate backend ("auto", "memory", "hnsw", or "qrdb") and
// returns a configured index with the given dimensionality.
//
// When opts include a WithVectorClient, "auto" and "hnsw" resolve to the qrdb-backed
// HNSW index for persistent, barrier-encrypted storage. Without a VectorClient they
// fall back to the in-memory brute-force implementation.
func NewVectorIndexFromConfig(cfg IndexingConfig, dimensions int, opts ...VectorIndexOption) (rag.VectorIndex, error) {
	if cfg.GPUAccelerated {
		return nil, &GPUUnavailableError{}
	}

	o := &vectorIndexOptions{}
	for _, opt := range opts {
		opt(o)
	}

	handler, ok := indexTypeHandlers[cfg.IndexType]
	if !ok {
		return nil, &UnsupportedIndexTypeError{IndexType: cfg.IndexType}
	}
	return handler(context.Background(), cfg, dimensions, o)
}

// newAutoIndex selects the qrdb HNSW index when a VectorClient is available;
// otherwise falls back to the in-memory brute-force index.
func newAutoIndex(ctx context.Context, cfg IndexingConfig, dimensions int, opts *vectorIndexOptions) (rag.VectorIndex, error) {
	if opts.vectorClient != nil {
		return newQRDBIndex(ctx, cfg, dimensions, opts)
	}
	return newMemoryIndex(ctx, cfg, dimensions, opts)
}

// newMemoryIndex creates an in-memory brute-force vector index.
// This backend is retained as an explicit dev/test option (index_type: "memory").
func newMemoryIndex(_ context.Context, _ IndexingConfig, dimensions int, _ *vectorIndexOptions) (rag.VectorIndex, error) {
	return rag.NewVectorIndex(&rag.VectorIndexConfig{
		Dimensions:     dimensions,
		DistanceMetric: rag.CosineDistance,
	})
}

// newHNSWOrQRDBIndex creates a qrdb-backed HNSW index when a VectorClient is
// available, otherwise falls back to the in-memory brute-force index.
func newHNSWOrQRDBIndex(ctx context.Context, cfg IndexingConfig, dimensions int, opts *vectorIndexOptions) (rag.VectorIndex, error) {
	if opts.vectorClient != nil {
		return newQRDBIndex(ctx, cfg, dimensions, opts)
	}
	return newMemoryIndex(ctx, cfg, dimensions, opts)
}

// newQRDBIndex creates a persistent qrdb-backed HNSW vector index.
// It requires a VectorClient to have been provided via WithVectorClient.
func newQRDBIndex(ctx context.Context, cfg IndexingConfig, dimensions int, opts *vectorIndexOptions) (rag.VectorIndex, error) {
	if opts.vectorClient == nil {
		return nil, &QRDBVectorClientNilError{}
	}
	idxCfg := qrdbVectorIndexConfig{
		Client:         opts.vectorClient,
		Namespace:      cfg.Namespace,
		TenantID:       opts.tenantID,
		Dimension:      dimensions,
		DistanceMetric: opts.distanceMetric,
	}
	return newQRDBVectorIndex(ctx, idxCfg)
}
