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

// Package service provides the unified service facade for QuicBaaS.
// This file implements the VectorService interface.
package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	qbErrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/shards"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// vectorService is the concrete implementation of VectorService.
// It orchestrates vector storage, indexing, and metadata operations.
// Write operations are delegated through the qrdb SDK client which handles
// Raft consensus internally. Read operations use the local storage backend.
type vectorService struct {
	storage        interfaces.VectorStorage
	sdkProvider    VectorClientProvider
	raftNode       interfaces.RaftNode
	shardID        uint64
	indexes        map[string]interfaces.VectorIndex // Map namespace -> vector index
	indexFactory   interfaces.VectorIndexFactory     // Factory for creating vector indexes
	loadCtx        context.Context                   // context of the startup namespace load
	logger         interfaces.Logger
	metrics        *vectorServiceMetrics
	mu             sync.RWMutex
	closed         bool
	indexMu        sync.RWMutex
	rebuildJobs    map[string]*RebuildJobState
	compactionJobs map[string]*CompactionJobState
	jobMu          sync.RWMutex
}

// vectorServiceMetrics holds Prometheus metrics for vector operations.
type vectorServiceMetrics struct {
	namespacesCreated *metrics.Counter
	namespacesDeleted *metrics.Counter
	vectorsUpserted   *metrics.Counter
	vectorsDeleted    *metrics.Counter
	searchesPerformed *metrics.Counter
	operationDuration *metrics.HistogramVec
	operationErrors   *metrics.CounterVec
	indexSizeBytes    *metrics.GaugeVec
	vectorCount       *metrics.GaugeVec
}

// RebuildJobState tracks the state of an index rebuild job.
type RebuildJobState struct {
	JobID            string
	Namespace        string
	InProgress       bool
	VectorsProcessed uint64
	TotalVectors     uint64
	StartedAt        time.Time
	CompletedAt      *time.Time
	Error            error
	mu               sync.RWMutex
}

// CompactionJobState tracks the state of a compaction job.
type CompactionJobState struct {
	OperationID    string
	Namespace      string
	InProgress     bool
	BytesCompacted int64
	TotalBytes     int64
	StartedAt      time.Time
	CompletedAt    *time.Time
	Error          error
	mu             sync.RWMutex
}

// VectorRaftNodeSetter allows injecting a Raft node into a VectorService
// after construction. This is needed for standalone mode where the backend
// (which implements interfaces.RaftNode) is created after the vectorService.
type VectorRaftNodeSetter interface {
	SetRaftNode(raftNode interfaces.RaftNode)
}

// VectorClientProvider provides the per-shard qrdb vector client the vector
// service writes through. The server's provider binds each client to the
// node's service principal; the caller's per-verb grant is checked in front
// of the service.
type VectorClientProvider interface {
	VectorForShard(shardID uint64) (qrdbtransport.VectorClient, error)
}

// VectorSDKProviderSetter allows injecting the vector client provider into a
// vector service after construction.
type VectorSDKProviderSetter interface {
	SetSDKProvider(provider VectorClientProvider)
}

// VectorServiceOption is a functional option for configuring the vector service.
type VectorServiceOption func(*vectorService)

// WithVectorShardID sets the shard ID for vector operations.
func WithVectorShardID(shardID uint64) VectorServiceOption {
	return func(v *vectorService) {
		v.shardID = shardID
	}
}

// WithVectorLoadContext sets the context the startup namespace load runs
// under. The load has no end user behind it, so a store that authenticates
// every call (the embedded qrdb node) needs the caller's service identity
// here. A nil ctx is ignored and the load uses context.Background().
func WithVectorLoadContext(ctx context.Context) VectorServiceOption {
	return func(v *vectorService) {
		if ctx != nil {
			v.loadCtx = ctx
		}
	}
}

// WithVectorIndexFactory sets a custom factory for creating vector indexes.
// This is useful for dependency injection and testing.
func WithVectorIndexFactory(factory interfaces.VectorIndexFactory) VectorServiceOption {
	return func(v *vectorService) {
		v.indexFactory = factory
	}
}

// WithVectorRaftNode sets the Raft node for direct vector write proposals.
// All write operations (CreateNamespace, DeleteNamespace, UpsertVector,
// DeleteVector, BatchUpsert) are proposed directly through Raft consensus.
func WithVectorRaftNode(raftNode interfaces.RaftNode) VectorServiceOption {
	return func(v *vectorService) {
		v.raftNode = raftNode
	}
}

// WithVectorSDKProvider sets the vector client provider for vector write
// operations. The qrdb client handles Raft consensus internally.
func WithVectorSDKProvider(provider VectorClientProvider) VectorServiceOption {
	return func(v *vectorService) {
		v.sdkProvider = provider
	}
}

// SetRaftNode sets the Raft node after construction. This supports
// initialization sequences where the Raft node is created after the
// vector service (e.g., standalone mode backends).
func (v *vectorService) SetRaftNode(raftNode interfaces.RaftNode) {
	v.raftNode = raftNode
}

// SetSDKProvider sets the vector client provider after construction. This supports
// initialization sequences where the ShardedClient is created after the vector
// service (e.g., standalone mode where startVector runs before startService).
func (v *vectorService) SetSDKProvider(provider VectorClientProvider) {
	v.sdkProvider = provider
}

// NewVectorService creates a new vector service implementation.
func NewVectorService(
	storage interfaces.VectorStorage,
	logger interfaces.Logger,
	opts ...VectorServiceOption,
) (VectorService, error) {
	if storage == nil {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "service.NewVectorService",
			Err:  &api.RequiredFieldError{Field: "storage"},
		}
	}

	if logger == nil {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "service.NewVectorService",
			Err:  &api.RequiredFieldError{Field: "logger"},
		}
	}

	svc := &vectorService{
		storage:        storage,
		shardID:        shards.PlatformShardID, // Default; override via WithVectorShardID
		indexes:        make(map[string]interfaces.VectorIndex),
		loadCtx:        context.Background(),
		logger:         logger.WithField("component", "vector_service"),
		rebuildJobs:    make(map[string]*RebuildJobState),
		compactionJobs: make(map[string]*CompactionJobState),
	}

	// Apply functional options
	for _, opt := range opts {
		opt(svc)
	}

	// Initialize metrics
	svc.initMetrics()

	// Load existing namespaces and initialize indexes
	if err := svc.loadNamespaces(); err != nil {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "service.NewVectorService",
			Err:  &api.OperationFailedError{Operation: "load namespaces", Cause: err},
		}
	}

	svc.logger.WithFields(map[string]interface{}{
		"shard_id":     svc.shardID,
		"raft_enabled": svc.raftNode != nil,
		"sdk_enabled":  svc.sdkProvider != nil,
	}).Info("vector service initialized")

	return svc, nil
}

// initMetrics initializes Prometheus metrics for the vector service.
func (v *vectorService) initMetrics() {
	v.metrics = &vectorServiceMetrics{
		namespacesCreated: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "vector",
			Name:      "namespaces_created_total",
			Help:      "Total number of vector namespaces created",
		}),

		namespacesDeleted: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "vector",
			Name:      "namespaces_deleted_total",
			Help:      "Total number of vector namespaces deleted",
		}),

		vectorsUpserted: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "vector",
			Name:      "vectors_upserted_total",
			Help:      "Total number of vectors upserted",
		}),

		vectorsDeleted: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "vector",
			Name:      "vectors_deleted_total",
			Help:      "Total number of vectors deleted",
		}),

		searchesPerformed: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "vector",
			Name:      "searches_total",
			Help:      "Total number of similarity searches performed",
		}),

		operationDuration: metrics.NewHistogramVec(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "vector",
			Name:      "operation_duration_seconds",
			Help:      "Duration of vector operations in seconds",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		}, []string{"operation"}),

		operationErrors: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "vector",
			Name:      "operation_errors_total",
			Help:      "Total errors by operation type",
		}, []string{"operation", "error_type"}),

		indexSizeBytes: metrics.NewGaugeVec(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "vector",
			Name:      "index_size_bytes",
			Help:      "Size of HNSW index in bytes",
		}, []string{"namespace"}),

		vectorCount: metrics.NewGaugeVec(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "vector",
			Name:      "vector_count",
			Help:      "Number of vectors in namespace",
		}, []string{"namespace"}),
	}
}

// loadNamespaces loads existing namespaces and initializes vector indexes.
func (v *vectorService) loadNamespaces() error {
	namespaces, err := v.storage.ListNamespaces(v.loadCtx)
	if err != nil {
		return err
	}

	v.logger.WithField("count", len(namespaces)).Debug("loading namespaces")

	for _, ns := range namespaces {
		// Initialize vector index for namespace using factory if available
		if v.indexFactory != nil {
			config := interfaces.DefaultVectorIndexConfig()
			config.DistanceMetric = "l2"

			index, err := v.indexFactory.CreateIndex(config)
			if err != nil {
				v.logger.WithFields(map[string]interface{}{
					"namespace": ns.Name,
					"error":     err,
				}).Warn("failed to create vector index for namespace")
				continue
			}

			v.indexMu.Lock()
			v.indexes[ns.Name] = index
			v.indexMu.Unlock()
		}

		// Update metrics
		if v.metrics != nil {
			v.metrics.vectorCount.With(ns.Name).Set(float64(ns.VectorCount))
		}
	}

	return nil
}

