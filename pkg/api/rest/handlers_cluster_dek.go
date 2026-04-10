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
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Cluster DEK Service Route Registration
// =============================================================================

// registerClusterDEKRoutes registers all cluster DEK-related routes.
func registerClusterDEKRoutes(s *Server, r chi.Router) {
	r.Route("/cluster/dek", func(r chi.Router) {
		// DEK lifecycle
		r.Post("/init", s.HandleInitializeClusterDEK)
		r.Get("/status", s.HandleGetClusterDEKStatus)
		r.Post("/rotate", s.HandleRotateClusterDEK)

		// Node DEK management
		r.Post("/sync/{nodeId}", s.HandleSyncNodeDEK)
		r.Get("/nodes/{nodeId}", s.HandleGetNodeDEKStatus)

		// Re-encryption management
		r.Post("/reencryption/start", s.HandleStartClusterReencryption)
		r.Get("/reencryption/status", s.HandleGetClusterReencryptionStatus)
		r.Delete("/reencryption", s.HandleCancelClusterReencryption)

		// Recovery
		r.Post("/recover", s.HandleRecoverDEK)
	})
}

// =============================================================================
// Request/Response Types for REST API
// =============================================================================

// InitClusterDEKRESTRequest represents the JSON request body for DEK initialization.
type InitClusterDEKRESTRequest struct {
	Algorithm               string            `json:"algorithm,omitempty"`
	KeySize                 int               `json:"key_size,omitempty"`
	Metadata                map[string]string `json:"metadata,omitempty"`
	RotationPolicyEnabled   bool              `json:"rotation_policy_enabled,omitempty"`
	RotationIntervalSeconds int64             `json:"rotation_interval_seconds,omitempty"`
	AutoReencrypt           bool              `json:"auto_reencrypt,omitempty"`
}

// InitClusterDEKRESTResponse represents the JSON response for DEK initialization.
type InitClusterDEKRESTResponse struct {
	Version     uint64 `json:"version"`
	CreatedAt   int64  `json:"created_at"`
	NodesSynced int    `json:"nodes_synced"`
	Algorithm   string `json:"algorithm"`
	KeySize     int    `json:"key_size"`
	Timestamp   int64  `json:"timestamp"`
}

// ClusterDEKStatusRESTResponse represents the JSON response for DEK status.
type ClusterDEKStatusRESTResponse struct {
	Initialized            bool                 `json:"initialized"`
	CurrentVersion         uint64               `json:"current_version"`
	CreatedAt              int64                `json:"created_at,omitempty"`
	RotatedAt              int64                `json:"rotated_at,omitempty"`
	NextRotation           int64                `json:"next_rotation,omitempty"`
	NodesSynced            int                  `json:"nodes_synced"`
	TotalNodes             int                  `json:"total_nodes"`
	NodesOutOfSync         []NodeDEKInfoREST    `json:"nodes_out_of_sync,omitempty"`
	Algorithm              string               `json:"algorithm,omitempty"`
	KeySize                int                  `json:"key_size,omitempty"`
	PreviousVersions       []DEKVersionInfoREST `json:"previous_versions,omitempty"`
	ReencryptionInProgress bool                 `json:"reencryption_in_progress"`
	ReencryptionProgress   float64              `json:"reencryption_progress,omitempty"`
	Timestamp              int64                `json:"timestamp"`
}

// NodeDEKInfoREST represents node DEK info in REST responses.
type NodeDEKInfoREST struct {
	NodeID         uint64 `json:"node_id"`
	NodeName       string `json:"node_name,omitempty"`
	CurrentVersion uint64 `json:"current_version"`
	LastSyncedAt   int64  `json:"last_synced_at,omitempty"`
}

// DEKVersionInfoREST represents DEK version info in REST responses.
type DEKVersionInfoREST struct {
	Version           uint64 `json:"version"`
	CreatedAt         int64  `json:"created_at"`
	DeprecatedAt      int64  `json:"deprecated_at,omitempty"`
	EntitiesEncrypted int64  `json:"entities_encrypted"`
	CanDelete         bool   `json:"can_delete"`
}

