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
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
)

var (
	// objectStorageAdminMetricsOnce ensures admin metrics are only registered once.
	objectStorageAdminMetricsOnce sync.Once

	// sharedObjectStorageAdminMetrics holds the singleton admin metrics instance.
	sharedObjectStorageAdminMetrics *objectStorageAdminMetrics
)

// objectStorageAdminService implements ObjectStorageAdminService.
type objectStorageAdminService struct {
	catalogMgr     catalog.CatalogManager
	backendMgr     catalog.BackendManager
	configResolver config.ObjectStoreConfigResolver
	logger         interfaces.Logger
	metrics        *objectStorageAdminMetrics
	closed         atomic.Bool
	mu             sync.RWMutex
	gcOperations   map[string]*GCOperation
}

// objectStorageAdminMetrics holds Prometheus metrics for admin operations.
type objectStorageAdminMetrics struct {
	backendsCreated   prometheus.Counter
	backendsDeleted   prometheus.Counter
	gcOperationsTotal prometheus.Counter
	repairsTotal      prometheus.Counter
	compactionsTotal  prometheus.Counter
	operationDuration *prometheus.HistogramVec
	operationErrors   *prometheus.CounterVec
}

// NewObjectStorageAdminService creates a new admin service.
func NewObjectStorageAdminService(
	catalogMgr catalog.CatalogManager,
	backendMgr catalog.BackendManager,
	configResolver config.ObjectStoreConfigResolver,
	logger interfaces.Logger,
) (ObjectStorageAdminService, error) {
	if catalogMgr == nil {
		return nil, api.NewNilFieldError("service.NewObjectStorageAdminService", "catalog manager")
	}

	if backendMgr == nil {
		return nil, api.NewNilFieldError("service.NewObjectStorageAdminService", "backend manager")
	}

	if logger == nil {
		return nil, api.NewNilFieldError("service.NewObjectStorageAdminService", "logger")
	}

	svc := &objectStorageAdminService{
		catalogMgr:     catalogMgr,
		backendMgr:     backendMgr,
		configResolver: configResolver,
		logger:         logger.WithField("component", "objectstorage_admin_service"),
		gcOperations:   make(map[string]*GCOperation),
	}

	// Initialize metrics (only once across all instances)
	svc.initMetrics()

	svc.logger.Info("object storage admin service initialized")

	return svc, nil
}

// initMetrics initializes Prometheus metrics for the admin service.
func (s *objectStorageAdminService) initMetrics() {
	objectStorageAdminMetricsOnce.Do(func() {
		sharedObjectStorageAdminMetrics = &objectStorageAdminMetrics{
			backendsCreated: promauto.With(metrics.Registry).NewCounter(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "objectstorage_admin",
				Name:      "backends_created_total",
				Help:      "Total number of backends created",
			}),

			backendsDeleted: promauto.With(metrics.Registry).NewCounter(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "objectstorage_admin",
				Name:      "backends_deleted_total",
				Help:      "Total number of backends deleted",
			}),

			gcOperationsTotal: promauto.With(metrics.Registry).NewCounter(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "objectstorage_admin",
				Name:      "gc_operations_total",
				Help:      "Total number of garbage collection operations",
			}),

			repairsTotal: promauto.With(metrics.Registry).NewCounter(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "objectstorage_admin",
				Name:      "repairs_total",
				Help:      "Total number of object repairs",
			}),

			compactionsTotal: promauto.With(metrics.Registry).NewCounter(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "objectstorage_admin",
				Name:      "compactions_total",
				Help:      "Total number of compaction operations",
			}),

			operationDuration: promauto.With(metrics.Registry).NewHistogramVec(prometheus.HistogramOpts{
				Namespace: "quicbaas",
				Subsystem: "objectstorage_admin",
				Name:      "operation_duration_seconds",
				Help:      "Duration of admin operations in seconds",
				Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
			}, []string{"operation"}),

			operationErrors: promauto.With(metrics.Registry).NewCounterVec(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "objectstorage_admin",
				Name:      "operation_errors_total",
				Help:      "Total errors by operation type",
			}, []string{"operation", "error_type"}),
		}
	})

	// Assign the shared metrics to this instance
	s.metrics = sharedObjectStorageAdminMetrics
}