// ensureIndex ensures that a vector index exists for the given namespace.
// If the index doesn't exist but the namespace does in storage, it creates the
// index and loads all vectors from storage. This handles the case where vectors
// are replicated to a node but the vector index hasn't been created yet.
func (v *vectorService) ensureIndex(ctx context.Context, namespace string) (interfaces.VectorIndex, error) {
	// First check if index already exists
	v.indexMu.RLock()
	index, exists := v.indexes[namespace]
	v.indexMu.RUnlock()

	if exists {
		return index, nil
	}

	// If no factory is available, we cannot create indexes
	if v.indexFactory == nil {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.ensureIndex",
			Err:  &api.ServiceUnavailableError{Service: "vector index factory not configured"},
		}
	}

	// Index doesn't exist, check if namespace exists in storage
	info, err := v.storage.GetNamespaceInfo(ctx, namespace)
	if err != nil {
		return nil, err
	}

	// Parse distance metric from storage
	var distanceMetric string
	switch info.DistanceMetric {
	case "COSINE", "cosine":
		distanceMetric = "cosine"
	case "DOT_PRODUCT", "dot_product":
		distanceMetric = "dot_product"
	default:
		distanceMetric = "l2"
	}

	// Create vector index via factory
	config := interfaces.DefaultVectorIndexConfig()
	config.DistanceMetric = distanceMetric

	newIndex, err := v.indexFactory.CreateIndex(config)
	if err != nil {
		return nil, &api.OperationFailedError{Operation: "create vector index", Cause: err}
	}

	// Load all vectors from storage into the index
	vectors, err := v.storage.ScanNamespace(ctx, namespace, nil)
	if err != nil {
		v.logger.WithFields(map[string]interface{}{
			"namespace": namespace,
			"error":     err,
		}).Warn("failed to scan vectors for index rebuild")
	} else {
		for _, vec := range vectors {
			if err := newIndex.Insert(vec.ID, vec.Embedding); err != nil {
				v.logger.WithFields(map[string]interface{}{
					"namespace": namespace,
					"vector_id": vec.ID,
					"error":     err,
				}).Warn("failed to insert vector into index")
			}
		}
	}

	// Store the index (check again in case another goroutine created it)
	v.indexMu.Lock()
	if existingIndex, alreadyExists := v.indexes[namespace]; alreadyExists {
		v.indexMu.Unlock()
		return existingIndex, nil
	}
	v.indexes[namespace] = newIndex
	v.indexMu.Unlock()

	v.logger.WithFields(map[string]interface{}{
		"namespace": namespace,
		"vectors":   len(vectors),
	}).Info("lazily created vector index for namespace")

	return newIndex, nil
}

// =============================================================================
// SDK Client Helper
// =============================================================================

// vectorClient returns the SDK VectorClient for vector write operations.
// Writes are routed through the qrdb SDK which handles Raft consensus internally.
func (v *vectorService) vectorClient() (qrdbtransport.VectorClient, error) {
	if v.sdkProvider == nil {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.vectorClient",
			Err:  &api.ServiceUnavailableError{Service: "SDK client provider not configured for vector writes"},
		}
	}
	client, err := v.sdkProvider.VectorForShard(v.shardID)
	if err != nil {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrRaft,
			Op:   "vector.vectorClient",
			Err:  err,
		}
	}
	return client, nil
}

// =============================================================================
// Shard Helpers
// =============================================================================

// resolveShardID returns the given shardID if non-zero, otherwise falls
// back to the service's default vector shard ID.
func (v *vectorService) resolveShardID(shardID uint64) uint64 {
	if shardID == 0 {
		return v.shardID
	}
	return shardID
}

// =============================================================================
// Namespace Management
// =============================================================================

// CreateNamespace creates a new vector namespace with specified dimensions.
func (v *vectorService) CreateNamespace(ctx context.Context, req *CreateNamespaceRequest) (*CreateNamespaceResponse, error) {
	start := time.Now()
	defer func() {
		if v.metrics != nil {
			v.metrics.operationDuration.With("create_namespace").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := v.validateCreateNamespaceRequest(req); err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("create_namespace", "invalid_input").Inc()
		}
		return nil, err
	}

	// Prepare tenant context and scope namespace
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedNamespace := vectorTenantScopedNamespace(tenantID, req.Namespace)

	v.mu.Lock()
	defer v.mu.Unlock()

	if v.closed {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.CreateNamespace",
			Err:  &api.ServiceUnavailableError{Service: "vector"},
		}
	}

	// Check if namespace already exists (before SDK call since this is a read)
	existingInfo, err := v.storage.GetNamespaceInfo(ctx, scopedNamespace)
	if err == nil && existingInfo != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("create_namespace", "already_exists").Inc()
		}
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrAlreadyExists,
			Op:   "vector.CreateNamespace",
			Err:  &api.InvalidInputError{Message: "namespace " + req.Namespace + " already exists"},
		}
	}

	// Get SDK vector client for write operation
	vc, err := v.vectorClient()
	if err != nil {
		return nil, err
	}

	v.logger.WithFields(map[string]interface{}{
		"namespace": req.Namespace,
		"dimension": req.Dimension,
	}).Debug("creating namespace via SDK")

	// Delegate to SDK which handles Raft consensus internally
	sdkResp, err := vc.CreateNamespace(ctx, &qrdbtransport.CreateNamespaceRequest{
		Namespace:      scopedNamespace,
		Dimension:      req.Dimension,
		DistanceMetric: req.DistanceMetric,
		IndexType:      req.IndexType,
		IndexConfig:    req.IndexConfig,
		Metadata:       req.Metadata,
		ShardID:        v.resolveShardID(req.ShardID),
	})
	if err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("create_namespace", "sdk_error").Inc()
		}
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrRaft,
			Op:   "vector.CreateNamespace",
			Err:  err,
		}
	}

	if v.metrics != nil {
		v.metrics.namespacesCreated.Inc()
		v.metrics.vectorCount.With(req.Namespace).Set(0)
	}
	return &CreateNamespaceResponse{
		Namespace: vectorTenantStripNamespace(tenantID, sdkResp.Namespace),
		Dimension: sdkResp.Dimension,
		Timestamp: sdkResp.Timestamp,
		Success:   sdkResp.Success,
	}, nil
}

// DeleteNamespace removes a vector namespace and all its vectors.
func (v *vectorService) DeleteNamespace(ctx context.Context, req *DeleteNamespaceRequest) (*DeleteNamespaceResponse, error) {
	start := time.Now()
	defer func() {
		if v.metrics != nil {
			v.metrics.operationDuration.With("delete_namespace").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := v.validateDeleteNamespaceRequest(req); err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("delete_namespace", "invalid_input").Inc()
		}
		return nil, err
	}

	// Prepare tenant context and scope namespace
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedNamespace := vectorTenantScopedNamespace(tenantID, req.Namespace)

	// Get SDK vector client for write operation
	vc, err := v.vectorClient()
	if err != nil {
		return nil, err
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	if v.closed {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.DeleteNamespace",
			Err:  &api.ServiceUnavailableError{Service: "vector"},
		}
	}

	v.logger.WithField("namespace", req.Namespace).Debug("deleting namespace via SDK")

	// Delegate to SDK which handles Raft consensus internally
	sdkResp, err := vc.DeleteNamespace(ctx, &qrdbtransport.DeleteNamespaceRequest{
		Namespace: scopedNamespace,
		ShardID:   v.resolveShardID(req.ShardID),
	})
	if err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("delete_namespace", "sdk_error").Inc()
		}
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrRaft,
			Op:   "vector.DeleteNamespace",
			Err:  err,
		}
	}

	// Remove local vector index
	v.indexMu.Lock()
	delete(v.indexes, scopedNamespace)
	v.indexMu.Unlock()

	if v.metrics != nil {
		v.metrics.namespacesDeleted.Inc()
		v.metrics.vectorCount.With(req.Namespace).Set(0)
	}
	return &DeleteNamespaceResponse{
		Success:   sdkResp.Success,
		Timestamp: sdkResp.Timestamp,
	}, nil
}

