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
	"encoding/json"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// =============================================================================
// MCP KMS Client
// =============================================================================

// mcpKMSClient implements the KMSClient interface using MCP transport.
type mcpKMSClient struct {
	sdk quicbaas.Client
}

// createMCPKMSClient creates an MCP client for KMS operations.
func createMCPKMSClient() (KMSClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return &mcpKMSClient{sdk: sdk}, nil
}

// KMSGenerateKey generates a new encryption key.
func (c *mcpKMSClient) KMSGenerateKey(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
	args := map[string]interface{}{}
	if req.KeyAttributes != nil {
		if req.KeyAttributes.SymmetricAlgorithm != "" {
			args["algorithm"] = string(req.KeyAttributes.SymmetricAlgorithm)
		} else {
			args["algorithm"] = req.KeyAttributes.KeyAlgorithm.String()
		}
	}
	if req.Name != "" {
		args["name"] = req.Name
	}
	if req.Description != "" {
		args["description"] = req.Description
	}
	if req.DeletionProtection {
		args["deletion_protection"] = req.DeletionProtection
	}
	if len(req.Usage) > 0 {
		usages := make([]string, len(req.Usage))
		for i, u := range req.Usage {
			usages[i] = string(u)
		}
		args["usage"] = usages
	}
	if len(req.Tags) > 0 {
		args["tags"] = req.Tags
	}

	result, err := c.callTool(ctx, "quicbaas_kms_generate_key", args)
	if err != nil {
		return nil, err
	}

	return parseKMSGenerateKeyResponse(result)
}

// KMSImportKey imports an existing key.
func (c *mcpKMSClient) KMSImportKey(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
	args := map[string]interface{}{
		"key_material": string(req.KeyMaterial),
	}
	if req.WrappingKeyID != "" {
		args["wrapping_key_id"] = req.WrappingKeyID
	}
	if req.Name != "" {
		args["name"] = req.Name
	}
	if req.Description != "" {
		args["description"] = req.Description
	}
	if req.KeyAttributes != nil {
		if req.KeyAttributes.SymmetricAlgorithm != "" {
			args["algorithm"] = string(req.KeyAttributes.SymmetricAlgorithm)
		} else {
			args["algorithm"] = req.KeyAttributes.KeyAlgorithm.String()
		}
		if req.KeyAttributes.Exportable {
			args["exportable"] = true
		}
	}

	result, err := c.callTool(ctx, "quicbaas_kms_import_key", args)
	if err != nil {
		return nil, err
	}

	return parseKMSImportKeyResponse(result)
}

// KMSGetKey retrieves key metadata.
func (c *mcpKMSClient) KMSGetKey(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
	args := map[string]interface{}{
		"key_id": req.KeyID,
	}
	if req.Version > 0 {
		args["version"] = req.Version
	}

	result, err := c.callTool(ctx, "quicbaas_kms_get_key", args)
	if err != nil {
		return nil, err
	}

	return parseKMSGetKeyResponse(result)
}

// KMSListKeys lists available keys.
func (c *mcpKMSClient) KMSListKeys(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
	args := map[string]interface{}{}
	if req.Limit > 0 {
		args["limit"] = req.Limit
	}
	if req.Cursor != "" {
		args["cursor"] = req.Cursor
	}
	if len(req.Filter) > 0 {
		args["filter"] = req.Filter
	}

	result, err := c.callTool(ctx, "quicbaas_kms_list_keys", args)
	if err != nil {
		return nil, err
	}

	return parseKMSListKeysResponse(result)
}

// KMSRotateKey rotates a key to a new version.
func (c *mcpKMSClient) KMSRotateKey(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
	args := map[string]interface{}{
		"key_id": req.KeyID,
	}

	result, err := c.callTool(ctx, "quicbaas_kms_rotate_key", args)
	if err != nil {
		return nil, err
	}

	return parseKMSRotateKeyResponse(result)
}

// KMSDeleteKey deletes a key.
func (c *mcpKMSClient) KMSDeleteKey(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
	args := map[string]interface{}{
		"key_id": req.KeyID,
	}
	if req.PendingWindowDays > 0 {
		args["pending_window_days"] = req.PendingWindowDays
	}
	if req.Force {
		args["force"] = req.Force
	}

	result, err := c.callTool(ctx, "quicbaas_kms_delete_key", args)
	if err != nil {
		return nil, err
	}

	return parseKMSDeleteKeyResponse(result)
}