// RotateClusterDEKRESTRequest represents the JSON request body for DEK rotation.
type RotateClusterDEKRESTRequest struct {
	Reason                string            `json:"reason,omitempty"`
	ScheduledTime         int64             `json:"scheduled_time,omitempty"`
	ForceRotation         bool              `json:"force_rotation,omitempty"`
	AutoStartReencryption bool              `json:"auto_start_reencryption,omitempty"`
	Metadata              map[string]string `json:"metadata,omitempty"`
}

// RotateClusterDEKRESTResponse represents the JSON response for DEK rotation.
type RotateClusterDEKRESTResponse struct {
	NewVersion                uint64 `json:"new_version"`
	OldVersion                uint64 `json:"old_version"`
	RotatedAt                 int64  `json:"rotated_at"`
	RotationID                string `json:"rotation_id"`
	NodesSynced               int    `json:"nodes_synced"`
	ReencryptionStarted       bool   `json:"reencryption_started"`
	EstimatedReencryptionTime int64  `json:"estimated_reencryption_time_seconds,omitempty"`
	Timestamp                 int64  `json:"timestamp"`
}

// SyncNodeDEKRESTRequest represents the JSON request body for node DEK sync.
type SyncNodeDEKRESTRequest struct {
	CurrentVersion uint64 `json:"current_version,omitempty"`
	ForceResync    bool   `json:"force_resync,omitempty"`
}

// SyncNodeDEKRESTResponse represents the JSON response for node DEK sync.
type SyncNodeDEKRESTResponse struct {
	NodeID         uint64   `json:"node_id"`
	VersionsSynced []uint64 `json:"versions_synced"`
	CurrentVersion uint64   `json:"current_version"`
	SyncedAt       int64    `json:"synced_at"`
	Timestamp      int64    `json:"timestamp"`
}

// NodeDEKStatusRESTResponse represents the JSON response for node DEK status.
type NodeDEKStatusRESTResponse struct {
	NodeID            uint64   `json:"node_id"`
	NodeName          string   `json:"node_name,omitempty"`
	CurrentVersion    uint64   `json:"current_version"`
	LastSyncedAt      int64    `json:"last_synced_at,omitempty"`
	Status            string   `json:"status"`
	Error             string   `json:"error,omitempty"`
	VersionsAvailable []uint64 `json:"versions_available,omitempty"`
	Timestamp         int64    `json:"timestamp"`
}

// StartClusterReencryptionRESTRequest represents the JSON request body for starting re-encryption.
type StartClusterReencryptionRESTRequest struct {
	Path         string `json:"path,omitempty"`
	FromVersion  uint64 `json:"from_version,omitempty"`
	ToVersion    uint64 `json:"to_version,omitempty"`
	BatchSize    int    `json:"batch_size,omitempty"`
	Concurrency  int    `json:"concurrency,omitempty"`
	ThrottleRate int    `json:"throttle_rate,omitempty"`
	Force        bool   `json:"force,omitempty"`
}

// StartClusterReencryptionRESTResponse represents the JSON response for starting re-encryption.
type StartClusterReencryptionRESTResponse struct {
	OperationID         string `json:"operation_id"`
	Path                string `json:"path,omitempty"`
	Started             bool   `json:"started"`
	EstimatedItems      int    `json:"estimated_items"`
	StartedAt           int64  `json:"started_at"`
	TotalEntities       int64  `json:"total_entities"`
	EstimatedCompletion int64  `json:"estimated_completion,omitempty"`
	Timestamp           int64  `json:"timestamp"`
}

// ClusterReencryptionStatusRESTResponse represents the JSON response for re-encryption status.
type ClusterReencryptionStatusRESTResponse struct {
	OperationID         string                         `json:"operation_id"`
	State               string                         `json:"state"`
	StartedAt           int64                          `json:"started_at"`
	CompletedAt         int64                          `json:"completed_at,omitempty"`
	EstimatedCompletion int64                          `json:"estimated_completion,omitempty"`
	Progress            float64                        `json:"progress"`
	TotalEntities       int64                          `json:"total_entities"`
	ProcessedEntities   int64                          `json:"processed_entities"`
	FailedEntities      int64                          `json:"failed_entities"`
	ThroughputRate      float64                        `json:"throughput_rate"`
	NodeProgress        []NodeReencryptionProgressREST `json:"node_progress,omitempty"`
	Errors              []ReencryptionErrorREST        `json:"errors,omitempty"`
	Timestamp           int64                          `json:"timestamp"`
}

