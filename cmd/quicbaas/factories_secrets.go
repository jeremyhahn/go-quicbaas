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
	"encoding/base64"
	"time"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// init overrides the default secretsClientFactory to be transport-aware.
func init() {
	secretsClientFactory = createTransportAwareSecretsClient
}

// createTransportAwareSecretsClient creates a client for secrets operations based on transport.
func createTransportAwareSecretsClient() (SecretsClient, error) {
	switch transport {
	case "grpc", "":
		return createGRPCSecretsClient()
	case "rest":
		return createRESTSecretsClient()
	case "quic":
		return createQUICSecretsClient()
	case "mcp":
		return createMCPSecretsClient()
	default:
		return createGRPCSecretsClient()
	}
}

// =============================================================================
// REST Secrets Client
// =============================================================================

// restSecretsClient wraps the SDK client for REST transport secrets operations.
type restSecretsClient struct {
	sdk quicbaas.Client
}

// createRESTSecretsClient creates a REST client for secrets operations.
func createRESTSecretsClient() (SecretsClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return &restSecretsClient{sdk: sdk}, nil
}

func (c *restSecretsClient) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	result, err := c.sdk.Transport().Request(ctx, "REST", "POST", map[string]interface{}{
		"path": "/api/v1/secrets/" + req.Name,
		"body": req,
	})
	if err != nil {
		return nil, err
	}
	return parseSecretsStoreResponse(result)
}

func (c *restSecretsClient) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	result, err := c.sdk.Transport().Request(ctx, "REST", "GET", map[string]interface{}{
		"path":    "/api/v1/secrets/" + req.SecretID,
		"version": req.Version,
	})
	if err != nil {
		return nil, err
	}
	return parseSecretsGetResponse(result)
}

func (c *restSecretsClient) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	result, err := c.sdk.Transport().Request(ctx, "REST", "DELETE", map[string]interface{}{
		"path":  "/api/v1/secrets/" + req.SecretID,
		"force": req.PermanentlyDelete,
	})
	if err != nil {
		return nil, err
	}
	return parseSecretsDeleteResponse(result)
}

func (c *restSecretsClient) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	result, err := c.sdk.Transport().Request(ctx, "REST", "GET", map[string]interface{}{
		"path":   "/api/v1/secrets",
		"limit":  req.Limit,
		"cursor": req.Cursor,
	})
	if err != nil {
		return nil, err
	}
	return parseSecretsListResponse(result)
}

func (c *restSecretsClient) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	result, err := c.sdk.Transport().Request(ctx, "REST", "GET", map[string]interface{}{
		"path":         "/api/v1/secrets/prefix/" + req.Prefix,
		"include_data": req.IncludeData,
		"limit":        req.Limit,
		"cursor":       req.Cursor,
	})
	if err != nil {
		return nil, err
	}
	return parseSecretsGetByPrefixResponse(result)
}

func (c *restSecretsClient) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	result, err := c.sdk.Transport().Request(ctx, "REST", "POST", map[string]interface{}{
		"path": "/api/v1/secrets/" + req.SecretID + "/rotate",
		"body": req,
	})
	if err != nil {
		return nil, err
	}
	return parseSecretsRotateResponse(result)
}

func (c *restSecretsClient) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	result, err := c.sdk.Transport().Request(ctx, "REST", "GET", map[string]interface{}{
		"path": "/api/v1/secrets/" + req.SecretID + "/versions",
	})
	if err != nil {
		return nil, err
	}
	return parseSecretsListVersionsResponse(result)
}

func (c *restSecretsClient) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	result, err := c.sdk.Transport().Request(ctx, "REST", "POST", map[string]interface{}{
		"path": "/api/v1/secrets/" + req.SecretID + "/rollback",
		"body": map[string]interface{}{
			"version": req.TargetVersion,
		},
	})
	if err != nil {
		return nil, err
	}
	return parseSecretsRollbackResponse(result)
}

func (c *restSecretsClient) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	result, err := c.sdk.Transport().Request(ctx, "REST", "PUT", map[string]interface{}{
		"path": "/api/v1/secrets/" + req.SecretID + "/status",
		"body": req,
	})
	if err != nil {
		return nil, err
	}
	return parseSecretsUpdateStatusResponse(result)
}

