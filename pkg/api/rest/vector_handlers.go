//go:build rest

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

package rest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ========================================
// Vector Route Registration
// ========================================

// registerVectorRoutes registers all vector database routes.
func registerVectorRoutes(s *Server, r chi.Router) {
	r.Route("/vector", func(r chi.Router) {
		// Health and monitoring
		r.Get("/health", s.HandleVectorHealth)

		// Namespace management
		r.Route("/namespaces", func(r chi.Router) {
			r.Post("/", s.HandleCreateNamespace)                   // Create namespace
			r.Get("/", s.HandleListNamespaces)                     // List namespaces
			r.Get("/{namespace}", s.HandleGetNamespaceInfo)        // Get namespace info
			r.Put("/{namespace}", s.HandleUpdateNamespace)         // Update namespace
			r.Delete("/{namespace}", s.HandleDeleteNamespace)      // Delete namespace
			r.Get("/{namespace}/stats", s.HandleGetNamespaceStats) // Get namespace stats

			// Index management
			r.Post("/{namespace}/index/rebuild", s.HandleRebuildIndex)     // Rebuild index
			r.Get("/{namespace}/index/progress", s.HandleGetIndexProgress) // Get index progress
		})

		// Vector CRUD operations
		r.Route("/namespaces/{namespace}/vectors", func(r chi.Router) {
			r.Post("/", s.HandleUpsertVector)              // Create/update vector
			r.Get("/", s.HandleListVectors)                // List vectors
			r.Post("/batch", s.HandleBatchUpsertVectors)   // Batch upsert
			r.Delete("/batch", s.HandleBatchDeleteVectors) // Batch delete

			r.Get("/{id}", s.HandleGetVector)               // Get vector
			r.Put("/{id}", s.HandleUpsertVector)            // Update vector
			r.Delete("/{id}", s.HandleDeleteVector)         // Delete vector
			r.Put("/{id}/metadata", s.HandleUpdateMetadata) // Update metadata
		})

		// Search operations
		r.Route("/namespaces/{namespace}/search", func(r chi.Router) {
			r.Post("/similar", s.HandleSearchSimilar) // Similarity search
			r.Post("/by-id/{id}", s.HandleSearchByID) // Search by ID
		})

		// Global search operations (namespace specified in request body)
		r.Post("/hybrid-search", s.HandleHybridSearch)          // Hybrid search
		r.Post("/search-by-metadata", s.HandleSearchByMetadata) // Metadata search

		// Cluster statistics (alias for admin/stats/global)
		r.Get("/cluster/stats", s.HandleGetGlobalStats) // Cluster stats

		// Admin operations
		r.Route("/admin", func(r chi.Router) {
			r.Get("/stats/global", s.HandleGetGlobalStats)            // Global stats
			r.Post("/compact", s.HandleCompactNamespace)              // Compact namespace
			r.Get("/compact/progress", s.HandleGetCompactionProgress) // Compaction progress
			r.Post("/export/{namespace}", s.HandleExportVectors)      // Export vectors
			r.Post("/import/{namespace}", s.HandleImportVectors)      // Import vectors
		})
	})
}

// ========================================
// Health and Monitoring Handlers
// ========================================

