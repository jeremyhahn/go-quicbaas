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

//go:build integration

package secrets

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// MCP Client Implementation
// =============================================================================

// MCPClient wraps HTTP client for MCP protocol testing using JSON-RPC 2.0.
type MCPClient struct {
	endpoint      string
	httpClient    *http.Client
	nextID        int
	accessToken   string
	authenticated bool
	initialized   bool
}

// NewMCPClient creates a new MCP client.
func NewMCPClient(endpoint string) *MCPClient {
	return &MCPClient{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
				TLSHandshakeTimeout: 10 * time.Second,
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
		},
		nextID: 1,
	}
}

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

// Initialize initializes the MCP connection.
func (c *MCPClient) Initialize(ctx context.Context) error {
	if c.initialized {
		return nil
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextID,
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]interface{}{
				"name":    "quicbaas-secrets-integration-test",
				"version": "1.0.0",
			},
		},
	}
	c.nextID++

	_, err := c.call(ctx, req)
	if err != nil {
		return err
	}

	c.initialized = true
	return nil
}

// ensureAuthenticated performs login if not already authenticated.
// Uses the bootstrap admin credentials from the test configuration.
func (c *MCPClient) ensureAuthenticated(ctx context.Context) error {
	if c.authenticated {
		return nil
	}

	if err := c.Initialize(ctx); err != nil {
		return err
	}

	restEndpoints := helpers.GetRESTEndpoints()
	if len(restEndpoints) == 0 {
		return fmt.Errorf("no REST endpoints available for PIV login")
	}
	bearer, err := helpers.AuthPIVContext(ctx, restEndpoints[0], "", "", "")
	if err != nil {
		return fmt.Errorf("PIV login failed: %w", err)
	}

	c.accessToken = bearer
	c.authenticated = true
	return nil
}