// KMSUpdateKeyStatus updates the status of a key.
func (c *mcpKMSClient) KMSUpdateKeyStatus(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
	args := map[string]interface{}{
		"key_id": req.KeyID,
		"status": string(req.Status),
	}
	if req.Version > 0 {
		args["version"] = req.Version
	}
	if req.Reason != "" {
		args["reason"] = req.Reason
	}

	result, err := c.callTool(ctx, "quicbaas_kms_key_status_update", args)
	if err != nil {
		return nil, err
	}

	return parseKMSUpdateKeyStatusResponse(result)
}

// KMSListKeyVersions lists all versions of a key.
func (c *mcpKMSClient) KMSListKeyVersions(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
	args := map[string]interface{}{
		"key_id": req.KeyID,
	}
	if req.Limit > 0 {
		args["limit"] = req.Limit
	}
	if req.Cursor != "" {
		args["cursor"] = req.Cursor
	}

	result, err := c.callTool(ctx, "quicbaas_kms_key_versions", args)
	if err != nil {
		return nil, err
	}

	return parseKMSListKeyVersionsResponse(result)
}

// KMSExportKey exports a key in wrapped form.
func (c *mcpKMSClient) KMSExportKey(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
	args := map[string]interface{}{
		"key_id": req.KeyID,
	}
	if req.WrappingKeyID != "" {
		args["wrapping_key_id"] = req.WrappingKeyID
	}
	if req.WrappingAlgorithm != "" {
		args["wrapping_algorithm"] = req.WrappingAlgorithm
	}
	if req.Format != "" {
		args["format"] = req.Format
	}

	result, err := c.callTool(ctx, "quicbaas_kms_export_key", args)
	if err != nil {
		return nil, err
	}

	return parseKMSExportKeyResponse(result)
}

// KMSUnwrapKey unwraps a wrapped key.
func (c *mcpKMSClient) KMSUnwrapKey(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	args := map[string]interface{}{
		"key_id":             req.KeyID,
		"wrapped_key":        string(req.WrappedKey),
		"wrapping_key_id":    req.WrappingKeyID,
		"wrapping_algorithm": req.WrappingAlgorithm,
		"algorithm":          req.Algorithm,
	}
	if req.SourceKeyID != "" {
		args["source_key_id"] = req.SourceKeyID
	}
	if req.Name != "" {
		args["name"] = req.Name
	}
	if req.KeyAttributes != nil && req.KeyAttributes.Exportable {
		args["exportable"] = true
	}

	result, err := c.callTool(ctx, "quicbaas_kms_unwrap_key", args)
	if err != nil {
		return nil, err
	}

	return parseKMSUnwrapKeyResponse(result)
}

// KMSSetKeySafety sets safety limits on a key.
func (c *mcpKMSClient) KMSSetKeySafety(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
	args := map[string]interface{}{
		"key_id": req.KeyID,
	}
	if req.MaxOperations > 0 {
		args["max_operations"] = req.MaxOperations
	}
	if req.MaxBytes > 0 {
		args["max_bytes"] = req.MaxBytes
	}
	if req.WarningPercent > 0 {
		args["warning_percent"] = req.WarningPercent
	}
	if req.AutoRotatePercent > 0 {
		args["auto_rotate_percent"] = req.AutoRotatePercent
	}

	result, err := c.callTool(ctx, "quicbaas_kms_set_key_safety", args)
	if err != nil {
		return nil, err
	}

	return parseKMSSetKeySafetyResponse(result)
}

// KMSClearKeySafety clears safety limits on a key.
func (c *mcpKMSClient) KMSClearKeySafety(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
	args := map[string]interface{}{
		"key_id": req.KeyID,
	}

	result, err := c.callTool(ctx, "quicbaas_kms_clear_key_safety", args)
	if err != nil {
		return nil, err
	}

	return parseKMSClearKeySafetyResponse(result)
}

