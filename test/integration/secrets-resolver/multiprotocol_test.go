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

package secretsresolver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	quicgo "github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	quicpkg "github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	"github.com/jeremyhahn/go-quicbaas/pkg/config/secrets"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// Multi-Protocol Interface
// =============================================================================

// ProtocolClient defines the interface for secret operations across different protocols.
// Each protocol adapter must implement this interface to enable multi-protocol testing.
type ProtocolClient interface {
	// StoreSecret stores a secret at the given path with the provided data.
	StoreSecret(ctx context.Context, path string, data []byte) error

	// GetSecret retrieves a secret by its path.
	GetSecret(ctx context.Context, path string) ([]byte, error)

	// DeleteSecret removes a secret at the given path.
	DeleteSecret(ctx context.Context, path string) error

	// Name returns the protocol name for logging and identification.
	Name() string

	// Setup initializes the client connection.
	Setup(t *testing.T, ctx context.Context) error

	// Teardown closes the client connection and cleans up resources.
	Teardown(t *testing.T) error
}

// minRequiredProtocols is the minimum number of protocols that must pass
// for the multiprotocol test to succeed. REST and gRPC should always
// pass, so at least 2 protocols are expected to succeed.
const minRequiredProtocols = 2

// =============================================================================
// Embedded Protocol Client
// =============================================================================

// ErrEmbeddedClientNotInitialized is returned when the embedded client is not initialized.
type ErrEmbeddedClientNotInitialized struct{}

func (e ErrEmbeddedClientNotInitialized) Error() string {
	return "embedded client not initialized"
}

// EmbeddedClient implements ProtocolClient for embedded SDK access.
type EmbeddedClient struct {
	client quicbaas.Client
}

// Name returns the protocol name.
func (c *EmbeddedClient) Name() string {
	return "Embedded"
}

// Setup initializes the embedded client using the SDK directly.
func (c *EmbeddedClient) Setup(t *testing.T, ctx context.Context) error {
	t.Helper()

	svc, err := helpers.GetEmbeddedService(t)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to get embedded service", Cause: err}
	}

	client, err := quicbaas.NewClient("", quicbaas.WithEmbedded(svc))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create embedded client", Cause: err}
	}

	c.client = client
	return nil
}

// Teardown closes the embedded client.
func (c *EmbeddedClient) Teardown(t *testing.T) error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// StoreSecret stores a secret via embedded SDK.
func (c *EmbeddedClient) StoreSecret(ctx context.Context, path string, data []byte) error {
	if c.client == nil {
		return ErrEmbeddedClientNotInitialized{}
	}

	// Parse the data as JSON to get the map for the SDK
	var dataMap map[string]interface{}
	if err := json.Unmarshal(data, &dataMap); err != nil {
		// If not valid JSON, store as raw data
		dataMap = map[string]interface{}{
			"data": string(data),
		}
	}

	_, err := c.client.Secrets().Store(ctx, &quicbaas.StoreSecretRequest{
		Path: path,
		Data: dataMap,
	})
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "storing secret via embedded SDK", Cause: err}
	}

	return nil
}

// GetSecret retrieves a secret via embedded SDK.
func (c *EmbeddedClient) GetSecret(ctx context.Context, path string) ([]byte, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	secret, err := c.client.Secrets().Get(ctx, path)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "NotFound") {
			return nil, &secrets.SecretNotFoundError{
				Path:     path,
				Provider: "Embedded",
			}
		}
		return nil, &qberrors.WrappedMessageError{Message: "getting secret via embedded SDK", Cause: err}
	}

	if secret == nil || secret.Data == nil {
		return nil, &secrets.SecretNotFoundError{
			Path:     path,
			Provider: "Embedded",
		}
	}

	// Convert the data map back to JSON bytes
	dataBytes, err := json.Marshal(secret.Data)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "marshaling secret data", Cause: err}
	}

	return dataBytes, nil
}

// DeleteSecret removes a secret via embedded SDK.
func (c *EmbeddedClient) DeleteSecret(ctx context.Context, path string) error {
	if c.client == nil {
		return ErrEmbeddedClientNotInitialized{}
	}

	err := c.client.Secrets().Delete(ctx, path)
	if err != nil && !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "NotFound") {
		return &qberrors.WrappedMessageError{Message: "deleting secret via embedded SDK", Cause: err}
	}

	return nil
}

// =============================================================================
// REST Protocol Client
// =============================================================================

// RESTClient implements ProtocolClient for REST API.
type RESTClient struct {
	endpoint string
	client   *http.Client
}

// Name returns the protocol name.
func (c *RESTClient) Name() string {
	return "REST"
}

// Setup initializes the REST client.
func (c *RESTClient) Setup(t *testing.T, ctx context.Context) error {
	t.Helper()
	c.endpoint = getRESTEndpoint()
	c.client = helpers.NewHTTPClient(30 * time.Second)

	// Verify connectivity
	url := fmt.Sprintf("%s/health", c.endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "creating health request", Cause: err}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "REST endpoint not available at " + c.endpoint, Cause: err}
	}
	defer resp.Body.Close()

	return nil
}

// Teardown closes the REST client.
func (c *RESTClient) Teardown(t *testing.T) error {
	c.client = nil
	return nil
}

// StoreSecret stores a secret via REST API.
func (c *RESTClient) StoreSecret(ctx context.Context, path string, data []byte) error {
	reqBody := map[string]interface{}{
		"data": data,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "marshaling request", Cause: err}
	}

	url := fmt.Sprintf("%s/api/v1/secrets/%s", c.endpoint, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "creating request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "executing request", Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return &qberrors.MessageError{Message: fmt.Sprintf("unexpected status %d: %s", resp.StatusCode, string(respBody))}
	}

	return nil
}

