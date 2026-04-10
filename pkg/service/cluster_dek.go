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

// Package service implements the unified service facade for QuicBaaS.
// This file contains the Cluster DEK service implementation.
package service

import (
	"context"
	"fmt"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// =============================================================================
// Cluster DEK Service Implementation
// =============================================================================

// clusterDEKService implements the ClusterDEKService interface.
// It provides cluster-wide Data Encryption Key management operations,
// orchestrating DEK lifecycle, distribution, rotation, and recovery.
type clusterDEKService struct {
	dekManager  interfaces.ClusterDEKManager
	sealManager interfaces.SealManager
	logger      interfaces.Logger
	metrics     *clusterDEKMetrics
}

// clusterDEKMetrics holds Prometheus metrics for cluster DEK operations.
type clusterDEKMetrics struct {
	dekInitializedTotal         *metrics.Counter
	dekRotatedTotal             *metrics.Counter
	nodeSyncOperationsTotal     *metrics.CounterVec
	reencryptionOperationsTotal *metrics.CounterVec
	dekRecoveryOperationsTotal  *metrics.CounterVec
}

// newClusterDEKService creates a new cluster DEK service.
//
// Parameters:
//   - dekManager: The cluster DEK manager for key operations
//   - sealManager: The seal manager for master key access
//   - logger: Logger for operation tracking
//
// Returns:
//   - A new ClusterDEKService
func newClusterDEKService(
	dekManager interfaces.ClusterDEKManager,
	sealManager interfaces.SealManager,
	logger interfaces.Logger,
) *clusterDEKService {
	svc := &clusterDEKService{
		dekManager:  dekManager,
		sealManager: sealManager,
		logger:      logger,
	}

	// Initialize metrics
	svc.initMetrics()

	return svc
}

// initMetrics initializes Prometheus metrics for cluster DEK operations.
func (s *clusterDEKService) initMetrics() {
	s.metrics = &clusterDEKMetrics{
		dekInitializedTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "cluster_dek",
			Name:      "initialized_total",
			Help:      "Total number of cluster DEK initializations",
		}),

		dekRotatedTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "cluster_dek",
			Name:      "rotated_total",
			Help:      "Total number of cluster DEK rotations",
		}),

		nodeSyncOperationsTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "cluster_dek",
			Name:      "node_sync_operations_total",
			Help:      "Total number of node DEK sync operations",
		}, []string{"status"}),

		reencryptionOperationsTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "cluster_dek",
			Name:      "reencryption_operations_total",
			Help:      "Total number of re-encryption operations",
		}, []string{"operation", "status"}),

		dekRecoveryOperationsTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "cluster_dek",
			Name:      "recovery_operations_total",
			Help:      "Total number of DEK recovery operations",
		}, []string{"method", "status"}),
	}
}

// =============================================================================
// DEK Lifecycle Operations
// =============================================================================

// InitializeClusterDEK creates and distributes the initial cluster DEK.
func (s *clusterDEKService) InitializeClusterDEK(
	ctx context.Context,
	req *InitClusterDEKRequest,
) (*InitClusterDEKResponse, error) {
	// Validate request
	if err := s.validateInitRequest(req); err != nil {
		return nil, err
	}

	// Check if seal is unsealed (master key available)
	if s.sealManager != nil && s.sealManager.IsSealed() {
		return nil, NewError(
			dberrors.ErrForbidden,
			"clusterDEKService.InitializeClusterDEK",
			"database must be unsealed before initializing cluster DEK",
		)
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"algorithm": req.Algorithm,
			"key_size":  req.KeySize,
		}).Info("Initializing cluster DEK")
	}

	// Determine key size in bytes
	keyBytes := req.KeySize / 8
	if keyBytes <= 0 {
		keyBytes = 32 // Default to 256 bits
	}

	// Initialize through manager
	if err := s.dekManager.Initialize(ctx, req.Algorithm, keyBytes); err != nil {
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"error": err.Error(),
			}).Error("Failed to initialize cluster DEK")
		}
		return nil, WrapError(
			err,
			dberrors.ErrInternal,
			"clusterDEKService.InitializeClusterDEK",
			"failed to initialize cluster DEK",
		)
	}

	// Get status to build response
	statusResp, err := s.dekManager.GetStatus(ctx)
	if err != nil {
		return nil, WrapError(
			err,
			dberrors.ErrInternal,
			"clusterDEKService.InitializeClusterDEK",
			"failed to get DEK status after initialization",
		)
	}

	// Update metrics
	s.metrics.dekInitializedTotal.Inc()

	// Build response
	resp := s.buildInitResponse(statusResp, req)

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"version":      resp.Version,
			"nodes_synced": resp.NodesSynced,
		}).Info("Cluster DEK initialized successfully")
	}

	return resp, nil
}

