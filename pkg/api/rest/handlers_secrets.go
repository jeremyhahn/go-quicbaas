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

	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// HandleSecretsAction handles POST /v1/secrets/{path...} with ?action= query param.
// Dispatches to appropriate handler based on action: rollback, rotate, destroy, or store (default).
func (s *Server) HandleSecretsAction(w http.ResponseWriter, r *http.Request) {
	action := r.URL.Query().Get("action")
	switch action {
	case "rollback":
		s.HandleRollbackSecret(w, r)
	case "rotate":
		s.HandleRotateSecret(w, r)
	case "destroy":
		s.HandleDestroySecret(w, r)
	default:
		s.HandleStoreSecret(w, r)
	}
}

// HandleGetSecretOrVersions handles GET /v1/secrets/{path...} with optional ?versions=true.
// Dispatches to HandleListVersions if versions=true, otherwise HandleGetSecret.
func (s *Server) HandleGetSecretOrVersions(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("versions") == "true" {
		s.HandleListVersions(w, r)
		return
	}
	s.HandleGetSecret(w, r)
}

// getSecretsService returns the secrets service or responds with an error.
func (s *Server) getSecretsService(w http.ResponseWriter, r *http.Request) service.SecretsService {
	secretsService := s.service.Secrets()
	if secretsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrSecretsServiceUnavailable)
		return nil
	}
	return secretsService
}