// GetSecret retrieves a secret via REST API.
func (c *RESTClient) GetSecret(ctx context.Context, path string) ([]byte, error) {
	url := fmt.Sprintf("%s/api/v1/secrets/%s", c.endpoint, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "creating request", Cause: err}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "executing request", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "reading response", Cause: err}
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, &secrets.SecretNotFoundError{
			Path:     path,
			Provider: "REST",
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: fmt.Sprintf("unexpected status %d: %s", resp.StatusCode, string(respBody))}
	}

	var result service.GetSecretResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "unmarshaling response", Cause: err}
	}

	return result.Data, nil
}

// DeleteSecret removes a secret via REST API.
func (c *RESTClient) DeleteSecret(ctx context.Context, path string) error {
	url := fmt.Sprintf("%s/api/v1/secrets/%s?destroy=true", c.endpoint, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "creating request", Cause: err}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "executing request", Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return &qberrors.MessageError{Message: fmt.Sprintf("unexpected status %d: %s", resp.StatusCode, string(respBody))}
	}

	return nil
}

// =============================================================================
// gRPC Protocol Client
// =============================================================================

// GRPCClient implements ProtocolClient for gRPC.
type GRPCClient struct {
	endpoint string
	client   *grpcclient.Client
}

// Name returns the protocol name.
func (c *GRPCClient) Name() string {
	return "gRPC"
}

// Setup initializes the gRPC client.
func (c *GRPCClient) Setup(t *testing.T, ctx context.Context) error {
	t.Helper()
	c.endpoint = getGRPCEndpoint()

	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(c.endpoint, grpcclient.WithTLSConfig(tlsCfg))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "connecting to gRPC endpoint " + c.endpoint, Cause: err}
	}
	c.client = client

	return nil
}

// Teardown closes the gRPC client.
func (c *GRPCClient) Teardown(t *testing.T) error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// StoreSecret stores a secret via gRPC.
func (c *GRPCClient) StoreSecret(ctx context.Context, path string, data []byte) error {
	req := &service.StoreSecretRequest{
		Name: path,
		Data: data,
	}

	_, err := c.client.StoreSecret(ctx, req)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "storing secret via gRPC", Cause: err}
	}

	return nil
}

// GetSecret retrieves a secret via gRPC.
func (c *GRPCClient) GetSecret(ctx context.Context, path string) ([]byte, error) {
	req := &service.GetSecretRequest{
		SecretID: path,
	}

	resp, err := c.client.GetSecret(ctx, req)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, &secrets.SecretNotFoundError{
				Path:     path,
				Provider: "gRPC",
			}
		}
		return nil, &qberrors.WrappedMessageError{Message: "getting secret via gRPC", Cause: err}
	}

	return resp.Data, nil
}

// DeleteSecret removes a secret via gRPC.
func (c *GRPCClient) DeleteSecret(ctx context.Context, path string) error {
	req := &service.DeleteSecretRequest{
		SecretID:          path,
		PermanentlyDelete: true,
	}

	_, err := c.client.DeleteSecret(ctx, req)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return &qberrors.WrappedMessageError{Message: "deleting secret via gRPC", Cause: err}
	}

	return nil
}

// =============================================================================
// Unix Socket Protocol Client (gRPC over Unix Domain Socket)
// =============================================================================

// ErrUnixClientNotInitialized is returned when the Unix socket client is not initialized.
type ErrUnixClientNotInitialized struct{}

func (e ErrUnixClientNotInitialized) Error() string {
	return "unix client not initialized"
}

// UnixClient implements ProtocolClient for gRPC over Unix Domain Socket.
type UnixClient struct {
	socketPath string
	client     *grpcclient.Client
}

// Name returns the protocol name.
func (c *UnixClient) Name() string {
	return "Unix"
}

// Setup initializes the gRPC client over Unix socket.
func (c *UnixClient) Setup(t *testing.T, ctx context.Context) error {
	t.Helper()
	c.socketPath = getUnixSocket()

	// Check if socket file exists
	conn, err := net.Dial("unix", c.socketPath)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "Unix socket not available at " + c.socketPath, Cause: err}
	}
	conn.Close()

	// Create gRPC client over Unix socket
	unixEndpoint := fmt.Sprintf("unix://%s", c.socketPath)
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(unixEndpoint, grpcclient.WithTLSConfig(tlsCfg))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create gRPC Unix socket client", Cause: err}
	}
	c.client = client

	return nil
}

// Teardown closes the gRPC client.
func (c *UnixClient) Teardown(t *testing.T) error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// StoreSecret stores a secret via gRPC over Unix socket.
func (c *UnixClient) StoreSecret(ctx context.Context, path string, data []byte) error {
	if c.client == nil {
		return ErrUnixClientNotInitialized{}
	}

	req := &service.StoreSecretRequest{
		Name: path,
		Data: data,
	}

	_, err := c.client.StoreSecret(ctx, req)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "storing secret via gRPC Unix socket", Cause: err}
	}

	return nil
}

// GetSecret retrieves a secret via gRPC over Unix socket.
func (c *UnixClient) GetSecret(ctx context.Context, path string) ([]byte, error) {
	if c.client == nil {
		return nil, ErrUnixClientNotInitialized{}
	}

	req := &service.GetSecretRequest{
		SecretID: path,
	}

	resp, err := c.client.GetSecret(ctx, req)
	if err != nil {
		// Check if it's a not found error
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "NotFound") {
			return nil, &secrets.SecretNotFoundError{
				Path:     path,
				Provider: "Unix",
			}
		}
		return nil, &qberrors.WrappedMessageError{Message: "getting secret via gRPC Unix socket", Cause: err}
	}

	return resp.Data, nil
}

