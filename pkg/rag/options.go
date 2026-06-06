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

package rag

import "github.com/jeremyhahn/go-quicbaas/pkg/config"

// SystemOption configures a RAG System during construction.
type SystemOption func(*systemOptions)

// systemOptions holds optional overrides for NewSystem.
type systemOptions struct {
	// vectorIndexOverride, when non-nil, replaces the package-level
	// createVectorIndexFunc for this specific NewSystem call. The server
	// uses this to inject a qrdb-backed HNSW index without creating an
	// import cycle between pkg/rag and pkg/rag/indexing.
	vectorIndexOverride vectorIndexFactory
}

// WithVectorIndexFactory replaces the vector index factory for this RAG System
// instance. The factory receives the RAGVectorConfig and must return a
// rag.VectorIndex. The caller is responsible for wiring any external
// dependencies (e.g. a qrdb VectorClient) into the factory closure.
//
// Example:
//
//	rag.NewSystem(cfg,
//	    rag.WithVectorIndexFactory(func(vcfg config.RAGVectorConfig) (rag.VectorIndex, error) {
//	        return indexing.NewVectorIndexFromConfig(
//	            idxCfg, vcfg.Dimensions,
//	            indexing.WithVectorClient(vectorClient),
//	            indexing.WithTenantID(tenantID),
//	        )
//	    }),
//	)
func WithVectorIndexFactory(factory func(cfg config.RAGVectorConfig) (VectorIndex, error)) SystemOption {
	return func(o *systemOptions) {
		o.vectorIndexOverride = factory
	}
}
