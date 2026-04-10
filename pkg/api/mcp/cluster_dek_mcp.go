//go:build mcp

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

// Package mcp implements Cluster DEK MCP integration handlers.
package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// registerClusterDEKTools registers all Cluster DEK tool handlers on the ToolRegistry.
func (r *ToolRegistry) registerClusterDEKTools() {
	clusterDEKTools := getAllClusterDEKTools()
	for _, tool := range clusterDEKTools {
		r.addTool(ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: convertInputSchema(tool.InputSchema),
		})
	}
}

// executeClusterDEKTool executes a Cluster DEK tool and returns the result.
func (r *ToolRegistry) executeClusterDEKTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	clusterDEKService := r.getClusterDEKService()
	if clusterDEKService == nil {
		return nil, api.NewServiceNotConfiguredError("cluster DEK")
	}

	switch name {
	case "quicbaas_cluster_dek_init":
		return r.handleClusterDEKInit(ctx, clusterDEKService, args)
	case "quicbaas_cluster_dek_status":
		return r.handleClusterDEKStatus(ctx, clusterDEKService, args)
	case "quicbaas_cluster_dek_rotate":
		return r.handleClusterDEKRotate(ctx, clusterDEKService, args)
	case "quicbaas_cluster_dek_sync_node":
		return r.handleClusterDEKSyncNode(ctx, clusterDEKService, args)
	case "quicbaas_cluster_dek_node_status":
		return r.handleClusterDEKNodeStatus(ctx, clusterDEKService, args)
	case "quicbaas_cluster_dek_start_reencryption":
		return r.handleClusterDEKStartReencryption(ctx, clusterDEKService, args)
	case "quicbaas_cluster_dek_reencryption_status":
		return r.handleClusterDEKReencryptionStatus(ctx, clusterDEKService, args)
	case "quicbaas_cluster_dek_cancel_reencryption":
		return r.handleClusterDEKCancelReencryption(ctx, clusterDEKService, args)
	case "quicbaas_cluster_dek_recover":
		return r.handleClusterDEKRecover(ctx, clusterDEKService, args)
	default:
		return nil, api.NewUnknownToolError(name)
	}
}

// getClusterDEKService retrieves the ClusterDEKService from the service.
// Note: This needs to be implemented based on how the QuicBaaS service exposes ClusterDEKService.
func (r *ToolRegistry) getClusterDEKService() service.ClusterDEKService {
	// The ClusterDEKService may need to be added to the QuicBaaS interface
	// For now, we check if it's available through type assertion or direct accessor
	if svc, ok := r.service.(interface {
		ClusterDEK() service.ClusterDEKService
	}); ok {
		return svc.ClusterDEK()
	}
	return nil
}

// handleClusterDEKInit handles the quicbaas_cluster_dek_init tool.
func (r *ToolRegistry) handleClusterDEKInit(ctx context.Context, svc service.ClusterDEKService, args map[string]interface{}) (interface{}, error) {
	algorithm := getString(args, "algorithm")
	if algorithm == "" {
		return nil, api.NewRequiredFieldError("algorithm")
	}

	keySize := getInt(args, "key_size")
	if keySize == 0 {
		return nil, api.NewRequiredFieldError("key_size")
	}

	req := &service.InitClusterDEKRequest{
		Algorithm: algorithm,
		KeySize:   keySize,
	}

	// Parse optional rotation policy
	rotationEnabled := getString(args, "rotation_enabled") == "true"
	if rotationEnabled {
		policy := &service.DEKRotationPolicy{
			Enabled: true,
		}

		if intervalStr := getString(args, "rotation_interval"); intervalStr != "" {
			interval, err := time.ParseDuration(intervalStr)
			if err != nil {
				return nil, api.NewInvalidFormatError("rotation_interval", err)
			}
			policy.RotationInterval = interval
		}

		if autoReencrypt := getString(args, "auto_reencrypt"); autoReencrypt == "true" {
			policy.AutoReencrypt = true
		}

		req.RotationPolicy = policy
	}

	resp, err := svc.InitializeClusterDEK(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("init cluster DEK", err)
	}

	return map[string]interface{}{
		"version":      resp.Version,
		"created_at":   resp.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"nodes_synced": resp.NodesSynced,
		"algorithm":    resp.Algorithm,
		"key_size":     resp.KeySize,
	}, nil
}