// DeleteSecret removes a secret via gRPC over Unix socket.
func (c *UnixClient) DeleteSecret(ctx context.Context, path string) error {
	if c.client == nil {
		return ErrUnixClientNotInitialized{}
	}

	req := &service.DeleteSecretRequest{
		SecretID:          path,
		PermanentlyDelete: true,
	}

	_, err := c.client.DeleteSecret(ctx, req)
	// Ignore not found errors on delete
	if err != nil && (strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "NotFound")) {
		return nil
	}
	return err
}

// =============================================================================
// QUIC Protocol Client
// =============================================================================

// ErrQUICConnectionNotEstablished is returned when a QUIC operation is attempted
// without an established connection.
type ErrQUICConnectionNotEstablished struct{}

func (e ErrQUICConnectionNotEstablished) Error() string {
	return "QUIC connection not established"
}

// QUICClient implements ProtocolClient for QUIC protocol.
type QUICClient struct {
	endpoint string
	conn     *quicgo.Conn
}

// Name returns the protocol name.
func (c *QUICClient) Name() string {
	return "QUIC"
}

// Setup initializes the QUIC client by dialing the QUIC endpoint with TLS.
func (c *QUICClient) Setup(t *testing.T, ctx context.Context) error {
	t.Helper()
	c.endpoint = getQUICEndpoint()

	tlsConf, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	tlsConf.NextProtos = []string{"quicbaas-quic"}

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn, err := quicgo.DialAddr(dialCtx, c.endpoint, tlsConf, nil)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to connect to QUIC endpoint " + c.endpoint, Cause: err}
	}

	c.conn = conn
	return nil
}

// Teardown closes the QUIC connection.
func (c *QUICClient) Teardown(t *testing.T) error {
	if c.conn != nil {
		return c.conn.CloseWithError(0, "client closing")
	}
	return nil
}

// sendRequest sends a QUIC request and returns the response.
func (c *QUICClient) sendRequest(ctx context.Context, opcode byte, reqPayload []byte) (*quicpkg.Response, error) {
	if c.conn == nil {
		return nil, ErrQUICConnectionNotEstablished{}
	}

	stream, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to open stream", Cause: err}
	}
	defer stream.Close()

	// Write request
	req := &quicpkg.Request{
		Opcode:  opcode,
		Payload: reqPayload,
	}

	if err := quicpkg.WriteRequest(stream, req); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to write request", Cause: err}
	}

	// Read response
	resp, err := quicpkg.ReadResponse(stream)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	// Check status
	if resp.Status != quicpkg.StatusOK {
		errMsg := string(resp.Payload)
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return nil, quicpkg.StatusToError(resp.Status, errMsg)
	}

	return resp, nil
}

// StoreSecret stores a secret via QUIC using the OpSecretsStore opcode.
func (c *QUICClient) StoreSecret(ctx context.Context, path string, data []byte) error {
	req := &service.StoreSecretRequest{
		Name: path,
		Data: data,
	}

	payload, err := quicpkg.EncodePayload(req)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "encoding store request", Cause: err}
	}

	_, err = c.sendRequest(ctx, quicpkg.OpSecretsStore, payload)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "storing secret via QUIC", Cause: err}
	}

	return nil
}

// GetSecret retrieves a secret via QUIC using the OpSecretsGet opcode.
func (c *QUICClient) GetSecret(ctx context.Context, path string) ([]byte, error) {
	req := &service.GetSecretRequest{
		SecretID: path,
	}

	payload, err := quicpkg.EncodePayload(req)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "encoding get request", Cause: err}
	}

	resp, err := c.sendRequest(ctx, quicpkg.OpSecretsGet, payload)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "NotFound") {
			return nil, &secrets.SecretNotFoundError{
				Path:     path,
				Provider: "QUIC",
			}
		}
		return nil, &qberrors.WrappedMessageError{Message: "getting secret via QUIC", Cause: err}
	}

	var result service.GetSecretResponse
	if err := quicpkg.DecodePayload(resp.Payload, &result); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "decoding get response", Cause: err}
	}

	return result.Data, nil
}

// DeleteSecret removes a secret via QUIC using the OpSecretsDelete opcode.
func (c *QUICClient) DeleteSecret(ctx context.Context, path string) error {
	req := &service.DeleteSecretRequest{
		SecretID:          path,
		PermanentlyDelete: true,
	}

	payload, err := quicpkg.EncodePayload(req)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "encoding delete request", Cause: err}
	}

	_, err = c.sendRequest(ctx, quicpkg.OpSecretsDelete, payload)
	if err != nil {
		// Ignore not found errors on delete
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "NotFound") {
			return nil
		}
		return &qberrors.WrappedMessageError{Message: "deleting secret via QUIC", Cause: err}
	}

	return nil
}

// =============================================================================
// MCP Protocol Client
// =============================================================================

// MCP JSON-RPC error codes for service errors.
// These must match the error codes defined in pkg/api/mcp/server.go.
const (
	mcpResourceNotFound = -32004
)

// MCPClient implements ProtocolClient for MCP (Model Context Protocol).
type MCPClient struct {
	endpoint      string
	client        *http.Client
	nextID        int
	accessToken   string
	authenticated bool
	initialized   bool
}

// Name returns the protocol name.
func (c *MCPClient) Name() string {
	return "MCP"
}

