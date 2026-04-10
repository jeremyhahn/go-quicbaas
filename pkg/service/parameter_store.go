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
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
)

var (
	// parameterStoreMetricsOnce ensures metrics are only registered once.
	parameterStoreMetricsOnce sync.Once

	// sharedParameterStoreMetrics holds the singleton metrics instance.
	sharedParameterStoreMetrics *parameterStoreMetrics
)

// parameterStoreService implements ParameterStoreService.
type parameterStoreService struct {
	paramManager interfaces.ParameterManager
	logger       interfaces.Logger
	metrics      *parameterStoreMetrics
	closed       atomic.Bool
}

// parameterStoreMetrics holds Prometheus metrics for parameter store operations.
type parameterStoreMetrics struct {
	parametersSet     prometheus.Counter
	parametersGet     prometheus.Counter
	parametersDeleted prometheus.Counter
	parametersListed  prometheus.Counter
	bytesWritten      prometheus.Counter
	bytesRead         prometheus.Counter
	operationDuration *prometheus.HistogramVec
	operationErrors   *prometheus.CounterVec
	expiredParameters prometheus.Counter
	encryptedOps      prometheus.Counter
}

// NewParameterStoreService creates a new parameter store service.
func NewParameterStoreService(
	paramManager interfaces.ParameterManager,
	logger interfaces.Logger,
) (ParameterStoreService, error) {
	if paramManager == nil {
		return nil, api.NewNilFieldError("service.NewParameterStoreService", "parameter manager")
	}

	if logger == nil {
		return nil, api.NewNilFieldError("service.NewParameterStoreService", "logger")
	}

	svc := &parameterStoreService{
		paramManager: paramManager,
		logger:       logger.WithField("component", "parameter_store_service"),
	}

	// Initialize metrics (only once across all instances)
	svc.initMetrics()

	svc.logger.Info("parameter store service initialized")

	return svc, nil
}

// initMetrics initializes Prometheus metrics for the parameter store service.
// Uses sync.Once to ensure metrics are only registered once.
func (s *parameterStoreService) initMetrics() {
	parameterStoreMetricsOnce.Do(func() {
		sharedParameterStoreMetrics = &parameterStoreMetrics{
			parametersSet: promauto.With(metrics.Registry).NewCounter(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "parameters",
				Name:      "set_total",
				Help:      "Total number of parameters set",
			}),

			parametersGet: promauto.With(metrics.Registry).NewCounter(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "parameters",
				Name:      "get_total",
				Help:      "Total number of parameters retrieved",
			}),

			parametersDeleted: promauto.With(metrics.Registry).NewCounter(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "parameters",
				Name:      "deleted_total",
				Help:      "Total number of parameters deleted",
			}),

			parametersListed: promauto.With(metrics.Registry).NewCounter(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "parameters",
				Name:      "listed_total",
				Help:      "Total number of list operations",
			}),

			bytesWritten: promauto.With(metrics.Registry).NewCounter(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "parameters",
				Name:      "bytes_written_total",
				Help:      "Total bytes written",
			}),

			bytesRead: promauto.With(metrics.Registry).NewCounter(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "parameters",
				Name:      "bytes_read_total",
				Help:      "Total bytes read",
			}),

			operationDuration: promauto.With(metrics.Registry).NewHistogramVec(prometheus.HistogramOpts{
				Namespace: "quicbaas",
				Subsystem: "parameters",
				Name:      "operation_duration_seconds",
				Help:      "Duration of parameter operations in seconds",
				Buckets:   []float64{.0001, .0005, .001, .005, .01, .025, .05, .1, .25, .5, 1},
			}, []string{"operation"}),

			operationErrors: promauto.With(metrics.Registry).NewCounterVec(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "parameters",
				Name:      "operation_errors_total",
				Help:      "Total errors by operation type",
			}, []string{"operation", "error_type"}),

			expiredParameters: promauto.With(metrics.Registry).NewCounter(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "parameters",
				Name:      "expired_total",
				Help:      "Total number of expired parameters encountered",
			}),

			encryptedOps: promauto.With(metrics.Registry).NewCounter(prometheus.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "parameters",
				Name:      "encrypted_ops_total",
				Help:      "Total number of encrypted parameter operations",
			}),
		}
	})

	// Assign the shared metrics to this instance
	s.metrics = sharedParameterStoreMetrics
}