// KMSEncrypt encrypts plaintext using the specified key.
func (c *mcpKMSClient) KMSEncrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	args := map[string]interface{}{
		"key_id":    req.KeyID,
		"plaintext": string(req.Plaintext),
	}
	if len(req.Context) > 0 {
		args["context"] = req.Context
	}

	result, err := c.callTool(ctx, "quicbaas_kms_encrypt", args)
	if err != nil {
		return nil, err
	}

	return parseKMSEncryptResponse(result)
}

// KMSDecrypt decrypts ciphertext using the specified key.
func (c *mcpKMSClient) KMSDecrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	args := map[string]interface{}{
		"key_id":     req.KeyID,
		"ciphertext": string(req.Ciphertext),
	}
	if len(req.Context) > 0 {
		args["context"] = req.Context
	}

	result, err := c.callTool(ctx, "quicbaas_kms_decrypt", args)
	if err != nil {
		return nil, err
	}

	return parseKMSDecryptResponse(result)
}

// Close closes the underlying SDK client.
func (c *mcpKMSClient) Close() error {
	return c.sdk.Close()
}

func (c *mcpKMSClient) callTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	return c.sdk.Transport().Request(ctx, "MCP", toolName, args)
}

// =============================================================================
// MCP Backend Client
// =============================================================================

// mcpBackendClient implements the BackendClient interface using MCP transport.
type mcpBackendClient struct {
	sdk quicbaas.Client
}

// createMCPBackendClient creates an MCP client for backend operations.
func createMCPBackendClient() (BackendClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return &mcpBackendClient{sdk: sdk}, nil
}

// KMSBackendStatus returns the backend status.
func (c *mcpBackendClient) KMSBackendStatus(ctx context.Context) (*service.BackendStatusResponse, error) {
	result, err := c.callTool(ctx, "quicbaas_kms_backend_status", nil)
	if err != nil {
		return nil, err
	}

	return parseBackendStatusResponse(result)
}

// KMSBackendTest runs a backend test.
func (c *mcpBackendClient) KMSBackendTest(ctx context.Context) (*service.BackendTestResponse, error) {
	result, err := c.callTool(ctx, "quicbaas_kms_backend_test", nil)
	if err != nil {
		return nil, err
	}

	return parseBackendTestResponse(result)
}

// Close closes the underlying SDK client.
func (c *mcpBackendClient) Close() error {
	return c.sdk.Close()
}

func (c *mcpBackendClient) callTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	return c.sdk.Transport().Request(ctx, "MCP", toolName, args)
}

// =============================================================================
// MCP Safety Client
// =============================================================================

// mcpSafetyClient implements the SafetyClient interface using MCP transport.
type mcpSafetyClient struct {
	sdk quicbaas.Client
}

// createMCPSafetyClient creates an MCP client for safety operations.
func createMCPSafetyClient() (SafetyClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return &mcpSafetyClient{sdk: sdk}, nil
}

// KMSSafetyCheck runs a safety check against usage thresholds.
func (c *mcpSafetyClient) KMSSafetyCheck(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
	args := map[string]interface{}{}
	if req.ThresholdPercent > 0 {
		args["threshold_percent"] = req.ThresholdPercent
	}

	result, err := c.callTool(ctx, "quicbaas_kms_safety_check", args)
	if err != nil {
		return nil, err
	}

	return parseSafetyCheckResponse(result)
}

// KMSSafetyReport generates a safety report.
func (c *mcpSafetyClient) KMSSafetyReport(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
	args := map[string]interface{}{}
	if req.IncludeDetails {
		args["include_details"] = req.IncludeDetails
	}

	result, err := c.callTool(ctx, "quicbaas_kms_safety_report", args)
	if err != nil {
		return nil, err
	}

	return parseSafetyReportResponse(result)
}

// Close closes the underlying SDK client.
func (c *mcpSafetyClient) Close() error {
	return c.sdk.Close()
}

func (c *mcpSafetyClient) callTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	return c.sdk.Transport().Request(ctx, "MCP", toolName, args)
}

// =============================================================================
// KMS Response Parsing Functions
// =============================================================================

// parseKMSGenerateKeyResponse parses the MCP response into a GenerateKeyResponse.
func parseKMSGenerateKeyResponse(result interface{}) (*service.GenerateKeyResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.GenerateKeyResponse{
		Key: &service.KeyHandle{
			KeyID:     getString(data, "key_id"),
			Algorithm: service.KeyAlgorithm(getString(data, "algorithm")),
			Status:    service.KeyStatus(getString(data, "status")),
			Version:   uint64(getInt64(data, "version")),
			CreatedAt: parseTimestamp(getInt64(data, "created_at")),
		},
		Timestamp: time.Now(),
	}
	return resp, nil
}