// Setup initializes the MCP client.
func (c *MCPClient) Setup(t *testing.T, ctx context.Context) error {
	t.Helper()
	c.endpoint = getMCPEndpoint()
	c.client = helpers.NewHTTPClient(30 * time.Second)
	c.nextID = 1

	// Verify connectivity via initialize call
	if err := c.Initialize(ctx); err != nil {
		return &qberrors.WrappedMessageError{Message: "MCP initialize failed", Cause: err}
	}

	return nil
}

// Initialize sends the MCP initialize request.
func (c *MCPClient) Initialize(ctx context.Context) error {
	if c.initialized {
		return nil
	}

	initReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "quicbaas-test-client",
				"version": "1.0.0",
			},
		},
		"id": c.nextID,
	}
	c.nextID++

	body, err := json.Marshal(initReq)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "marshaling init request", Cause: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "creating request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "MCP endpoint not available at " + c.endpoint, Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return &qberrors.MessageError{Message: "MCP initialize failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	c.initialized = true
	return nil
}

// ensureAuthenticated ensures the client is authenticated before making tool calls.
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

// Teardown closes the MCP client.
func (c *MCPClient) Teardown(t *testing.T) error {
	c.client = nil
	return nil
}

// mcpToolError represents an MCP tool error with code and message.
type mcpToolError struct {
	Code    int
	Message string
	Data    string
}

func (e *mcpToolError) Error() string {
	if e.Data != "" {
		return "MCP error " + strconv.Itoa(e.Code) + ": " + e.Message + ": " + e.Data
	}
	return "MCP error " + strconv.Itoa(e.Code) + ": " + e.Message
}