// ListNamespaces returns all available vector namespaces.
func (v *vectorService) ListNamespaces(ctx context.Context, req *ListNamespacesRequest) (*ListNamespacesResponse, error) {
	start := time.Now()
	defer func() {
		if v.metrics != nil {
			v.metrics.operationDuration.With("list_namespaces").Observe(time.Since(start).Seconds())
		}
	}()

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.ListNamespaces",
			Err:  &api.ServiceUnavailableError{Service: "vector"},
		}
	}

	// List namespaces from storage
	namespaces, err := v.storage.ListNamespaces(ctx)
	if err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("list_namespaces", "storage_error").Inc()
		}
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrStorage,
			Op:   "vector.ListNamespaces",
			Err:  &api.OperationFailedError{Operation: "list namespaces", Cause: err},
		}
	}

	// Prepare tenant context for namespace filtering
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	// Filter namespaces by tenant prefix
	tenantFiltered := make([]interfaces.VectorNamespaceInfo, 0, len(namespaces))
	for _, ns := range namespaces {
		if vectorNamespaceBelongsToTenant(tenantID, ns.Name) {
			// Strip tenant prefix from namespace name for response
			ns.Name = vectorTenantStripNamespace(tenantID, ns.Name)
			tenantFiltered = append(tenantFiltered, ns)
		}
	}

	// Filter by prefix if specified
	filtered := make([]*NamespaceInfo, 0)
	for _, ns := range tenantFiltered {
		if req.Prefix != "" && len(ns.Name) >= len(req.Prefix) {
			if ns.Name[:len(req.Prefix)] != req.Prefix {
				continue
			}
		}
		filtered = append(filtered, &NamespaceInfo{
			Namespace:      ns.Name,
			Dimension:      ns.Dimension,
			VectorCount:    ns.VectorCount,
			DistanceMetric: ns.DistanceMetric,
			IndexType:      ns.IndexType,
			IndexConfig:    ns.IndexConfig,
			Metadata:       ns.Metadata,
			CreatedAt:      ns.CreatedAt,
			UpdatedAt:      ns.UpdatedAt,
		})
	}

	// Apply pagination
	totalCount := len(filtered)
	offset := req.Offset
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	endIdx := offset + limit
	if endIdx > len(filtered) {
		endIdx = len(filtered)
	}
	if offset > len(filtered) {
		filtered = []*NamespaceInfo{}
	} else {
		filtered = filtered[offset:endIdx]
	}

	return &ListNamespacesResponse{
		Namespaces: filtered,
		TotalCount: totalCount,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

// GetNamespaceInfo retrieves metadata about a specific namespace.
func (v *vectorService) GetNamespaceInfo(ctx context.Context, req *GetNamespaceInfoRequest) (*GetNamespaceInfoResponse, error) {
	start := time.Now()
	defer func() {
		if v.metrics != nil {
			v.metrics.operationDuration.With("get_namespace_info").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := v.validateGetNamespaceInfoRequest(req); err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("get_namespace_info", "invalid_input").Inc()
		}
		return nil, err
	}

	// Prepare tenant context and scope namespace
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedNamespace := vectorTenantScopedNamespace(tenantID, req.Namespace)

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.GetNamespaceInfo",
			Err:  &api.ServiceUnavailableError{Service: "vector"},
		}
	}

	// Get namespace info from storage
	info, err := v.storage.GetNamespaceInfo(ctx, scopedNamespace)
	if err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("get_namespace_info", "not_found").Inc()
		}
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrNotFound,
			Op:   "vector.GetNamespaceInfo",
			Err:  &api.OperationFailedError{Operation: "find namespace", Cause: err},
		}
	}

	// Convert storage metadata to interface{} map for response
	metadata := make(map[string]interface{})
	if info.Metadata != nil {
		for k, val := range info.Metadata {
			metadata[k] = val
		}
	}

	// Get index config from storage or default
	indexConfig := info.IndexConfig
	if indexConfig == nil {
		indexConfig = make(map[string]string)
	}

	// Get distance metric from storage or default
	distanceMetric := info.DistanceMetric
	if distanceMetric == "" {
		distanceMetric = "L2"
	}

	// Get index type from storage or default
	indexType := info.IndexType
	if indexType == "" {
		indexType = "HNSW"
	}

	return &GetNamespaceInfoResponse{
		Namespace:      vectorTenantStripNamespace(tenantID, info.Name),
		Dimension:      info.Dimension,
		VectorCount:    info.VectorCount,
		DistanceMetric: distanceMetric,
		IndexType:      indexType,
		IndexConfig:    indexConfig,
		Metadata:       metadata,
		CreatedAt:      info.CreatedAt,
		UpdatedAt:      info.UpdatedAt,
	}, nil
}

// =============================================================================
// Vector CRUD Operations
// =============================================================================

// UpsertVector inserts or updates a vector with optional metadata.
func (v *vectorService) UpsertVector(ctx context.Context, req *UpsertVectorRequest) (*UpsertVectorResponse, error) {
	start := time.Now()
	defer func() {
		if v.metrics != nil {
			v.metrics.operationDuration.With("upsert_vector").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := v.validateUpsertVectorRequest(req); err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("upsert_vector", "invalid_input").Inc()
		}
		return nil, err
	}

	// Prepare tenant context and scope namespace
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedNamespace := vectorTenantScopedNamespace(tenantID, req.Namespace)

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.UpsertVector",
			Err:  &api.ServiceUnavailableError{Service: "vector"},
		}
	}

	// Verify namespace exists and get its configuration
	nsInfo, err := v.storage.GetNamespaceInfo(ctx, scopedNamespace)
	if err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("upsert_vector", "namespace_not_found").Inc()
		}
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrNotFound,
			Op:   "vector.UpsertVector",
			Err:  &api.OperationFailedError{Operation: "find namespace", Cause: err},
		}
	}

	// Validate vector dimension matches namespace dimension
	// #nosec G115 -- vector dimension bounded by practical limits
	if nsInfo.Dimension > 0 && uint32(len(req.Vector)) != nsInfo.Dimension {
		if v.metrics != nil {
			v.metrics.operationErrors.With("upsert_vector", "invalid_dimension").Inc()
		}
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.UpsertVector",
			Err:  &api.InvalidInputError{Message: fmt.Sprintf("vector dimension %d does not match namespace dimension %d", len(req.Vector), nsInfo.Dimension)},
		}
	}

	// Get SDK vector client for write operation
	vc, err := v.vectorClient()
	if err != nil {
		return nil, err
	}

	// Delegate to SDK which handles Raft consensus internally
	sdkResp, err := vc.UpsertVector(ctx, &qrdbtransport.UpsertVectorRequest{
		Namespace: scopedNamespace,
		ID:        req.ID,
		Vector:    req.Vector,
		Metadata:  req.Metadata,
		ShardID:   v.resolveShardID(req.ShardID),
	})
	if err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("upsert_vector", "sdk_error").Inc()
		}
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrRaft,
			Op:   "vector.UpsertVector",
			Err:  err,
		}
	}

	// Update local vector index
	index, indexErr := v.ensureIndex(ctx, scopedNamespace)
	if indexErr != nil {
		v.logger.WithFields(map[string]interface{}{
			"namespace": req.Namespace,
			"error":     indexErr,
		}).Warn("failed to ensure index after upsert")
	} else {
		if insertErr := index.Insert(req.ID, req.Vector); insertErr != nil {
			v.logger.WithFields(map[string]interface{}{
				"namespace": req.Namespace,
				"id":        req.ID,
				"error":     insertErr,
			}).Warn("failed to insert vector into local index")
		}
	}

	if v.metrics != nil {
		v.metrics.vectorsUpserted.Inc()
		v.metrics.vectorCount.With(req.Namespace).Inc()
	}
	return &UpsertVectorResponse{
		ID:        sdkResp.ID,
		Version:   sdkResp.Version,
		Timestamp: sdkResp.Timestamp,
		Success:   sdkResp.Success,
	}, nil
}

// GetVector retrieves a vector by ID with optional metadata.
func (v *vectorService) GetVector(ctx context.Context, req *GetVectorRequest) (*GetVectorResponse, error) {
	start := time.Now()
	defer func() {
		if v.metrics != nil {
			v.metrics.operationDuration.With("get_vector").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := v.validateGetVectorRequest(req); err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("get_vector", "invalid_input").Inc()
		}
		return nil, err
	}

	// Prepare tenant context and scope namespace
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedNamespace := vectorTenantScopedNamespace(tenantID, req.Namespace)

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.GetVector",
			Err:  &api.ServiceUnavailableError{Service: "vector"},
		}
	}

	// Get vector from storage
	vecValue, err := v.storage.GetVector(ctx, scopedNamespace, req.ID)
	if err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("get_vector", "not_found").Inc()
		}
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrNotFound,
			Op:   "vector.GetVector",
			Err:  &api.OperationFailedError{Operation: "find vector", Cause: err},
		}
	}

	resp := &GetVectorResponse{
		ID:        vecValue.ID,
		Version:   vecValue.Version,
		CreatedAt: vecValue.CreatedAt,
		UpdatedAt: vecValue.UpdatedAt,
		Found:     true,
	}

	if req.IncludeVector {
		resp.Vector = vecValue.Embedding
	}

	if req.IncludeMetadata {
		resp.Metadata = convertFromStringMap(vecValue.Metadata)
	}

	return resp, nil
}

// DeleteVector removes a vector from the namespace.
func (v *vectorService) DeleteVector(ctx context.Context, req *DeleteVectorRequest) (*DeleteVectorResponse, error) {
	start := time.Now()
	defer func() {
		if v.metrics != nil {
			v.metrics.operationDuration.With("delete_vector").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := v.validateDeleteVectorRequest(req); err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("delete_vector", "invalid_input").Inc()
		}
		return nil, err
	}

	// Prepare tenant context and scope namespace
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedNamespace := vectorTenantScopedNamespace(tenantID, req.Namespace)

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.DeleteVector",
			Err:  &api.ServiceUnavailableError{Service: "vector"},
		}
	}

	// Get SDK vector client for write operation
	vc, err := v.vectorClient()
	if err != nil {
		return nil, err
	}

	// Delegate to SDK which handles Raft consensus internally
	sdkResp, err := vc.DeleteVector(ctx, &qrdbtransport.DeleteVectorRequest{
		Namespace: scopedNamespace,
		ID:        req.ID,
		ShardID:   v.resolveShardID(req.ShardID),
	})
	if err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("delete_vector", "sdk_error").Inc()
		}
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrRaft,
			Op:   "vector.DeleteVector",
			Err:  err,
		}
	}

	// Update local vector index
	v.indexMu.RLock()
	index, exists := v.indexes[scopedNamespace]
	v.indexMu.RUnlock()
	if exists {
		if deleteErr := index.Delete(req.ID); deleteErr != nil {
			v.logger.WithFields(map[string]interface{}{
				"namespace": req.Namespace,
				"id":        req.ID,
				"error":     deleteErr,
			}).Warn("failed to delete vector from local index")
		}
	}

	if v.metrics != nil {
		v.metrics.vectorsDeleted.Inc()
		v.metrics.vectorCount.With(req.Namespace).Dec()
	}
	return &DeleteVectorResponse{
		Success:   sdkResp.Success,
		Timestamp: sdkResp.Timestamp,
	}, nil
}

