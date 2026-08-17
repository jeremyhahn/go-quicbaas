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
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Object Storage Admin REST Types
// =============================================================================

// CreateObjectBackendRESTRequest is the JSON request for creating a storage backend.
type CreateObjectBackendRESTRequest struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	Tier    string            `json:"tier"`
	Config  map[string]string `json:"config,omitempty"`
	Enabled bool              `json:"enabled"`
}

// UpdateObjectBackendRESTRequest is the JSON request for updating a storage backend.
type UpdateObjectBackendRESTRequest struct {
	Config map[string]string `json:"config,omitempty"`
	Tier   string            `json:"tier,omitempty"`
}

// DeleteObjectBackendRESTRequest is the JSON request for deleting a storage backend.
type DeleteObjectBackendRESTRequest struct {
	MigrateTo string `json:"migrate_to,omitempty"`
	Force     bool   `json:"force,omitempty"`
}

// SetObjectBackendStatusRESTRequest is the JSON request for setting backend status.
type SetObjectBackendStatusRESTRequest struct {
	Enabled bool `json:"enabled"`
}

// RunGarbageCollectionRESTRequest is the JSON request for running garbage collection.
type RunGarbageCollectionRESTRequest struct {
	Backends   []string `json:"backends,omitempty"`
	DryRun     bool     `json:"dry_run,omitempty"`
	Aggressive bool     `json:"aggressive,omitempty"`
}

// RepairObjectRESTRequest is the JSON request for repairing an object.
type RepairObjectRESTRequest struct {
	Key            string `json:"key"`
	SourceLocation string `json:"source_location,omitempty"`
	TimeoutMs      int64  `json:"timeout_ms,omitempty"`
}

// CompactObjectBackendRESTRequest is the JSON request for triggering compaction.
type CompactObjectBackendRESTRequest struct {
	Aggressive bool `json:"aggressive,omitempty"`
}

// =============================================================================
// Backend Management Handlers
// =============================================================================

// HandleCreateObjectBackend creates a new storage backend.
// POST /api/v1/admin/object-backends.
func (s *Server) HandleCreateObjectBackend(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	adminService := s.service.ObjectStorageAdminService()
	if adminService == nil {
		RespondServiceUnavailable(w, r, "object storage admin service not available")
		return
	}

	var reqBody CreateObjectBackendRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		RespondBadRequest(w, r, "invalid request body")
		return
	}
	defer r.Body.Close()

	if reqBody.Name == "" {
		RespondBadRequest(w, r, "backend name is required")
		return
	}
	if reqBody.Type == "" {
		RespondBadRequest(w, r, "backend type is required")
		return
	}
	if reqBody.Tier == "" {
		RespondBadRequest(w, r, "storage tier is required")
		return
	}

	req := &service.CreateBackendRequest{
		Name:    reqBody.Name,
		Type:    service.BackendType(reqBody.Type),
		Tier:    service.StorageTier(reqBody.Tier),
		Config:  reqBody.Config,
		Enabled: reqBody.Enabled,
	}

	resp, err := adminService.CreateBackend(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusCreated, resp)
}