// NodeReencryptionProgressREST represents per-node re-encryption progress.
type NodeReencryptionProgressREST struct {
	NodeID            uint64  `json:"node_id"`
	NodeName          string  `json:"node_name,omitempty"`
	Progress          float64 `json:"progress"`
	ProcessedEntities int64   `json:"processed_entities"`
	TotalEntities     int64   `json:"total_entities"`
	ThroughputRate    float64 `json:"throughput_rate"`
	LastUpdatedAt     int64   `json:"last_updated_at"`
}

// ReencryptionErrorREST represents a re-encryption error.
type ReencryptionErrorREST struct {
	Timestamp int64  `json:"timestamp"`
	NodeID    uint64 `json:"node_id"`
	EntityID  string `json:"entity_id,omitempty"`
	Error     string `json:"error"`
	Retried   bool   `json:"retried"`
}

// RecoverDEKRESTRequest represents the JSON request body for DEK recovery.
type RecoverDEKRESTRequest struct {
	RecoveryKeys   []string `json:"recovery_keys,omitempty"`
	BackupLocation string   `json:"backup_location,omitempty"`
	ManualKey      string   `json:"manual_key,omitempty"`
	RecoveryMethod string   `json:"recovery_method"`
	Reason         string   `json:"reason"`
}

// RecoverDEKRESTResponse represents the JSON response for DEK recovery.
type RecoverDEKRESTResponse struct {
	Success          bool     `json:"success"`
	RecoveredVersion uint64   `json:"recovered_version"`
	RecoveredAt      int64    `json:"recovered_at"`
	NodesSynced      int      `json:"nodes_synced"`
	AuditID          string   `json:"audit_id"`
	Warnings         []string `json:"warnings,omitempty"`
	Timestamp        int64    `json:"timestamp"`
}

// =============================================================================
// Handler Functions
// =============================================================================

// HandleInitializeClusterDEK handles POST /v1/cluster/dek/init - Initialize cluster DEK.
func (s *Server) HandleInitializeClusterDEK(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req InitClusterDEKRESTRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}

	clusterDEKService := s.service.ClusterDEK()
	if clusterDEKService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrClusterDEKServiceUnavailable)
		return
	}

	svcReq := &service.InitClusterDEKRequest{
		Algorithm: req.Algorithm,
		KeySize:   req.KeySize,
		Metadata:  req.Metadata,
	}

	if req.RotationPolicyEnabled {
		svcReq.RotationPolicy = &service.DEKRotationPolicy{
			Enabled:          req.RotationPolicyEnabled,
			RotationInterval: time.Duration(req.RotationIntervalSeconds) * time.Second,
			AutoReencrypt:    req.AutoReencrypt,
		}
	}

	svcResp, err := clusterDEKService.InitializeClusterDEK(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &InitClusterDEKRESTResponse{
		Version:     svcResp.Version,
		CreatedAt:   svcResp.CreatedAt.Unix(),
		NodesSynced: svcResp.NodesSynced,
		Algorithm:   svcResp.Algorithm,
		KeySize:     svcResp.KeySize,
		Timestamp:   now.Unix(),
	}

	RespondJSON(w, r, http.StatusCreated, resp)
}