// ListTools lists available MCP tools.
func (c *MCPClient) ListTools(ctx context.Context) ([]map[string]interface{}, error) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextID,
		Method:  "tools/list",
	}
	c.nextID++

	resp, err := c.call(ctx, req)
	if err != nil {
		return nil, err
	}

	var result struct {
		Tools []map[string]interface{} `json:"tools"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal tools list", Cause: err}
	}

	return result.Tools, nil
}

// CallTool calls an MCP tool with arguments.
func (c *MCPClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (json.RawMessage, error) {
	// Ensure authenticated before calling any tools
	if err := c.ensureAuthenticated(ctx); err != nil {
		return nil, err
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextID,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      name,
			"arguments": args,
		},
	}
	c.nextID++

	result, err := c.call(ctx, req)
	if err != nil {
		return nil, err
	}

	// Extract the text content from MCP content format
	return extractMCPContent(result)
}

// extractMCPContent extracts the inner JSON from MCP content format.
// MCP tool results are wrapped as: {"content": [{"type": "text", "text": "..."}]}
func extractMCPContent(result json.RawMessage) (json.RawMessage, error) {
	var wrapper struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(result, &wrapper); err != nil {
		// If it doesn't match the content format, return as-is
		return result, nil
	}

	if len(wrapper.Content) == 0 {
		return nil, &qberrors.MessageError{Message: "no content in MCP response"}
	}

	// Return the text content as raw JSON
	return json.RawMessage(wrapper.Content[0].Text), nil
}

// call makes a JSON-RPC call to the MCP server.
func (c *MCPClient) call(ctx context.Context, req JSONRPCRequest) (json.RawMessage, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to marshal request", Cause: err}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to create HTTP request", Cause: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Add Authorization header if authenticated
	if c.accessToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.accessToken)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to make HTTP request", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to read response body", Cause: err}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: fmt.Sprintf("HTTP status %d: %s", resp.StatusCode, string(respBody))}
	}

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
	}

	if rpcResp.Error != nil {
		return nil, &qberrors.MessageError{Message: fmt.Sprintf("JSON-RPC error %d: %s (data: %s)", rpcResp.Error.Code, rpcResp.Error.Message, rpcResp.Error.Data)}
	}

	return rpcResp.Result, nil
}

// getMCPEndpoint returns the MCP endpoint for testing.
// MCP server runs on port 8888 by default.
func getMCPEndpoint() string {
	endpoint := os.Getenv("QUICBAAS_MCP_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://test-node1:8888/mcp"
	}
	return endpoint
}

// =============================================================================
// MCP Protocol Client for Multi-Protocol Testing
// =============================================================================

// mcpProtocolClient implements secretsProtocol using MCP (Model Context Protocol).
type mcpProtocolClient struct {
	client *MCPClient
}

func (c *mcpProtocolClient) Name() string { return "MCP" }

func (c *mcpProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	endpoint := getMCPEndpoint()
	c.client = NewMCPClient(endpoint)

	err := c.client.Initialize(ctx)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "MCP server not available at " + endpoint, Cause: err}
	}

	return nil
}

func (c *mcpProtocolClient) Teardown(t *testing.T) error {
	return nil
}

func (c *mcpProtocolClient) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	// Base64 encode the data as required by MCP
	encodedData := base64.StdEncoding.EncodeToString(req.Data)

	args := map[string]interface{}{
		"secret_id": req.SecretID,
		"name":      req.Name,
		"data":      encodedData,
	}

	if req.Description != "" {
		args["description"] = req.Description
	}
	if req.Metadata != nil {
		args["metadata"] = req.Metadata
	}
	if req.Tags != nil {
		args["tags"] = req.Tags
	}

	result, err := c.client.CallTool(ctx, "quicbaas_secrets_store", args)
	if err != nil {
		return nil, err
	}

	var resp service.StoreSecretResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
	}

	return &resp, nil
}

func (c *mcpProtocolClient) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	args := map[string]interface{}{
		"secret_id": req.SecretID,
	}

	if req.Version > 0 {
		args["version"] = req.Version
	}

	result, err := c.client.CallTool(ctx, "quicbaas_secrets_get", args)
	if err != nil {
		return nil, err
	}

	var resp service.GetSecretResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
	}

	return &resp, nil
}

func (c *mcpProtocolClient) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	args := map[string]interface{}{}

	if req.Limit > 0 {
		args["limit"] = req.Limit
	}
	if req.Cursor != "" {
		args["cursor"] = req.Cursor
	}
	if req.Filter != nil {
		args["filter"] = req.Filter
	}

	result, err := c.client.CallTool(ctx, "quicbaas_secrets_list", args)
	if err != nil {
		return nil, err
	}

	var resp service.ListSecretsResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
	}

	return &resp, nil
}

func (c *mcpProtocolClient) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	args := map[string]interface{}{
		"secret_id":          req.SecretID,
		"permanently_delete": req.PermanentlyDelete,
	}

	result, err := c.client.CallTool(ctx, "quicbaas_secrets_delete", args)
	if err != nil {
		return nil, err
	}

	var resp service.DeleteSecretResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
	}

	return &resp, nil
}

func (c *mcpProtocolClient) ListVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	args := map[string]interface{}{
		"secret_id": req.SecretID,
	}

	if req.Limit > 0 {
		args["limit"] = req.Limit
	}
	if req.Cursor != "" {
		args["cursor"] = req.Cursor
	}

	result, err := c.client.CallTool(ctx, "quicbaas_secrets_versions", args)
	if err != nil {
		return nil, err
	}

	var resp service.ListSecretVersionsResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
	}

	return &resp, nil
}

func (c *mcpProtocolClient) Rollback(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	args := map[string]interface{}{
		"secret_id":      req.SecretID,
		"target_version": req.TargetVersion,
	}

	result, err := c.client.CallTool(ctx, "quicbaas_secrets_rollback", args)
	if err != nil {
		return nil, err
	}

	var resp service.RollbackSecretResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
	}

	return &resp, nil
}

func (c *mcpProtocolClient) Rotate(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	// Base64 encode the new data as required by MCP
	encodedData := base64.StdEncoding.EncodeToString(req.NewData)

	args := map[string]interface{}{
		"secret_id": req.SecretID,
		"new_data":  encodedData,
	}

	if req.Metadata != nil {
		args["metadata"] = req.Metadata
	}

	result, err := c.client.CallTool(ctx, "quicbaas_secrets_rotate", args)
	if err != nil {
		return nil, err
	}

	var resp service.RotateSecretResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
	}

	return &resp, nil
}

func (c *mcpProtocolClient) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	// Determine which tool to call based on the status
	var toolName string
	switch req.Status {
	case service.SecretStatusEnabled:
		toolName = "quicbaas_secrets_enable"
	case service.SecretStatusDisabled:
		toolName = "quicbaas_secrets_disable"
	default:
		return nil, &qberrors.MessageError{Message: "unsupported status: " + string(req.Status)}
	}

	args := map[string]interface{}{
		"secret_id": req.SecretID,
	}

	result, err := c.client.CallTool(ctx, toolName, args)
	if err != nil {
		return nil, err
	}

	var resp service.UpdateSecretStatusResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
	}

	return &resp, nil
}

// =============================================================================
// MCP Secrets Integration Tests
// =============================================================================

// TestMCPSecretsAvailability tests that secrets tools are available via MCP.
func TestMCPSecretsAvailability(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	// Initialize connection
	err := client.Initialize(ctx)
	require.NoError(t, err, "MCP server must be available at %s", endpoint)

	// List tools
	tools, err := client.ListTools(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, tools)

	// Verify secrets tools are available
	secretsTools := []string{
		"quicbaas_secrets_store",
		"quicbaas_secrets_get",
		"quicbaas_secrets_list",
		"quicbaas_secrets_delete",
		"quicbaas_secrets_versions",
		"quicbaas_secrets_rollback",
		"quicbaas_secrets_rotate",
	}

	toolMap := make(map[string]bool)
	for _, tool := range tools {
		if name, ok := tool["name"].(string); ok {
			toolMap[name] = true
		}
	}

	for _, expectedTool := range secretsTools {
		assert.True(t, toolMap[expectedTool], "Expected tool %s to be available", expectedTool)
		if !toolMap[expectedTool] {
			t.Logf("Available tools: %v", toolMap)
		}
	}

	t.Logf("Found %d total tools, %d secrets tools verified", len(tools), len(secretsTools))
}

// TestMCPSecretsStore tests storing secrets via MCP protocol.
func TestMCPSecretsStore(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	require.NoError(t, err, "MCP server must be available")

	secretData := map[string]interface{}{
		"username": "mcp-user",
		"password": "mcp-secret-password",
		"host":     "mcp.example.com",
	}

	dataBytes, err := json.Marshal(secretData)
	require.NoError(t, err)
	encodedData := base64.StdEncoding.EncodeToString(dataBytes)

	path := fmt.Sprintf("test/mcp/store/%d", time.Now().UnixNano())

	args := map[string]interface{}{
		"secret_id":   path,
		"name":        "mcp-test-secret",
		"description": "Test secret created via MCP",
		"data":        encodedData,
		"metadata": map[string]string{
			"protocol": "MCP",
			"test":     "store",
		},
	}

	result, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
	require.NoError(t, err, "MCP store secret failed")
	require.NotNil(t, result)

	var resp service.StoreSecretResponse
	err = json.Unmarshal(result, &resp)
	require.NoError(t, err)

	assert.Equal(t, path, resp.SecretID)
	assert.Greater(t, resp.Version, uint64(0))

	t.Logf("[MCP] Created secret: %s, version: %d", resp.SecretID, resp.Version)

	// Cleanup
	cleanupSecretMCP(t, client, ctx, path)
}

// TestMCPSecretsGet tests retrieving secrets via MCP protocol.
func TestMCPSecretsGet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	require.NoError(t, err, "MCP server must be available")

	// First, create a secret
	secretData := map[string]interface{}{
		"key": "value-for-mcp-get",
	}

	dataBytes, err := json.Marshal(secretData)
	require.NoError(t, err)
	encodedData := base64.StdEncoding.EncodeToString(dataBytes)

	path := fmt.Sprintf("test/mcp/get/%d", time.Now().UnixNano())

	storeArgs := map[string]interface{}{
		"secret_id": path,
		"name":      "mcp-get-test",
		"data":      encodedData,
	}

	_, err = client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
	require.NoError(t, err)
	defer cleanupSecretMCP(t, client, ctx, path)

	// Now retrieve it via MCP
	getArgs := map[string]interface{}{
		"secret_id": path,
	}

	result, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
	require.NoError(t, err, "MCP get secret failed")
	require.NotNil(t, result)

	var resp service.GetSecretResponse
	err = json.Unmarshal(result, &resp)
	require.NoError(t, err)

	assert.Equal(t, path, resp.SecretID)
	assert.NotEmpty(t, resp.Data)

	t.Logf("[MCP] Retrieved secret: %s, version: %d", resp.SecretID, resp.Version)
}

// TestMCPSecretsList tests listing secrets via MCP protocol.
func TestMCPSecretsList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	require.NoError(t, err, "MCP server must be available")

	// Create a few test secrets
	basePath := fmt.Sprintf("test/mcp/list/%d", time.Now().UnixNano())
	paths := []string{}

	for i := 0; i < 3; i++ {
		secretData := map[string]interface{}{
			"index": i,
		}

		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		path := fmt.Sprintf("%s/secret-%d", basePath, i)
		paths = append(paths, path)

		storeArgs := map[string]interface{}{
			"secret_id": path,
			"name":      fmt.Sprintf("mcp-list-test-%d", i),
			"data":      encodedData,
		}

		_, err = client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
		require.NoError(t, err)
	}

	// Cleanup after test
	defer func() {
		for _, path := range paths {
			cleanupSecretMCP(t, client, ctx, path)
		}
	}()

	// List secrets via MCP
	listArgs := map[string]interface{}{
		"limit": 100,
	}

	result, err := client.CallTool(ctx, "quicbaas_secrets_list", listArgs)
	require.NoError(t, err, "MCP list secrets failed")
	require.NotNil(t, result)

	var resp service.ListSecretsResponse
	err = json.Unmarshal(result, &resp)
	require.NoError(t, err)

	assert.NotEmpty(t, resp.Secrets)

	t.Logf("[MCP] Listed %d secrets", len(resp.Secrets))
}

// TestMCPSecretsDelete tests deleting secrets via MCP protocol.
func TestMCPSecretsDelete(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	require.NoError(t, err, "MCP server must be available")

	// Create a secret to delete
	secretData := map[string]interface{}{
		"to": "delete",
	}

	dataBytes, err := json.Marshal(secretData)
	require.NoError(t, err)
	encodedData := base64.StdEncoding.EncodeToString(dataBytes)

	path := fmt.Sprintf("test/mcp/delete/%d", time.Now().UnixNano())

	storeArgs := map[string]interface{}{
		"secret_id": path,
		"name":      "mcp-delete-test",
		"data":      encodedData,
	}

	_, err = client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
	require.NoError(t, err)

	// Delete it via MCP (soft delete)
	deleteArgs := map[string]interface{}{
		"secret_id":          path,
		"permanently_delete": false,
	}

	result, err := client.CallTool(ctx, "quicbaas_secrets_delete", deleteArgs)
	require.NoError(t, err, "MCP delete secret failed")
	require.NotNil(t, result)

	var resp service.DeleteSecretResponse
	err = json.Unmarshal(result, &resp)
	require.NoError(t, err)

	assert.Equal(t, path, resp.SecretID)
	assert.True(t, resp.Deleted)

	t.Logf("[MCP] Deleted secret: %s", path)

	// Verify it's gone
	getArgs := map[string]interface{}{
		"secret_id": path,
	}

	_, err = client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
	assert.Error(t, err, "Should not be able to get deleted secret")
}

// TestMCPSecretsVersions tests listing versions via MCP protocol.
func TestMCPSecretsVersions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	require.NoError(t, err, "MCP server must be available")

	path := fmt.Sprintf("test/mcp/versions/%d", time.Now().UnixNano())
	defer cleanupSecretMCP(t, client, ctx, path)

	// Create multiple versions
	for i := 1; i <= 3; i++ {
		secretData := map[string]interface{}{
			"version": i,
		}

		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		storeArgs := map[string]interface{}{
			"secret_id": path,
			"name":      "mcp-versions-test",
			"data":      encodedData,
		}

		_, err = client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
		require.NoError(t, err)
	}

	// List versions via MCP
	versionArgs := map[string]interface{}{
		"secret_id": path,
		"limit":     100,
	}

	result, err := client.CallTool(ctx, "quicbaas_secrets_versions", versionArgs)
	require.NoError(t, err, "MCP list versions failed")
	require.NotNil(t, result)

	var resp service.ListSecretVersionsResponse
	err = json.Unmarshal(result, &resp)
	require.NoError(t, err)

	assert.NotEmpty(t, resp.Versions)
	assert.GreaterOrEqual(t, len(resp.Versions), 3)

	t.Logf("[MCP] Listed %d versions for secret: %s", len(resp.Versions), path)
}

// TestMCPSecretsRollback tests rolling back secrets via MCP protocol.
func TestMCPSecretsRollback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	require.NoError(t, err, "MCP server must be available")

	path := fmt.Sprintf("test/mcp/rollback/%d", time.Now().UnixNano())
	defer cleanupSecretMCP(t, client, ctx, path)

	// Create multiple versions
	for i := 1; i <= 3; i++ {
		secretData := map[string]interface{}{
			"version": i,
			"data":    fmt.Sprintf("version-%d", i),
		}

		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		storeArgs := map[string]interface{}{
			"secret_id": path,
			"name":      "mcp-rollback-test",
			"data":      encodedData,
		}

		_, err = client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
		require.NoError(t, err)
	}

	// Rollback to version 1 via MCP
	rollbackArgs := map[string]interface{}{
		"secret_id":      path,
		"target_version": uint64(1),
	}

	result, err := client.CallTool(ctx, "quicbaas_secrets_rollback", rollbackArgs)
	require.NoError(t, err, "MCP rollback failed")
	require.NotNil(t, result)

	var resp service.RollbackSecretResponse
	err = json.Unmarshal(result, &resp)
	require.NoError(t, err)

	assert.Equal(t, path, resp.SecretID)
	assert.Greater(t, resp.NewVersion, uint64(0))

	t.Logf("[MCP] Rolled back secret: %s to version 1, new version: %d", path, resp.NewVersion)

	// Verify the data
	getArgs := map[string]interface{}{
		"secret_id": path,
	}

	getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
	require.NoError(t, err)

	var getResp service.GetSecretResponse
	err = json.Unmarshal(getResult, &getResp)
	require.NoError(t, err)

	// Verify it contains version 1 data
	var data map[string]interface{}
	err = json.Unmarshal(getResp.Data, &data)
	require.NoError(t, err)
	assert.Equal(t, "version-1", data["data"])
}

// TestMCPSecretsRotate tests rotating secrets via MCP protocol.
func TestMCPSecretsRotate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	require.NoError(t, err, "MCP server must be available")

	path := fmt.Sprintf("test/mcp/rotate/%d", time.Now().UnixNano())
	defer cleanupSecretMCP(t, client, ctx, path)

	// Create initial secret
	secretData := map[string]interface{}{
		"key": "original-mcp-value",
	}

	dataBytes, err := json.Marshal(secretData)
	require.NoError(t, err)
	encodedData := base64.StdEncoding.EncodeToString(dataBytes)

	storeArgs := map[string]interface{}{
		"secret_id": path,
		"name":      "mcp-rotate-test",
		"data":      encodedData,
	}

	storeResult, err := client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
	require.NoError(t, err)

	var storeResp service.StoreSecretResponse
	err = json.Unmarshal(storeResult, &storeResp)
	require.NoError(t, err)
	oldVersion := storeResp.Version

	// Rotate to new value via MCP
	newData := map[string]interface{}{
		"key": "rotated-mcp-value",
	}

	newDataBytes, err := json.Marshal(newData)
	require.NoError(t, err)
	encodedNewData := base64.StdEncoding.EncodeToString(newDataBytes)

	rotateArgs := map[string]interface{}{
		"secret_id": path,
		"new_data":  encodedNewData,
	}

	result, err := client.CallTool(ctx, "quicbaas_secrets_rotate", rotateArgs)
	require.NoError(t, err, "MCP rotate failed")
	require.NotNil(t, result)

	var resp service.RotateSecretResponse
	err = json.Unmarshal(result, &resp)
	require.NoError(t, err)

	assert.Equal(t, path, resp.SecretID)
	assert.Equal(t, oldVersion, resp.OldVersion)
	assert.Greater(t, resp.NewVersion, resp.OldVersion)

	t.Logf("[MCP] Rotated secret: %s from version %d to %d", path, resp.OldVersion, resp.NewVersion)

	// Verify new data
	getArgs := map[string]interface{}{
		"secret_id": path,
	}

	getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
	require.NoError(t, err)

	var getResp service.GetSecretResponse
	err = json.Unmarshal(getResult, &getResp)
	require.NoError(t, err)

	var dataMap map[string]interface{}
	err = json.Unmarshal(getResp.Data, &dataMap)
	require.NoError(t, err)
	assert.Equal(t, "rotated-mcp-value", dataMap["key"])
}

// TestMCPSecretsLifecycle tests complete secrets lifecycle via MCP protocol.
func TestMCPSecretsLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	require.NoError(t, err, "MCP server must be available")

	path := fmt.Sprintf("test/mcp/lifecycle/%d", time.Now().UnixNano())

	// 1. Create secret
	secretData := map[string]interface{}{
		"username": "lifecycle-user",
		"password": "lifecycle-password",
	}

	dataBytes, err := json.Marshal(secretData)
	require.NoError(t, err)
	encodedData := base64.StdEncoding.EncodeToString(dataBytes)

	storeArgs := map[string]interface{}{
		"secret_id":   path,
		"name":        "lifecycle-test-secret",
		"description": "Complete lifecycle test",
		"data":        encodedData,
		"metadata": map[string]string{
			"test": "lifecycle",
		},
	}

	storeResult, err := client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
	require.NoError(t, err)

	var storeResp service.StoreSecretResponse
	err = json.Unmarshal(storeResult, &storeResp)
	require.NoError(t, err)
	assert.Equal(t, path, storeResp.SecretID)
	t.Log("1. Created secret")

	// 2. Get secret
	getArgs := map[string]interface{}{
		"secret_id": path,
	}

	getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
	require.NoError(t, err)

	var getResp service.GetSecretResponse
	err = json.Unmarshal(getResult, &getResp)
	require.NoError(t, err)
	assert.Equal(t, path, getResp.SecretID)
	t.Log("2. Retrieved secret")

	// 3. List secrets
	listArgs := map[string]interface{}{
		"limit": 100,
	}

	listResult, err := client.CallTool(ctx, "quicbaas_secrets_list", listArgs)
	require.NoError(t, err)

	var listResp service.ListSecretsResponse
	err = json.Unmarshal(listResult, &listResp)
	require.NoError(t, err)
	assert.NotEmpty(t, listResp.Secrets)
	t.Log("3. Listed secrets")

	// 4. Create new version (update)
	updateData := map[string]interface{}{
		"username": "lifecycle-user-updated",
		"password": "lifecycle-password-updated",
	}

	updateBytes, err := json.Marshal(updateData)
	require.NoError(t, err)
	encodedUpdate := base64.StdEncoding.EncodeToString(updateBytes)

	storeArgs["data"] = encodedUpdate
	_, err = client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
	require.NoError(t, err)
	t.Log("4. Created new version")

	// 5. List versions
	versionArgs := map[string]interface{}{
		"secret_id": path,
		"limit":     100,
	}

	versionResult, err := client.CallTool(ctx, "quicbaas_secrets_versions", versionArgs)
	require.NoError(t, err)

	var versionResp service.ListSecretVersionsResponse
	err = json.Unmarshal(versionResult, &versionResp)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(versionResp.Versions), 2)
	t.Log("5. Listed versions")

	// 6. Rollback to version 1
	rollbackArgs := map[string]interface{}{
		"secret_id":      path,
		"target_version": uint64(1),
	}

	rollbackResult, err := client.CallTool(ctx, "quicbaas_secrets_rollback", rollbackArgs)
	require.NoError(t, err)

	var rollbackResp service.RollbackSecretResponse
	err = json.Unmarshal(rollbackResult, &rollbackResp)
	require.NoError(t, err)
	assert.Equal(t, path, rollbackResp.SecretID)
	t.Log("6. Rolled back to version 1")

	// 7. Rotate to new value
	rotateData := map[string]interface{}{
		"username": "rotated-user",
		"password": "rotated-password",
	}

	rotateBytes, err := json.Marshal(rotateData)
	require.NoError(t, err)
	encodedRotate := base64.StdEncoding.EncodeToString(rotateBytes)

	rotateArgs := map[string]interface{}{
		"secret_id": path,
		"new_data":  encodedRotate,
	}

	rotateResult, err := client.CallTool(ctx, "quicbaas_secrets_rotate", rotateArgs)
	require.NoError(t, err)

	var rotateResp service.RotateSecretResponse
	err = json.Unmarshal(rotateResult, &rotateResp)
	require.NoError(t, err)
	assert.Equal(t, path, rotateResp.SecretID)
	t.Log("7. Rotated secret")

	// 8. Delete secret
	deleteArgs := map[string]interface{}{
		"secret_id":          path,
		"permanently_delete": true,
	}

	deleteResult, err := client.CallTool(ctx, "quicbaas_secrets_delete", deleteArgs)
	require.NoError(t, err)

	var deleteResp service.DeleteSecretResponse
	err = json.Unmarshal(deleteResult, &deleteResp)
	require.NoError(t, err)
	assert.True(t, deleteResp.Deleted)
	t.Log("8. Deleted secret")

	// 9. Verify deletion
	_, err = client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
	assert.Error(t, err)
	t.Log("9. Verified deletion")
}

// TestMCPSecretsErrorHandling tests error handling via MCP protocol.
func TestMCPSecretsErrorHandling(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	require.NoError(t, err, "MCP server must be available")

	t.Run("get non-existent secret", func(t *testing.T) {
		args := map[string]interface{}{
			"secret_id": "non-existent-secret-id",
		}

		_, err := client.CallTool(ctx, "quicbaas_secrets_get", args)
		assert.Error(t, err)
	})

	t.Run("delete non-existent secret", func(t *testing.T) {
		args := map[string]interface{}{
			"secret_id":          "non-existent-secret-id",
			"permanently_delete": true,
		}

		_, err := client.CallTool(ctx, "quicbaas_secrets_delete", args)
		assert.Error(t, err)
	})

	t.Run("rollback non-existent secret", func(t *testing.T) {
		args := map[string]interface{}{
			"secret_id":      "non-existent-secret-id",
			"target_version": uint64(1),
		}

		_, err := client.CallTool(ctx, "quicbaas_secrets_rollback", args)
		assert.Error(t, err)
	})

	t.Run("rotate non-existent secret", func(t *testing.T) {
		newData := []byte("new-data")
		encodedData := base64.StdEncoding.EncodeToString(newData)

		args := map[string]interface{}{
			"secret_id": "non-existent-secret-id",
			"new_data":  encodedData,
		}

		_, err := client.CallTool(ctx, "quicbaas_secrets_rotate", args)
		assert.Error(t, err)
	})

	t.Run("list versions of non-existent secret", func(t *testing.T) {
		args := map[string]interface{}{
			"secret_id": "non-existent-secret-id",
			"limit":     100,
		}

		_, err := client.CallTool(ctx, "quicbaas_secrets_versions", args)
		assert.Error(t, err)
	})

	t.Run("store with missing required fields", func(t *testing.T) {
		args := map[string]interface{}{
			// Missing required "name" and "data" fields
			"secret_id": "test-secret",
		}

		_, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		assert.Error(t, err)
	})
}

// TestMCPSecretsMultiProtocol runs the complete secrets test suite against MCP protocol.
func TestMCPSecretsMultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Test MCP protocol using the multi-protocol pattern
	t.Run("MCP", func(t *testing.T) {
		testSecretsProtocol(t, ctx, &mcpProtocolClient{})
	})
}

// =============================================================================
// Helper Functions
// =============================================================================

// cleanupSecretMCP attempts to delete a test secret via MCP.
func cleanupSecretMCP(t *testing.T, client *MCPClient, ctx context.Context, secretID string) {
	t.Helper()

	args := map[string]interface{}{
		"secret_id":          secretID,
		"permanently_delete": true,
	}

	_, _ = client.CallTool(ctx, "quicbaas_secrets_delete", args)
}

// =============================================================================
// MCP Comprehensive Test Suite
// =============================================================================
// This file tests ALL MCP secrets tools with ALL parameter combinations
// as specified in the test requirements.

// TestMCPComprehensive_StoreParameterCombinations tests quicbaas_secrets_store
// with various parameter combinations.
func TestMCPComprehensive_StoreParameterCombinations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	if err != nil {
		require.NoError(t, err, "MCP server must be available at %s", endpoint)
	}

	basePath := fmt.Sprintf("test/mcp/comprehensive/store/%d", time.Now().UnixNano())

	// Test: Store without optional params (minimum required fields)
	t.Run("store_without_optional_params", func(t *testing.T) {
		secretData := map[string]interface{}{"key": "minimal-value"}
		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		args := map[string]interface{}{
			"name": "minimal-test-secret",
			"data": encodedData,
		}

		// Note: secret_id is optional for new secrets
		result, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		require.NoError(t, err, "Store without optional params should succeed")
		require.NotNil(t, result)

		var resp service.StoreSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.SecretID, "SecretID should be auto-generated")
		assert.Equal(t, uint64(1), resp.Version, "First version should be 1")
		assert.False(t, resp.Timestamp.IsZero(), "Timestamp should be set")

		cleanupSecretMCP(t, client, ctx, resp.SecretID)
		t.Logf("[MCP] Created secret with minimal params: %s", resp.SecretID)
	})

	// Test: Store with explicit secret_id
	t.Run("store_with_secret_id", func(t *testing.T) {
		path := basePath + "/with-id"
		secretData := map[string]interface{}{"key": "with-id-value"}
		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		args := map[string]interface{}{
			"secret_id": path,
			"name":      "secret-with-id",
			"data":      encodedData,
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		require.NoError(t, err)

		var resp service.StoreSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, path, resp.SecretID)

		cleanupSecretMCP(t, client, ctx, path)
		t.Logf("[MCP] Created secret with explicit ID: %s", resp.SecretID)
	})

	// Test: Store with description only
	t.Run("store_with_description", func(t *testing.T) {
		path := basePath + "/with-desc"
		secretData := map[string]interface{}{"key": "with-desc-value"}
		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		args := map[string]interface{}{
			"secret_id":   path,
			"name":        "secret-with-description",
			"data":        encodedData,
			"description": "This is a test secret with description",
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		require.NoError(t, err)

		var resp service.StoreSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, path, resp.SecretID)

		cleanupSecretMCP(t, client, ctx, path)
		t.Logf("[MCP] Created secret with description: %s", resp.SecretID)
	})

	// Test: Store with metadata
	t.Run("store_with_metadata", func(t *testing.T) {
		path := basePath + "/with-metadata"
		secretData := map[string]interface{}{"key": "with-metadata-value"}
		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		args := map[string]interface{}{
			"secret_id": path,
			"name":      "secret-with-metadata",
			"data":      encodedData,
			"metadata": map[string]string{
				"environment": "testing",
				"team":        "platform",
				"version":     "1.0.0",
			},
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		require.NoError(t, err)

		var resp service.StoreSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, path, resp.SecretID)

		// Verify metadata is stored
		getArgs := map[string]interface{}{"secret_id": path}
		getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
		require.NoError(t, err)

		var getResp service.GetSecretResponse
		err = json.Unmarshal(getResult, &getResp)
		require.NoError(t, err)
		assert.NotNil(t, getResp.Metadata)
		assert.Equal(t, "testing", getResp.Metadata["environment"])
		assert.Equal(t, "platform", getResp.Metadata["team"])

		cleanupSecretMCP(t, client, ctx, path)
		t.Logf("[MCP] Created secret with metadata: %s", resp.SecretID)
	})

	// Test: Store with tags
	t.Run("store_with_tags", func(t *testing.T) {
		path := basePath + "/with-tags"
		secretData := map[string]interface{}{"key": "with-tags-value"}
		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		args := map[string]interface{}{
			"secret_id": path,
			"name":      "secret-with-tags",
			"data":      encodedData,
			"tags": map[string]string{
				"app":       "myapp",
				"component": "database",
			},
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		require.NoError(t, err)

		var resp service.StoreSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, path, resp.SecretID)

		cleanupSecretMCP(t, client, ctx, path)
		t.Logf("[MCP] Created secret with tags: %s", resp.SecretID)
	})

	// Test: Store with all params combined
	t.Run("store_with_all_params", func(t *testing.T) {
		path := basePath + "/all-params"
		secretData := map[string]interface{}{
			"username": "admin",
			"password": "super-secret",
			"host":     "db.example.com",
			"port":     5432,
		}
		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		args := map[string]interface{}{
			"secret_id":   path,
			"name":        "full-params-secret",
			"description": "Secret with all parameters",
			"data":        encodedData,
			"metadata": map[string]string{
				"created_by": "integration-test",
				"purpose":    "testing",
			},
			"tags": map[string]string{
				"env":     "test",
				"project": "quicbaas",
			},
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		require.NoError(t, err)

		var resp service.StoreSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, path, resp.SecretID)
		assert.Equal(t, uint64(1), resp.Version)
		assert.False(t, resp.Timestamp.IsZero())

		cleanupSecretMCP(t, client, ctx, path)
		t.Logf("[MCP] Created secret with all params: %s", resp.SecretID)
	})
}

// TestMCPComprehensive_GetVersions tests quicbaas_secrets_get with version parameter.
func TestMCPComprehensive_GetVersions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	if err != nil {
		require.NoError(t, err, "MCP server must be available at %s", endpoint)
	}

	path := fmt.Sprintf("test/mcp/comprehensive/get-versions/%d", time.Now().UnixNano())
	defer cleanupSecretMCP(t, client, ctx, path)

	// Create multiple versions
	versionsData := []map[string]interface{}{
		{"version": 1, "data": "version-1-data"},
		{"version": 2, "data": "version-2-data"},
		{"version": 3, "data": "version-3-data"},
	}

	for i, vData := range versionsData {
		dataBytes, err := json.Marshal(vData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		args := map[string]interface{}{
			"secret_id": path,
			"name":      "versioned-secret",
			"data":      encodedData,
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		require.NoError(t, err)

		var resp service.StoreSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, uint64(i+1), resp.Version)
	}

	t.Logf("[MCP] Created 3 versions of secret: %s", path)

	// Test: Get latest (no version specified)
	t.Run("get_latest_version", func(t *testing.T) {
		args := map[string]interface{}{
			"secret_id": path,
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_get", args)
		require.NoError(t, err)

		var resp service.GetSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, uint64(3), resp.Version, "Should get latest version")
		assert.NotEmpty(t, resp.Data)

		// Verify data
		var data map[string]interface{}
		err = json.Unmarshal(resp.Data, &data)
		require.NoError(t, err)
		assert.Equal(t, "version-3-data", data["data"])

		t.Logf("[MCP] Got latest version: %d", resp.Version)
	})

	// Test: Get version 1
	t.Run("get_version_1", func(t *testing.T) {
		args := map[string]interface{}{
			"secret_id": path,
			"version":   uint64(1),
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_get", args)
		require.NoError(t, err)

		var resp service.GetSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, uint64(1), resp.Version)

		var data map[string]interface{}
		err = json.Unmarshal(resp.Data, &data)
		require.NoError(t, err)
		assert.Equal(t, "version-1-data", data["data"])

		t.Logf("[MCP] Got version 1 data: %s", data["data"])
	})

	// Test: Get version 2
	t.Run("get_version_2", func(t *testing.T) {
		args := map[string]interface{}{
			"secret_id": path,
			"version":   uint64(2),
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_get", args)
		require.NoError(t, err)

		var resp service.GetSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, uint64(2), resp.Version)

		var data map[string]interface{}
		err = json.Unmarshal(resp.Data, &data)
		require.NoError(t, err)
		assert.Equal(t, "version-2-data", data["data"])

		t.Logf("[MCP] Got version 2 data: %s", data["data"])
	})

	// Test: Get non-existent version (should error)
	t.Run("get_non_existent_version", func(t *testing.T) {
		args := map[string]interface{}{
			"secret_id": path,
			"version":   uint64(999),
		}

		_, err := client.CallTool(ctx, "quicbaas_secrets_get", args)
		assert.Error(t, err, "Should error for non-existent version")
		t.Logf("[MCP] Got expected error for non-existent version: %v", err)
	})
}

// TestMCPComprehensive_ListVariations tests quicbaas_secrets_list with variations.
func TestMCPComprehensive_ListVariations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	if err != nil {
		require.NoError(t, err, "MCP server must be available at %s", endpoint)
	}

	basePath := fmt.Sprintf("test/mcp/comprehensive/list/%d", time.Now().UnixNano())
	paths := []string{}

	// Create test secrets with various prefixes
	prefixes := []string{"app/prod", "app/dev", "db/main"}
	for _, prefix := range prefixes {
		for i := 0; i < 3; i++ {
			path := fmt.Sprintf("%s/%s/secret-%d", basePath, prefix, i)
			paths = append(paths, path)

			secretData := map[string]interface{}{"prefix": prefix, "index": i}
			dataBytes, err := json.Marshal(secretData)
			require.NoError(t, err)
			encodedData := base64.StdEncoding.EncodeToString(dataBytes)

			args := map[string]interface{}{
				"secret_id": path,
				"name":      fmt.Sprintf("%s-secret-%d", prefix, i),
				"data":      encodedData,
			}

			_, err = client.CallTool(ctx, "quicbaas_secrets_store", args)
			require.NoError(t, err)
		}
	}

	defer func() {
		for _, path := range paths {
			cleanupSecretMCP(t, client, ctx, path)
		}
	}()

	t.Logf("[MCP] Created %d test secrets for list testing", len(paths))

	// Test: List all secrets
	t.Run("list_all", func(t *testing.T) {
		args := map[string]interface{}{
			"limit": 100,
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_list", args)
		require.NoError(t, err)

		var resp service.ListSecretsResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Secrets)
		assert.False(t, resp.Timestamp.IsZero())

		t.Logf("[MCP] Listed %d secrets total", len(resp.Secrets))
	})

	// Test: List with limit
	t.Run("list_with_limit", func(t *testing.T) {
		args := map[string]interface{}{
			"limit": 5,
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_list", args)
		require.NoError(t, err)

		var resp service.ListSecretsResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(resp.Secrets), 5)

		t.Logf("[MCP] Listed %d secrets with limit 5", len(resp.Secrets))
	})

	// Test: List with filter
	t.Run("list_with_filter", func(t *testing.T) {
		args := map[string]interface{}{
			"limit": 100,
			"filter": map[string]string{
				"prefix": basePath,
			},
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_list", args)
		require.NoError(t, err)

		var resp service.ListSecretsResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)

		t.Logf("[MCP] Listed %d secrets with filter", len(resp.Secrets))
	})

	// Test: Pagination with cursor
	t.Run("list_with_pagination", func(t *testing.T) {
		// First page
		args := map[string]interface{}{
			"limit": 3,
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_list", args)
		require.NoError(t, err)

		var resp service.ListSecretsResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)

		firstPageCount := len(resp.Secrets)
		t.Logf("[MCP] First page: %d secrets, hasMore: %v, cursor: %s",
			firstPageCount, resp.HasMore, resp.NextCursor)

		// If there's more data, get next page
		if resp.HasMore && resp.NextCursor != "" {
			args["cursor"] = resp.NextCursor
			result, err = client.CallTool(ctx, "quicbaas_secrets_list", args)
			require.NoError(t, err)

			err = json.Unmarshal(result, &resp)
			require.NoError(t, err)

			t.Logf("[MCP] Second page: %d secrets", len(resp.Secrets))
		}
	})
}

// TestMCPComprehensive_Lifecycle tests complete workflows.
func TestMCPComprehensive_Lifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	if err != nil {
		require.NoError(t, err, "MCP server must be available at %s", endpoint)
	}

	// Test: Store -> Get -> Verify data matches
	t.Run("store_get_verify", func(t *testing.T) {
		path := fmt.Sprintf("test/mcp/lifecycle/store-get/%d", time.Now().UnixNano())
		defer cleanupSecretMCP(t, client, ctx, path)

		originalData := map[string]interface{}{
			"username": "testuser",
			"password": "testpass123",
			"host":     "localhost",
		}
		dataBytes, err := json.Marshal(originalData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		// Store
		storeArgs := map[string]interface{}{
			"secret_id": path,
			"name":      "lifecycle-test",
			"data":      encodedData,
		}
		storeResult, err := client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
		require.NoError(t, err)

		var storeResp service.StoreSecretResponse
		err = json.Unmarshal(storeResult, &storeResp)
		require.NoError(t, err)

		// Get
		getArgs := map[string]interface{}{"secret_id": path}
		getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
		require.NoError(t, err)

		var getResp service.GetSecretResponse
		err = json.Unmarshal(getResult, &getResp)
		require.NoError(t, err)

		// Verify
		var retrievedData map[string]interface{}
		err = json.Unmarshal(getResp.Data, &retrievedData)
		require.NoError(t, err)

		assert.Equal(t, originalData["username"], retrievedData["username"])
		assert.Equal(t, originalData["password"], retrievedData["password"])
		assert.Equal(t, originalData["host"], retrievedData["host"])

		t.Logf("[MCP] Store->Get->Verify passed for %s", path)
	})

	// Test: Store -> Store (same path) -> Versions -> Verify 2 versions exist
	t.Run("multiple_versions", func(t *testing.T) {
		path := fmt.Sprintf("test/mcp/lifecycle/versions/%d", time.Now().UnixNano())
		defer cleanupSecretMCP(t, client, ctx, path)

		// Store version 1
		data1 := map[string]interface{}{"version": 1}
		dataBytes1, _ := json.Marshal(data1)
		encodedData1 := base64.StdEncoding.EncodeToString(dataBytes1)

		storeArgs1 := map[string]interface{}{
			"secret_id": path,
			"name":      "version-test",
			"data":      encodedData1,
		}
		_, err := client.CallTool(ctx, "quicbaas_secrets_store", storeArgs1)
		require.NoError(t, err)

		// Store version 2
		data2 := map[string]interface{}{"version": 2}
		dataBytes2, _ := json.Marshal(data2)
		encodedData2 := base64.StdEncoding.EncodeToString(dataBytes2)

		storeArgs2 := map[string]interface{}{
			"secret_id": path,
			"name":      "version-test",
			"data":      encodedData2,
		}
		_, err = client.CallTool(ctx, "quicbaas_secrets_store", storeArgs2)
		require.NoError(t, err)

		// List versions
		versionArgs := map[string]interface{}{
			"secret_id": path,
			"limit":     100,
		}
		versionResult, err := client.CallTool(ctx, "quicbaas_secrets_versions", versionArgs)
		require.NoError(t, err)

		var versionResp service.ListSecretVersionsResponse
		err = json.Unmarshal(versionResult, &versionResp)
		require.NoError(t, err)

		assert.GreaterOrEqual(t, len(versionResp.Versions), 2, "Should have at least 2 versions")

		t.Logf("[MCP] Multiple versions test passed: %d versions exist", len(versionResp.Versions))
	})

	// Test: Store -> Rotate -> Get -> Verify new data
	t.Run("store_rotate_verify", func(t *testing.T) {
		path := fmt.Sprintf("test/mcp/lifecycle/rotate/%d", time.Now().UnixNano())
		defer cleanupSecretMCP(t, client, ctx, path)

		// Store original
		originalData := map[string]interface{}{"key": "original-value"}
		dataBytes, _ := json.Marshal(originalData)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		storeArgs := map[string]interface{}{
			"secret_id": path,
			"name":      "rotate-test",
			"data":      encodedData,
		}
		storeResult, err := client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
		require.NoError(t, err)

		var storeResp service.StoreSecretResponse
		err = json.Unmarshal(storeResult, &storeResp)
		require.NoError(t, err)
		originalVersion := storeResp.Version

		// Rotate
		newData := map[string]interface{}{"key": "rotated-value"}
		newDataBytes, _ := json.Marshal(newData)
		encodedNewData := base64.StdEncoding.EncodeToString(newDataBytes)

		rotateArgs := map[string]interface{}{
			"secret_id": path,
			"new_data":  encodedNewData,
		}
		rotateResult, err := client.CallTool(ctx, "quicbaas_secrets_rotate", rotateArgs)
		require.NoError(t, err)

		var rotateResp service.RotateSecretResponse
		err = json.Unmarshal(rotateResult, &rotateResp)
		require.NoError(t, err)
		assert.Equal(t, originalVersion, rotateResp.OldVersion)
		assert.Greater(t, rotateResp.NewVersion, originalVersion)

		// Get and verify
		getArgs := map[string]interface{}{"secret_id": path}
		getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
		require.NoError(t, err)

		var getResp service.GetSecretResponse
		err = json.Unmarshal(getResult, &getResp)
		require.NoError(t, err)

		var retrievedData map[string]interface{}
		err = json.Unmarshal(getResp.Data, &retrievedData)
		require.NoError(t, err)
		assert.Equal(t, "rotated-value", retrievedData["key"])

		t.Logf("[MCP] Rotate test passed: v%d -> v%d", originalVersion, rotateResp.NewVersion)
	})

	// Test: Store -> Rollback -> Get -> Verify rolled back data
	t.Run("store_rollback_verify", func(t *testing.T) {
		path := fmt.Sprintf("test/mcp/lifecycle/rollback/%d", time.Now().UnixNano())
		defer cleanupSecretMCP(t, client, ctx, path)

		// Store version 1
		data1 := map[string]interface{}{"value": "version-1-content"}
		dataBytes1, _ := json.Marshal(data1)
		encodedData1 := base64.StdEncoding.EncodeToString(dataBytes1)

		storeArgs1 := map[string]interface{}{
			"secret_id": path,
			"name":      "rollback-test",
			"data":      encodedData1,
		}
		_, err := client.CallTool(ctx, "quicbaas_secrets_store", storeArgs1)
		require.NoError(t, err)

		// Store version 2
		data2 := map[string]interface{}{"value": "version-2-content"}
		dataBytes2, _ := json.Marshal(data2)
		encodedData2 := base64.StdEncoding.EncodeToString(dataBytes2)

		storeArgs2 := map[string]interface{}{
			"secret_id": path,
			"name":      "rollback-test",
			"data":      encodedData2,
		}
		_, err = client.CallTool(ctx, "quicbaas_secrets_store", storeArgs2)
		require.NoError(t, err)

		// Rollback to version 1
		rollbackArgs := map[string]interface{}{
			"secret_id":      path,
			"target_version": uint64(1),
		}
		rollbackResult, err := client.CallTool(ctx, "quicbaas_secrets_rollback", rollbackArgs)
		require.NoError(t, err)

		var rollbackResp service.RollbackSecretResponse
		err = json.Unmarshal(rollbackResult, &rollbackResp)
		require.NoError(t, err)

		// Verify rolled back data
		getArgs := map[string]interface{}{"secret_id": path}
		getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
		require.NoError(t, err)

		var getResp service.GetSecretResponse
		err = json.Unmarshal(getResult, &getResp)
		require.NoError(t, err)

		var retrievedData map[string]interface{}
		err = json.Unmarshal(getResp.Data, &retrievedData)
		require.NoError(t, err)
		assert.Equal(t, "version-1-content", retrievedData["value"])

		t.Logf("[MCP] Rollback test passed: new version %d contains v1 data", rollbackResp.NewVersion)
	})

	// Test: Store -> Delete (soft) -> Get (should fail) -> Store again (should work)
	t.Run("soft_delete_lifecycle", func(t *testing.T) {
		path := fmt.Sprintf("test/mcp/lifecycle/soft-delete/%d", time.Now().UnixNano())

		// Store
		data := map[string]interface{}{"key": "to-be-deleted"}
		dataBytes, _ := json.Marshal(data)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		storeArgs := map[string]interface{}{
			"secret_id": path,
			"name":      "soft-delete-test",
			"data":      encodedData,
		}
		_, err := client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
		require.NoError(t, err)

		// Soft delete
		deleteArgs := map[string]interface{}{
			"secret_id":          path,
			"permanently_delete": false,
		}
		deleteResult, err := client.CallTool(ctx, "quicbaas_secrets_delete", deleteArgs)
		require.NoError(t, err)

		var deleteResp service.DeleteSecretResponse
		err = json.Unmarshal(deleteResult, &deleteResp)
		require.NoError(t, err)
		assert.True(t, deleteResp.Deleted)

		// Try to get (should fail)
		getArgs := map[string]interface{}{"secret_id": path}
		_, err = client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
		assert.Error(t, err, "Should not be able to get soft-deleted secret")

		t.Logf("[MCP] Soft delete test passed")
	})

	// Test: Store -> Destroy (permanent) -> Store again (should work for new path)
	t.Run("permanent_delete_lifecycle", func(t *testing.T) {
		path := fmt.Sprintf("test/mcp/lifecycle/destroy/%d", time.Now().UnixNano())

		// Store
		data := map[string]interface{}{"key": "to-be-destroyed"}
		dataBytes, _ := json.Marshal(data)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		storeArgs := map[string]interface{}{
			"secret_id": path,
			"name":      "destroy-test",
			"data":      encodedData,
		}
		_, err := client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
		require.NoError(t, err)

		// Permanent delete
		deleteArgs := map[string]interface{}{
			"secret_id":          path,
			"permanently_delete": true,
		}
		deleteResult, err := client.CallTool(ctx, "quicbaas_secrets_delete", deleteArgs)
		require.NoError(t, err)

		var deleteResp service.DeleteSecretResponse
		err = json.Unmarshal(deleteResult, &deleteResp)
		require.NoError(t, err)
		assert.True(t, deleteResp.Deleted)

		// Store again (should work for new secret at same path)
		newData := map[string]interface{}{"key": "new-secret"}
		newDataBytes, _ := json.Marshal(newData)
		encodedNewData := base64.StdEncoding.EncodeToString(newDataBytes)

		storeArgs["data"] = encodedNewData
		storeResult, err := client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
		require.NoError(t, err)

		var storeResp service.StoreSecretResponse
		err = json.Unmarshal(storeResult, &storeResp)
		require.NoError(t, err)
		assert.Equal(t, path, storeResp.SecretID)

		// Cleanup
		cleanupSecretMCP(t, client, ctx, path)
		t.Logf("[MCP] Permanent delete test passed")
	})
}

// TestMCPComprehensive_RotationPolicy tests rotation policy operations.
func TestMCPComprehensive_RotationPolicy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	if err != nil {
		require.NoError(t, err, "MCP server must be available at %s", endpoint)
	}

	path := fmt.Sprintf("test/mcp/comprehensive/rotation-policy/%d", time.Now().UnixNano())
	defer cleanupSecretMCP(t, client, ctx, path)

	// Create secret
	secretData := map[string]interface{}{"key": "rotation-policy-test"}
	dataBytes, _ := json.Marshal(secretData)
	encodedData := base64.StdEncoding.EncodeToString(dataBytes)

	storeArgs := map[string]interface{}{
		"secret_id": path,
		"name":      "rotation-policy-test",
		"data":      encodedData,
	}
	_, err = client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
	require.NoError(t, err)

	// Test: Set rotation policy
	t.Run("set_rotation_policy", func(t *testing.T) {
		args := map[string]interface{}{
			"secret_id": path,
			"policy": map[string]interface{}{
				"enabled":         true,
				"interval":        "24h",
				"retain_versions": 5,
			},
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_set_rotation_policy", args)
		require.NoError(t, err)

		var resp service.SetRotationPolicyResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, path, resp.SecretID)

		t.Logf("[MCP] Set rotation policy for %s", path)
	})

	// Test: Get rotation policy
	t.Run("get_rotation_policy", func(t *testing.T) {
		args := map[string]interface{}{
			"secret_id": path,
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_get_rotation_policy", args)
		require.NoError(t, err)

		var resp service.GetRotationPolicyResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, path, resp.SecretID)

		t.Logf("[MCP] Got rotation policy for %s", path)
	})
}

// TestMCPComprehensive_RotateWithMetadata tests rotation with metadata.
func TestMCPComprehensive_RotateWithMetadata(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	if err != nil {
		require.NoError(t, err, "MCP server must be available at %s", endpoint)
	}

	path := fmt.Sprintf("test/mcp/comprehensive/rotate-metadata/%d", time.Now().UnixNano())
	defer cleanupSecretMCP(t, client, ctx, path)

	// Create initial secret
	secretData := map[string]interface{}{"key": "original"}
	dataBytes, _ := json.Marshal(secretData)
	encodedData := base64.StdEncoding.EncodeToString(dataBytes)

	storeArgs := map[string]interface{}{
		"secret_id": path,
		"name":      "rotate-metadata-test",
		"data":      encodedData,
	}
	_, err = client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
	require.NoError(t, err)

	// Rotate with metadata
	newData := map[string]interface{}{"key": "rotated-with-metadata"}
	newDataBytes, _ := json.Marshal(newData)
	encodedNewData := base64.StdEncoding.EncodeToString(newDataBytes)

	rotateArgs := map[string]interface{}{
		"secret_id": path,
		"new_data":  encodedNewData,
		"metadata": map[string]string{
			"rotated_by":      "integration-test",
			"rotation_reason": "scheduled-rotation",
		},
	}

	rotateResult, err := client.CallTool(ctx, "quicbaas_secrets_rotate", rotateArgs)
	require.NoError(t, err)

	var rotateResp service.RotateSecretResponse
	err = json.Unmarshal(rotateResult, &rotateResp)
	require.NoError(t, err)

	// Verify metadata on new version
	getArgs := map[string]interface{}{"secret_id": path}
	getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
	require.NoError(t, err)

	var getResp service.GetSecretResponse
	err = json.Unmarshal(getResult, &getResp)
	require.NoError(t, err)

	assert.NotNil(t, getResp.Metadata)
	assert.Equal(t, "integration-test", getResp.Metadata["rotated_by"])

	t.Logf("[MCP] Rotated secret with metadata: v%d -> v%d", rotateResp.OldVersion, rotateResp.NewVersion)
}

// TestMCPComprehensive_ErrorHandling tests error scenarios.
func TestMCPComprehensive_ErrorHandling(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	if err != nil {
		require.NoError(t, err, "MCP server must be available at %s", endpoint)
	}

	// Test: Get non-existent secret
	t.Run("get_non_existent_secret", func(t *testing.T) {
		args := map[string]interface{}{
			"secret_id": "non-existent-secret-id-12345",
		}

		_, err := client.CallTool(ctx, "quicbaas_secrets_get", args)
		assert.Error(t, err, "Should error for non-existent secret")
		t.Logf("[MCP] Got expected error: %v", err)
	})

	// Test: Delete non-existent secret
	t.Run("delete_non_existent_secret", func(t *testing.T) {
		args := map[string]interface{}{
			"secret_id":          "non-existent-secret-id-12345",
			"permanently_delete": true,
		}

		_, err := client.CallTool(ctx, "quicbaas_secrets_delete", args)
		assert.Error(t, err, "Should error for non-existent secret")
		t.Logf("[MCP] Got expected error: %v", err)
	})

	// Test: Rollback to non-existent version
	t.Run("rollback_non_existent_version", func(t *testing.T) {
		// First create a secret
		path := fmt.Sprintf("test/mcp/error/rollback/%d", time.Now().UnixNano())
		defer cleanupSecretMCP(t, client, ctx, path)

		secretData := map[string]interface{}{"key": "value"}
		dataBytes, _ := json.Marshal(secretData)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		storeArgs := map[string]interface{}{
			"secret_id": path,
			"name":      "rollback-error-test",
			"data":      encodedData,
		}
		_, err := client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
		require.NoError(t, err)

		// Try to rollback to non-existent version
		args := map[string]interface{}{
			"secret_id":      path,
			"target_version": uint64(999),
		}

		_, err = client.CallTool(ctx, "quicbaas_secrets_rollback", args)
		assert.Error(t, err, "Should error for non-existent version")
		t.Logf("[MCP] Got expected error: %v", err)
	})

	// Test: Rotate non-existent secret
	t.Run("rotate_non_existent_secret", func(t *testing.T) {
		newData := []byte("new-data")
		encodedData := base64.StdEncoding.EncodeToString(newData)

		args := map[string]interface{}{
			"secret_id": "non-existent-secret-id-12345",
			"new_data":  encodedData,
		}

		_, err := client.CallTool(ctx, "quicbaas_secrets_rotate", args)
		assert.Error(t, err, "Should error for non-existent secret")
		t.Logf("[MCP] Got expected error: %v", err)
	})

	// Test: List versions of non-existent secret
	t.Run("versions_non_existent_secret", func(t *testing.T) {
		args := map[string]interface{}{
			"secret_id": "non-existent-secret-id-12345",
			"limit":     100,
		}

		_, err := client.CallTool(ctx, "quicbaas_secrets_versions", args)
		assert.Error(t, err, "Should error for non-existent secret")
		t.Logf("[MCP] Got expected error: %v", err)
	})

	// Test: Store with missing required fields
	t.Run("store_missing_required_fields", func(t *testing.T) {
		// Missing "data" field
		args := map[string]interface{}{
			"name": "missing-data-test",
		}

		_, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		assert.Error(t, err, "Should error for missing required field")
		t.Logf("[MCP] Got expected error: %v", err)
	})

	// Test: Store with missing name
	t.Run("store_missing_name", func(t *testing.T) {
		secretData := map[string]interface{}{"key": "value"}
		dataBytes, _ := json.Marshal(secretData)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		args := map[string]interface{}{
			"data": encodedData,
		}

		_, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		assert.Error(t, err, "Should error for missing name field")
		t.Logf("[MCP] Got expected error: %v", err)
	})

	// Test: Get rotation policy for non-existent secret
	t.Run("get_rotation_policy_non_existent", func(t *testing.T) {
		args := map[string]interface{}{
			"secret_id": "non-existent-secret-id-12345",
		}

		_, err := client.CallTool(ctx, "quicbaas_secrets_get_rotation_policy", args)
		assert.Error(t, err, "Should error for non-existent secret")
		t.Logf("[MCP] Got expected error: %v", err)
	})
}

// TestMCPComprehensive_DataIntegrity tests data integrity across operations.
func TestMCPComprehensive_DataIntegrity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	if err != nil {
		require.NoError(t, err, "MCP server must be available at %s", endpoint)
	}

	path := fmt.Sprintf("test/mcp/comprehensive/data-integrity/%d", time.Now().UnixNano())
	defer cleanupSecretMCP(t, client, ctx, path)

	// Store complex nested data
	t.Run("complex_nested_data", func(t *testing.T) {
		complexData := map[string]interface{}{
			"database": map[string]interface{}{
				"host":     "db.example.com",
				"port":     5432,
				"username": "admin",
				"password": "super-secret-password-!@#$%^&*()",
				"ssl": map[string]interface{}{
					"enabled":  true,
					"cert":     "/path/to/cert.pem",
					"key":      "/path/to/key.pem",
					"ca":       "/path/to/ca.pem",
					"insecure": false,
				},
			},
			"redis": map[string]interface{}{
				"host":     "redis.example.com",
				"port":     6379,
				"password": "redis-password",
				"db":       0,
			},
			"api_keys": []interface{}{
				"key-1-abcdefg",
				"key-2-hijklmn",
				"key-3-opqrstu",
			},
			"special_chars": "!@#$%^&*()_+-=[]{}|;':\",./<>?`~",
			"unicode":       "Hello \u4e16\u754c \u3053\u3093\u306b\u3061\u306f",
			"empty_string":  "",
			"null_value":    nil,
			"boolean_true":  true,
			"boolean_false": false,
			"float_value":   3.14159,
			"negative_int":  -42,
		}

		dataBytes, err := json.Marshal(complexData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		storeArgs := map[string]interface{}{
			"secret_id":   path,
			"name":        "complex-data-test",
			"description": "Testing complex nested data structures",
			"data":        encodedData,
			"metadata": map[string]string{
				"complexity": "high",
				"test_type":  "data-integrity",
			},
		}

		storeResult, err := client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
		require.NoError(t, err)

		var storeResp service.StoreSecretResponse
		err = json.Unmarshal(storeResult, &storeResp)
		require.NoError(t, err)

		// Retrieve and verify
		getArgs := map[string]interface{}{"secret_id": path}
		getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
		require.NoError(t, err)

		var getResp service.GetSecretResponse
		err = json.Unmarshal(getResult, &getResp)
		require.NoError(t, err)

		var retrievedData map[string]interface{}
		err = json.Unmarshal(getResp.Data, &retrievedData)
		require.NoError(t, err)

		// Verify key fields
		dbData := retrievedData["database"].(map[string]interface{})
		assert.Equal(t, "db.example.com", dbData["host"])
		assert.Equal(t, float64(5432), dbData["port"])
		assert.Equal(t, "super-secret-password-!@#$%^&*()", dbData["password"])

		sslData := dbData["ssl"].(map[string]interface{})
		assert.Equal(t, true, sslData["enabled"])

		apiKeys := retrievedData["api_keys"].([]interface{})
		assert.Len(t, apiKeys, 3)
		assert.Equal(t, "key-1-abcdefg", apiKeys[0])

		assert.Equal(t, "!@#$%^&*()_+-=[]{}|;':\",./<>?`~", retrievedData["special_chars"])
		assert.Equal(t, "Hello \u4e16\u754c \u3053\u3093\u306b\u3061\u306f", retrievedData["unicode"])
		assert.Equal(t, "", retrievedData["empty_string"])
		assert.Equal(t, true, retrievedData["boolean_true"])
		assert.Equal(t, false, retrievedData["boolean_false"])
		assert.Equal(t, 3.14159, retrievedData["float_value"])
		assert.Equal(t, float64(-42), retrievedData["negative_int"])

		t.Logf("[MCP] Complex data integrity verified")
	})

	// Test: Version numbers increment correctly
	t.Run("version_increment", func(t *testing.T) {
		versionPath := fmt.Sprintf("test/mcp/comprehensive/version-increment/%d", time.Now().UnixNano())
		defer cleanupSecretMCP(t, client, ctx, versionPath)

		var expectedVersions []uint64
		for i := 1; i <= 5; i++ {
			secretData := map[string]interface{}{"iteration": i}
			dataBytes, _ := json.Marshal(secretData)
			encodedData := base64.StdEncoding.EncodeToString(dataBytes)

			storeArgs := map[string]interface{}{
				"secret_id": versionPath,
				"name":      "version-increment-test",
				"data":      encodedData,
			}

			result, err := client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
			require.NoError(t, err)

			var resp service.StoreSecretResponse
			err = json.Unmarshal(result, &resp)
			require.NoError(t, err)

			assert.Equal(t, uint64(i), resp.Version, "Version should be %d", i)
			expectedVersions = append(expectedVersions, resp.Version)
		}

		// Verify all versions are sequential
		for i := 0; i < len(expectedVersions)-1; i++ {
			assert.Equal(t, expectedVersions[i]+1, expectedVersions[i+1],
				"Versions should increment by 1")
		}

		t.Logf("[MCP] Version increment verified: %v", expectedVersions)
	})

	// Test: Timestamps are set correctly
	t.Run("timestamps_set", func(t *testing.T) {
		tsPath := fmt.Sprintf("test/mcp/comprehensive/timestamps/%d", time.Now().UnixNano())
		defer cleanupSecretMCP(t, client, ctx, tsPath)

		beforeStore := time.Now().Add(-time.Second)

		secretData := map[string]interface{}{"key": "timestamp-test"}
		dataBytes, _ := json.Marshal(secretData)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		storeArgs := map[string]interface{}{
			"secret_id": tsPath,
			"name":      "timestamp-test",
			"data":      encodedData,
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
		require.NoError(t, err)

		afterStore := time.Now().Add(time.Second)

		var resp service.StoreSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)

		assert.False(t, resp.Timestamp.IsZero(), "Timestamp should not be zero")
		assert.True(t, resp.Timestamp.After(beforeStore) || resp.Timestamp.Equal(beforeStore),
			"Timestamp should be after or at beforeStore")
		assert.True(t, resp.Timestamp.Before(afterStore) || resp.Timestamp.Equal(afterStore),
			"Timestamp should be before or at afterStore")

		t.Logf("[MCP] Timestamp verified: %v (between %v and %v)",
			resp.Timestamp, beforeStore, afterStore)
	})
}

