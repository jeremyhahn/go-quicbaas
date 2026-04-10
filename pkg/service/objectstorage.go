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

package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
)

var (
	// objectStorageMetricsOnce ensures metrics are only registered once.
	objectStorageMetricsOnce sync.Once

	// sharedObjectStorageMetrics holds the singleton metrics instance.
	sharedObjectStorageMetrics *objectStorageMetrics
)

// objectStorageService implements ObjectStorageService.
type objectStorageService struct {
	catalogMgr     catalog.CatalogManager
	backendMgr     catalog.BackendManager
	configResolver config.ObjectStoreConfigResolver
	workerMgr      *catalog.WorkerManager
	logger         interfaces.Logger
	metrics        *objectStorageMetrics
	closed         atomic.Bool
	mu             sync.RWMutex
	objectIDGen    atomic.Uint64
}

// objectStorageMetrics holds Prometheus metrics for object storage operations.
type objectStorageMetrics struct {
	objectsPut        prometheus.Counter
	objectsGet        prometheus.Counter
	objectsDeleted    prometheus.Counter
	bytesWritten      prometheus.Counter
	bytesRead         prometheus.Counter
	operationDuration *prometheus.HistogramVec
	operationErrors   *prometheus.CounterVec
}

// NewObjectStorageService creates a new object storage service.
func NewObjectStorageService(
	catalogMgr catalog.CatalogManager,
	backendMgr catalog.BackendManager,
	configResolver config.ObjectStoreConfigResolver,
	workerMgr *catalog.WorkerManager,
	logger interfaces.Logger,
) (ObjectStorageService, error) {
	if catalogMgr == nil {
		return nil, api.NewNilFieldError("service.NewObjectStorageService", "catalog manager")
	}

	if backendMgr == nil {
		return nil, api.NewNilFieldError("service.NewObjectStorageService", "backend manager")
	}

	if configResolver == nil {
		return nil, api.NewNilFieldError("service.NewObjectStorageService", "config resolver")
	}

	if logger == nil {
		return nil, api.NewNilFieldError("service.NewObjectStorageService", "logger")
	}

	svc := &objectStorageService{
		catalogMgr:     catalogMgr,
		backendMgr:     backendMgr,
		configResolver: configResolver,
		workerMgr:      workerMgr,
		logger:         logger.WithField("component", "objectstorage_service"),
	}

	// Initialize metrics (only once across all instances)
	svc.initMetrics()

	// Set initial object ID to current timestamp for uniqueness
	svc.objectIDGen.Store(uint64(time.Now().UnixNano())) // #nosec G115 -- UnixNano returns positive timestamp

	svc.logger.Info("object storage service initialized")

	return svc, nil
}

// initMetrics initializes Prometheus metrics for the object storage service.
// Uses sync.Once to ensure metrics are only registered once.
func (s *objectStorageService) initMetrics() {
	objectStorageMetricsOnce.Do(func() {
		sharedObjectStorageMetrics = &objectStorageMetrics{
			objectsPut: promauto.With(metrics.Registry).NewCounter(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "objectstorage",
				Name:      "objects_put_total",
				Help:      "Total number of objects stored",
			}),

			objectsGet: promauto.With(metrics.Registry).NewCounter(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "objectstorage",
				Name:      "objects_get_total",
				Help:      "Total number of objects retrieved",
			}),

			objectsDeleted: promauto.With(metrics.Registry).NewCounter(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "objectstorage",
				Name:      "objects_deleted_total",
				Help:      "Total number of objects deleted",
			}),

			bytesWritten: promauto.With(metrics.Registry).NewCounter(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "objectstorage",
				Name:      "bytes_written_total",
				Help:      "Total bytes written",
			}),

			bytesRead: promauto.With(metrics.Registry).NewCounter(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "objectstorage",
				Name:      "bytes_read_total",
				Help:      "Total bytes read",
			}),

			operationDuration: promauto.With(metrics.Registry).NewHistogramVec(prometheus.HistogramOpts{
				Namespace: "quicbaas",
				Subsystem: "objectstorage",
				Name:      "operation_duration_seconds",
				Help:      "Duration of object storage operations in seconds",
				Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
			}, []string{"operation"}),

			operationErrors: promauto.With(metrics.Registry).NewCounterVec(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "objectstorage",
				Name:      "operation_errors_total",
				Help:      "Total errors by operation type",
			}, []string{"operation", "error_type"}),
		}
	})

	// Assign the shared metrics to this instance
	s.metrics = sharedObjectStorageMetrics
}

// generateObjectRef generates a unique object reference for storage.
func (s *objectStorageService) generateObjectRef(key string) string {
	// Combine key with timestamp and object ID for uniqueness
	objectID := s.objectIDGen.Add(1)
	combined := fmt.Sprintf("%s:%d:%d", key, time.Now().UnixNano(), objectID)
	hash := sha256.Sum256([]byte(combined))
	return hex.EncodeToString(hash[:])
}

// checkClosed returns an error if the service is closed.
func (s *objectStorageService) checkClosed() error {
	if s.closed.Load() {
		return api.NewServiceClosedError("objectstorage.checkClosed", "object storage service")
	}
	return nil
}

// =============================================================================
// Object CRUD Operations
// =============================================================================

