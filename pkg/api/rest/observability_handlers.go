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
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
)

// Phase4Handler provides REST API handlers for Phase 4 features.
type Phase4Handler struct {
	backupMgr *backup.BackupManager
	authMgr   *auth.AuthManager
	cacheMgr  *cache.CacheManager
}

// NewPhase4Handler creates a new Phase 4 handler.
func NewPhase4Handler(
	backupMgr *backup.BackupManager,
	authMgr *auth.AuthManager,
	cacheMgr *cache.CacheManager,
) *Phase4Handler {
	return &Phase4Handler{
		backupMgr: backupMgr,
		authMgr:   authMgr,
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
		// NumShards is typically a small number (< 1000) representing shard count
		numShards := b.Metadata.NumShards
		if numShards > 2147483647 { // Max int32
			numShards = 2147483647
		}
		info := &pb.BackupInfo{
			BackupId:  b.ID,
			Timestamp: b.Timestamp.UnixNano(),
			Size:      b.Size,
			NumShards: int32(numShards), //nolint:gosec // G115: bounded by max int32 check above
			Tags:      b.Metadata.Tags,
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
// Authentication Operations
// (Password-based login is intentionally not exposed. Use the unified login
// ceremony under /api/v1/auth/login/{begin,finish} (mtls / fido2 / piv) or
// /api/v1/auth/login/piv to obtain access tokens.)
// =============================================================================

// HandleLogout revokes user tokens
// POST /api/v1/auth/logout.
func (h *Phase4Handler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req pb.LogoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	err := h.authMgr.RevokeToken(r.Context(), req.AccessToken)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := &pb.LogoutResponse{
		Success: true,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp) // Best effort encoding
}

// HandleRefreshToken generates a new access token
// POST /api/v1/auth/refresh.
func (h *Phase4Handler) HandleRefreshToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req pb.RefreshTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	token, err := h.authMgr.RefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	resp := &pb.RefreshTokenResponse{
		AccessToken: token.AccessToken,
		ExpiresAt:   token.ExpiresAt.UnixNano(),
		TokenType:   token.TokenType,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp) // Best effort encoding
}

// =============================================================================
// User Management
// =============================================================================

// HandleCreateUser creates a new user
// POST /api/v1/users.
func (h *Phase4Handler) HandleCreateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req pb.CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	user, err := h.authMgr.CreateUser(req.Username, req.Roles)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := &pb.CreateUserResponse{
		UserId:   user.ID,
		Username: user.Username,
		Created:  user.Created.UnixNano(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp) // Best effort encoding
}

// HandleListUsers lists all users
// GET /api/v1/users.
func (h *Phase4Handler) HandleListUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	users := h.authMgr.GetUserStore().ListUsers()

	userInfos := make([]*pb.UserInfo, 0, len(users))
	for _, user := range users {
		userInfos = append(userInfos, &pb.UserInfo{
			UserId:   user.GetID(),
			Username: user.GetUsername(),
			Roles:    user.GetRoles(),
			Created:  user.GetCreated().UnixNano(),
			LastAuth: user.GetLastAuth().UnixNano(),
		})
	}

	resp := &pb.ListUsersResponse{
		Users: userInfos,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp) // Best effort encoding
}

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

	// L1Size and L1Capacity are cache sizes, typically small numbers
	l1Size := stats.L1Size
	if l1Size > 2147483647 { // Max int32
		l1Size = 2147483647
	}
	l1Capacity := stats.L1Capacity
	if l1Capacity > 2147483647 { // Max int32
		l1Capacity = 2147483647
	}

	resp := &pb.GetCacheStatsResponse{
		Enabled:     stats.Enabled,
		L1Size:      int32(l1Size),     //nolint:gosec // G115: bounded by max int32 check above
		L1Capacity:  int32(l1Capacity), //nolint:gosec // G115: bounded by max int32 check above
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

	// L1Size is cache size before clearing, typically small
	l1SizeBefore := statsBefore.L1Size
	if l1SizeBefore > 2147483647 { // Max int32
		l1SizeBefore = 2147483647
	}

	resp := &pb.ClearCacheResponse{
		Success:        true,
		EntriesCleared: int32(l1SizeBefore), //nolint:gosec // G115: bounded by max int32 check above
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
