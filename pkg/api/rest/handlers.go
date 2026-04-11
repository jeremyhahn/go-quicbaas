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
	stderrors "errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// HandleBatchPut handles atomic batch put operations.
func (s *Server) HandleBatchPut(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "failed to read request body",
		})
		return
	}
	defer r.Body.Close()

	// Parse batch request
	var batchReq BatchPutRequest
	if err := json.Unmarshal(body, &batchReq); err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "invalid JSON body",
		})
		return
	}

	// Validate batch
	if len(batchReq.Operations) == 0 {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "batch operations cannot be empty",
		})
		return
	}

	// Convert to service batch request
	serviceReq := &service.BatchPutRequest{
		Requests: make([]*service.PutRequest, 0, len(batchReq.Operations)),
	}

	for _, op := range batchReq.Operations {
		// Validate operation
		if op.EntityType == "" {
			RespondError(w, r, http.StatusBadRequest, &ValidationError{
				Field:   "entity_type",
				Message: "entity_type is required for all operations",
			})
			return
		}
		if op.ID == 0 {
			RespondError(w, r, http.StatusBadRequest, &ValidationError{
				Field:   "id",
				Message: "id is required for all operations",
			})
			return
		}

		// Add to service request
		serviceReq.Requests = append(serviceReq.Requests, &service.PutRequest{
			EntityType:  op.EntityType,
			ID:          op.ID,
			Data:        op.Data,
			Consistency: service.ConsistencyLevelLinearizable,
			WriteMode:   service.WriteModeBatched,
		})
	}

	// Call service
	serviceResp, err := s.service.BatchPut(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	responses := make([]PutResponse, 0, len(serviceResp.Responses))
	for i, resp := range serviceResp.Responses {
		responses = append(responses, PutResponse{
			ID:         resp.ID,
			EntityType: batchReq.Operations[i].EntityType,
			Created:    true,
			Timestamp:  time.Unix(0, resp.Timestamp),
		})
	}

	resp := BatchPutResponse{
		Success:   true,
		Responses: responses,
		Timestamp: serviceResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleBatchGet handles batch get operations for retrieving multiple entities.
func (s *Server) HandleBatchGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "failed to read request body",
		})
		return
	}
	defer r.Body.Close()

	// Parse batch request
	var batchReq BatchGetRequest
	if err := json.Unmarshal(body, &batchReq); err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "invalid JSON body",
		})
		return
	}

	// Validate batch
	if len(batchReq.Operations) == 0 {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "batch operations cannot be empty",
		})
		return
	}

	// Validate consistency level if provided
	if batchReq.ConsistencyLevel != "" {
		if err := ConsistencyLevel(batchReq.ConsistencyLevel).Validate(); err != nil {
			RespondError(w, r, http.StatusBadRequest, err)
			return
		}
	}

	// Convert consistency level
	consistencyLevel := convertToServiceConsistency(ConsistencyLevel(batchReq.ConsistencyLevel))

	// Convert to service batch request
	serviceReq := &service.BatchGetRequest{
		Requests: make([]*service.GetRequest, 0, len(batchReq.Operations)),
	}

	for _, op := range batchReq.Operations {
		// Validate operation
		if op.EntityType == "" {
			RespondError(w, r, http.StatusBadRequest, &ValidationError{
				Field:   "entity_type",
				Message: "entity_type is required for all operations",
			})
			return
		}
		if op.ID == 0 {
			RespondError(w, r, http.StatusBadRequest, &ValidationError{
				Field:   "id",
				Message: "id is required for all operations",
			})
			return
		}

		// Add to service request
		serviceReq.Requests = append(serviceReq.Requests, &service.GetRequest{
			EntityType:  op.EntityType,
			ID:          op.ID,
			Consistency: consistencyLevel,
			TimeoutMs:   5000, // Default 5s read timeout per item
		})
	}

	// Call service
	serviceResp, err := s.service.BatchGet(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	responses := make([]BatchGetItemResponse, 0, len(serviceResp.Responses))
	for i, resp := range serviceResp.Responses {
		item := BatchGetItemResponse{
			ID:         batchReq.Operations[i].ID,
			EntityType: batchReq.Operations[i].EntityType,
			Found:      resp.Found,
		}
		if resp.Found {
			item.Data = json.RawMessage(resp.Data)
			item.Timestamp = time.Unix(0, resp.Timestamp)
		}
		responses = append(responses, item)
	}

	resp := BatchGetResponse{
		Success:   true,
		Responses: responses,
		Timestamp: time.Now().UnixNano(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleBatchDelete handles atomic batch delete operations.
func (s *Server) HandleBatchDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "failed to read request body",
		})
		return
	}
	defer r.Body.Close()

	// Parse batch request
	var batchReq BatchDeleteRequest
	if err := json.Unmarshal(body, &batchReq); err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "invalid JSON body",
		})
		return
	}

	// Validate batch
	if len(batchReq.Operations) == 0 {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "batch operations cannot be empty",
		})
		return
	}

	// Convert to service batch request
	serviceReq := &service.BatchDeleteRequest{
		Requests: make([]*service.DeleteRequest, 0, len(batchReq.Operations)),
	}

	for _, op := range batchReq.Operations {
		// Validate operation
		if op.EntityType == "" {
			RespondError(w, r, http.StatusBadRequest, &ValidationError{
				Field:   "entity_type",
				Message: "entity_type is required for all operations",
			})
			return
		}
		if op.ID == 0 {
			RespondError(w, r, http.StatusBadRequest, &ValidationError{
				Field:   "id",
				Message: "id is required for all operations",
			})
			return
		}

		// Add to service request
		serviceReq.Requests = append(serviceReq.Requests, &service.DeleteRequest{
			EntityType:  op.EntityType,
			ID:          op.ID,
			Consistency: service.ConsistencyLevelLinearizable,
		})
	}

	// Call service
	serviceResp, err := s.service.BatchDelete(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	responses := make([]DeleteResponse, 0, len(serviceResp.Responses))
	for i, resp := range serviceResp.Responses {
		responses = append(responses, DeleteResponse{
			ID:         batchReq.Operations[i].ID,
			EntityType: batchReq.Operations[i].EntityType,
			Deleted:    resp.Deleted,
			Timestamp:  time.Unix(0, resp.Timestamp),
		})
	}

	resp := BatchDeleteResponse{
		Success:   true,
		Responses: responses,
		Timestamp: serviceResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandlePut handles PUT and POST requests to create or update entities.
func (s *Server) HandlePut(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract entity type from URL
	entityType := chi.URLParam(r, "type")
	if entityType == "" {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "type",
			Message: "entity type is required",
		})
		return
	}

	// Extract entity ID from URL (for PUT) or body (for POST)
	var entityID uint64
	idParam := chi.URLParam(r, "id")
	if idParam != "" {
		var err error
		entityID, err = strconv.ParseUint(idParam, 10, 64)
		if err != nil {
			RespondError(w, r, http.StatusBadRequest, &ValidationError{
				Field:   "id",
				Message: "invalid entity ID",
			})
			return
		}
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "failed to read request body",
		})
		return
	}
	defer r.Body.Close()

	// Parse request
	var req PutRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "invalid JSON body",
		})
		return
	}

	// Override with URL params
	req.EntityType = entityType
	if entityID > 0 {
		req.ID = entityID
	}

	// Validate consistency level
	if req.ConsistencyLevel != "" {
		if err := ConsistencyLevel(req.ConsistencyLevel).Validate(); err != nil {
			RespondError(w, r, http.StatusBadRequest, err)
			return
		}
	}

	// Get write mode from query parameter (default: sync)
	writeModeStr := r.URL.Query().Get("write_mode")
	writeMode := service.WriteModeSync // Default
	if writeModeStr != "" {
		writeMode = convertWriteMode(writeModeStr)
	}

	// Convert to service request
	shardID := s.shardRouter.Route(req.EntityType, req.ID)
	serviceReq := &service.PutRequest{
		EntityType:  req.EntityType,
		ID:          req.ID,
		Data:        req.Data,
		Consistency: convertToServiceConsistency(ConsistencyLevel(req.ConsistencyLevel)),
		WriteMode:   writeMode,
	}

	// Expose shard and record metrics
	if s.config.ExposeShardHeader {
		w.Header().Set("X-QuicBaaS-Shard", fmt.Sprintf("%d", shardID))
		w.Header().Set("X-QuicBaaS-Write-Mode", writeModeStr)
	}
	incRestShardRequest(shardID, req.EntityType, "put")

	// Call service
	serviceResp, err := s.service.Put(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Determine if this was a create or update
	created := req.ID == 0 || idParam == ""

	// Build response
	resp := PutResponse{
		ID:         serviceResp.ID,
		EntityType: req.EntityType,
		Created:    created,
		Timestamp:  time.Unix(0, serviceResp.Timestamp),
	}

	// Respond with appropriate status code
	statusCode := http.StatusOK
	if created {
		statusCode = http.StatusCreated
	}

	RespondJSON(w, r, statusCode, resp)
}