// GetClusterDEKStatus retrieves current cluster DEK status and metadata.
func (s *clusterDEKService) GetClusterDEKStatus(
	ctx context.Context,
) (*ClusterDEKStatusResponse, error) {
	// Get status from manager
	status, err := s.dekManager.GetStatus(ctx)
	if err != nil {
		return nil, WrapError(
			err,
			dberrors.ErrInternal,
			"clusterDEKService.GetClusterDEKStatus",
			"failed to get cluster DEK status",
		)
	}

	// Convert to service response
	resp := s.buildStatusResponse(status)

	return resp, nil
}

// RotateClusterDEK generates a new cluster DEK and initiates re-encryption.
func (s *clusterDEKService) RotateClusterDEK(
	ctx context.Context,
	req *RotateClusterDEKRequest,
) (*RotateClusterDEKResponse, error) {
	// Validate request
	if err := s.validateRotateRequest(req); err != nil {
		return nil, err
	}

	// Check if seal is unsealed
	if s.sealManager != nil && s.sealManager.IsSealed() {
		return nil, NewError(
			dberrors.ErrForbidden,
			"clusterDEKService.RotateClusterDEK",
			"database must be unsealed before rotating cluster DEK",
		)
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"reason":                  req.Reason,
			"auto_start_reencryption": req.AutoStartReencryption,
		}).Info("Rotating cluster DEK")
	}

	// Get current version before rotation
	oldVersion := s.dekManager.GetDEKVersion()

	// Perform rotation
	if err := s.dekManager.Rotate(ctx); err != nil {
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"error": err.Error(),
			}).Error("Failed to rotate cluster DEK")
		}
		return nil, WrapError(
			err,
			dberrors.ErrInternal,
			"clusterDEKService.RotateClusterDEK",
			"failed to rotate cluster DEK",
		)
	}

	// Get new version
	newVersion := s.dekManager.GetDEKVersion()

	// Start re-encryption if requested
	reencryptionStarted := false
	if req.AutoStartReencryption {
		if err := s.dekManager.StartReencryption(ctx); err != nil {
			if s.logger != nil {
				s.logger.WithFields(map[string]interface{}{
					"error": err.Error(),
				}).Warn("Failed to start re-encryption after rotation")
			}
			// Don't fail the rotation, just log the warning
		} else {
			reencryptionStarted = true
		}
	}

	// Update metrics
	s.metrics.dekRotatedTotal.Inc()
	if reencryptionStarted {
		s.metrics.reencryptionOperationsTotal.With("start", "success").Inc()
	}

	// Build response
	resp := &RotateClusterDEKResponse{
		NewVersion:          newVersion,
		OldVersion:          oldVersion,
		RotatedAt:           time.Now(),
		RotationID:          fmt.Sprintf("rot-%d-%d", oldVersion, newVersion),
		NodesSynced:         0, // Will be updated by status
		ReencryptionStarted: reencryptionStarted,
	}

	// Get status to update nodes synced count
	if status, err := s.dekManager.GetStatus(ctx); err == nil {
		resp.NodesSynced = s.countSyncedNodes(status)
	}

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"old_version":          oldVersion,
			"new_version":          newVersion,
			"reencryption_started": reencryptionStarted,
		}).Info("Cluster DEK rotated successfully")
	}

	return resp, nil
}

// =============================================================================
// Node Key Management
// =============================================================================

// SyncNodeDEK synchronizes cluster DEK to a specific node.
func (s *clusterDEKService) SyncNodeDEK(
	ctx context.Context,
	req *SyncNodeDEKRequest,
) (*SyncNodeDEKResponse, error) {
	// Validate request
	if err := s.validateSyncNodeRequest(req); err != nil {
		return nil, err
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"node_id":         req.NodeID,
			"current_version": req.CurrentVersion,
			"force_resync":    req.ForceResync,
		}).Info("Syncing cluster DEK to node")
	}

	// Perform sync
	if err := s.dekManager.SyncToNode(ctx, req.NodeID); err != nil {
		s.metrics.nodeSyncOperationsTotal.With("failed").Inc()
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"node_id": req.NodeID,
				"error":   err.Error(),
			}).Error("Failed to sync DEK to node")
		}
		return nil, WrapError(
			err,
			dberrors.ErrInternal,
			"clusterDEKService.SyncNodeDEK",
			fmt.Sprintf("failed to sync DEK to node %d", req.NodeID),
		)
	}

	// Update metrics
	s.metrics.nodeSyncOperationsTotal.With("success").Inc()

	// Build response
	currentVersion := s.dekManager.GetDEKVersion()
	resp := &SyncNodeDEKResponse{
		NodeID:         req.NodeID,
		VersionsSynced: []uint64{currentVersion},
		CurrentVersion: currentVersion,
		SyncedAt:       time.Now(),
	}

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"node_id": req.NodeID,
			"version": currentVersion,
		}).Info("Node DEK sync completed successfully")
	}

	return resp, nil
}

