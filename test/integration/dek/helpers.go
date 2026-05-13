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
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	quicgo "github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
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

// getQUICEndpoints returns all QUIC endpoints for testing.
func getQUICEndpoints() []string {
	return helpers.GetQUICEndpoints()
}

// getMCPEndpoint returns the MCP endpoint for testing.
func getMCPEndpoint() string {
	return helpers.GetMCPEndpoints()[0]
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
	c.client = NewRESTDEKClient(endpoints[0])
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

	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
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

// =============================================================================
// QUIC Protocol Client Implementation
// =============================================================================

// quicProtocolClient implements dekProtocol using QUIC.
type quicProtocolClient struct {
	client *QUICDEKClient
}

func (c *quicProtocolClient) Name() string { return "QUIC" }

func (c *quicProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := getQUICEndpoints()
	require.NotEmpty(t, endpoints, "QUIC endpoints must be available")

	client, err := NewQUICDEKClient(endpoints[0])
	require.NoError(t, err, "Must connect to QUIC at %s", endpoints[0])
	c.client = client

	// Verify connection
	err = c.client.Health(ctx)
	if err != nil {
		c.client.Close()
	}
	require.NoError(t, err, "QUIC health check must succeed")

	return nil
}

func (c *quicProtocolClient) Teardown(t *testing.T) error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// Reconnect closes the current connection and creates a new one.
// This is useful after concurrent tests that may leave the connection in a bad state.
func (c *quicProtocolClient) Reconnect(ctx context.Context) error {
	if c.client != nil {
		c.client.Close()
	}

	endpoints := getQUICEndpoints()
	if len(endpoints) == 0 {
		return fmt.Errorf("no QUIC endpoints available for reconnect")
	}

	client, err := NewQUICDEKClient(endpoints[0])
	if err != nil {
		return fmt.Errorf("failed to reconnect: %w", err)
	}
	c.client = client

	// Verify connection
	if err := c.client.Health(ctx); err != nil {
		c.client.Close()
		return fmt.Errorf("reconnect health check failed: %w", err)
	}

	return nil
}

func (c *quicProtocolClient) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	return c.client.CreateDEK(ctx, req)
}

func (c *quicProtocolClient) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	return c.client.GetDEK(ctx, req)
}

func (c *quicProtocolClient) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	return c.client.ListDEKs(ctx, req)
}

func (c *quicProtocolClient) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	return c.client.RotateDEK(ctx, req)
}

func (c *quicProtocolClient) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	return c.client.DeleteDEK(ctx, req)
}

func (c *quicProtocolClient) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	return c.client.GetDEKStatus(ctx, req)
}

func (c *quicProtocolClient) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	return c.client.SetDefaultDEK(ctx, req)
}

func (c *quicProtocolClient) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	return c.client.SetDEKSafety(ctx, req)
}

func (c *quicProtocolClient) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	return c.client.ClearDEKSafety(ctx, req)
}

func (c *quicProtocolClient) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	return c.client.StartReencryption(ctx, req)
}

func (c *quicProtocolClient) GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	return c.client.GetReencryptionProgress(ctx, req)
}

// =============================================================================
// Unix Socket Protocol Client Implementation
// =============================================================================

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

	// Create gRPC connection over unix socket
	conn, err := grpc.DialContext(ctx,
		"unix://"+socketPath,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	)
	require.NoError(t, err, "Must connect to unix socket at %s", socketPath)
	c.conn = conn

	client, err := grpcclient.NewClient("unix://"+socketPath, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
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
// MCP Protocol Client Implementation
// =============================================================================

// mcpProtocolClient implements dekProtocol using MCP (Model Context Protocol).
type mcpProtocolClient struct {
	client    *MCPDEKClient
	requestID atomic.Int64
}

func (c *mcpProtocolClient) Name() string { return "MCP" }

func (c *mcpProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	endpoint := getMCPEndpoint()

	client, err := NewMCPDEKClient(endpoint)
	require.NoError(t, err, "Must create MCP client at %s", endpoint)
	c.client = client

	// Verify connection by calling ping
	err = c.client.Ping(ctx)
	require.NoError(t, err, "MCP ping must succeed")

	return nil
}

func (c *mcpProtocolClient) Teardown(t *testing.T) error {
	// MCP HTTP client doesn't need explicit cleanup
	return nil
}

func (c *mcpProtocolClient) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	return c.client.CreateDEK(ctx, req)
}

