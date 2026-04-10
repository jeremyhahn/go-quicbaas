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
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// HandleCreateDEK handles POST /api/v1/kms/deks - Create a new data encryption key.
func (s *Server) HandleCreateDEK(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req service.CreateDEKRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.Path == "" {
		RespondBadRequest(w, r, "path is required")
		return
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	resp, err := kmsService.CreateDEK(ctx, &req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusCreated, resp)
}

// HandleGetDEK handles GET /api/v1/kms/deks/{path} - Get a data encryption key.
func (s *Server) HandleGetDEK(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract path from URL - handle nested paths
	path := chi.URLParam(r, "*")
	if path == "" {
		RespondBadRequest(w, r, "path is required")
		return
	}

	// URL-decode the path in case it was encoded by the client
	decodedPath, err := url.PathUnescape(path)
	if err == nil {
		path = decodedPath
	}

	// Parse optional version from query parameter
	var version uint64
	if versionStr := r.URL.Query().Get("version"); versionStr != "" {
		v, err := strconv.ParseUint(versionStr, 10, 64)
		if err != nil {
			RespondBadRequest(w, r, "invalid version format")
			return
		}
		version = v
	}

	req := &service.GetDEKRequest{
		Path:    path,
		Version: version,
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	resp, err := kmsService.GetDEK(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleListDEKs handles GET /api/v1/kms/deks - List data encryption keys.
func (s *Server) HandleListDEKs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		l, err := strconv.Atoi(limitStr)
		if err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	cursor := r.URL.Query().Get("cursor")
	prefix := r.URL.Query().Get("prefix")
	recursive := r.URL.Query().Get("recursive") == "true"

	req := &service.ListDEKsRequest{
		Prefix:    prefix,
		Recursive: recursive,
		Limit:     limit,
		Cursor:    cursor,
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	resp, err := kmsService.ListDEKs(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleRotateDEK handles POST /api/v1/kms/deks/{path}/rotate - Rotate a data encryption key.
func (s *Server) HandleRotateDEK(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract path from URL - need to strip /rotate suffix
	fullPath := chi.URLParam(r, "*")
	if fullPath == "" {
		RespondBadRequest(w, r, "path is required")
		return
	}

	// URL-decode the path in case it was encoded by the client
	if decodedPath, err := url.PathUnescape(fullPath); err == nil {
		fullPath = decodedPath
	}

	// Remove /rotate suffix if present
	path := strings.TrimSuffix(fullPath, "/rotate")
	if path == "" {
		RespondBadRequest(w, r, "path is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil && !stderrors.Is(err, io.EOF) {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req service.RotateDEKRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}

	req.Path = path

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	resp, err := kmsService.RotateDEK(ctx, &req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleDeleteDEK handles DELETE /api/v1/kms/deks/{path} - Delete a data encryption key.
func (s *Server) HandleDeleteDEK(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract path from URL
	path := chi.URLParam(r, "*")
	if path == "" {
		RespondBadRequest(w, r, "path is required")
		return
	}

	// URL-decode the path in case it was encoded by the client
	if decodedPath, err := url.PathUnescape(path); err == nil {
		path = decodedPath
	}

	body, err := io.ReadAll(r.Body)
	if err != nil && !stderrors.Is(err, io.EOF) {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req service.DeleteDEKRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}

	req.Path = path

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	resp, err := kmsService.DeleteDEK(ctx, &req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetDEKStatus handles GET /api/v1/kms/deks/{path}/status - Get data encryption key status.
func (s *Server) HandleGetDEKStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract path from URL - need to strip /status suffix
	fullPath := chi.URLParam(r, "*")
	if fullPath == "" {
		RespondBadRequest(w, r, "path is required")
		return
	}

	// URL-decode the path in case it was encoded by the client
	if decodedPath, err := url.PathUnescape(fullPath); err == nil {
		fullPath = decodedPath
	}

	// Remove /status suffix if present
	path := strings.TrimSuffix(fullPath, "/status")
	if path == "" {
		RespondBadRequest(w, r, "path is required")
		return
	}

	req := &service.GetDEKStatusRequest{
		Path: path,
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	resp, err := kmsService.GetDEKStatus(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleSetDefaultDEK handles POST /api/v1/kms/deks/{path}/set-default - Set a data encryption key as default.
func (s *Server) HandleSetDefaultDEK(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract path from URL - need to strip /set-default suffix
	fullPath := chi.URLParam(r, "*")
	if fullPath == "" {
		RespondBadRequest(w, r, "path is required")
		return
	}

	// URL-decode the path in case it was encoded by the client
	if decodedPath, err := url.PathUnescape(fullPath); err == nil {
		fullPath = decodedPath
	}

	// Remove /set-default suffix if present
	path := strings.TrimSuffix(fullPath, "/set-default")
	if path == "" {
		RespondBadRequest(w, r, "path is required")
		return
	}

	req := &service.SetDefaultDEKRequest{
		Path: path,
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	resp, err := kmsService.SetDefaultDEK(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleSetDefaultDEKFromBody handles PUT /api/v1/kms/deks/default - Set a data encryption key as default.
// This variant reads the DEK path from the request body instead of the URL.
func (s *Server) HandleSetDefaultDEKFromBody(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req service.SetDefaultDEKRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.Path == "" {
		RespondBadRequest(w, r, "path is required")
		return
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	resp, err := kmsService.SetDefaultDEK(ctx, &req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleSetDEKSafety handles PUT /api/v1/kms/deks/{path}/safety - Set data encryption key safety limits.
func (s *Server) HandleSetDEKSafety(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract path from URL - need to strip /safety suffix
	fullPath := chi.URLParam(r, "*")
	if fullPath == "" {
		RespondBadRequest(w, r, "path is required")
		return
	}

	// URL-decode the path in case it was encoded by the client
	if decodedPath, err := url.PathUnescape(fullPath); err == nil {
		fullPath = decodedPath
	}

	// Remove /safety suffix if present
	path := strings.TrimSuffix(fullPath, "/safety")
	if path == "" {
		RespondBadRequest(w, r, "path is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req service.SetDEKSafetyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	req.Path = path

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	resp, err := kmsService.SetDEKSafety(ctx, &req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleClearDEKSafety handles DELETE /api/v1/kms/deks/{path}/safety - Clear data encryption key safety limits.
func (s *Server) HandleClearDEKSafety(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract path from URL - need to strip /safety suffix
	fullPath := chi.URLParam(r, "*")
	if fullPath == "" {
		RespondBadRequest(w, r, "path is required")
		return
	}

	// URL-decode the path in case it was encoded by the client
	if decodedPath, err := url.PathUnescape(fullPath); err == nil {
		fullPath = decodedPath
	}

	// Remove /safety suffix if present
	path := strings.TrimSuffix(fullPath, "/safety")
	if path == "" {
		RespondBadRequest(w, r, "path is required")
		return
	}

	req := &service.ClearDEKSafetyRequest{
		Path: path,
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	resp, err := kmsService.ClearDEKSafety(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleStartReencryption handles POST /api/v1/kms/deks/{path}/reencrypt - Start re-encryption operation.
func (s *Server) HandleStartReencryption(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract path from URL - need to strip /reencrypt suffix
	fullPath := chi.URLParam(r, "*")
	if fullPath == "" {
		RespondBadRequest(w, r, "path is required")
		return
	}

	// URL-decode the path in case it was encoded by the client
	if decodedPath, err := url.PathUnescape(fullPath); err == nil {
		fullPath = decodedPath
	}

	// Remove /reencrypt suffix if present
	path := strings.TrimSuffix(fullPath, "/reencrypt")
	if path == "" {
		RespondBadRequest(w, r, "path is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil && !stderrors.Is(err, io.EOF) {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req service.StartReencryptionRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}

	req.Path = path

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	resp, err := kmsService.StartReencryption(ctx, &req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusAccepted, resp)
}

// HandleGetReencryptionProgress handles GET /api/v1/kms/deks/{path}/reencrypt - Get re-encryption progress.
func (s *Server) HandleGetReencryptionProgress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract path from URL - need to strip /reencrypt suffix
	fullPath := chi.URLParam(r, "*")
	if fullPath == "" {
		RespondBadRequest(w, r, "path is required")
		return
	}

	// URL-decode the path in case it was encoded by the client
	if decodedPath, err := url.PathUnescape(fullPath); err == nil {
		fullPath = decodedPath
	}

	// Remove /reencrypt suffix if present
	path := strings.TrimSuffix(fullPath, "/reencrypt")
	if path == "" {
		RespondBadRequest(w, r, "path is required")
		return
	}

	// Parse optional operation_id from query parameter
	operationID := r.URL.Query().Get("operation_id")
	verbose := r.URL.Query().Get("verbose") == "true"

	req := &service.GetReencryptionProgressRequest{
		Path:        path,
		OperationID: operationID,
		Verbose:     verbose,
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	resp, err := kmsService.GetReencryptionProgress(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}