// GetNodeDEKStatus retrieves DEK sync status for a specific node.
func (s *clusterDEKService) GetNodeDEKStatus(
	ctx context.Context,
	req *GetNodeDEKStatusRequest,
) (*GetNodeDEKStatusResponse, error) {
	// Validate request
	if req.NodeID == 0 {
		return nil, NewError(
			dberrors.ErrInvalidInput,
			"clusterDEKService.GetNodeDEKStatus",
			"node ID is required",
		)
	}

	// Check if node is synced
	isSynced := s.dekManager.IsNodeSynced(req.NodeID)
	currentVersion := s.dekManager.GetDEKVersion()

	// Determine status
	var status NodeDEKSyncStatus
	if isSynced {
		status = NodeDEKSynced
	} else {
		status = NodeDEKOutOfDate
	}

	// Get the actual last sync time from the manager status
	lastSyncedAt := s.getNodeLastSyncTime(ctx, req.NodeID)

	// Build response
	resp := &GetNodeDEKStatusResponse{
		NodeID:            req.NodeID,
		CurrentVersion:    currentVersion,
		LastSyncedAt:      lastSyncedAt,
		Status:            status,
		VersionsAvailable: []uint64{currentVersion},
	}

	return resp, nil
}

// getNodeLastSyncTime retrieves the last sync time for a specific node from the manager.
func (s *clusterDEKService) getNodeLastSyncTime(ctx context.Context, nodeID uint64) time.Time {
	status, err := s.dekManager.GetStatus(ctx)
	if err != nil {
		// If we can't get status, return zero time indicating unknown
		return time.Time{}
	}

	// Try to extract sync time from ClusterDEKStatus
	if dekStatus, ok := status.(interface {
		GetSyncedNodes() map[uint64]string
	}); ok {
		syncedNodes := dekStatus.GetSyncedNodes()
		if syncTimeStr, exists := syncedNodes[nodeID]; exists {
			if syncTime, err := time.Parse(time.RFC3339, syncTimeStr); err == nil {
				return syncTime
			}
		}
	}

	// Try direct field access for ClusterDEKStatus-like struct
	if dekStatus, ok := status.(interface{ SyncedNodes() map[uint64]string }); ok {
		syncedNodes := dekStatus.SyncedNodes()
		if syncTimeStr, exists := syncedNodes[nodeID]; exists {
			if syncTime, err := time.Parse(time.RFC3339, syncTimeStr); err == nil {
				return syncTime
			}
		}
	}

	// Fallback: try to access via reflection-free approach using map
	if statusMap, ok := status.(map[string]interface{}); ok {
		if syncedNodes, ok := statusMap["synced_nodes"].(map[uint64]string); ok {
			if syncTimeStr, exists := syncedNodes[nodeID]; exists {
				if syncTime, err := time.Parse(time.RFC3339, syncTimeStr); err == nil {
					return syncTime
				}
			}
		}
	}

	// If node is synced but we can't get exact time, return current time as best estimate
	if s.dekManager.IsNodeSynced(nodeID) {
		return time.Now()
	}

	// Return zero time for nodes that have never synced
	return time.Time{}
}

// =============================================================================
// Re-encryption Operations
// =============================================================================

// StartReencryption manually initiates re-encryption of data with new DEK.
func (s *clusterDEKService) StartReencryption(
	ctx context.Context,
	req *StartReencryptionRequest,
) (*StartReencryptionResponse, error) {
	// Validate request
	if err := s.validateReencryptionRequest(req); err != nil {
		return nil, err
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"from_version": req.FromVersion,
			"to_version":   req.ToVersion,
			"batch_size":   req.BatchSize,
			"concurrency":  req.Concurrency,
		}).Info("Starting re-encryption")
	}

	// Start re-encryption
	if err := s.dekManager.StartReencryption(ctx); err != nil {
		s.metrics.reencryptionOperationsTotal.With("start", "failed").Inc()
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"error": err.Error(),
			}).Error("Failed to start re-encryption")
		}
		return nil, WrapError(
			err,
			dberrors.ErrInternal,
			"clusterDEKService.StartReencryption",
			"failed to start re-encryption",
		)
	}

	// Update metrics
	s.metrics.reencryptionOperationsTotal.With("start", "success").Inc()

	// Get re-encryption status to extract actual values
	reencStatus, err := s.dekManager.GetReencryptionStatus(ctx)
	if err != nil {
		// If we can't get status, use defaults
		operationID := fmt.Sprintf("reenc-%d-%d-%d", req.FromVersion, req.ToVersion, time.Now().Unix())
		return &StartReencryptionResponse{
			OperationID:         operationID,
			Started:             true,
			StartedAt:           time.Now(),
			TotalEntities:       0,
			EstimatedCompletion: time.Now().Add(time.Hour), // Default fallback
		}, nil
	}

	// Extract actual values from re-encryption status
	totalEntities, estimatedCompletion := s.extractReencryptionInfo(reencStatus, req)

	// Build response
	operationID := fmt.Sprintf("reenc-%d-%d-%d", req.FromVersion, req.ToVersion, time.Now().Unix())
	resp := &StartReencryptionResponse{
		OperationID:         operationID,
		Started:             true,
		StartedAt:           time.Now(),
		TotalEntities:       totalEntities,
		EstimatedCompletion: estimatedCompletion,
	}

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"operation_id":   operationID,
			"total_entities": totalEntities,
		}).Info("Re-encryption started successfully")
	}

	return resp, nil
}