// BatchUpsert inserts or updates multiple vectors atomically.
func (v *vectorService) BatchUpsert(ctx context.Context, req *BatchUpsertRequest) (*BatchUpsertResponse, error) {
	start := time.Now()
	defer func() {
		if v.metrics != nil {
			v.metrics.operationDuration.With("batch_upsert").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := v.validateBatchUpsertRequest(req); err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("batch_upsert", "invalid_input").Inc()
		}
		return nil, err
	}

	// Prepare tenant context and scope namespace
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedNamespace := vectorTenantScopedNamespace(tenantID, req.Namespace)

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.BatchUpsert",
			Err:  &api.ServiceUnavailableError{Service: "vector"},
		}
	}

	// Get SDK vector client for write operation
	vc, err := v.vectorClient()
	if err != nil {
		return nil, err
	}

	// Convert to SDK transport types
	sdkVectors := make([]*qrdbtransport.VectorData, len(req.Vectors))
	for i, vec := range req.Vectors {
		sdkVectors[i] = &qrdbtransport.VectorData{
			ID:       vec.ID,
			Vector:   vec.Vector,
			Metadata: vec.Metadata,
		}
	}

	// Delegate to SDK which handles Raft consensus internally
	sdkResp, err := vc.BatchUpsert(ctx, &qrdbtransport.BatchUpsertRequest{
		Namespace: scopedNamespace,
		Vectors:   sdkVectors,
		ShardID:   v.resolveShardID(req.ShardID),
	})
	if err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("batch_upsert", "sdk_error").Inc()
		}
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrRaft,
			Op:   "vector.BatchUpsert",
			Err:  err,
		}
	}

	// Update local vector index
	index, indexErr := v.ensureIndex(ctx, scopedNamespace)
	if indexErr != nil {
		v.logger.WithFields(map[string]interface{}{
			"namespace": req.Namespace,
			"error":     indexErr,
		}).Warn("failed to ensure index after batch upsert")
	} else {
		for _, vec := range req.Vectors {
			if insertErr := index.Insert(vec.ID, vec.Vector); insertErr != nil {
				v.logger.WithFields(map[string]interface{}{
					"namespace": req.Namespace,
					"id":        vec.ID,
					"error":     insertErr,
				}).Warn("failed to insert vector into local index")
			}
		}
	}

	if v.metrics != nil {
		v.metrics.vectorsUpserted.Add(float64(len(req.Vectors)))
		v.metrics.vectorCount.With(req.Namespace).Add(float64(len(req.Vectors)))
	}
	return &BatchUpsertResponse{
		IDs:           sdkResp.IDs,
		Timestamp:     sdkResp.Timestamp,
		Success:       sdkResp.Success,
		InsertedCount: sdkResp.InsertedCount,
	}, nil
}

// =============================================================================
// Search Operations
// =============================================================================

// SearchSimilar performs k-nearest neighbor similarity search.
func (v *vectorService) SearchSimilar(ctx context.Context, req *SearchSimilarRequest) (*SearchSimilarResponse, error) {
	start := time.Now()
	defer func() {
		if v.metrics != nil {
			v.metrics.operationDuration.With("search_similar").Observe(time.Since(start).Seconds())
			v.metrics.searchesPerformed.Inc()
		}
	}()

	// Validate request
	if err := v.validateSearchSimilarRequest(req); err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("search_similar", "invalid_input").Inc()
		}
		return nil, err
	}

	// Prepare tenant context and scope namespace
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedNamespace := vectorTenantScopedNamespace(tenantID, req.Namespace)

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.SearchSimilar",
			Err:  &api.ServiceUnavailableError{Service: "vector"},
		}
	}

	// Get or create HNSW index (lazily initialize if needed for replicated data)
	index, err := v.ensureIndex(ctx, scopedNamespace)
	if err != nil {
		// Namespace may not have been replicated to this node yet
		// Return empty results instead of an error to handle eventual consistency
		v.logger.WithFields(map[string]interface{}{
			"namespace": req.Namespace,
			"error":     err,
		}).Debug("namespace not available on this node yet, returning empty results")
		return &SearchSimilarResponse{
			Results:       []*VectorSearchResult{},
			QueryDuration: 0,
			Timestamp:     time.Now().UnixNano(),
		}, nil
	}

	// Perform HNSW search
	searchStart := time.Now()
	results, err := index.Search(req.QueryVector, req.K, nil)
	if err != nil {
		// If the index is empty, return empty results instead of an error
		if index.Count() == 0 {
			return &SearchSimilarResponse{
				Results:       []*VectorSearchResult{},
				QueryDuration: time.Since(searchStart).Microseconds(),
				Timestamp:     time.Now().UnixNano(),
			}, nil
		}
		if v.metrics != nil {
			v.metrics.operationErrors.With("search_similar", "search_failed").Inc()
		}
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.SearchSimilar",
			Err:  &api.OperationFailedError{Operation: "HNSW search", Cause: err},
		}
	}
	queryDuration := time.Since(searchStart).Microseconds()

	// Convert results
	searchResults := make([]*VectorSearchResult, 0, len(results))
	for i, res := range results {
		// Apply min score filter
		if req.MinScore > 0 && res.Distance > req.MinScore {
			continue
		}

		result := &VectorSearchResult{
			ID:       res.ID,
			Score:    res.Distance,
			Distance: res.Distance,
			Rank:     i + 1,
		}

		// Fetch full vector and metadata if requested
		if req.IncludeVectors || req.IncludeMetadata {
			vec, err := v.storage.GetVector(ctx, scopedNamespace, res.ID)
			if err == nil {
				if req.IncludeVectors {
					result.Vector = vec.Embedding
				}
				if req.IncludeMetadata {
					result.Metadata = convertFromStringMap(vec.Metadata)

					// Apply metadata filter
					if req.MetadataFilter != nil && !matchesMetadataFilter(result.Metadata, req.MetadataFilter) {
						continue
					}
				}
			}
		}

		searchResults = append(searchResults, result)
	}

	return &SearchSimilarResponse{
		Results:       searchResults,
		Timestamp:     time.Now().UnixNano(),
		QueryDuration: queryDuration,
	}, nil
}

// SearchByMetadata performs metadata-based vector search.
func (v *vectorService) SearchByMetadata(ctx context.Context, req *SearchByMetadataRequest) (*SearchByMetadataResponse, error) {
	start := time.Now()
	defer func() {
		if v.metrics != nil {
			v.metrics.operationDuration.With("search_by_metadata").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := v.validateSearchByMetadataRequest(req); err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("search_by_metadata", "invalid_input").Inc()
		}
		return nil, err
	}

	// Prepare tenant context and scope namespace
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedNamespace := vectorTenantScopedNamespace(tenantID, req.Namespace)

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.SearchByMetadata",
			Err:  &api.ServiceUnavailableError{Service: "vector"},
		}
	}

	// Build metadata filters
	filters := make([]interfaces.VectorMetadataFilter, 0, len(req.Filters))
	for key, value := range req.Filters {
		filters = append(filters, interfaces.VectorMetadataFilter{
			Field:    key,
			Value:    fmt.Sprintf("%v", value),
			Operator: interfaces.VectorFilterEqual,
		})
	}

	// Scan by filters
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	opts := &interfaces.VectorScanOptions{
		Limit:  limit,
		Offset: req.Offset,
	}

	vecs, err := v.storage.ScanByMultipleFilters(ctx, scopedNamespace, filters, opts)
	if err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("search_by_metadata", "scan_failed").Inc()
		}
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.SearchByMetadata",
			Err:  &api.OperationFailedError{Operation: "metadata scan", Cause: err},
		}
	}

	// Convert results
	results := make([]*VectorData, len(vecs))
	for i, vec := range vecs {
		results[i] = &VectorData{
			ID:        vec.ID,
			Version:   vec.Version,
			CreatedAt: vec.CreatedAt,
			UpdatedAt: vec.UpdatedAt,
		}

		if req.IncludeVectors {
			results[i].Vector = vec.Embedding
		}

		if req.IncludeMetadata {
			results[i].Metadata = convertFromStringMap(vec.Metadata)
		}
	}

	return &SearchByMetadataResponse{
		Results:    results,
		TotalCount: uint64(len(results)),
		Timestamp:  time.Now().UnixNano(),
		NextCursor: "",
	}, nil
}

// HybridSearch combines similarity and metadata filtering.
func (v *vectorService) HybridSearch(ctx context.Context, req *HybridSearchRequest) (*HybridSearchResponse, error) {
	start := time.Now()
	defer func() {
		if v.metrics != nil {
			v.metrics.operationDuration.With("hybrid_search").Observe(time.Since(start).Seconds())
		}
	}()

	// Perform similarity search first
	similarReq := &SearchSimilarRequest{
		Namespace:       req.Namespace,
		QueryVector:     req.QueryVector,
		K:               req.K * 2, // Get more candidates for filtering
		EfSearch:        req.EfSearch,
		MinScore:        req.MinScore,
		IncludeVectors:  req.IncludeVectors,
		IncludeMetadata: true, // Always need metadata for filtering
		ShardID:         v.resolveShardID(req.ShardID),
		Consistency:     req.Consistency,
	}

	similarResp, err := v.SearchSimilar(ctx, similarReq)
	if err != nil {
		return nil, err
	}

	// Filter by metadata
	filtered := make([]*VectorSearchResult, 0)
	for _, result := range similarResp.Results {
		if matchesMetadataFilter(result.Metadata, req.MetadataFilter) {
			filtered = append(filtered, result)
			if len(filtered) >= req.K {
				break
			}
		}
	}

	return &HybridSearchResponse{
		Results:         filtered,
		TotalCandidates: len(similarResp.Results),
		TotalFiltered:   len(filtered),
		Timestamp:       time.Now().UnixNano(),
		QueryDuration:   similarResp.QueryDuration,
	}, nil
}

// =============================================================================
// Index Management
// =============================================================================