func (c *mcpProtocolClient) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	return c.client.GetDEK(ctx, req)
}

func (c *mcpProtocolClient) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	return c.client.ListDEKs(ctx, req)
}

func (c *mcpProtocolClient) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	return c.client.RotateDEK(ctx, req)
}

func (c *mcpProtocolClient) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	return c.client.DeleteDEK(ctx, req)
}

func (c *mcpProtocolClient) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	return c.client.GetDEKStatus(ctx, req)
}

func (c *mcpProtocolClient) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	return c.client.SetDefaultDEK(ctx, req)
}

func (c *mcpProtocolClient) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	return c.client.SetDEKSafety(ctx, req)
}

func (c *mcpProtocolClient) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	return c.client.ClearDEKSafety(ctx, req)
}

func (c *mcpProtocolClient) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	return c.client.StartReencryption(ctx, req)
}

func (c *mcpProtocolClient) GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	return c.client.GetReencryptionProgress(ctx, req)
}

// =============================================================================
// MCP DEK Client
// =============================================================================

// MCPDEKClient wraps HTTP client for MCP protocol DEK testing.
type MCPDEKClient struct {
	baseURL       string
	httpClient    *http.Client
	requestID     atomic.Int64
	accessToken   string
	authenticated bool
	initialized   bool
}