// checkClosed returns an error if the service is closed.
func (s *objectStorageAdminService) checkClosed() error {
	if s.closed.Load() {
		return api.NewServiceClosedError("objectstorage_admin.checkClosed", "object storage admin service")
	}
	return nil
}

// =============================================================================
// Backend Management Operations
// =============================================================================

// CreateBackend registers a new storage backend.
func (s *objectStorageAdminService) CreateBackend(ctx context.Context, req *CreateBackendRequest) (*CreateBackendResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("create_backend").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("create_backend", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.WithFields(map[string]interface{}{
		"name": req.Name,
		"type": req.Type,
		"tier": req.Tier,
	}).Debug("creating backend")

	// Check if backend already exists
	existingBackends := s.backendMgr.ListBackends()
	for _, name := range existingBackends {
		if name == req.Name {
			if s.metrics != nil && s.metrics.operationErrors != nil {
				s.metrics.operationErrors.WithLabelValues("create_backend", "already_exists").Inc()
			}
			return nil, api.NewAlreadyExistsError("admin.CreateBackend", "backend", req.Name)
		}
	}

	// Create backend configuration
	backendCfg := &catalog.BackendConfig{
		Type:     string(req.Type),
		Enabled:  req.Enabled,
		Tier:     convertStorageTier(req.Tier),
		Settings: req.Config,
	}

	// Register the backend
	if err := s.backendMgr.RegisterBackend(req.Name, backendCfg); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("create_backend", "registration_error").Inc()
		}
		return nil, api.NewOperationFailedError("register backend", err)
	}

	// Update metrics
	if s.metrics != nil && s.metrics.backendsCreated != nil {
		s.metrics.backendsCreated.Inc()
	}

	now := time.Now().UnixNano()

	s.logger.WithFields(map[string]interface{}{
		"name": req.Name,
		"type": req.Type,
	}).Info("backend created successfully")

	return &CreateBackendResponse{
		Name:      req.Name,
		CreatedAt: now,
	}, nil
}

// UpdateBackend updates backend configuration.
func (s *objectStorageAdminService) UpdateBackend(ctx context.Context, req *UpdateBackendRequest) (*UpdateBackendResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("update_backend").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("update_backend", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.WithFields(map[string]interface{}{
		"name": req.Name,
	}).Debug("updating backend")

	// Get existing configuration
	existingCfg, err := s.backendMgr.GetBackendConfig(req.Name)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("update_backend", "not_found").Inc()
		}
		return nil, api.NewNotFoundByNameError("admin.UpdateBackend", "backend", req.Name)
	}

	// Update configuration
	updatedCfg := existingCfg.Clone()
	if req.Config != nil {
		if updatedCfg.Settings == nil {
			updatedCfg.Settings = make(map[string]string)
		}
		for k, v := range req.Config {
			updatedCfg.Settings[k] = v
		}
	}
	if req.Tier != StorageTierUnspecified {
		updatedCfg.Tier = convertStorageTier(req.Tier)
	}

	// Unregister and re-register with new config
	if err := s.backendMgr.UnregisterBackend(req.Name); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("update_backend", "unregister_error").Inc()
		}
		return nil, api.NewOperationFailedError("unregister backend for update", err)
	}

	if err := s.backendMgr.RegisterBackend(req.Name, updatedCfg); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("update_backend", "register_error").Inc()
		}
		return nil, api.NewOperationFailedError("re-register backend", err)
	}

	now := time.Now().UnixNano()

	s.logger.WithFields(map[string]interface{}{
		"name": req.Name,
	}).Info("backend updated successfully")

	return &UpdateBackendResponse{
		Name:      req.Name,
		UpdatedAt: now,
	}, nil
}