func (c *restSecretsClient) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	// REST transport doesn't support streaming natively via the SDK transport.
	return nil, &UnsupportedTransportError{Transport: "rest-watch"}
}

func (c *restSecretsClient) Close() error {
	return c.sdk.Close()
}

// =============================================================================
// MCP Secrets Client
// =============================================================================

// mcpSecretsClient implements the SecretsClient interface using MCP transport.
type mcpSecretsClient struct {
	sdk quicbaas.Client
}

// createMCPSecretsClient creates an MCP client for secrets operations.
func createMCPSecretsClient() (SecretsClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return &mcpSecretsClient{sdk: sdk}, nil
}

func (c *mcpSecretsClient) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	args := map[string]interface{}{
		"name": req.Name,
		"data": base64.StdEncoding.EncodeToString(req.Data),
	}
	if req.Description != "" {
		args["description"] = req.Description
	}
	if len(req.Metadata) > 0 {
		args["metadata"] = req.Metadata
	}

	result, err := c.callTool(ctx, "quicbaas_secrets_store", args)
	if err != nil {
		return nil, err
	}
	return parseSecretsStoreResponse(result)
}

func (c *mcpSecretsClient) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	args := map[string]interface{}{
		"secret_id": req.SecretID,
	}
	if req.Version > 0 {
		args["version"] = req.Version
	}

	result, err := c.callTool(ctx, "quicbaas_secrets_get", args)
	if err != nil {
		return nil, err
	}
	return parseSecretsGetResponse(result)
}

func (c *mcpSecretsClient) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	args := map[string]interface{}{
		"secret_id": req.SecretID,
	}
	if req.PermanentlyDelete {
		args["permanently_delete"] = req.PermanentlyDelete
	}

	result, err := c.callTool(ctx, "quicbaas_secrets_delete", args)
	if err != nil {
		return nil, err
	}
	return parseSecretsDeleteResponse(result)
}

func (c *mcpSecretsClient) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	args := map[string]interface{}{}
	if req.Limit > 0 {
		args["limit"] = req.Limit
	}
	if req.Cursor != "" {
		args["cursor"] = req.Cursor
	}

	result, err := c.callTool(ctx, "quicbaas_secrets_list", args)
	if err != nil {
		return nil, err
	}
	return parseSecretsListResponse(result)
}

func (c *mcpSecretsClient) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	args := map[string]interface{}{
		"prefix": req.Prefix,
	}
	if req.IncludeData {
		args["include_data"] = req.IncludeData
	}
	if req.Limit > 0 {
		args["limit"] = req.Limit
	}
	if req.Cursor != "" {
		args["cursor"] = req.Cursor
	}

	result, err := c.callTool(ctx, "quicbaas_secrets_get_prefix", args)
	if err != nil {
		return nil, err
	}
	return parseSecretsGetByPrefixResponse(result)
}

func (c *mcpSecretsClient) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	args := map[string]interface{}{
		"secret_id": req.SecretID,
	}
	if len(req.NewData) > 0 {
		args["new_data"] = base64.StdEncoding.EncodeToString(req.NewData)
	}

	result, err := c.callTool(ctx, "quicbaas_secrets_rotate", args)
	if err != nil {
		return nil, err
	}
	return parseSecretsRotateResponse(result)
}

func (c *mcpSecretsClient) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	args := map[string]interface{}{
		"secret_id": req.SecretID,
	}

	result, err := c.callTool(ctx, "quicbaas_secrets_versions", args)
	if err != nil {
		return nil, err
	}
	return parseSecretsListVersionsResponse(result)
}

func (c *mcpSecretsClient) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	args := map[string]interface{}{
		"secret_id":      req.SecretID,
		"target_version": req.TargetVersion,
	}

	result, err := c.callTool(ctx, "quicbaas_secrets_rollback", args)
	if err != nil {
		return nil, err
	}
	return parseSecretsRollbackResponse(result)
}

func (c *mcpSecretsClient) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	args := map[string]interface{}{
		"secret_id": req.SecretID,
		"status":    string(req.Status),
	}

	result, err := c.callTool(ctx, "quicbaas_secrets_update_status", args)
	if err != nil {
		return nil, err
	}
	return parseSecretsUpdateStatusResponse(result)
}