// PutObject stores an object with specified metadata and policies.
func (s *objectStorageService) PutObject(ctx context.Context, req *PutObjectRequest) (*PutObjectResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("put_object").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("put_object", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	s.logger.WithFields(map[string]interface{}{
		"key":     req.Key,
		"size":    len(req.Value),
		"tenant":  req.TenantID,
		"backend": req.TargetBackend,
	}).Debug("putting object")

	// Resolve configuration for tenant/shard
	cfg, err := s.configResolver.ResolveConfig(req.TenantID, req.ShardID)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("put_object", "config_error").Inc()
		}
		return nil, api.NewOperationFailedError("resolve config", err)
	}

	// Check quotas if tenant is specified
	if req.TenantID != "" {
		usage, _ := s.catalogMgr.GetTenantUsage(ctx, req.TenantID)
		if usage == nil {
			usage = &catalog.TenantUsage{TenantID: req.TenantID}
		}
		tenantUsage := &config.TenantUsage{
			ObjectCount: usage.ObjectCount,
			TotalBytes:  usage.TotalSize,
		}
		if err := s.configResolver.CheckQuotas(req.TenantID, int64(len(req.Value)), tenantUsage); err != nil {
			if s.metrics != nil && s.metrics.operationErrors != nil {
				s.metrics.operationErrors.WithLabelValues("put_object", "quota_exceeded").Inc()
			}
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrResourceExhausted,
				Op:   "objectstorage.PutObject",
				Err:  err,
			}
		}
	}

	// Select backend
	backendName := req.TargetBackend
	if backendName == "" {
		backendName = cfg.PrimaryBackend
	}

	backend, err := s.backendMgr.GetBackend(backendName)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("put_object", "backend_not_found").Inc()
		}
		return nil, api.NewBackendUnavailableError("objectstorage.PutObject", err)
	}

	// Compute checksum
	checksum := sha256.Sum256(req.Value)
	if req.Checksum != nil {
		// Verify client-provided checksum matches
		if !bytes.Equal(checksum[:], req.Checksum) {
			if s.metrics != nil && s.metrics.operationErrors != nil {
				s.metrics.operationErrors.WithLabelValues("put_object", "checksum_mismatch").Inc()
			}
			return nil, api.NewChecksumMismatchError("objectstorage.PutObject")
		}
	}

	// Generate object reference for backend storage
	objectRef := s.generateObjectRef(req.Key)

	// Write to backend
	if err := backend.PutWithContext(ctx, objectRef, bytes.NewReader(req.Value)); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("put_object", "storage_error").Inc()
		}
		return nil, api.NewStorageOperationError("objectstorage.PutObject", "write to backend", err)
	}

	// Generate object ID
	objectID := s.objectIDGen.Add(1)
	now := time.Now().UnixNano()

	// Create catalog entry
	entry := catalog.NewCatalogEntry(req.Key, int64(len(req.Value)), checksum[:])
	entry.ObjectID = objectID
	entry.ContentType = req.ContentType
	entry.TenantID = req.TenantID
	entry.ShardID = req.ShardID
	entry.Tags = req.Tags
	entry.PrimaryStore = catalog.StoreLocation{
		Store:      backendName,
		ObjectRef:  objectRef,
		StoredSize: int64(len(req.Value)),
		Status:     catalog.LocationStatusActive,
		SyncedAt:   now,
	}

	// Apply policy overrides
	if req.ReplicationPolicy != nil {
		entry.ReplicationPolicy = req.ReplicationPolicy
	} else if cfg.Replication != nil && cfg.Replication.Enabled {
		entry.ReplicationPolicy = &catalog.ReplicationPolicy{
			Enabled:      cfg.Replication.Enabled,
			MinReplicas:  cfg.Replication.MinReplicas,
			TargetStores: cfg.Replication.TargetStores,
			Mode:         catalog.ReplicationMode(cfg.Replication.Mode),
			Priority:     cfg.Replication.Priority,
		}
	}

	if req.ArchivePolicy != nil {
		entry.ArchivePolicy = req.ArchivePolicy
	}

	if req.RetentionPolicy != nil {
		entry.RetentionPolicy = req.RetentionPolicy
	}

	// Store catalog entry
	if err := s.catalogMgr.Put(ctx, entry); err != nil {
		// Attempt to clean up the backend object
		if delErr := backend.DeleteWithContext(ctx, objectRef); delErr != nil {
			s.logger.WithError(delErr).WithField("object_ref", objectRef).Warn("failed to clean up backend object after catalog error")
		}
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("put_object", "catalog_error").Inc()
		}
		return nil, api.NewStorageOperationError("objectstorage.PutObject", "store catalog entry", err)
	}

	// Enqueue async replication if configured
	if entry.ReplicationPolicy != nil && entry.ReplicationPolicy.Enabled {
		if entry.ReplicationPolicy.Mode == "async" && len(entry.ReplicationPolicy.TargetStores) > 0 {
			if enqErr := s.catalogMgr.EnqueueReplication(ctx, req.Key, entry.ReplicationPolicy.TargetStores, entry.ReplicationPolicy.Priority); enqErr != nil {
				s.logger.WithError(enqErr).WithField("key", req.Key).Warn("failed to enqueue async replication")
			}
		}
	}

	// Update metrics
	if s.metrics != nil {
		if s.metrics.objectsPut != nil {
			s.metrics.objectsPut.Inc()
		}
		if s.metrics.bytesWritten != nil {
			s.metrics.bytesWritten.Add(float64(len(req.Value)))
		}
	}

	s.logger.WithFields(map[string]interface{}{
		"key":       req.Key,
		"object_id": objectID,
		"size":      len(req.Value),
		"backend":   backendName,
	}).Info("object stored successfully")

	return &PutObjectResponse{
		Key:       req.Key,
		ObjectID:  objectID,
		Checksum:  checksum[:],
		Size:      int64(len(req.Value)),
		Backend:   backendName,
		Version:   entry.Version,
		CreatedAt: now,
	}, nil
}