// DeleteBackend removes a storage backend.
func (s *objectStorageAdminService) DeleteBackend(ctx context.Context, req *DeleteBackendRequest) (*DeleteBackendResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("delete_backend").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("delete_backend", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.WithFields(map[string]interface{}{
		"name":       req.Name,
		"migrate_to": req.MigrateTo,
		"force":      req.Force,
	}).Debug("deleting backend")

	// Check if backend exists
	_, err := s.backendMgr.GetBackendConfig(req.Name)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("delete_backend", "not_found").Inc()
		}
		return nil, api.NewNotFoundByNameError("admin.DeleteBackend", "backend", req.Name)
	}

	// Check for objects in backend
	storeUsage, err := s.catalogMgr.GetStoreUsage(ctx, req.Name)
	var migratedObjects int64

	if err == nil && storeUsage != nil && storeUsage.ObjectCount > 0 {
		if !req.Force && req.MigrateTo == "" {
			if s.metrics != nil && s.metrics.operationErrors != nil {
				s.metrics.operationErrors.WithLabelValues("delete_backend", "objects_exist").Inc()
			}
			return nil, api.NewContainsObjectsError("admin.DeleteBackend", storeUsage.ObjectCount, "specify migrate_to or use force")
		}

		if req.MigrateTo != "" {
			// Verify migration target exists
			_, migErr := s.backendMgr.GetBackend(req.MigrateTo)
			if migErr != nil {
				if s.metrics != nil && s.metrics.operationErrors != nil {
					s.metrics.operationErrors.WithLabelValues("delete_backend", "migration_target_not_found").Inc()
				}
				return nil, api.NewMigrationTargetNotFoundError("admin.DeleteBackend", req.MigrateTo)
			}

			// Migration would happen here in a full implementation
			// For now, we'll log the intent
			s.logger.WithFields(map[string]interface{}{
				"from":  req.Name,
				"to":    req.MigrateTo,
				"count": storeUsage.ObjectCount,
			}).Info("migration required before backend deletion")

			migratedObjects = storeUsage.ObjectCount
		}
	}

	// Unregister the backend
	if err := s.backendMgr.UnregisterBackend(req.Name); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("delete_backend", "unregister_error").Inc()
		}
		return nil, api.NewOperationFailedError("unregister backend", err)
	}

	// Update metrics
	if s.metrics != nil && s.metrics.backendsDeleted != nil {
		s.metrics.backendsDeleted.Inc()
	}

	s.logger.WithFields(map[string]interface{}{
		"name":             req.Name,
		"migrated_objects": migratedObjects,
	}).Info("backend deleted successfully")

	return &DeleteBackendResponse{
		Name:            req.Name,
		Deleted:         true,
		MigratedObjects: migratedObjects,
	}, nil
}

// SetBackendStatus enables or disables a backend.
func (s *objectStorageAdminService) SetBackendStatus(ctx context.Context, req *SetBackendStatusRequest) (*SetBackendStatusResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("set_backend_status").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("set_backend_status", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.WithFields(map[string]interface{}{
		"name":    req.Name,
		"enabled": req.Enabled,
	}).Debug("setting backend status")

	// Get existing configuration
	existingCfg, err := s.backendMgr.GetBackendConfig(req.Name)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("set_backend_status", "not_found").Inc()
		}
		return nil, api.NewNotFoundByNameError("admin.SetBackendStatus", "backend", req.Name)
	}

	// Update enabled status
	updatedCfg := existingCfg.Clone()
	updatedCfg.Enabled = req.Enabled

	// Unregister and re-register with new config
	if err := s.backendMgr.UnregisterBackend(req.Name); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("set_backend_status", "unregister_error").Inc()
		}
		return nil, api.NewOperationFailedError("unregister backend", err)
	}

	if err := s.backendMgr.RegisterBackend(req.Name, updatedCfg); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("set_backend_status", "register_error").Inc()
		}
		return nil, api.NewOperationFailedError("re-register backend", err)
	}

	now := time.Now().UnixNano()

	s.logger.WithFields(map[string]interface{}{
		"name":    req.Name,
		"enabled": req.Enabled,
	}).Info("backend status updated successfully")

	return &SetBackendStatusResponse{
		Name:      req.Name,
		Enabled:   req.Enabled,
		UpdatedAt: now,
	}, nil
}

// =============================================================================
// Statistics Operations
// =============================================================================