// checkClosed returns an error if the service is closed.
func (s *parameterStoreService) checkClosed() error {
	if s.closed.Load() {
		return api.NewServiceClosedError("service.parameterstore", "parameter store service")
	}
	return nil
}

// GetParameter retrieves a single parameter by path.
func (s *parameterStoreService) GetParameter(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.WithLabelValues("get").Observe(time.Since(start).Seconds())
		}
	}()

	// Check if service is closed
	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	// Validate request
	if req == nil {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("get", "invalid_input").Inc()
		}
		return nil, api.NewNilRequestError("service.parameterstore.GetParameter")
	}

	if req.Path == "" {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("get", "invalid_input").Inc()
		}
		return nil, api.NewRequiredFieldError("path")
	}

	// Prepare tenant context for storage scoping
	tenantID, err := paramPrepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedPath := paramTenantScopedPath(tenantID, req.Path)

	// Set ABAC context (uses original path for ABAC/audit)
	ctx = s.setParameterContext(ctx, req.Path, "", iam.ParameterOperationGet, false, nil)

	// Log operation
	s.logger.WithFields(map[string]interface{}{
		"path":            req.Path,
		"with_decryption": req.WithDecryption,
	}).Debug("getting parameter")

	// Get from manager using tenant-scoped path
	value, metadata, err := s.paramManager.Get(ctx, scopedPath, req.WithDecryption)
	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("get", "storage").Inc()
		}
		return nil, err
	}

	// Record metrics
	if s.metrics != nil {
		s.metrics.parametersGet.Inc()
		s.metrics.bytesRead.Add(float64(len(value)))
		if metadata.IsEncrypted {
			s.metrics.encryptedOps.Inc()
		}
	}

	return &interfaces.GetParameterResponse{
		Parameter: s.metadataToParameterValue(req.Path, value, metadata),
		Timestamp: time.Now(),
	}, nil
}

// SetParameter creates or updates a parameter.
func (s *parameterStoreService) SetParameter(ctx context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.WithLabelValues("set").Observe(time.Since(start).Seconds())
		}
	}()

	// Check if service is closed
	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	// Validate request
	if err := s.validateSetRequest(req); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("set", "invalid_input").Inc()
		}
		return nil, err
	}

	// Prepare tenant context for storage scoping
	tenantID, err := paramPrepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedPath := paramTenantScopedPath(tenantID, req.Path)

	// Build metadata
	metadata := s.buildMetadata(ctx, req)

	// Set ABAC context (uses original path for ABAC/audit)
	isEncrypted := req.KMSKeyID != ""
	ctx = s.setParameterContext(ctx, req.Path, req.Type, iam.ParameterOperationSet, isEncrypted, req.Tags)

	// Log operation
	s.logger.WithFields(map[string]interface{}{
		"path":      req.Path,
		"type":      req.Type,
		"encrypted": isEncrypted,
		"overwrite": req.Overwrite,
	}).Debug("setting parameter")

	// Build store options if encryption is requested
	var opts *interfaces.StoreOptions
	if isEncrypted {
		opts = &interfaces.StoreOptions{
			KeyIdentifier: req.KMSKeyID,
		}
	}

	// Check if parameter already exists using tenant-scoped path
	_, _, getErr := s.paramManager.Get(ctx, scopedPath, false)
	existed := getErr == nil

	// Store the parameter using tenant-scoped path
	if opts != nil {
		err = s.paramManager.StoreWithOptions(ctx, scopedPath, req.Value, metadata, req.Overwrite, opts)
	} else {
		err = s.paramManager.Store(ctx, scopedPath, req.Value, metadata, req.Overwrite)
	}

	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("set", "storage").Inc()
		}
		return nil, err
	}

	// Record metrics
	if s.metrics != nil {
		s.metrics.parametersSet.Inc()
		s.metrics.bytesWritten.Add(float64(len(req.Value)))
		if isEncrypted {
			s.metrics.encryptedOps.Inc()
		}
	}

	// Determine if this was a creation or update (return original path)
	return &interfaces.SetParameterResponse{
		Path:      req.Path,
		Created:   !existed, // Created only if it didn't exist before
		Timestamp: time.Now(),
	}, nil
}

