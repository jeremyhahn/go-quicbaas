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

// Package rest provides Phase 4 REST API handlers for QuicBaaS.
// Implements backup, authentication, and cache management endpoints.
package rest

import (
	"encoding/json"
	"net/http"
	"strconv"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// Phase4Handler provides REST API handlers for Phase 4 features.
type Phase4Handler struct {
	backupMgr *backup.BackupManager
	cacheMgr  *cache.CacheManager
}

// NewPhase4Handler creates a new Phase 4 handler.
func NewPhase4Handler(
	backupMgr *backup.BackupManager,
	cacheMgr *cache.CacheManager,
) *Phase4Handler {
	return &Phase4Handler{
		backupMgr: backupMgr,
		cacheMgr:  cacheMgr,
	}
}

// =============================================================================
// Backup Operations
// =============================================================================

// HandleCreateBackup creates a new backup
// POST /api/v1/backups.
func (h *Phase4Handler) HandleCreateBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req pb.CreateBackupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Convert proto backup type to internal type
	var backupType backup.BackupType
	switch req.BackupType {
	case pb.BackupType_BACKUP_TYPE_FULL:
		backupType = backup.BackupTypeFull
	case pb.BackupType_BACKUP_TYPE_INCREMENTAL:
		backupType = backup.BackupTypeIncremental
	case pb.BackupType_BACKUP_TYPE_SNAPSHOT:
		// Snapshot type not supported yet, use full backup
		backupType = backup.BackupTypeFull
	default:
		backupType = backup.BackupTypeFull
	}

	result, err := h.backupMgr.CreateBackup(r.Context(), backupType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := &pb.CreateBackupResponse{
		BackupId:  result.ID,
		Timestamp: result.Timestamp.UnixNano(),
		Size:      result.Size,
		Duration:  result.Metadata.Duration.Nanoseconds(),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp) // Best effort encoding
}

// HandleListBackups lists all backups
// GET /api/v1/backups.
func (h *Phase4Handler) HandleListBackups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	backupList, err := h.backupMgr.ListBackups(r.Context(), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to proto format
	backups := make([]*pb.BackupInfo, 0, len(backupList.Backups))
	for _, b := range backupList.Backups {
		info, err := backupInfoToProto(b)
		if err != nil {
			http.Error(w, ErrMsgServerFailure, http.StatusInternalServerError)
			return
		}
		backups = append(backups, info)
	}

	resp := &pb.ListBackupsResponse{
		Backups:   backups,
		NextToken: backupList.NextToken,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp) // Best effort encoding
}

// backupInfoToProto converts a stored backup to its wire form. A shard
// count beyond the int32 wire field is refused rather than misreported,
// since a restore relies on it.
func backupInfoToProto(b *backup.Backup) (*pb.BackupInfo, error) {
	numShards, err := conv.SafeIntToInt32(b.Metadata.NumShards)
	if err != nil {
		return nil, api.NewEncodeError("rest.backups.list", "shard count", err)
	}
	return &pb.BackupInfo{
		BackupId:  b.ID,
		Timestamp: b.Timestamp.UnixNano(),
		Size:      b.Size,
		NumShards: numShards,
		Tags:      b.Metadata.Tags,
	}, nil
}

// HandleRestoreBackup restores from a backup
// POST /api/v1/backups/{id}/restore.
func (h *Phase4Handler) HandleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract backup ID from URL path
	backupID := extractPathParam(r.URL.Path, "/api/v1/backups/", "/restore")

	var req pb.RestoreBackupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req.BackupId = backupID

	opts := &backup.RestoreOptions{
		VerifyChecksum: req.VerifyChecksum,
		SkipExisting:   req.SkipExisting,
		TargetShards:   req.TargetShards,
	}

	err := h.backupMgr.RestoreBackup(r.Context(), backupID, opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := &pb.RestoreBackupResponse{
		BackupId:  backupID,
		Timestamp: 0,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp) // Best effort encoding
}

// HandleDeleteBackup deletes a backup
// DELETE /api/v1/backups/{id}.
func (h *Phase4Handler) HandleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	backupID := extractPathParam(r.URL.Path, "/api/v1/backups/", "")

	err := h.backupMgr.DeleteBackup(r.Context(), backupID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := &pb.DeleteBackupResponse{
		Success: true,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp) // Best effort encoding
}

// =============================================================================
// Authentication and user operations
//
// There are none here. Session lifecycle (logout / refresh / validate) is
// served by the IAM handlers, which route to iam.OIDCProvider — the authority
// that minted the bearer — and user administration is served by IAMHandler
// under /api/v1/iam, the single user surface. The four handlers that used to
// sit here (HandleLogout, HandleRefreshToken, HandleCreateUser,
// HandleListUsers) drove an auth.AuthManager that carried its own user store,
// were never registered on any router, and were deleted with the parallel
// identity model in issue #905.
// =============================================================================

// =============================================================================
// Cache Management
// =============================================================================

// HandleGetCacheStats returns cache statistics
// GET /api/v1/cache/stats.
func (h *Phase4Handler) HandleGetCacheStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.cacheMgr == nil {
		http.Error(w, "Cache manager not initialized", http.StatusServiceUnavailable)
		return
	}

	stats := h.cacheMgr.Stats()

	// The int32 wire fields are observability gauges: a cache larger than
	// they hold reports as saturated rather than failing the stats read.
	resp := &pb.GetCacheStatsResponse{
		Enabled:     stats.Enabled,
		L1Size:      conv.ClampIntToInt32(stats.L1Size),
		L1Capacity:  conv.ClampIntToInt32(stats.L1Capacity),
		L1Hits:      stats.L1Hits,
		L1Misses:    stats.L1Misses,
		L1HitRate:   stats.L1HitRate,
		L1Evictions: 0,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp) // Best effort encoding
}

// HandleClearCache clears the cache
// POST /api/v1/cache/clear.
func (h *Phase4Handler) HandleClearCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.cacheMgr == nil {
		http.Error(w, "Cache manager not initialized", http.StatusServiceUnavailable)
		return
	}

	statsBefore := h.cacheMgr.Stats()
	h.cacheMgr.Clear()

	// EntriesCleared is an int32 gauge: the clear has already happened, so a
	// count larger than the field holds reports as saturated, not as a failure.
	resp := &pb.ClearCacheResponse{
		Success:        true,
		EntriesCleared: conv.ClampIntToInt32(statsBefore.L1Size),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp) // Best effort encoding
}

// =============================================================================
// Helper Functions
// =============================================================================

// extractPathParam extracts a parameter from URL path.
func extractPathParam(path, prefix, suffix string) string {
	// Remove prefix
	if len(prefix) > 0 {
		path = path[len(prefix):]
	}
	// Remove suffix
	if len(suffix) > 0 && len(path) > len(suffix) {
		path = path[:len(path)-len(suffix)]
	}
	return path
}