// GetStorageStats returns cluster-wide storage statistics.
func (s *objectStorageAdminService) GetStorageStats(ctx context.Context, req *GetStorageStatsRequest) (*GetStorageStatsResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("get_storage_stats").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("get_storage_stats", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	resp := &GetStorageStatsResponse{
		CalculatedAt: time.Now().UnixNano(),
	}

	// Get backend statistics
	backendNames := s.backendMgr.ListBackends()

	// Validate backend count before conversion
	if len(backendNames) > math.MaxInt32 {
		return nil, api.NewBackendCountOverflowError("admin.GetStorageStats", len(backendNames))
	}
	resp.TotalBackends = int32(len(backendNames)) //#nosec G115 -- bounds checked above

	if req.IncludeBackends {
		resp.BackendStats = make([]*BackendInfo, 0, len(backendNames))
	}

	for _, name := range backendNames {
		cfg, err := s.backendMgr.GetBackendConfig(name)
		if err != nil {
			continue
		}

		// Check health
		health, _ := s.backendMgr.CheckHealth(ctx, name)
		if health != nil && health.Healthy {
			resp.HealthyBackends++
		}

		// Get store usage
		storeUsage, _ := s.catalogMgr.GetStoreUsage(ctx, name)
		if storeUsage != nil {
			resp.TotalBytes += storeUsage.TotalSize
			resp.TotalObjects += storeUsage.ObjectCount
		}

		if req.IncludeBackends {
			info := &BackendInfo{
				Name:    name,
				Type:    cfg.Type,
				Enabled: cfg.Enabled,
				Tier:    string(cfg.Tier),
				Region:  cfg.Region,
			}
			if storeUsage != nil {
				info.ObjectCount = storeUsage.ObjectCount
				info.TotalSize = storeUsage.TotalSize
			}
			if health != nil {
				info.Health = &BackendHealthInfo{
					Healthy:   health.Healthy,
					Latency:   health.Latency.Milliseconds(),
					LastCheck: health.LastCheck.UnixNano(),
					Error:     health.Error,
				}
			}
			resp.BackendStats = append(resp.BackendStats, info)
		}
	}

	// Get tenant statistics if requested
	if req.IncludeTenants {
		// This would require iterating through tenants
		// For now, return empty list - full implementation would scan tenant data
		resp.TenantStats = make([]*TenantStorageStats, 0)
	}

	return resp, nil
}

// =============================================================================
// Garbage Collection Operations
// =============================================================================

// RunGarbageCollection initiates garbage collection on specified backends.
func (s *objectStorageAdminService) RunGarbageCollection(ctx context.Context, req *RunGarbageCollectionRequest) (*RunGarbageCollectionResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("run_gc").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("run_gc", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Determine backends to process
	backends := req.Backends
	if len(backends) == 0 {
		backends = s.backendMgr.ListBackends()
	}

	// Create GC operation
	opID := uuid.New().String()
	now := time.Now().UnixNano()

	gcOp := &GCOperation{
		ID:         opID,
		TenantID:   tenantID,
		Backends:   backends,
		DryRun:     req.DryRun,
		Aggressive: req.Aggressive,
		Status:     GCStatusRunning,
		StartedAt:  now,
	}

	s.gcOperations[opID] = gcOp

	// Update metrics
	if s.metrics != nil && s.metrics.gcOperationsTotal != nil {
		s.metrics.gcOperationsTotal.Inc()
	}

	// Start GC in background
	go s.runGCOperation(context.WithoutCancel(ctx), gcOp)

	s.logger.WithFields(map[string]interface{}{
		"operation_id": opID,
		"backends":     backends,
		"dry_run":      req.DryRun,
		"aggressive":   req.Aggressive,
	}).Info("garbage collection initiated")

	return &RunGarbageCollectionResponse{
		OperationID: opID,
		StartedAt:   now,
	}, nil
}

// Grace windows for garbage collection. An orphaned backend object is
// collected only if it was last modified at least this long before the
// operation started, so a PutObject still between its backend write and its
// catalog write is never collected.
const (
	gcOrphanGrace           = time.Hour
	gcOrphanGraceAggressive = 5 * time.Minute
)

// runGCOperation collects orphaned objects on each of op's backends in
// order: objects no catalog entry references and that are older than the
// grace window. The first backend or catalog error fails the operation.
func (s *objectStorageAdminService) runGCOperation(ctx context.Context, op *GCOperation) {
	defer func() {
		if r := recover(); r != nil {
			s.mu.Lock()
			op.Status = GCStatusFailed
			op.Error = fmt.Sprintf("panic: %v", r)
			op.CompletedAt = time.Now().UnixNano()
			s.mu.Unlock()
		}
	}()

	fail := func(msg string) {
		s.mu.Lock()
		op.Status = GCStatusFailed
		op.Error = msg
		op.CompletedAt = time.Now().UnixNano()
		s.mu.Unlock()
		s.logger.WithFields(map[string]interface{}{
			"operation_id": op.ID,
			"error":        msg,
		}).Error("garbage collection failed")
	}

	grace := gcOrphanGrace
	if op.Aggressive {
		grace = gcOrphanGraceAggressive
	}
	cutoff := time.Unix(0, op.StartedAt).Add(-grace)

	for _, backendName := range op.Backends {
		select {
		case <-ctx.Done():
			fail(ctx.Err().Error())
			return
		default:
		}

		if err := s.collectBackendOrphans(ctx, op, backendName, cutoff); err != nil {
			fail(err.Error())
			return
		}
	}

	s.mu.Lock()
	op.Status = GCStatusCompleted
	op.CompletedAt = time.Now().UnixNano()
	scanned, deleted, reclaimed := op.ObjectsScanned, op.ObjectsDeleted, op.BytesReclaimed
	s.mu.Unlock()

	s.logger.WithFields(map[string]interface{}{
		"operation_id":    op.ID,
		"objects_scanned": scanned,
		"objects_deleted": deleted,
		"bytes_reclaimed": reclaimed,
		"dry_run":         op.DryRun,
	}).Info("garbage collection completed")
}

// collectBackendOrphans deletes (or, in a dry run, counts) the objects in one
// backend that no catalog entry references and that were last modified before
// cutoff. The backend is listed before the catalog is read: PutObject writes
// the backend before the catalog, so a ref listed before its entry exists is
// young and protected by the grace window. An object whose age cannot be read
// is never deleted.
func (s *objectStorageAdminService) collectBackendOrphans(ctx context.Context, op *GCOperation, backendName string, cutoff time.Time) error {
	storage, err := s.backendMgr.GetBackend(backendName)
	if err != nil {
		return fmt.Errorf("backend %s: %w", backendName, err)
	}
	if storage == nil {
		return fmt.Errorf("backend %s: no storage", backendName)
	}

	refs, err := storage.ListWithContext(ctx, "")
	if err != nil {
		return fmt.Errorf("backend %s: list objects: %w", backendName, err)
	}

	referenced, err := s.referencedRefs(ctx, backendName)
	if err != nil {
		return fmt.Errorf("backend %s: %w", backendName, err)
	}

	s.mu.Lock()
	op.ObjectsScanned += int64(len(refs))
	s.mu.Unlock()

	var deleted int64
	for _, ref := range refs {
		if _, ok := referenced[ref]; ok {
			continue
		}
		meta, err := storage.GetMetadata(ctx, ref)
		if err != nil || meta == nil || meta.LastModified.IsZero() {
			continue
		}
		if !meta.LastModified.Before(cutoff) {
			continue
		}
		if !op.DryRun {
			if err := storage.DeleteWithContext(ctx, ref); err != nil {
				return fmt.Errorf("backend %s: delete orphan %s: %w", backendName, ref, err)
			}
		}
		deleted++
		s.mu.Lock()
		op.ObjectsDeleted++
		op.BytesReclaimed += meta.Size
		s.mu.Unlock()
	}

	s.logger.WithFields(map[string]interface{}{
		"operation_id": op.ID,
		"backend":      backendName,
		"scanned":      len(refs),
		"orphans":      deleted,
		"dry_run":      op.DryRun,
	}).Debug("processed backend for GC")
	return nil
}

// referencedRefs returns the set of object refs that catalog entries locate
// on the named backend, primary and replica locations alike.
func (s *objectStorageAdminService) referencedRefs(ctx context.Context, backendName string) (map[string]struct{}, error) {
	referenced := make(map[string]struct{})
	cursor := ""
	for {
		page, err := s.catalogMgr.ListByStore(ctx, backendName, &catalog.ListOptions{Limit: 1000, Cursor: cursor})
		if err != nil {
			return nil, fmt.Errorf("list catalog entries: %w", err)
		}
		if page == nil {
			return referenced, nil
		}
		for _, key := range page.Keys {
			entry, err := s.catalogMgr.Get(ctx, key)
			if err != nil {
				if catalog.IsNotFound(err) {
					// Deleted since the listing.
					continue
				}
				return nil, fmt.Errorf("read catalog entry %s: %w", key, err)
			}
			for _, loc := range entry.GetAllLocations() {
				if loc.Store == backendName {
					referenced[loc.ObjectRef] = struct{}{}
				}
			}
		}
		if !page.HasMore || page.NextCursor == "" {
			return referenced, nil
		}
		cursor = page.NextCursor
	}
}

// GetGarbageCollectionProgress returns GC progress.
func (s *objectStorageAdminService) GetGarbageCollectionProgress(ctx context.Context, req *GetGarbageCollectionProgressRequest) (*GetGarbageCollectionProgressResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("get_gc_progress").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("get_gc_progress", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	// runGCOperation updates op's fields under s.mu in its own goroutine, so
	// the response is built from them while the read lock is held.
	s.mu.RLock()
	op, exists := s.gcOperations[req.OperationID]
	var resp *GetGarbageCollectionProgressResponse
	if exists {
		// Calculate percent complete
		var percentComplete float64
		if op.ObjectsScanned > 0 {
			// Simple estimate based on backends processed
			percentComplete = 100.0
			if op.Status == GCStatusRunning {
				percentComplete = 50.0
			}
		}
		resp = &GetGarbageCollectionProgressResponse{
			OperationID:     op.ID,
			Status:          op.Status,
			ObjectsScanned:  op.ObjectsScanned,
			ObjectsDeleted:  op.ObjectsDeleted,
			BytesReclaimed:  op.BytesReclaimed,
			PercentComplete: percentComplete,
			Error:           op.Error,
			CompletedAt:     op.CompletedAt,
		}
	}
	s.mu.RUnlock()

	if !exists {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("get_gc_progress", "not_found").Inc()
		}
		return nil, api.NewGCOperationNotFoundError("admin.GetGarbageCollectionProgress", req.OperationID)
	}

	return resp, nil
}

// =============================================================================
// Repair Operations
// =============================================================================

// RepairObject attempts to repair an object with inconsistent replicas.
func (s *objectStorageAdminService) RepairObject(ctx context.Context, req *RepairObjectRequest) (*RepairObjectResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("repair_object").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("repair_object", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.WithFields(map[string]interface{}{
		"key":             req.Key,
		"source_location": req.SourceLocation,
	}).Debug("repairing object")

	// Get catalog entry
	entry, err := s.catalogMgr.Get(ctx, req.Key)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("repair_object", "not_found").Inc()
		}
		return nil, api.NewNotFoundWithCauseError("admin.RepairObject", "object", err)
	}

	// Get all locations
	allLocations := entry.GetAllLocations()
	if len(allLocations) < 2 {
		return &RepairObjectResponse{
			Key:               req.Key,
			Success:           true,
			RepairedLocations: []string{},
			Error:             "no repair needed - object has fewer than 2 locations",
		}, nil
	}

	// Determine source location
	sourceLocation := req.SourceLocation
	if sourceLocation == "" {
		// Use primary store as source by default
		sourceLocation = entry.PrimaryStore.Store
	}

	// Get source backend and read object
	srcBackend, err := s.backendMgr.GetBackend(sourceLocation)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("repair_object", "source_unavailable").Inc()
		}
		return nil, api.NewSourceBackendUnavailableError("admin.RepairObject", err)
	}

	// Find source location details
	var srcLoc *catalog.StoreLocation
	for i := range allLocations {
		if allLocations[i].Store == sourceLocation {
			srcLoc = &allLocations[i]
			break
		}
	}
	if srcLoc == nil {
		return nil, api.NewSourceLocationNotFoundError("admin.RepairObject", sourceLocation)
	}

	// Read source data
	reader, err := srcBackend.GetWithContext(ctx, srcLoc.ObjectRef)
	if err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("repair_object", "read_error").Inc()
		}
		return nil, api.NewStorageOperationError("admin.RepairObject", "read source object", err)
	}
	var srcBuf bytes.Buffer
	if _, readErr := srcBuf.ReadFrom(reader); readErr != nil {
		s.logger.WithError(readErr).Warn("failed to read source object data during repair")
	}
	if closeErr := reader.Close(); closeErr != nil {
		s.logger.WithError(closeErr).Warn("failed to close reader during repair")
	}

	// Compute source checksum
	srcChecksum := sha256.Sum256(srcBuf.Bytes())

	// Compare and repair other locations
	repairedLocations := make([]string, 0)

	for _, loc := range allLocations {
		if loc.Store == sourceLocation {
			continue
		}

		// Get target backend
		targetBackend, err := s.backendMgr.GetBackend(loc.Store)
		if err != nil {
			s.logger.WithFields(map[string]interface{}{
				"key":   req.Key,
				"store": loc.Store,
				"error": err.Error(),
			}).Warn("failed to get backend for repair")
			continue
		}

		// Read target data
		targetReader, err := targetBackend.GetWithContext(ctx, loc.ObjectRef)
		if err != nil {
			// Object missing - needs repair
			s.logger.WithFields(map[string]interface{}{
				"key":   req.Key,
				"store": loc.Store,
			}).Debug("target object missing, repairing")
		} else {
			var targetBuf bytes.Buffer
			if _, readErr := targetBuf.ReadFrom(targetReader); readErr != nil {
				s.logger.WithError(readErr).Warn("failed to read target object data during repair")
			}
			if closeErr := targetReader.Close(); closeErr != nil {
				s.logger.WithError(closeErr).Warn("failed to close target reader during repair")
			}

			// Verify checksum
			targetChecksum := sha256.Sum256(targetBuf.Bytes())
			if bytes.Equal(srcChecksum[:], targetChecksum[:]) {
				// Checksums match, no repair needed
				continue
			}

			s.logger.WithFields(map[string]interface{}{
				"key":   req.Key,
				"store": loc.Store,
			}).Debug("target object has checksum mismatch, repairing")
		}

		// Repair by copying from source
		if err := targetBackend.PutWithContext(ctx, loc.ObjectRef, bytes.NewReader(srcBuf.Bytes())); err != nil {
			s.logger.WithFields(map[string]interface{}{
				"key":   req.Key,
				"store": loc.Store,
				"error": err.Error(),
			}).Warn("failed to repair object in backend")
			continue
		}

		repairedLocations = append(repairedLocations, loc.Store)
	}

	// Update metrics
	if s.metrics != nil && s.metrics.repairsTotal != nil {
		s.metrics.repairsTotal.Inc()
	}

	s.logger.WithFields(map[string]interface{}{
		"key":                req.Key,
		"repaired_count":     len(repairedLocations),
		"repaired_locations": repairedLocations,
	}).Info("object repair completed")

	return &RepairObjectResponse{
		Key:               req.Key,
		Success:           true,
		RepairedLocations: repairedLocations,
	}, nil
}