// HandleGetClusterDEKStatus handles GET /v1/cluster/dek/status - Get cluster DEK status.
func (s *Server) HandleGetClusterDEKStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	clusterDEKService := s.service.ClusterDEK()
	if clusterDEKService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrClusterDEKServiceUnavailable)
		return
	}

	svcResp, err := clusterDEKService.GetClusterDEKStatus(ctx)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &ClusterDEKStatusRESTResponse{
		Initialized:            svcResp.Initialized,
		CurrentVersion:         svcResp.CurrentVersion,
		NodesSynced:            svcResp.NodesSynced,
		TotalNodes:             svcResp.TotalNodes,
		Algorithm:              svcResp.Algorithm,
		KeySize:                svcResp.KeySize,
		ReencryptionInProgress: svcResp.ReencryptionInProgress,
		ReencryptionProgress:   svcResp.ReencryptionProgress,
		Timestamp:              now.Unix(),
	}

	if !svcResp.CreatedAt.IsZero() {
		resp.CreatedAt = svcResp.CreatedAt.Unix()
	}
	if svcResp.RotatedAt != nil {
		resp.RotatedAt = svcResp.RotatedAt.Unix()
	}
	if svcResp.NextRotation != nil {
		resp.NextRotation = svcResp.NextRotation.Unix()
	}

	if len(svcResp.NodesOutOfSync) > 0 {
		resp.NodesOutOfSync = make([]NodeDEKInfoREST, 0, len(svcResp.NodesOutOfSync))
		for _, node := range svcResp.NodesOutOfSync {
			nodeInfo := NodeDEKInfoREST{
				NodeID:         node.NodeID,
				NodeName:       node.NodeName,
				CurrentVersion: node.CurrentVersion,
			}
			if !node.LastSyncedAt.IsZero() {
				nodeInfo.LastSyncedAt = node.LastSyncedAt.Unix()
			}
			resp.NodesOutOfSync = append(resp.NodesOutOfSync, nodeInfo)
		}
	}

	if len(svcResp.PreviousVersions) > 0 {
		resp.PreviousVersions = make([]DEKVersionInfoREST, 0, len(svcResp.PreviousVersions))
		for _, ver := range svcResp.PreviousVersions {
			verInfo := DEKVersionInfoREST{
				Version:           ver.Version,
				CreatedAt:         ver.CreatedAt.Unix(),
				EntitiesEncrypted: ver.EntitiesEncrypted,
				CanDelete:         ver.CanDelete,
			}
			if ver.DeprecatedAt != nil {
				verInfo.DeprecatedAt = ver.DeprecatedAt.Unix()
			}
			resp.PreviousVersions = append(resp.PreviousVersions, verInfo)
		}
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleRotateClusterDEK handles POST /v1/cluster/dek/rotate - Rotate cluster DEK.
func (s *Server) HandleRotateClusterDEK(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req RotateClusterDEKRESTRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}

	clusterDEKService := s.service.ClusterDEK()
	if clusterDEKService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrClusterDEKServiceUnavailable)
		return
	}

	svcReq := &service.RotateClusterDEKRequest{
		Reason:                req.Reason,
		ForceRotation:         req.ForceRotation,
		AutoStartReencryption: req.AutoStartReencryption,
		Metadata:              req.Metadata,
	}

	if req.ScheduledTime > 0 {
		scheduledTime := time.Unix(req.ScheduledTime, 0)
		svcReq.ScheduledTime = &scheduledTime
	}

	svcResp, err := clusterDEKService.RotateClusterDEK(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &RotateClusterDEKRESTResponse{
		NewVersion:          svcResp.NewVersion,
		OldVersion:          svcResp.OldVersion,
		RotatedAt:           svcResp.RotatedAt.Unix(),
		RotationID:          svcResp.RotationID,
		NodesSynced:         svcResp.NodesSynced,
		ReencryptionStarted: svcResp.ReencryptionStarted,
		Timestamp:           now.Unix(),
	}

	if svcResp.EstimatedReencryptionTime > 0 {
		resp.EstimatedReencryptionTime = int64(svcResp.EstimatedReencryptionTime.Seconds())
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleSyncNodeDEK handles POST /v1/cluster/dek/sync/{nodeId} - Sync DEK to a node.
func (s *Server) HandleSyncNodeDEK(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	nodeIDStr := chi.URLParam(r, "nodeId")
	if nodeIDStr == "" {
		RespondBadRequest(w, r, "node ID is required")
		return
	}

	nodeID, err := strconv.ParseUint(nodeIDStr, 10, 64)
	if err != nil {
		RespondBadRequest(w, r, "invalid node ID format")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req SyncNodeDEKRESTRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}

	clusterDEKService := s.service.ClusterDEK()
	if clusterDEKService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrClusterDEKServiceUnavailable)
		return
	}

	svcReq := &service.SyncNodeDEKRequest{
		NodeID:         nodeID,
		CurrentVersion: req.CurrentVersion,
		ForceResync:    req.ForceResync,
	}

	svcResp, err := clusterDEKService.SyncNodeDEK(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &SyncNodeDEKRESTResponse{
		NodeID:         svcResp.NodeID,
		VersionsSynced: svcResp.VersionsSynced,
		CurrentVersion: svcResp.CurrentVersion,
		SyncedAt:       svcResp.SyncedAt.Unix(),
		Timestamp:      now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetNodeDEKStatus handles GET /v1/cluster/dek/nodes/{nodeId} - Get node DEK status.
func (s *Server) HandleGetNodeDEKStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	nodeIDStr := chi.URLParam(r, "nodeId")
	if nodeIDStr == "" {
		RespondBadRequest(w, r, "node ID is required")
		return
	}

	nodeID, err := strconv.ParseUint(nodeIDStr, 10, 64)
	if err != nil {
		RespondBadRequest(w, r, "invalid node ID format")
		return
	}

	clusterDEKService := s.service.ClusterDEK()
	if clusterDEKService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrClusterDEKServiceUnavailable)
		return
	}

	svcReq := &service.GetNodeDEKStatusRequest{
		NodeID: nodeID,
	}

	svcResp, err := clusterDEKService.GetNodeDEKStatus(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &NodeDEKStatusRESTResponse{
		NodeID:            svcResp.NodeID,
		NodeName:          svcResp.NodeName,
		CurrentVersion:    svcResp.CurrentVersion,
		Status:            string(svcResp.Status),
		Error:             svcResp.Error,
		VersionsAvailable: svcResp.VersionsAvailable,
		Timestamp:         now.Unix(),
	}

	if !svcResp.LastSyncedAt.IsZero() {
		resp.LastSyncedAt = svcResp.LastSyncedAt.Unix()
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleStartClusterReencryption handles POST /v1/cluster/dek/reencryption/start - Start re-encryption.
func (s *Server) HandleStartClusterReencryption(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req StartClusterReencryptionRESTRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}

	clusterDEKService := s.service.ClusterDEK()
	if clusterDEKService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrClusterDEKServiceUnavailable)
		return
	}

	svcReq := &service.StartReencryptionRequest{
		Path:         req.Path,
		FromVersion:  req.FromVersion,
		ToVersion:    req.ToVersion,
		BatchSize:    req.BatchSize,
		Concurrency:  req.Concurrency,
		ThrottleRate: req.ThrottleRate,
		Force:        req.Force,
	}

	svcResp, err := clusterDEKService.StartReencryption(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &StartClusterReencryptionRESTResponse{
		OperationID:    svcResp.OperationID,
		Path:           svcResp.Path,
		Started:        svcResp.Started,
		EstimatedItems: svcResp.EstimatedItems,
		StartedAt:      svcResp.StartedAt.Unix(),
		TotalEntities:  svcResp.TotalEntities,
		Timestamp:      now.Unix(),
	}

	if !svcResp.EstimatedCompletion.IsZero() {
		resp.EstimatedCompletion = svcResp.EstimatedCompletion.Unix()
	}

	RespondJSON(w, r, http.StatusAccepted, resp)
}

// HandleGetClusterReencryptionStatus handles GET /v1/cluster/dek/reencryption/status - Get re-encryption status.
func (s *Server) HandleGetClusterReencryptionStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	clusterDEKService := s.service.ClusterDEK()
	if clusterDEKService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrClusterDEKServiceUnavailable)
		return
	}

	svcResp, err := clusterDEKService.GetReencryptionStatus(ctx)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &ClusterReencryptionStatusRESTResponse{
		OperationID:       svcResp.OperationID,
		State:             string(svcResp.State),
		StartedAt:         svcResp.StartedAt.Unix(),
		Progress:          svcResp.Progress,
		TotalEntities:     svcResp.TotalEntities,
		ProcessedEntities: svcResp.ProcessedEntities,
		FailedEntities:    svcResp.FailedEntities,
		ThroughputRate:    svcResp.ThroughputRate,
		Timestamp:         now.Unix(),
	}

	if svcResp.CompletedAt != nil {
		resp.CompletedAt = svcResp.CompletedAt.Unix()
	}
	if svcResp.EstimatedCompletion != nil {
		resp.EstimatedCompletion = svcResp.EstimatedCompletion.Unix()
	}

	if len(svcResp.NodeProgress) > 0 {
		resp.NodeProgress = make([]NodeReencryptionProgressREST, 0, len(svcResp.NodeProgress))
		for _, np := range svcResp.NodeProgress {
			resp.NodeProgress = append(resp.NodeProgress, NodeReencryptionProgressREST{
				NodeID:            np.NodeID,
				NodeName:          np.NodeName,
				Progress:          np.Progress,
				ProcessedEntities: np.ProcessedEntities,
				TotalEntities:     np.TotalEntities,
				ThroughputRate:    np.ThroughputRate,
				LastUpdatedAt:     np.LastUpdatedAt.Unix(),
			})
		}
	}

	if len(svcResp.Errors) > 0 {
		resp.Errors = make([]ReencryptionErrorREST, 0, len(svcResp.Errors))
		for _, e := range svcResp.Errors {
			resp.Errors = append(resp.Errors, ReencryptionErrorREST{
				Timestamp: e.Timestamp.Unix(),
				NodeID:    e.NodeID,
				EntityID:  e.EntityID,
				Error:     e.Error,
				Retried:   e.Retried,
			})
		}
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleCancelClusterReencryption handles DELETE /v1/cluster/dek/reencryption - Cancel re-encryption.
func (s *Server) HandleCancelClusterReencryption(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	clusterDEKService := s.service.ClusterDEK()
	if clusterDEKService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrClusterDEKServiceUnavailable)
		return
	}

	err := clusterDEKService.CancelReencryption(ctx)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := map[string]interface{}{
		"success":   true,
		"timestamp": now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleRecoverDEK handles POST /v1/cluster/dek/recover - Recover cluster DEK.
func (s *Server) HandleRecoverDEK(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req RecoverDEKRESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.RecoveryMethod == "" {
		RespondBadRequest(w, r, "recovery_method is required")
		return
	}
	if req.Reason == "" {
		RespondBadRequest(w, r, "reason is required")
		return
	}

	clusterDEKService := s.service.ClusterDEK()
	if clusterDEKService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrClusterDEKServiceUnavailable)
		return
	}

	svcReq := &service.RecoverDEKRequest{
		BackupLocation: req.BackupLocation,
		RecoveryMethod: service.RecoveryMethod(req.RecoveryMethod),
		Reason:         req.Reason,
	}

	// Convert base64-encoded recovery keys to byte slices
	if len(req.RecoveryKeys) > 0 {
		svcReq.RecoveryKeys = make([][]byte, 0, len(req.RecoveryKeys))
		for _, keyStr := range req.RecoveryKeys {
			keyBytes, decodeErr := base64.StdEncoding.DecodeString(keyStr)
			if decodeErr != nil {
				RespondBadRequest(w, r, "invalid base64 encoding in recovery_keys")
				return
			}
			svcReq.RecoveryKeys = append(svcReq.RecoveryKeys, keyBytes)
		}
	}

	// Convert base64-encoded manual key
	if req.ManualKey != "" {
		keyBytes, decodeErr := base64.StdEncoding.DecodeString(req.ManualKey)
		if decodeErr != nil {
			RespondBadRequest(w, r, "invalid base64 encoding in manual_key")
			return
		}
		svcReq.ManualKey = keyBytes
	}

	svcResp, err := clusterDEKService.RecoverDEK(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &RecoverDEKRESTResponse{
		Success:          svcResp.Success,
		RecoveredVersion: svcResp.RecoveredVersion,
		RecoveredAt:      svcResp.RecoveredAt.Unix(),
		NodesSynced:      svcResp.NodesSynced,
		AuditID:          svcResp.AuditID,
		Warnings:         svcResp.Warnings,
		Timestamp:        now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}