// parseKMSImportKeyResponse parses the MCP response into an ImportKeyResponse.
func parseKMSImportKeyResponse(result interface{}) (*service.ImportKeyResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.ImportKeyResponse{
		Key: &service.KeyHandle{
			KeyID:     getString(data, "key_id"),
			Algorithm: service.KeyAlgorithm(getString(data, "algorithm")),
			Status:    service.KeyStatus(getString(data, "status")),
			Version:   uint64(getInt64(data, "version")),
			CreatedAt: parseTimestamp(getInt64(data, "created_at")),
		},
		Timestamp: time.Now(),
	}
	return resp, nil
}

// parseKMSGetKeyResponse parses the MCP response into a GetKeyResponse.
func parseKMSGetKeyResponse(result interface{}) (*service.GetKeyResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	// Check for nested metadata
	keyData := data
	if metaMap := getMap(data, "metadata"); metaMap != nil {
		keyData = metaMap
	}

	resp := &service.GetKeyResponse{
		Key: &service.KeyHandle{
			KeyID:     getString(keyData, "key_id"),
			Algorithm: service.KeyAlgorithm(getString(keyData, "algorithm")),
			Status:    service.KeyStatus(getString(keyData, "status")),
			Version:   uint64(getInt64(keyData, "version")),
			CreatedAt: parseTimestamp(getInt64(keyData, "created_at")),
		},
		Metadata: &service.KeyMetadata{
			KeyID:     getString(keyData, "key_id"),
			Algorithm: service.KeyAlgorithm(getString(keyData, "algorithm")),
			Status:    service.KeyStatus(getString(keyData, "status")),
			Version:   uint64(getInt64(keyData, "version")),
			Name:      getString(keyData, "name"),
		},
		Timestamp: time.Now(),
	}
	return resp, nil
}

// parseKMSListKeysResponse parses the MCP response into a ListKeysResponse.
func parseKMSListKeysResponse(result interface{}) (*service.ListKeysResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.ListKeysResponse{
		Keys:       make([]*service.KeyHandle, 0),
		NextCursor: getString(data, "next_cursor"),
		HasMore:    getBool(data, "has_more"),
	}

	keysRaw, ok := data["keys"].([]interface{})
	if ok {
		for _, kRaw := range keysRaw {
			kMap, ok := kRaw.(map[string]interface{})
			if !ok {
				continue
			}
			resp.Keys = append(resp.Keys, &service.KeyHandle{
				KeyID:     getString(kMap, "key_id"),
				Algorithm: service.KeyAlgorithm(getString(kMap, "algorithm")),
				Status:    service.KeyStatus(getString(kMap, "status")),
				Version:   uint64(getInt64(kMap, "version")),
				CreatedAt: parseTimestamp(getInt64(kMap, "created_at")),
			})
		}
	}

	return resp, nil
}

// parseKMSRotateKeyResponse parses the MCP response into a RotateKeyResponse.
func parseKMSRotateKeyResponse(result interface{}) (*service.RotateKeyResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.RotateKeyResponse{
		KeyID:      getString(data, "key_id"),
		OldVersion: uint64(getInt64(data, "old_version")),
		NewVersion: uint64(getInt64(data, "new_version")),
		Timestamp:  time.Now(),
	}
	return resp, nil
}

// parseKMSDeleteKeyResponse parses the MCP response into a DeleteKeyResponse.
func parseKMSDeleteKeyResponse(result interface{}) (*service.DeleteKeyResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.DeleteKeyResponse{
		KeyID:     getString(data, "key_id"),
		Status:    service.KeyStatus(getString(data, "status")),
		Timestamp: time.Now(),
	}
	return resp, nil
}

// parseKMSUpdateKeyStatusResponse parses the MCP response into an UpdateKeyStatusResponse.
func parseKMSUpdateKeyStatusResponse(result interface{}) (*service.UpdateKeyStatusResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.UpdateKeyStatusResponse{
		KeyID:     getString(data, "key_id"),
		Version:   uint64(getInt64(data, "version")),
		OldStatus: service.KeyStatus(getString(data, "old_status")),
		NewStatus: service.KeyStatus(getString(data, "new_status")),
		Status:    service.KeyStatus(getString(data, "status")),
		Timestamp: time.Now(),
	}
	return resp, nil
}