// extractReencryptionInfo extracts total entities and calculates estimated completion
// from the re-encryption status returned by the manager.
func (s *clusterDEKService) extractReencryptionInfo(
	status interface{},
	req *StartReencryptionRequest,
) (totalEntities int64, estimatedCompletion time.Time) {
	// Default values
	totalEntities = 0
	estimatedCompletion = time.Now().Add(time.Hour)

	if status == nil {
		return
	}

	// Try to extract from ReencryptionInfo type
	if info, ok := status.(interface {
		GetTotalKeys() uint64
		GetProcessedKeys() uint64
		GetStartedAt() time.Time
	}); ok {
		totalEntities = int64(info.GetTotalKeys()) // #nosec G115 -- key count fits in int64
		estimatedCompletion = s.calculateEstimatedCompletion(
			info.GetStartedAt(),
			info.GetTotalKeys(),
			info.GetProcessedKeys(),
			req,
		)
		return
	}

	// Try direct field access for ReencryptionInfo-like struct
	type reencryptionInfo struct {
		TotalKeys     uint64
		ProcessedKeys uint64
		StartedAt     time.Time
	}

	if info, ok := status.(*reencryptionInfo); ok && info != nil {
		//nolint:gosec // G115: TotalKeys is bounded by key rotation limits, safe for int64
		totalEntities = int64(info.TotalKeys)
		estimatedCompletion = s.calculateEstimatedCompletion(
			info.StartedAt,
			info.TotalKeys,
			info.ProcessedKeys,
			req,
		)
		return
	}

	// Try map-based access
	if statusMap, ok := status.(map[string]interface{}); ok {
		if total, ok := statusMap["total_keys"].(uint64); ok {
			//nolint:gosec // G115: total is bounded by key count, safe for int64
			totalEntities = int64(total)
		}
		if started, ok := statusMap["started_at"].(time.Time); ok {
			processedKeys := uint64(0)
			if processed, ok := statusMap["processed_keys"].(uint64); ok {
				processedKeys = processed
			}
			estimatedCompletion = s.calculateEstimatedCompletion(
				started,
				uint64(totalEntities), // #nosec G115 -- totalEntities is non-negative after assignment
				processedKeys,
				req,
			)
		}
	}

	return
}

// calculateEstimatedCompletion calculates the estimated completion time based on
// current progress and throughput rate.
func (s *clusterDEKService) calculateEstimatedCompletion(
	startedAt time.Time,
	totalKeys, processedKeys uint64,
	req *StartReencryptionRequest,
) time.Time {
	now := time.Now()

	// If no keys to process, complete immediately
	if totalKeys == 0 {
		return now
	}

	// If we have processed some keys, calculate based on actual throughput
	if processedKeys > 0 {
		elapsed := now.Sub(startedAt)
		if elapsed > 0 {
			keysPerSecond := float64(processedKeys) / elapsed.Seconds()
			if keysPerSecond > 0 {
				remainingKeys := totalKeys - processedKeys
				remainingSeconds := float64(remainingKeys) / keysPerSecond
				return now.Add(time.Duration(remainingSeconds) * time.Second)
			}
		}
	}

	// Estimate based on batch size and concurrency from request
	batchSize := req.BatchSize
	if batchSize <= 0 {
		batchSize = 100 // Default batch size
	}
	concurrency := req.Concurrency
	if concurrency <= 0 {
		concurrency = 4 // Default concurrency
	}

	// Estimate ~50ms per batch (conservative estimate)
	batchDuration := 50 * time.Millisecond
	batchesNeeded := (totalKeys + uint64(batchSize) - 1) / uint64(batchSize)           // #nosec G115 -- batchSize validated > 0
	parallelBatches := (batchesNeeded + uint64(concurrency) - 1) / uint64(concurrency) // #nosec G115 -- concurrency validated > 0
	estimatedDuration := time.Duration(parallelBatches) * batchDuration                // #nosec G115 -- parallelBatches bounded by batch calculation

	// Add a minimum duration and cap at reasonable maximum
	if estimatedDuration < time.Minute {
		estimatedDuration = time.Minute
	}
	if estimatedDuration > 24*time.Hour {
		estimatedDuration = 24 * time.Hour
	}

	return now.Add(estimatedDuration)
}