// handleClusterDEKStatus handles the quicbaas_cluster_dek_status tool.
func (r *ToolRegistry) handleClusterDEKStatus(ctx context.Context, svc service.ClusterDEKService, args map[string]interface{}) (interface{}, error) {
	resp, err := svc.GetClusterDEKStatus(ctx)
	if err != nil {
		return nil, api.NewOperationFailedError("get cluster DEK status", err)
	}

	result := map[string]interface{}{
		"initialized":              resp.Initialized,
		"current_version":          resp.CurrentVersion,
		"created_at":               resp.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"nodes_synced":             resp.NodesSynced,
		"total_nodes":              resp.TotalNodes,
		"algorithm":                resp.Algorithm,
		"key_size":                 resp.KeySize,
		"reencryption_in_progress": resp.ReencryptionInProgress,
		"reencryption_progress":    resp.ReencryptionProgress,
	}

	if resp.RotatedAt != nil {
		result["rotated_at"] = resp.RotatedAt.Format("2006-01-02T15:04:05Z07:00")
	}

	if resp.NextRotation != nil {
		result["next_rotation"] = resp.NextRotation.Format("2006-01-02T15:04:05Z07:00")
	}

	if len(resp.NodesOutOfSync) > 0 {
		nodesOutOfSync := make([]map[string]interface{}, 0, len(resp.NodesOutOfSync))
		for _, node := range resp.NodesOutOfSync {
			nodesOutOfSync = append(nodesOutOfSync, map[string]interface{}{
				"node_id":         node.NodeID,
				"node_name":       node.NodeName,
				"current_version": node.CurrentVersion,
				"last_synced_at":  node.LastSyncedAt.Format("2006-01-02T15:04:05Z07:00"),
			})
		}
		result["nodes_out_of_sync"] = nodesOutOfSync
	}

	if len(resp.PreviousVersions) > 0 {
		previousVersions := make([]map[string]interface{}, 0, len(resp.PreviousVersions))
		for _, ver := range resp.PreviousVersions {
			verMap := map[string]interface{}{
				"version":            ver.Version,
				"created_at":         ver.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
				"entities_encrypted": ver.EntitiesEncrypted,
				"can_delete":         ver.CanDelete,
			}
			if ver.DeprecatedAt != nil {
				verMap["deprecated_at"] = ver.DeprecatedAt.Format("2006-01-02T15:04:05Z07:00")
			}
			previousVersions = append(previousVersions, verMap)
		}
		result["previous_versions"] = previousVersions
	}

	return result, nil
}

// handleClusterDEKRotate handles the quicbaas_cluster_dek_rotate tool.
func (r *ToolRegistry) handleClusterDEKRotate(ctx context.Context, svc service.ClusterDEKService, args map[string]interface{}) (interface{}, error) {
	reason := getString(args, "reason")
	if reason == "" {
		return nil, api.NewRequiredFieldError("reason")
	}

	req := &service.RotateClusterDEKRequest{
		Reason: reason,
	}

	// Parse optional scheduled time
	if scheduledTimeStr := getString(args, "scheduled_time"); scheduledTimeStr != "" {
		scheduledTime, err := time.Parse(time.RFC3339, scheduledTimeStr)
		if err != nil {
			return nil, api.NewInvalidFormatError("scheduled_time", err)
		}
		req.ScheduledTime = &scheduledTime
	}

	// Parse boolean flags
	if forceRotation := getString(args, "force_rotation"); forceRotation == "true" {
		req.ForceRotation = true
	}
	if autoStart := getString(args, "auto_start_reencryption"); autoStart == "true" {
		req.AutoStartReencryption = true
	}

	resp, err := svc.RotateClusterDEK(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("rotate cluster DEK", err)
	}

	result := map[string]interface{}{
		"new_version":          resp.NewVersion,
		"old_version":          resp.OldVersion,
		"rotated_at":           resp.RotatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"rotation_id":          resp.RotationID,
		"nodes_synced":         resp.NodesSynced,
		"reencryption_started": resp.ReencryptionStarted,
	}

	if resp.EstimatedReencryptionTime > 0 {
		result["estimated_reencryption_time"] = resp.EstimatedReencryptionTime.String()
	}

	return result, nil
}