// GetObject retrieves an object by key.
func (s *objectStorageService) GetObject(ctx context.Context, req *GetObjectRequest) (*GetObjectResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("get_object").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("get_object", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get catalog entry
	entry, err := s.catalogMgr.Get(ctx, req.Key)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("get_object", "not_found").Inc()
		}
		return nil, api.NewNotFoundWithCauseError("objectstorage.GetObject", "object", err)
	}

	// Validate tenant access if specified
	if req.TenantID != "" && entry.TenantID != "" && entry.TenantID != req.TenantID {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("get_object", "forbidden").Inc()
		}
		return nil, api.NewCrossTenantAccessError("objectstorage.GetObject")
	}

	// Check if object is active
	if entry.Status != catalog.StatusActive && entry.Status != catalog.StatusArchived {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("get_object", "not_available").Inc()
		}
		return nil, api.NewObjectNotAvailableError("objectstorage.GetObject", string(entry.Status))
	}

	// Get primary backend
	backend, err := s.backendMgr.GetBackend(entry.PrimaryStore.Store)
	if err != nil {
		// Try replicas if primary is unavailable
		for _, replica := range entry.Replicas {
			if replica.Status == catalog.LocationStatusActive {
				backend, err = s.backendMgr.GetBackend(replica.Store)
				if err == nil {
					break
				}
			}
		}
		if err != nil {
			if s.metrics != nil && s.metrics.operationErrors != nil {
				s.metrics.operationErrors.WithLabelValues("get_object", "backend_unavailable").Inc()
			}
			return nil, api.NewNoAvailableBackendError("objectstorage.GetObject", err)
		}
	}

	// Read from backend
	reader, err := backend.GetWithContext(ctx, entry.PrimaryStore.ObjectRef)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("get_object", "storage_error").Inc()
		}
		return nil, api.NewStorageOperationError("objectstorage.GetObject", "read from backend", err)
	}
	defer reader.Close()

	// Read data
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(reader); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("get_object", "read_error").Inc()
		}
		return nil, api.NewStorageOperationError("objectstorage.GetObject", "read object data", err)
	}

	// Update access time (async)
	go func() {
		if accessErr := s.catalogMgr.UpdateAccessTime(context.WithoutCancel(ctx), req.Key); accessErr != nil {
			s.logger.WithError(accessErr).WithField("key", req.Key).Warn("failed to update access time")
		}
	}()

	// Update metrics
	if s.metrics != nil {
		if s.metrics.objectsGet != nil {
			s.metrics.objectsGet.Inc()
		}
		if s.metrics.bytesRead != nil {
			s.metrics.bytesRead.Add(float64(buf.Len()))
		}
	}

	resp := &GetObjectResponse{
		Key:         entry.Key,
		Value:       buf.Bytes(),
		ContentType: entry.ContentType,
		Size:        entry.Size,
		Checksum:    entry.Checksum,
		Version:     entry.Version,
		TenantID:    entry.TenantID,
		CreatedAt:   entry.CreatedAt,
		ModifiedAt:  entry.ModifiedAt,
		Backend:     entry.PrimaryStore.Store,
	}

	if req.IncludeMetadata {
		resp.Tags = entry.Tags
	}

	return resp, nil
}

