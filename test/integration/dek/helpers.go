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

//go:build integration && quic

package dek

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// DEK Protocol Interface
// =============================================================================

// dekProtocol abstracts the different protocols for DEK operations testing.
type dekProtocol interface {
	Setup(t *testing.T, ctx context.Context) error
	Teardown(t *testing.T) error
	CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error)
	GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error)
	ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error)
	RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error)
	DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error)
	GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error)
	SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error)
	SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error)
	ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error)
	StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error)
	GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error)
	Name() string
}

// ErrNotImplemented indicates the operation is not implemented.
var ErrNotImplemented = errors.New("operation not implemented")

// =============================================================================
// Endpoint Helpers
// =============================================================================

// getGRPCEndpoints returns all gRPC endpoints for testing.
func getGRPCEndpoints() []string {
	return helpers.GetClusterEndpoints()
}

// getRESTEndpoints returns all REST API endpoints for testing.
func getRESTEndpoints() []string {
	return helpers.GetRESTEndpoints()
}

// =============================================================================
// Test Data Helpers
// =============================================================================

// generateUniqueDEKPath generates a unique DEK path with a prefix.
func generateUniqueDEKPath(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// =============================================================================
// Skip Helpers
// =============================================================================

// skipIfDEKUnavailable checks if the DEK service is available and skips the test if not.
func skipIfDEKUnavailable(t *testing.T, client *grpcclient.Client, ctx context.Context) {
	t.Helper()
	req := &service.ListDEKsRequest{
		Limit: 1,
	}
	_, err := client.KMSListDEKs(ctx, req)
	if err != nil && (strings.Contains(err.Error(), "Unimplemented") ||
		strings.Contains(err.Error(), "unknown service") ||
		strings.Contains(err.Error(), "not available")) {
		require.Fail(t, "DEK service must be enabled on the server", "got error: %v", err)
	}
}

// =============================================================================
// Embedded Protocol Client Implementation
// =============================================================================

// ErrEmbeddedClientNotInitialized is returned when the embedded client is not initialized.
type ErrEmbeddedClientNotInitialized struct{}

func (e ErrEmbeddedClientNotInitialized) Error() string {
	return "embedded client not initialized"
}

// embeddedProtocolClient implements dekProtocol using embedded SDK.
type embeddedProtocolClient struct {
	client quicbaas.Client
}

func (c *embeddedProtocolClient) Name() string { return "Embedded" }

func (c *embeddedProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	svc, err := helpers.GetEmbeddedService(t)
	if err != nil {
		return fmt.Errorf("failed to get embedded service: %w", err)
	}

	// Use "embedded" as a placeholder address since the embedded transport ignores it
	client, err := quicbaas.NewClient("embedded", quicbaas.WithEmbedded(svc))
	if err != nil {
		return fmt.Errorf("failed to create embedded client: %w", err)
	}

	c.client = client
	return nil
}

func (c *embeddedProtocolClient) Teardown(t *testing.T) error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *embeddedProtocolClient) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}
	// Embedded DEK operations are not yet implemented in the SDK
	// This would require exposing DEK operations through the SDK's transport layer
	return nil, fmt.Errorf("embedded DEK operations not yet implemented in SDK")
}

func (c *embeddedProtocolClient) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}
	return nil, fmt.Errorf("embedded DEK operations not yet implemented in SDK")
}

func (c *embeddedProtocolClient) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}
	return nil, fmt.Errorf("embedded DEK operations not yet implemented in SDK")
}

func (c *embeddedProtocolClient) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}
	return nil, fmt.Errorf("embedded DEK operations not yet implemented in SDK")
}

func (c *embeddedProtocolClient) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}
	return nil, fmt.Errorf("embedded DEK operations not yet implemented in SDK")
}

func (c *embeddedProtocolClient) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}
	return nil, fmt.Errorf("embedded DEK operations not yet implemented in SDK")
}

func (c *embeddedProtocolClient) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}
	return nil, fmt.Errorf("embedded DEK operations not yet implemented in SDK")
}

func (c *embeddedProtocolClient) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}
	return nil, fmt.Errorf("embedded DEK operations not yet implemented in SDK")
}