// MCPJSONRPCRequest represents a JSON-RPC 2.0 request for MCP.
type MCPJSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// MCPJSONRPCResponse represents a JSON-RPC 2.0 response from MCP.
type MCPJSONRPCResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      int64            `json:"id"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *MCPJSONRPCError `json:"error,omitempty"`
}

// MCPJSONRPCError represents a JSON-RPC 2.0 error.
type MCPJSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// NewMCPDEKClient creates a new MCP DEK client.
func NewMCPDEKClient(baseURL string) (*MCPDEKClient, error) {
	return &MCPDEKClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// Initialize initializes the MCP connection.
func (c *MCPDEKClient) Initialize(ctx context.Context) error {
	if c.initialized {
		return nil
	}

	requestID := c.requestID.Add(1)
	req := MCPJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      requestID,
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]interface{}{
				"name":    "quicbaas-dek-integration-test",
				"version": "1.0.0",
			},
		},
	}

	_, err := c.sendRequest(ctx, req)
	if err != nil {
		return err
	}
	c.initialized = true
	return nil
}

// ensureAuthenticated obtains a bearer token via the cluster's PIV mTLS
// login endpoint when not already authenticated. Password-based login has
// been removed across all transports; tests mint a bearer through the
// REST PIV endpoint and reuse it for MCP requests.
func (c *MCPDEKClient) ensureAuthenticated(ctx context.Context) error {
	if c.authenticated {
		return nil
	}

	// Initialize first
	if err := c.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
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

// sendRequest sends a JSON-RPC request without authentication requirement.
func (c *MCPDEKClient) sendRequest(ctx context.Context, req MCPJSONRPCRequest) (json.RawMessage, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.accessToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.accessToken)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var jsonRPCResp MCPJSONRPCResponse
	if err := json.Unmarshal(respBody, &jsonRPCResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if jsonRPCResp.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s", jsonRPCResp.Error.Code, jsonRPCResp.Error.Message)
	}

	return jsonRPCResp.Result, nil
}

// callTool executes an MCP tool and returns the result.
func (c *MCPDEKClient) callTool(ctx context.Context, toolName string, arguments map[string]interface{}) (map[string]interface{}, error) {
	// Ensure authenticated before calling tools
	if err := c.ensureAuthenticated(ctx); err != nil {
		return nil, err
	}

	requestID := c.requestID.Add(1)

	req := MCPJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      requestID,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      toolName,
			"arguments": arguments,
		},
	}

	result, err := c.sendRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	var wrappedResult map[string]interface{}
	if err := json.Unmarshal(result, &wrappedResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	// Unwrap MCP content format: {"content": [{"type": "text", "text": "..."}]}
	content, ok := wrappedResult["content"].([]interface{})
	if !ok || len(content) == 0 {
		return nil, fmt.Errorf("invalid MCP response: missing or empty content array")
	}

	firstContent, ok := content[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid MCP response: content element is not an object")
	}

	textContent, ok := firstContent["text"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid MCP response: missing text field in content")
	}

	var parsedResult map[string]interface{}
	if err := json.Unmarshal([]byte(textContent), &parsedResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal content text: %w", err)
	}

	return parsedResult, nil
}

// Ping checks connectivity to the MCP server.
func (c *MCPDEKClient) Ping(ctx context.Context) error {
	requestID := c.requestID.Add(1)

	req := MCPJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      requestID,
		Method:  "ping",
	}

	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send ping request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// CreateDEK creates a DEK via MCP.
func (c *MCPDEKClient) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	args := map[string]interface{}{
		"path":      req.Path,
		"algorithm": string(req.Algorithm),
	}
	if req.Description != "" {
		args["description"] = req.Description
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

	result, err := c.callTool(ctx, "quicbaas_dek_create", args)
	if err != nil {
		return nil, err
	}

	return parseDEKCreateResponse(result)
}

// GetDEK retrieves a DEK via MCP.
func (c *MCPDEKClient) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	args := map[string]interface{}{
		"path": req.Path,
	}
	if req.Version > 0 {
		args["version"] = req.Version
	}

	result, err := c.callTool(ctx, "quicbaas_dek_get", args)
	if err != nil {
		return nil, err
	}

	return parseDEKGetResponse(result)
}

// ListDEKs lists DEKs via MCP.
func (c *MCPDEKClient) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	args := map[string]interface{}{}
	if req.Prefix != "" {
		args["prefix"] = req.Prefix
	}
	if req.Limit > 0 {
		args["limit"] = req.Limit
	}
	if req.Cursor != "" {
		args["cursor"] = req.Cursor
	}
	if req.Recursive {
		args["recursive"] = true
	}

	result, err := c.callTool(ctx, "quicbaas_dek_list", args)
	if err != nil {
		return nil, err
	}

	return parseDEKListResponse(result)
}

// RotateDEK rotates a DEK via MCP.
func (c *MCPDEKClient) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	args := map[string]interface{}{
		"path": req.Path,
	}
	if req.Background {
		args["background"] = true
	}
	if req.RetainOldVersion {
		args["retain_old_version"] = true
	}

	result, err := c.callTool(ctx, "quicbaas_dek_rotate", args)
	if err != nil {
		return nil, err
	}

	return parseDEKRotateResponse(result)
}

// DeleteDEK deletes a DEK via MCP.
func (c *MCPDEKClient) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	args := map[string]interface{}{
		"path": req.Path,
	}
	if req.Force {
		args["force"] = true
	}
	if req.MigrateTo != "" {
		args["migrate_to"] = req.MigrateTo
	}

	result, err := c.callTool(ctx, "quicbaas_dek_delete", args)
	if err != nil {
		return nil, err
	}

	return parseDEKDeleteResponse(result)
}

// GetDEKStatus retrieves DEK status via MCP.
func (c *MCPDEKClient) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	args := map[string]interface{}{
		"path": req.Path,
	}

	result, err := c.callTool(ctx, "quicbaas_dek_status", args)
	if err != nil {
		return nil, err
	}

	return parseDEKStatusResponse(result)
}

// SetDefaultDEK sets a DEK as default via MCP.
func (c *MCPDEKClient) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	args := map[string]interface{}{
		"path": req.Path,
	}

	result, err := c.callTool(ctx, "quicbaas_dek_set_default", args)
	if err != nil {
		return nil, err
	}

	return parseDEKSetDefaultResponse(result)
}

// SetDEKSafety sets DEK safety limits via MCP.
func (c *MCPDEKClient) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	args := map[string]interface{}{
		"path": req.Path,
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

	result, err := c.callTool(ctx, "quicbaas_dek_set_safety", args)
	if err != nil {
		return nil, err
	}

	return parseDEKSetSafetyResponse(result)
}

// ClearDEKSafety clears DEK safety limits via MCP.
func (c *MCPDEKClient) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	args := map[string]interface{}{
		"path": req.Path,
	}

	result, err := c.callTool(ctx, "quicbaas_dek_clear_safety", args)
	if err != nil {
		return nil, err
	}

	return parseDEKClearSafetyResponse(result)
}

// StartReencryption starts re-encryption via MCP.
func (c *MCPDEKClient) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	args := map[string]interface{}{
		"from_version": req.FromVersion,
		"to_version":   req.ToVersion,
	}
	if req.Path != "" {
		args["path"] = req.Path
	}
	if req.BatchSize > 0 {
		args["batch_size"] = req.BatchSize
	}
	if req.Concurrency > 0 {
		args["concurrency"] = req.Concurrency
	}
	if req.ThrottleRate > 0 {
		args["throttle_rate"] = req.ThrottleRate
	}
	if req.Force {
		args["force"] = true
	}

	result, err := c.callTool(ctx, "quicbaas_dek_start_reencryption", args)
	if err != nil {
		return nil, err
	}

	return parseDEKStartReencryptionResponse(result)
}

// GetReencryptionProgress gets re-encryption progress via MCP.
func (c *MCPDEKClient) GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	args := map[string]interface{}{}
	if req.Path != "" {
		args["path"] = req.Path
	}
	if req.OperationID != "" {
		args["operation_id"] = req.OperationID
	}
	if req.Verbose {
		args["verbose"] = true
	}

	result, err := c.callTool(ctx, "quicbaas_dek_get_reencryption_progress", args)
	if err != nil {
		return nil, err
	}

	return parseDEKGetReencryptionProgressResponse(result)
}

// =============================================================================
// MCP Response Parsers
// =============================================================================

func parseDEKMetadata(m map[string]interface{}) *service.DEKMetadata {
	if m == nil {
		return nil
	}
	dek := &service.DEKMetadata{}
	if v, ok := m["path"].(string); ok {
		dek.Path = v
	}
	if v, ok := m["algorithm"].(string); ok {
		dek.Algorithm = service.KeyAlgorithm(v)
	}
	if v, ok := m["version"].(float64); ok {
		dek.Version = uint64(v)
	}
	if v, ok := m["status"].(string); ok {
		dek.Status = v
	}
	if v, ok := m["is_default"].(bool); ok {
		dek.IsDefault = v
	}
	if v, ok := m["description"].(string); ok {
		dek.Description = v
	}
	if v, ok := m["usage_count"].(float64); ok {
		dek.UsageCount = uint64(v)
	}
	if v, ok := m["bytes_used"].(float64); ok {
		dek.BytesUsed = uint64(v)
	}
	return dek
}

func parseDEKCreateResponse(result map[string]interface{}) (*service.CreateDEKResponse, error) {
	resp := &service.CreateDEKResponse{}
	if dekMap, ok := result["dek"].(map[string]interface{}); ok {
		resp.DEK = parseDEKMetadata(dekMap)
	}
	return resp, nil
}

func parseDEKGetResponse(result map[string]interface{}) (*service.GetDEKResponse, error) {
	resp := &service.GetDEKResponse{}
	if dekMap, ok := result["dek"].(map[string]interface{}); ok {
		resp.DEK = parseDEKMetadata(dekMap)
	}
	return resp, nil
}

func parseDEKListResponse(result map[string]interface{}) (*service.ListDEKsResponse, error) {
	resp := &service.ListDEKsResponse{}
	if deksSlice, ok := result["deks"].([]interface{}); ok {
		for _, item := range deksSlice {
			if dekMap, ok := item.(map[string]interface{}); ok {
				resp.DEKs = append(resp.DEKs, parseDEKMetadata(dekMap))
			}
		}
	}
	if v, ok := result["total_count"].(float64); ok {
		resp.TotalCount = int(v)
	}
	if v, ok := result["has_more"].(bool); ok {
		resp.HasMore = v
	}
	if v, ok := result["next_cursor"].(string); ok {
		resp.NextCursor = v
	}
	return resp, nil
}

func parseDEKRotateResponse(result map[string]interface{}) (*service.RotateDEKResponse, error) {
	resp := &service.RotateDEKResponse{}
	if v, ok := result["path"].(string); ok {
		resp.Path = v
	}
	if v, ok := result["old_version"].(float64); ok {
		resp.OldVersion = uint64(v)
	}
	if v, ok := result["new_version"].(float64); ok {
		resp.NewVersion = uint64(v)
	}
	return resp, nil
}

func parseDEKDeleteResponse(result map[string]interface{}) (*service.DeleteDEKResponse, error) {
	resp := &service.DeleteDEKResponse{}
	if v, ok := result["path"].(string); ok {
		resp.Path = v
	}
	if v, ok := result["deleted"].(bool); ok {
		resp.Deleted = v
	}
	return resp, nil
}

func parseDEKStatusResponse(result map[string]interface{}) (*service.GetDEKStatusResponse, error) {
	resp := &service.GetDEKStatusResponse{}
	if dekMap, ok := result["dek"].(map[string]interface{}); ok {
		resp.DEK = parseDEKMetadata(dekMap)
	}
	return resp, nil
}

func parseDEKSetDefaultResponse(result map[string]interface{}) (*service.SetDefaultDEKResponse, error) {
	resp := &service.SetDefaultDEKResponse{}
	if v, ok := result["path"].(string); ok {
		resp.Path = v
	}
	if v, ok := result["previous_default"].(string); ok {
		resp.PreviousDefault = v
	}
	return resp, nil
}

func parseDEKSetSafetyResponse(result map[string]interface{}) (*service.SetDEKSafetyResponse, error) {
	resp := &service.SetDEKSafetyResponse{}
	if v, ok := result["path"].(string); ok {
		resp.Path = v
	}
	if configMap, ok := result["config"].(map[string]interface{}); ok {
		resp.Config = &service.DEKSafetyConfig{}
		if v, ok := configMap["max_operations"].(float64); ok {
			resp.Config.MaxOperations = uint64(v)
		}
		if v, ok := configMap["max_bytes"].(float64); ok {
			resp.Config.MaxBytes = uint64(v)
		}
		if v, ok := configMap["warning_percent"].(float64); ok {
			resp.Config.WarningPercent = int(v)
		}
	}
	return resp, nil
}

func parseDEKClearSafetyResponse(result map[string]interface{}) (*service.ClearDEKSafetyResponse, error) {
	resp := &service.ClearDEKSafetyResponse{}
	if v, ok := result["path"].(string); ok {
		resp.Path = v
	}
	if v, ok := result["cleared"].(bool); ok {
		resp.Cleared = v
	}
	return resp, nil
}

func parseDEKStartReencryptionResponse(result map[string]interface{}) (*service.StartReencryptionResponse, error) {
	resp := &service.StartReencryptionResponse{}
	if v, ok := result["operation_id"].(string); ok {
		resp.OperationID = v
	}
	if v, ok := result["path"].(string); ok {
		resp.Path = v
	}
	if v, ok := result["started"].(bool); ok {
		resp.Started = v
	}
	if v, ok := result["estimated_items"].(float64); ok {
		resp.EstimatedItems = int(v) // EstimatedItems is int, not int64
	}
	if v, ok := result["total_entities"].(float64); ok {
		resp.TotalEntities = int64(v) // TotalEntities is int64
	}
	return resp, nil
}

func parseDEKGetReencryptionProgressResponse(result map[string]interface{}) (*service.GetReencryptionProgressResponse, error) {
	resp := &service.GetReencryptionProgressResponse{}
	if v, ok := result["path"].(string); ok {
		resp.Path = v
	}
	if v, ok := result["operation_id"].(string); ok {
		resp.OperationID = v
	}
	if v, ok := result["status"].(string); ok {
		resp.Status = v
	}
	if progressMap, ok := result["progress"].(map[string]interface{}); ok {
		resp.Progress = &service.ReencryptionProgress{}
		if v, ok := progressMap["status"].(string); ok {
			resp.Progress.Status = v
		}
		if v, ok := progressMap["in_progress"].(bool); ok {
			resp.Progress.InProgress = v
		}
		if v, ok := progressMap["total_keys"].(float64); ok {
			resp.Progress.TotalKeys = uint64(v) // TotalKeys is uint64
		}
		if v, ok := progressMap["processed_keys"].(float64); ok {
			resp.Progress.ProcessedKeys = uint64(v) // ProcessedKeys is uint64
		}
		if v, ok := progressMap["percent_complete"].(float64); ok {
			resp.Progress.PercentComplete = v
		}
	}
	return resp, nil
}

// =============================================================================
// REST DEK Client
// =============================================================================

// RESTDEKClient wraps HTTP client for REST API DEK testing.
type RESTDEKClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewRESTDEKClient creates a new REST DEK client.
func NewRESTDEKClient(baseURL string) *RESTDEKClient {
	return &RESTDEKClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
				TLSHandshakeTimeout: 10 * time.Second,
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
		},
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

// =============================================================================
// QUIC DEK Client
// =============================================================================

// QUICDEKClient wraps QUIC connection for DEK testing.
type QUICDEKClient struct {
	addr       string
	conn       *quicgo.Conn
	tlsConfig  *tls.Config
	ctx        context.Context
	cancelFunc context.CancelFunc
}

// NewQUICDEKClient creates a new QUIC client for DEK operations.
func NewQUICDEKClient(addr string) (*QUICDEKClient, error) {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true, // Test only - skip certificate verification
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{"quicbaas-quic"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := quicgo.DialAddr(ctx, addr, tlsConf, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to dial QUIC server at %s: %w", addr, err)
	}

	clientCtx, clientCancel := context.WithCancel(context.Background())

	return &QUICDEKClient{
		addr:       addr,
		conn:       conn,
		tlsConfig:  tlsConf,
		ctx:        clientCtx,
		cancelFunc: clientCancel,
	}, nil
}

// Close closes the QUIC connection.
func (c *QUICDEKClient) Close() error {
	c.cancelFunc()
	if c.conn != nil {
		return c.conn.CloseWithError(0, "client closing")
	}
	return nil
}

// sendRequest sends a QUIC request and returns the response.
func (c *QUICDEKClient) sendRequest(ctx context.Context, opcode byte, reqPayload []byte) (*quic.Response, error) {
	stream, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()

	// Write request
	req := &quic.Request{
		Opcode:  opcode,
		Payload: reqPayload,
	}

	if err := quic.WriteRequest(stream, req); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Read response
	resp, err := quic.ReadResponse(stream)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check status
	if resp.Status != quic.StatusOK {
		errMsg := string(resp.Payload)
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return nil, quic.StatusToError(resp.Status, errMsg)
	}

	return resp, nil
}

// Health checks server health via QUIC.
func (c *QUICDEKClient) Health(ctx context.Context) error {
	_, err := c.sendRequest(ctx, quic.OpHealth, nil)
	return err
}

// CreateDEK creates a DEK via QUIC.
func (c *QUICDEKClient) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	payload, err := quic.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quic.OpKMSDEKCreate, payload)
	if err != nil {
		return nil, err
	}

	var result service.CreateDEKResponse
	if err := quic.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetDEK retrieves a DEK via QUIC.
func (c *QUICDEKClient) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	payload, err := quic.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quic.OpKMSDEKGet, payload)
	if err != nil {
		return nil, err
	}

	var result service.GetDEKResponse
	if err := quic.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ListDEKs lists DEKs via QUIC.
func (c *QUICDEKClient) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	payload, err := quic.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quic.OpKMSDEKList, payload)
	if err != nil {
		return nil, err
	}

	var result service.ListDEKsResponse
	if err := quic.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// RotateDEK rotates a DEK via QUIC.
func (c *QUICDEKClient) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	payload, err := quic.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quic.OpKMSDEKRotate, payload)
	if err != nil {
		return nil, err
	}

	var result service.RotateDEKResponse
	if err := quic.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// DeleteDEK deletes a DEK via QUIC.
func (c *QUICDEKClient) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	payload, err := quic.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quic.OpKMSDEKDelete, payload)
	if err != nil {
		return nil, err
	}

	var result service.DeleteDEKResponse
	if err := quic.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetDEKStatus retrieves DEK status via QUIC.
func (c *QUICDEKClient) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	payload, err := quic.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quic.OpKMSDEKStatus, payload)
	if err != nil {
		return nil, err
	}

	var result service.GetDEKStatusResponse
	if err := quic.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SetDefaultDEK sets a DEK as default via QUIC.
func (c *QUICDEKClient) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	payload, err := quic.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quic.OpKMSDEKSetDefault, payload)
	if err != nil {
		return nil, err
	}

	var result service.SetDefaultDEKResponse
	if err := quic.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SetDEKSafety sets DEK safety limits via QUIC.
func (c *QUICDEKClient) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	payload, err := quic.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quic.OpKMSDEKSetSafety, payload)
	if err != nil {
		return nil, err
	}

	var result service.SetDEKSafetyResponse
	if err := quic.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ClearDEKSafety clears DEK safety limits via QUIC.
func (c *QUICDEKClient) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	payload, err := quic.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quic.OpKMSDEKClearSafety, payload)
	if err != nil {
		return nil, err
	}

	var result service.ClearDEKSafetyResponse
	if err := quic.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// StartReencryption starts re-encryption via QUIC.
func (c *QUICDEKClient) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	payload, err := quic.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quic.OpKMSDEKStartReencryption, payload)
	if err != nil {
		return nil, err
	}

	var result service.StartReencryptionResponse
	if err := quic.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetReencryptionProgress gets re-encryption progress via QUIC.
func (c *QUICDEKClient) GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	payload, err := quic.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quic.OpKMSDEKGetReencryptProgress, payload)
	if err != nil {
		return nil, err
	}

	var result service.GetReencryptionProgressResponse
	if err := quic.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// =============================================================================
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