// DeleteObject removes an object from storage.
func (s *objectStorageService) DeleteObject(ctx context.Context, req *DeleteObjectRequest) (*DeleteObjectResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("delete_object").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("delete_object", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Get catalog entry
	entry, err := s.catalogMgr.Get(ctx, req.Key)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("delete_object", "not_found").Inc()
		}
		return nil, api.NewNotFoundWithCauseError("objectstorage.DeleteObject", "object", err)
	}

	// Validate tenant access if specified
	if req.TenantID != "" && entry.TenantID != "" && entry.TenantID != req.TenantID {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("delete_object", "forbidden").Inc()
		}
		return nil, api.NewCrossTenantAccessError("objectstorage.DeleteObject")
	}

	size := entry.Size

	// Delete from all backends
	allLocations := entry.GetAllLocations()
	for _, loc := range allLocations {
		backend, err := s.backendMgr.GetBackend(loc.Store)
		if err != nil {
			s.logger.WithFields(map[string]interface{}{
				"key":   req.Key,
				"store": loc.Store,
				"error": err.Error(),
			}).Warn("failed to get backend for deletion")
			continue
		}

		if err := backend.DeleteWithContext(ctx, loc.ObjectRef); err != nil {
			s.logger.WithFields(map[string]interface{}{
				"key":   req.Key,
				"store": loc.Store,
				"error": err.Error(),
			}).Warn("failed to delete object from backend")
		}
	}

	// Delete catalog entry
	if err := s.catalogMgr.Delete(ctx, req.Key); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("delete_object", "catalog_error").Inc()
		}
		return nil, api.NewStorageOperationError("objectstorage.DeleteObject", "delete catalog entry", err)
	}

	// Update metrics
	if s.metrics != nil && s.metrics.objectsDeleted != nil {
		s.metrics.objectsDeleted.Inc()
	}

	s.logger.WithFields(map[string]interface{}{
		"key":  req.Key,
		"size": size,
	}).Info("object deleted successfully")

	return &DeleteObjectResponse{
		Key:       req.Key,
		Success:   true,
		Size:      size,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// HeadObject retrieves object metadata without the data.
func (s *objectStorageService) HeadObject(ctx context.Context, req *HeadObjectRequest) (*HeadObjectResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("head_object").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("head_object", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get catalog entry
	entry, err := s.catalogMgr.Get(ctx, req.Key)
	if err != nil {
		// Return not found response instead of error for HEAD
		return &HeadObjectResponse{
			Key:    req.Key,
			Exists: false,
		}, nil
	}

	// Validate tenant access if specified
	if req.TenantID != "" && entry.TenantID != "" && entry.TenantID != req.TenantID {
		return &HeadObjectResponse{
			Key:    req.Key,
			Exists: false,
		}, nil
	}

	return &HeadObjectResponse{
		Key:          entry.Key,
		Exists:       true,
		ContentType:  entry.ContentType,
		Size:         entry.Size,
		Checksum:     entry.Checksum,
		Version:      entry.Version,
		Tags:         entry.Tags,
		TenantID:     entry.TenantID,
		Status:       string(entry.Status),
		CreatedAt:    entry.CreatedAt,
		ModifiedAt:   entry.ModifiedAt,
		LastAccessAt: entry.LastAccessAt,
		Backend:      entry.PrimaryStore.Store,
		ReplicaCount: len(entry.Replicas),
	}, nil
}

// =============================================================================
// Listing Operations
// =============================================================================

// ListObjects lists objects matching the specified criteria.
func (s *objectStorageService) ListObjects(ctx context.Context, req *ListObjectsRequest) (*ListObjectsResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("list_objects").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("list_objects", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	opts := &catalog.ListOptions{
		Limit:  limit,
		Cursor: req.Cursor,
	}

	result, err := s.catalogMgr.ListByPrefix(ctx, req.Prefix, opts)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("list_objects", "storage_error").Inc()
		}
		return nil, api.NewStorageOperationError("objectstorage.ListObjects", "list objects", err)
	}

	// Convert to ObjectSummary
	objects := make([]*ObjectSummary, 0, len(result.Keys))
	for _, key := range result.Keys {
		entry, err := s.catalogMgr.Get(ctx, key)
		if err != nil {
			continue
		}

		// Filter by tenant if specified
		if req.TenantID != "" && entry.TenantID != "" && entry.TenantID != req.TenantID {
			continue
		}

		summary := &ObjectSummary{
			Key:         entry.Key,
			Size:        entry.Size,
			ContentType: entry.ContentType,
			Version:     entry.Version,
			Status:      string(entry.Status),
			CreatedAt:   entry.CreatedAt,
			ModifiedAt:  entry.ModifiedAt,
		}

		if req.IncludeTags {
			summary.Tags = entry.Tags
		}

		objects = append(objects, summary)
	}

	return &ListObjectsResponse{
		Objects:    objects,
		NextCursor: result.NextCursor,
		HasMore:    result.HasMore,
		TotalCount: result.Total,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

// ListByTag lists objects with a specific tag.
func (s *objectStorageService) ListByTag(ctx context.Context, req *ListByTagRequest) (*ListByTagResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("list_by_tag").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("list_by_tag", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	opts := &catalog.ListOptions{
		Limit:  limit,
		Cursor: req.Cursor,
	}

	result, err := s.catalogMgr.ListByTag(ctx, req.TagName, req.TagValue, opts)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("list_by_tag", "storage_error").Inc()
		}
		return nil, api.NewStorageOperationError("objectstorage.ListByTag", "list objects by tag", err)
	}

	// Convert to ObjectSummary
	objects := make([]*ObjectSummary, 0, len(result.Keys))
	for _, key := range result.Keys {
		entry, err := s.catalogMgr.Get(ctx, key)
		if err != nil {
			continue
		}

		// Filter by tenant if specified
		if req.TenantID != "" && entry.TenantID != "" && entry.TenantID != req.TenantID {
			continue
		}

		objects = append(objects, &ObjectSummary{
			Key:         entry.Key,
			Size:        entry.Size,
			ContentType: entry.ContentType,
			Version:     entry.Version,
			Status:      string(entry.Status),
			CreatedAt:   entry.CreatedAt,
			ModifiedAt:  entry.ModifiedAt,
		})
	}

	return &ListByTagResponse{
		Objects:    objects,
		NextCursor: result.NextCursor,
		HasMore:    result.HasMore,
		TotalCount: result.Total,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

// ListByTenant lists objects owned by a specific tenant.
func (s *objectStorageService) ListByTenant(ctx context.Context, req *ListByTenantRequest) (*ListByTenantResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("list_by_tenant").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("list_by_tenant", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	opts := &catalog.ListOptions{
		Limit:  limit,
		Cursor: req.Cursor,
	}

	result, err := s.catalogMgr.ListByTenant(ctx, req.TenantID, opts)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("list_by_tenant", "storage_error").Inc()
		}
		return nil, api.NewStorageOperationError("objectstorage.ListByTenant", "list objects by tenant", err)
	}

	// Convert to ObjectSummary
	objects := make([]*ObjectSummary, 0, len(result.Keys))
	for _, key := range result.Keys {
		entry, err := s.catalogMgr.Get(ctx, key)
		if err != nil {
			continue
		}

		summary := &ObjectSummary{
			Key:         entry.Key,
			Size:        entry.Size,
			ContentType: entry.ContentType,
			Version:     entry.Version,
			Status:      string(entry.Status),
			CreatedAt:   entry.CreatedAt,
			ModifiedAt:  entry.ModifiedAt,
		}

		if req.IncludeTags {
			summary.Tags = entry.Tags
		}

		objects = append(objects, summary)
	}

	return &ListByTenantResponse{
		Objects:    objects,
		NextCursor: result.NextCursor,
		HasMore:    result.HasMore,
		TotalCount: result.Total,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

// =============================================================================
// Copy Operations
// =============================================================================

// CopyObject copies an object to a new location.
func (s *objectStorageService) CopyObject(ctx context.Context, req *CopyObjectRequest) (*CopyObjectResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("copy_object").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("copy_object", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	// Get source object
	getReq := &GetObjectRequest{
		Key:             req.SourceKey,
		IncludeMetadata: true,
	}
	sourceObj, err := s.GetObject(ctx, getReq)
	if err != nil {
		return nil, err
	}

	// Check if destination exists
	exists, _ := s.catalogMgr.Exists(ctx, req.DestinationKey)
	if exists && !req.OverwriteExisting {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("copy_object", "already_exists").Inc()
		}
		return nil, api.NewDestinationExistsError("objectstorage.CopyObject")
	}

	// Put to destination
	putReq := &PutObjectRequest{
		Key:           req.DestinationKey,
		Value:         sourceObj.Value,
		ContentType:   sourceObj.ContentType,
		TenantID:      req.TenantID,
		Tags:          sourceObj.Tags,
		TargetBackend: req.TargetBackend,
	}

	if req.TenantID == "" {
		putReq.TenantID = sourceObj.TenantID
	}

	putResp, err := s.PutObject(ctx, putReq)
	if err != nil {
		return nil, err
	}

	return &CopyObjectResponse{
		Key:       putResp.Key,
		ObjectID:  putResp.ObjectID,
		Size:      putResp.Size,
		Checksum:  putResp.Checksum,
		Version:   putResp.Version,
		Backend:   putResp.Backend,
		CreatedAt: putResp.CreatedAt,
	}, nil
}

// =============================================================================
// Replication & Migration Operations
// =============================================================================

// ReplicateObject replicates an object to target backends.
func (s *objectStorageService) ReplicateObject(ctx context.Context, req *ReplicateObjectRequest) (*ReplicateObjectResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("replicate_object").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("replicate_object", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Get catalog entry
	entry, err := s.catalogMgr.Get(ctx, req.Key)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("replicate_object", "not_found").Inc()
		}
		return nil, api.NewNotFoundWithCauseError("objectstorage.ReplicateObject", "object", err)
	}

	// Validate tenant access
	if req.TenantID != "" && entry.TenantID != "" && entry.TenantID != req.TenantID {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("replicate_object", "forbidden").Inc()
		}
		return nil, api.NewCrossTenantAccessError("objectstorage.ReplicateObject")
	}

	// Get source backend
	srcBackend, err := s.backendMgr.GetBackend(entry.PrimaryStore.Store)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("replicate_object", "source_unavailable").Inc()
		}
		return nil, api.NewBackendUnavailableError("objectstorage.ReplicateObject", err)
	}

	// Read object data
	reader, err := srcBackend.GetWithContext(ctx, entry.PrimaryStore.ObjectRef)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("replicate_object", "read_error").Inc()
		}
		return nil, api.NewStorageOperationError("objectstorage.ReplicateObject", "read source object", err)
	}
	var buf bytes.Buffer
	if _, readErr := buf.ReadFrom(reader); readErr != nil {
		s.logger.WithError(readErr).Warn("failed to read object data during replication")
	}
	if closeErr := reader.Close(); closeErr != nil {
		s.logger.WithError(closeErr).Warn("failed to close reader during replication")
	}

	// Replicate to each target
	results := make(map[string]*ReplicationResult)
	allSuccess := true

	for _, target := range req.Targets {
		result := &ReplicationResult{
			Target: target,
		}
		replicationStart := time.Now()

		// Skip if already replicated
		if entry.HasLocation(target) {
			result.Success = true
			result.Duration = time.Since(replicationStart).Milliseconds()
			results[target] = result
			continue
		}

		// Get target backend
		targetBackend, err := s.backendMgr.GetBackend(target)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			result.Duration = time.Since(replicationStart).Milliseconds()
			results[target] = result
			allSuccess = false
			continue
		}

		// Generate new object ref for replica
		replicaRef := s.generateObjectRef(req.Key + ":" + target)

		// Write to target
		if err := targetBackend.PutWithContext(ctx, replicaRef, bytes.NewReader(buf.Bytes())); err != nil {
			result.Success = false
			result.Error = err.Error()
			result.Duration = time.Since(replicationStart).Milliseconds()
			results[target] = result
			allSuccess = false
			continue
		}

		// Add replica location to catalog
		loc := &catalog.StoreLocation{
			Store:      target,
			ObjectRef:  replicaRef,
			StoredSize: int64(buf.Len()),
			Status:     catalog.LocationStatusActive,
			SyncedAt:   time.Now().UnixNano(),
		}

		if err := s.catalogMgr.AddLocation(ctx, req.Key, loc); err != nil {
			// Best effort - location added but catalog update failed
			s.logger.WithFields(map[string]interface{}{
				"key":    req.Key,
				"target": target,
				"error":  err.Error(),
			}).Warn("failed to add replica location to catalog")
		}

		result.Success = true
		result.Duration = time.Since(replicationStart).Milliseconds()
		results[target] = result
	}

	status := "completed"
	if !allSuccess {
		status = "partial"
	}

	return &ReplicateObjectResponse{
		Key:       req.Key,
		Status:    status,
		Results:   results,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// MigrateObject migrates an object to a new backend.
func (s *objectStorageService) MigrateObject(ctx context.Context, req *MigrateObjectRequest) (*MigrateObjectResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("migrate_object").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("migrate_object", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	// Get catalog entry
	entry, err := s.catalogMgr.Get(ctx, req.Key)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("migrate_object", "not_found").Inc()
		}
		return nil, api.NewNotFoundWithCauseError("objectstorage.MigrateObject", "object", err)
	}

	sourceBackend := entry.PrimaryStore.Store

	// Replicate to target first
	replicateReq := &ReplicateObjectRequest{
		Key:         req.Key,
		Targets:     []string{req.TargetBackend},
		TenantID:    req.TenantID,
		Synchronous: true,
	}

	_, err = s.ReplicateObject(ctx, replicateReq)
	if err != nil {
		return nil, err
	}

	// Set target as primary
	if err := s.catalogMgr.SetPrimaryStore(ctx, req.Key, req.TargetBackend); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("migrate_object", "set_primary_error").Inc()
		}
		return nil, api.NewStorageOperationError("objectstorage.MigrateObject", "set new primary store", err)
	}

	deletedFromSource := false

	// Delete from source if requested
	if req.DeleteFromSource {
		if err := s.catalogMgr.RemoveLocation(ctx, req.Key, sourceBackend); err != nil {
			s.logger.WithFields(map[string]interface{}{
				"key":    req.Key,
				"source": sourceBackend,
				"error":  err.Error(),
			}).Warn("failed to remove source location from catalog")
		}

		// Delete from source backend
		srcBackend, err := s.backendMgr.GetBackend(sourceBackend)
		if err == nil {
			if err := srcBackend.DeleteWithContext(ctx, entry.PrimaryStore.ObjectRef); err != nil {
				s.logger.WithFields(map[string]interface{}{
					"key":    req.Key,
					"source": sourceBackend,
					"error":  err.Error(),
				}).Warn("failed to delete object from source backend")
			} else {
				deletedFromSource = true
			}
		}
	}

	return &MigrateObjectResponse{
		Key:               req.Key,
		SourceBackend:     sourceBackend,
		TargetBackend:     req.TargetBackend,
		Success:           true,
		DeletedFromSource: deletedFromSource,
		Timestamp:         time.Now().UnixNano(),
	}, nil
}