// parseKMSListKeyVersionsResponse parses the MCP response into a ListKeyVersionsResponse.
func parseKMSListKeyVersionsResponse(result interface{}) (*service.ListKeyVersionsResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.ListKeyVersionsResponse{
		KeyID:      getString(data, "key_id"),
		Versions:   make([]*service.KeyVersionInfo, 0),
		NextCursor: getString(data, "next_cursor"),
		HasMore:    getBool(data, "has_more"),
		Timestamp:  time.Now(),
	}

	versionsRaw, ok := data["versions"].([]interface{})
	if ok {
		for _, vRaw := range versionsRaw {
			vMap, ok := vRaw.(map[string]interface{})
			if !ok {
				continue
			}
			resp.Versions = append(resp.Versions, &service.KeyVersionInfo{
				Version:   uint64(getInt64(vMap, "version")),
				Status:    service.KeyStatus(getString(vMap, "status")),
				Algorithm: service.KeyAlgorithm(getString(vMap, "algorithm")),
				CreatedAt: parseTimestamp(getInt64(vMap, "created_at")),
			})
		}
	}

	return resp, nil
}

// parseKMSEncryptResponse parses the MCP response into an EncryptResponse.
func parseKMSEncryptResponse(result interface{}) (*service.EncryptResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.EncryptResponse{
		KeyID:      getString(data, "key_id"),
		KeyVersion: uint64(getInt64(data, "key_version")),
		Ciphertext: getBytes(data, "ciphertext"),
	}
	return resp, nil
}

// parseKMSDecryptResponse parses the MCP response into a DecryptResponse.
func parseKMSDecryptResponse(result interface{}) (*service.DecryptResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.DecryptResponse{
		KeyID:      getString(data, "key_id"),
		KeyVersion: uint64(getInt64(data, "key_version")),
		Plaintext:  getBytes(data, "plaintext"),
	}
	return resp, nil
}

// parseKMSExportKeyResponse parses the MCP response into an ExportKeyResponse.
func parseKMSExportKeyResponse(result interface{}) (*service.ExportKeyResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.ExportKeyResponse{
		KeyID:             getString(data, "key_id"),
		Version:           uint64(getInt64(data, "version")),
		WrappedKey:        getBytes(data, "wrapped_key"),
		WrappingKeyID:     getString(data, "wrapping_key_id"),
		WrappingAlgorithm: getString(data, "wrapping_algorithm"),
		Format:            getString(data, "format"),
		Timestamp:         time.Now(),
	}
	return resp, nil
}

// parseKMSUnwrapKeyResponse parses the MCP response into an UnwrapKeyResponse.
func parseKMSUnwrapKeyResponse(result interface{}) (*service.UnwrapKeyResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.UnwrapKeyResponse{
		KeyID:     getString(data, "key_id"),
		Version:   uint64(getInt64(data, "version")),
		Algorithm: service.KeyAlgorithm(getString(data, "algorithm")),
		Status:    service.KeyStatus(getString(data, "status")),
		Timestamp: time.Now(),
	}
	return resp, nil
}

// parseKMSSetKeySafetyResponse parses the MCP response into a SetKeySafetyResponse.
func parseKMSSetKeySafetyResponse(result interface{}) (*service.SetKeySafetyResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.SetKeySafetyResponse{
		KeyID:     getString(data, "key_id"),
		Timestamp: time.Now(),
	}

	if configMap := getMap(data, "config"); configMap != nil {
		resp.Config = service.AEADSafetyConfigDTO{
			MaxOperations:     uint64(getInt64(configMap, "max_operations")),
			MaxBytes:          uint64(getInt64(configMap, "max_bytes")),
			WarningPercent:    int(getInt64(configMap, "warning_percent")),
			AutoRotatePercent: int(getInt64(configMap, "auto_rotate_percent")),
		}
	}

	return resp, nil
}

