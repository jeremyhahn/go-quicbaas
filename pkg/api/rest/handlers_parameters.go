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
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Parameter Store Handlers
// =============================================================================

// HandleGetParameter handles GET /api/v1/parameters/* - Get a single parameter.
//
// Request:
//
//	GET /api/v1/parameters/{path}?with_decryption=true
//
// Response (200 OK):
//
//	{
//	  "parameter": {
//	    "path": "/app/database/host",
//	    "value": "base64-encoded-value",
//	    "type": "string",
//	    "description": "Database host",
//	    "tags": {"env": "prod"},
//	    "is_encrypted": true,
//	    "created_at": "2024-01-01T00:00:00Z",
//	    "updated_at": "2024-01-01T00:00:00Z"
//	  },
//	  "timestamp": "2024-01-01T00:00:00Z"
//	}
func (s *Server) HandleGetParameter(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract path from wildcard
	path := chi.URLParam(r, "*")
	if path == "" {
		RespondBadRequest(w, r, "parameter path is required")
		return
	}

	// URL-decode the path
	if decodedPath, err := url.PathUnescape(path); err == nil {
		path = decodedPath
	}

	// Ensure path starts with /
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// Parse query parameters
	withDecryption := r.URL.Query().Get("with_decryption") != "false"

	req := &interfaces.GetParameterRequest{
		Path:           path,
		WithDecryption: withDecryption,
	}

	paramService := s.service.ParameterStoreService()
	if paramService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrParameterStoreUnavailable)
		return
	}

	resp, err := paramService.GetParameter(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleSetParameter handles PUT /api/v1/parameters/* - Create or update a parameter.
//
// Request:
//
//	PUT /api/v1/parameters/{path}
//	Content-Type: application/json
//	{
//	  "value": "base64-encoded-value",
//	  "type": "string",
//	  "description": "Parameter description",
//	  "kms_key_id": "default",
//	  "tags": {"env": "prod"},
//	  "ttl_seconds": 3600,
//	  "audit_enabled": true,
//	  "overwrite": true
//	}
//
// Response (200 OK):
//
//	{
//	  "path": "/app/database/host",
//	  "created": true,
//	  "timestamp": "2024-01-01T00:00:00Z"
//	}
func (s *Server) HandleSetParameter(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract path from wildcard
	path := chi.URLParam(r, "*")
	if path == "" {
		RespondBadRequest(w, r, "parameter path is required")
		return
	}

	// URL-decode the path
	if decodedPath, err := url.PathUnescape(path); err == nil {
		path = decodedPath
	}

	// Ensure path starts with /
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody setParameterRequest
	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body: "+err.Error())
		return
	}

	// Validate value
	if len(reqBody.Value) == 0 {
		RespondBadRequest(w, r, "value is required")
		return
	}

	req := &interfaces.SetParameterRequest{
		Path:         path,
		Value:        reqBody.Value,
		Type:         reqBody.Type,
		Description:  reqBody.Description,
		KMSKeyID:     reqBody.KMSKeyID,
		Tags:         reqBody.Tags,
		TTLSeconds:   reqBody.TTLSeconds,
		AuditEnabled: reqBody.AuditEnabled,
		Overwrite:    reqBody.Overwrite,
	}

	// Default overwrite to true if not specified
	if !reqBody.OverwriteSet {
		req.Overwrite = true
	}

	paramService := s.service.ParameterStoreService()
	if paramService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrParameterStoreUnavailable)
		return
	}

	resp, err := paramService.SetParameter(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	statusCode := http.StatusOK
	if resp.Created {
		statusCode = http.StatusCreated
	}

	RespondJSON(w, r, statusCode, resp)
}

// HandleDeleteParameter handles DELETE /api/v1/parameters/* - Delete a parameter.
//
// Request:
//
//	DELETE /api/v1/parameters/{path}
//
// Response (200 OK):
//
//	{
//	  "path": "/app/database/host",
//	  "deleted": true,
//	  "timestamp": "2024-01-01T00:00:00Z"
//	}
func (s *Server) HandleDeleteParameter(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract path from wildcard
	path := chi.URLParam(r, "*")
	if path == "" {
		RespondBadRequest(w, r, "parameter path is required")
		return
	}

	// URL-decode the path
	if decodedPath, err := url.PathUnescape(path); err == nil {
		path = decodedPath
	}

	// Ensure path starts with /
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	req := &interfaces.DeleteParameterRequest{
		Path: path,
	}

	paramService := s.service.ParameterStoreService()
	if paramService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrParameterStoreUnavailable)
		return
	}

	resp, err := paramService.DeleteParameter(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleListParameters handles GET /api/v1/parameters - List parameters by prefix.
//
// Request:
//
//	GET /api/v1/parameters?prefix=/app/&recursive=true&limit=100&cursor=xxx&with_values=false
//
// Response (200 OK):
//
//	{
//	  "parameters": [
//	    {"path": "/app/database/host", ...},
//	    {"path": "/app/database/port", ...}
//	  ],
//	  "next_cursor": "xxx",
//	  "has_more": true,
//	  "total_count": 150,
//	  "timestamp": "2024-01-01T00:00:00Z"
//	}
func (s *Server) HandleListParameters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters
	prefix := r.URL.Query().Get("prefix")
	recursive := r.URL.Query().Get("recursive") != "false"
	withValues := r.URL.Query().Get("with_values") == "true"
	withDecryption := r.URL.Query().Get("with_decryption") == "true"
	cursor := r.URL.Query().Get("cursor")

	// Parse limit
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	req := &interfaces.ListParametersRequest{
		Prefix:         prefix,
		Recursive:      recursive,
		WithDecryption: withDecryption,
		WithValues:     withValues,
		Limit:          limit,
		Cursor:         cursor,
	}

	paramService := s.service.ParameterStoreService()
	if paramService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrParameterStoreUnavailable)
		return
	}

	resp, err := paramService.ListParameters(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetParametersBatch handles POST /api/v1/parameters/batch/get - Batch get parameters.
//
// Request:
//
//	POST /api/v1/parameters/batch/get
//	Content-Type: application/json
//	{
//	  "paths": ["/app/database/host", "/app/database/port"],
//	  "with_decryption": true
//	}
//
// Response (200 OK):
//
//	{
//	  "parameters": [
//	    {"path": "/app/database/host", "value": "...", ...},
//	    {"path": "/app/database/port", "value": "...", ...}
//	  ],
//	  "errors": {"/app/missing": "parameter not found"},
//	  "timestamp": "2024-01-01T00:00:00Z"
//	}
func (s *Server) HandleGetParametersBatch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody getParametersBatchRequest
	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body: "+err.Error())
		return
	}

	if len(reqBody.Paths) == 0 {
		RespondBadRequest(w, r, "at least one path is required")
		return
	}

	req := &interfaces.GetParametersRequest{
		Paths:          reqBody.Paths,
		WithDecryption: reqBody.WithDecryption,
	}

	paramService := s.service.ParameterStoreService()
	if paramService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrParameterStoreUnavailable)
		return
	}

	resp, err := paramService.GetParameters(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleSetParametersBatch handles POST /api/v1/parameters/batch/set - Batch set parameters.
//
// Request:
//
//	POST /api/v1/parameters/batch/set
//	Content-Type: application/json
//	{
//	  "parameters": [
//	    {"path": "/app/database/host", "value": "...", "type": "string"},
//	    {"path": "/app/database/port", "value": "...", "type": "number"}
//	  ]
//	}
//
// Response (200 OK):
//
//	{
//	  "results": [
//	    {"path": "/app/database/host", "created": true, "success": true},
//	    {"path": "/app/database/port", "created": false, "success": true}
//	  ],
//	  "timestamp": "2024-01-01T00:00:00Z"
//	}
func (s *Server) HandleSetParametersBatch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody setParametersBatchRequest
	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body: "+err.Error())
		return
	}

	if len(reqBody.Parameters) == 0 {
		RespondBadRequest(w, r, "at least one parameter is required")
		return
	}

	// Convert to interface types
	params := make([]*interfaces.SetParameterRequest, len(reqBody.Parameters))
	for i, p := range reqBody.Parameters {
		if p.Path == "" {
			RespondBadRequest(w, r, "parameter path is required at index "+strconv.Itoa(i))
			return
		}
		if len(p.Value) == 0 {
			RespondBadRequest(w, r, "parameter value is required at index "+strconv.Itoa(i))
			return
		}

		params[i] = &interfaces.SetParameterRequest{
			Path:         p.Path,
			Value:        p.Value,
			Type:         p.Type,
			Description:  p.Description,
			KMSKeyID:     p.KMSKeyID,
			Tags:         p.Tags,
			TTLSeconds:   p.TTLSeconds,
			AuditEnabled: p.AuditEnabled,
			Overwrite:    true, // Default to overwrite for batch
		}
	}

	req := &interfaces.SetParametersRequest{
		Parameters: params,
	}

	paramService := s.service.ParameterStoreService()
	if paramService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrParameterStoreUnavailable)
		return
	}

	resp, err := paramService.SetParameters(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleDeleteParametersBatch handles POST /api/v1/parameters/batch/delete - Batch delete parameters.
//
// Request:
//
//	POST /api/v1/parameters/batch/delete
//	Content-Type: application/json
//	{
//	  "paths": ["/app/database/host", "/app/database/port"]
//	}
//
// Response (200 OK):
//
//	{
//	  "results": [
//	    {"path": "/app/database/host", "deleted": true},
//	    {"path": "/app/database/port", "deleted": true}
//	  ],
//	  "deleted_count": 2,
//	  "timestamp": "2024-01-01T00:00:00Z"
//	}
func (s *Server) HandleDeleteParametersBatch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody deleteParametersBatchRequest
	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body: "+err.Error())
		return
	}

	if len(reqBody.Paths) == 0 {
		RespondBadRequest(w, r, "at least one path is required")
		return
	}

	req := &interfaces.DeleteParametersRequest{
		Paths: reqBody.Paths,
	}

	paramService := s.service.ParameterStoreService()
	if paramService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrParameterStoreUnavailable)
		return
	}

	resp, err := paramService.DeleteParameters(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleSearchParametersByTags handles POST /api/v1/parameters/search - Search parameters by tags.
//
// Request:
//
//	POST /api/v1/parameters/search
//	Content-Type: application/json
//	{
//	  "tags": {"env": "prod", "service": "database"},
//	  "prefix": "/app/",
//	  "with_values": false,
//	  "with_decryption": false,
//	  "limit": 100,
//	  "cursor": "xxx"
//	}
//
// Response (200 OK):
//
//	{
//	  "parameters": [
//	    {"path": "/app/database/host", ...},
//	    {"path": "/app/database/port", ...}
//	  ],
//	  "next_cursor": "xxx",
//	  "has_more": true,
//	  "timestamp": "2024-01-01T00:00:00Z"
//	}
func (s *Server) HandleSearchParametersByTags(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody searchParametersRequest
	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body: "+err.Error())
		return
	}

	if len(reqBody.Tags) == 0 {
		RespondBadRequest(w, r, "at least one tag is required")
		return
	}

	req := &interfaces.SearchByTagsRequest{
		Tags:           reqBody.Tags,
		Prefix:         reqBody.Prefix,
		WithValues:     reqBody.WithValues,
		WithDecryption: reqBody.WithDecryption,
		Limit:          reqBody.Limit,
		Cursor:         reqBody.Cursor,
	}

	// Default limit
	if req.Limit <= 0 {
		req.Limit = 100
	}

	paramService := s.service.ParameterStoreService()
	if paramService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrParameterStoreUnavailable)
		return
	}

	resp, err := paramService.SearchByTags(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Request/Response Types
// =============================================================================

// setParameterRequest is the request body for setting a parameter.
type setParameterRequest struct {
	Value        []byte            `json:"value"`
	Type         string            `json:"type,omitempty"`
	Description  string            `json:"description,omitempty"`
	KMSKeyID     string            `json:"kms_key_id,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
	TTLSeconds   int64             `json:"ttl_seconds,omitempty"`
	AuditEnabled bool              `json:"audit_enabled,omitempty"`
	Overwrite    bool              `json:"overwrite,omitempty"`
	OverwriteSet bool              `json:"-"` // Internal flag to track if overwrite was explicitly set
}

// UnmarshalJSON custom unmarshaler to track if overwrite was set.
func (r *setParameterRequest) UnmarshalJSON(data []byte) error {
	type alias setParameterRequest
	aux := &struct {
		Overwrite *bool `json:"overwrite,omitempty"`
		*alias
	}{
		alias: (*alias)(r),
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	if aux.Overwrite != nil {
		r.Overwrite = *aux.Overwrite
		r.OverwriteSet = true
	}

	return nil
}

// getParametersBatchRequest is the request body for batch get.
type getParametersBatchRequest struct {
	Paths          []string `json:"paths"`
	WithDecryption bool     `json:"with_decryption,omitempty"`
}

// batchSetParameterEntry is a single entry in a batch set request.
type batchSetParameterEntry struct {
	Path         string            `json:"path"`
	Value        []byte            `json:"value"`
	Type         string            `json:"type,omitempty"`
	Description  string            `json:"description,omitempty"`
	KMSKeyID     string            `json:"kms_key_id,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
	TTLSeconds   int64             `json:"ttl_seconds,omitempty"`
	AuditEnabled bool              `json:"audit_enabled,omitempty"`
}

// setParametersBatchRequest is the request body for batch set.
type setParametersBatchRequest struct {
	Parameters []batchSetParameterEntry `json:"parameters"`
}

// deleteParametersBatchRequest is the request body for batch delete.
type deleteParametersBatchRequest struct {
	Paths []string `json:"paths"`
}

// searchParametersRequest is the request body for tag search.
type searchParametersRequest struct {
	Tags           map[string]string `json:"tags"`
	Prefix         string            `json:"prefix,omitempty"`
	WithValues     bool              `json:"with_values,omitempty"`
	WithDecryption bool              `json:"with_decryption,omitempty"`
	Limit          int               `json:"limit,omitempty"`
	Cursor         string            `json:"cursor,omitempty"`
}