// =============================================================================
// Archive & Restore Operations
// =============================================================================

// ArchiveObject moves an object to archive storage.
func (s *objectStorageService) ArchiveObject(ctx context.Context, req *ArchiveObjectRequest) (*ArchiveObjectResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("archive_object").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("archive_object", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	// Get catalog entry
	entry, err := s.catalogMgr.Get(ctx, req.Key)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("archive_object", "not_found").Inc()
		}
		return nil, api.NewNotFoundWithCauseError("objectstorage.ArchiveObject", "object", err)
	}

	// Check if already archived
	if entry.Status == catalog.StatusArchived {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("archive_object", "already_archived").Inc()
		}
		return nil, api.NewAlreadyArchivedError("objectstorage.ArchiveObject")
	}

	// Validate tenant access
	if req.TenantID != "" && entry.TenantID != "" && entry.TenantID != req.TenantID {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("archive_object", "forbidden").Inc()
		}
		return nil, api.NewCrossTenantAccessError("objectstorage.ArchiveObject")
	}

	// Determine archive target
	archiveStore := req.TargetStore
	if archiveStore == "" {
		cfg, _ := s.configResolver.ResolveConfig(entry.TenantID, entry.ShardID)
		if cfg != nil && cfg.Archive != nil && cfg.Archive.TargetStore != "" {
			archiveStore = cfg.Archive.TargetStore
		}
	}

	if archiveStore == "" {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("archive_object", "no_archive_store").Inc()
		}
		return nil, api.NewNotConfiguredError("objectstorage.ArchiveObject", "archive store")
	}

	// Replicate to archive store
	replicateReq := &ReplicateObjectRequest{
		Key:         req.Key,
		Targets:     []string{archiveStore},
		TenantID:    req.TenantID,
		Synchronous: true,
	}

	_, err = s.ReplicateObject(ctx, replicateReq)
	if err != nil {
		return nil, err
	}

	// Get the archive location
	entry, catalogErr := s.catalogMgr.Get(ctx, req.Key)
	if catalogErr != nil {
		s.logger.WithError(catalogErr).WithField("key", req.Key).Warn("failed to refresh catalog entry after archive replication")
	}
	var archiveLoc *catalog.StoreLocation
	for i := range entry.Replicas {
		if entry.Replicas[i].Store == archiveStore {
			archiveLoc = &entry.Replicas[i]
			break
		}
	}

	// Update entry status and set archive store
	err = s.catalogMgr.Update(ctx, req.Key, func(e *catalog.CatalogEntry) error {
		if archiveLoc != nil {
			e.ArchiveStore = archiveLoc.Clone()
		}
		e.MarkArchived()
		return nil
	})

	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("archive_object", "update_error").Inc()
		}
		return nil, api.NewStorageOperationError("objectstorage.ArchiveObject", "update archive status", err)
	}

	deletedFromPrimary := false

	// Delete from primary if requested
	if req.DeleteFromPrimary {
		primaryStore := entry.PrimaryStore.Store
		primaryBackend, err := s.backendMgr.GetBackend(primaryStore)
		if err == nil {
			if err := primaryBackend.DeleteWithContext(ctx, entry.PrimaryStore.ObjectRef); err != nil {
				s.logger.WithFields(map[string]interface{}{
					"key":     req.Key,
					"primary": primaryStore,
					"error":   err.Error(),
				}).Warn("failed to delete object from primary")
			} else {
				deletedFromPrimary = true
			}
		}
	}

	return &ArchiveObjectResponse{
		Key:                req.Key,
		ArchiveStore:       archiveStore,
		Success:            true,
		DeletedFromPrimary: deletedFromPrimary,
		ArchivedAt:         time.Now().UnixNano(),
	}, nil
}