// HandleUpdateObjectBackend updates backend configuration.
// PUT /api/v1/admin/object-backends/{name}.
func (s *Server) HandleUpdateObjectBackend(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondBadRequest(w, r, "backend name is required")
		return
	}

	adminService := s.service.ObjectStorageAdminService()
	if adminService == nil {
		RespondServiceUnavailable(w, r, "object storage admin service not available")
		return
	}

	var reqBody UpdateObjectBackendRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		RespondBadRequest(w, r, "invalid request body")
		return
	}
	defer r.Body.Close()

	req := &service.UpdateBackendRequest{
		Name:   name,
		Config: reqBody.Config,
		Tier:   service.StorageTier(reqBody.Tier),
	}

	resp, err := adminService.UpdateBackend(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleDeleteObjectBackend removes a storage backend.
// DELETE /api/v1/admin/object-backends/{name}.
func (s *Server) HandleDeleteObjectBackend(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondBadRequest(w, r, "backend name is required")
		return
	}

	adminService := s.service.ObjectStorageAdminService()
	if adminService == nil {
		RespondServiceUnavailable(w, r, "object storage admin service not available")
		return
	}

	var reqBody DeleteObjectBackendRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		// Allow empty body
		reqBody = DeleteObjectBackendRESTRequest{}
	}
	defer r.Body.Close()

	// Also check query parameters for migrate_to and force
	if reqBody.MigrateTo == "" {
		reqBody.MigrateTo = r.URL.Query().Get("migrate_to")
	}
	if !reqBody.Force {
		reqBody.Force = r.URL.Query().Get("force") == "true"
	}

	req := &service.DeleteBackendRequest{
		Name:      name,
		MigrateTo: reqBody.MigrateTo,
		Force:     reqBody.Force,
	}

	resp, err := adminService.DeleteBackend(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleSetObjectBackendStatus enables/disables a backend.
// PUT /api/v1/admin/object-backends/{name}/status.
func (s *Server) HandleSetObjectBackendStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondBadRequest(w, r, "backend name is required")
		return
	}

	adminService := s.service.ObjectStorageAdminService()
	if adminService == nil {
		RespondServiceUnavailable(w, r, "object storage admin service not available")
		return
	}

	var reqBody SetObjectBackendStatusRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		RespondBadRequest(w, r, "invalid request body")
		return
	}
	defer r.Body.Close()

	req := &service.SetBackendStatusRequest{
		Name:    name,
		Enabled: reqBody.Enabled,
	}

	resp, err := adminService.SetBackendStatus(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Statistics Handlers
// =============================================================================

// HandleGetStorageStats returns cluster storage statistics.
// GET /api/v1/admin/storage/stats.
func (s *Server) HandleGetStorageStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	adminService := s.service.ObjectStorageAdminService()
	if adminService == nil {
		RespondServiceUnavailable(w, r, "object storage admin service not available")
		return
	}

	req := &service.GetStorageStatsRequest{
		IncludeBackends: r.URL.Query().Get("backends") == "true",
		IncludeTenants:  r.URL.Query().Get("tenants") == "true",
	}

	resp, err := adminService.GetStorageStats(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Garbage Collection Handlers
// =============================================================================

// HandleRunGarbageCollection initiates GC.
// POST /api/v1/admin/storage/gc.
func (s *Server) HandleRunGarbageCollection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	adminService := s.service.ObjectStorageAdminService()
	if adminService == nil {
		RespondServiceUnavailable(w, r, "object storage admin service not available")
		return
	}

	var reqBody RunGarbageCollectionRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		// Allow empty body for defaults
		reqBody = RunGarbageCollectionRESTRequest{}
	}
	defer r.Body.Close()

	req := &service.RunGarbageCollectionRequest{
		Backends:   reqBody.Backends,
		DryRun:     reqBody.DryRun,
		Aggressive: reqBody.Aggressive,
	}

	resp, err := adminService.RunGarbageCollection(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusAccepted, resp)
}

// HandleGetGarbageCollectionProgress returns GC progress.
// GET /api/v1/admin/storage/gc/progress.
func (s *Server) HandleGetGarbageCollectionProgress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	operationID := r.URL.Query().Get("operation_id")
	if operationID == "" {
		RespondBadRequest(w, r, "operation_id query parameter is required")
		return
	}

	adminService := s.service.ObjectStorageAdminService()
	if adminService == nil {
		RespondServiceUnavailable(w, r, "object storage admin service not available")
		return
	}

	req := &service.GetGarbageCollectionProgressRequest{
		OperationID: operationID,
	}

	resp, err := adminService.GetGarbageCollectionProgress(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Repair Handlers
// =============================================================================

// HandleRepairObject repairs an object.
// POST /api/v1/admin/objects/repair.
func (s *Server) HandleRepairObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	adminService := s.service.ObjectStorageAdminService()
	if adminService == nil {
		RespondServiceUnavailable(w, r, "object storage admin service not available")
		return
	}

	var reqBody RepairObjectRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		RespondBadRequest(w, r, "invalid request body")
		return
	}
	defer r.Body.Close()

	if reqBody.Key == "" {
		RespondBadRequest(w, r, "key is required")
		return
	}

	req := &service.RepairObjectRequest{
		Key:            reqBody.Key,
		SourceLocation: reqBody.SourceLocation,
		TimeoutMs:      reqBody.TimeoutMs,
	}

	resp, err := adminService.RepairObject(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Compaction Handlers
// =============================================================================

// HandleCompactObjectBackend triggers compaction.
// POST /api/v1/admin/object-backends/{name}/compact.
func (s *Server) HandleCompactObjectBackend(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondBadRequest(w, r, "backend name is required")
		return
	}

	adminService := s.service.ObjectStorageAdminService()
	if adminService == nil {
		RespondServiceUnavailable(w, r, "object storage admin service not available")
		return
	}

	var reqBody CompactObjectBackendRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		// Allow empty body for defaults
		reqBody = CompactObjectBackendRESTRequest{}
	}
	defer r.Body.Close()

	req := &service.CompactBackendRequest{
		Name:       name,
		Aggressive: reqBody.Aggressive,
	}

	resp, err := adminService.CompactBackend(ctx, req)
	if err != nil {
		s.handleObjectStorageError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusAccepted, resp)
}

// =============================================================================
// Route Registration
// =============================================================================