// GetReencryptionStatus retrieves current re-encryption progress.
func (s *clusterDEKService) GetReencryptionStatus(
	ctx context.Context,
) (*ReencryptionStatusResponse, error) {
	// Get status from manager
	status, err := s.dekManager.GetReencryptionStatus(ctx)
	if err != nil {
		return nil, WrapError(
			err,
			dberrors.ErrInternal,
			"clusterDEKService.GetReencryptionStatus",
			"failed to get re-encryption status",
		)
	}

	// Convert to service response
	resp := s.buildReencryptionStatusResponse(status)

	return resp, nil
}

// CancelReencryption cancels ongoing re-encryption process.
func (s *clusterDEKService) CancelReencryption(ctx context.Context) error {
	// Log operation
	if s.logger != nil {
		s.logger.Info("Canceling re-encryption")
	}

	// Cancel through manager
	if err := s.dekManager.CancelReencryption(ctx); err != nil {
		s.metrics.reencryptionOperationsTotal.With("cancel", "failed").Inc()
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"error": err.Error(),
			}).Error("Failed to cancel re-encryption")
		}
		return WrapError(
			err,
			dberrors.ErrInternal,
			"clusterDEKService.CancelReencryption",
			"failed to cancel re-encryption",
		)
	}

	// Update metrics
	s.metrics.reencryptionOperationsTotal.With("cancel", "success").Inc()

	if s.logger != nil {
		s.logger.Info("Re-encryption canceled successfully")
	}

	return nil
}

// =============================================================================
// Recovery Operations
// =============================================================================

// RecoverDEK recovers cluster DEK using recovery keys or backup mechanism.
func (s *clusterDEKService) RecoverDEK(
	ctx context.Context,
	req *RecoverDEKRequest,
) (*RecoverDEKResponse, error) {
	// Validate request
	if err := s.validateRecoveryRequest(req); err != nil {
		return nil, err
	}

	// Log operation (without sensitive data)
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"recovery_method": req.RecoveryMethod,
			"reason":          req.Reason,
		}).Warn("Attempting DEK recovery")
	}

	// Perform recovery through manager
	// Note: req is passed as interface{} to match ClusterDEKManager interface
	if err := s.dekManager.RecoverDEK(ctx, req); err != nil {
		s.metrics.dekRecoveryOperationsTotal.With(string(req.RecoveryMethod), "failed").Inc()
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"error": err.Error(),
			}).Error("DEK recovery failed")
		}
		return nil, WrapError(
			err,
			dberrors.ErrInternal,
			"clusterDEKService.RecoverDEK",
			"failed to recover cluster DEK",
		)
	}

	// Update metrics
	s.metrics.dekRecoveryOperationsTotal.With(string(req.RecoveryMethod), "success").Inc()

	// Get recovered version
	recoveredVersion := s.dekManager.GetDEKVersion()

	// Build response
	resp := &RecoverDEKResponse{
		Success:          true,
		RecoveredVersion: recoveredVersion,
		RecoveredAt:      time.Now(),
		NodesSynced:      0, // Will be updated by status
		AuditID:          fmt.Sprintf("recovery-%d-%d", recoveredVersion, time.Now().Unix()),
		Warnings:         []string{},
	}

	// Get status to update nodes synced count
	if status, err := s.dekManager.GetStatus(ctx); err == nil {
		resp.NodesSynced = s.countSyncedNodes(status)
	}

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"version":      recoveredVersion,
			"audit_id":     resp.AuditID,
			"nodes_synced": resp.NodesSynced,
		}).Info("DEK recovery completed successfully")
	}

	return resp, nil
}

// =============================================================================
// Validation Methods
// =============================================================================

// validateInitRequest validates cluster DEK initialization request.
func (s *clusterDEKService) validateInitRequest(req *InitClusterDEKRequest) error {
	if req == nil {
		return NewError(
			dberrors.ErrInvalidInput,
			"clusterDEKService.validateInitRequest",
			"request cannot be nil",
		)
	}

	if req.Algorithm == "" {
		return NewError(
			dberrors.ErrInvalidInput,
			"clusterDEKService.validateInitRequest",
			"algorithm is required",
		)
	}

	if req.KeySize <= 0 {
		return NewError(
			dberrors.ErrInvalidInput,
			"clusterDEKService.validateInitRequest",
			"key size must be positive",
		)
	}

	// Validate key size is a multiple of 8 (bits to bytes)
	if req.KeySize%8 != 0 {
		return NewError(
			dberrors.ErrInvalidInput,
			"clusterDEKService.validateInitRequest",
			"key size must be a multiple of 8",
		)
	}

	return nil
}

