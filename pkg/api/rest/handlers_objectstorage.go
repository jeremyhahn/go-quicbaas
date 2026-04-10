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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Object Storage REST Types
// =============================================================================

// PutObjectRESTRequest is the JSON request for object storage PUT operations.
type PutObjectRESTRequest struct {
	ContentType       string                     `json:"content_type,omitempty"`
	Tags              map[string]string          `json:"tags,omitempty"`
	ReplicationPolicy *catalog.ReplicationPolicy `json:"replication_policy,omitempty"`
	ArchivePolicy     *catalog.ArchivePolicy     `json:"archive_policy,omitempty"`
	RetentionPolicy   *catalog.RetentionPolicy   `json:"retention_policy,omitempty"`
}

// CopyObjectRESTRequest is the JSON request for object copy operations.
type CopyObjectRESTRequest struct {
	DestinationKey    string `json:"destination_key"`
	TenantID          string `json:"tenant_id,omitempty"`
	OverwriteExisting bool   `json:"overwrite_existing,omitempty"`
	TargetBackend     string `json:"target_backend,omitempty"`
}

// ReplicateObjectRESTRequest is the JSON request for object replication.
type ReplicateObjectRESTRequest struct {
	Targets     []string `json:"targets"`
	Priority    int      `json:"priority,omitempty"`
	Synchronous bool     `json:"synchronous,omitempty"`
}

// MigrateObjectRESTRequest is the JSON request for object migration.
type MigrateObjectRESTRequest struct {
	TargetBackend    string `json:"target_backend"`
	DeleteFromSource bool   `json:"delete_from_source,omitempty"`
}

// ArchiveObjectRESTRequest is the JSON request for object archival.
type ArchiveObjectRESTRequest struct {
	TargetStore       string `json:"target_store,omitempty"`
	DeleteFromPrimary bool   `json:"delete_from_primary,omitempty"`
}

// RestoreObjectRESTRequest is the JSON request for object restoration.
type RestoreObjectRESTRequest struct {
	TargetBackend string `json:"target_backend,omitempty"`
}

// VerifyObjectRESTRequest is the JSON request for object verification.
type VerifyObjectRESTRequest struct {
	Stores           []string `json:"stores,omitempty"`
	RepairMismatches bool     `json:"repair_mismatches,omitempty"`
}

// SetObjectPolicyRESTRequest is the JSON request for setting object policies.
type SetObjectPolicyRESTRequest struct {
	ReplicationPolicy *catalog.ReplicationPolicy `json:"replication_policy,omitempty"`
	ArchivePolicy     *catalog.ArchivePolicy     `json:"archive_policy,omitempty"`
	RetentionPolicy   *catalog.RetentionPolicy   `json:"retention_policy,omitempty"`
}

// =============================================================================
// Batch Operation REST Types
// =============================================================================