// RestoreObject restores an archived object to active storage.
func (s *objectStorageService) RestoreObject(ctx context.Context, req *RestoreObjectRequest) (*RestoreObjectResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("restore_object").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("restore_object", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	// Get catalog entry
	entry, err := s.catalogMgr.Get(ctx, req.Key)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("restore_object", "not_found").Inc()
		}
		return nil, api.NewNotFoundWithCauseError("objectstorage.RestoreObject", "object", err)
	}

	// Check if object is archived
	if entry.Status != catalog.StatusArchived {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("restore_object", "not_archived").Inc()
		}
		return nil, api.NewNotArchivedError("objectstorage.RestoreObject")
	}

	// Validate tenant access
	if req.TenantID != "" && entry.TenantID != "" && entry.TenantID != req.TenantID {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("restore_object", "forbidden").Inc()
		}
		return nil, api.NewCrossTenantAccessError("objectstorage.RestoreObject")
	}

	// Determine target backend
	targetBackend := req.TargetBackend
	if targetBackend == "" {
		cfg, _ := s.configResolver.ResolveConfig(entry.TenantID, entry.ShardID)
		if cfg != nil {
			targetBackend = cfg.PrimaryBackend
		}
	}

	if targetBackend == "" {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("restore_object", "no_target").Inc()
		}
		return nil, api.NewNoTargetSpecifiedError("objectstorage.RestoreObject", "target backend")
	}

	// Replicate from archive to target
	replicateReq := &ReplicateObjectRequest{
		Key:         req.Key,
		Targets:     []string{targetBackend},
		TenantID:    req.TenantID,
		Synchronous: true,
	}

	_, err = s.ReplicateObject(ctx, replicateReq)
	if err != nil {
		return nil, err
	}

	// Set restored location as primary
	if err := s.catalogMgr.SetPrimaryStore(ctx, req.Key, targetBackend); err != nil {
		s.logger.WithFields(map[string]interface{}{
			"key":    req.Key,
			"target": targetBackend,
			"error":  err.Error(),
		}).Warn("failed to set primary store after restore")
	}

	// Update status to active
	err = s.catalogMgr.Update(ctx, req.Key, func(e *catalog.CatalogEntry) error {
		e.Status = catalog.StatusActive
		e.ModifiedAt = time.Now().UnixNano()
		e.Version++
		return nil
	})

	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("restore_object", "update_error").Inc()
		}
		return nil, api.NewStorageOperationError("objectstorage.RestoreObject", "update status", err)
	}

	return &RestoreObjectResponse{
		Key:        req.Key,
		Backend:    targetBackend,
		Success:    true,
		RestoredAt: time.Now().UnixNano(),
	}, nil
}

// =============================================================================
// Verification Operations
// =============================================================================