func (c *embeddedProtocolClient) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}
	return nil, fmt.Errorf("embedded DEK operations not yet implemented in SDK")
}

func (c *embeddedProtocolClient) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}
	return nil, fmt.Errorf("embedded DEK operations not yet implemented in SDK")
}

func (c *embeddedProtocolClient) GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}
	return nil, fmt.Errorf("embedded DEK operations not yet implemented in SDK")
}

// =============================================================================
// REST Protocol Client Implementation
// =============================================================================

// restProtocolClient implements dekProtocol using REST API.
type restProtocolClient struct {
	client *RESTDEKClient
}

func (c *restProtocolClient) Name() string { return "REST" }

func (c *restProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := getRESTEndpoints()
	require.NotEmpty(t, endpoints, "REST endpoints must be available")
	c.client = NewRESTDEKClient(t, endpoints[0])
	return nil
}

func (c *restProtocolClient) Teardown(t *testing.T) error {
	return nil
}

func (c *restProtocolClient) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	return c.client.CreateDEK(ctx, req)
}

func (c *restProtocolClient) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	return c.client.GetDEK(ctx, req)
}

func (c *restProtocolClient) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	return c.client.ListDEKs(ctx, req)
}

func (c *restProtocolClient) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	return c.client.RotateDEK(ctx, req)
}

func (c *restProtocolClient) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	return c.client.DeleteDEK(ctx, req)
}

func (c *restProtocolClient) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	return c.client.GetDEKStatus(ctx, req)
}

func (c *restProtocolClient) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	return c.client.SetDefaultDEK(ctx, req)
}

func (c *restProtocolClient) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	return c.client.SetDEKSafety(ctx, req)
}

func (c *restProtocolClient) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	return c.client.ClearDEKSafety(ctx, req)
}

func (c *restProtocolClient) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	return c.client.StartReencryption(ctx, req)
}

func (c *restProtocolClient) GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	return c.client.GetReencryptionProgress(ctx, req)
}

// =============================================================================
// gRPC Protocol Client Implementation
// =============================================================================

// grpcProtocolClient implements dekProtocol using gRPC.
type grpcProtocolClient struct {
	client *grpcclient.Client
	conn   *grpc.ClientConn
}

func (c *grpcProtocolClient) Name() string { return "gRPC" }

func (c *grpcProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := getGRPCEndpoints()
	require.NotEmpty(t, endpoints, "gRPC endpoints must be available")

	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg),
		grpcclient.WithDialOptions(grpc.WithPerRPCCredentials(helpers.TenantGRPCCredentials(t))))
	require.NoError(t, err, "Must connect to gRPC at %s", endpoints[0])
	c.client = client

	// Check if DEK service is available
	skipIfDEKUnavailable(t, client, ctx)

	return nil
}

func (c *grpcProtocolClient) Teardown(t *testing.T) error {
	if c.client != nil {
		c.client.Close()
	}
	return nil
}

func (c *grpcProtocolClient) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	return c.client.KMSCreateDEK(ctx, req)
}

func (c *grpcProtocolClient) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	return c.client.KMSGetDEK(ctx, req)
}

func (c *grpcProtocolClient) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	return c.client.KMSListDEKs(ctx, req)
}

func (c *grpcProtocolClient) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	return c.client.KMSRotateDEK(ctx, req)
}

func (c *grpcProtocolClient) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	return c.client.KMSDeleteDEK(ctx, req)
}

func (c *grpcProtocolClient) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	return c.client.KMSGetDEKStatus(ctx, req)
}

func (c *grpcProtocolClient) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	return c.client.KMSSetDefaultDEK(ctx, req)
}

func (c *grpcProtocolClient) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	return c.client.KMSSetDEKSafety(ctx, req)
}

func (c *grpcProtocolClient) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	return c.client.KMSClearDEKSafety(ctx, req)
}

func (c *grpcProtocolClient) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	return c.client.KMSStartReencryption(ctx, req)
}

func (c *grpcProtocolClient) GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	return c.client.KMSGetReencryptionProgress(ctx, req)
}

