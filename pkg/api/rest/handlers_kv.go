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
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/id"
)

// HandleKVPut handles PUT/POST requests to store key-value pairs with string keys.
// Endpoint: PUT/POST /api/v1/kv/{type}/{key}
//
// The string key is hashed to a uint64 for internal storage, but the original
// string key is preserved in the data payload for retrieval.
func (s *Server) HandleKVPut(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract entity type and key from URL
	entityType := chi.URLParam(r, "type")
	key := chi.URLParam(r, "key")

	if entityType == "" {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "type",
			Message: "entity type is required",
		})
		return
	}

	if key == "" {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "key",
			Message: "key is required",
		})
		return
	}

	// URL-decode the key
	decodedKey, err := url.PathUnescape(key)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "key",
			Message: "invalid key encoding",
		})
		return
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

	// Parse request (optional - body can be raw value)
	var reqBody KVPutRequest
	var valueData json.RawMessage

	if len(body) > 0 {
		// Try to parse as KVPutRequest first
		if err := json.Unmarshal(body, &reqBody); err == nil && len(reqBody.Value) > 0 {
			valueData = reqBody.Value
		} else {
			// Treat entire body as the value
			valueData = body
		}
	}

	// Validate consistency level if provided
	if reqBody.ConsistencyLevel != "" {
		if err := ConsistencyLevel(reqBody.ConsistencyLevel).Validate(); err != nil {
			RespondError(w, r, http.StatusBadRequest, err)
			return
		}
	}

	// Create a wrapper that includes the original string key for retrieval
	// This allows us to store the string key alongside the value
	kvWrapper := kvDataWrapper{
		Key:   decodedKey,
		Value: valueData,
	}
	wrappedData, err := json.Marshal(kvWrapper)
	if err != nil {
		RespondError(w, r, http.StatusInternalServerError, &InternalServerError{
			Message: "failed to encode data",
		})
		return
	}

	// Hash the string key to get a consistent uint64 ID
	hashedID := hashStringKey(entityType, decodedKey)

	// Route to shard
	shardID := s.shardRouter.Route(entityType, hashedID)
	if s.config.ExposeShardHeader {
		w.Header().Set("X-QuicBaaS-Shard", fmt.Sprintf("%d", shardID))
	}
	incRestShardRequest(shardID, entityType, "kv_put")

	// Convert to service request
	serviceReq := &service.PutRequest{
		EntityType:  entityType,
		ID:          hashedID,
		Data:        wrappedData,
		Consistency: convertToServiceConsistency(ConsistencyLevel(reqBody.ConsistencyLevel)),
	}

	// Call service
	serviceResp, err := s.service.Put(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := KVPutResponse{
		EntityType: entityType,
		Key:        decodedKey,
		Success:    serviceResp.ID > 0,
		Message:    "stored successfully",
		Timestamp:  time.Unix(0, serviceResp.Timestamp),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleKVGet handles GET requests to retrieve values by string key.
// Endpoint: GET /api/v1/kv/{type}/{key}.
func (s *Server) HandleKVGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract entity type and key from URL
	entityType := chi.URLParam(r, "type")
	key := chi.URLParam(r, "key")

	if entityType == "" || key == "" {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "entity type and key are required",
		})
		return
	}

	// URL-decode the key
	decodedKey, err := url.PathUnescape(key)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "key",
			Message: "invalid key encoding",
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

	// Hash the string key to get the same uint64 ID used during Put
	hashedID := hashStringKey(entityType, decodedKey)

	// Route to shard
	shardID := s.shardRouter.Route(entityType, hashedID)
	if s.config.ExposeShardHeader {
		w.Header().Set("X-QuicBaaS-Shard", fmt.Sprintf("%d", shardID))
	}
	incRestShardRequest(shardID, entityType, "kv_get")

	// Convert to service request
	serviceReq := &service.GetRequest{
		EntityType:  entityType,
		ID:          hashedID,
		Consistency: convertToServiceConsistency(ConsistencyLevel(consistencyLevelStr)),
	}

	// Call service
	serviceResp, err := s.service.Get(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Check if key exists
	if !serviceResp.Found {
		RespondError(w, r, http.StatusNotFound, &NotFoundError{
			Resource: fmt.Sprintf("%s/%s", entityType, decodedKey),
		})
		return
	}

	// Unwrap the stored data to get the original value
	var wrapper kvDataWrapper
	if err := json.Unmarshal(serviceResp.Data, &wrapper); err != nil || (wrapper.Key == "" && wrapper.Value == nil) {
		// If we can't unmarshal as wrapper or wrapper fields are empty, return the raw data
		// (for backwards compatibility with non-KV stored data, e.g., transaction PUT)
		resp := KVGetResponse{
			EntityType: entityType,
			Key:        decodedKey,
			Value:      serviceResp.Data,
			Found:      serviceResp.Found,
			Timestamp:  time.Unix(0, serviceResp.Timestamp),
		}
		RespondJSON(w, r, http.StatusOK, resp)
		return
	}

	// Build response with unwrapped value
	resp := KVGetResponse{
		EntityType: entityType,
		Key:        decodedKey,
		Value:      wrapper.Value,
		Found:      serviceResp.Found,
		Timestamp:  time.Unix(0, serviceResp.Timestamp),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleKVDelete handles DELETE requests to remove key-value pairs.
// Endpoint: DELETE /api/v1/kv/{type}/{key}.
func (s *Server) HandleKVDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract entity type and key from URL
	entityType := chi.URLParam(r, "type")
	key := chi.URLParam(r, "key")

	if entityType == "" || key == "" {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "entity type and key are required",
		})
		return
	}

	// URL-decode the key
	decodedKey, err := url.PathUnescape(key)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "key",
			Message: "invalid key encoding",
		})
		return
	}

	// Hash the string key to get the same uint64 ID
	hashedID := hashStringKey(entityType, decodedKey)

	// Route to shard
	shardID := s.shardRouter.Route(entityType, hashedID)
	if s.config.ExposeShardHeader {
		w.Header().Set("X-QuicBaaS-Shard", fmt.Sprintf("%d", shardID))
	}
	incRestShardRequest(shardID, entityType, "kv_delete")

	// Convert to service request
	serviceReq := &service.DeleteRequest{
		EntityType:  entityType,
		ID:          hashedID,
		Consistency: service.ConsistencyLevelLinearizable,
	}

	// Call service
	serviceResp, err := s.service.Delete(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := KVDeleteResponse{
		EntityType: entityType,
		Key:        decodedKey,
		Deleted:    serviceResp.Deleted,
		Message:    "deleted successfully",
		Timestamp:  time.Unix(0, serviceResp.Timestamp),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleKVList handles GET requests to list key-value pairs for an entity type.
// Endpoint: GET /api/v1/kv/{type}.
func (s *Server) HandleKVList(w http.ResponseWriter, r *http.Request) {
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
	cursor := r.URL.Query().Get("cursor")
	consistencyLevel := r.URL.Query().Get("consistency")

	limit := uint64(100) // Default limit
	if limitStr != "" {
		parsedLimit, err := strconv.ParseUint(limitStr, 10, 64)
		if err != nil || parsedLimit == 0 || parsedLimit > 1000 {
			RespondError(w, r, http.StatusBadRequest, &ValidationError{
				Field:   "limit",
				Message: "limit must be between 1 and 1000",
			})
			return
		}
		limit = parsedLimit
	}

	// Validate consistency level
	if consistencyLevel != "" {
		if err := ConsistencyLevel(consistencyLevel).Validate(); err != nil {
			RespondError(w, r, http.StatusBadRequest, err)
			return
		}
	}

	// Route to shard (use 0 for ID since we're listing)
	shardID := s.shardRouter.Route(entityType, 0)
	if s.config.ExposeShardHeader {
		w.Header().Set("X-QuicBaaS-Shard", fmt.Sprintf("%d", shardID))
	}
	incRestShardRequest(shardID, entityType, "kv_list")

	// Convert to service request
	serviceReq := &service.ListRequest{
		EntityType:  entityType,
		Consistency: convertToServiceConsistency(ConsistencyLevel(consistencyLevel)),
		PageSize:    limit,
		PageToken:   cursor,
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
	listResp, timedOut, err := firstStreamResponse(ctx, stream, errChan)
	switch {
	case timedOut:
		RespondError(w, r, http.StatusRequestTimeout, &RequestTimeoutError{
			Timeout: 30 * time.Second,
		})
		return
	case err != nil:
		s.handleServiceError(w, r, err)
		return
	}

	// Handle nil response
	if listResp == nil {
		listResp = &service.ListResponse{
			Entities:      []*service.Entity{},
			NextPageToken: "",
			TotalCount:    0,
		}
	}

	// Convert to REST response, extracting original keys from wrapper
	items := make([]KVListItem, 0, len(listResp.Entities))
	for _, entity := range listResp.Entities {
		var wrapper kvDataWrapper
		if err := json.Unmarshal(entity.Data, &wrapper); err != nil {
			// Skip entities that don't have the KV wrapper format
			// (they were stored via entity API, not KV API)
			continue
		}
		items = append(items, KVListItem{
			Key:       wrapper.Key,
			Value:     wrapper.Value,
			Timestamp: time.Unix(0, entity.Timestamp),
		})
	}

	total, err := conv.SafeUint64ToInt(listResp.TotalCount)
	if err != nil {
		s.handleServiceError(w, r, api.NewEncodeError("rest.kv.list", "total count", err))
		return
	}

	// Build response
	resp := KVListResponse{
		EntityType: entityType,
		Items:      items,
		Total:      total,
		NextCursor: listResp.NextPageToken,
		HasMore:    listResp.NextPageToken != "",
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// kvDataWrapper wraps KV data to preserve the original string key.
type kvDataWrapper struct {
	Key   string          `json:"_key"`
	Value json.RawMessage `json:"_value"`
}

// hashStringKey converts a string key to a uint64 ID using FNV-1a hash.
// This provides a consistent mapping from string keys to numeric IDs.
func hashStringKey(entityType, key string) uint64 {
	return id.HashString(entityType + ":" + key)
}