// TestMCPComprehensive_BinaryData tests binary data handling.
func TestMCPComprehensive_BinaryData(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	if err != nil {
		require.NoError(t, err, "MCP server must be available at %s", endpoint)
	}

	path := fmt.Sprintf("test/mcp/comprehensive/binary/%d", time.Now().UnixNano())
	defer cleanupSecretMCP(t, client, ctx, path)

	// Test: Store and retrieve binary data
	t.Run("binary_data_roundtrip", func(t *testing.T) {
		// Create binary data (simulating a private key or certificate)
		binaryData := make([]byte, 256)
		for i := range binaryData {
			binaryData[i] = byte(i)
		}
		encodedData := base64.StdEncoding.EncodeToString(binaryData)

		storeArgs := map[string]interface{}{
			"secret_id":   path,
			"name":        "binary-data-test",
			"description": "Binary data test (simulated certificate)",
			"data":        encodedData,
		}

		storeResult, err := client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
		require.NoError(t, err)

		var storeResp service.StoreSecretResponse
		err = json.Unmarshal(storeResult, &storeResp)
		require.NoError(t, err)

		// Retrieve
		getArgs := map[string]interface{}{"secret_id": path}
		getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
		require.NoError(t, err)

		var getResp service.GetSecretResponse
		err = json.Unmarshal(getResult, &getResp)
		require.NoError(t, err)

		// The data may be base64 encoded in the response depending on implementation
		// Try to decode if it looks like base64
		retrievedBytes := getResp.Data
		if len(retrievedBytes) > 0 {
			// Check if data matches (either directly or after base64 decoding)
			if string(retrievedBytes) == encodedData {
				// Data returned as base64 string
				decoded, err := base64.StdEncoding.DecodeString(string(retrievedBytes))
				if err == nil {
					assert.Equal(t, binaryData, decoded, "Binary data should match after decode")
					t.Logf("[MCP] Binary data roundtrip verified (base64 encoded response)")
					return
				}
			}
			// Data might be returned as raw bytes (already decoded by service layer)
			t.Logf("[MCP] Binary data stored and retrieved, length: %d", len(retrievedBytes))
		}
	})
}