// unixSocketProtocolClient implements dekProtocol using Unix domain sockets.
type unixSocketProtocolClient struct {
	client *grpcclient.Client
	conn   *grpc.ClientConn
}

func (c *unixSocketProtocolClient) Name() string { return "UnixSocket" }

func (c *unixSocketProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	socketPath := os.Getenv("QUICBAAS_NODE1_UNIX")
	if socketPath == "" {
		socketPath = "/var/run/quicbaas/node1.sock"
	}

	// Check if socket exists
	_, err := net.Dial("unix", socketPath)
	require.NoError(t, err, "Unix socket must be available at %s", socketPath)

	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	tlsCfg.ServerName = "localhost"

	// Create gRPC connection over unix socket
	conn, err := grpc.DialContext(ctx,
		"unix://"+socketPath,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	)
	require.NoError(t, err, "Must connect to unix socket at %s", socketPath)
	c.conn = conn

	client, err := grpcclient.NewClient("unix://"+socketPath, grpcclient.WithTLSConfig(tlsCfg),
		grpcclient.WithDialOptions(grpc.WithPerRPCCredentials(helpers.TenantGRPCCredentials(t))))
	if err != nil {
		conn.Close()
	}
	require.NoError(t, err, "Must create gRPC client over unix socket")
	c.client = client

	// Check if DEK service is available
	skipIfDEKUnavailable(t, client, ctx)

	return nil
}

func (c *unixSocketProtocolClient) Teardown(t *testing.T) error {
	if c.client != nil {
		c.client.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}
	return nil
}

func (c *unixSocketProtocolClient) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	return c.client.KMSCreateDEK(ctx, req)
}

func (c *unixSocketProtocolClient) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	return c.client.KMSGetDEK(ctx, req)
}

func (c *unixSocketProtocolClient) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	return c.client.KMSListDEKs(ctx, req)
}

func (c *unixSocketProtocolClient) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	return c.client.KMSRotateDEK(ctx, req)
}

func (c *unixSocketProtocolClient) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	return c.client.KMSDeleteDEK(ctx, req)
}

func (c *unixSocketProtocolClient) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	return c.client.KMSGetDEKStatus(ctx, req)
}

func (c *unixSocketProtocolClient) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	return c.client.KMSSetDefaultDEK(ctx, req)
}

func (c *unixSocketProtocolClient) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	return c.client.KMSSetDEKSafety(ctx, req)
}

func (c *unixSocketProtocolClient) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	return c.client.KMSClearDEKSafety(ctx, req)
}

func (c *unixSocketProtocolClient) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	return c.client.KMSStartReencryption(ctx, req)
}

func (c *unixSocketProtocolClient) GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	return c.client.KMSGetReencryptionProgress(ctx, req)
}

// =============================================================================
// REST DEK Client
// =============================================================================

// RESTDEKClient wraps HTTP client for REST API DEK testing.
type RESTDEKClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewRESTDEKClient creates a new REST DEK client acting as the tenant
// principal.
func NewRESTDEKClient(t *testing.T, baseURL string) *RESTDEKClient {
	t.Helper()
	return &RESTDEKClient{
		baseURL:    baseURL,
		httpClient: helpers.NewTenantHTTPClient(t, 30*time.Second),
	}
}

