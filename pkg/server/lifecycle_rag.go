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

//go:build rag

package server

import (
	ggrpc "google.golang.org/grpc"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/indexing"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// startRAG initializes the RAG system if enabled in config.
// The RAG system provides Retrieval-Augmented Generation capabilities
// for semantic search and context building.
func (s *Server) startRAG() error {
	// Skip if RAG is not enabled
	if !s.config.RAG.Enabled {
		s.logger.Info("RAG system is disabled, skipping initialization")
		return nil
	}

	s.logger.Info("Initializing RAG system...")

	// Validate RAG configuration
	if err := s.config.RAG.Validate(); err != nil {
		// RAG is optional - if configuration is invalid, log a warning and continue without RAG
		// instead of failing the entire server. This allows servers to start even with incomplete
		// RAG configuration (e.g., missing ollama service).
		s.logger.WithFields(map[string]interface{}{
			"error": err.Error(),
		}).Warn("RAG configuration validation failed - continuing without RAG support. " +
			"Fix configuration and restart server to enable RAG features.")
		return nil
	}

	// Collect system options. When the vector service is running and the qrdb
	// client is available, inject the persistent HNSW index backed by qrdb.
	ragOpts := s.buildRAGSystemOptions()

	// Create RAG system using config
	ragSystem, err := rag.NewSystem(s.config.RAG, ragOpts...)
	if err != nil {
		// RAG is optional - if the embedding provider is unavailable (e.g., ollama not running),
		// log a warning and continue without RAG instead of failing the entire server.
		s.logger.WithFields(map[string]interface{}{
			"error":    err.Error(),
			"provider": s.config.RAG.Embedding.Provider,
		}).Warn("RAG system initialization failed - continuing without RAG support. " +
			"RAG features will be unavailable until the embedding service is available and server is restarted.")
		return nil
	}

	s.ragSystem = ragSystem
	s.logger.WithFields(map[string]interface{}{
		"index_path":   s.config.RAG.IndexPath,
		"provider":     s.config.RAG.Embedding.Provider,
		"dimensions":   s.config.RAG.Embedding.Dimensions,
		"vector_index": s.ragVectorBackend(),
	}).Info("RAG system initialized successfully")

	return nil
}

// stopRAG gracefully shuts down the RAG system.
func (s *Server) stopRAG() error {
	if s.ragSystem == nil {
		return nil
	}

	s.logger.Debug("Closing RAG system...")
	if err := s.ragSystem.Close(); err != nil {
		return &qberrors.QBError{
			Code: qberrors.ErrInternal,
			Op:   "server.stopRAG",
			Err:  &ServerCauseError{Message: "failed to close RAG system", Cause: err},
		}
	}

	s.ragSystem = nil
	s.logger.Debug("RAG system closed")
	return nil
}

// buildRAGSystemOptions constructs the slice of rag.SystemOption that wires
// the persistent qrdb vector index into the RAG system when the vector
// service and qrdb client are both available.
//
// Tenant isolation: the physical qrdb namespace is prefixed with the tenant
// ID extracted from iam context if set. The RAG system itself is not per-request,
// so a static system-level tenant (empty = system tenant) is used here; for
// true per-tenant RAG a separate System per tenant is required.
func (s *Server) buildRAGSystemOptions() []rag.SystemOption {
	if s.qrdbClient == nil {
		return nil
	}

	shardID := s.config.Vector.ShardID
	if shardID == 0 {
		shardID = qbshards.PlatformShardID
	}

	sdkClient, err := s.qrdbClient.ForShard(shardID)
	if err != nil {
		s.logger.WithFields(map[string]interface{}{
			"shard_id": shardID,
			"error":    err.Error(),
		}).Warn("RAG: could not obtain qrdb client for vector shard, falling back to in-memory index")
		return nil
	}

	vectorClient := sdkClient.Vector()
	namespace := ragNamespace(s.config.RAG)
	tenantID := "" // system-level RAG uses the system tenant

	factory := buildQRDBVectorIndexFactory(vectorClient, namespace, tenantID)
	return []rag.SystemOption{rag.WithVectorIndexFactory(factory)}
}

// buildQRDBVectorIndexFactory returns a vectorIndexFactory closure that creates
// a qrdb-backed VectorIndex for the given namespace and tenant ID.
func buildQRDBVectorIndexFactory(
	client qrdbtransport.VectorClient,
	namespace string,
	tenantID string,
) func(cfg config.RAGVectorConfig) (rag.VectorIndex, error) {
	return func(vcfg config.RAGVectorConfig) (rag.VectorIndex, error) {
		idxCfg := indexing.IndexingConfig{
			IndexType: "qrdb",
			Namespace: namespace,
			Enabled:   true,
		}
		return indexing.NewVectorIndexFromConfig(
			idxCfg,
			vcfg.Dimensions,
			indexing.WithVectorClient(client),
			indexing.WithTenantID(tenantID),
			indexing.WithDistanceMetric(ragMetricToQRDB(vcfg.Metric)),
		)
	}
}

// ragNamespace derives the physical vector namespace from RAG configuration.
// The IndexPath basename is used when set; otherwise "rag" is used.
func ragNamespace(cfg config.RAGConfig) string {
	if cfg.IndexPath != "" {
		// Use the last path component as a namespace discriminator.
		for i := len(cfg.IndexPath) - 1; i >= 0; i-- {
			if cfg.IndexPath[i] == '/' {
				if tail := cfg.IndexPath[i+1:]; tail != "" {
					return "rag-" + tail
				}
			}
		}
		return "rag-" + cfg.IndexPath
	}
	return "rag"
}

// ragMetricToQRDB converts a RAG distance metric string to the qrdb
// transport constant. Falls back to cosine for unrecognised values.
func ragMetricToQRDB(metric string) string {
	metrics := map[string]string{
		"cosine": qrdbtransport.DistanceCosine,
		"l2":     qrdbtransport.DistanceEuclidean,
		"ip":     qrdbtransport.DistanceDotProduct,
		"dot":    qrdbtransport.DistanceDotProduct,
	}
	if m, ok := metrics[metric]; ok {
		return m
	}
	return qrdbtransport.DistanceCosine
}

// ragVectorBackend returns a human-readable string identifying the vector
// index backend used by the running RAG system.
func (s *Server) ragVectorBackend() string {
	if s.qrdbClient != nil {
		return "qrdb-hnsw"
	}
	return "memory"
}

// startOllama initializes the Ollama service if Ollama integration is enabled.
// The Ollama service provides LLM embedding generation and model management
// capabilities via the Ollama API.
func (s *Server) startOllama() error {
	if !s.config.Ollama.Enabled {
		s.logger.Info("Ollama service is disabled, skipping initialization")
		return nil
	}

	s.logger.Info("Initializing Ollama service...")

	// Merge with defaults to fill in zero-value fields
	ollamaCfg := s.config.Ollama.MergeDefaults()

	ollamaSvc, err := service.NewOllamaService(&ollamaCfg)
	if err != nil {
		// Ollama is optional - if initialization fails (e.g., Ollama not running),
		// log a warning and continue without Ollama support instead of failing
		// the entire server.
		s.logger.WithFields(map[string]interface{}{
			"error":    err.Error(),
			"base_url": ollamaCfg.BaseURL,
		}).Warn("Ollama service initialization failed - continuing without Ollama support. " +
			"Fix configuration and restart server to enable Ollama features.")
		return nil
	}

	s.ollamaService = ollamaSvc
	s.logger.WithFields(map[string]interface{}{
		"base_url": ollamaCfg.BaseURL,
		"model":    ollamaCfg.DefaultEmbeddingModel,
	}).Info("Ollama service initialized successfully")

	return nil
}

// stopOllama gracefully shuts down the Ollama service.
func (s *Server) stopOllama() error {
	if s.ollamaService == nil {
		return nil
	}

	s.ollamaService = nil
	s.logger.Debug("Ollama service closed")
	return nil
}

// registerOllamaService registers the Ollama gRPC service if available.
func (s *Server) registerOllamaService(grpcServer *ggrpc.Server) {
	if s.quicBaaSService == nil {
		return
	}

	ollamaSvc := s.quicBaaSService.Ollama()
	if ollamaSvc == nil {
		s.logger.Debug("Ollama service not configured, skipping gRPC registration")
		return
	}

	ollamaServer := grpcapi.NewOllamaServer(ollamaSvc, s.logger)
	pb.RegisterOllamaServiceServer(grpcServer, ollamaServer)
	s.logger.Info("Registered Ollama gRPC service")
}