// DeleteParameter removes a parameter by path.
func (s *parameterStoreService) DeleteParameter(ctx context.Context, req *interfaces.DeleteParameterRequest) (*interfaces.DeleteParameterResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.WithLabelValues("delete").Observe(time.Since(start).Seconds())
		}
	}()

	// Check if service is closed
	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	// Validate request
	if req == nil {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("delete", "invalid_input").Inc()
		}
		return nil, api.NewNilRequestError("service.parameterstore.DeleteParameter")
	}

	if req.Path == "" {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("delete", "invalid_input").Inc()
		}
		return nil, api.NewRequiredFieldError("path")
	}

	// Prepare tenant context for storage scoping
	tenantID, err := paramPrepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedPath := paramTenantScopedPath(tenantID, req.Path)

	// Set ABAC context (uses original path for ABAC/audit)
	ctx = s.setParameterContext(ctx, req.Path, "", iam.ParameterOperationDelete, false, nil)

	// Log operation
	s.logger.WithFields(map[string]interface{}{
		"path": req.Path,
	}).Debug("deleting parameter")

	// Delete from manager using tenant-scoped path
	err = s.paramManager.Delete(ctx, scopedPath)
	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("delete", "storage").Inc()
		}
		return nil, err
	}

	// Record metrics
	if s.metrics != nil {
		s.metrics.parametersDeleted.Inc()
	}

	return &interfaces.DeleteParameterResponse{
		Path:      req.Path,
		Deleted:   true,
		Timestamp: time.Now(),
	}, nil
}

// GetParameters retrieves multiple parameters by paths (batch get).
func (s *parameterStoreService) GetParameters(ctx context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.WithLabelValues("batch_get").Observe(time.Since(start).Seconds())
		}
	}()

	// Check if service is closed
	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	// Validate request
	if req == nil {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("batch_get", "invalid_input").Inc()
		}
		return nil, api.NewNilRequestError("service.parameterstore.GetParameters")
	}

	if len(req.Paths) == 0 {
		return &interfaces.GetParametersResponse{
			Parameters: []*interfaces.ParameterValue{},
			Timestamp:  time.Now(),
		}, nil
	}

	// Prepare tenant context for storage scoping
	tenantID, err := paramPrepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Scope each path to the tenant namespace
	scopedPaths := make([]string, len(req.Paths))
	for i, path := range req.Paths {
		scopedPaths[i] = paramTenantScopedPath(tenantID, path)
	}

	// Set ABAC context for batch operation
	ctx = s.setParameterContext(ctx, "", "", iam.ParameterOperationBatchGet, false, nil)
	paramCtx := iam.ParameterContextFromContext(ctx)
	if paramCtx != nil {
		paramCtx.BatchSize = len(req.Paths)
		ctx = iam.ContextWithParameterContext(ctx, paramCtx)
	}

	// Log operation
	s.logger.WithFields(map[string]interface{}{
		"batch_size":      len(req.Paths),
		"with_decryption": req.WithDecryption,
	}).Debug("getting parameters batch")

	// Get batch from manager using tenant-scoped paths
	results, errs, err := s.paramManager.GetBatch(ctx, scopedPaths, req.WithDecryption)
	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("batch_get", "storage").Inc()
		}
		return nil, err
	}

	// Convert results to response, stripping tenant prefix from paths
	params := make([]*interfaces.ParameterValue, 0, len(results))
	errorMap := make(map[string]string)

	var totalBytesRead int
	for scopedPath, result := range results {
		originalPath := paramTenantStripPath(tenantID, scopedPath)
		params = append(params, s.metadataToParameterValue(originalPath, result.Value, result.Metadata))
		totalBytesRead += len(result.Value)
		if result.Metadata.IsEncrypted && s.metrics != nil {
			s.metrics.encryptedOps.Inc()
		}
	}

	for scopedPath, e := range errs {
		originalPath := paramTenantStripPath(tenantID, scopedPath)
		errorMap[originalPath] = e.Error()
	}

	// Record metrics
	if s.metrics != nil {
		s.metrics.parametersGet.Add(float64(len(results)))
		s.metrics.bytesRead.Add(float64(totalBytesRead))
	}

	return &interfaces.GetParametersResponse{
		Parameters: params,
		Errors:     errorMap,
		Timestamp:  time.Now(),
	}, nil
}