// validateRotateRequest validates DEK rotation request.
func (s *clusterDEKService) validateRotateRequest(req *RotateClusterDEKRequest) error {
	if req == nil {
		return NewError(
			dberrors.ErrInvalidInput,
			"clusterDEKService.validateRotateRequest",
			"request cannot be nil",
		)
	}

	if req.Reason == "" {
		return NewError(
			dberrors.ErrInvalidInput,
			"clusterDEKService.validateRotateRequest",
			"rotation reason is required for audit trail",
		)
	}

	return nil
}

// validateSyncNodeRequest validates node DEK sync request.
func (s *clusterDEKService) validateSyncNodeRequest(req *SyncNodeDEKRequest) error {
	if req == nil {
		return NewError(
			dberrors.ErrInvalidInput,
			"clusterDEKService.validateSyncNodeRequest",
			"request cannot be nil",
		)
	}

	if req.NodeID == 0 {
		return NewError(
			dberrors.ErrInvalidInput,
			"clusterDEKService.validateSyncNodeRequest",
			"node ID is required",
		)
	}

	return nil
}

// validateReencryptionRequest validates re-encryption start request.
func (s *clusterDEKService) validateReencryptionRequest(req *StartReencryptionRequest) error {
	if req == nil {
		return NewError(
			dberrors.ErrInvalidInput,
			"clusterDEKService.validateReencryptionRequest",
			"request cannot be nil",
		)
	}

	if req.FromVersion == 0 || req.ToVersion == 0 {
		return NewError(
			dberrors.ErrInvalidInput,
			"clusterDEKService.validateReencryptionRequest",
			"from and to versions are required",
		)
	}

	if req.FromVersion == req.ToVersion {
		return NewError(
			dberrors.ErrInvalidInput,
			"clusterDEKService.validateReencryptionRequest",
			"from and to versions must be different",
		)
	}

	return nil
}

// validateRecoveryRequest validates DEK recovery request.
func (s *clusterDEKService) validateRecoveryRequest(req *RecoverDEKRequest) error {
	if req == nil {
		return NewError(
			dberrors.ErrInvalidInput,
			"clusterDEKService.validateRecoveryRequest",
			"request cannot be nil",
		)
	}

	if req.RecoveryMethod == "" {
		return NewError(
			dberrors.ErrInvalidInput,
			"clusterDEKService.validateRecoveryRequest",
			"recovery method is required",
		)
	}

	if req.Reason == "" {
		return NewError(
			dberrors.ErrInvalidInput,
			"clusterDEKService.validateRecoveryRequest",
			"recovery reason is required for audit trail",
		)
	}

	// Validate recovery method specific requirements
	switch req.RecoveryMethod {
	case RecoveryMethodKeys:
		if len(req.RecoveryKeys) == 0 {
			return NewError(
				dberrors.ErrInvalidInput,
				"clusterDEKService.validateRecoveryRequest",
				"recovery keys are required for recovery method 'recovery_keys'",
			)
		}
	case RecoveryMethodBackup:
		if req.BackupLocation == "" {
			return NewError(
				dberrors.ErrInvalidInput,
				"clusterDEKService.validateRecoveryRequest",
				"backup location is required for recovery method 'backup'",
			)
		}
	case RecoveryMethodManual:
		if len(req.ManualKey) == 0 {
			return NewError(
				dberrors.ErrInvalidInput,
				"clusterDEKService.validateRecoveryRequest",
				"manual key is required for recovery method 'manual'",
			)
		}
	default:
		return NewErrorf(
			dberrors.ErrInvalidInput,
			"clusterDEKService.validateRecoveryRequest",
			"unsupported recovery method: %s",
			req.RecoveryMethod,
		)
	}

	return nil
}

// =============================================================================
// Helper Methods
// =============================================================================

// buildInitResponse builds initialization response from manager status.
func (s *clusterDEKService) buildInitResponse(
	status interface{},
	req *InitClusterDEKRequest,
) *InitClusterDEKResponse {
	// Default response
	resp := &InitClusterDEKResponse{
		Version:     s.dekManager.GetDEKVersion(),
		CreatedAt:   time.Now(),
		NodesSynced: 0,
		Algorithm:   req.Algorithm,
		KeySize:     req.KeySize,
	}

	// Update with actual status if available
	if status != nil {
		resp.NodesSynced = s.countSyncedNodes(status)
	}

	return resp
}