// RebuildIndex rebuilds the vector index for a namespace.
func (v *vectorService) RebuildIndex(ctx context.Context, req *RebuildIndexRequest) (*RebuildIndexResponse, error) {
	start := time.Now()
	defer func() {
		if v.metrics != nil {
			v.metrics.operationDuration.With("rebuild_index").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := v.validateRebuildIndexRequest(req); err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("rebuild_index", "invalid_input").Inc()
		}
		return nil, err
	}

	// Prepare tenant context and scope namespace
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedNamespace := vectorTenantScopedNamespace(tenantID, req.Namespace)

	// Generate job ID
	jobID := uuid.New().String()

	// Get namespace info
	info, err := v.storage.GetNamespaceInfo(ctx, scopedNamespace)
	if err != nil {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrNotFound,
			Op:   "vector.RebuildIndex",
			Err:  &api.OperationFailedError{Operation: "find namespace", Cause: err},
		}
	}

	// Create rebuild job
	job := &RebuildJobState{
		JobID:            jobID,
		Namespace:        scopedNamespace,
		InProgress:       true,
		VectorsProcessed: 0,
		TotalVectors:     info.VectorCount,
		StartedAt:        time.Now(),
	}

	v.jobMu.Lock()
	v.rebuildJobs[jobID] = job
	v.jobMu.Unlock()

	// If async, start background rebuild
	if req.Async {
		go func() {
			if err := v.performIndexRebuild(ctx, jobID, scopedNamespace, req.IndexConfig); err != nil {
				v.logger.WithFields(map[string]interface{}{
					"job_id":    jobID,
					"namespace": req.Namespace,
					"error":     err,
				}).Error("background index rebuild failed")
			}
		}()

		return &RebuildIndexResponse{
			JobID:                  jobID,
			Status:                 "in_progress",
			VectorsProcessed:       0,
			TotalVectors:           info.VectorCount,
			PercentComplete:        0.0,
			EstimatedTimeRemaining: 0,
			Timestamp:              time.Now().UnixNano(),
			Success:                false,
		}, nil
	}

	// Synchronous rebuild
	if err := v.performIndexRebuild(ctx, jobID, scopedNamespace, req.IndexConfig); err != nil {
		return nil, err
	}

	job.mu.RLock()
	completed := job.CompletedAt
	job.mu.RUnlock()

	var ts int64
	if completed != nil {
		ts = completed.UnixNano()
	}

	return &RebuildIndexResponse{
		JobID:                  jobID,
		Status:                 "completed",
		VectorsProcessed:       info.VectorCount,
		TotalVectors:           info.VectorCount,
		PercentComplete:        100.0,
		EstimatedTimeRemaining: 0,
		Timestamp:              ts,
		Success:                true,
	}, nil
}

// GetRebuildProgress retrieves the progress of an index rebuild operation.
func (v *vectorService) GetRebuildProgress(ctx context.Context, req *GetRebuildProgressRequest) (*GetRebuildProgressResponse, error) {
	if err := v.validateGetRebuildProgressRequest(req); err != nil {
		return nil, err
	}

	var job *RebuildJobState
	var found bool

	v.jobMu.RLock()
	if req.JobID != "" {
		// Find by job ID
		job, found = v.rebuildJobs[req.JobID]
	} else {
		// Find by namespace (return most recent job)
		var latestStartTime time.Time
		for _, j := range v.rebuildJobs {
			j.mu.RLock()
			if j.Namespace == req.Namespace && j.StartedAt.After(latestStartTime) {
				job = j
				latestStartTime = j.StartedAt
				found = true
			}
			j.mu.RUnlock()
		}
	}
	v.jobMu.RUnlock()

	if !found {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrNotFound,
			Op:   "vector.GetRebuildProgress",
			Err:  &api.NotFoundError{Resource: "rebuild job", ID: ""},
		}
	}

	job.mu.RLock()
	defer job.mu.RUnlock()

	// Calculate progress
	var percentComplete float64
	if job.CompletedAt != nil && job.Error == nil {
		// Completed successfully - 100%
		percentComplete = 100.0
	} else if job.TotalVectors > 0 {
		percentComplete = float64(job.VectorsProcessed) / float64(job.TotalVectors) * 100.0
	}

	// Calculate elapsed time
	elapsed := time.Since(job.StartedAt)
	elapsedMs := elapsed.Milliseconds()

	// Calculate estimated time remaining
	var estimatedRemainingMs int64
	if job.InProgress && job.VectorsProcessed > 0 && job.TotalVectors > job.VectorsProcessed {
		// Calculate rate (vectors per millisecond)
		rate := float64(job.VectorsProcessed) / float64(elapsedMs)
		if rate > 0 {
			remaining := job.TotalVectors - job.VectorsProcessed
			estimatedRemainingMs = int64(float64(remaining) / rate)
		}
	}

	// Determine status
	var status string
	switch {
	case job.Error != nil:
		status = "failed"
	case job.CompletedAt != nil:
		status = "completed"
	case job.InProgress:
		status = "running"
	default:
		status = "pending"
	}

	// Format error message
	var errMsg string
	if job.Error != nil {
		errMsg = job.Error.Error()
	}

	// Get completed timestamp
	var completedAt int64
	if job.CompletedAt != nil {
		completedAt = job.CompletedAt.Unix()
	}

	return &GetRebuildProgressResponse{
		JobID:                    job.JobID,
		Namespace:                job.Namespace,
		Status:                   status,
		VectorsProcessed:         job.VectorsProcessed,
		TotalVectors:             job.TotalVectors,
		PercentComplete:          percentComplete,
		ElapsedTimeMs:            elapsedMs,
		EstimatedTimeRemainingMs: estimatedRemainingMs,
		StartedAt:                job.StartedAt.Unix(),
		CompletedAt:              completedAt,
		Error:                    errMsg,
	}, nil
}

// performIndexRebuild performs the actual index rebuild operation.
func (v *vectorService) performIndexRebuild(ctx context.Context, jobID, namespace string, indexConfig map[string]string) error {
	v.jobMu.RLock()
	job, exists := v.rebuildJobs[jobID]
	v.jobMu.RUnlock()

	if !exists {
		return &qbErrors.QBError{
			Code: qbErrors.ErrNotFound,
			Op:   "vector.performIndexRebuild",
			Err:  &api.NotFoundError{Resource: "rebuild job", ID: jobID},
		}
	}

	defer func() {
		job.mu.Lock()
		job.InProgress = false
		now := time.Now()
		job.CompletedAt = &now
		job.mu.Unlock()
	}()

	// If no factory is available, we cannot rebuild
	if v.indexFactory == nil {
		job.mu.Lock()
		job.Error = &api.ServiceUnavailableError{Service: "vector index factory not configured"}
		job.mu.Unlock()
		return &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.performIndexRebuild",
			Err:  &api.ServiceUnavailableError{Service: "vector index factory not configured"},
		}
	}

	// Create new vector index via factory
	config := interfaces.DefaultVectorIndexConfig()

	// Parse custom config if provided
	if indexConfig != nil {
		if m, ok := indexConfig["m"]; ok {
			if _, err := fmt.Sscanf(m, "%d", &config.M); err != nil {
				v.logger.WithFields(map[string]interface{}{
					"parameter": "m",
					"value":     m,
					"error":     err,
				}).Warn("failed to parse index parameter")
			} else {
				config.MMax = config.M * 2
			}
		}
		if ef, ok := indexConfig["ef_construction"]; ok {
			if _, err := fmt.Sscanf(ef, "%d", &config.EfConstruction); err != nil {
				v.logger.WithFields(map[string]interface{}{
					"parameter": "ef_construction",
					"value":     ef,
					"error":     err,
				}).Warn("failed to parse index parameter")
			}
		}
		if ef, ok := indexConfig["ef_search"]; ok {
			if _, err := fmt.Sscanf(ef, "%d", &config.EfSearch); err != nil {
				v.logger.WithFields(map[string]interface{}{
					"parameter": "ef_search",
					"value":     ef,
					"error":     err,
				}).Warn("failed to parse index parameter")
			}
		}
	}

	newIndex, err := v.indexFactory.CreateIndex(config)
	if err != nil {
		job.mu.Lock()
		job.Error = err
		job.mu.Unlock()
		return &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.performIndexRebuild",
			Err:  &api.OperationFailedError{Operation: "create new index", Cause: err},
		}
	}

	// Scan all vectors and rebuild index
	opts := &interfaces.VectorScanOptions{
		Limit: 1000,
	}

	offset := 0
	for {
		opts.Offset = offset
		vecs, err := v.storage.ScanNamespace(ctx, namespace, opts)
		if err != nil {
			job.mu.Lock()
			job.Error = err
			job.mu.Unlock()
			return &qbErrors.QBError{
				Code: qbErrors.ErrInternal,
				Op:   "vector.performIndexRebuild",
				Err:  &api.OperationFailedError{Operation: "scan vectors", Cause: err},
			}
		}

		if len(vecs) == 0 {
			break
		}

		for _, vec := range vecs {
			if err := newIndex.Insert(vec.ID, vec.Embedding); err != nil {
				v.logger.WithFields(map[string]interface{}{
					"namespace": namespace,
					"id":        vec.ID,
					"error":     err,
				}).Warn("failed to insert vector into new index")
			}

			job.mu.Lock()
			job.VectorsProcessed++
			job.mu.Unlock()
		}

		offset += len(vecs)

		// Break if we got less than requested
		if len(vecs) < opts.Limit {
			break
		}
	}

	// Replace old index with new one
	v.indexMu.Lock()
	v.indexes[namespace] = newIndex
	v.indexMu.Unlock()

	v.logger.WithFields(map[string]interface{}{
		"namespace": namespace,
		"job_id":    jobID,
	}).Info("index rebuild completed successfully")

	return nil
}