// SetParameters creates or updates multiple parameters (batch set).
func (s *parameterStoreService) SetParameters(ctx context.Context, req *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.WithLabelValues("batch_set").Observe(time.Since(start).Seconds())
		}
	}()

	// Check if service is closed
	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	// Validate request
	if req == nil {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("batch_set", "invalid_input").Inc()
		}
		return nil, api.NewNilRequestError("service.parameterstore.SetParameters")
	}

	if len(req.Parameters) == 0 {
		return &interfaces.SetParametersResponse{
			Results:   []*interfaces.SetParameterResult{},
			Timestamp: time.Now(),
		}, nil
	}

	// Prepare tenant context for storage scoping
	tenantID, err := paramPrepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Set ABAC context for batch operation
	ctx = s.setParameterContext(ctx, "", "", iam.ParameterOperationBatchSet, false, nil)
	paramCtx := iam.ParameterContextFromContext(ctx)
	if paramCtx != nil {
		paramCtx.BatchSize = len(req.Parameters)
		ctx = iam.ContextWithParameterContext(ctx, paramCtx)
	}

	// Log operation
	s.logger.WithFields(map[string]interface{}{
		"batch_size": len(req.Parameters),
	}).Debug("setting parameters batch")

	// Convert requests to entries with tenant-scoped paths
	entries := make([]*interfaces.ParameterEntry, len(req.Parameters))
	var totalBytes int
	var hasEncryption bool
	var opts *interfaces.StoreOptions

	for i, p := range req.Parameters {
		if err := s.validateSetRequest(p); err != nil {
			if s.metrics != nil {
				s.metrics.operationErrors.WithLabelValues("batch_set", "invalid_input").Inc()
			}
			return nil, err
		}

		metadata := s.buildMetadata(ctx, p)
		scopedPath := paramTenantScopedPath(tenantID, p.Path)
		entries[i] = &interfaces.ParameterEntry{
			Path:     scopedPath,
			Value:    p.Value,
			Metadata: metadata,
		}
		totalBytes += len(p.Value)

		if p.KMSKeyID != "" {
			hasEncryption = true
			// Use the first key ID for the batch (simplified)
			if opts == nil {
				opts = &interfaces.StoreOptions{
					KeyIdentifier: p.KMSKeyID,
				}
			}
		}
	}

	// Store batch
	// Note: This assumes all parameters use the same encryption settings
	// A more sophisticated implementation could handle per-parameter encryption
	err = s.paramManager.StoreBatch(ctx, entries, true, opts)

	// Build results (return original paths to caller)
	results := make([]*interfaces.SetParameterResult, len(req.Parameters))
	for i, p := range req.Parameters {
		results[i] = &interfaces.SetParameterResult{
			Path:    p.Path,
			Created: true,
			Success: err == nil,
		}
		if err != nil {
			results[i].Error = err.Error()
		}
	}

	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("batch_set", "storage").Inc()
		}
		// Return partial results with error info
		return &interfaces.SetParametersResponse{
			Results:   results,
			Timestamp: time.Now(),
		}, err
	}

	// Record metrics
	if s.metrics != nil {
		s.metrics.parametersSet.Add(float64(len(req.Parameters)))
		s.metrics.bytesWritten.Add(float64(totalBytes))
		if hasEncryption {
			s.metrics.encryptedOps.Add(float64(len(req.Parameters)))
		}
	}

	return &interfaces.SetParametersResponse{
		Results:   results,
		Timestamp: time.Now(),
	}, nil
}