// HandleGet handles GET requests to retrieve a single entity.
func (s *Server) HandleGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract entity type and ID from URL
	entityType := chi.URLParam(r, "type")
	idParam := chi.URLParam(r, "id")

	if entityType == "" || idParam == "" {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "entity type and ID are required",
		})
		return
	}

	// Parse entity ID
	entityID, err := strconv.ParseUint(idParam, 10, 64)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "id",
			Message: "invalid entity ID",
		})
		return
	}

	// Get consistency level from query params
	consistencyLevelStr := r.URL.Query().Get("consistency")
	if consistencyLevelStr != "" {
		if err := ConsistencyLevel(consistencyLevelStr).Validate(); err != nil {
			RespondError(w, r, http.StatusBadRequest, err)
			return
		}
	}

	// Get timeout from query params
	timeoutStr := r.URL.Query().Get("timeout_ms")
	timeout := uint64(5000) // Default 5 seconds
	if timeoutStr != "" {
		if t, err := strconv.ParseUint(timeoutStr, 10, 64); err == nil {
			timeout = t
		}
	}

	// Route to shard for read
	shardID := s.shardRouter.Route(entityType, entityID)
	if s.config.ExposeShardHeader {
		w.Header().Set("X-QuicBaaS-Shard", fmt.Sprintf("%d", shardID))
	}
	incRestShardRequest(shardID, entityType, "get")

	// Convert to service request
	serviceReq := &service.GetRequest{
		EntityType:  entityType,
		ID:          entityID,
		Consistency: convertToServiceConsistency(ConsistencyLevel(consistencyLevelStr)),
		TimeoutMs:   timeout,
	}

	// Call service
	serviceResp, err := s.service.Get(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Check if entity exists
	if !serviceResp.Found {
		RespondError(w, r, http.StatusNotFound, &NotFoundError{
			Resource: fmt.Sprintf("%s/%d", entityType, entityID),
		})
		return
	}

	// Build response
	resp := GetResponse{
		ID:         serviceResp.ID,
		EntityType: entityType,
		Data:       json.RawMessage(serviceResp.Data),
		Timestamp:  time.Unix(0, serviceResp.Timestamp),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleDelete handles DELETE requests to remove entities.
func (s *Server) HandleDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract entity type and ID from URL
	entityType := chi.URLParam(r, "type")
	idParam := chi.URLParam(r, "id")

	if entityType == "" || idParam == "" {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "entity type and ID are required",
		})
		return
	}

	// Parse entity ID
	entityID, err := strconv.ParseUint(idParam, 10, 64)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "id",
			Message: "invalid entity ID",
		})
		return
	}

	// Get write mode from query parameter (default: sync)
	writeModeStr := r.URL.Query().Get("write_mode")
	_ = writeModeStr // Not used in Delete request currently

	// Propose delete to Raft using routing with specified write mode
	shardID := s.shardRouter.Route(entityType, entityID)
	if s.config.ExposeShardHeader {
		w.Header().Set("X-QuicBaaS-Shard", fmt.Sprintf("%d", shardID))
		w.Header().Set("X-QuicBaaS-Write-Mode", writeModeStr)
	}
	incRestShardRequest(shardID, entityType, "delete")

	// Convert to service request
	serviceReq := &service.DeleteRequest{
		EntityType:  entityType,
		ID:          entityID,
		Consistency: service.ConsistencyLevelLinearizable,
	}

	// Call service
	serviceResp, err := s.service.Delete(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := DeleteResponse{
		ID:         entityID,
		EntityType: entityType,
		Deleted:    serviceResp.Deleted,
		Timestamp:  time.Unix(0, serviceResp.Timestamp),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleList handles GET requests to list entities.
func (s *Server) HandleList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract entity type from URL
	entityType := chi.URLParam(r, "type")
	if entityType == "" {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "type",
			Message: "entity type is required",
		})
		return
	}

	// Parse query parameters
	limitStr := r.URL.Query().Get("limit")
	pageToken := r.URL.Query().Get("cursor")
	consistencyLevel := r.URL.Query().Get("consistency")

	limit := uint64(100) // Default limit
	if limitStr != "" {
		var err error
		limit, err = strconv.ParseUint(limitStr, 10, 64)
		if err != nil || limit == 0 || limit > 1000 {
			RespondError(w, r, http.StatusBadRequest, &ValidationError{
				Field:   "limit",
				Message: "limit must be between 1 and 1000",
			})
			return
		}
	}

	// Validate consistency level
	if consistencyLevel != "" {
		if err := ConsistencyLevel(consistencyLevel).Validate(); err != nil {
			RespondError(w, r, http.StatusBadRequest, err)
			return
		}
	}

	// Read from service using routing (no ID context)
	shardID := s.shardRouter.Route(entityType, 0)
	if s.config.ExposeShardHeader {
		w.Header().Set("X-QuicBaaS-Shard", fmt.Sprintf("%d", shardID))
	}
	incRestShardRequest(shardID, entityType, "list")

	// Convert to service request
	serviceReq := &service.ListRequest{
		EntityType:  entityType,
		Consistency: convertToServiceConsistency(ConsistencyLevel(consistencyLevel)),
		PageSize:    limit,
		PageToken:   pageToken,
	}

	// Create channel for streaming results
	stream := make(chan *service.ListResponse, 1)
	errChan := make(chan error, 1)

	// Call service in goroutine
	go func() {
		err := s.service.List(ctx, serviceReq, stream)
		if err != nil {
			errChan <- err
		}
		close(errChan)
	}()

	// Wait for first response or error
	var listResp *service.ListResponse
	select {
	case listResp = <-stream:
		// Got response
	case err := <-errChan:
		if err != nil {
			s.handleServiceError(w, r, err)
			return
		}
	case <-ctx.Done():
		RespondError(w, r, http.StatusRequestTimeout, &RequestTimeoutError{
			Timeout: 30 * time.Second,
		})
		return
	}

	// Handle nil response (channel closed without sending)
	if listResp == nil {
		listResp = &service.ListResponse{
			Entities:      []*service.Entity{},
			NextPageToken: "",
			TotalCount:    0,
		}
	}

	// Convert to REST response
	entities := make([]EntityItem, 0, len(listResp.Entities))
	for _, entity := range listResp.Entities {
		entities = append(entities, EntityItem{
			ID:         entity.ID,
			EntityType: entity.EntityType,
			Data:       entity.Data,
		})
	}

	// Build response
	resp := ListResponse{
		Entities:   entities,
		Total:      int(listResp.TotalCount), //nolint:gosec // G115: TotalCount represents count, safe conversion
		HasMore:    listResp.NextPageToken != "",
		NextCursor: listResp.NextPageToken,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleListEntityTypes handles GET requests to list unique entity types.
func (s *Server) HandleListEntityTypes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters
	limitStr := r.URL.Query().Get("limit")
	pageToken := r.URL.Query().Get("cursor")

	pageSize := 100 // Default limit
	if limitStr != "" {
		var err error
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit <= 0 || limit > 1000 {
			RespondError(w, r, http.StatusBadRequest, &ValidationError{
				Field:   "limit",
				Message: "limit must be between 1 and 1000",
			})
			return
		}
		pageSize = limit
	}

	// Convert to service request
	serviceReq := &service.ListEntityTypesRequest{
		PageSize:  pageSize,
		PageToken: pageToken,
	}

	// Call service
	serviceResp, err := s.service.ListEntityTypes(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Handle nil response
	if serviceResp == nil {
		serviceResp = &service.ListEntityTypesResponse{
			EntityTypes: []string{},
		}
	}

	// Build response
	resp := ListEntityTypesResponse{
		EntityTypes:   serviceResp.EntityTypes,
		NextPageToken: serviceResp.NextPageToken,
		TotalCount:    serviceResp.TotalCount,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleQuery handles GET requests to query entities by index.
func (s *Server) HandleQuery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract parameters from URL
	entityType := chi.URLParam(r, "type")
	indexName := chi.URLParam(r, "index")

	if entityType == "" || indexName == "" {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "entity type and index are required",
		})
		return
	}

	// Get value from query params
	value := r.URL.Query().Get("value")
	if value == "" {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "value",
			Message: "query value is required",
		})
		return
	}

	consistencyLevel := r.URL.Query().Get("consistency")
	if consistencyLevel != "" {
		if err := ConsistencyLevel(consistencyLevel).Validate(); err != nil {
			RespondError(w, r, http.StatusBadRequest, err)
			return
		}
	}

	// Query using service
	serviceReq := &service.QueryRequest{
		EntityType:  entityType,
		IndexName:   indexName,
		Value:       []byte(value),
		Consistency: convertToServiceConsistency(ConsistencyLevel(consistencyLevel)),
	}

	// Create channel for streaming results
	stream := make(chan *service.QueryResponse, 1)
	errChan := make(chan error, 1)

	// Call service in goroutine
	go func() {
		err := s.service.Query(ctx, serviceReq, stream)
		if err != nil {
			errChan <- err
		}
		close(errChan)
	}()

	// Wait for first response or error
	var queryResp *service.QueryResponse
	select {
	case queryResp = <-stream:
		// Got response
	case err := <-errChan:
		if err != nil {
			s.handleServiceError(w, r, err)
			return
		}
	case <-ctx.Done():
		RespondError(w, r, http.StatusRequestTimeout, &RequestTimeoutError{
			Timeout: 30 * time.Second,
		})
		return
	}

	// Handle nil response (channel closed without sending)
	if queryResp == nil {
		queryResp = &service.QueryResponse{
			Entities: []*service.Entity{},
		}
	}

	// Convert to REST response
	results := make([]EntityItem, 0, len(queryResp.Entities))
	for _, entity := range queryResp.Entities {
		results = append(results, EntityItem{
			ID:         entity.ID,
			EntityType: entity.EntityType,
			Data:       entity.Data,
		})
	}

	resp := QueryResponse{
		Results: results,
		Count:   len(results),
		Index:   indexName,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleRangeQuery handles POST requests for range queries.
func (s *Server) HandleRangeQuery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "failed to read request body",
		})
		return
	}
	defer r.Body.Close()

	// Parse request
	var req RangeQueryRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "invalid JSON body",
		})
		return
	}

	// Validate consistency level
	if req.ConsistencyLevel != "" {
		if err := ConsistencyLevel(req.ConsistencyLevel).Validate(); err != nil {
			RespondError(w, r, http.StatusBadRequest, err)
			return
		}
	}

	// Query using service scan
	serviceReq := &service.ScanRequest{
		EntityType: req.EntityType,
		IndexName:  req.Index,
		StartKey:   interfaceToBytes(req.Start),
		EndKey:     interfaceToBytes(req.End),
		Limit:      100,
	}

	// Create channel for streaming results
	stream := make(chan *service.ScanResponse, 1)
	errChan := make(chan error, 1)

	// Call service in goroutine
	go func() {
		err := s.service.ScanIndex(ctx, serviceReq, stream)
		if err != nil {
			errChan <- err
		}
		close(errChan)
	}()

	// Collect all results
	var allEntities []EntityItem
	for {
		select {
		case scanResp, ok := <-stream:
			if !ok {
				// Stream closed
				goto done
			}
			// Handle nil response (shouldn't happen in loop but be defensive)
			if scanResp == nil {
				continue
			}
			// Convert to REST response
			for _, entity := range scanResp.Entities {
				allEntities = append(allEntities, EntityItem{
					ID:         entity.ID,
					EntityType: entity.EntityType,
					Data:       entity.Data,
				})
			}
		case err := <-errChan:
			if err != nil {
				s.handleServiceError(w, r, err)
				return
			}
		case <-ctx.Done():
			RespondError(w, r, http.StatusRequestTimeout, &RequestTimeoutError{
				Timeout: 30 * time.Second,
			})
			return
		}
	}