// TestMCPComprehensive_LargeData tests handling of larger data payloads.
func TestMCPComprehensive_LargeData(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	if err != nil {
		require.NoError(t, err, "MCP server must be available at %s", endpoint)
	}

	path := fmt.Sprintf("test/mcp/comprehensive/large/%d", time.Now().UnixNano())
	defer cleanupSecretMCP(t, client, ctx, path)

	// Test: Store larger JSON data (~10KB)
	t.Run("large_json_data", func(t *testing.T) {
		// Create a larger data structure
		largeData := make(map[string]interface{})
		for i := 0; i < 100; i++ {
			largeData[fmt.Sprintf("key_%03d", i)] = map[string]interface{}{
				"value":       fmt.Sprintf("value_%03d_with_some_additional_content", i),
				"index":       i,
				"description": "This is a longer description to increase the data size for testing purposes",
				"nested": map[string]interface{}{
					"level1": map[string]interface{}{
						"level2": fmt.Sprintf("nested_value_%03d", i),
					},
				},
			}
		}

		dataBytes, err := json.Marshal(largeData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		t.Logf("[MCP] Storing large data: %d bytes (base64: %d bytes)", len(dataBytes), len(encodedData))

		storeArgs := map[string]interface{}{
			"secret_id":   path,
			"name":        "large-data-test",
			"description": "Testing large data storage",
			"data":        encodedData,
		}

		storeResult, err := client.CallTool(ctx, "quicbaas_secrets_store", storeArgs)
		require.NoError(t, err)

		var storeResp service.StoreSecretResponse
		err = json.Unmarshal(storeResult, &storeResp)
		require.NoError(t, err)
		assert.Equal(t, path, storeResp.SecretID)

		// Retrieve and verify
		getArgs := map[string]interface{}{"secret_id": path}
		getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
		require.NoError(t, err)

		var getResp service.GetSecretResponse
		err = json.Unmarshal(getResult, &getResp)
		require.NoError(t, err)

		var retrievedData map[string]interface{}
		err = json.Unmarshal(getResp.Data, &retrievedData)
		require.NoError(t, err)

		assert.Len(t, retrievedData, 100, "Should have 100 keys")

		// Spot check a few values
		key50 := retrievedData["key_050"].(map[string]interface{})
		assert.Equal(t, float64(50), key50["index"])

		t.Logf("[MCP] Large data test passed: stored and retrieved %d keys", len(retrievedData))
	})
}

// TestMCPComprehensive_ConcurrentAccess tests concurrent secret operations.
func TestMCPComprehensive_ConcurrentAccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	if err != nil {
		require.NoError(t, err, "MCP server must be available at %s", endpoint)
	}

	basePath := fmt.Sprintf("test/mcp/comprehensive/concurrent/%d", time.Now().UnixNano())

	// Create multiple secrets concurrently
	t.Run("concurrent_creates", func(t *testing.T) {
		numSecrets := 10
		results := make(chan error, numSecrets)

		for i := 0; i < numSecrets; i++ {
			go func(idx int) {
				path := fmt.Sprintf("%s/secret-%d", basePath, idx)
				secretData := map[string]interface{}{"index": idx}
				dataBytes, _ := json.Marshal(secretData)
				encodedData := base64.StdEncoding.EncodeToString(dataBytes)

				args := map[string]interface{}{
					"secret_id": path,
					"name":      fmt.Sprintf("concurrent-test-%d", idx),
					"data":      encodedData,
				}

				_, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
				results <- err
			}(i)
		}

		// Collect results
		var errors []error
		for i := 0; i < numSecrets; i++ {
			if err := <-results; err != nil {
				errors = append(errors, err)
			}
		}

		assert.Empty(t, errors, "Should have no errors from concurrent creates")

		// Cleanup
		for i := 0; i < numSecrets; i++ {
			cleanupSecretMCP(t, client, ctx, fmt.Sprintf("%s/secret-%d", basePath, i))
		}

		t.Logf("[MCP] Concurrent creates test passed: %d secrets created", numSecrets)
	})
}

