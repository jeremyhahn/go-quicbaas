//go:build rest

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// handlers_kms_admin.go provides the REST handlers for the KMS key
// administration verbs that complete transport parity with gRPC/QUIC/MCP:
// key revocation, public key retrieval, metadata get/update, and the
// supported-algorithms query. Every handler delegates to the service layer
// (service.KMSService) only.

package rest

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// REST wire types
// =============================================================================

// RevokeKeyRESTRequest is the JSON body for key revocation.
type RevokeKeyRESTRequest struct {
	// Reason optionally records why the key was revoked.
	Reason string `json:"reason,omitempty"`
}

// RevokeKeyRESTResponse is the JSON response for key revocation.
type RevokeKeyRESTResponse struct {
	KeyID     string `json:"key_id"`
	Status    string `json:"status"`
	Timestamp int64  `json:"timestamp"`
}

// GetKeyMetadataRESTResponse is the JSON response for reading key metadata.
type GetKeyMetadataRESTResponse struct {
	Metadata  *KeyMetadataResponse `json:"metadata"`
	Timestamp int64                `json:"timestamp"`
}

// UpdateKeyMetadataRESTRequest is the JSON body for updating key metadata.
// Pointer fields distinguish "not provided" from "set to empty".
type UpdateKeyMetadataRESTRequest struct {
	Name        *string           `json:"name,omitempty"`
	Description *string           `json:"description,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	RemoveTags  []string          `json:"remove_tags,omitempty"`
}

// UpdateKeyMetadataRESTResponse is the JSON response for updating key metadata.
type UpdateKeyMetadataRESTResponse struct {
	Metadata  *KeyMetadataResponse `json:"metadata"`
	Timestamp int64                `json:"timestamp"`
}

// GetPublicKeyRESTResponse is the JSON response for public key retrieval.
// PublicKey is the DER-encoded (PKIX) public key, base64-encoded on the wire.
type GetPublicKeyRESTResponse struct {
	KeyID     string `json:"key_id"`
	PublicKey []byte `json:"public_key"`
	Algorithm string `json:"algorithm"`
	Version   uint64 `json:"version"`
	Timestamp int64  `json:"timestamp"`
}

// SupportedAlgorithmsRESTResponse is the JSON response for the
// supported-algorithms query.
type SupportedAlgorithmsRESTResponse struct {
	Algorithms []string `json:"algorithms"`
	Timestamp  int64    `json:"timestamp"`
}

// =============================================================================
// Handlers
// =============================================================================

// HandleRevokeKey marks a key as revoked and unusable.
// POST /api/v1/kms/keys/{name}/revoke.
func (s *Server) HandleRevokeKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	// The body is optional; an empty body revokes without a recorded reason.
	var reqBody RevokeKeyRESTRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()
	if len(body) > 0 {
		if err := json.Unmarshal(body, &reqBody); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}

	svcReq := &service.RevokeKeyRequest{
		KeyID:  keyID,
		Reason: reqBody.Reason,
	}

	svcResp, err := kmsService.RevokeKey(ctx, svcReq)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	resp := &RevokeKeyRESTResponse{
		KeyID:     svcResp.KeyID,
		Status:    string(svcResp.Status),
		Timestamp: svcResp.Timestamp.Unix(),
	}
	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetKeyMetadata retrieves metadata for a key without exposing key material.
// GET /api/v1/kms/keys/{name}/metadata.
func (s *Server) HandleGetKeyMetadata(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	svcReq := &service.GetKeyMetadataRequest{KeyID: keyID}

	svcResp, err := kmsService.GetKeyMetadata(ctx, svcReq)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	resp := &GetKeyMetadataRESTResponse{
		Metadata:  svcMetadataToRESTResponse(svcResp.Metadata),
		Timestamp: svcResp.Timestamp.Unix(),
	}
	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleUpdateKeyMetadata updates mutable metadata fields for a key.
// PUT /api/v1/kms/keys/{name}/metadata.
func (s *Server) HandleUpdateKeyMetadata(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody UpdateKeyMetadataRESTRequest
	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	svcReq := &service.UpdateKeyMetadataRequest{
		KeyID:       keyID,
		Name:        reqBody.Name,
		Description: reqBody.Description,
		Tags:        reqBody.Tags,
		RemoveTags:  reqBody.RemoveTags,
	}

	svcResp, err := kmsService.UpdateKeyMetadata(ctx, svcReq)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	resp := &UpdateKeyMetadataRESTResponse{
		Metadata:  svcMetadataToRESTResponse(svcResp.Metadata),
		Timestamp: svcResp.Timestamp.Unix(),
	}
	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetPublicKey retrieves the DER-encoded public key component of an
// asymmetric key.
// GET /api/v1/kms/keys/{name}/public?version=N.
func (s *Server) HandleGetPublicKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	var version uint64
	if versionStr := r.URL.Query().Get("version"); versionStr != "" {
		v, err := strconv.ParseUint(versionStr, 10, 64)
		if err != nil {
			RespondBadRequest(w, r, "invalid version parameter")
			return
		}
		version = v
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	svcReq := &service.GetPublicKeyRequest{
		KeyID:   keyID,
		Version: version,
	}

	svcResp, err := kmsService.GetPublicKey(ctx, svcReq)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	resp := &GetPublicKeyRESTResponse{
		KeyID:     svcResp.KeyID,
		PublicKey: svcResp.PublicKey,
		Algorithm: string(svcResp.Algorithm),
		Version:   svcResp.Version,
		Timestamp: svcResp.Timestamp.Unix(),
	}
	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetSupportedAlgorithms returns the algorithms supported by the
// current KMS backend.
// GET /api/v1/kms/algorithms.
func (s *Server) HandleGetSupportedAlgorithms(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	algorithms, err := kmsService.GetSupportedAlgorithms(ctx)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}
	if algorithms == nil {
		algorithms = []string{}
	}

	resp := &SupportedAlgorithmsRESTResponse{
		Algorithms: algorithms,
		Timestamp:  time.Now().Unix(),
	}
	RespondJSON(w, r, http.StatusOK, resp)
}