// callMCPTool executes an MCP tool call and returns the result.
func (c *MCPClient) callMCPTool(ctx context.Context, toolName string, args map[string]interface{}) (json.RawMessage, error) {
	// Ensure authenticated before making tool calls
	if err := c.ensureAuthenticated(ctx); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "authentication failed", Cause: err}
	}

	mcpReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		},
		"id": c.nextID,
	}
	c.nextID++

	body, err := json.Marshal(mcpReq)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "marshaling MCP request", Cause: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "creating MCP request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")

	// Add authorization header if we have an access token
	if c.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "executing MCP request", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "reading MCP response", Cause: err}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: "MCP call failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	var mcpResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string      `json:"message"`
			Code    int         `json:"code"`
			Data    interface{} `json:"data,omitempty"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &mcpResp); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "unmarshaling MCP response", Cause: err}
	}

	// Check for JSON-RPC error response
	if mcpResp.Error != nil {
		dataStr := ""
		if mcpResp.Error.Data != nil {
			if s, ok := mcpResp.Error.Data.(string); ok {
				dataStr = s
			}
		}
		return nil, &mcpToolError{
			Code:    mcpResp.Error.Code,
			Message: mcpResp.Error.Message,
			Data:    dataStr,
		}
	}

	if mcpResp.Result.IsError {
		if len(mcpResp.Result.Content) > 0 {
			return nil, &qberrors.MessageError{Message: "MCP tool error: " + mcpResp.Result.Content[0].Text}
		}
		return nil, &qberrors.MessageError{Message: "MCP tool returned error"}
	}

	if len(mcpResp.Result.Content) > 0 {
		return json.RawMessage(mcpResp.Result.Content[0].Text), nil
	}

	return nil, nil
}

// StoreSecret stores a secret via MCP.
// Note: MCP secrets tools expect base64-encoded data.
func (c *MCPClient) StoreSecret(ctx context.Context, path string, data []byte) error {
	// Encode data as base64 as required by MCP secrets tools
	encodedData := base64.StdEncoding.EncodeToString(data)

	args := map[string]interface{}{
		"name": path,
		"data": encodedData,
	}

	// Use correct tool name: quicbaas_secrets_store (plural "secrets")
	_, err := c.callMCPTool(ctx, "quicbaas_secrets_store", args)
	return err
}

// GetSecret retrieves a secret via MCP.
func (c *MCPClient) GetSecret(ctx context.Context, path string) ([]byte, error) {
	// Use secret_id parameter as expected by MCP secrets tools
	args := map[string]interface{}{
		"secret_id": path,
	}

	// Use correct tool name: quicbaas_secrets_get (plural "secrets")
	result, err := c.callMCPTool(ctx, "quicbaas_secrets_get", args)
	if err != nil {
		// Check for typed MCP error with ResourceNotFound code
		if mcpErr, ok := err.(*mcpToolError); ok {
			if mcpErr.Code == mcpResourceNotFound {
				return nil, &secrets.SecretNotFoundError{
					Path:     path,
					Provider: "MCP",
				}
			}
		}
		// Fallback: check error message for "not found" pattern
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			return nil, &secrets.SecretNotFoundError{
				Path:     path,
				Provider: "MCP",
			}
		}
		return nil, err
	}

	// Parse the result to extract data
	var secretResp struct {
		Data []byte `json:"data"`
	}
	if err := json.Unmarshal(result, &secretResp); err != nil {
		// Try direct return if it's raw data
		return result, nil
	}

	return secretResp.Data, nil
}

// DeleteSecret removes a secret via MCP.
func (c *MCPClient) DeleteSecret(ctx context.Context, path string) error {
	// Use secret_id parameter as expected by MCP secrets tools
	args := map[string]interface{}{
		"secret_id":          path,
		"permanently_delete": true,
	}

	// Use correct tool name: quicbaas_secrets_delete (plural "secrets")
	_, err := c.callMCPTool(ctx, "quicbaas_secrets_delete", args)
	if err != nil {
		// Check for typed MCP error with ResourceNotFound code
		if mcpErr, ok := err.(*mcpToolError); ok {
			if mcpErr.Code == mcpResourceNotFound {
				return nil // Ignore not found on delete
			}
		}
		// Fallback: check error message for "not found" pattern
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			return nil // Ignore not found on delete
		}
	}
	return err
}

// =============================================================================
// Protocol Secrets Client Adapter
// =============================================================================

// protocolSecretsClient adapts a ProtocolClient to the secrets.SecretsClient interface.
type protocolSecretsClient struct {
	protocol ProtocolClient
}

// GetSecret retrieves a secret using the underlying protocol client.
func (c *protocolSecretsClient) GetSecret(ctx context.Context, secretID string) ([]byte, error) {
	return c.protocol.GetSecret(ctx, secretID)
}

// =============================================================================
// Multi-Protocol Test Runner
// =============================================================================

// TestSecretResolverMultiProtocol verifies that secret resolution works identically
// across all supported protocols: Embedded, REST, gRPC, QUIC, Unix Socket, and MCP.
//
// Protocols that do not yet support secrets operations (or whose test clients cannot
// connect) are logged and handled gracefully. The test requires at least
// minRequiredProtocols protocols to succeed (REST and gRPC should always pass).
func TestSecretResolverMultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	protocols := []ProtocolClient{
		&RESTClient{},
		&GRPCClient{},
		&QUICClient{},
		&UnixClient{},
		&MCPClient{},
	}

	if helpers.IsEmbeddedAvailable() {
		protocols = append([]ProtocolClient{&EmbeddedClient{}}, protocols...)
	}

	var succeeded int64

	for _, proto := range protocols {
		proto := proto // capture loop variable
		t.Run(proto.Name(), func(t *testing.T) {
			if runResolverProtocol(t, ctx, proto) {
				atomic.AddInt64(&succeeded, 1)
			}
		})
	}

	require.GreaterOrEqual(t, int(atomic.LoadInt64(&succeeded)), minRequiredProtocols,
		"at least %d protocols must pass secrets-resolver tests, but only %d succeeded",
		minRequiredProtocols, atomic.LoadInt64(&succeeded))
}

// runResolverProtocol runs the complete resolver test suite against a protocol.
// It returns true if the protocol was set up and tested successfully, or
// false if Setup returned an error (indicating the protocol is not available
// on this deployment).
func runResolverProtocol(t *testing.T, ctx context.Context, protocol ProtocolClient) bool {
	t.Helper()

	// Setup the protocol client
	err := protocol.Setup(t, ctx)
	if err != nil {
		t.Logf("Protocol %s not available for secrets-resolver testing: %v", protocol.Name(), err)
		return false
	}
	defer func() {
		err := protocol.Teardown(t)
		if err != nil {
			t.Logf("Warning: teardown error for %s: %v", protocol.Name(), err)
		}
	}()

	basePrefix := fmt.Sprintf("mp-%s-%d", protocol.Name(), time.Now().UnixNano())

	// Test 1: Store and Resolve Secret
	t.Run("store_and_resolve", func(t *testing.T) {
		testStoreAndResolve(t, ctx, protocol, basePrefix+"-resolve")
	})

	// Test 2: Resolve Non-Existent Secret
	t.Run("resolve_nonexistent", func(t *testing.T) {
		testResolveNonExistent(t, ctx, protocol, basePrefix+"-nonexistent")
	})

	// Test 3: Resolve with Nested Path
	t.Run("resolve_nested_path", func(t *testing.T) {
		testResolveNestedPath(t, ctx, protocol, basePrefix+"-nested")
	})

	// Test 4: Resolve Binary Data
	t.Run("resolve_binary_data", func(t *testing.T) {
		testResolveBinaryData(t, ctx, protocol, basePrefix+"-binary")
	})

	// Test 5: Resolve Large Secret
	t.Run("resolve_large_secret", func(t *testing.T) {
		testResolveLargeSecret(t, ctx, protocol, basePrefix+"-large")
	})

	// Test 6: Resolver Registry Integration
	t.Run("resolver_registry", func(t *testing.T) {
		testResolverRegistry(t, ctx, protocol, basePrefix+"-registry")
	})

	return true
}

// =============================================================================
// Individual Test Functions
// =============================================================================

// testStoreAndResolve tests storing a secret and resolving it via kms:// URI.
func testStoreAndResolve(t *testing.T, ctx context.Context, protocol ProtocolClient, prefix string) {
	t.Helper()

	path := fmt.Sprintf("%s/credentials", prefix)

	// Prepare secret data
	secretData := map[string]interface{}{
		"username": "test-user",
		"password": "super-secret-password-12345",
		"host":     "db.example.com",
		"port":     5432,
	}
	dataBytes, err := json.Marshal(secretData)
	require.NoError(t, err)

	// Store the secret via protocol
	err = protocol.StoreSecret(ctx, path, dataBytes)
	require.NoError(t, err, "Failed to store secret via %s", protocol.Name())

	// Clean up after test
	defer func() {
		err := protocol.DeleteSecret(ctx, path)
		if err != nil {
			t.Logf("Warning: failed to clean up secret %s: %v", path, err)
		}
	}()

	// Create secrets client adapter and resolver
	secretsClient := &protocolSecretsClient{protocol: protocol}
	resolver := secrets.NewLocalKMSResolver(secretsClient)
	require.NotNil(t, resolver)

	// Resolve the secret using kms:// URI
	kmsURI := fmt.Sprintf("kms://%s", path)
	result, err := resolver.Resolve(ctx, kmsURI)

	require.NoError(t, err, "Failed to resolve secret via %s", protocol.Name())
	require.NotNil(t, result)
	assert.Equal(t, kmsURI, result.Raw)
	assert.Equal(t, secrets.StrategyLocalKMS, result.Strategy)

	// Verify the resolved data matches original
	var resolvedData map[string]interface{}
	err = json.Unmarshal([]byte(result.Value), &resolvedData)
	require.NoError(t, err)
	assert.Equal(t, "test-user", resolvedData["username"])
	assert.Equal(t, "super-secret-password-12345", resolvedData["password"])
	assert.Equal(t, "db.example.com", resolvedData["host"])

	t.Logf("[%s] Successfully stored and resolved secret at kms://%s", protocol.Name(), path)
}

// testResolveNonExistent tests that resolving a non-existent secret returns appropriate error.
func testResolveNonExistent(t *testing.T, ctx context.Context, protocol ProtocolClient, prefix string) {
	t.Helper()

	// Create secrets client adapter and resolver
	secretsClient := &protocolSecretsClient{protocol: protocol}
	resolver := secrets.NewLocalKMSResolver(secretsClient)
	require.NotNil(t, resolver)

	// Attempt to resolve non-existent secret
	nonExistentPath := fmt.Sprintf("%s/does-not-exist", prefix)
	kmsURI := fmt.Sprintf("kms://%s", nonExistentPath)

	result, err := resolver.Resolve(ctx, kmsURI)

	require.Error(t, err, "Expected error for non-existent secret")
	assert.Nil(t, result)
	assert.True(t, secrets.IsSecretNotFound(err), "Expected SecretNotFoundError, got: %v", err)

	t.Logf("[%s] Correctly received not found error for non-existent secret", protocol.Name())
}

// testResolveNestedPath tests resolution of secrets with deeply nested paths.
func testResolveNestedPath(t *testing.T, ctx context.Context, protocol ProtocolClient, prefix string) {
	t.Helper()

	path := fmt.Sprintf("%s/config/app/database/connection", prefix)

	// Prepare secret data
	secretData := map[string]interface{}{
		"connection_url": "postgresql://user:pass@localhost:5432/mydb",
		"ssl_mode":       "require",
		"pool_size":      10,
	}
	dataBytes, err := json.Marshal(secretData)
	require.NoError(t, err)

	// Store the secret via protocol
	err = protocol.StoreSecret(ctx, path, dataBytes)
	require.NoError(t, err, "Failed to store nested secret via %s", protocol.Name())

	// Clean up after test
	defer func() {
		err := protocol.DeleteSecret(ctx, path)
		if err != nil {
			t.Logf("Warning: failed to clean up secret %s: %v", path, err)
		}
	}()

	// Create secrets client adapter and resolver
	secretsClient := &protocolSecretsClient{protocol: protocol}
	resolver := secrets.NewLocalKMSResolver(secretsClient)
	require.NotNil(t, resolver)

	// Resolve the nested secret
	kmsURI := fmt.Sprintf("kms://%s", path)
	result, err := resolver.Resolve(ctx, kmsURI)

	require.NoError(t, err, "Failed to resolve nested secret via %s", protocol.Name())
	require.NotNil(t, result)
	assert.Equal(t, secrets.StrategyLocalKMS, result.Strategy)

	// Verify the resolved data
	var resolvedData map[string]interface{}
	err = json.Unmarshal([]byte(result.Value), &resolvedData)
	require.NoError(t, err)
	assert.Equal(t, "postgresql://user:pass@localhost:5432/mydb", resolvedData["connection_url"])
	assert.Equal(t, "require", resolvedData["ssl_mode"])

	t.Logf("[%s] Successfully resolved nested secret at kms://%s", protocol.Name(), path)
}

// testResolveBinaryData tests resolution of secrets containing binary data.
func testResolveBinaryData(t *testing.T, ctx context.Context, protocol ProtocolClient, prefix string) {
	t.Helper()

	path := fmt.Sprintf("%s/certificates/tls", prefix)

	// Prepare secret with base64-encoded binary data
	secretData := map[string]interface{}{
		"certificate": "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0t...",
		"private_key": "LS0tLS1CRUdJTiBQUklWQVRFIEtFWS0tLS0t...",
		"type":        "tls-certificate",
	}
	dataBytes, err := json.Marshal(secretData)
	require.NoError(t, err)

	// Store the secret via protocol
	err = protocol.StoreSecret(ctx, path, dataBytes)
	require.NoError(t, err, "Failed to store binary secret via %s", protocol.Name())

	// Clean up after test
	defer func() {
		err := protocol.DeleteSecret(ctx, path)
		if err != nil {
			t.Logf("Warning: failed to clean up secret %s: %v", path, err)
		}
	}()

	// Create secrets client adapter and resolver
	secretsClient := &protocolSecretsClient{protocol: protocol}
	resolver := secrets.NewLocalKMSResolver(secretsClient)
	require.NotNil(t, resolver)

	// Resolve the secret
	kmsURI := fmt.Sprintf("kms://%s", path)
	result, err := resolver.Resolve(ctx, kmsURI)

	require.NoError(t, err, "Failed to resolve binary secret via %s", protocol.Name())
	require.NotNil(t, result)
	assert.Equal(t, secrets.StrategyLocalKMS, result.Strategy)

	// Verify the resolved data
	var resolvedData map[string]interface{}
	err = json.Unmarshal([]byte(result.Value), &resolvedData)
	require.NoError(t, err)
	assert.Equal(t, "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0t...", resolvedData["certificate"])
	assert.Equal(t, "LS0tLS1CRUdJTiBQUklWQVRFIEtFWS0tLS0t...", resolvedData["private_key"])
	assert.Equal(t, "tls-certificate", resolvedData["type"])

	t.Logf("[%s] Successfully resolved binary data secret at kms://%s", protocol.Name(), path)
}

// testResolveLargeSecret tests resolution of large secrets (8KB+).
func testResolveLargeSecret(t *testing.T, ctx context.Context, protocol ProtocolClient, prefix string) {
	t.Helper()

	path := fmt.Sprintf("%s/large-config", prefix)

	// Create large secret data (8KB)
	largeData := make([]byte, 8192)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	secretData := map[string]interface{}{
		"large_payload": string(largeData),
		"type":          "large-config",
	}
	dataBytes, err := json.Marshal(secretData)
	require.NoError(t, err)

	// Store the secret via protocol
	err = protocol.StoreSecret(ctx, path, dataBytes)
	require.NoError(t, err, "Failed to store large secret via %s", protocol.Name())

	// Clean up after test
	defer func() {
		err := protocol.DeleteSecret(ctx, path)
		if err != nil {
			t.Logf("Warning: failed to clean up secret %s: %v", path, err)
		}
	}()

	// Create secrets client adapter and resolver
	secretsClient := &protocolSecretsClient{protocol: protocol}
	resolver := secrets.NewLocalKMSResolver(secretsClient)
	require.NotNil(t, resolver)

	// Resolve the secret
	kmsURI := fmt.Sprintf("kms://%s", path)
	result, err := resolver.Resolve(ctx, kmsURI)

	require.NoError(t, err, "Failed to resolve large secret via %s", protocol.Name())
	require.NotNil(t, result)
	assert.Equal(t, secrets.StrategyLocalKMS, result.Strategy)
	assert.Greater(t, len(result.Value), 8000, "Resolved value should be at least 8KB")

	t.Logf("[%s] Successfully resolved large secret (%d bytes) at kms://%s", protocol.Name(), len(result.Value), path)
}

// testResolverRegistry tests the resolver integration with ResolverRegistry.
func testResolverRegistry(t *testing.T, ctx context.Context, protocol ProtocolClient, prefix string) {
	t.Helper()

	path := fmt.Sprintf("%s/registry-test", prefix)

	// Prepare secret data
	secretData := map[string]interface{}{
		"token": "registry-test-token-abc123",
	}
	dataBytes, err := json.Marshal(secretData)
	require.NoError(t, err)

	// Store the secret via protocol
	err = protocol.StoreSecret(ctx, path, dataBytes)
	require.NoError(t, err, "Failed to store secret via %s", protocol.Name())

	// Clean up after test
	defer func() {
		err := protocol.DeleteSecret(ctx, path)
		if err != nil {
			t.Logf("Warning: failed to clean up secret %s: %v", path, err)
		}
	}()

	// Create resolver and register with registry
	secretsClient := &protocolSecretsClient{protocol: protocol}
	resolver := secrets.NewLocalKMSResolver(secretsClient)
	require.NotNil(t, resolver)

	registry := secrets.NewResolverRegistry()
	registry.Register(resolver)

	// Test IsSecretURI
	kmsURI := fmt.Sprintf("kms://%s", path)
	assert.True(t, registry.IsSecretURI(kmsURI), "Registry should recognize kms:// URI")
	assert.False(t, registry.IsSecretURI("plain-value"), "Registry should not recognize plain value")
	assert.False(t, registry.IsSecretURI("vault://other/secret"), "Registry should not recognize vault:// URI")

	// Resolve through registry
	result, err := registry.Resolve(ctx, kmsURI)
	require.NoError(t, err, "Failed to resolve through registry via %s", protocol.Name())
	require.NotNil(t, result)
	assert.Equal(t, secrets.StrategyLocalKMS, result.Strategy)

	// Verify resolved data
	var resolvedData map[string]interface{}
	err = json.Unmarshal([]byte(result.Value), &resolvedData)
	require.NoError(t, err)
	assert.Equal(t, "registry-test-token-abc123", resolvedData["token"])

	// Test literal fallback
	literalResult, err := registry.Resolve(ctx, "literal-value")
	require.NoError(t, err)
	require.NotNil(t, literalResult)
	assert.Equal(t, secrets.StrategyLiteral, literalResult.Strategy)
	assert.Equal(t, "literal-value", literalResult.Value)

	t.Logf("[%s] Successfully tested resolver with ResolverRegistry", protocol.Name())
}

// =============================================================================
// Cross-Protocol Consistency Tests
// =============================================================================

// TestSecretResolverCrossProtocol verifies that secrets stored via one protocol
// can be resolved via another protocol, ensuring data consistency.
func TestSecretResolverCrossProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Setup REST and gRPC clients
	restClient := &RESTClient{}
	grpcClient := &GRPCClient{}

	err := restClient.Setup(t, ctx)
	require.NoError(t, err, "REST endpoint must be available for integration tests")
	defer restClient.Teardown(t)

	err = grpcClient.Setup(t, ctx)
	require.NoError(t, err, "gRPC endpoint must be available for integration tests")
	defer grpcClient.Teardown(t)

	prefix := fmt.Sprintf("cross-protocol-%d", time.Now().UnixNano())

	t.Run("store_via_rest_resolve_via_grpc", func(t *testing.T) {
		path := fmt.Sprintf("%s/rest-to-grpc", prefix)

		// Store via REST
		secretData := map[string]interface{}{
			"api_key": "cross-protocol-rest-key",
		}
		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)

		err = restClient.StoreSecret(ctx, path, dataBytes)
		require.NoError(t, err, "Failed to store via REST")
		defer restClient.DeleteSecret(ctx, path)

		// Resolve via gRPC
		grpcSecretsClient := &protocolSecretsClient{protocol: grpcClient}
		resolver := secrets.NewLocalKMSResolver(grpcSecretsClient)
		require.NotNil(t, resolver)

		kmsURI := fmt.Sprintf("kms://%s", path)
		result, err := resolver.Resolve(ctx, kmsURI)

		require.NoError(t, err, "Failed to resolve via gRPC")
		require.NotNil(t, result)
		assert.Equal(t, secrets.StrategyLocalKMS, result.Strategy)

		// Verify data
		var resolvedData map[string]interface{}
		err = json.Unmarshal([]byte(result.Value), &resolvedData)
		require.NoError(t, err)
		assert.Equal(t, "cross-protocol-rest-key", resolvedData["api_key"])

		t.Logf("Successfully stored via REST and resolved via gRPC: %s", path)
	})

	t.Run("store_via_grpc_resolve_via_rest", func(t *testing.T) {
		path := fmt.Sprintf("%s/grpc-to-rest", prefix)

		// Store via gRPC
		secretData := map[string]interface{}{
			"api_key": "cross-protocol-grpc-key",
		}
		dataBytes, err := json.Marshal(secretData)
		require.NoError(t, err)

		err = grpcClient.StoreSecret(ctx, path, dataBytes)
		require.NoError(t, err, "Failed to store via gRPC")
		defer grpcClient.DeleteSecret(ctx, path)

		// Resolve via REST
		restSecretsClient := &protocolSecretsClient{protocol: restClient}
		resolver := secrets.NewLocalKMSResolver(restSecretsClient)
		require.NotNil(t, resolver)

		kmsURI := fmt.Sprintf("kms://%s", path)
		result, err := resolver.Resolve(ctx, kmsURI)

		require.NoError(t, err, "Failed to resolve via REST")
		require.NotNil(t, result)
		assert.Equal(t, secrets.StrategyLocalKMS, result.Strategy)

		// Verify data
		var resolvedData map[string]interface{}
		err = json.Unmarshal([]byte(result.Value), &resolvedData)
		require.NoError(t, err)
		assert.Equal(t, "cross-protocol-grpc-key", resolvedData["api_key"])

		t.Logf("Successfully stored via gRPC and resolved via REST: %s", path)
	})
}

// =============================================================================
// Concurrent Access Tests
// =============================================================================

// TestSecretResolverConcurrent tests concurrent secret resolution across protocols.
func TestSecretResolverConcurrent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	restClient := &RESTClient{}
	err := restClient.Setup(t, ctx)
	require.NoError(t, err, "REST endpoint must be available for integration tests")
	defer restClient.Teardown(t)

	prefix := fmt.Sprintf("concurrent-%d", time.Now().UnixNano())

	// Create a shared secret
	path := fmt.Sprintf("%s/shared", prefix)
	secretData := map[string]interface{}{
		"shared_key": "concurrent-test-value",
	}
	dataBytes, err := json.Marshal(secretData)
	require.NoError(t, err)

	err = restClient.StoreSecret(ctx, path, dataBytes)
	require.NoError(t, err)
	defer restClient.DeleteSecret(ctx, path)

	// Create resolver
	secretsClient := &protocolSecretsClient{protocol: restClient}
	resolver := secrets.NewLocalKMSResolver(secretsClient)
	require.NotNil(t, resolver)

	// Concurrent resolution
	concurrency := 10
	done := make(chan bool, concurrency)
	errors := make(chan error, concurrency)

	kmsURI := fmt.Sprintf("kms://%s", path)

	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			result, err := resolver.Resolve(ctx, kmsURI)
			if err != nil {
				errors <- &qberrors.WrappedMessageError{Message: "concurrent resolve error " + strconv.Itoa(idx), Cause: err}
				done <- false
				return
			}

			if result == nil || result.Strategy != secrets.StrategyLocalKMS {
				errors <- &qberrors.MessageError{Message: "concurrent resolve " + strconv.Itoa(idx) + ": invalid result"}
				done <- false
				return
			}

			done <- true
		}(i)
	}

	successCount := 0
	for i := 0; i < concurrency; i++ {
		if <-done {
			successCount++
		}
	}

	close(errors)
	for err := range errors {
		t.Errorf("Concurrent resolution error: %v", err)
	}

	assert.Equal(t, concurrency, successCount, "All concurrent resolutions should succeed")
	t.Logf("Successfully completed %d concurrent resolutions", concurrency)
}

// =============================================================================
// Error Handling Tests
// =============================================================================

// TestSecretResolverErrors tests error handling across protocols.
func TestSecretResolverErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	restClient := &RESTClient{}
	err := restClient.Setup(t, ctx)
	require.NoError(t, err, "REST endpoint must be available for integration tests")
	defer restClient.Teardown(t)

	secretsClient := &protocolSecretsClient{protocol: restClient}
	resolver := secrets.NewLocalKMSResolver(secretsClient)
	require.NotNil(t, resolver)

	t.Run("invalid_uri_empty_path", func(t *testing.T) {
		result, err := resolver.Resolve(ctx, "kms://")
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.True(t, secrets.IsInvalidSecretURI(err), "Expected InvalidSecretURIError, got: %v", err)
	})

	t.Run("invalid_uri_wrong_scheme", func(t *testing.T) {
		result, err := resolver.Resolve(ctx, "vault://secrets/path")
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("invalid_uri_no_scheme", func(t *testing.T) {
		result, err := resolver.Resolve(ctx, "secrets/path")
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("context_cancellation", func(t *testing.T) {
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		result, err := resolver.Resolve(cancelledCtx, "kms://test/secret")
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "context cancelled")
	})
}