// HandleStoreSecret handles POST /v1/secrets/{path...} - Store secret.
func (s *Server) HandleStoreSecret(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	path := chi.URLParam(r, "*")
	if path == "" {
		RespondBadRequest(w, r, "secret path is required")
		return
	}

	// URL-decode the path in case it was encoded by the client
	if decodedPath, err := url.PathUnescape(path); err == nil {
		path = decodedPath
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody struct {
		Data        json.RawMessage   `json:"data"`
		Metadata    map[string]string `json:"metadata,omitempty"`
		Description string            `json:"description,omitempty"`
		Algorithm   string            `json:"algorithm,omitempty"`
		KeyID       string            `json:"key_id,omitempty"`
		TenantID    string            `json:"tenant_id,omitempty"`
	}

	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if len(reqBody.Data) == 0 {
		RespondBadRequest(w, r, "secret data is required")
		return
	}

	// Validate algorithm if provided (case-insensitive comparison)
	if reqBody.Algorithm != "" {
		isValidAlgorithm := false
		for _, supported := range secrets.SupportedAlgorithms {
			if strings.EqualFold(reqBody.Algorithm, supported) {
				isValidAlgorithm = true
				break
			}
		}
		if !isValidAlgorithm {
			RespondBadRequest(w, r, "invalid algorithm: supported algorithms are "+strings.Join(secrets.SupportedAlgorithms, ", "))
			return
		}
	}

	// Handle data as either raw bytes (base64) or JSON object
	var secretData []byte
	switch {
	case len(reqBody.Data) > 0 && reqBody.Data[0] == '"':
		// Data is a base64-encoded string
		if err := json.Unmarshal(reqBody.Data, &secretData); err != nil {
			RespondBadRequest(w, r, "invalid base64 data")
			return
		}
	case len(reqBody.Data) > 0 && reqBody.Data[0] == '{':
		// Data is a JSON object - store as-is
		secretData = reqBody.Data
	default:
		// Try to unmarshal as raw bytes
		if err := json.Unmarshal(reqBody.Data, &secretData); err != nil {
			// If that fails, use the raw JSON
			secretData = reqBody.Data
		}
	}

	// Validate that secret data is not empty after processing
	if len(secretData) == 0 {
		RespondBadRequest(w, r, "secret data cannot be empty")
		return
	}

	req := &service.StoreSecretRequest{
		Name:        path,
		Data:        secretData,
		Description: reqBody.Description,
		Metadata:    reqBody.Metadata,
		Algorithm:   reqBody.Algorithm,
		KeyID:       reqBody.KeyID,
		TenantID:    reqBody.TenantID,
	}

	secretsService := s.getSecretsService(w, r)
	if secretsService == nil {
		return
	}

	resp, err := secretsService.StoreSecret(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusCreated, resp)
}

// HandleGetSecret handles GET /v1/secrets/{path...} - Get secret.
func (s *Server) HandleGetSecret(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	path := chi.URLParam(r, "*")
	if path == "" {
		RespondBadRequest(w, r, "secret path is required")
		return
	}

	// URL-decode the path in case it was encoded by the client
	if decodedPath, err := url.PathUnescape(path); err == nil {
		path = decodedPath
	}

	versionStr := r.URL.Query().Get("version")
	var version uint64
	if versionStr != "" {
		v, err := strconv.ParseUint(versionStr, 10, 64)
		if err != nil {
			RespondBadRequest(w, r, "invalid version parameter")
			return
		}
		version = v
	}

	req := &service.GetSecretRequest{
		SecretID: path,
		Version:  version,
	}

	secretsService := s.getSecretsService(w, r)
	if secretsService == nil {
		return
	}

	resp, err := secretsService.GetSecret(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleListSecrets handles GET /v1/secrets - List secrets.
func (s *Server) HandleListSecrets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		l, err := strconv.Atoi(limitStr)
		if err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	cursor := r.URL.Query().Get("cursor")
	prefix := r.URL.Query().Get("prefix")

	req := &service.ListSecretsRequest{
		Limit:  limit,
		Cursor: cursor,
		Prefix: prefix,
	}

	secretsService := s.getSecretsService(w, r)
	if secretsService == nil {
		return
	}

	resp, err := secretsService.ListSecrets(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleDeleteSecret handles DELETE /v1/secrets/{path...} - Delete secret.
// Accepts ?permanent=true or ?destroy=true for permanent deletion.
func (s *Server) HandleDeleteSecret(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	path := chi.URLParam(r, "*")
	if path == "" {
		RespondBadRequest(w, r, "secret path is required")
		return
	}

	// URL-decode the path in case it was encoded by the client
	if decodedPath, err := url.PathUnescape(path); err == nil {
		path = decodedPath
	}

	permanent := r.URL.Query().Get("permanent") == "true" ||
		r.URL.Query().Get("destroy") == "true"

	req := &service.DeleteSecretRequest{
		SecretID:          path,
		PermanentlyDelete: permanent,
	}

	secretsService := s.getSecretsService(w, r)
	if secretsService == nil {
		return
	}

	resp, err := secretsService.DeleteSecret(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleDestroySecret handles POST /v1/secrets/{path...}?action=destroy - Destroy all versions.
// Also supports legacy POST /v1/secrets/{path...}/destroy for backward compatibility.
func (s *Server) HandleDestroySecret(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	path := chi.URLParam(r, "*")
	// URL-decode the path in case it was encoded by the client
	if decodedPath, err := url.PathUnescape(path); err == nil {
		path = decodedPath
	}
	path = strings.TrimSuffix(path, "/destroy")
	if path == "" {
		RespondBadRequest(w, r, "secret path is required")
		return
	}

	req := &service.DeleteSecretRequest{
		SecretID:          path,
		PermanentlyDelete: true,
	}

	secretsService := s.getSecretsService(w, r)
	if secretsService == nil {
		return
	}

	resp, err := secretsService.DeleteSecret(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleListSecretVersions handles GET /v1/secrets/{path...}/versions - List versions.
func (s *Server) HandleListSecretVersions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	path := chi.URLParam(r, "*")
	// URL-decode the path in case it was encoded by the client
	if decodedPath, err := url.PathUnescape(path); err == nil {
		path = decodedPath
	}
	path = strings.TrimSuffix(path, "/versions")
	if path == "" {
		RespondBadRequest(w, r, "secret path is required")
		return
	}

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		l, err := strconv.Atoi(limitStr)
		if err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	cursor := r.URL.Query().Get("cursor")

	req := &service.ListSecretVersionsRequest{
		SecretID: path,
		Limit:    limit,
		Cursor:   cursor,
	}

	secretsService := s.getSecretsService(w, r)
	if secretsService == nil {
		return
	}

	resp, err := secretsService.ListSecretVersions(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleRollbackSecret handles POST /v1/secrets/{path...}/rollback - Rollback to version.
func (s *Server) HandleRollbackSecret(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	path := chi.URLParam(r, "*")
	if path == "" {
		RespondBadRequest(w, r, "secret path is required")
		return
	}

	// URL-decode the path in case it was encoded by the client
	if decodedPath, err := url.PathUnescape(path); err == nil {
		path = decodedPath
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody struct {
		Version uint64 `json:"version"`
	}

	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if reqBody.Version == 0 {
		RespondBadRequest(w, r, "target version is required")
		return
	}

	req := &service.RollbackSecretRequest{
		SecretID:      path,
		TargetVersion: reqBody.Version,
	}

	secretsService := s.getSecretsService(w, r)
	if secretsService == nil {
		return
	}

	resp, err := secretsService.RollbackSecret(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleRotateSecret handles POST /v1/secrets/{path...}/rotate - Rotate secret.
func (s *Server) HandleRotateSecret(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	path := chi.URLParam(r, "*")
	if path == "" {
		RespondBadRequest(w, r, "secret path is required")
		return
	}

	// URL-decode the path in case it was encoded by the client
	if decodedPath, err := url.PathUnescape(path); err == nil {
		path = decodedPath
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody struct {
		Data     json.RawMessage   `json:"data"`
		Metadata map[string]string `json:"metadata,omitempty"`
	}

	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if len(reqBody.Data) == 0 {
		RespondBadRequest(w, r, "new secret data is required")
		return
	}

	// Handle data as either raw bytes (base64) or JSON object
	var secretData []byte
	switch {
	case len(reqBody.Data) > 0 && reqBody.Data[0] == '"':
		// Data is a base64-encoded string
		if err := json.Unmarshal(reqBody.Data, &secretData); err != nil {
			RespondBadRequest(w, r, "invalid base64 data")
			return
		}
	case len(reqBody.Data) > 0 && reqBody.Data[0] == '{':
		// Data is a JSON object - store as-is
		secretData = reqBody.Data
	default:
		// Try to unmarshal as raw bytes
		if err := json.Unmarshal(reqBody.Data, &secretData); err != nil {
			// If that fails, use the raw JSON
			secretData = reqBody.Data
		}
	}

	req := &service.RotateSecretRequest{
		SecretID: path,
		NewData:  secretData,
		Metadata: reqBody.Metadata,
	}

	secretsService := s.getSecretsService(w, r)
	if secretsService == nil {
		return
	}

	resp, err := secretsService.RotateSecret(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleListVersions handles GET /v1/secrets/{path...}?versions=true - List secret versions.
func (s *Server) HandleListVersions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	path := chi.URLParam(r, "*")
	if path == "" {
		RespondBadRequest(w, r, "secret path is required")
		return
	}

	// URL-decode the path in case it was encoded by the client
	if decodedPath, err := url.PathUnescape(path); err == nil {
		path = decodedPath
	}

	req := &service.ListSecretVersionsRequest{
		SecretID: path,
	}

	secretsService := s.getSecretsService(w, r)
	if secretsService == nil {
		return
	}

	resp, err := secretsService.ListSecretVersions(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleEnableSecret handles POST /api/v1/secrets/{path...}/enable - Enable a secret.
func (s *Server) HandleEnableSecret(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	path := chi.URLParam(r, "*")
	// URL-decode the path in case it was encoded by the client
	if decodedPath, err := url.PathUnescape(path); err == nil {
		path = decodedPath
	}
	path = strings.TrimSuffix(path, "/enable")
	if path == "" {
		RespondBadRequest(w, r, "secret path is required")
		return
	}

	req := &service.UpdateSecretStatusRequest{
		SecretID: path,
		Status:   service.SecretStatusEnabled,
	}

	secretsService := s.getSecretsService(w, r)
	if secretsService == nil {
		return
	}

	resp, err := secretsService.UpdateSecretStatus(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleDisableSecret handles POST /api/v1/secrets/{path...}/disable - Disable a secret.
func (s *Server) HandleDisableSecret(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	path := chi.URLParam(r, "*")
	// URL-decode the path in case it was encoded by the client
	if decodedPath, err := url.PathUnescape(path); err == nil {
		path = decodedPath
	}
	path = strings.TrimSuffix(path, "/disable")
	if path == "" {
		RespondBadRequest(w, r, "secret path is required")
		return
	}

	req := &service.UpdateSecretStatusRequest{
		SecretID: path,
		Status:   service.SecretStatusDisabled,
	}

	secretsService := s.getSecretsService(w, r)
	if secretsService == nil {
		return
	}

	resp, err := secretsService.UpdateSecretStatus(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetSecretsByPrefix handles GET /v1/secrets/prefix/{prefix...} - Get all secrets by prefix.
func (s *Server) HandleGetSecretsByPrefix(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	prefix := chi.URLParam(r, "*")
	if prefix == "" {
		RespondBadRequest(w, r, "prefix is required")
		return
	}

	// URL-decode the prefix in case it was encoded by the client
	if decodedPrefix, err := url.PathUnescape(prefix); err == nil {
		prefix = decodedPrefix
	}

	includeData := r.URL.Query().Get("include_data") != "false"

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		l, err := strconv.Atoi(limitStr)
		if err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	cursor := r.URL.Query().Get("cursor")

	req := &service.GetSecretsByPrefixRequest{
		Prefix:      prefix,
		IncludeData: includeData,
		Limit:       limit,
		Cursor:      cursor,
	}

	secretsService := s.getSecretsService(w, r)
	if secretsService == nil {
		return
	}

	resp, err := secretsService.GetSecretsByPrefix(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleWatchSecrets handles GET /v1/secrets/watch/{prefix...} - Watch secrets via SSE.
func (s *Server) HandleWatchSecrets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	prefix := chi.URLParam(r, "*")
	if prefix == "" {
		RespondBadRequest(w, r, "prefix is required")
		return
	}

	// URL-decode the prefix in case it was encoded by the client
	if decodedPrefix, err := url.PathUnescape(prefix); err == nil {
		prefix = decodedPrefix
	}

	var startVersion int64
	if versionStr := r.URL.Query().Get("start_version"); versionStr != "" {
		v, err := strconv.ParseInt(versionStr, 10, 64)
		if err == nil {
			startVersion = v
		}
	}

	req := &service.WatchSecretsRequest{
		Prefix:       prefix,
		StartVersion: startVersion,
	}

	secretsService := s.getSecretsService(w, r)
	if secretsService == nil {
		return
	}

	ch, err := secretsService.WatchSecretsByPrefix(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrStreamingNotSupported)
		return
	}

	// Send initial connected event
	if _, err := w.Write([]byte("event: connected\ndata: {\"status\":\"connected\"}\n\n")); err != nil {
		return
	}
	flusher.Flush()

	// Stream changes
	for {
		select {
		case change, ok := <-ch:
			if !ok {
				// Channel closed
				if _, err := w.Write([]byte("event: close\ndata: {\"status\":\"closed\"}\n\n")); err != nil {
					return
				}
				flusher.Flush()
				return
			}

			data, err := json.Marshal(change)
			if err != nil {
				continue
			}

			event := "event: change\ndata: " + string(data) + "\n\n"
			if _, err := w.Write([]byte(event)); err != nil {
				return
			}
			flusher.Flush()

		case <-ctx.Done():
			// Client disconnected
			return
		}
	}
}