// buildStatusResponse builds status response from manager status.
func (s *clusterDEKService) buildStatusResponse(status interface{}) *ClusterDEKStatusResponse {
	// Default response
	currentVersion := s.dekManager.GetDEKVersion()
	resp := &ClusterDEKStatusResponse{
		Initialized:            currentVersion > 0,
		CurrentVersion:         currentVersion,
		CreatedAt:              time.Now(),
		NodesSynced:            0,
		TotalNodes:             0,
		Algorithm:              "AES-256-GCM", // Default
		KeySize:                256,           // Default
		ReencryptionInProgress: false,
		ReencryptionProgress:   0,
	}

	// Parse actual status from manager to populate fields
	if status == nil {
		return resp
	}

	// Try to extract ClusterDEKStatus fields via type assertion
	// The ClusterDEKStatus response may have these fields:
	// Initialized, Version, Algorithm, KeyBytes, CreatedAt, SyncedNodes, Reencryption
	s.parseClusterDEKStatus(status, resp)

	return resp
}

// parseClusterDEKStatus extracts values from the manager's status response.
func (s *clusterDEKService) parseClusterDEKStatus(status interface{}, resp *ClusterDEKStatusResponse) {
	// Try interface-based access for common patterns
	if st, ok := status.(interface{ GetInitialized() bool }); ok {
		resp.Initialized = st.GetInitialized()
	}
	if st, ok := status.(interface{ GetVersion() uint64 }); ok {
		resp.CurrentVersion = st.GetVersion()
	}
	if st, ok := status.(interface{ GetAlgorithm() string }); ok {
		if algo := st.GetAlgorithm(); algo != "" {
			resp.Algorithm = algo
		}
	}
	if st, ok := status.(interface{ GetKeyBytes() int }); ok {
		if keyBytes := st.GetKeyBytes(); keyBytes > 0 {
			resp.KeySize = keyBytes * 8 // Convert bytes to bits
		}
	}
	if st, ok := status.(interface{ GetCreatedAt() time.Time }); ok {
		if created := st.GetCreatedAt(); !created.IsZero() {
			resp.CreatedAt = created
		}
	}

	// Get synced nodes count
	resp.NodesSynced = s.countSyncedNodes(status)

	// Check for reencryption info
	if st, ok := status.(interface{ GetReencryption() interface{} }); ok {
		if reenc := st.GetReencryption(); reenc != nil {
			s.parseReencryptionFromStatus(reenc, resp)
		}
	}

	// Try direct struct field access pattern for ClusterDEKStatus-like types
	type clusterDEKStatusLike struct {
		Initialized  bool
		Version      uint64
		Algorithm    string
		KeyBytes     int
		CreatedAt    time.Time
		SyncedNodes  map[uint64]string
		Reencryption interface{}
	}

	if st, ok := status.(*clusterDEKStatusLike); ok && st != nil {
		resp.Initialized = st.Initialized
		resp.CurrentVersion = st.Version
		if st.Algorithm != "" {
			resp.Algorithm = st.Algorithm
		}
		if st.KeyBytes > 0 {
			resp.KeySize = st.KeyBytes * 8
		}
		if !st.CreatedAt.IsZero() {
			resp.CreatedAt = st.CreatedAt
		}
		resp.NodesSynced = len(st.SyncedNodes)
		if st.Reencryption != nil {
			s.parseReencryptionFromStatus(st.Reencryption, resp)
		}
	}
}

// parseReencryptionFromStatus extracts reencryption info from status.
func (s *clusterDEKService) parseReencryptionFromStatus(reenc interface{}, resp *ClusterDEKStatusResponse) {
	if reenc == nil {
		return
	}

	// Try interface-based access
	if r, ok := reenc.(interface{ GetActive() bool }); ok {
		resp.ReencryptionInProgress = r.GetActive()
	}
	if r, ok := reenc.(interface{ GetProgress() float64 }); ok {
		resp.ReencryptionProgress = r.GetProgress() * 100 // Convert to percentage
	}

	// Try direct struct access for ReencryptionInfo
	type reencryptionInfoLike struct {
		Active   bool
		Progress float64
	}

	if r, ok := reenc.(*reencryptionInfoLike); ok && r != nil {
		resp.ReencryptionInProgress = r.Active
		resp.ReencryptionProgress = r.Progress * 100
	}
}

// buildReencryptionStatusResponse builds re-encryption status response.
func (s *clusterDEKService) buildReencryptionStatusResponse(
	status interface{},
) *ReencryptionStatusResponse {
	// Default response
	resp := &ReencryptionStatusResponse{
		OperationID:       "unknown",
		State:             ReencryptionRunning,
		StartedAt:         time.Now(),
		Progress:          0,
		TotalEntities:     0,
		ProcessedEntities: 0,
		FailedEntities:    0,
		ThroughputRate:    0,
		NodeProgress:      []NodeReencryptionProgress{},
		Errors:            []ReencryptionError{},
	}

	if status == nil {
		return resp
	}

	// Parse actual status from manager to populate fields
	s.parseReencryptionStatus(status, resp)

	return resp
}