// parseKMSClearKeySafetyResponse parses the MCP response into a ClearKeySafetyResponse.
func parseKMSClearKeySafetyResponse(result interface{}) (*service.ClearKeySafetyResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.ClearKeySafetyResponse{
		KeyID:     getString(data, "key_id"),
		Timestamp: time.Now(),
	}
	return resp, nil
}

// =============================================================================
// DEK Response Parsing Functions
// =============================================================================

// =============================================================================
// Backend & Safety Response Parsing Functions
// =============================================================================

// parseBackendStatusResponse parses the MCP response into a BackendStatusResponse.
func parseBackendStatusResponse(result interface{}) (*service.BackendStatusResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.BackendStatusResponse{
		BackendType: getString(data, "backend_type"),
		Status:      getString(data, "status"),
		Device:      getString(data, "device"),
		Region:      getString(data, "region"),
		KeyID:       getString(data, "key_id"),
		Timestamp:   time.Now(),
	}

	if capsMap := getMap(data, "capabilities"); capsMap != nil {
		resp.Capabilities = make(map[string]bool)
		for k, v := range capsMap {
			if b, ok := v.(bool); ok {
				resp.Capabilities[k] = b
			}
		}
	}

	return resp, nil
}

// parseBackendTestResponse parses the MCP response into a BackendTestResponse.
func parseBackendTestResponse(result interface{}) (*service.BackendTestResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.BackendTestResponse{
		Success:   getBool(data, "success"),
		Duration:  parseDuration(getString(data, "duration")),
		Message:   getString(data, "message"),
		Timestamp: time.Now(),
	}
	return resp, nil
}

// parseSafetyCheckResponse parses the MCP response into a SafetyCheckResponse.
func parseSafetyCheckResponse(result interface{}) (*service.SafetyCheckResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.SafetyCheckResponse{
		ThresholdPercent: getFloat64(data, "threshold_percent"),
		TotalKeysChecked: int(getInt64(data, "total_keys_checked")),
		TotalDEKsChecked: int(getInt64(data, "total_deks_checked")),
		Items:            make([]service.SafetyCheckItem, 0),
		Timestamp:        time.Now(),
	}

	itemsRaw, ok := data["items"].([]interface{})
	if ok {
		for _, iRaw := range itemsRaw {
			iMap, ok := iRaw.(map[string]interface{})
			if !ok {
				continue
			}
			resp.Items = append(resp.Items, service.SafetyCheckItem{
				ID:             getString(iMap, "id"),
				Type:           getString(iMap, "type"),
				OperationCount: uint64(getInt64(iMap, "operation_count")),
				BytesProcessed: uint64(getInt64(iMap, "bytes_processed")),
				UsagePercent:   getFloat64(iMap, "usage_percent"),
				MaxOperations:  uint64(getInt64(iMap, "max_operations")),
				MaxBytes:       uint64(getInt64(iMap, "max_bytes")),
			})
		}
	}

	return resp, nil
}

// parseSafetyReportResponse parses the MCP response into a SafetyReportResponse.
func parseSafetyReportResponse(result interface{}) (*service.SafetyReportResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.SafetyReportResponse{
		TotalOperations:     uint64(getInt64(data, "total_operations")),
		TotalBytesProcessed: uint64(getInt64(data, "total_bytes_processed")),
		Items:               make([]service.SafetyCheckItem, 0),
		Timestamp:           time.Now(),
	}

	if scMap := getMap(data, "server_config"); scMap != nil {
		resp.ServerConfig = service.AEADServerConfig{
			Enforce:           getBool(scMap, "enforce"),
			MaxOperations:     uint64(getInt64(scMap, "max_operations")),
			MaxBytes:          uint64(getInt64(scMap, "max_bytes")),
			WarningPercent:    int(getInt64(scMap, "warning_percent")),
			AutoRotatePercent: int(getInt64(scMap, "auto_rotate_percent")),
		}
	}

	itemsRaw, ok := data["items"].([]interface{})
	if ok {
		for _, iRaw := range itemsRaw {
			iMap, ok := iRaw.(map[string]interface{})
			if !ok {
				continue
			}
			resp.Items = append(resp.Items, service.SafetyCheckItem{
				ID:             getString(iMap, "id"),
				Type:           getString(iMap, "type"),
				OperationCount: uint64(getInt64(iMap, "operation_count")),
				BytesProcessed: uint64(getInt64(iMap, "bytes_processed")),
				UsagePercent:   getFloat64(iMap, "usage_percent"),
				MaxOperations:  uint64(getInt64(iMap, "max_operations")),
				MaxBytes:       uint64(getInt64(iMap, "max_bytes")),
			})
		}
	}

	return resp, nil
}