// BatchPutObjectOperation represents a single object in a batch put request.
type BatchPutObjectOperation struct {
	Key           string            `json:"key"`
	Value         string            `json:"value"`
	ContentType   string            `json:"content_type,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	TargetBackend string            `json:"target_backend,omitempty"`
}

// BatchPutObjectsRESTRequest is the JSON request for batch put operations.
type BatchPutObjectsRESTRequest struct {
	Objects []BatchPutObjectOperation `json:"objects"`
}

// BatchPutObjectResult represents the result of storing a single object.
type BatchPutObjectResult struct {
	Key      string `json:"key"`
	ObjectID uint64 `json:"object_id,omitempty"`
	Success  bool   `json:"success"`
	Error    string `json:"error,omitempty"`
	Version  uint64 `json:"version,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// BatchPutObjectsRESTResponse is the JSON response for batch put operations.
type BatchPutObjectsRESTResponse struct {
	Results   []BatchPutObjectResult `json:"results"`
	Success   int                    `json:"success"`
	Failed    int                    `json:"failed"`
	Timestamp int64                  `json:"timestamp"`
}

// BatchGetObjectsRESTRequest is the JSON request for batch get operations.
type BatchGetObjectsRESTRequest struct {
	Keys            []string `json:"keys"`
	IncludeMetadata bool     `json:"include_metadata,omitempty"`
}

// BatchGetObjectResult represents the result of retrieving a single object.
type BatchGetObjectResult struct {
	Key         string            `json:"key"`
	Value       string            `json:"value,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
	Size        int64             `json:"size,omitempty"`
	Version     uint64            `json:"version,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Success     bool              `json:"success"`
	Error       string            `json:"error,omitempty"`
}

// BatchGetObjectsRESTResponse is the JSON response for batch get operations.
type BatchGetObjectsRESTResponse struct {
	Results   []BatchGetObjectResult `json:"results"`
	Success   int                    `json:"success"`
	Failed    int                    `json:"failed"`
	Timestamp int64                  `json:"timestamp"`
}

// BatchDeleteObjectsRESTRequest is the JSON request for batch delete operations.
type BatchDeleteObjectsRESTRequest struct {
	Keys []string `json:"keys"`
}

// BatchDeleteObjectResult represents the result of deleting a single object.
type BatchDeleteObjectResult struct {
	Key     string `json:"key"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Size    int64  `json:"size,omitempty"`
}

// BatchDeleteObjectsRESTResponse is the JSON response for batch delete operations.
type BatchDeleteObjectsRESTResponse struct {
	Results   []BatchDeleteObjectResult `json:"results"`
	Success   int                       `json:"success"`
	Failed    int                       `json:"failed"`
	Timestamp int64                     `json:"timestamp"`
}

// SetTenantQuotasRESTRequest is the JSON request for setting tenant quotas.
type SetTenantQuotasRESTRequest struct {
	MaxStorageBytes int64 `json:"max_storage_bytes"`
	MaxObjectSize   int64 `json:"max_object_size"`
	MaxObjectCount  int64 `json:"max_object_count"`
}

// =============================================================================
// Object CRUD Handlers
// =============================================================================

// HandlePutObject stores an object.
// PUT /api/v1/objects/{key:.*}.
func (s *Server) HandlePutObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := chi.URLParam(r, "*")

	if key == "" {
		RespondBadRequest(w, r, "object key is required")
		return
	}

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	if len(body) == 0 {
		RespondBadRequest(w, r, "request body is required")
		return
	}

	// Build service request
	req := &service.PutObjectRequest{
		Key:           key,
		Value:         body,
		ContentType:   r.Header.Get("Content-Type"),
		TenantID:      r.Header.Get("X-Tenant-ID"),
		TargetBackend: r.Header.Get("X-Target-Backend"),
	}

	// Parse shard ID from header
	if shardStr := r.Header.Get("X-Shard-ID"); shardStr != "" {
		shardID, err := strconv.ParseUint(shardStr, 10, 64)
		if err == nil {
			req.ShardID = shardID
		}
	}

	// Parse tags from query parameter
	if tagsJSON := r.URL.Query().Get("tags"); tagsJSON != "" {
		if err := json.Unmarshal([]byte(tagsJSON), &req.Tags); err != nil {
			RespondBadRequest(w, r, "invalid tags JSON format")
			return
		}
	}

	// Parse checksum from header
	if checksumHex := r.Header.Get("X-Checksum-SHA256"); checksumHex != "" {
		checksum, err := hex.DecodeString(checksumHex)
		if err == nil {
			req.Checksum = checksum
		}
	}

	// Call service
	resp, err := objService.PutObject(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	// Return response with ETag
	w.Header().Set("ETag", fmt.Sprintf("\"%x\"", resp.Checksum))
	w.Header().Set("X-Object-Version", strconv.FormatUint(resp.Version, 10))
	w.Header().Set("X-Object-ID", strconv.FormatUint(resp.ObjectID, 10))

	RespondJSON(w, r, http.StatusCreated, resp)
}

// HandleGetObject retrieves an object.
// GET /api/v1/objects/{key:.*}.
func (s *Server) HandleGetObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := chi.URLParam(r, "*")

	if key == "" {
		RespondBadRequest(w, r, "object key is required")
		return
	}

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	req := &service.GetObjectRequest{
		Key:             key,
		TenantID:        r.Header.Get("X-Tenant-ID"),
		IncludeMetadata: r.URL.Query().Get("metadata") == "true",
	}

	resp, err := objService.GetObject(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	// Set response headers
	contentType := resp.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(resp.Size, 10))
	w.Header().Set("ETag", fmt.Sprintf("\"%x\"", resp.Checksum))
	w.Header().Set("X-Object-Version", strconv.FormatUint(resp.Version, 10))
	w.Header().Set("Last-Modified", time.Unix(0, resp.ModifiedAt).UTC().Format(http.TimeFormat))

	// Write body directly (not JSON)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp.Value)
}

// HandleDeleteObject removes an object.
// DELETE /api/v1/objects/{key:.*}.
func (s *Server) HandleDeleteObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := chi.URLParam(r, "*")

	if key == "" {
		RespondBadRequest(w, r, "object key is required")
		return
	}

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	req := &service.DeleteObjectRequest{
		Key:      key,
		TenantID: r.Header.Get("X-Tenant-ID"),
	}

	resp, err := objService.DeleteObject(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleHeadObject retrieves object metadata without data.
// HEAD /api/v1/objects/{key:.*}.
func (s *Server) HandleHeadObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := chi.URLParam(r, "*")

	if key == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	objService := s.service.ObjectStorageService()
	if objService == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	req := &service.HeadObjectRequest{
		Key:      key,
		TenantID: r.Header.Get("X-Tenant-ID"),
	}

	resp, err := objService.HeadObject(ctx, req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if !resp.Exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Set metadata headers
	contentType := resp.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(resp.Size, 10))
	w.Header().Set("ETag", fmt.Sprintf("\"%x\"", resp.Checksum))
	w.Header().Set("X-Object-Version", strconv.FormatUint(resp.Version, 10))
	w.Header().Set("X-Object-Status", resp.Status)
	w.Header().Set("X-Replica-Count", strconv.Itoa(resp.ReplicaCount))
	w.Header().Set("Last-Modified", time.Unix(0, resp.ModifiedAt).UTC().Format(http.TimeFormat))

	if resp.TenantID != "" {
		w.Header().Set("X-Tenant-ID", resp.TenantID)
	}

	w.WriteHeader(http.StatusOK)
}

// =============================================================================
// Batch Operation Handlers
// =============================================================================

// HandleBatchPutObjects stores multiple objects.
// POST /api/v1/objects/batch.
func (s *Server) HandleBatchPutObjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	var reqBody BatchPutObjectsRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		RespondBadRequest(w, r, "invalid request body")
		return
	}
	defer r.Body.Close()

	if len(reqBody.Objects) == 0 {
		RespondBadRequest(w, r, "objects array cannot be empty")
		return
	}

	tenantID := r.Header.Get("X-Tenant-ID")
	results := make([]BatchPutObjectResult, 0, len(reqBody.Objects))
	successCount := 0
	failedCount := 0

	for _, obj := range reqBody.Objects {
		if obj.Key == "" {
			results = append(results, BatchPutObjectResult{
				Key:     obj.Key,
				Success: false,
				Error:   "key is required",
			})
			failedCount++
			continue
		}

		req := &service.PutObjectRequest{
			Key:           obj.Key,
			Value:         []byte(obj.Value),
			ContentType:   obj.ContentType,
			Tags:          obj.Tags,
			TenantID:      tenantID,
			TargetBackend: obj.TargetBackend,
		}

		resp, err := objService.PutObject(ctx, req)
		if err != nil {
			results = append(results, BatchPutObjectResult{
				Key:     obj.Key,
				Success: false,
				Error:   err.Error(),
			})
			failedCount++
			continue
		}

		results = append(results, BatchPutObjectResult{
			Key:      obj.Key,
			ObjectID: resp.ObjectID,
			Success:  true,
			Version:  resp.Version,
			Size:     resp.Size,
		})
		successCount++
	}

	response := BatchPutObjectsRESTResponse{
		Results:   results,
		Success:   successCount,
		Failed:    failedCount,
		Timestamp: time.Now().UnixNano(),
	}

	RespondJSON(w, r, http.StatusOK, response)
}

// HandleBatchGetObjects retrieves multiple objects.
// POST /api/v1/objects/batch/get.
func (s *Server) HandleBatchGetObjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	var reqBody BatchGetObjectsRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		RespondBadRequest(w, r, "invalid request body")
		return
	}
	defer r.Body.Close()

	if len(reqBody.Keys) == 0 {
		RespondBadRequest(w, r, "keys array cannot be empty")
		return
	}

	tenantID := r.Header.Get("X-Tenant-ID")
	results := make([]BatchGetObjectResult, 0, len(reqBody.Keys))
	successCount := 0
	failedCount := 0

	for _, key := range reqBody.Keys {
		if key == "" {
			results = append(results, BatchGetObjectResult{
				Key:     key,
				Success: false,
				Error:   "key cannot be empty",
			})
			failedCount++
			continue
		}

		req := &service.GetObjectRequest{
			Key:             key,
			TenantID:        tenantID,
			IncludeMetadata: reqBody.IncludeMetadata,
		}

		resp, err := objService.GetObject(ctx, req)
		if err != nil {
			results = append(results, BatchGetObjectResult{
				Key:     key,
				Success: false,
				Error:   err.Error(),
			})
			failedCount++
			continue
		}

		result := BatchGetObjectResult{
			Key:         key,
			Value:       string(resp.Value),
			ContentType: resp.ContentType,
			Size:        resp.Size,
			Version:     resp.Version,
			Success:     true,
		}

		if reqBody.IncludeMetadata {
			result.Tags = resp.Tags
		}

		results = append(results, result)
		successCount++
	}

	response := BatchGetObjectsRESTResponse{
		Results:   results,
		Success:   successCount,
		Failed:    failedCount,
		Timestamp: time.Now().UnixNano(),
	}

	RespondJSON(w, r, http.StatusOK, response)
}

// HandleBatchDeleteObjects deletes multiple objects.
// POST /api/v1/objects/batch/delete.
func (s *Server) HandleBatchDeleteObjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	var reqBody BatchDeleteObjectsRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		RespondBadRequest(w, r, "invalid request body")
		return
	}
	defer r.Body.Close()

	if len(reqBody.Keys) == 0 {
		RespondBadRequest(w, r, "keys array cannot be empty")
		return
	}

	tenantID := r.Header.Get("X-Tenant-ID")
	results := make([]BatchDeleteObjectResult, 0, len(reqBody.Keys))
	successCount := 0
	failedCount := 0

	for _, key := range reqBody.Keys {
		if key == "" {
			results = append(results, BatchDeleteObjectResult{
				Key:     key,
				Success: false,
				Error:   "key cannot be empty",
			})
			failedCount++
			continue
		}

		req := &service.DeleteObjectRequest{
			Key:      key,
			TenantID: tenantID,
		}

		resp, err := objService.DeleteObject(ctx, req)
		if err != nil {
			results = append(results, BatchDeleteObjectResult{
				Key:     key,
				Success: false,
				Error:   err.Error(),
			})
			failedCount++
			continue
		}

		results = append(results, BatchDeleteObjectResult{
			Key:     key,
			Success: resp.Success,
			Size:    resp.Size,
		})
		successCount++
	}

	response := BatchDeleteObjectsRESTResponse{
		Results:   results,
		Success:   successCount,
		Failed:    failedCount,
		Timestamp: time.Now().UnixNano(),
	}

	RespondJSON(w, r, http.StatusOK, response)
}

// =============================================================================
// Listing Handlers
// =============================================================================

// HandleListObjects lists objects by prefix.
// GET /api/v1/objects.
func (s *Server) HandleListObjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	// Parse query parameters
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	req := &service.ListObjectsRequest{
		Prefix:      r.URL.Query().Get("prefix"),
		TenantID:    r.Header.Get("X-Tenant-ID"),
		Limit:       limit,
		Cursor:      r.URL.Query().Get("cursor"),
		IncludeTags: r.URL.Query().Get("include_tags") == "true",
	}

	resp, err := objService.ListObjects(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleListByTag lists objects by tag.
// GET /api/v1/objects/by-tag.
func (s *Server) HandleListByTag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	tagName := r.URL.Query().Get("name")
	if tagName == "" {
		RespondBadRequest(w, r, "tag name is required")
		return
	}

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	req := &service.ListByTagRequest{
		TagName:  tagName,
		TagValue: r.URL.Query().Get("value"),
		TenantID: r.Header.Get("X-Tenant-ID"),
		Limit:    limit,
		Cursor:   r.URL.Query().Get("cursor"),
	}

	resp, err := objService.ListByTag(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleListByTenant lists objects by tenant.
// GET /api/v1/objects/by-tenant/{tenant}.
func (s *Server) HandleListByTenant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant ID is required")
		return
	}

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	req := &service.ListByTenantRequest{
		TenantID:    tenantID,
		Limit:       limit,
		Cursor:      r.URL.Query().Get("cursor"),
		IncludeTags: r.URL.Query().Get("include_tags") == "true",
	}

	resp, err := objService.ListByTenant(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Operation Handlers
// =============================================================================

// HandleCopyObject copies an object to a new key.
// POST /api/v1/objects/{key:.*}/copy.
func (s *Server) HandleCopyObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := chi.URLParam(r, "*")

	// Remove /copy suffix if present
	if len(key) > 5 && key[len(key)-5:] == "/copy" {
		key = key[:len(key)-5]
	}

	if key == "" {
		RespondBadRequest(w, r, "source key is required")
		return
	}

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	var reqBody CopyObjectRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		RespondBadRequest(w, r, "invalid request body")
		return
	}
	defer r.Body.Close()

	if reqBody.DestinationKey == "" {
		RespondBadRequest(w, r, "destination_key is required")
		return
	}

	req := &service.CopyObjectRequest{
		SourceKey:         key,
		DestinationKey:    reqBody.DestinationKey,
		TenantID:          reqBody.TenantID,
		OverwriteExisting: reqBody.OverwriteExisting,
		TargetBackend:     reqBody.TargetBackend,
	}

	resp, err := objService.CopyObject(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusCreated, resp)
}

// HandleReplicateObject replicates an object to target backends.
// POST /api/v1/objects/{key:.*}/replicate.
func (s *Server) HandleReplicateObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := chi.URLParam(r, "*")

	// Remove /replicate suffix if present
	if len(key) > 10 && key[len(key)-10:] == "/replicate" {
		key = key[:len(key)-10]
	}

	if key == "" {
		RespondBadRequest(w, r, "object key is required")
		return
	}

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	var reqBody ReplicateObjectRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		RespondBadRequest(w, r, "invalid request body")
		return
	}
	defer r.Body.Close()

	if len(reqBody.Targets) == 0 {
		RespondBadRequest(w, r, "at least one target is required")
		return
	}

	req := &service.ReplicateObjectRequest{
		Key:         key,
		Targets:     reqBody.Targets,
		TenantID:    r.Header.Get("X-Tenant-ID"),
		Priority:    reqBody.Priority,
		Synchronous: reqBody.Synchronous,
	}

	resp, err := objService.ReplicateObject(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleMigrateObject migrates an object to a new backend.
// POST /api/v1/objects/{key:.*}/migrate.
func (s *Server) HandleMigrateObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := chi.URLParam(r, "*")

	// Remove /migrate suffix if present
	if len(key) > 8 && key[len(key)-8:] == "/migrate" {
		key = key[:len(key)-8]
	}

	if key == "" {
		RespondBadRequest(w, r, "object key is required")
		return
	}

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	var reqBody MigrateObjectRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		RespondBadRequest(w, r, "invalid request body")
		return
	}
	defer r.Body.Close()

	if reqBody.TargetBackend == "" {
		RespondBadRequest(w, r, "target_backend is required")
		return
	}

	req := &service.MigrateObjectRequest{
		Key:              key,
		TargetBackend:    reqBody.TargetBackend,
		TenantID:         r.Header.Get("X-Tenant-ID"),
		DeleteFromSource: reqBody.DeleteFromSource,
	}

	resp, err := objService.MigrateObject(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleArchiveObject archives an object to cold storage.
// POST /api/v1/objects/{key:.*}/archive.
func (s *Server) HandleArchiveObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := chi.URLParam(r, "*")

	// Remove /archive suffix if present
	if len(key) > 8 && key[len(key)-8:] == "/archive" {
		key = key[:len(key)-8]
	}

	if key == "" {
		RespondBadRequest(w, r, "object key is required")
		return
	}

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	var reqBody ArchiveObjectRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil && err != io.EOF {
		RespondBadRequest(w, r, "invalid request body")
		return
	}
	defer r.Body.Close()

	req := &service.ArchiveObjectRequest{
		Key:               key,
		TargetStore:       reqBody.TargetStore,
		TenantID:          r.Header.Get("X-Tenant-ID"),
		DeleteFromPrimary: reqBody.DeleteFromPrimary,
	}

	resp, err := objService.ArchiveObject(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleRestoreObject restores an archived object.
// POST /api/v1/objects/{key:.*}/restore.
func (s *Server) HandleRestoreObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := chi.URLParam(r, "*")

	// Remove /restore suffix if present
	if len(key) > 8 && key[len(key)-8:] == "/restore" {
		key = key[:len(key)-8]
	}

	if key == "" {
		RespondBadRequest(w, r, "object key is required")
		return
	}

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	var reqBody RestoreObjectRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil && err != io.EOF {
		RespondBadRequest(w, r, "invalid request body")
		return
	}
	defer r.Body.Close()

	req := &service.RestoreObjectRequest{
		Key:           key,
		TargetBackend: reqBody.TargetBackend,
		TenantID:      r.Header.Get("X-Tenant-ID"),
	}

	resp, err := objService.RestoreObject(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleVerifyObject verifies object integrity.
// POST /api/v1/objects/{key:.*}/verify.
func (s *Server) HandleVerifyObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := chi.URLParam(r, "*")

	// Remove /verify suffix if present
	if len(key) > 7 && key[len(key)-7:] == "/verify" {
		key = key[:len(key)-7]
	}

	if key == "" {
		RespondBadRequest(w, r, "object key is required")
		return
	}

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	var reqBody VerifyObjectRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil && err != io.EOF {
		RespondBadRequest(w, r, "invalid request body")
		return
	}
	defer r.Body.Close()

	req := &service.VerifyObjectRequest{
		Key:              key,
		Stores:           reqBody.Stores,
		TenantID:         r.Header.Get("X-Tenant-ID"),
		RepairMismatches: reqBody.RepairMismatches,
	}

	resp, err := objService.VerifyObject(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Policy Handlers
// =============================================================================

// HandleGetObjectPolicy retrieves object policies.
// GET /api/v1/objects/{key:.*}/policy.
func (s *Server) HandleGetObjectPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := chi.URLParam(r, "*")

	// Remove /policy suffix if present
	if len(key) > 7 && key[len(key)-7:] == "/policy" {
		key = key[:len(key)-7]
	}

	if key == "" {
		RespondBadRequest(w, r, "object key is required")
		return
	}

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	req := &service.GetObjectPolicyRequest{
		Key:      key,
		TenantID: r.Header.Get("X-Tenant-ID"),
	}

	resp, err := objService.GetObjectPolicy(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleSetObjectPolicy sets object policies.
// PUT /api/v1/objects/{key:.*}/policy.
func (s *Server) HandleSetObjectPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := chi.URLParam(r, "*")

	// Remove /policy suffix if present
	if len(key) > 7 && key[len(key)-7:] == "/policy" {
		key = key[:len(key)-7]
	}

	if key == "" {
		RespondBadRequest(w, r, "object key is required")
		return
	}

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	var reqBody SetObjectPolicyRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		RespondBadRequest(w, r, "invalid request body")
		return
	}
	defer r.Body.Close()

	req := &service.SetObjectPolicyRequest{
		Key:               key,
		TenantID:          r.Header.Get("X-Tenant-ID"),
		ReplicationPolicy: reqBody.ReplicationPolicy,
		ArchivePolicy:     reqBody.ArchivePolicy,
		RetentionPolicy:   reqBody.RetentionPolicy,
	}

	resp, err := objService.SetObjectPolicy(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Backend Management Handlers
// =============================================================================

// HandleListBackends returns available storage backends.
// GET /api/v1/backends.
func (s *Server) HandleListObjectBackends(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	req := &service.ListBackendsRequest{
		IncludeHealth: r.URL.Query().Get("health") == "true",
		FilterByType:  r.URL.Query().Get("type"),
	}

	resp, err := objService.ListBackends(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetBackendHealth returns backend health status.
// GET /api/v1/backends/{name}/health.
func (s *Server) HandleGetObjectBackendHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondBadRequest(w, r, "backend name is required")
		return
	}

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	req := &service.GetBackendHealthRequest{
		Name: name,
	}

	resp, err := objService.GetBackendHealth(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Tenant Management Handlers
// =============================================================================

// HandleGetTenantUsage returns storage usage for a tenant.
// GET /api/v1/tenants/{tenant}/usage.
func (s *Server) HandleGetTenantUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant ID is required")
		return
	}

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	req := &service.GetTenantUsageRequest{
		TenantID: tenantID,
	}

	resp, err := objService.GetTenantUsage(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetTenantQuotas returns quota settings for a tenant.
// GET /api/v1/tenants/{tenant}/quotas.
func (s *Server) HandleGetTenantQuotas(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant ID is required")
		return
	}

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	req := &service.GetTenantQuotasRequest{
		TenantID: tenantID,
	}

	resp, err := objService.GetTenantQuotas(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleSetTenantQuotas sets tenant quotas.
// PUT /api/v1/tenants/{tenant}/quotas.
func (s *Server) HandleSetTenantQuotas(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant ID is required")
		return
	}

	objService := s.service.ObjectStorageService()
	if objService == nil {
		RespondServiceUnavailable(w, r, "object storage service not available")
		return
	}

	var reqBody SetTenantQuotasRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		RespondBadRequest(w, r, "invalid request body")
		return
	}
	defer r.Body.Close()

	req := &service.SetTenantQuotasRequest{
		TenantID:        tenantID,
		MaxStorageBytes: reqBody.MaxStorageBytes,
		MaxObjectSize:   reqBody.MaxObjectSize,
		MaxObjectCount:  reqBody.MaxObjectCount,
	}

	resp, err := objService.SetTenantQuotas(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Routing Helper
// =============================================================================

// handleObjectAction routes object operations based on path suffix.
func (s *Server) handleObjectAction(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")

	switch r.Method {
	case http.MethodGet:
		if len(path) > 7 && path[len(path)-7:] == "/policy" {
			s.HandleGetObjectPolicy(w, r)
			return
		}
		s.HandleGetObject(w, r)

	case http.MethodPut:
		if len(path) > 7 && path[len(path)-7:] == "/policy" {
			s.HandleSetObjectPolicy(w, r)
			return
		}
		s.HandlePutObject(w, r)

	case http.MethodPost:
		if len(path) > 5 && path[len(path)-5:] == "/copy" {
			s.HandleCopyObject(w, r)
			return
		}
		if len(path) > 10 && path[len(path)-10:] == "/replicate" {
			s.HandleReplicateObject(w, r)
			return
		}
		if len(path) > 8 && path[len(path)-8:] == "/migrate" {
			s.HandleMigrateObject(w, r)
			return
		}
		if len(path) > 8 && path[len(path)-8:] == "/archive" {
			s.HandleArchiveObject(w, r)
			return
		}
		if len(path) > 8 && path[len(path)-8:] == "/restore" {
			s.HandleRestoreObject(w, r)
			return
		}
		if len(path) > 7 && path[len(path)-7:] == "/verify" {
			s.HandleVerifyObject(w, r)
			return
		}
		RespondBadRequest(w, r, "unknown object action")

	case http.MethodDelete:
		s.HandleDeleteObject(w, r)

	case http.MethodHead:
		s.HandleHeadObject(w, r)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// =============================================================================
// Error Handling
// =============================================================================

// handleObjectStorageError converts service errors to HTTP responses.
func (s *Server) handleObjectStorageError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}

	var dragonErr *errors.DragonError
	if !errors.As(err, &dragonErr) {
		RespondInternalError(w, r, err)
		return
	}

	switch dragonErr.Code {
	case errors.ErrNotFound:
		RespondNotFound(w, r, "object")
	case errors.ErrAlreadyExists:
		RespondConflict(w, r, dragonErr.Error())
	case errors.ErrInvalidInput:
		RespondBadRequest(w, r, dragonErr.Error())
	case errors.ErrForbidden:
		RespondError(w, r, http.StatusForbidden, dragonErr)
	case errors.ErrResourceExhausted:
		RespondError(w, r, http.StatusTooManyRequests, dragonErr)
	case errors.ErrTimeout:
		RespondError(w, r, http.StatusGatewayTimeout, dragonErr)
	case errors.ErrStorage:
		RespondInternalError(w, r, dragonErr)
	default:
		RespondInternalError(w, r, dragonErr)
	}
}

// =============================================================================
// Route Registration
// =============================================================================

// registerObjectStorageRoutes registers all object storage routes.
func registerObjectStorageRoutes(s *Server, r chi.Router) {
	// Object listing routes (must be registered before wildcard routes)
	r.Get("/objects", s.HandleListObjects)
	r.Get("/objects/by-tag", s.HandleListByTag)
	r.Get("/objects/by-tenant/{tenant}", s.HandleListByTenant)

	// Batch routes
	r.Post("/objects/batch", s.HandleBatchPutObjects)
	r.Post("/objects/batch/get", s.HandleBatchGetObjects)
	r.Post("/objects/batch/delete", s.HandleBatchDeleteObjects)

	// Object CRUD and action routes using wildcard
	r.Route("/objects", func(r chi.Router) {
		r.Put("/*", s.handleObjectAction)
		r.Get("/*", s.handleObjectAction)
		r.Delete("/*", s.handleObjectAction)
		r.Head("/*", s.handleObjectAction)
		r.Post("/*", s.handleObjectAction)
	})

	// Backend management routes
	r.Get("/object-backends", s.HandleListObjectBackends)
	r.Get("/object-backends/{name}/health", s.HandleGetObjectBackendHealth)

	// Tenant management routes
	r.Get("/tenants/{tenant}/usage", s.HandleGetTenantUsage)
	r.Get("/tenants/{tenant}/quotas", s.HandleGetTenantQuotas)
	r.Put("/tenants/{tenant}/quotas", s.HandleSetTenantQuotas)
}