// handleClusterDEKSyncNode handles the quicbaas_cluster_dek_sync_node tool.
func (r *ToolRegistry) handleClusterDEKSyncNode(ctx context.Context, svc service.ClusterDEKService, args map[string]interface{}) (interface{}, error) {
	nodeID := getUint64(args, "node_id")
	if nodeID == 0 {
		return nil, api.NewRequiredFieldError("node_id")
	}

	req := &service.SyncNodeDEKRequest{
		NodeID:         nodeID,
		CurrentVersion: getUint64(args, "current_version"),
	}

	// Parse force resync flag
	if forceResync := getString(args, "force_resync"); forceResync == "true" {
		req.ForceResync = true
	}

	resp, err := svc.SyncNodeDEK(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("sync node DEK", err)
	}

	return map[string]interface{}{
		"node_id":         resp.NodeID,
		"versions_synced": resp.VersionsSynced,
		"current_version": resp.CurrentVersion,
		"synced_at":       resp.SyncedAt.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

// handleClusterDEKNodeStatus handles the quicbaas_cluster_dek_node_status tool.
func (r *ToolRegistry) handleClusterDEKNodeStatus(ctx context.Context, svc service.ClusterDEKService, args map[string]interface{}) (interface{}, error) {
	nodeID := getUint64(args, "node_id")
	if nodeID == 0 {
		return nil, api.NewRequiredFieldError("node_id")
	}

	req := &service.GetNodeDEKStatusRequest{
		NodeID: nodeID,
	}

	resp, err := svc.GetNodeDEKStatus(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("get node DEK status", err)
	}

	result := map[string]interface{}{
		"node_id":            resp.NodeID,
		"current_version":    resp.CurrentVersion,
		"last_synced_at":     resp.LastSyncedAt.Format("2006-01-02T15:04:05Z07:00"),
		"status":             string(resp.Status),
		"versions_available": resp.VersionsAvailable,
	}

	if resp.NodeName != "" {
		result["node_name"] = resp.NodeName
	}
	if resp.Error != "" {
		result["error"] = resp.Error
	}

	return result, nil
}

// handleClusterDEKStartReencryption handles the quicbaas_cluster_dek_start_reencryption tool.
func (r *ToolRegistry) handleClusterDEKStartReencryption(ctx context.Context, svc service.ClusterDEKService, args map[string]interface{}) (interface{}, error) {
	fromVersion := getUint64(args, "from_version")
	if fromVersion == 0 {
		return nil, api.NewRequiredFieldError("from_version")
	}

	toVersion := getUint64(args, "to_version")
	if toVersion == 0 {
		return nil, api.NewRequiredFieldError("to_version")
	}

	req := &service.StartReencryptionRequest{
		Path:         getString(args, "path"),
		FromVersion:  fromVersion,
		ToVersion:    toVersion,
		BatchSize:    getInt(args, "batch_size"),
		Concurrency:  getInt(args, "concurrency"),
		ThrottleRate: getInt(args, "throttle_rate"),
	}

	// Parse force flag
	if force := getString(args, "force"); force == "true" {
		req.Force = true
	}

	resp, err := svc.StartReencryption(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("start reencryption", err)
	}

	result := map[string]interface{}{
		"operation_id":         resp.OperationID,
		"started":              resp.Started,
		"estimated_items":      resp.EstimatedItems,
		"timestamp":            resp.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
		"started_at":           resp.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
		"total_entities":       resp.TotalEntities,
		"estimated_completion": resp.EstimatedCompletion.Format("2006-01-02T15:04:05Z07:00"),
	}

	if resp.Path != "" {
		result["path"] = resp.Path
	}

	return result, nil
}

// handleClusterDEKReencryptionStatus handles the quicbaas_cluster_dek_reencryption_status tool.
func (r *ToolRegistry) handleClusterDEKReencryptionStatus(ctx context.Context, svc service.ClusterDEKService, args map[string]interface{}) (interface{}, error) {
	resp, err := svc.GetReencryptionStatus(ctx)
	if err != nil {
		return nil, api.NewOperationFailedError("get reencryption status", err)
	}

	result := map[string]interface{}{
		"operation_id":       resp.OperationID,
		"state":              string(resp.State),
		"started_at":         resp.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
		"progress":           resp.Progress,
		"total_entities":     resp.TotalEntities,
		"processed_entities": resp.ProcessedEntities,
		"failed_entities":    resp.FailedEntities,
		"throughput_rate":    resp.ThroughputRate,
	}

	if resp.CompletedAt != nil {
		result["completed_at"] = resp.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	if resp.EstimatedCompletion != nil {
		result["estimated_completion"] = resp.EstimatedCompletion.Format("2006-01-02T15:04:05Z07:00")
	}

	if len(resp.NodeProgress) > 0 {
		nodeProgress := make([]map[string]interface{}, 0, len(resp.NodeProgress))
		for _, np := range resp.NodeProgress {
			nodeProgress = append(nodeProgress, map[string]interface{}{
				"node_id":            np.NodeID,
				"node_name":          np.NodeName,
				"progress":           np.Progress,
				"processed_entities": np.ProcessedEntities,
				"total_entities":     np.TotalEntities,
				"throughput_rate":    np.ThroughputRate,
				"last_updated_at":    np.LastUpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
			})
		}
		result["node_progress"] = nodeProgress
	}

	if len(resp.Errors) > 0 {
		errors := make([]map[string]interface{}, 0, len(resp.Errors))
		for _, e := range resp.Errors {
			errors = append(errors, map[string]interface{}{
				"timestamp": e.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
				"node_id":   e.NodeID,
				"entity_id": e.EntityID,
				"error":     e.Error,
				"retried":   e.Retried,
			})
		}
		result["errors"] = errors
	}

	return result, nil
}

// handleClusterDEKCancelReencryption handles the quicbaas_cluster_dek_cancel_reencryption tool.
func (r *ToolRegistry) handleClusterDEKCancelReencryption(ctx context.Context, svc service.ClusterDEKService, args map[string]interface{}) (interface{}, error) {
	err := svc.CancelReencryption(ctx)
	if err != nil {
		return nil, api.NewOperationFailedError("cancel reencryption", err)
	}

	return map[string]interface{}{
		"cancelled":    true,
		"cancelled_at": time.Now().Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

// handleClusterDEKRecover handles the quicbaas_cluster_dek_recover tool.
func (r *ToolRegistry) handleClusterDEKRecover(ctx context.Context, svc service.ClusterDEKService, args map[string]interface{}) (interface{}, error) {
	recoveryMethod := getString(args, "recovery_method")
	if recoveryMethod == "" {
		return nil, api.NewRequiredFieldError("recovery_method")
	}

	reason := getString(args, "reason")
	if reason == "" {
		return nil, api.NewRequiredFieldError("reason")
	}

	req := &service.RecoverDEKRequest{
		RecoveryMethod: service.RecoveryMethod(recoveryMethod),
		Reason:         reason,
		BackupLocation: getString(args, "backup_location"),
	}

	// Parse recovery keys if provided
	if recoveryKeysJSON := getString(args, "recovery_keys"); recoveryKeysJSON != "" {
		var keysBase64 []string
		if err := json.Unmarshal([]byte(recoveryKeysJSON), &keysBase64); err != nil {
			return nil, api.NewInvalidFormatError("recovery_keys JSON", err)
		}
		req.RecoveryKeys = make([][]byte, 0, len(keysBase64))
		for _, keyB64 := range keysBase64 {
			keyBytes, err := base64.StdEncoding.DecodeString(keyB64)
			if err != nil {
				return nil, api.NewInvalidBase64Error("recovery key", err)
			}
			req.RecoveryKeys = append(req.RecoveryKeys, keyBytes)
		}
	}

	// Parse manual key if provided
	if manualKeyB64 := getString(args, "manual_key"); manualKeyB64 != "" {
		manualKey, err := base64.StdEncoding.DecodeString(manualKeyB64)
		if err != nil {
			return nil, api.NewInvalidBase64Error("manual key", err)
		}
		req.ManualKey = manualKey
	}

	resp, err := svc.RecoverDEK(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("recover DEK", err)
	}

	result := map[string]interface{}{
		"success":           resp.Success,
		"recovered_version": resp.RecoveredVersion,
		"recovered_at":      resp.RecoveredAt.Format("2006-01-02T15:04:05Z07:00"),
		"nodes_synced":      resp.NodesSynced,
		"audit_id":          resp.AuditID,
	}

	if len(resp.Warnings) > 0 {
		result["warnings"] = resp.Warnings
	}

	return result, nil
}

// isClusterDEKTool returns true if the tool name is a Cluster DEK tool.
func isClusterDEKTool(name string) bool {
	switch name {
	case "quicbaas_cluster_dek_init",
		"quicbaas_cluster_dek_status",
		"quicbaas_cluster_dek_rotate",
		"quicbaas_cluster_dek_sync_node",
		"quicbaas_cluster_dek_node_status",
		"quicbaas_cluster_dek_start_reencryption",
		"quicbaas_cluster_dek_reencryption_status",
		"quicbaas_cluster_dek_cancel_reencryption",
		"quicbaas_cluster_dek_recover":
		return true
	default:
		return false
	}
}