// VerifyObject verifies object integrity across storage backends.
func (s *objectStorageService) VerifyObject(ctx context.Context, req *VerifyObjectRequest) (*VerifyObjectResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("verify_object").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("verify_object", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get catalog entry
	entry, err := s.catalogMgr.Get(ctx, req.Key)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("verify_object", "not_found").Inc()
		}
		return nil, api.NewNotFoundWithCauseError("objectstorage.VerifyObject", "object", err)
	}

	// Validate tenant access
	if req.TenantID != "" && entry.TenantID != "" && entry.TenantID != req.TenantID {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("verify_object", "forbidden").Inc()
		}
		return nil, api.NewCrossTenantAccessError("objectstorage.VerifyObject")
	}

	// Determine stores to verify
	storesToVerify := req.Stores
	if len(storesToVerify) == 0 {
		// Verify all locations
		locations := entry.GetAllLocations()
		storesToVerify = make([]string, len(locations))
		for i, loc := range locations {
			storesToVerify[i] = loc.Store
		}
	}

	results := make(map[string]*VerificationResult)
	allValid := true

	for _, store := range storesToVerify {
		result := &VerificationResult{
			Store: store,
		}

		// Get location for this store
		loc := entry.GetLocation(store)
		if loc == nil {
			result.Valid = false
			result.Error = "location not found in catalog"
			results[store] = result
			allValid = false
			continue
		}

		// Get backend
		backend, err := s.backendMgr.GetBackend(store)
		if err != nil {
			result.Valid = false
			result.Error = fmt.Sprintf("backend unavailable: %v", err)
			results[store] = result
			allValid = false
			continue
		}

		// Read object and verify
		reader, err := backend.GetWithContext(ctx, loc.ObjectRef)
		if err != nil {
			result.Valid = false
			result.Error = fmt.Sprintf("failed to read object: %v", err)
			results[store] = result
			allValid = false
			continue
		}

		var buf bytes.Buffer
		_, err = buf.ReadFrom(reader)
		if closeErr := reader.Close(); closeErr != nil {
			s.logger.WithError(closeErr).Warn("failed to close reader during integrity check")
		}

		if err != nil {
			result.Valid = false
			result.Error = fmt.Sprintf("failed to read object data: %v", err)
			results[store] = result
			allValid = false
			continue
		}

		// Verify size
		result.SizeMatch = int64(buf.Len()) == entry.Size

		// Verify checksum
		computedChecksum := sha256.Sum256(buf.Bytes())
		result.ChecksumMatch = bytes.Equal(computedChecksum[:], entry.Checksum)

		result.Valid = result.SizeMatch && result.ChecksumMatch

		if !result.Valid {
			allValid = false
			if !result.SizeMatch {
				result.Error = fmt.Sprintf("size mismatch: expected %d, got %d", entry.Size, buf.Len())
			} else {
				result.Error = "checksum mismatch"
			}
		}

		results[store] = result
	}

	return &VerifyObjectResponse{
		Key:       req.Key,
		Valid:     allValid,
		Results:   results,
		Repaired:  false,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// =============================================================================
// Policy Management Operations
// =============================================================================

// SetObjectPolicy sets policies for an object.
func (s *objectStorageService) SetObjectPolicy(ctx context.Context, req *SetObjectPolicyRequest) (*SetObjectPolicyResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("set_object_policy").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("set_object_policy", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	var newVersion uint64

	err := s.catalogMgr.Update(ctx, req.Key, func(entry *catalog.CatalogEntry) error {
		// Validate tenant access
		if req.TenantID != "" && entry.TenantID != "" && entry.TenantID != req.TenantID {
			return api.NewCrossTenantAccessError("objectstorage.SetObjectPolicy")
		}

		if req.ReplicationPolicy != nil {
			entry.ReplicationPolicy = req.ReplicationPolicy
		}
		if req.ArchivePolicy != nil {
			entry.ArchivePolicy = req.ArchivePolicy
		}
		if req.RetentionPolicy != nil {
			entry.RetentionPolicy = req.RetentionPolicy
		}

		newVersion = entry.Version + 1
		return nil
	})

	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("set_object_policy", "update_error").Inc()
		}
		return nil, err
	}

	return &SetObjectPolicyResponse{
		Key:       req.Key,
		Success:   true,
		Version:   newVersion,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// GetObjectPolicy retrieves policies for an object.
func (s *objectStorageService) GetObjectPolicy(ctx context.Context, req *GetObjectPolicyRequest) (*GetObjectPolicyResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("get_object_policy").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("get_object_policy", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, err := s.catalogMgr.Get(ctx, req.Key)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("get_object_policy", "not_found").Inc()
		}
		return nil, api.NewNotFoundWithCauseError("objectstorage.GetObjectPolicy", "object", err)
	}

	// Validate tenant access
	if req.TenantID != "" && entry.TenantID != "" && entry.TenantID != req.TenantID {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("get_object_policy", "forbidden").Inc()
		}
		return nil, api.NewCrossTenantAccessError("objectstorage.GetObjectPolicy")
	}

	return &GetObjectPolicyResponse{
		Key:               req.Key,
		ReplicationPolicy: entry.ReplicationPolicy,
		ArchivePolicy:     entry.ArchivePolicy,
		RetentionPolicy:   entry.RetentionPolicy,
		Timestamp:         time.Now().UnixNano(),
	}, nil
}

// =============================================================================
// Backend Management Operations
// =============================================================================