// GetIndexStats retrieves statistics about the vector index.
func (v *vectorService) GetIndexStats(ctx context.Context, req *GetIndexStatsRequest) (*GetIndexStatsResponse, error) {
	start := time.Now()
	defer func() {
		if v.metrics != nil {
			v.metrics.operationDuration.With("get_index_stats").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := v.validateGetIndexStatsRequest(req); err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("get_index_stats", "invalid_input").Inc()
		}
		return nil, err
	}

	// Prepare tenant context and scope namespace
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedNamespace := vectorTenantScopedNamespace(tenantID, req.Namespace)

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.GetIndexStats",
			Err:  &api.ServiceUnavailableError{Service: "vector"},
		}
	}

	// Get namespace info
	info, err := v.storage.GetNamespaceInfo(ctx, scopedNamespace)
	if err != nil {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrNotFound,
			Op:   "vector.GetIndexStats",
			Err:  &api.OperationFailedError{Operation: "find namespace", Cause: err},
		}
	}

	// Get vector index stats
	v.indexMu.RLock()
	index, exists := v.indexes[scopedNamespace]
	v.indexMu.RUnlock()

	var indexCount int64
	var indexSizeBytes uint64
	var memoryUsageBytes uint64

	if exists {
		indexCount = index.Count()

		// Calculate index size and memory usage based on index structure
		stats := index.GetStats()
		if stats != nil && stats.NodeCount > 0 {
			// Base size per node (ID + Layer + Mutex + Map overhead)
			const baseNodeOverhead = 8 + 8 + 24 + 48 // 88 bytes

			// Vector data size: 4 bytes per float32 dimension
			vectorDataSize := conv.ClampIntToUint64(stats.Dimensions) * 4

			// Connection data size: average connections * 8 bytes (uint64) * (maxLayer + 1)
			avgConnectionsPerNode := stats.AvgConnections
			maxLayers := stats.MaxLayer + 1
			if maxLayers < 1 {
				maxLayers = 1
			}
			connectionDataSize := uint64(avgConnectionsPerNode * float64(maxLayers) * 8)

			// Total per-node size
			perNodeSize := uint64(baseNodeOverhead) + vectorDataSize + connectionDataSize

			// Total index size
			indexSizeBytes = uint64(stats.NodeCount) * perNodeSize // #nosec G115 -- NodeCount is int64 from HNSW stats, always non-negative

			// Memory usage includes additional runtime overhead (slice headers, map buckets, etc.)
			// Estimate ~20% overhead for Go runtime structures
			memoryUsageBytes = indexSizeBytes + (indexSizeBytes / 5)
		}
	}

	// Check for negative values before conversion
	var vectorCount uint64
	if indexCount < 0 {
		v.logger.WithFields(map[string]interface{}{
			"namespace":   req.Namespace,
			"index_count": indexCount,
		}).Warn("negative index count detected, setting to 0")
		vectorCount = 0
	} else {
		vectorCount = uint64(indexCount)
	}

	return &GetIndexStatsResponse{
		Namespace:        req.Namespace,
		IndexType:        "HNSW",
		VectorCount:      vectorCount,
		Dimension:        info.Dimension,
		IndexSizeBytes:   indexSizeBytes,
		MemoryUsageBytes: memoryUsageBytes,
		IndexConfig:      make(map[string]string),
		SearchPerformance: &IndexPerformanceMetrics{
			AverageSearchTimeMs: 0.0,
			P50SearchTimeMs:     0.0,
			P95SearchTimeMs:     0.0,
			P99SearchTimeMs:     0.0,
			TotalSearches:       0,
			CacheHitRate:        0.0,
		},
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// =============================================================================
// Validation Methods
// =============================================================================

func (v *vectorService) validateCreateNamespaceRequest(req *CreateNamespaceRequest) error {
	if req == nil {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateCreateNamespaceRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Namespace == "" {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateCreateNamespaceRequest",
			Err:  &api.RequiredFieldError{Field: "namespace"},
		}
	}
	if req.Dimension == 0 {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateCreateNamespaceRequest",
			Err:  &api.InvalidInputError{Message: "dimension must be greater than 0"},
		}
	}
	return nil
}

func (v *vectorService) validateDeleteNamespaceRequest(req *DeleteNamespaceRequest) error {
	if req == nil {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateDeleteNamespaceRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Namespace == "" {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateDeleteNamespaceRequest",
			Err:  &api.RequiredFieldError{Field: "namespace"},
		}
	}
	return nil
}

func (v *vectorService) validateGetNamespaceInfoRequest(req *GetNamespaceInfoRequest) error {
	if req == nil {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateGetNamespaceInfoRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Namespace == "" {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateGetNamespaceInfoRequest",
			Err:  &api.RequiredFieldError{Field: "namespace"},
		}
	}
	return nil
}

func (v *vectorService) validateUpsertVectorRequest(req *UpsertVectorRequest) error {
	if req == nil {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateUpsertVectorRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Namespace == "" {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateUpsertVectorRequest",
			Err:  &api.RequiredFieldError{Field: "namespace"},
		}
	}
	if req.ID == 0 {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateUpsertVectorRequest",
			Err:  &api.InvalidInputError{Message: "ID must be greater than 0"},
		}
	}
	if len(req.Vector) == 0 {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateUpsertVectorRequest",
			Err:  &api.RequiredFieldError{Field: "vector"},
		}
	}
	return nil
}

func (v *vectorService) validateGetVectorRequest(req *GetVectorRequest) error {
	if req == nil {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateGetVectorRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Namespace == "" {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateGetVectorRequest",
			Err:  &api.RequiredFieldError{Field: "namespace"},
		}
	}
	if req.ID == 0 {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateGetVectorRequest",
			Err:  &api.InvalidInputError{Message: "ID must be greater than 0"},
		}
	}
	return nil
}

func (v *vectorService) validateDeleteVectorRequest(req *DeleteVectorRequest) error {
	if req == nil {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateDeleteVectorRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Namespace == "" {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateDeleteVectorRequest",
			Err:  &api.RequiredFieldError{Field: "namespace"},
		}
	}
	if req.ID == 0 {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateDeleteVectorRequest",
			Err:  &api.InvalidInputError{Message: "ID must be greater than 0"},
		}
	}
	return nil
}

func (v *vectorService) validateBatchUpsertRequest(req *BatchUpsertRequest) error {
	if req == nil {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateBatchUpsertRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Namespace == "" {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateBatchUpsertRequest",
			Err:  &api.RequiredFieldError{Field: "namespace"},
		}
	}
	if len(req.Vectors) == 0 {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateBatchUpsertRequest",
			Err:  &api.RequiredFieldError{Field: "vectors"},
		}
	}
	return nil
}

func (v *vectorService) validateSearchSimilarRequest(req *SearchSimilarRequest) error {
	if req == nil {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateSearchSimilarRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Namespace == "" {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateSearchSimilarRequest",
			Err:  &api.RequiredFieldError{Field: "namespace"},
		}
	}
	if len(req.QueryVector) == 0 {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateSearchSimilarRequest",
			Err:  &api.RequiredFieldError{Field: "query vector"},
		}
	}
	if req.K <= 0 {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateSearchSimilarRequest",
			Err:  &api.InvalidInputError{Message: "k must be greater than 0"},
		}
	}
	return nil
}

func (v *vectorService) validateSearchByMetadataRequest(req *SearchByMetadataRequest) error {
	if req == nil {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateSearchByMetadataRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Namespace == "" {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateSearchByMetadataRequest",
			Err:  &api.RequiredFieldError{Field: "namespace"},
		}
	}
	return nil
}

func (v *vectorService) validateRebuildIndexRequest(req *RebuildIndexRequest) error {
	if req == nil {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateRebuildIndexRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Namespace == "" {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateRebuildIndexRequest",
			Err:  &api.RequiredFieldError{Field: "namespace"},
		}
	}
	return nil
}

func (v *vectorService) validateGetRebuildProgressRequest(req *GetRebuildProgressRequest) error {
	if req == nil {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateGetRebuildProgressRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.JobID == "" && req.Namespace == "" {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateGetRebuildProgressRequest",
			Err:  &api.InvalidInputError{Message: "job_id or namespace is required"},
		}
	}
	return nil
}

func (v *vectorService) validateGetIndexStatsRequest(req *GetIndexStatsRequest) error {
	if req == nil {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateGetIndexStatsRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Namespace == "" {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateGetIndexStatsRequest",
			Err:  &api.RequiredFieldError{Field: "namespace"},
		}
	}
	return nil
}

// =============================================================================
// Helper Functions
// =============================================================================

