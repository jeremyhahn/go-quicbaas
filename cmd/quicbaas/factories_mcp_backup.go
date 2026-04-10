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

// Package main provides the QuicBaaS command-line interface.
package main

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// mcpBackupClient implements the BackupClient interface using MCP transport.
type mcpBackupClient struct {
	sdk quicbaas.Client
}

// createMCPBackupClient creates an MCP client for backup operations.
func createMCPBackupClient() (BackupClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return &mcpBackupClient{sdk: sdk}, nil
}

// CreateBackup creates a new backup.
func (c *mcpBackupClient) CreateBackup(ctx context.Context, req *BackupCreateRequest) (*cli.BackupCreateResponse, error) {
	args := map[string]interface{}{}
	if req.Name != "" {
		args["name"] = req.Name
	}
	if req.BackupType != "" {
		args["backup_type"] = req.BackupType
	}
	if req.Compression != "" {
		args["compression"] = req.Compression
	}
	if req.Encryption != "" {
		args["encryption"] = req.Encryption
	}
	if req.StoragePath != "" {
		args["storage_path"] = req.StoragePath
	}
	if req.StorageType != "" {
		args["storage_type"] = req.StorageType
	}
	if len(req.Tags) > 0 {
		args["tags"] = req.Tags
	}

	result, err := c.callTool(ctx, "quicbaas_backup_create", args)
	if err != nil {
		return nil, err
	}

	return parseBackupCreateResponse(result)
}

// ListBackups lists available backups.
func (c *mcpBackupClient) ListBackups(ctx context.Context, req *BackupListRequest) (*cli.BackupListResponse, error) {
	args := map[string]interface{}{}
	if req.Limit > 0 {
		args["limit"] = req.Limit
	}
	if req.Offset > 0 {
		args["offset"] = req.Offset
	}
	if req.StoragePath != "" {
		args["storage_path"] = req.StoragePath
	}
	if req.StorageType != "" {
		args["storage_type"] = req.StorageType
	}

	result, err := c.callTool(ctx, "quicbaas_backup_list", args)
	if err != nil {
		return nil, err
	}

	return parseBackupListResponse(result)
}

// RestoreBackup restores a backup.
func (c *mcpBackupClient) RestoreBackup(ctx context.Context, req *BackupRestoreRequest) (*cli.BackupRestoreResponse, error) {
	args := map[string]interface{}{
		"backup_id": req.BackupID,
	}
	if req.StoragePath != "" {
		args["storage_path"] = req.StoragePath
	}
	if req.VerifyFirst {
		args["verify_first"] = req.VerifyFirst
	}
	if req.VerifyChecksum {
		args["verify_checksum"] = req.VerifyChecksum
	}
	if req.SkipExisting {
		args["skip_existing"] = req.SkipExisting
	}
	if req.StorageType != "" {
		args["storage_type"] = req.StorageType
	}
	if req.EncryptionKey != "" {
		args["encryption_key"] = req.EncryptionKey
	}

	result, err := c.callTool(ctx, "quicbaas_backup_restore", args)
	if err != nil {
		return nil, err
	}

	return parseBackupRestoreResponse(result)
}

// DeleteBackup deletes a backup.
func (c *mcpBackupClient) DeleteBackup(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error) {
	args := map[string]interface{}{
		"backup_id": req.BackupID,
	}
	if req.StoragePath != "" {
		args["storage_path"] = req.StoragePath
	}
	if req.Force {
		args["force"] = req.Force
	}

	result, err := c.callTool(ctx, "quicbaas_backup_delete", args)
	if err != nil {
		return nil, err
	}

	return parseBackupDeleteResponse(result)
}

// VerifyBackup verifies a backup.
func (c *mcpBackupClient) VerifyBackup(ctx context.Context, req *BackupVerifyRequest) (*cli.BackupVerifyResponse, error) {
	args := map[string]interface{}{
		"backup_id": req.BackupID,
	}
	if req.StoragePath != "" {
		args["storage_path"] = req.StoragePath
	}
	if req.StorageType != "" {
		args["storage_type"] = req.StorageType
	}
	if req.EncryptionKey != "" {
		args["encryption_key"] = req.EncryptionKey
	}

	result, err := c.callTool(ctx, "quicbaas_backup_verify", args)
	if err != nil {
		return nil, err
	}

	return parseBackupVerifyResponse(result)
}

// Close closes the underlying SDK client.
func (c *mcpBackupClient) Close() error {
	return c.sdk.Close()
}

func (c *mcpBackupClient) callTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	return c.sdk.Transport().Request(ctx, "MCP", toolName, args)
}

// =============================================================================
// Response Parsing Functions
//
// The MCP server marshals service response types via json.Marshal(). The backup
// service types in service.go lack JSON tags, so the JSON output uses PascalCase
// field names (e.g., "BackupID" instead of "backup_id"). These parse functions
// check both snake_case and PascalCase keys to handle responses from any source.
// =============================================================================