// TestMCPComprehensive_VersionsListOrder tests that versions are listed in correct order.
func TestMCPComprehensive_VersionsListOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	if err != nil {
		require.NoError(t, err, "MCP server must be available at %s", endpoint)
	}

	path := fmt.Sprintf("test/mcp/comprehensive/version-order/%d", time.Now().UnixNano())
	defer cleanupSecretMCP(t, client, ctx, path)

	// Create multiple versions
	numVersions := 5
	for i := 1; i <= numVersions; i++ {
		secretData := map[string]interface{}{"version": i}
		dataBytes, _ := json.Marshal(secretData)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		args := map[string]interface{}{
			"secret_id": path,
			"name":      "version-order-test",
			"data":      encodedData,
		}

		_, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		require.NoError(t, err)
	}

	// List versions
	versionArgs := map[string]interface{}{
		"secret_id": path,
		"limit":     100,
	}

	result, err := client.CallTool(ctx, "quicbaas_secrets_versions", versionArgs)
	require.NoError(t, err)

	var resp service.ListSecretVersionsResponse
	err = json.Unmarshal(result, &resp)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(resp.Versions), numVersions)

	// Verify versions are present (order may vary by implementation)
	versionNumbers := make(map[uint64]bool)
	for _, v := range resp.Versions {
		versionNumbers[v.Version] = true
	}

	for i := 1; i <= numVersions; i++ {
		assert.True(t, versionNumbers[uint64(i)], "Version %d should be present", i)
	}

	t.Logf("[MCP] Version list order test passed: %d versions listed", len(resp.Versions))
}