// convertToStringMap converts map[string]interface{} to map[string]string.
func convertToStringMap(m map[string]interface{}) map[string]string {
	if m == nil {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, val := range m {
		result[k] = fmt.Sprintf("%v", val)
	}
	return result
}

// convertFromStringMap converts map[string]string to map[string]interface{}.
func convertFromStringMap(m map[string]string) map[string]interface{} {
	if m == nil {
		return nil
	}
	result := make(map[string]interface{}, len(m))
	for k, val := range m {
		result[k] = val
	}
	return result
}

// matchesMetadataFilter checks if metadata matches filter criteria.
func matchesMetadataFilter(metadata, filter map[string]interface{}) bool {
	if len(filter) == 0 {
		return true
	}
	for key, value := range filter {
		metaValue, exists := metadata[key]
		if !exists {
			return false
		}
		if fmt.Sprintf("%v", metaValue) != fmt.Sprintf("%v", value) {
			return false
		}
	}
	return true
}

// =============================================================================
// Compaction Operations
// =============================================================================

// CompactNamespace triggers compaction for a vector namespace.
func (v *vectorService) CompactNamespace(ctx context.Context, req *CompactNamespaceRequest) (*CompactNamespaceResponse, error) {
	start := time.Now()
	defer func() {
		if v.metrics != nil {
			v.metrics.operationDuration.With("compact_namespace").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := v.validateCompactNamespaceRequest(req); err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("compact_namespace", "invalid_input").Inc()
		}
		return nil, err
	}

	// Prepare tenant context and scope namespace
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedNamespace := vectorTenantScopedNamespace(tenantID, req.Namespace)

	v.mu.RLock()
	if v.closed {
		v.mu.RUnlock()
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.CompactNamespace",
			Err:  &api.ServiceUnavailableError{Service: "vector"},
		}
	}
	v.mu.RUnlock()

	// Verify namespace exists
	info, err := v.storage.GetNamespaceInfo(ctx, scopedNamespace)
	if err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("compact_namespace", "not_found").Inc()
		}
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrNotFound,
			Op:   "vector.CompactNamespace",
			Err:  &api.OperationFailedError{Operation: "find namespace", Cause: err},
		}
	}

	// Generate operation ID
	operationID := uuid.New().String()

	// Create compaction job
	job := &CompactionJobState{
		OperationID: operationID,
		Namespace:   scopedNamespace,
		InProgress:  true,
		TotalBytes:  int64(info.StorageBytes),
		StartedAt:   time.Now(),
	}

	v.jobMu.Lock()
	v.compactionJobs[operationID] = job
	v.jobMu.Unlock()

	v.logger.WithFields(map[string]interface{}{
		"namespace":    req.Namespace,
		"operation_id": operationID,
		"async":        req.Async,
	}).Info("starting namespace compaction")

	// If async, start background compaction
	if req.Async {
		go func() {
			if err := v.performCompaction(context.WithoutCancel(ctx), operationID, scopedNamespace); err != nil {
				v.logger.WithFields(map[string]interface{}{
					"operation_id": operationID,
					"namespace":    req.Namespace,
					"error":        err,
				}).Error("background compaction failed")
			}
		}()

		return &CompactNamespaceResponse{
			OperationID: operationID,
			StartedAt:   job.StartedAt.UnixNano(),
			Success:     false, // Will be true when complete
		}, nil
	}

	// Synchronous compaction
	if err := v.performCompaction(ctx, operationID, scopedNamespace); err != nil {
		return nil, err
	}

	job.mu.RLock()
	bytesCompacted := job.BytesCompacted
	completedAt := job.CompletedAt
	job.mu.RUnlock()

	var completedAtNano *int64
	if completedAt != nil {
		ts := completedAt.UnixNano()
		completedAtNano = &ts
	}

	return &CompactNamespaceResponse{
		OperationID:    operationID,
		StartedAt:      job.StartedAt.UnixNano(),
		CompletedAt:    completedAtNano,
		BytesCompacted: bytesCompacted,
		Success:        true,
	}, nil
}

// performCompaction executes the actual compaction operation.
func (v *vectorService) performCompaction(ctx context.Context, operationID, namespace string) error {
	v.jobMu.RLock()
	job, exists := v.compactionJobs[operationID]
	v.jobMu.RUnlock()

	if !exists {
		return &qbErrors.QBError{
			Code: qbErrors.ErrNotFound,
			Op:   "vector.performCompaction",
			Err:  &api.NotFoundError{Resource: "compaction job", ID: operationID},
		}
	}

	defer func() {
		job.mu.Lock()
		job.InProgress = false
		now := time.Now()
		job.CompletedAt = &now
		job.mu.Unlock()
	}()

	// Get pre-compaction stats
	preStats, _ := v.storage.GetStats(ctx, namespace)
	var preSizeBytes int64
	if preStats != nil {
		preSizeBytes = conv.ClampUint64ToInt64(preStats.SizeBytes)
	}

	// Trigger compaction on storage
	if err := v.storage.Compact(ctx, namespace); err != nil {
		job.mu.Lock()
		job.Error = err
		job.mu.Unlock()
		return &qbErrors.QBError{
			Code: qbErrors.ErrStorage,
			Op:   "vector.performCompaction",
			Err:  &api.OperationFailedError{Operation: "compaction", Cause: err},
		}
	}

	// Get post-compaction stats
	postStats, _ := v.storage.GetStats(ctx, namespace)
	var postSizeBytes int64
	if postStats != nil {
		postSizeBytes = conv.ClampUint64ToInt64(postStats.SizeBytes)
	}

	// Calculate bytes compacted (reclaimed)
	bytesCompacted := preSizeBytes - postSizeBytes
	if bytesCompacted < 0 {
		bytesCompacted = 0 // In case size increased due to other operations
	}

	job.mu.Lock()
	job.BytesCompacted = bytesCompacted
	job.mu.Unlock()

	v.logger.WithFields(map[string]interface{}{
		"namespace":       namespace,
		"operation_id":    operationID,
		"bytes_compacted": bytesCompacted,
	}).Info("compaction completed successfully")

	return nil
}

// GetCompactionProgress retrieves the progress of a compaction operation.
func (v *vectorService) GetCompactionProgress(ctx context.Context, req *GetCompactionProgressRequest) (*GetCompactionProgressResponse, error) {
	start := time.Now()
	defer func() {
		if v.metrics != nil {
			v.metrics.operationDuration.With("get_compaction_progress").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := v.validateGetCompactionProgressRequest(req); err != nil {
		if v.metrics != nil {
			v.metrics.operationErrors.With("get_compaction_progress", "invalid_input").Inc()
		}
		return nil, err
	}

	v.mu.RLock()
	if v.closed {
		v.mu.RUnlock()
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.GetCompactionProgress",
			Err:  &api.ServiceUnavailableError{Service: "vector"},
		}
	}
	v.mu.RUnlock()

	v.jobMu.RLock()
	defer v.jobMu.RUnlock()

	// If operation ID provided, look for specific job
	if req.OperationID != "" {
		job, exists := v.compactionJobs[req.OperationID]
		if !exists {
			return nil, &qbErrors.QBError{
				Code: qbErrors.ErrNotFound,
				Op:   "vector.GetCompactionProgress",
				Err:  &api.NotFoundError{Resource: "compaction operation", ID: req.OperationID},
			}
		}

		job.mu.RLock()
		defer job.mu.RUnlock()

		var completedAt *int64
		if job.CompletedAt != nil {
			ts := job.CompletedAt.UnixNano()
			completedAt = &ts
		}

		var errMsg string
		if job.Error != nil {
			errMsg = job.Error.Error()
		}

		percentComplete := 0.0
		if job.TotalBytes > 0 {
			percentComplete = float64(job.BytesCompacted) / float64(job.TotalBytes) * 100
		}
		if !job.InProgress && job.Error == nil {
			percentComplete = 100.0
		}

		return &GetCompactionProgressResponse{
			OperationID:     job.OperationID,
			Namespace:       job.Namespace,
			BytesCompacted:  job.BytesCompacted,
			TotalBytes:      job.TotalBytes,
			PercentComplete: percentComplete,
			InProgress:      job.InProgress,
			StartedAt:       job.StartedAt.UnixNano(),
			CompletedAt:     completedAt,
			Error:           errMsg,
		}, nil
	}

	// Find the most recent job for the namespace
	var latestJob *CompactionJobState
	for _, job := range v.compactionJobs {
		if job.Namespace == req.Namespace {
			if latestJob == nil || job.StartedAt.After(latestJob.StartedAt) {
				latestJob = job
			}
		}
	}

	if latestJob == nil {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrNotFound,
			Op:   "vector.GetCompactionProgress",
			Err:  &api.NotFoundError{Resource: "compaction operations for namespace", ID: req.Namespace},
		}
	}

	latestJob.mu.RLock()
	defer latestJob.mu.RUnlock()

	var completedAt *int64
	if latestJob.CompletedAt != nil {
		ts := latestJob.CompletedAt.UnixNano()
		completedAt = &ts
	}

	var errMsg string
	if latestJob.Error != nil {
		errMsg = latestJob.Error.Error()
	}

	percentComplete := 0.0
	if latestJob.TotalBytes > 0 {
		percentComplete = float64(latestJob.BytesCompacted) / float64(latestJob.TotalBytes) * 100
	}
	if !latestJob.InProgress && latestJob.Error == nil {
		percentComplete = 100.0
	}

	return &GetCompactionProgressResponse{
		OperationID:     latestJob.OperationID,
		Namespace:       latestJob.Namespace,
		BytesCompacted:  latestJob.BytesCompacted,
		TotalBytes:      latestJob.TotalBytes,
		PercentComplete: percentComplete,
		InProgress:      latestJob.InProgress,
		StartedAt:       latestJob.StartedAt.UnixNano(),
		CompletedAt:     completedAt,
		Error:           errMsg,
	}, nil
}

// validateCompactNamespaceRequest validates the compact namespace request.
func (v *vectorService) validateCompactNamespaceRequest(req *CompactNamespaceRequest) error {
	if req == nil {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateCompactNamespaceRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Namespace == "" {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateCompactNamespaceRequest",
			Err:  &api.RequiredFieldError{Field: "namespace"},
		}
	}
	return nil
}

// validateGetCompactionProgressRequest validates the get compaction progress request.
func (v *vectorService) validateGetCompactionProgressRequest(req *GetCompactionProgressRequest) error {
	if req == nil {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateGetCompactionProgressRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	// Either operation ID or namespace must be provided
	if req.OperationID == "" && req.Namespace == "" {
		return &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.validateGetCompactionProgressRequest",
			Err:  &api.InvalidInputError{Message: "either operation_id or namespace is required"},
		}
	}
	return nil
}

// =============================================================================
// Extended Vector Operations
// =============================================================================