// DeleteParameters removes multiple parameters (batch delete).
func (s *parameterStoreService) DeleteParameters(ctx context.Context, req *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.WithLabelValues("batch_delete").Observe(time.Since(start).Seconds())
		}
	}()

	// Check if service is closed
	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	// Validate request
	if req == nil {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("batch_delete", "invalid_input").Inc()
		}
		return nil, api.NewNilRequestError("service.parameterstore.DeleteParameters")
	}

	if len(req.Paths) == 0 {
		return &interfaces.DeleteParametersResponse{
			Results:      []*interfaces.DeleteParameterResult{},
			DeletedCount: 0,
			Timestamp:    time.Now(),
		}, nil
	}

	// Prepare tenant context for storage scoping
	tenantID, err := paramPrepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Scope each path to the tenant namespace
	scopedPaths := make([]string, len(req.Paths))
	for i, path := range req.Paths {
		scopedPaths[i] = paramTenantScopedPath(tenantID, path)
	}

	// Set ABAC context for batch operation
	ctx = s.setParameterContext(ctx, "", "", iam.ParameterOperationBatchDelete, false, nil)
	paramCtx := iam.ParameterContextFromContext(ctx)
	if paramCtx != nil {
		paramCtx.BatchSize = len(req.Paths)
		ctx = iam.ContextWithParameterContext(ctx, paramCtx)
	}

	// Log operation
	s.logger.WithFields(map[string]interface{}{
		"batch_size": len(req.Paths),
	}).Debug("deleting parameters batch")

	// Delete batch from manager using tenant-scoped paths
	deleteResults, deleteErrors, err := s.paramManager.DeleteBatch(ctx, scopedPaths)
	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("batch_delete", "storage").Inc()
		}
		return nil, err
	}

	// Build results (return original paths to caller)
	results := make([]*interfaces.DeleteParameterResult, len(req.Paths))
	deletedCount := 0

	for i, path := range req.Paths {
		scopedPath := paramTenantScopedPath(tenantID, path)
		results[i] = &interfaces.DeleteParameterResult{
			Path:    path,
			Deleted: deleteResults[scopedPath],
		}
		if e, ok := deleteErrors[scopedPath]; ok {
			results[i].Error = e.Error()
		}
		if deleteResults[scopedPath] {
			deletedCount++
		}
	}

	// Record metrics
	if s.metrics != nil {
		s.metrics.parametersDeleted.Add(float64(deletedCount))
	}

	return &interfaces.DeleteParametersResponse{
		Results:      results,
		DeletedCount: deletedCount,
		Timestamp:    time.Now(),
	}, nil
}

// ListParameters returns parameters matching a path prefix.
func (s *parameterStoreService) ListParameters(ctx context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.WithLabelValues("list").Observe(time.Since(start).Seconds())
		}
	}()

	// Check if service is closed
	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	// Validate request
	if req == nil {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("list", "invalid_input").Inc()
		}
		return nil, api.NewNilRequestError("service.parameterstore.ListParameters")
	}

	// Set defaults
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	// Prepare tenant context for storage scoping
	tenantID, err := paramPrepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedPrefix := paramTenantListPrefix(tenantID) + req.Prefix

	// Set ABAC context (uses original prefix for ABAC/audit)
	ctx = s.setParameterContext(ctx, req.Prefix, "", iam.ParameterOperationList, false, nil)

	// Log operation
	s.logger.WithFields(map[string]interface{}{
		"prefix":    req.Prefix,
		"recursive": req.Recursive,
		"limit":     limit,
	}).Debug("listing parameters")

	// List from manager using tenant-scoped prefix
	paths, nextCursor, err := s.paramManager.List(ctx, scopedPrefix, req.Recursive, limit, req.Cursor)
	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("list", "storage").Inc()
		}
		return nil, err
	}

	// Optionally load values
	var params []*interfaces.ParameterValue
	if req.WithValues {
		results, _, err := s.paramManager.GetBatch(ctx, paths, req.WithDecryption)
		if err != nil {
			if s.metrics != nil {
				s.metrics.operationErrors.WithLabelValues("list", "storage").Inc()
			}
			return nil, err
		}

		params = make([]*interfaces.ParameterValue, 0, len(results))
		for scopedPath, result := range results {
			originalPath := paramTenantStripPath(tenantID, scopedPath)
			params = append(params, s.metadataToParameterValue(originalPath, result.Value, result.Metadata))
		}
	} else {
		// Return paths only (with minimal metadata), stripping tenant prefix
		params = make([]*interfaces.ParameterValue, len(paths))
		for i, scopedPath := range paths {
			params[i] = &interfaces.ParameterValue{
				Path: paramTenantStripPath(tenantID, scopedPath),
			}
		}
	}

	// Record metrics
	if s.metrics != nil {
		s.metrics.parametersListed.Inc()
	}

	return &interfaces.ListParametersResponse{
		Parameters: params,
		NextCursor: nextCursor,
		HasMore:    nextCursor != "",
		TotalCount: int64(len(params)),
		Timestamp:  time.Now(),
	}, nil
}