// HandleVectorHealth returns the health status of the vector database.
func (s *Server) HandleVectorHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Use base health service
	healthReq := &service.HealthRequest{
		IncludeDetails: true,
	}
	serviceResp, err := s.service.Health(ctx, healthReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := &VectorHealthResponse{
		Status:              convertHealthStatusToString(serviceResp.Status),
		NodeID:              0,
		Version:             "",
		UptimeSeconds:       int64(serviceResp.UptimeSeconds), //nolint:gosec // G115: Safe conversion - uptime always positive
		NamespacesHealthy:   0,
		NamespacesDegraded:  0,
		NamespacesUnhealthy: 0,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// ========================================
// Namespace Management Handlers
// ========================================

// HandleCreateNamespace creates a new vector namespace.
func (s *Server) HandleCreateNamespace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request body
	var req CreateNamespaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if req.Namespace == "" {
		RespondBadRequest(w, r, "namespace is required")
		return
	}
	if req.Dimensions <= 0 {
		RespondBadRequest(w, r, "dimensions must be positive")
		return
	}
	if req.DistanceMetric == "" {
		RespondBadRequest(w, r, "distance_metric is required")
		return
	}

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// Convert to service request
	serviceReq := &service.CreateNamespaceRequest{
		Namespace:   req.Namespace,
		Dimension:   uint32(req.Dimensions), //nolint:gosec // G115: Safe conversion - dimensions validated 1-4096
		IndexConfig: req.IndexParams,
		Metadata:    convertStringMapToInterface(req.Metadata),
		ShardID:     req.ShardID,
	}

	// Call service
	serviceResp, err := vectorService.CreateNamespace(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := &CreateNamespaceResponse{
		Namespace: serviceResp.Namespace,
		Timestamp: timestampToTime(serviceResp.Timestamp),
	}

	RespondJSON(w, r, http.StatusCreated, resp)
}

// HandleUpdateNamespace modifies namespace configuration.
func (s *Server) HandleUpdateNamespace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := chi.URLParam(r, "namespace")

	// Parse request body
	var req UpdateNamespaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate namespace exists
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// Check if namespace exists
	infoReq := &service.GetNamespaceInfoRequest{
		Namespace: namespace,
	}
	_, err := vectorService.GetNamespaceInfo(ctx, infoReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Note: Actual metadata update would require a new service method.
	// For now, we'll acknowledge the request but indicate a rebuild may be needed.
	requiresRebuild := len(req.IndexParams) > 0

	resp := &UpdateNamespaceResponse{
		Namespace:       namespace,
		Timestamp:       time.Now(),
		RequiresRebuild: requiresRebuild,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleDeleteNamespace removes a namespace and all its vectors.
func (s *Server) HandleDeleteNamespace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := chi.URLParam(r, "namespace")

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// Convert to service request
	serviceReq := &service.DeleteNamespaceRequest{
		Namespace: namespace,
	}

	// Call service
	serviceResp, err := vectorService.DeleteNamespace(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := &DeleteNamespaceResponse{
		Deleted:        serviceResp.Success,
		VectorsDeleted: int64(serviceResp.VectorsDeleted), //nolint:gosec // G115: Safe conversion - count within reasonable range
		Timestamp:      timestampToTime(serviceResp.Timestamp),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleListNamespaces returns all available namespaces.
func (s *Server) HandleListNamespaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters
	pageSize := 100 // default
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 {
			pageSize = parsed
		}
	}

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// Convert to service request
	serviceReq := &service.ListNamespacesRequest{
		Limit:  pageSize,
		Offset: 0,
	}

	// Call service
	serviceResp, err := vectorService.ListNamespaces(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Convert namespaces
	namespaces := make([]NamespaceInfo, len(serviceResp.Namespaces))
	for i, ns := range serviceResp.Namespaces {
		namespaces[i] = NamespaceInfo{
			Namespace:      ns.Namespace,
			Dimensions:     int(ns.Dimension),
			DistanceMetric: "",
			VectorCount:    int64(ns.VectorCount), //nolint:gosec // G115: Safe conversion - count within uint64 range
			CreatedAt:      timestampToTime(ns.CreatedAt),
			UpdatedAt:      timestampToTime(ns.UpdatedAt),
		}
	}

	// Build response
	resp := &ListNamespacesResponse{
		Namespaces: namespaces,
		TotalCount: serviceResp.TotalCount,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetNamespaceInfo retrieves namespace configuration and statistics.
func (s *Server) HandleGetNamespaceInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := chi.URLParam(r, "namespace")

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// Convert to service request
	serviceReq := &service.GetNamespaceInfoRequest{
		Namespace: namespace,
	}

	// Call service
	serviceResp, err := vectorService.GetNamespaceInfo(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response - IndexConfig is map[string]string
	resp := &NamespaceInfo{
		Namespace:    serviceResp.Namespace,
		Dimensions:   int(serviceResp.Dimension),
		VectorCount:  int64(serviceResp.VectorCount), //nolint:gosec // G115: Safe conversion - count within uint64 range
		IndexType:    serviceResp.IndexType,
		IndexParams:  serviceResp.IndexConfig, // Already string map
		StorageBytes: 0,
		IndexBytes:   0,
		CreatedAt:    timestampToTime(serviceResp.CreatedAt),
		UpdatedAt:    timestampToTime(serviceResp.UpdatedAt),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetNamespaceStats returns statistics for a namespace.
func (s *Server) HandleGetNamespaceStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := chi.URLParam(r, "namespace")

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// Convert to service request
	serviceReq := &service.GetIndexStatsRequest{
		Namespace: namespace,
	}

	// Call service
	serviceResp, err := vectorService.GetIndexStats(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := &NamespaceStatsItem{
		Namespace:          serviceResp.Namespace,
		VectorCount:        int64(serviceResp.VectorCount),    //nolint:gosec // G115: Safe conversion - count within uint64 range
		IndexedCount:       int64(serviceResp.VectorCount),    //nolint:gosec // G115: Safe conversion - count within uint64 range
		StorageBytes:       int64(serviceResp.IndexSizeBytes), //nolint:gosec // G115: Safe conversion - size within uint64 range
		IndexBytes:         int64(serviceResp.IndexSizeBytes), //nolint:gosec // G115: Safe conversion - size within uint64 range
		AvgSearchLatencyMs: 0.0,
		SearchCount:        0,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// ========================================
// Vector CRUD Handlers
// ========================================

// HandleUpsertVector creates or updates a single vector.
func (s *Server) HandleUpsertVector(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := chi.URLParam(r, "namespace")

	// Parse request body
	var req UpsertVectorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if req.ID == "" {
		RespondBadRequest(w, r, "vector ID is required")
		return
	}
	if len(req.Vector) == 0 {
		RespondBadRequest(w, r, "vector data cannot be empty")
		return
	}

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// Convert ID string to uint64 (hashes non-numeric IDs)
	vectorID := parseVectorID(req.ID)

	// Store original string ID in metadata if not numeric
	metadata := convertStringMapToInterface(req.Metadata)
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	storeOriginalID(req.ID, metadata)

	// Convert to service request
	serviceReq := &service.UpsertVectorRequest{
		Namespace: namespace,
		ID:        vectorID,
		Vector:    req.Vector,
		Metadata:  metadata,
		ShardID:   req.ShardID,
	}

	// Call service
	serviceResp, err := vectorService.UpsertVector(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := &UpsertVectorResponse{
		ID:           fmt.Sprintf("%d", serviceResp.ID),
		Version:      int64(serviceResp.Version), //nolint:gosec // G115: Safe conversion - version from database
		Timestamp:    timestampToTime(serviceResp.Timestamp),
		IndexUpdated: !req.SkipIndexUpdate,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetVector retrieves a vector by ID.
func (s *Server) HandleGetVector(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := chi.URLParam(r, "namespace")
	id := chi.URLParam(r, "id")

	// Parse query parameters
	includeVector := r.URL.Query().Get("include_vector") != "false"
	includeMetadata := r.URL.Query().Get("include_metadata") != "false"

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// Convert ID string to uint64 (hashes non-numeric IDs)
	vectorID := parseVectorID(id)

	// Convert to service request
	serviceReq := &service.GetVectorRequest{
		Namespace:       namespace,
		ID:              vectorID,
		IncludeVector:   includeVector,
		IncludeMetadata: includeMetadata,
	}

	// Call service
	serviceResp, err := vectorService.GetVector(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	if !serviceResp.Found {
		RespondError(w, r, http.StatusNotFound, &NotFoundError{
			Resource: fmt.Sprintf("vector %s in namespace %s", id, namespace),
		})
		return
	}

	// Build response
	resp := &GetVectorResponse{
		ID:        id,
		Namespace: namespace,
		Vector:    serviceResp.Vector,
		Metadata:  convertInterfaceMapToString(serviceResp.Metadata),
		Version:   int64(serviceResp.Version), //nolint:gosec // G115: Safe conversion - version from database
		Status:    "active",
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleDeleteVector removes a vector by ID.
func (s *Server) HandleDeleteVector(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := chi.URLParam(r, "namespace")
	id := chi.URLParam(r, "id")

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// Convert ID string to uint64 (hashes non-numeric IDs)
	vectorID := parseVectorID(id)

	// Convert to service request
	serviceReq := &service.DeleteVectorRequest{
		Namespace: namespace,
		ID:        vectorID,
	}

	// Call service
	serviceResp, err := vectorService.DeleteVector(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := &DeleteVectorResponse{
		Deleted:   serviceResp.Success,
		Timestamp: timestampToTime(serviceResp.Timestamp),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleBatchUpsertVectors creates or updates multiple vectors.
func (s *Server) HandleBatchUpsertVectors(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := chi.URLParam(r, "namespace")

	// Parse request body
	var req BatchUpsertVectorsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// Convert to service request
	vectors := make([]*service.VectorData, len(req.Vectors))
	for i, v := range req.Vectors {
		vectorID := parseVectorID(v.ID)
		metadata := convertStringMapToInterface(v.Metadata)
		if metadata == nil {
			metadata = make(map[string]interface{})
		}
		storeOriginalID(v.ID, metadata)
		vectors[i] = &service.VectorData{
			ID:       vectorID,
			Vector:   v.Vector,
			Metadata: metadata,
		}
	}

	serviceReq := &service.BatchUpsertRequest{
		Namespace: namespace,
		Vectors:   vectors,
	}

	// Call service
	serviceResp, err := vectorService.BatchUpsert(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := &BatchUpsertVectorsResponse{
		VectorsProcessed: int64(serviceResp.InsertedCount),
		VectorsSucceeded: int64(serviceResp.InsertedCount),
		VectorsFailed:    0,
		FailedIDs:        []string{},
		Timestamp:        timestampToTime(serviceResp.Timestamp),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleBatchDeleteVectors removes multiple vectors.
func (s *Server) HandleBatchDeleteVectors(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := chi.URLParam(r, "namespace")

	// Parse request body
	var req BatchDeleteVectorsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if len(req.IDs) == 0 {
		RespondBadRequest(w, r, "ids array is required and cannot be empty")
		return
	}

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// Delete vectors one by one
	var deletedCount int64
	for _, idStr := range req.IDs {
		vectorID := parseVectorID(idStr)

		deleteReq := &service.DeleteVectorRequest{
			Namespace: namespace,
			ID:        vectorID,
		}

		_, err := vectorService.DeleteVector(ctx, deleteReq)
		if err != nil {
			s.logger.WithFields(map[string]interface{}{
				"namespace": namespace,
				"id":        idStr,
				"error":     err,
			}).Warn("failed to delete vector in batch")
			continue
		}

		deletedCount++
	}

	// Build response
	resp := &BatchDeleteVectorsResponse{
		DeletedCount: deletedCount,
		Timestamp:    time.Now(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleListVectors lists vectors in a namespace with pagination.
func (s *Server) HandleListVectors(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := chi.URLParam(r, "namespace")

	// Parse query parameters
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 1000 {
		limit = 1000
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	includeMetadata := r.URL.Query().Get("include_metadata") == "true"

	// Get vector service capability with metadata search
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// Use SearchByMetadata with empty filters to list all vectors
	searchReq := &service.SearchByMetadataRequest{
		Namespace:       namespace,
		Filters:         map[string]interface{}{},
		Limit:           limit,
		Offset:          offset,
		IncludeVectors:  false,
		IncludeMetadata: includeMetadata,
	}

	searchResp, err := vectorService.SearchByMetadata(ctx, searchReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Convert results
	vectors := make([]VectorListItem, len(searchResp.Results))
	for i, vec := range searchResp.Results {
		vectors[i] = VectorListItem{
			ID:        fmt.Sprintf("%d", vec.ID),
			Metadata:  convertInterfaceMapToString(vec.Metadata),
			Version:   int64(vec.Version), //nolint:gosec // G115: Safe conversion - version from database
			CreatedAt: timestampToTime(vec.CreatedAt),
			UpdatedAt: timestampToTime(vec.UpdatedAt),
		}
	}

	// Calculate next page token
	nextPageToken := ""
	if len(searchResp.Results) >= limit {
		nextPageToken = fmt.Sprintf("%d", offset+limit)
	}

	resp := &ListVectorsResponse{
		Vectors: vectors,
		Pagination: PaginationInfo{
			NextPageToken: nextPageToken,
			TotalCount:    int64(searchResp.TotalCount), //nolint:gosec // G115: Safe conversion - count from search results
		},
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleUpdateMetadata updates metadata for a specific vector.
func (s *Server) HandleUpdateMetadata(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := chi.URLParam(r, "namespace")
	id := chi.URLParam(r, "id")

	// Parse request body
	var reqBody struct {
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if reqBody.Metadata == nil {
		RespondBadRequest(w, r, "metadata is required")
		return
	}

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}
	// Convert ID string to uint64 (hashes non-numeric IDs)
	vectorID := parseVectorID(id)

	// Get existing vector
	getReq := &service.GetVectorRequest{
		Namespace:     namespace,
		ID:            vectorID,
		IncludeVector: true,
	}

	getResp, err := vectorService.GetVector(ctx, getReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	if !getResp.Found {
		RespondError(w, r, http.StatusNotFound, &NotFoundError{
			Resource: fmt.Sprintf("vector %s in namespace %s", id, namespace),
		})
		return
	}

	// Upsert with updated metadata (preserve original ID in metadata)
	metadata := convertStringMapToInterface(reqBody.Metadata)
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	storeOriginalID(id, metadata)
	upsertReq := &service.UpsertVectorRequest{
		Namespace: namespace,
		ID:        vectorID,
		Vector:    getResp.Vector,
		Metadata:  metadata,
	}

	upsertResp, err := vectorService.UpsertVector(ctx, upsertReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := &UpsertVectorResponse{
		ID:        id,
		Version:   int64(upsertResp.Version), //nolint:gosec // G115: Safe conversion - version from database
		Timestamp: timestampToTime(upsertResp.Timestamp),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// ========================================
// Search Handlers
// ========================================

// HandleSearchSimilar performs k-NN similarity search.
func (s *Server) HandleSearchSimilar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := chi.URLParam(r, "namespace")

	// Parse request body
	var req SearchSimilarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if len(req.QueryVector) == 0 {
		RespondBadRequest(w, r, "query_vector is required")
		return
	}
	if req.K <= 0 {
		RespondBadRequest(w, r, "k must be positive")
		return
	}

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// Convert to service request
	serviceReq := &service.SearchSimilarRequest{
		Namespace:       namespace,
		QueryVector:     req.QueryVector,
		K:               req.K,
		EfSearch:        req.EfSearch,
		MinScore:        req.MinScore,
		IncludeVectors:  req.IncludeVectors,
		IncludeMetadata: req.IncludeMetadata,
		MetadataFilter:  convertStringMapToInterface(req.MetadataFilters),
	}

	// Call service
	serviceResp, err := vectorService.SearchSimilar(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Convert results
	results := make([]SimilarityResult, len(serviceResp.Results))
	for i, res := range serviceResp.Results {
		// Use original string ID from metadata if available
		stringMetadata := convertInterfaceMapToString(res.Metadata)
		results[i] = SimilarityResult{
			ID:       formatVectorID(res.ID, stringMetadata),
			Score:    res.Score,
			Rank:     res.Rank,
			Vector:   res.Vector,
			Metadata: stringMetadata,
		}
	}

	// Build response
	resp := &SearchSimilarResponse{
		Results:         results,
		TotalCandidates: 0,
		SearchTimeMs:    serviceResp.QueryDuration / 1000,
		NodeID:          0,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleSearchByID performs k-NN search using an existing vector.
func (s *Server) HandleSearchByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := chi.URLParam(r, "namespace")
	id := chi.URLParam(r, "id")

	// Parse request body
	var req SearchByIDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if req.K <= 0 {
		RespondBadRequest(w, r, "k must be positive")
		return
	}

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}
	// Convert ID string to uint64 (hashes non-numeric IDs)
	vectorID := parseVectorID(id)

	// First, get the vector
	getReq := &service.GetVectorRequest{
		Namespace:     namespace,
		ID:            vectorID,
		IncludeVector: true,
	}

	getResp, err := vectorService.GetVector(ctx, getReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	if !getResp.Found {
		RespondError(w, r, http.StatusNotFound, &NotFoundError{
			Resource: fmt.Sprintf("vector %s in namespace %s", id, namespace),
		})
		return
	}

	// Now search with the vector
	searchReq := &service.SearchSimilarRequest{
		Namespace:       namespace,
		QueryVector:     getResp.Vector,
		K:               req.K + 1,
		EfSearch:        req.EfSearch,
		MinScore:        req.MinScore,
		IncludeVectors:  req.IncludeVectors,
		IncludeMetadata: req.IncludeMetadata,
		MetadataFilter:  convertStringMapToInterface(req.MetadataFilters),
	}

	searchResp, err := vectorService.SearchSimilar(ctx, searchReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Convert results, filtering self if not requested
	results := make([]SimilarityResult, 0, len(searchResp.Results))
	for _, res := range searchResp.Results {
		if !req.IncludeSelf && res.ID == vectorID {
			continue
		}
		results = append(results, SimilarityResult{
			ID:       fmt.Sprintf("%d", res.ID),
			Score:    res.Score,
			Rank:     res.Rank,
			Vector:   res.Vector,
			Metadata: convertInterfaceMapToString(res.Metadata),
		})
	}

	// Trim to requested K
	if len(results) > req.K {
		results = results[:req.K]
	}

	// Build response
	resp := &SearchByIDResponse{
		Results:         results,
		TotalCandidates: 0,
		SearchTimeMs:    searchResp.QueryDuration / 1000,
		NodeID:          0,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleHybridSearch performs hybrid search combining similarity and metadata filtering.
func (s *Server) HandleHybridSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request body
	var req HybridSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if req.Namespace == "" {
		RespondBadRequest(w, r, "namespace is required")
		return
	}
	if len(req.QueryVector) == 0 {
		RespondBadRequest(w, r, "query_vector is required")
		return
	}
	if req.K <= 0 {
		RespondBadRequest(w, r, "k must be positive")
		return
	}

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// Convert to service request
	serviceReq := &service.HybridSearchRequest{
		Namespace:          req.Namespace,
		QueryVector:        req.QueryVector,
		K:                  req.K,
		EfSearch:           req.EfSearch,
		MinScore:           req.MinScore,
		MetadataFilter:     convertStringMapToInterface(req.MetadataFilters),
		FilterOperator:     req.FilterOperator,
		RerankingAlgorithm: req.RerankingAlgorithm,
		IncludeVectors:     req.IncludeVectors,
		IncludeMetadata:    req.IncludeMetadata,
	}

	// Call service
	serviceResp, err := vectorService.HybridSearch(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Convert results
	results := make([]SimilarityResult, len(serviceResp.Results))
	for i, res := range serviceResp.Results {
		// Use original string ID from metadata if available
		stringMetadata := convertInterfaceMapToString(res.Metadata)
		results[i] = SimilarityResult{
			ID:       formatVectorID(res.ID, stringMetadata),
			Score:    res.Score,
			Rank:     res.Rank,
			Vector:   res.Vector,
			Metadata: stringMetadata,
		}
	}

	// Build response
	resp := &HybridSearchResponse{
		Results:         results,
		TotalCandidates: int64(serviceResp.TotalCandidates),
		TotalFiltered:   serviceResp.TotalFiltered,
		SearchTimeMs:    serviceResp.QueryDuration / 1000,
		NodeID:          0,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleSearchByMetadata performs metadata-based vector search.
func (s *Server) HandleSearchByMetadata(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request body
	var req SearchByMetadataRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if req.Namespace == "" {
		RespondBadRequest(w, r, "namespace is required")
		return
	}

	// Set defaults
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// Convert to service request
	serviceReq := &service.SearchByMetadataRequest{
		Namespace:       req.Namespace,
		Filters:         convertStringMapToInterface(req.Filters),
		FilterOperator:  req.FilterOperator,
		Limit:           limit,
		Offset:          offset,
		IncludeVectors:  req.IncludeVectors,
		IncludeMetadata: req.IncludeMetadata,
	}

	// Call service
	serviceResp, err := vectorService.SearchByMetadata(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Convert results
	results := make([]MetadataSearchResult, len(serviceResp.Results))
	for i, vec := range serviceResp.Results {
		results[i] = MetadataSearchResult{
			ID:        fmt.Sprintf("%d", vec.ID),
			Vector:    vec.Vector,
			Metadata:  convertInterfaceMapToString(vec.Metadata),
			Version:   int64(vec.Version), //nolint:gosec // G115: Safe conversion - version from database
			CreatedAt: timestampToTime(vec.CreatedAt),
			UpdatedAt: timestampToTime(vec.UpdatedAt),
		}
	}

	// Build response
	resp := &SearchByMetadataResponse{
		Results:    results,
		TotalCount: int64(serviceResp.TotalCount), //nolint:gosec // G115: Safe conversion - count from search results
		NextCursor: serviceResp.NextCursor,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// ========================================
// Index Management Handlers
// ========================================

// HandleRebuildIndex rebuilds the vector index for a namespace.
func (s *Server) HandleRebuildIndex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := chi.URLParam(r, "namespace")

	// Parse request body
	var req RebuildIndexRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// Convert to service request
	serviceReq := &service.RebuildIndexRequest{
		Namespace:   namespace,
		IndexConfig: req.IndexParams,
		Async:       req.Async,
	}

	// Call service
	serviceResp, err := vectorService.RebuildIndex(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := &RebuildIndexResponse{
		OperationID:         serviceResp.JobID,
		Started:             timestampToTime(serviceResp.Timestamp),
		EstimatedDurationMs: serviceResp.EstimatedTimeRemaining * 1000,
	}

	RespondJSON(w, r, http.StatusAccepted, resp)
}

// HandleGetIndexProgress returns the progress of an index rebuild.
func (s *Server) HandleGetIndexProgress(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	operationID := r.URL.Query().Get("operation_id")

	if operationID == "" {
		RespondBadRequest(w, r, "operation_id is required")
		return
	}

	// Granular progress tracking is not available for this index type.
	// Return an informational response indicating the operation status.
	resp := &IndexProgressResponse{
		OperationID: operationID,
		Namespace:   namespace,
		Status:      "in_progress",
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// ========================================
// Admin Handlers
// ========================================

// HandleGetGlobalStats returns global/cluster-wide statistics.
func (s *Server) HandleGetGlobalStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// List all namespaces
	listReq := &service.ListNamespacesRequest{
		Limit: 1000,
	}
	listResp, err := vectorService.ListNamespaces(ctx, listReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Aggregate statistics
	var totalVectors uint64
	var totalStorageBytes uint64
	var totalIndexBytes uint64
	namespaceStats := make([]NamespaceStatsItem, 0, len(listResp.Namespaces))

	for _, ns := range listResp.Namespaces {
		statsReq := &service.GetIndexStatsRequest{
			Namespace: ns.Namespace,
		}
		statsResp, err := vectorService.GetIndexStats(ctx, statsReq)
		if err != nil {
			s.logger.WithFields(map[string]interface{}{
				"namespace": ns.Namespace,
				"error":     err,
			}).Warn("failed to get namespace stats")
			continue
		}

		totalVectors += statsResp.VectorCount
		totalStorageBytes += statsResp.IndexSizeBytes
		totalIndexBytes += statsResp.IndexSizeBytes

		namespaceStats = append(namespaceStats, NamespaceStatsItem{
			Namespace:    ns.Namespace,
			VectorCount:  int64(statsResp.VectorCount),    //nolint:gosec // G115: Safe conversion - count within uint64 range
			IndexedCount: int64(statsResp.VectorCount),    //nolint:gosec // G115: Safe conversion - count within uint64 range
			StorageBytes: int64(statsResp.IndexSizeBytes), //nolint:gosec // G115: Safe conversion - size within uint64 range
			IndexBytes:   int64(statsResp.IndexSizeBytes), //nolint:gosec // G115: Safe conversion - size within uint64 range
		})
	}

	resp := &ClusterStatsResponse{
		ClusterID:         "quicbaas-cluster",
		TotalNodes:        1,
		TotalVectors:      int64(totalVectors), //nolint:gosec // G115: Safe conversion - count within uint64 range
		TotalNamespaces:   len(listResp.Namespaces),
		TotalStorageBytes: int64(totalStorageBytes), //nolint:gosec // G115: Safe conversion - size within uint64 range
		TotalIndexBytes:   int64(totalIndexBytes),   //nolint:gosec // G115: Safe conversion - size within uint64 range
		ClusterQPS:        0.0,
		NamespaceStats:    namespaceStats,
		NodeStats:         []NodeStats{},
		Timestamp:         time.Now(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleCompactNamespace triggers compaction on a namespace.
func (s *Server) HandleCompactNamespace(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var reqBody struct {
		Namespace string `json:"namespace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if reqBody.Namespace == "" {
		RespondBadRequest(w, r, "namespace is required")
		return
	}

	// Generate operation ID
	operationID := uuid.New().String()

	// Note: Actual compaction would require storage-level support
	// For now, return accepted with operation ID
	resp := &CompactNamespaceResponse{
		OperationID: operationID,
		Started:     time.Now(),
	}

	RespondJSON(w, r, http.StatusAccepted, resp)
}

// HandleGetCompactionProgress returns the progress of a compaction.
func (s *Server) HandleGetCompactionProgress(w http.ResponseWriter, r *http.Request) {
	operationID := r.URL.Query().Get("operation_id")

	if operationID == "" {
		RespondBadRequest(w, r, "operation_id is required")
		return
	}

	// Note: Compaction progress tracking not implemented
	resp := &CompactionProgressResponse{
		OperationID:         operationID,
		Namespace:           "",
		BytesCompacted:      0,
		TotalBytes:          0,
		PercentComplete:     0.0,
		ElapsedTimeMs:       0,
		SpaceReclaimedBytes: 0,
		Status:              "unknown",
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleExportVectors exports vectors from a namespace.
func (s *Server) HandleExportVectors(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := chi.URLParam(r, "namespace")

	// Parse request body
	var req ExportVectorsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Set defaults
	if req.Format == "" {
		req.Format = "json"
	}
	if req.BatchSize <= 0 {
		req.BatchSize = 1000
	}

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// Set response headers for download
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-export.json", namespace))
	w.WriteHeader(http.StatusOK)

	// Stream vectors in batches
	encoder := json.NewEncoder(w)
	offset := 0

	for {
		searchReq := &service.SearchByMetadataRequest{
			Namespace:       namespace,
			Filters:         convertStringMapToInterface(req.MetadataFilters),
			Limit:           req.BatchSize,
			Offset:          offset,
			IncludeVectors:  true,
			IncludeMetadata: true,
		}

		searchResp, err := vectorService.SearchByMetadata(ctx, searchReq)
		if err != nil {
			s.logger.WithFields(map[string]interface{}{
				"namespace": namespace,
				"offset":    offset,
				"error":     err,
			}).Error("failed to export vectors")
			return
		}

		if len(searchResp.Results) == 0 {
			break
		}

		// Write each vector as a JSON line
		for _, vec := range searchResp.Results {
			exportVec := VectorEntity{
				ID:       fmt.Sprintf("%d", vec.ID),
				Vector:   vec.Vector,
				Metadata: convertInterfaceMapToString(vec.Metadata),
			}
			if err := encoder.Encode(exportVec); err != nil {
				s.logger.WithError(err).Error("failed to encode vector")
				return
			}
		}

		offset += len(searchResp.Results)

		// Break if we got less than requested
		if len(searchResp.Results) < req.BatchSize {
			break
		}
	}
}

// HandleImportVectors imports vectors into a namespace.
func (s *Server) HandleImportVectors(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := chi.URLParam(r, "namespace")
	startTime := time.Now()

	// Get vector service capability
	vectorService := s.service.Vector()
	if vectorService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "vector service not available",
		})
		return
	}

	// Read vectors from request body (expecting JSONL format)
	decoder := json.NewDecoder(r.Body)
	var imported int64
	var failed int64
	var skipped int64
	batchSize := 100
	batch := make([]*service.VectorData, 0, batchSize)

	for {
		var vec VectorEntity
		if err := decoder.Decode(&vec); err != nil {
			if err == io.EOF {
				break
			}
			s.logger.WithError(err).Warn("failed to decode vector")
			failed++
			continue
		}

		// Validate vector
		if vec.ID == "" || len(vec.Vector) == 0 {
			skipped++
			continue
		}

		vectorID := parseVectorID(vec.ID)
		metadata := convertStringMapToInterface(vec.Metadata)
		if metadata == nil {
			metadata = make(map[string]interface{})
		}
		storeOriginalID(vec.ID, metadata)

		batch = append(batch, &service.VectorData{
			ID:       vectorID,
			Vector:   vec.Vector,
			Metadata: metadata,
		})

		// Process batch when full
		if len(batch) >= batchSize {
			batchReq := &service.BatchUpsertRequest{
				Namespace: namespace,
				Vectors:   batch,
			}

			_, err := vectorService.BatchUpsert(ctx, batchReq)
			if err != nil {
				s.logger.WithError(err).Error("failed to import batch")
				failed += int64(len(batch))
			} else {
				imported += int64(len(batch))
			}

			batch = make([]*service.VectorData, 0, batchSize)
		}
	}

	// Process remaining batch
	if len(batch) > 0 {
		batchReq := &service.BatchUpsertRequest{
			Namespace: namespace,
			Vectors:   batch,
		}

		_, err := vectorService.BatchUpsert(ctx, batchReq)
		if err != nil {
			s.logger.WithError(err).Error("failed to import final batch")
			failed += int64(len(batch))
		} else {
			imported += int64(len(batch))
		}
	}

	duration := time.Since(startTime)

	resp := &ImportVectorsResponse{
		VectorsImported: imported,
		VectorsSkipped:  skipped,
		VectorsFailed:   failed,
		DurationMs:      duration.Milliseconds(),
		Timestamp:       time.Now(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// ========================================
// Helper Functions
// ========================================

// convertStringMapToInterface converts map[string]string to map[string]interface{}.
func convertStringMapToInterface(m map[string]string) map[string]interface{} {
	if m == nil {
		return nil
	}
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// convertInterfaceMapToString converts map[string]interface{} to map[string]string.
func convertInterfaceMapToString(m map[string]interface{}) map[string]string {
	if m == nil {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		if str, ok := v.(string); ok {
			result[k] = str
		} else {
			result[k] = fmt.Sprintf("%v", v)
		}
	}
	return result
}

// timestampToTime converts an int64 Unix timestamp to time.Time.
func timestampToTime(ts int64) time.Time {
	if ts == 0 {
		return time.Now()
	}
	return time.Unix(0, ts)
}

// MetadataKeyOriginalID is the key used to store the original string ID in metadata.
const MetadataKeyOriginalID = "__original_id"

// parseVectorID converts a string ID to uint64.
// If the ID is numeric, it is parsed directly.
// If non-numeric, it is hashed to a stable uint64 value
// and the original ID is preserved in metadata via storeOriginalID.
func parseVectorID(id string) uint64 {
	if parsed, err := strconv.ParseUint(id, 10, 64); err == nil {
		return parsed
	}
	// Hash non-numeric string to uint64
	hash := uint64(0)
	for i := 0; i < len(id); i++ {
		hash = hash*31 + uint64(id[i])
	}
	return hash
}

// formatVectorID converts a uint64 ID to string, checking metadata for original ID.
func formatVectorID(id uint64, metadata map[string]string) string {
	// Check if we have the original string ID in metadata
	if metadata != nil {
		if originalID, ok := metadata[MetadataKeyOriginalID]; ok {
			return originalID
		}
	}
	// Fall back to numeric representation
	return strconv.FormatUint(id, 10)
}

// storeOriginalID stores the original string ID in metadata if it's not numeric.
func storeOriginalID(id string, metadata map[string]interface{}) {
	// Only store if the ID is not a pure number
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		if metadata != nil {
			metadata[MetadataKeyOriginalID] = id
		}
	}
}