// parseReencryptionStatus extracts values from the manager's reencryption status response.
func (s *clusterDEKService) parseReencryptionStatus(status interface{}, resp *ReencryptionStatusResponse) {
	// Try interface-based access for ReencryptionInfo fields
	if st, ok := status.(interface{ GetActive() bool }); ok {
		if st.GetActive() {
			resp.State = ReencryptionRunning
		} else {
			resp.State = ReencryptionCompleted
		}
	}
	if st, ok := status.(interface{ GetStartedAt() time.Time }); ok {
		if started := st.GetStartedAt(); !started.IsZero() {
			resp.StartedAt = started
		}
	}
	if st, ok := status.(interface{ GetTotalKeys() uint64 }); ok {
		//nolint:gosec // G115: TotalKeys is bounded by key count, safe for int64
		resp.TotalEntities = int64(st.GetTotalKeys())
	}
	if st, ok := status.(interface{ GetProcessedKeys() uint64 }); ok {
		resp.ProcessedEntities = int64(st.GetProcessedKeys()) //nolint:gosec // G115: key count bounded
	}
	if st, ok := status.(interface{ GetProgress() float64 }); ok {
		resp.Progress = st.GetProgress() * 100 // Convert to percentage
	}
	if st, ok := status.(interface{ GetOldVersion() uint64 }); ok {
		if oldVer := st.GetOldVersion(); oldVer > 0 {
			resp.OperationID = fmt.Sprintf("reenc-%d-%d", oldVer, oldVer+1)
		}
	}

	// Calculate throughput if we have processed keys and start time
	if resp.ProcessedEntities > 0 && !resp.StartedAt.IsZero() {
		elapsed := time.Since(resp.StartedAt).Seconds()
		if elapsed > 0 {
			resp.ThroughputRate = float64(resp.ProcessedEntities) / elapsed
		}
	}

	// Try direct struct access for ReencryptionInfo
	type reencryptionInfoLike struct {
		Active        bool
		StartedAt     time.Time
		TotalKeys     uint64
		ProcessedKeys uint64
		OldVersion    uint64
		NewVersion    uint64
		Progress      float64
	}

	if r, ok := status.(*reencryptionInfoLike); ok && r != nil {
		if r.Active {
			resp.State = ReencryptionRunning
		} else {
			resp.State = ReencryptionCompleted
		}
		if !r.StartedAt.IsZero() {
			resp.StartedAt = r.StartedAt
		}
		resp.TotalEntities = int64(r.TotalKeys)         //nolint:gosec // G115: key count bounded
		resp.ProcessedEntities = int64(r.ProcessedKeys) //nolint:gosec // G115: key count bounded
		resp.Progress = r.Progress * 100
		if r.OldVersion > 0 {
			resp.OperationID = fmt.Sprintf("reenc-%d-%d", r.OldVersion, r.NewVersion)
		}
		// Recalculate throughput
		if resp.ProcessedEntities > 0 && !resp.StartedAt.IsZero() {
			elapsed := time.Since(resp.StartedAt).Seconds()
			if elapsed > 0 {
				resp.ThroughputRate = float64(resp.ProcessedEntities) / elapsed
			}
		}
	}
}

// countSyncedNodes counts the number of nodes with current DEK.
func (s *clusterDEKService) countSyncedNodes(status interface{}) int {
	if status == nil {
		return 0
	}

	// Try interface-based access for SyncedNodes map
	if st, ok := status.(interface{ GetSyncedNodes() map[uint64]string }); ok {
		return len(st.GetSyncedNodes())
	}

	// Try direct method call
	if st, ok := status.(interface{ SyncedNodes() map[uint64]string }); ok {
		return len(st.SyncedNodes())
	}

	// Try direct struct field access for ClusterDEKStatus
	type statusWithSyncedNodes struct {
		SyncedNodes map[uint64]string
	}

	if st, ok := status.(*statusWithSyncedNodes); ok && st != nil {
		return len(st.SyncedNodes)
	}

	// Try map-based access
	if statusMap, ok := status.(map[string]interface{}); ok {
		if syncedNodes, ok := statusMap["synced_nodes"].(map[uint64]string); ok {
			return len(syncedNodes)
		}
		// Try alternate key name
		if syncedNodes, ok := statusMap["SyncedNodes"].(map[uint64]string); ok {
			return len(syncedNodes)
		}
	}

	return 0
}