// parseBackupCreateResponse parses the MCP response into a BackupCreateResponse.
// Handles both snake_case keys (from tagged types) and PascalCase keys (from
// untagged service.CreateBackupResponse: BackupID, Size, Timestamp, Status).
func parseBackupCreateResponse(result interface{}) (*cli.BackupCreateResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &cli.BackupCreateResponse{
		BackupID:    getStringMulti(data, "backup_id", "BackupID"),
		Type:        getStringMulti(data, "type", "Type"),
		Size:        getInt64Multi(data, "size", "Size"),
		Compression: getStringMulti(data, "compression", "Compression"),
		Encryption:  getStringMulti(data, "encryption", "Encryption"),
		Duration:    parseDuration(getStringMulti(data, "duration", "Duration")),
		Timestamp:   time.Now(),
		Success:     getBoolMulti(data, "success", "Success"),
		Message:     getStringMulti(data, "message", "Message"),
	}

	// If Status is present (PascalCase from service type), map it to the
	// CLI response fields. The service type uses Status as a string field.
	if status := getStringMulti(data, "status", "Status"); status != "" {
		if resp.Message == "" {
			resp.Message = status
		}
		resp.Success = true
	}

	return resp, nil
}

// parseBackupListResponse parses the MCP response into a BackupListResponse.
// Handles both snake_case and PascalCase keys from service.ListBackupsResponse
// (TotalCount, Backups) and service.BackupMetadata (BackupID, Name, Description,
// Size, Timestamp, Status, Shards).
func parseBackupListResponse(result interface{}) (*cli.BackupListResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &cli.BackupListResponse{
		Backups:     make([]cli.BackupInfo, 0),
		TotalCount:  int(getInt64Multi(data, "total_count", "TotalCount")),
		StoragePath: getStringMulti(data, "storage_path", "StoragePath"),
	}

	backupsRaw := getSliceMulti(data, "backups", "Backups")
	for _, bRaw := range backupsRaw {
		bMap, ok := bRaw.(map[string]interface{})
		if !ok {
			continue
		}
		resp.Backups = append(resp.Backups, cli.BackupInfo{
			ID:          getStringMulti(bMap, "id", "ID", "backup_id", "BackupID"),
			Type:        getStringMulti(bMap, "type", "Type"),
			Size:        getInt64Multi(bMap, "size", "Size"),
			Compression: getStringMulti(bMap, "compression", "Compression"),
			Encryption:  getStringMulti(bMap, "encryption", "Encryption"),
			NumShards:   int(getInt64Multi(bMap, "num_shards", "NumShards")),
			NumKeys:     getInt64Multi(bMap, "num_keys", "NumKeys"),
			Timestamp:   parseTimestamp(getInt64Multi(bMap, "timestamp", "Timestamp")),
		})
	}

	return resp, nil
}

// parseBackupRestoreResponse parses the MCP response into a BackupRestoreResponse.
// Handles both snake_case and PascalCase keys from service.RestoreBackupResponse
// (JobID, Status, Progress).
func parseBackupRestoreResponse(result interface{}) (*cli.BackupRestoreResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &cli.BackupRestoreResponse{
		BackupID:       getStringMulti(data, "backup_id", "BackupID", "job_id", "JobID"),
		KeysRestored:   getInt64Multi(data, "keys_restored", "KeysRestored"),
		ShardsRestored: int(getInt64Multi(data, "shards_restored", "ShardsRestored")),
		Duration:       parseDuration(getStringMulti(data, "duration", "Duration")),
		Timestamp:      time.Now(),
		Success:        getBoolMulti(data, "success", "Success"),
		Message:        getStringMulti(data, "message", "Message"),
	}

	// Map service Status field to CLI success/message fields
	if status := getStringMulti(data, "status", "Status"); status != "" {
		if resp.Message == "" {
			resp.Message = status
		}
		resp.Success = true
	}

	return resp, nil
}

// parseBackupDeleteResponse parses the MCP response into a BackupDeleteResponse.
// Handles both snake_case and PascalCase keys from service.DeleteBackupResponse
// (Success, Message).
func parseBackupDeleteResponse(result interface{}) (*cli.BackupDeleteResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &cli.BackupDeleteResponse{
		BackupID:  getStringMulti(data, "backup_id", "BackupID"),
		Deleted:   getBoolMulti(data, "deleted", "Deleted", "success", "Success"),
		Message:   getStringMulti(data, "message", "Message"),
		Timestamp: time.Now(),
	}
	return resp, nil
}

// parseBackupVerifyResponse parses the MCP response into a BackupVerifyResponse.
// Handles both snake_case and PascalCase keys from service.VerifyBackupResponse
// (Valid, ChecksumMatch, Issues).
func parseBackupVerifyResponse(result interface{}) (*cli.BackupVerifyResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &cli.BackupVerifyResponse{
		BackupID:      getStringMulti(data, "backup_id", "BackupID"),
		Valid:         getBoolMulti(data, "valid", "Valid"),
		ChecksumValid: getBoolMulti(data, "checksum_valid", "ChecksumValid", "checksum_match", "ChecksumMatch"),
		ShardsValid:   int(getInt64Multi(data, "shards_valid", "ShardsValid")),
		Timestamp:     time.Now(),
	}

	// Parse issues array from either key
	issuesRaw := getSliceMulti(data, "issues", "Issues")
	for _, issue := range issuesRaw {
		if s, ok := issue.(string); ok {
			resp.Issues = append(resp.Issues, s)
		}
	}

	return resp, nil
}

// Compile-time interface check.
var _ BackupClient = (*mcpBackupClient)(nil)
