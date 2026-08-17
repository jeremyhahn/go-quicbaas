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
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// CreateBackupRequest represents the REST API request for creating a backup.
type CreateBackupRequest struct {
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
	Compression   string   `json:"compression,omitempty"`    // e.g., "gzip", "lz4", "none"
	Encryption    string   `json:"encryption,omitempty"`     // e.g., "aes256", "none"
	IncludeShards []uint64 `json:"include_shards,omitempty"` // specific shards to backup
}

// CreateBackupResponse represents the REST API response for creating a backup.
type CreateBackupResponse struct {
	BackupID  string `json:"backup_id"`
	Timestamp int64  `json:"timestamp"`
	Size      uint64 `json:"size"`
	Status    string `json:"status"`
}

// ListBackupsResponse represents the REST API response for listing backups.
type ListBackupsResponse struct {
	Backups    []*BackupInfo `json:"backups"`
	TotalCount uint64        `json:"total_count"`
}

// BackupInfo represents backup metadata in REST API responses.
type BackupInfo struct {
	BackupID    string   `json:"backup_id"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Timestamp   int64    `json:"timestamp"`
	Size        uint64   `json:"size"`
	Status      string   `json:"status"`
	Shards      []uint64 `json:"shards,omitempty"`
}

// RestoreBackupRequest represents the REST API request for restoring a backup.
type RestoreBackupRequest struct {
	TargetShards []uint64 `json:"target_shards,omitempty"`
}

// RestoreBackupResponse represents the REST API response for restoring a backup.
type RestoreBackupResponse struct {
	JobID    string  `json:"job_id"`
	Status   string  `json:"status"`
	Progress float64 `json:"progress"`
}

// DeleteBackupResponse represents the REST API response for deleting a backup.
type DeleteBackupResponse struct {
	Success bool `json:"success"`
}

// VerifyBackupResponse represents the REST API response for verifying a backup.
type VerifyBackupResponse struct {
	Valid         bool     `json:"valid"`
	ChecksumMatch bool     `json:"checksum_match"`
	Issues        []string `json:"issues,omitempty"`
}

// BackupStatusResponse represents the REST API response for backup status.
type BackupStatusResponse struct {
	BackupID      string   `json:"backup_id"`
	Valid         bool     `json:"valid"`
	ChecksumMatch bool     `json:"checksum_match"`
	Issues        []string `json:"issues,omitempty"`
}

// HandleCreateBackup creates a new backup.
// POST /api/v1/backups.
func (s *Server) HandleCreateBackup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request body
	var req CreateBackupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Allow empty body - use defaults
		req = CreateBackupRequest{}
	}

	// Call service layer
	serviceReq := &service.CreateBackupRequest{
		Name:          req.Name,
		Description:   req.Description,
		Compression:   req.Compression,
		Encryption:    req.Encryption,
		IncludeShards: req.IncludeShards,
	}

	resp, err := s.service.CreateBackup(ctx, serviceReq)
	if err != nil {
		s.logger.Errorf("Failed to create backup: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to REST response
	restResp := &CreateBackupResponse{
		BackupID:  resp.BackupID,
		Timestamp: resp.Timestamp,
		Size:      resp.Size,
		Status:    resp.Status,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(restResp)
}

// HandleListBackups lists all available backups.
// GET /api/v1/backups.
func (s *Server) HandleListBackups(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters
	limitStr := r.URL.Query().Get("limit")
	var limit uint64 = 100
	if limitStr != "" {
		if l, err := strconv.ParseUint(limitStr, 10, 64); err == nil && l > 0 {
			limit = l
		}
	}

	offsetStr := r.URL.Query().Get("offset")
	var offset uint64 = 0
	if offsetStr != "" {
		if o, err := strconv.ParseUint(offsetStr, 10, 64); err == nil {
			offset = o
		}
	}

	// Call service layer
	serviceReq := &service.ListBackupsRequest{
		Limit:  limit,
		Offset: offset,
	}

	resp, err := s.service.ListBackups(ctx, serviceReq)
	if err != nil {
		s.logger.Errorf("Failed to list backups: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to REST response
	backups := make([]*BackupInfo, 0, len(resp.Backups))
	for _, b := range resp.Backups {
		info := &BackupInfo{
			BackupID:    b.BackupID,
			Name:        b.Name,
			Description: b.Description,
			Timestamp:   b.Timestamp,
			Size:        b.Size,
			Status:      b.Status,
			Shards:      b.Shards,
		}
		backups = append(backups, info)
	}

	restResp := &ListBackupsResponse{
		Backups:    backups,
		TotalCount: resp.TotalCount,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(restResp)
}

// HandleRestoreBackup restores from a backup.
// POST /api/v1/backups/{id}/restore.
func (s *Server) HandleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract backup ID from URL path
	backupID := chi.URLParam(r, "id")
	if backupID == "" {
		http.Error(w, "backup ID is required", http.StatusBadRequest)
		return
	}

	// Parse request body
	var req RestoreBackupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Allow empty body - use defaults
		req = RestoreBackupRequest{}
	}

	// Call service layer
	serviceReq := &service.RestoreBackupRequest{
		BackupID:     backupID,
		TargetShards: req.TargetShards,
	}

	resp, err := s.service.RestoreBackup(ctx, serviceReq)
	if err != nil {
		s.logger.Errorf("Failed to restore backup: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to REST response
	restResp := &RestoreBackupResponse{
		JobID:    resp.JobID,
		Status:   resp.Status,
		Progress: resp.Progress,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(restResp)
}

// HandleDeleteBackup deletes a backup.
// DELETE /api/v1/backups/{id}.
func (s *Server) HandleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract backup ID from URL path
	backupID := chi.URLParam(r, "id")
	if backupID == "" {
		http.Error(w, "backup ID is required", http.StatusBadRequest)
		return
	}

	// Call service layer
	serviceReq := &service.DeleteBackupRequest{
		BackupID: backupID,
	}

	resp, err := s.service.DeleteBackup(ctx, serviceReq)
	if err != nil {
		s.logger.Errorf("Failed to delete backup: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to REST response
	restResp := &DeleteBackupResponse{
		Success: resp.Success,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(restResp)
}

// HandleVerifyBackup verifies the integrity of a backup.
// POST /api/v1/backups/{id}/verify.
func (s *Server) HandleVerifyBackup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract backup ID from URL path
	backupID := chi.URLParam(r, "id")
	if backupID == "" {
		http.Error(w, "backup ID is required", http.StatusBadRequest)
		return
	}

	// Call service layer
	serviceReq := &service.VerifyBackupRequest{
		BackupID: backupID,
	}

	resp, err := s.service.VerifyBackup(ctx, serviceReq)
	if err != nil {
		s.logger.Errorf("Failed to verify backup: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to REST response
	restResp := &VerifyBackupResponse{
		Valid:         resp.Valid,
		ChecksumMatch: resp.ChecksumMatch,
		Issues:        resp.Issues,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(restResp)
}

// HandleBackupStatus retrieves the status of a backup.
// GET /api/v1/backups/{id}/status.
func (s *Server) HandleBackupStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract backup ID from URL path
	backupID := chi.URLParam(r, "id")
	if backupID == "" {
		http.Error(w, "backup ID is required", http.StatusBadRequest)
		return
	}

	// Call service layer to verify backup and return its current state
	serviceReq := &service.VerifyBackupRequest{
		BackupID: backupID,
	}

	resp, err := s.service.VerifyBackup(ctx, serviceReq)
	if err != nil {
		s.logger.Errorf("Failed to get backup status: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to REST response
	restResp := &BackupStatusResponse{
		BackupID:      backupID,
		Valid:         resp.Valid,
		ChecksumMatch: resp.ChecksumMatch,
		Issues:        resp.Issues,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(restResp)
}