// CreateDEK creates a DEK via REST API.
func (c *RESTDEKClient) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/kms/deks", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.CreateDEKResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetDEK retrieves a DEK via REST API.
func (c *RESTDEKClient) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/deks/%s", c.baseURL, req.Path)
	if req.Version > 0 {
		url += fmt.Sprintf("?version=%d", req.Version)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.GetDEKResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ListDEKs lists DEKs via REST API.
func (c *RESTDEKClient) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/deks", c.baseURL)

	// Add query parameters
	params := []string{}
	if req.Prefix != "" {
		params = append(params, fmt.Sprintf("prefix=%s", req.Prefix))
	}
	if req.Limit > 0 {
		params = append(params, fmt.Sprintf("limit=%d", req.Limit))
	}
	if req.Cursor != "" {
		params = append(params, fmt.Sprintf("cursor=%s", req.Cursor))
	}
	if req.Recursive {
		params = append(params, "recursive=true")
	}
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.ListDEKsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// RotateDEK rotates a DEK via REST API.
func (c *RESTDEKClient) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/kms/deks/%s/rotate", c.baseURL, req.Path)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.RotateDEKResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// DeleteDEK deletes a DEK via REST API.
func (c *RESTDEKClient) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/deks/%s", c.baseURL, req.Path)

	params := []string{}
	if req.Force {
		params = append(params, "force=true")
	}
	if req.MigrateTo != "" {
		params = append(params, fmt.Sprintf("migrate_to=%s", req.MigrateTo))
	}
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.DeleteDEKResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetDEKStatus retrieves DEK status via REST API.
func (c *RESTDEKClient) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/deks/%s/status", c.baseURL, req.Path)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.GetDEKStatusResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SetDefaultDEK sets a DEK as default via REST API.
// Uses POST with /set-default suffix to match server routing.
func (c *RESTDEKClient) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	// Use POST with /set-default suffix per server routing
	url := fmt.Sprintf("%s/api/v1/kms/deks/%s/set-default", c.baseURL, req.Path)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.SetDefaultDEKResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SetDEKSafety sets DEK safety limits via REST API.
func (c *RESTDEKClient) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/kms/deks/%s/safety", c.baseURL, req.Path)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.SetDEKSafetyResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ClearDEKSafety clears DEK safety limits via REST API.
func (c *RESTDEKClient) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/deks/%s/safety", c.baseURL, req.Path)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.ClearDEKSafetyResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// StartReencryption starts re-encryption via REST API.
// Uses POST with /reencrypt suffix to match server routing.
func (c *RESTDEKClient) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	// Use POST with /reencrypt suffix per server routing
	url := fmt.Sprintf("%s/api/v1/kms/deks/%s/reencrypt", c.baseURL, req.Path)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.StartReencryptionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetReencryptionProgress gets re-encryption progress via REST API.
// Uses GET with /reencrypt suffix to match server routing.
func (c *RESTDEKClient) GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	// Use GET with /reencrypt suffix per server routing
	url := fmt.Sprintf("%s/api/v1/kms/deks/%s/reencrypt", c.baseURL, req.Path)

	params := []string{}
	if req.OperationID != "" {
		params = append(params, fmt.Sprintf("operation_id=%s", req.OperationID))
	}
	if req.Verbose {
		params = append(params, "verbose=true")
	}
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.GetReencryptionProgressResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// Cleanup Helpers
// =============================================================================

// cleanupDEK attempts to delete a test DEK.
func cleanupDEK(t *testing.T, protocol dekProtocol, ctx context.Context, path string) {
	t.Helper()

	req := &service.DeleteDEKRequest{
		Path:  path,
		Force: true,
	}

	_, _ = protocol.DeleteDEK(ctx, req)
}

// cleanupDEKByClient attempts to delete a test DEK using gRPC client directly.
func cleanupDEKByClient(t *testing.T, client *grpcclient.Client, ctx context.Context, path string) {
	t.Helper()

	req := &service.DeleteDEKRequest{
		Path:  path,
		Force: true,
	}

	_, _ = client.KMSDeleteDEK(ctx, req)
}

// =============================================================================
// Assertion Helpers
// =============================================================================

// assertDEKExists verifies a DEK exists.
func assertDEKExists(t *testing.T, protocol dekProtocol, ctx context.Context, path string) {
	t.Helper()

	req := &service.GetDEKRequest{
		Path: path,
	}

	resp, err := protocol.GetDEK(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.DEK)
	require.Equal(t, path, resp.DEK.Path)
}

// assertDEKNotFound verifies a DEK does not exist.
func assertDEKNotFound(t *testing.T, protocol dekProtocol, ctx context.Context, path string) {
	t.Helper()

	req := &service.GetDEKRequest{
		Path: path,
	}

	_, err := protocol.GetDEK(ctx, req)
	require.Error(t, err)
}