// ListBackends returns available storage backends.
func (s *objectStorageService) ListBackends(ctx context.Context, req *ListBackendsRequest) (*ListBackendsResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("list_backends").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("list_backends", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	names := s.backendMgr.ListBackends()
	backends := make([]*BackendInfo, 0, len(names))

	for _, name := range names {
		// Filter by type if specified
		cfg, err := s.backendMgr.GetBackendConfig(name)
		if err != nil {
			continue
		}

		if req.FilterByType != "" && cfg.Type != req.FilterByType {
			continue
		}

		info := &BackendInfo{
			Name:    name,
			Type:    cfg.Type,
			Enabled: cfg.Enabled,
			Default: cfg.Default,
			Tier:    string(cfg.Tier),
			Region:  cfg.Region,
		}

		// Get health if requested
		if req.IncludeHealth {
			health, _ := s.backendMgr.CheckHealth(ctx, name)
			if health != nil {
				info.Health = &BackendHealthInfo{
					Healthy:   health.Healthy,
					Latency:   health.Latency.Milliseconds(),
					LastCheck: health.LastCheck.UnixNano(),
					Error:     health.Error,
				}
			}
		}

		backends = append(backends, info)
	}

	return &ListBackendsResponse{
		Backends:  backends,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// GetBackendHealth returns health status for a backend.
func (s *objectStorageService) GetBackendHealth(ctx context.Context, req *GetBackendHealthRequest) (*GetBackendHealthResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("get_backend_health").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("get_backend_health", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	health, err := s.backendMgr.CheckHealth(ctx, req.Name)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("get_backend_health", "not_found").Inc()
		}
		return nil, api.NewNotFoundWithCauseError("objectstorage.GetBackendHealth", "backend", err)
	}

	return &GetBackendHealthResponse{
		Name:      health.Name,
		Healthy:   health.Healthy,
		Latency:   health.Latency.Milliseconds(),
		LastCheck: health.LastCheck.UnixNano(),
		Status:    health.Status,
		Error:     health.Error,
	}, nil
}

// =============================================================================
// Tenant Management Operations
// =============================================================================

// GetTenantUsage returns storage usage for a tenant.
func (s *objectStorageService) GetTenantUsage(ctx context.Context, req *GetTenantUsageRequest) (*GetTenantUsageResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("get_tenant_usage").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("get_tenant_usage", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	usage, err := s.catalogMgr.GetTenantUsage(ctx, req.TenantID)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("get_tenant_usage", "storage_error").Inc()
		}
		return nil, api.NewStorageOperationError("objectstorage.GetTenantUsage", "get tenant usage", err)
	}

	return &GetTenantUsageResponse{
		TenantID:      usage.TenantID,
		ObjectCount:   usage.ObjectCount,
		TotalSize:     usage.TotalSize,
		ActiveCount:   usage.ActiveCount,
		ArchivedCount: usage.ArchivedCount,
		Timestamp:     time.Now().UnixNano(),
	}, nil
}

// GetTenantQuotas returns quota settings for a tenant.
func (s *objectStorageService) GetTenantQuotas(ctx context.Context, req *GetTenantQuotasRequest) (*GetTenantQuotasResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("get_tenant_quotas").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("get_tenant_quotas", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	// Get tenant config
	tenantCfg := s.configResolver.GetTenantConfig(req.TenantID)

	var maxStorageBytes, maxObjectSize, maxObjectCount int64
	if tenantCfg != nil {
		maxStorageBytes = tenantCfg.Quotas.MaxStorageBytes
		maxObjectSize = tenantCfg.Quotas.MaxObjectSize
		maxObjectCount = tenantCfg.Quotas.MaxObjectCount
	}

	// Get current usage
	usage, _ := s.catalogMgr.GetTenantUsage(ctx, req.TenantID)
	var usedStorageBytes, usedObjectCount int64
	if usage != nil {
		usedStorageBytes = usage.TotalSize
		usedObjectCount = usage.ObjectCount
	}

	return &GetTenantQuotasResponse{
		TenantID:         req.TenantID,
		MaxStorageBytes:  maxStorageBytes,
		MaxObjectSize:    maxObjectSize,
		MaxObjectCount:   maxObjectCount,
		UsedStorageBytes: usedStorageBytes,
		UsedObjectCount:  usedObjectCount,
		Timestamp:        time.Now().UnixNano(),
	}, nil
}

// SetTenantQuotas sets or updates quotas for a tenant.
func (s *objectStorageService) SetTenantQuotas(ctx context.Context, req *SetTenantQuotasRequest) (*SetTenantQuotasResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("set_tenant_quotas").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("set_tenant_quotas", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	// Set tenant quotas via config resolver
	quotas := &config.TenantQuotas{
		MaxStorageBytes: req.MaxStorageBytes,
		MaxObjectSize:   req.MaxObjectSize,
		MaxObjectCount:  req.MaxObjectCount,
	}

	if err := s.configResolver.SetTenantQuotas(ctx, req.TenantID, quotas); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("set_tenant_quotas", "storage").Inc()
		}
		return nil, api.NewStorageOperationError("objectstorage.SetTenantQuotas", "set tenant quotas", err)
	}

	return &SetTenantQuotasResponse{
		TenantID:  req.TenantID,
		UpdatedAt: time.Now().UnixNano(),
	}, nil
}

// =============================================================================
// Batch Operations
// =============================================================================

// BatchPutObjects stores multiple objects in a single operation.
func (s *objectStorageService) BatchPutObjects(ctx context.Context, req *BatchPutObjectsRequest) (*BatchPutObjectsResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	results := make([]*BatchPutResult, 0, len(req.Objects))
	successCount := 0
	failureCount := 0

	for _, putReq := range req.Objects {
		// Apply tenant override if specified
		if req.TenantID != "" && putReq.TenantID == "" {
			putReq.TenantID = req.TenantID
		}

		resp, err := s.PutObject(ctx, putReq)
		if err != nil {
			failureCount++
			results = append(results, &BatchPutResult{
				Key:     putReq.Key,
				Success: false,
				Error:   err.Error(),
			})

			if !req.ContinueOnError {
				break
			}
			continue
		}

		successCount++
		results = append(results, &BatchPutResult{
			Key:      putReq.Key,
			Success:  true,
			ObjectID: resp.ObjectID,
			Checksum: resp.Checksum,
		})
	}

	return &BatchPutObjectsResponse{
		Results:      results,
		SuccessCount: successCount,
		FailureCount: failureCount,
		Timestamp:    time.Now().UnixNano(),
	}, nil
}

// BatchGetObjects retrieves multiple objects in a single operation.
func (s *objectStorageService) BatchGetObjects(ctx context.Context, req *BatchGetObjectsRequest) (*BatchGetObjectsResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	results := make([]*BatchGetResult, 0, len(req.Keys))
	foundCount := 0
	notFoundCount := 0

	for _, key := range req.Keys {
		getReq := &GetObjectRequest{
			Key:             key,
			TenantID:        req.TenantID,
			IncludeMetadata: req.IncludeMetadata,
		}

		resp, err := s.GetObject(ctx, getReq)
		if err != nil {
			notFoundCount++
			results = append(results, &BatchGetResult{
				Key:   key,
				Found: false,
				Error: err.Error(),
			})
			continue
		}

		foundCount++
		results = append(results, &BatchGetResult{
			Key:         key,
			Found:       true,
			Value:       resp.Value,
			ContentType: resp.ContentType,
			Size:        resp.Size,
			Checksum:    resp.Checksum,
			Version:     resp.Version,
			Tags:        resp.Tags,
		})
	}

	return &BatchGetObjectsResponse{
		Results:       results,
		FoundCount:    foundCount,
		NotFoundCount: notFoundCount,
		Timestamp:     time.Now().UnixNano(),
	}, nil
}

// BatchDeleteObjects deletes multiple objects in a single operation.
func (s *objectStorageService) BatchDeleteObjects(ctx context.Context, req *BatchDeleteObjectsRequest) (*BatchDeleteObjectsResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	results := make([]*BatchDeleteResult, 0, len(req.Keys))
	deletedCount := 0
	failureCount := 0

	for _, key := range req.Keys {
		deleteReq := &DeleteObjectRequest{
			Key:      key,
			TenantID: req.TenantID,
		}

		resp, err := s.DeleteObject(ctx, deleteReq)
		if err != nil {
			failureCount++
			results = append(results, &BatchDeleteResult{
				Key:     key,
				Success: false,
				Error:   err.Error(),
			})

			if !req.ContinueOnError {
				break
			}
			continue
		}

		deletedCount++
		results = append(results, &BatchDeleteResult{
			Key:     key,
			Success: true,
			Size:    resp.Size,
		})
	}

	return &BatchDeleteObjectsResponse{
		Results:      results,
		DeletedCount: deletedCount,
		FailureCount: failureCount,
		Timestamp:    time.Now().UnixNano(),
	}, nil
}