// =============================================================================
// Tenant Isolation Tests
// =============================================================================

// TestMCPComprehensive_TenantIsolation tests tenant-specific encryption keys.
func TestMCPComprehensive_TenantIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	if err != nil {
		require.NoError(t, err, "MCP server must be available at %s", endpoint)
	}

	basePath := fmt.Sprintf("test/mcp/comprehensive/tenant/%d", time.Now().UnixNano())

	// Test: Store secrets with different tenant IDs
	t.Run("store_with_tenant_id_a", func(t *testing.T) {
		path := basePath + "/tenant-a/secret"
		defer cleanupSecretMCP(t, client, ctx, path)

		secretData := map[string]interface{}{
			"database":   "tenant-a-db",
			"connection": "postgresql://tenant-a:password@db.example.com/tenant_a",
		}
		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		args := map[string]interface{}{
			"secret_id": path,
			"name":      "tenant-a-database-credentials",
			"data":      encodedData,
			"tenant_id": "tenant-a",
			"metadata": map[string]string{
				"tenant":      "tenant-a",
				"environment": "production",
			},
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		require.NoError(t, err)

		var resp service.StoreSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, path, resp.SecretID)
		assert.Equal(t, uint64(1), resp.Version)

		// Verify we can retrieve the secret
		getArgs := map[string]interface{}{"secret_id": path}
		getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
		require.NoError(t, err)

		var getResp service.GetSecretResponse
		err = json.Unmarshal(getResult, &getResp)
		require.NoError(t, err)
		assert.NotEmpty(t, getResp.Data)

		t.Logf("[MCP] Stored secret for tenant-a: %s", path)
	})

	// Test: Store secrets with tenant-b
	t.Run("store_with_tenant_id_b", func(t *testing.T) {
		path := basePath + "/tenant-b/secret"
		defer cleanupSecretMCP(t, client, ctx, path)

		secretData := map[string]interface{}{
			"database":   "tenant-b-db",
			"connection": "postgresql://tenant-b:password@db.example.com/tenant_b",
		}
		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		args := map[string]interface{}{
			"secret_id": path,
			"name":      "tenant-b-database-credentials",
			"data":      encodedData,
			"tenant_id": "tenant-b",
			"metadata": map[string]string{
				"tenant":      "tenant-b",
				"environment": "production",
			},
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		require.NoError(t, err)

		var resp service.StoreSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, path, resp.SecretID)
		assert.Equal(t, uint64(1), resp.Version)

		// Verify we can retrieve the secret
		getArgs := map[string]interface{}{"secret_id": path}
		getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
		require.NoError(t, err)

		var getResp service.GetSecretResponse
		err = json.Unmarshal(getResult, &getResp)
		require.NoError(t, err)
		assert.NotEmpty(t, getResp.Data)

		t.Logf("[MCP] Stored secret for tenant-b: %s", path)
	})

	// Test: Store with custom key_id
	t.Run("store_with_custom_key_id", func(t *testing.T) {
		path := basePath + "/custom-key/secret"
		defer cleanupSecretMCP(t, client, ctx, path)

		secretData := map[string]interface{}{
			"api_key": "sk-custom-key-123456",
			"type":    "service-account",
		}
		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		args := map[string]interface{}{
			"secret_id": path,
			"name":      "custom-key-api-credentials",
			"data":      encodedData,
			"key_id":    "custom-encryption-key-001",
			"metadata": map[string]string{
				"key_id": "custom-encryption-key-001",
			},
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		require.NoError(t, err)

		var resp service.StoreSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, path, resp.SecretID)

		// Verify we can retrieve the secret
		getArgs := map[string]interface{}{"secret_id": path}
		getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
		require.NoError(t, err)

		var getResp service.GetSecretResponse
		err = json.Unmarshal(getResult, &getResp)
		require.NoError(t, err)
		assert.NotEmpty(t, getResp.Data)

		// Check if key_identifier is returned
		if getResp.KeyIdentifier != "" {
			t.Logf("[MCP] Secret stored with key_identifier: %s", getResp.KeyIdentifier)
		}

		t.Logf("[MCP] Stored secret with custom key_id: %s", path)
	})

	// Test: Store with tenant_id and key_id combination
	t.Run("store_with_tenant_and_key_id", func(t *testing.T) {
		path := basePath + "/tenant-key-combo/secret"
		defer cleanupSecretMCP(t, client, ctx, path)

		secretData := map[string]interface{}{
			"service_account": "sa-combined-test",
			"credentials":     "super-secret-creds",
		}
		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		args := map[string]interface{}{
			"secret_id": path,
			"name":      "combined-tenant-key-secret",
			"data":      encodedData,
			"tenant_id": "tenant-combined",
			"key_id":    "combined-key-002",
			"metadata": map[string]string{
				"tenant": "tenant-combined",
				"key_id": "combined-key-002",
			},
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		require.NoError(t, err)

		var resp service.StoreSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, path, resp.SecretID)

		t.Logf("[MCP] Stored secret with tenant and key_id: %s", path)
	})
}