done:
	resp := QueryResponse{
		Results: allEntities,
		Count:   len(allEntities),
		Index:   req.Index,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleClusterStatus handles GET requests for cluster status.
func (s *Server) HandleClusterStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Call service
	serviceReq := &service.ClusterStatusRequest{
		IncludeDetails: true,
	}

	serviceResp, err := s.service.ClusterStatus(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Handle nil response
	if serviceResp == nil {
		RespondError(w, r, http.StatusInternalServerError, &InternalServerError{
			Message: "cluster status unavailable",
		})
		return
	}

	// Build shard info
	var shards []ShardInfo
	for _, shardInfo := range serviceResp.Shards {
		shard := ShardInfo{
			ShardID:  shardInfo.ShardID,
			LeaderID: shardInfo.LeaderID,
			IsLeader: false, // Determined by comparing node ID with leader ID
			Members:  shardInfo.Replicas,
			State:    convertHealthStatus(shardInfo.Status),
		}
		shards = append(shards, shard)
	}

	// Extract info from first node
	var leaderIDStr string
	var totalNodes int
	var members []uint64
	var nodeID uint64
	if len(serviceResp.Nodes) > 0 {
		nodeID = serviceResp.Nodes[0].NodeID
		totalNodes = len(serviceResp.Nodes)
	}
	if len(shards) > 0 {
		leaderIDStr = fmt.Sprintf("%d", shards[0].LeaderID)
		members = shards[0].Members
	}

	// Build response
	resp := ClusterStatusResponse{
		NodeID:      nodeID,
		RaftAddress: "", // Not available in service response
		LeaderID:    leaderIDStr,
		TotalNodes:  totalNodes,
		Members:     members,
		Shards:      shards,
		Healthy:     len(shards) > 0,
		Timestamp:   time.Unix(0, serviceResp.Timestamp),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleClusterShards returns detailed shard information (leaders, members, state).
func (s *Server) HandleClusterShards(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Call service
	serviceReq := &service.ClusterStatusRequest{
		IncludeDetails: true,
	}

	serviceResp, err := s.service.ClusterStatus(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Handle nil response
	if serviceResp == nil {
		RespondError(w, r, http.StatusInternalServerError, &InternalServerError{
			Message: "cluster status unavailable",
		})
		return
	}

	// Build shard info
	var shards []ShardInfo
	for _, shardInfo := range serviceResp.Shards {
		shard := ShardInfo{
			ShardID:  shardInfo.ShardID,
			LeaderID: shardInfo.LeaderID,
			IsLeader: false,
			Members:  shardInfo.Replicas,
			State:    convertHealthStatus(shardInfo.Status),
		}
		shards = append(shards, shard)
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"shards":    shards,
		"timestamp": time.Unix(0, serviceResp.Timestamp),
	})
}

// HandleGetLeader handles GET requests to get shard leader.
func (s *Server) HandleGetLeader(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract shard ID from URL
	shardIDParam := chi.URLParam(r, "shardID")
	shardID, err := strconv.ParseUint(shardIDParam, 10, 64)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "shardID",
			Message: "invalid shard ID",
		})
		return
	}

	// Call service
	serviceReq := &service.GetLeaderRequest{
		ShardID: shardID,
	}

	serviceResp, err := s.service.GetLeader(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Handle nil response
	if serviceResp == nil {
		RespondError(w, r, http.StatusInternalServerError, &InternalServerError{
			Message: "leader information unavailable",
		})
		return
	}

	// Build response
	resp := LeaderResponse{
		ShardID:  shardID,
		LeaderID: serviceResp.LeaderID,
		Valid:    serviceResp.LeaderID > 0,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleHealth handles GET requests for health checks.
func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Call service
	serviceReq := &service.HealthRequest{
		IncludeDetails: true,
	}

	serviceResp, err := s.service.Health(ctx, serviceReq)
	if err != nil {
		// Health check should never fail completely
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "health check failed",
		})
		return
	}

	// Convert service response to REST response
	checks := make(map[string]CheckResult)
	for component, health := range serviceResp.Details {
		checks[component] = CheckResult{
			Status:  convertHealthStatusToString(health.Status),
			Message: health.Message,
		}
	}

	// Overall status
	status := convertHealthStatusToString(serviceResp.Status)

	resp := HealthResponse{
		Status:    status,
		Checks:    checks,
		Timestamp: time.Now(),
		Uptime:    (time.Duration(serviceResp.UptimeSeconds) * time.Second).String(), //nolint:gosec // G115: UptimeSeconds represents duration, safe conversion
	}

	statusCode := http.StatusOK
	if status == "unhealthy" {
		statusCode = http.StatusServiceUnavailable
	}

	RespondJSON(w, r, statusCode, resp)
}