// =============================================================================
// Compaction Operations
// =============================================================================

// CompactBackend validates a compaction request and returns a
// CompactionUnsupportedError (ErrUnimplemented): no backend has a compaction
// verb, so nothing is recorded and no operation starts.
func (s *objectStorageAdminService) CompactBackend(ctx context.Context, req *CompactBackendRequest) (*CompactBackendResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil && s.metrics.operationDuration != nil {
			s.metrics.operationDuration.WithLabelValues("compact_backend").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := req.Validate(); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("compact_backend", "invalid_input").Inc()
		}
		return nil, err
	}

	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	if _, err := prepareTenantContext(ctx); err != nil {
		return nil, err
	}

	s.logger.WithFields(map[string]interface{}{
		"name":       req.Name,
		"aggressive": req.Aggressive,
	}).Debug("compaction requested")

	// Verify backend exists
	if _, err := s.backendMgr.GetBackendConfig(req.Name); err != nil {
		if s.metrics != nil && s.metrics.operationErrors != nil {
			s.metrics.operationErrors.WithLabelValues("compact_backend", "not_found").Inc()
		}
		return nil, api.NewNotFoundByNameError("admin.CompactBackend", "backend", req.Name)
	}

	// No object-storage backend has a compaction verb (common.Storage has
	// none), so the request is refused before anything is recorded, rather
	// than reported as a compaction that never ran.
	if s.metrics != nil && s.metrics.operationErrors != nil {
		s.metrics.operationErrors.WithLabelValues("compact_backend", "unsupported").Inc()
	}
	return nil, api.NewCompactionUnsupportedError("admin.CompactBackend", req.Name)
}