func (c *mcpSecretsClient) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	// MCP transport doesn't support streaming natively
	return nil, &UnsupportedTransportError{Transport: "mcp-watch"}
}

func (c *mcpSecretsClient) Close() error {
	return c.sdk.Close()
}

func (c *mcpSecretsClient) callTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	return c.sdk.Transport().Request(ctx, "MCP", toolName, args)
}

// =============================================================================
// Response Parsing Functions
// =============================================================================

// parseSecretsStoreResponse parses the response into a StoreSecretResponse.
func parseSecretsStoreResponse(result interface{}) (*service.StoreSecretResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.StoreSecretResponse{
		SecretID:  getString(data, "secret_id"),
		Version:   uint64(getInt64(data, "version")),
		Timestamp: parseSecretsTimestamp(getInt64(data, "timestamp")),
	}
	return resp, nil
}

// parseSecretsGetResponse parses the response into a GetSecretResponse.
func parseSecretsGetResponse(result interface{}) (*service.GetSecretResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	// Get the secret data - try base64 decoding first since MCP serializes []byte as base64
	var secretData []byte
	if b64 := getString(data, "data"); b64 != "" {
		// Try base64 decoding - this is the expected format from MCP
		decoded, decodeErr := base64.StdEncoding.DecodeString(b64)
		if decodeErr == nil {
			secretData = decoded
		} else {
			// Fallback: treat as raw string data
			secretData = []byte(b64)
		}
	} else {
		// Try direct bytes
		secretData = getBytes(data, "data")
	}

	resp := &service.GetSecretResponse{
		SecretID:  getString(data, "secret_id"),
		Version:   uint64(getInt64(data, "version")),
		Data:      secretData,
		CreatedAt: parseSecretsTimestamp(getInt64(data, "created_at")),
		Timestamp: parseSecretsTimestamp(getInt64(data, "timestamp")),
	}

	// Handle metadata
	if metadata := getMap(data, "metadata"); metadata != nil {
		resp.Metadata = make(map[string]string)
		for k, v := range metadata {
			if s, ok := v.(string); ok {
				resp.Metadata[k] = s
			}
		}
	}

	return resp, nil
}

// parseSecretsDeleteResponse parses the response into a DeleteSecretResponse.
func parseSecretsDeleteResponse(result interface{}) (*service.DeleteSecretResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.DeleteSecretResponse{
		SecretID:  getString(data, "secret_id"),
		Deleted:   getBool(data, "deleted"),
		Timestamp: parseSecretsTimestamp(getInt64(data, "timestamp")),
	}
	return resp, nil
}

// parseSecretsListResponse parses the response into a ListSecretsResponse.
func parseSecretsListResponse(result interface{}) (*service.ListSecretsResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.ListSecretsResponse{
		Secrets:    make([]*service.Secret, 0),
		NextCursor: getString(data, "next_cursor"),
		HasMore:    getBool(data, "has_more"),
		TotalCount: int(getInt64(data, "total_count")),
		Timestamp:  parseSecretsTimestamp(getInt64(data, "timestamp")),
	}

	secretsRaw, ok := data["secrets"].([]interface{})
	if ok {
		for _, secretRaw := range secretsRaw {
			secretMap, ok := secretRaw.(map[string]interface{})
			if !ok {
				continue
			}
			resp.Secrets = append(resp.Secrets, &service.Secret{
				SecretID:       getString(secretMap, "secret_id"),
				Name:           getString(secretMap, "name"),
				Description:    getString(secretMap, "description"),
				CurrentVersion: uint64(getInt64(secretMap, "current_version")),
				CreatedAt:      parseSecretsTimestamp(getInt64(secretMap, "created_at")),
				UpdatedAt:      parseSecretsTimestamp(getInt64(secretMap, "updated_at")),
			})
		}
	}

	return resp, nil
}