// HandleLiveness handles liveness probe requests.
func (s *Server) HandleLiveness(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Call service
	serviceReq := &service.LivenessRequest{}

	serviceResp, err := s.service.Liveness(ctx, serviceReq)
	if err != nil || !serviceResp.Alive {
		resp := HealthResponse{
			Status:    "unhealthy",
			Timestamp: time.Now(),
			Uptime:    s.Uptime().String(),
			Checks: map[string]CheckResult{
				"server": {
					Status:  "fail",
					Message: "server is not alive",
				},
			},
		}
		RespondJSON(w, r, http.StatusServiceUnavailable, resp)
		return
	}

	// Simple liveness check - server is alive
	resp := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now(),
		Uptime:    s.Uptime().String(),
		Checks: map[string]CheckResult{
			"server": {
				Status:  "pass",
				Message: "server is running",
			},
		},
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleReadiness handles readiness probe requests.
func (s *Server) HandleReadiness(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Call service
	serviceReq := &service.ReadinessRequest{}

	serviceResp, err := s.service.Readiness(ctx, serviceReq)
	if err != nil {
		resp := HealthResponse{
			Status:    "unhealthy",
			Timestamp: time.Now(),
			Uptime:    s.Uptime().String(),
			Checks: map[string]CheckResult{
				"service": {
					Status:  "fail",
					Message: "service readiness check failed",
				},
			},
		}
		RespondJSON(w, r, http.StatusServiceUnavailable, resp)
		return
	}

	// Check if ready
	status := "healthy"
	statusCode := http.StatusOK
	checks := make(map[string]CheckResult)

	if serviceResp.Ready {
		checks["service"] = CheckResult{
			Status:  "pass",
			Message: "service is ready",
		}
	} else {
		status = "unhealthy"
		statusCode = http.StatusServiceUnavailable
		checks["service"] = CheckResult{
			Status:  "fail",
			Message: serviceResp.Reason,
		}
	}

	resp := HealthResponse{
		Status:    status,
		Checks:    checks,
		Timestamp: time.Now(),
		Uptime:    s.Uptime().String(),
	}

	RespondJSON(w, r, statusCode, resp)
}

// Helper functions

// buildEntityKey builds a storage key for an entity.
func buildEntityKey(entityType string, entityID uint64) []byte {
	return []byte(fmt.Sprintf("%s:%d", entityType, entityID))
}

// handleServiceError handles service errors and responds appropriately.
func (s *Server) handleServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var qbErr *errors.QBError
	if errors.As(err, &qbErr) {
		// Use the error code's built-in HTTP status mapping
		httpStatus := qbErr.Code.HTTPStatus()
		var errMsg string
		if qbErr.Err != nil {
			errMsg = qbErr.Err.Error()
		} else {
			errMsg = qbErr.Code.String()
		}

		switch qbErr.Code {
		case errors.ErrNotFound:
			RespondError(w, r, httpStatus, &NotFoundError{
				Resource: "entity",
			})
		case errors.ErrLeaderNotAvailable:
			RespondError(w, r, httpStatus, &ServiceUnavailableError{
				Message: "leader not available",
			})
		case errors.ErrTimeout, errors.ErrDeadlineExceeded:
			RespondError(w, r, httpStatus, &RequestTimeoutError{
				Timeout: 30 * time.Second,
			})
		case errors.ErrInvalidInput, errors.ErrValidation:
			RespondError(w, r, httpStatus, &ValidationError{
				Message: errMsg,
			})
		case errors.ErrForbidden, errors.ErrNotAllowed:
			RespondError(w, r, httpStatus, &ForbiddenError{
				Reason: errMsg,
			})
		case errors.ErrUnauthorized:
			RespondError(w, r, httpStatus, &UnauthorizedError{
				Message: errMsg,
			})
		case errors.ErrConflict, errors.ErrAlreadyExists:
			RespondError(w, r, httpStatus, &ConflictError{
				Message: errMsg,
			})
		case errors.ErrUnavailable, errors.ErrStorage, errors.ErrRaft:
			RespondError(w, r, httpStatus, &ServiceUnavailableError{
				Message: errMsg,
			})
		case errors.ErrInternal, errors.ErrUnknown:
			RespondError(w, r, httpStatus, &InternalServerError{
				Message: errMsg,
			})
		default:
			// For any other error codes, include the actual error message
			// to aid debugging while using the code's proper HTTP status
			RespondError(w, r, httpStatus, &InternalServerError{
				Message: errMsg,
			})
		}
		return
	}

	// Check scheduler-specific error types
	var notFoundErr *scheduler.JobNotFoundError
	if stderrors.As(err, &notFoundErr) {
		RespondError(w, r, http.StatusNotFound, &NotFoundError{
			Resource: notFoundErr.Error(),
		})
		return
	}

	var alreadyExistsErr *scheduler.JobAlreadyExistsError
	if stderrors.As(err, &alreadyExistsErr) {
		RespondError(w, r, http.StatusConflict, &ConflictError{
			Message: alreadyExistsErr.Error(),
		})
		return
	}

	var invalidJobErr *scheduler.InvalidJobError
	if stderrors.As(err, &invalidJobErr) {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: invalidJobErr.Error(),
		})
		return
	}

	var invalidScheduleErr *scheduler.InvalidScheduleError
	if stderrors.As(err, &invalidScheduleErr) {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: invalidScheduleErr.Error(),
		})
		return
	}

	var handlerNotFoundErr *scheduler.HandlerNotFoundError
	if stderrors.As(err, &handlerNotFoundErr) {
		RespondError(w, r, http.StatusNotFound, &NotFoundError{
			Resource: handlerNotFoundErr.Error(),
		})
		return
	}

	var timeoutErr *scheduler.JobTimeoutError
	if stderrors.As(err, &timeoutErr) {
		RespondError(w, r, http.StatusGatewayTimeout, &RequestTimeoutError{
			Timeout: 0,
		})
		return
	}

	var maxRetriesErr *scheduler.MaxRetriesExceededError
	if stderrors.As(err, &maxRetriesErr) {
		RespondError(w, r, http.StatusTooManyRequests, &InternalServerError{
			Message: maxRetriesErr.Error(),
		})
		return
	}

	var stoppedErr *scheduler.SchedulerStoppedError
	if stderrors.As(err, &stoppedErr) {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: stoppedErr.Error(),
		})
		return
	}

	var unauthorizedErr *scheduler.UnauthorizedError
	if stderrors.As(err, &unauthorizedErr) {
		RespondError(w, r, http.StatusForbidden, &ForbiddenError{
			Reason: unauthorizedErr.Error(),
		})
		return
	}

	var storageErr *scheduler.ErrStorageError
	if stderrors.As(err, &storageErr) {
		RespondError(w, r, http.StatusInternalServerError, &InternalServerError{
			Message: storageErr.Error(),
		})
		return
	}

	RespondError(w, r, http.StatusInternalServerError, err)
}