// BatchDeleteVectors removes multiple vectors from the namespace.
func (v *vectorService) BatchDeleteVectors(ctx context.Context, req *BatchDeleteVectorsRequest) (*BatchDeleteVectorsResponse, error) {
	start := time.Now()

	// Validate request
	if req == nil {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.BatchDelete",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Namespace == "" {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.BatchDelete",
			Err:  &api.RequiredFieldError{Field: "namespace"},
		}
	}
	if len(req.IDs) == 0 {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.BatchDelete",
			Err:  &api.EmptyCollectionError{Field: "vector IDs"},
		}
	}

	// Prepare tenant context and scope namespace
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedNamespace := vectorTenantScopedNamespace(tenantID, req.Namespace)

	// Use storage batch delete if available
	err = v.storage.BatchDeleteVectors(ctx, scopedNamespace, req.IDs)
	if err != nil {
		// Fallback to individual deletes
		deletedCount := 0
		var failedIDs []uint64

		for _, id := range req.IDs {
			deleteReq := &DeleteVectorRequest{
				Namespace: req.Namespace,
				ID:        id,
				ShardID:   v.resolveShardID(req.ShardID),
			}

			_, deleteErr := v.DeleteVector(ctx, deleteReq)
			if deleteErr != nil {
				failedIDs = append(failedIDs, id)
			} else {
				deletedCount++
			}
		}

		if v.metrics != nil {
			v.metrics.operationDuration.With("batch_delete").Observe(time.Since(start).Seconds())
		}

		return &BatchDeleteVectorsResponse{
			DeletedCount: deletedCount,
			FailedIDs:    failedIDs,
			Timestamp:    time.Now().UnixNano(),
			Success:      len(failedIDs) == 0,
		}, nil
	}

	if v.metrics != nil {
		v.metrics.operationDuration.With("batch_delete").Observe(time.Since(start).Seconds())
	}

	return &BatchDeleteVectorsResponse{
		DeletedCount: len(req.IDs),
		FailedIDs:    nil,
		Timestamp:    time.Now().UnixNano(),
		Success:      true,
	}, nil
}

// ListVectors returns vectors in a namespace with pagination.
func (v *vectorService) ListVectors(ctx context.Context, req *ListVectorsRequest) (*ListVectorsResponse, error) {
	start := time.Now()

	// Validate request
	if req == nil {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.ListVectors",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Namespace == "" {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.ListVectors",
			Err:  &api.RequiredFieldError{Field: "namespace"},
		}
	}

	// Prepare tenant context and scope namespace
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedNamespace := vectorTenantScopedNamespace(tenantID, req.Namespace)

	// Get namespace info to validate it exists and get vector count
	namespaceInfo, err := v.storage.GetNamespaceInfo(ctx, scopedNamespace)
	if err != nil {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrNotFound,
			Op:   "vector.ListVectors",
			Err:  &api.OperationFailedError{Operation: "find namespace " + req.Namespace, Cause: err},
		}
	}

	// Set default limit
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	// Parse cursor to get offset
	offset := 0
	if req.Cursor != "" {
		if _, parseErr := fmt.Sscanf(req.Cursor, "%d", &offset); parseErr != nil {
			offset = 0
		}
	}

	// List vectors from storage using scan
	scanOpts := &interfaces.VectorScanOptions{
		Limit:  limit + 1, // Request one extra to determine if there's a next page
		Offset: offset,
	}
	vectors, err := v.storage.ScanNamespace(ctx, scopedNamespace, scanOpts)
	if err != nil {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.ListVectors",
			Err:  &api.OperationFailedError{Operation: "list vectors", Cause: err},
		}
	}

	// Determine if there's a next page
	var nextCursor string
	if len(vectors) > limit {
		vectors = vectors[:limit]
		nextCursor = fmt.Sprintf("%d", offset+limit)
	}

	// Convert storage vectors to service vectors
	result := make([]*VectorData, len(vectors))
	for i, vec := range vectors {
		var metadata map[string]interface{}
		if req.IncludeMetadata && vec.Metadata != nil {
			metadata = make(map[string]interface{})
			for k, val := range vec.Metadata {
				metadata[k] = val
			}
		}

		var embeddingData []float32
		if req.IncludeVectors {
			embeddingData = vec.Embedding
		}

		result[i] = &VectorData{
			ID:        vec.ID,
			Vector:    embeddingData,
			Metadata:  metadata,
			Version:   vec.Version,
			CreatedAt: vec.CreatedAt,
			UpdatedAt: vec.UpdatedAt,
		}
	}

	if v.metrics != nil {
		v.metrics.operationDuration.With("list_vectors").Observe(time.Since(start).Seconds())
	}

	return &ListVectorsResponse{
		Vectors:    result,
		NextCursor: nextCursor,
		TotalCount: namespaceInfo.VectorCount,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

// UpdateMetadata updates only the metadata of a vector without modifying the vector values.
func (v *vectorService) UpdateMetadata(ctx context.Context, req *UpdateVectorMetadataRequest) (*UpdateVectorMetadataResponse, error) {
	start := time.Now()

	// Validate request
	if req == nil {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.UpdateMetadata",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Namespace == "" {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.UpdateMetadata",
			Err:  &api.RequiredFieldError{Field: "namespace"},
		}
	}
	if req.ID == 0 {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.UpdateMetadata",
			Err:  &api.RequiredFieldError{Field: "vector ID"},
		}
	}

	// Prepare tenant context and scope namespace
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedNamespace := vectorTenantScopedNamespace(tenantID, req.Namespace)

	// Get existing vector
	existingVector, err := v.storage.GetVector(ctx, scopedNamespace, req.ID)
	if err != nil {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrNotFound,
			Op:   "vector.UpdateMetadata",
			Err:  &api.OperationFailedError{Operation: fmt.Sprintf("find vector %d in namespace %s", req.ID, req.Namespace), Cause: err},
		}
	}

	// Prepare new metadata (convert interface{} to string for storage)
	var newMetadata map[string]interface{}
	if req.MergeMetadata {
		// Merge with existing metadata
		newMetadata = make(map[string]interface{})
		if existingVector.Metadata != nil {
			for k, val := range existingVector.Metadata {
				newMetadata[k] = val
			}
		}
		for k, val := range req.Metadata {
			newMetadata[k] = val
		}
		// Delete specified keys
		for _, key := range req.DeleteKeys {
			delete(newMetadata, key)
		}
	} else {
		// Replace entire metadata
		newMetadata = req.Metadata
	}

	// Update vector using upsert with existing embedding values
	upsertReq := &UpsertVectorRequest{
		Namespace: scopedNamespace,
		ID:        req.ID,
		Vector:    existingVector.Embedding,
		Metadata:  newMetadata,
		ShardID:   v.resolveShardID(req.ShardID),
	}

	resp, err := v.UpsertVector(ctx, upsertReq)
	if err != nil {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInternal,
			Op:   "vector.UpdateMetadata",
			Err:  &api.OperationFailedError{Operation: "update metadata", Cause: err},
		}
	}

	if v.metrics != nil {
		v.metrics.operationDuration.With("update_metadata").Observe(time.Since(start).Seconds())
	}

	return &UpdateVectorMetadataResponse{
		ID:        req.ID,
		Version:   resp.Version,
		Timestamp: resp.Timestamp,
		Success:   true,
	}, nil
}

// SearchByID finds similar vectors using an existing vector's ID as the query.
func (v *vectorService) SearchByID(ctx context.Context, req *SearchByIDRequest) (*SearchByIDResponse, error) {
	start := time.Now()

	// Validate request
	if req == nil {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.SearchByID",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Namespace == "" {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.SearchByID",
			Err:  &api.RequiredFieldError{Field: "namespace"},
		}
	}
	if req.ID == 0 {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.SearchByID",
			Err:  &api.RequiredFieldError{Field: "source vector ID"},
		}
	}
	if req.K <= 0 {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrInvalidInput,
			Op:   "vector.SearchByID",
			Err:  &api.InvalidInputError{Message: "k must be positive"},
		}
	}

	// Prepare tenant context and scope namespace
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedNamespace := vectorTenantScopedNamespace(tenantID, req.Namespace)

	// Get the source vector
	sourceVector, err := v.storage.GetVector(ctx, scopedNamespace, req.ID)
	if err != nil {
		return nil, &qbErrors.QBError{
			Code: qbErrors.ErrNotFound,
			Op:   "vector.SearchByID",
			Err:  &api.OperationFailedError{Operation: fmt.Sprintf("find source vector %d in namespace %s", req.ID, req.Namespace), Cause: err},
		}
	}

	// Use the source vector's embedding as the query
	k := req.K
	if req.ExcludeSelf {
		// Request one extra result to account for potentially excluding self
		k++
	}

	searchReq := &SearchSimilarRequest{
		Namespace:       req.Namespace,
		QueryVector:     sourceVector.Embedding,
		K:               k,
		EfSearch:        req.EfSearch,
		MinScore:        req.MinScore,
		IncludeVectors:  req.IncludeVectors,
		IncludeMetadata: req.IncludeMetadata,
		MetadataFilter:  req.MetadataFilter,
		ShardID:         v.resolveShardID(req.ShardID),
		Consistency:     req.Consistency,
	}

	searchResp, err := v.SearchSimilar(ctx, searchReq)
	if err != nil {
		return nil, err
	}

	// Filter out the source vector if requested
	var results []*VectorSearchResult
	if req.ExcludeSelf {
		for _, result := range searchResp.Results {
			if result.ID != req.ID {
				results = append(results, result)
			}
		}
		// Trim to requested K
		if len(results) > req.K {
			results = results[:req.K]
		}
	} else {
		results = searchResp.Results
	}

	if v.metrics != nil {
		v.metrics.operationDuration.With("search_by_id").Observe(time.Since(start).Seconds())
	}

	return &SearchByIDResponse{
		SourceID:      req.ID,
		Results:       results,
		Timestamp:     time.Now().UnixNano(),
		QueryDuration: searchResp.QueryDuration,
	}, nil
}