// SearchByTags finds parameters matching tag key-value pairs.
func (s *parameterStoreService) SearchByTags(ctx context.Context, req *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.WithLabelValues("search").Observe(time.Since(start).Seconds())
		}
	}()

	// Check if service is closed
	if err := s.checkClosed(); err != nil {
		return nil, err
	}

	// Validate request
	if req == nil {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("search", "invalid_input").Inc()
		}
		return nil, api.NewNilRequestError("service.parameterstore.SearchByTags")
	}

	if len(req.Tags) == 0 {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("search", "invalid_input").Inc()
		}
		return nil, api.NewRequiredFieldError("at least one tag")
	}

	// Set defaults
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	// Prepare tenant context for storage scoping
	tenantID, err := paramPrepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedPrefix := paramTenantListPrefix(tenantID) + req.Prefix

	// Set ABAC context (uses original prefix for ABAC/audit)
	ctx = s.setParameterContext(ctx, req.Prefix, "", iam.ParameterOperationSearch, false, nil)

	// Log operation
	s.logger.WithFields(map[string]interface{}{
		"tags":   req.Tags,
		"prefix": req.Prefix,
		"limit":  limit,
	}).Debug("searching parameters by tags")

	// Search from manager using tenant-scoped prefix
	paths, nextCursor, err := s.paramManager.SearchByTags(ctx, req.Tags, scopedPrefix, limit, req.Cursor)
	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.WithLabelValues("search", "storage").Inc()
		}
		return nil, err
	}

	// Optionally load values
	var params []*interfaces.ParameterValue
	if req.WithValues {
		results, _, err := s.paramManager.GetBatch(ctx, paths, req.WithDecryption)
		if err != nil {
			if s.metrics != nil {
				s.metrics.operationErrors.WithLabelValues("search", "storage").Inc()
			}
			return nil, err
		}

		params = make([]*interfaces.ParameterValue, 0, len(results))
		for scopedPath, result := range results {
			originalPath := paramTenantStripPath(tenantID, scopedPath)
			params = append(params, s.metadataToParameterValue(originalPath, result.Value, result.Metadata))
		}
	} else {
		// Return paths only, stripping tenant prefix
		params = make([]*interfaces.ParameterValue, len(paths))
		for i, scopedPath := range paths {
			params[i] = &interfaces.ParameterValue{
				Path: paramTenantStripPath(tenantID, scopedPath),
			}
		}
	}

	return &interfaces.SearchByTagsResponse{
		Parameters: params,
		NextCursor: nextCursor,
		HasMore:    nextCursor != "",
		Timestamp:  time.Now(),
	}, nil
}

// Close releases resources held by the service.
func (s *parameterStoreService) Close() error {
	if s.closed.Swap(true) {
		return api.NewAlreadyClosedError("service.parameterstore.Close", "service")
	}

	s.logger.Info("parameter store service closed")
	return nil
}