// Custom error types

// ValidationError represents a 400 validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("validation error for %s: %s", e.Field, e.Message)
	}
	return fmt.Sprintf("validation error: %s", e.Message)
}

// NotFoundError represents a 404 error.
type NotFoundError struct {
	Resource string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("resource not found: %s", e.Resource)
}

// ServiceUnavailableError represents a 503 error.
type ServiceUnavailableError struct {
	Message string
}

func (e *ServiceUnavailableError) Error() string {
	return e.Message
}

// Conversion helpers

// convertToServiceConsistency converts REST ConsistencyLevel to service.ConsistencyLevel.
func convertToServiceConsistency(level ConsistencyLevel) service.ConsistencyLevel {
	switch level {
	case ConsistencyStrong:
		return service.ConsistencyLevelStrong
	case ConsistencyLinearizable:
		return service.ConsistencyLevelLinearizable
	case ConsistencyEventual:
		return service.ConsistencyLevelEventual
	case "": // Default
		return service.ConsistencyLevelLinearizable
	default:
		return service.ConsistencyLevelLinearizable
	}
}

// convertWriteMode converts string write mode to service.WriteMode.
func convertWriteMode(mode string) service.WriteMode {
	switch mode {
	case "sync":
		return service.WriteModeSync
	case "batched":
		return service.WriteModeBatched
	case "wal_durable":
		return service.WriteModeWALDurable
	default:
		return service.WriteModeSync
	}
}

// convertHealthStatus converts service health status to string.
func convertHealthStatus(status service.HealthStatus) string {
	switch status {
	case service.HealthStatusHealthy:
		return "healthy"
	case service.HealthStatusDegraded:
		return "degraded"
	case service.HealthStatusUnhealthy:
		return "unhealthy"
	default:
		return "unknown"
	}
}

// convertHealthStatusToString converts service health status to string.
func convertHealthStatusToString(status service.HealthStatus) string {
	switch status {
	case service.HealthStatusHealthy:
		return "healthy"
	case service.HealthStatusDegraded:
		return "degraded"
	case service.HealthStatusUnhealthy:
		return "unhealthy"
	default:
		return "unknown"
	}
}

// interfaceToBytes converts interface{} to []byte
// Handles string and []byte types.
func interfaceToBytes(v interface{}) []byte {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case []byte:
		return val
	case string:
		return []byte(val)
	default:
		// Try to convert to string
		return []byte(fmt.Sprintf("%v", val))
	}
}