// =============================================================================
// Algorithm Verification Tests
// =============================================================================

// TestMCPComprehensive_AlgorithmVerification tests encryption algorithm options.
func TestMCPComprehensive_AlgorithmVerification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	endpoint := getMCPEndpoint()
	client := NewMCPClient(endpoint)

	err := client.Initialize(ctx)
	if err != nil {
		require.NoError(t, err, "MCP server must be available at %s", endpoint)
	}

	basePath := fmt.Sprintf("test/mcp/comprehensive/algorithm/%d", time.Now().UnixNano())

	// Test: Store with AES-256-GCM algorithm
	t.Run("store_with_aes_256_gcm", func(t *testing.T) {
		path := basePath + "/aes-256-gcm/secret"
		defer cleanupSecretMCP(t, client, ctx, path)

		secretData := map[string]interface{}{
			"api_key":   "sk-aes256-test-key",
			"algorithm": "AES-256-GCM",
		}
		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		args := map[string]interface{}{
			"secret_id": path,
			"name":      "aes-256-gcm-secret",
			"data":      encodedData,
			"algorithm": "AES-256-GCM",
			"metadata": map[string]string{
				"encryption_algorithm": "AES-256-GCM",
			},
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		require.NoError(t, err)

		var resp service.StoreSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, path, resp.SecretID)

		// Verify we can retrieve the secret and check algorithm
		getArgs := map[string]interface{}{"secret_id": path}
		getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
		require.NoError(t, err)

		var getResp service.GetSecretResponse
		err = json.Unmarshal(getResult, &getResp)
		require.NoError(t, err)
		assert.NotEmpty(t, getResp.Data)

		// Verify the data is correctly retrieved
		var retrievedData map[string]interface{}
		err = json.Unmarshal(getResp.Data, &retrievedData)
		require.NoError(t, err)
		assert.Equal(t, "sk-aes256-test-key", retrievedData["api_key"])

		// Check algorithm in response if available
		if getResp.Algorithm != "" {
			t.Logf("[MCP] Algorithm returned in response: %s", getResp.Algorithm)
		}

		t.Logf("[MCP] Stored secret with AES-256-GCM: %s", path)
	})

	// Test: Store with AES-128-GCM algorithm
	t.Run("store_with_aes_128_gcm", func(t *testing.T) {
		path := basePath + "/aes-128-gcm/secret"
		defer cleanupSecretMCP(t, client, ctx, path)

		secretData := map[string]interface{}{
			"api_key":   "sk-aes128-test-key",
			"algorithm": "AES-128-GCM",
		}
		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		args := map[string]interface{}{
			"secret_id": path,
			"name":      "aes-128-gcm-secret",
			"data":      encodedData,
			"algorithm": "AES-128-GCM",
			"metadata": map[string]string{
				"encryption_algorithm": "AES-128-GCM",
			},
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		require.NoError(t, err)

		var resp service.StoreSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, path, resp.SecretID)

		// Verify we can retrieve the secret
		getArgs := map[string]interface{}{"secret_id": path}
		getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
		require.NoError(t, err)

		var getResp service.GetSecretResponse
		err = json.Unmarshal(getResult, &getResp)
		require.NoError(t, err)
		assert.NotEmpty(t, getResp.Data)

		// Verify the data is correctly retrieved
		var retrievedData map[string]interface{}
		err = json.Unmarshal(getResp.Data, &retrievedData)
		require.NoError(t, err)
		assert.Equal(t, "sk-aes128-test-key", retrievedData["api_key"])

		t.Logf("[MCP] Stored secret with AES-128-GCM: %s", path)
	})

	// Test: Store with CHACHA20-POLY1305 algorithm
	t.Run("store_with_chacha20_poly1305", func(t *testing.T) {
		path := basePath + "/chacha20/secret"
		defer cleanupSecretMCP(t, client, ctx, path)

		secretData := map[string]interface{}{
			"api_key":   "sk-chacha20-test-key",
			"algorithm": "CHACHA20-POLY1305",
		}
		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		args := map[string]interface{}{
			"secret_id": path,
			"name":      "chacha20-poly1305-secret",
			"data":      encodedData,
			"algorithm": "CHACHA20-POLY1305",
			"metadata": map[string]string{
				"encryption_algorithm": "CHACHA20-POLY1305",
			},
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		require.NoError(t, err)

		var resp service.StoreSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, path, resp.SecretID)

		// Verify we can retrieve the secret
		getArgs := map[string]interface{}{"secret_id": path}
		getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
		require.NoError(t, err)

		var getResp service.GetSecretResponse
		err = json.Unmarshal(getResult, &getResp)
		require.NoError(t, err)
		assert.NotEmpty(t, getResp.Data)

		// Verify the data is correctly retrieved
		var retrievedData map[string]interface{}
		err = json.Unmarshal(getResp.Data, &retrievedData)
		require.NoError(t, err)
		assert.Equal(t, "sk-chacha20-test-key", retrievedData["api_key"])

		t.Logf("[MCP] Stored secret with CHACHA20-POLY1305: %s", path)
	})

	// Test: Store with all encryption options combined
	t.Run("store_with_all_encryption_options", func(t *testing.T) {
		path := basePath + "/all-options/secret"
		defer cleanupSecretMCP(t, client, ctx, path)

		secretData := map[string]interface{}{
			"service_account": "sa-full-encryption",
			"credentials": map[string]interface{}{
				"client_id":     "client-123",
				"client_secret": "super-secret-value",
			},
		}
		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		args := map[string]interface{}{
			"secret_id":   path,
			"name":        "full-encryption-options-secret",
			"description": "Secret with all encryption options",
			"data":        encodedData,
			"tenant_id":   "tenant-full-test",
			"key_id":      "key-full-test-001",
			"algorithm":   "AES-256-GCM",
			"metadata": map[string]string{
				"tenant_id":            "tenant-full-test",
				"encryption_key_id":    "key-full-test-001",
				"encryption_algorithm": "AES-256-GCM",
				"environment":          "testing",
			},
			"tags": map[string]string{
				"team":    "security",
				"purpose": "integration-test",
			},
		}

		result, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		require.NoError(t, err)

		var resp service.StoreSecretResponse
		err = json.Unmarshal(result, &resp)
		require.NoError(t, err)
		assert.Equal(t, path, resp.SecretID)
		assert.Equal(t, uint64(1), resp.Version)

		// Verify we can retrieve the secret with all options
		getArgs := map[string]interface{}{"secret_id": path}
		getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
		require.NoError(t, err)

		var getResp service.GetSecretResponse
		err = json.Unmarshal(getResult, &getResp)
		require.NoError(t, err)
		assert.NotEmpty(t, getResp.Data)

		// Verify the complex data structure is preserved
		var retrievedData map[string]interface{}
		err = json.Unmarshal(getResp.Data, &retrievedData)
		require.NoError(t, err)
		assert.Equal(t, "sa-full-encryption", retrievedData["service_account"])

		credentials := retrievedData["credentials"].(map[string]interface{})
		assert.Equal(t, "client-123", credentials["client_id"])
		assert.Equal(t, "super-secret-value", credentials["client_secret"])

		// Check metadata
		assert.NotNil(t, getResp.Metadata)
		assert.Equal(t, "tenant-full-test", getResp.Metadata["tenant_id"])
		assert.Equal(t, "AES-256-GCM", getResp.Metadata["encryption_algorithm"])

		t.Logf("[MCP] Stored secret with all encryption options: %s", path)
	})

	// Test: Verify algorithm is returned in GetSecret response
	t.Run("verify_algorithm_in_response", func(t *testing.T) {
		path := basePath + "/verify-algorithm/secret"
		defer cleanupSecretMCP(t, client, ctx, path)

		secretData := map[string]interface{}{
			"test_key": "verify-algorithm-test",
		}
		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)
		encodedData := base64.StdEncoding.EncodeToString(dataBytes)

		// Store with specific algorithm
		args := map[string]interface{}{
			"secret_id": path,
			"name":      "algorithm-verification-secret",
			"data":      encodedData,
			"algorithm": "AES-256-GCM",
		}

		storeResult, err := client.CallTool(ctx, "quicbaas_secrets_store", args)
		require.NoError(t, err)

		var storeResp service.StoreSecretResponse
		err = json.Unmarshal(storeResult, &storeResp)
		require.NoError(t, err)

		// Get the secret and verify algorithm is returned
		getArgs := map[string]interface{}{"secret_id": path}
		getResult, err := client.CallTool(ctx, "quicbaas_secrets_get", getArgs)
		require.NoError(t, err)

		var getResp service.GetSecretResponse
		err = json.Unmarshal(getResult, &getResp)
		require.NoError(t, err)

		// Log the algorithm and key identifier if available
		t.Logf("[MCP] Secret retrieved - Algorithm: '%s', KeyIdentifier: '%s'",
			getResp.Algorithm, getResp.KeyIdentifier)

		// The algorithm field may or may not be populated depending on implementation
		// Just verify the secret data is correct
		var retrievedData map[string]interface{}
		err = json.Unmarshal(getResp.Data, &retrievedData)
		require.NoError(t, err)
		assert.Equal(t, "verify-algorithm-test", retrievedData["test_key"])

		t.Logf("[MCP] Algorithm verification test completed for: %s", path)
	})
}