// =============================================================================
// Helper Methods
// =============================================================================

// validateSetRequest validates a set parameter request.
func (s *parameterStoreService) validateSetRequest(req *interfaces.SetParameterRequest) error {
	if req == nil {
		return api.NewNilRequestError("service.parameterstore.validateSetRequest")
	}

	if req.Path == "" {
		return api.NewRequiredFieldError("path")
	}

	if len(req.Value) == 0 {
		return api.NewRequiredFieldError("value")
	}

	// Validate path format
	if err := validateParameterPath(req.Path); err != nil {
		return err
	}

	return nil
}

// validateParameterPath validates the path format.
func validateParameterPath(path string) error {
	if path == "" {
		return api.NewRequiredFieldError("path")
	}

	// Path must start with /
	if path[0] != '/' {
		return api.NewInvalidPathError("service.parameterstore.validateParameterPath", "path must start with /")
	}

	// Path cannot end with /
	if len(path) > 1 && path[len(path)-1] == '/' {
		return api.NewInvalidPathError("service.parameterstore.validateParameterPath", "path cannot end with /")
	}

	// Path cannot contain //
	for i := 0; i < len(path)-1; i++ {
		if path[i] == '/' && path[i+1] == '/' {
			return api.NewInvalidPathError("service.parameterstore.validateParameterPath", "path cannot contain //")
		}
	}

	return nil
}

// buildMetadata builds parameter metadata from a set request.
func (s *parameterStoreService) buildMetadata(ctx context.Context, req *interfaces.SetParameterRequest) *interfaces.ParameterMetadata {
	now := time.Now()

	metadata := &interfaces.ParameterMetadata{
		Type:         req.Type,
		Description:  req.Description,
		Tags:         req.Tags,
		KMSKeyID:     req.KMSKeyID,
		IsEncrypted:  req.KMSKeyID != "",
		TTLSeconds:   req.TTLSeconds,
		AuditEnabled: req.AuditEnabled,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	// Set default type
	if metadata.Type == "" {
		metadata.Type = "string"
	}

	// Calculate expiration
	if req.TTLSeconds > 0 {
		expiresAt := now.Add(time.Duration(req.TTLSeconds) * time.Second)
		metadata.ExpiresAt = &expiresAt
	}

	// Extract user from context
	if userID := iam.UserFromContext(ctx); userID != "" {
		metadata.CreatedBy = userID
		metadata.UpdatedBy = userID
	}

	return metadata
}

// setParameterContext sets the ABAC parameter context on the context.
func (s *parameterStoreService) setParameterContext(ctx context.Context, path, paramType, operation string, isEncrypted bool, tags map[string]string) context.Context {
	paramCtx := &iam.ParameterContext{
		Path:        path,
		Type:        paramType,
		IsEncrypted: isEncrypted,
		Tags:        tags,
		Operation:   operation,
	}

	// Extract tenant from context
	if tenantID := iam.TenantFromContext(ctx); tenantID != "" {
		paramCtx.TenantID = tenantID
	}

	// Extract user from context
	if userID := iam.UserFromContext(ctx); userID != "" {
		paramCtx.Owner = userID
	}

	return iam.ContextWithParameterContext(ctx, paramCtx)
}

// metadataToParameterValue converts internal metadata to the interface type.
func (s *parameterStoreService) metadataToParameterValue(path string, value []byte, metadata *interfaces.ParameterMetadata) *interfaces.ParameterValue {
	pv := &interfaces.ParameterValue{
		Path:         path,
		Value:        value,
		Type:         metadata.Type,
		Description:  metadata.Description,
		Tags:         metadata.Tags,
		IsEncrypted:  metadata.IsEncrypted,
		KMSKeyID:     metadata.KMSKeyID,
		ExpiresAt:    metadata.ExpiresAt,
		AuditEnabled: metadata.AuditEnabled,
		CreatedAt:    metadata.CreatedAt,
		UpdatedAt:    metadata.UpdatedAt,
		CreatedBy:    metadata.CreatedBy,
		UpdatedBy:    metadata.UpdatedBy,
	}

	return pv
}