// parseSecretsGetByPrefixResponse parses the response into a GetSecretsByPrefixResponse.
func parseSecretsGetByPrefixResponse(result interface{}) (*service.GetSecretsByPrefixResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.GetSecretsByPrefixResponse{
		Secrets:    make(map[string]map[string][]byte),
		Metadata:   make(map[string]map[string]string),
		Version:    getInt64(data, "version"),
		NextCursor: getString(data, "next_cursor"),
		HasMore:    getBool(data, "has_more"),
		TotalCount: int(getInt64(data, "total_count")),
		Timestamp:  parseSecretsTimestamp(getInt64(data, "timestamp")),
	}

	// Parse the secrets map structure
	secretsRaw := getMap(data, "secrets")
	for path, pathData := range secretsRaw {
		pathMap, ok := pathData.(map[string]interface{})
		if !ok {
			continue
		}
		resp.Secrets[path] = make(map[string][]byte)
		for key, val := range pathMap {
			switch v := val.(type) {
			case string:
				decoded, err := base64.StdEncoding.DecodeString(v)
				if err == nil {
					resp.Secrets[path][key] = decoded
				} else {
					resp.Secrets[path][key] = []byte(v)
				}
			case []byte:
				resp.Secrets[path][key] = v
			}
		}
	}

	// Parse the metadata map structure
	metadataRaw := getMap(data, "metadata")
	for path, pathData := range metadataRaw {
		pathMap, ok := pathData.(map[string]interface{})
		if !ok {
			continue
		}
		resp.Metadata[path] = make(map[string]string)
		for key, val := range pathMap {
			if s, ok := val.(string); ok {
				resp.Metadata[path][key] = s
			}
		}
	}

	return resp, nil
}

// parseSecretsRotateResponse parses the response into a RotateSecretResponse.
func parseSecretsRotateResponse(result interface{}) (*service.RotateSecretResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.RotateSecretResponse{
		SecretID:   getString(data, "secret_id"),
		OldVersion: uint64(getInt64(data, "old_version")),
		NewVersion: uint64(getInt64(data, "new_version")),
		Timestamp:  parseSecretsTimestamp(getInt64(data, "timestamp")),
	}
	return resp, nil
}

// parseSecretsListVersionsResponse parses the response into a ListSecretVersionsResponse.
func parseSecretsListVersionsResponse(result interface{}) (*service.ListSecretVersionsResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.ListSecretVersionsResponse{
		Versions:  make([]*service.SecretVersion, 0),
		Timestamp: parseSecretsTimestamp(getInt64(data, "timestamp")),
	}

	versionsRaw, ok := data["versions"].([]interface{})
	if ok {
		for _, vRaw := range versionsRaw {
			vMap, ok := vRaw.(map[string]interface{})
			if !ok {
				continue
			}
			resp.Versions = append(resp.Versions, &service.SecretVersion{
				SecretID:  getString(vMap, "secret_id"),
				Version:   uint64(getInt64(vMap, "version")),
				CreatedAt: parseSecretsTimestamp(getInt64(vMap, "created_at")),
				CreatedBy: getString(vMap, "created_by"),
			})
		}
	}

	return resp, nil
}

// parseSecretsRollbackResponse parses the response into a RollbackSecretResponse.
func parseSecretsRollbackResponse(result interface{}) (*service.RollbackSecretResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.RollbackSecretResponse{
		SecretID:   getString(data, "secret_id"),
		NewVersion: uint64(getInt64(data, "new_version")),
		Timestamp:  parseSecretsTimestamp(getInt64(data, "timestamp")),
	}
	return resp, nil
}

// parseSecretsUpdateStatusResponse parses the response into an UpdateSecretStatusResponse.
func parseSecretsUpdateStatusResponse(result interface{}) (*service.UpdateSecretStatusResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.UpdateSecretStatusResponse{
		SecretID:  getString(data, "secret_id"),
		OldStatus: service.SecretStatus(getString(data, "old_status")),
		NewStatus: service.SecretStatus(getString(data, "new_status")),
		Timestamp: parseSecretsTimestamp(getInt64(data, "timestamp")),
	}
	return resp, nil
}

// parseSecretsTimestamp converts a Unix timestamp (seconds) to time.Time.
func parseSecretsTimestamp(ts int64) time.Time {
	if ts == 0 {
		return time.Time{}
	}
	return time.Unix(ts, 0)
}