// =============================================================================
// Shared Helper Functions
// =============================================================================

// toJSONMap converts an interface to a map by JSON marshaling and unmarshaling.
func toJSONMap(v interface{}) (map[string]interface{}, error) {
	if v == nil {
		return make(map[string]interface{}), nil
	}

	// If already a map, return it
	if m, ok := v.(map[string]interface{}); ok {
		return m, nil
	}

	// JSON round-trip for other types
	data, err := json.Marshal(v)
	if err != nil {
		return nil, api.NewMarshalError(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, api.NewOperationFailedError("unmarshal result", err)
	}

	return result, nil
}

// getString safely gets a string from a map.
func getString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getStringMulti safely gets a string from a map, checking multiple keys in order.
func getStringMulti(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if s := getString(m, key); s != "" {
			return s
		}
	}
	return ""
}

// getInt64 safely gets an int64 from a map.
func getInt64(m map[string]interface{}, key string) int64 {
	if m == nil {
		return 0
	}
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int64:
			return n
		case int:
			return int64(n)
		}
	}
	return 0
}

// getInt64Multi safely gets an int64 from a map, checking multiple keys in order.
func getInt64Multi(m map[string]interface{}, keys ...string) int64 {
	for _, key := range keys {
		if n := getInt64(m, key); n != 0 {
			return n
		}
	}
	return 0
}

// getFloat64 safely gets a float64 from a map.
func getFloat64(m map[string]interface{}, key string) float64 {
	if m == nil {
		return 0
	}
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}

// getBool safely gets a bool from a map.
func getBool(m map[string]interface{}, key string) bool {
	if m == nil {
		return false
	}
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// getBoolMulti safely gets a bool from a map, checking multiple keys in order.
func getBoolMulti(m map[string]interface{}, keys ...string) bool {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			if b, ok := v.(bool); ok {
				return b
			}
		}
	}
	return false
}

// getMap safely gets a nested map from a map.
func getMap(m map[string]interface{}, key string) map[string]interface{} {
	if m == nil {
		return nil
	}
	if v, ok := m[key]; ok {
		if nested, ok := v.(map[string]interface{}); ok {
			return nested
		}
	}
	return nil
}

// getBytes safely gets bytes from a map (expecting base64 or string).
func getBytes(m map[string]interface{}, key string) []byte {
	if m == nil {
		return nil
	}
	if v, ok := m[key]; ok {
		switch b := v.(type) {
		case string:
			return []byte(b)
		case []byte:
			return b
		}
	}
	return nil
}

// getSliceMulti safely gets a slice from a map, checking multiple keys in order.
func getSliceMulti(m map[string]interface{}, keys ...string) []interface{} {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			if arr, ok := v.([]interface{}); ok {
				return arr
			}
		}
	}
	return nil
}

// parseTimestamp converts a Unix timestamp to time.Time.
// It auto-detects whether the value is in seconds or nanoseconds.
// Values above 1e15 are treated as nanoseconds since that threshold
// corresponds to year ~33658 in seconds, well beyond any valid timestamp.
func parseTimestamp(ts int64) time.Time {
	if ts == 0 {
		return time.Time{}
	}
	const nanosecondThreshold = int64(1e15)
	if ts > nanosecondThreshold || ts < -nanosecondThreshold {
		return time.Unix(0, ts)
	}
	return time.Unix(ts, 0)
}

// parseKeyAlgorithm converts a string to KeyAlgorithm.
func parseKeyAlgorithm(s string) service.KeyAlgorithm {
	return service.KeyAlgorithm(s)
}

// parseDuration converts a duration string to time.Duration.
func parseDuration(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, _ := time.ParseDuration(s)
	return d
}

// =============================================================================
// Compile-time interface checks.
// =============================================================================

var _ KMSClient = (*mcpKMSClient)(nil)
var _ BackendClient = (*mcpBackendClient)(nil)
var _ SafetyClient = (*mcpSafetyClient)(nil)
