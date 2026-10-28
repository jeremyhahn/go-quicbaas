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

package helpers

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/id"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// SetupGRPCConnectionToEndpoint creates a gRPC connection to a specific endpoint.
// Uses non-blocking dial with a 30-second timeout to prevent hanging tests.
// opts are appended to the TLS dial options, e.g. per-RPC credentials.
func SetupGRPCConnectionToEndpoint(t *testing.T, ctx context.Context, endpoint string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	tlsCfg, err := IntegrationTLSConfig()
	if err != nil {
		return nil, err
	}

	// Create a connection with timeout - don't use WithBlock() as it can hang
	dialOpts := append([]grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	}, opts...)
	conn, err := grpc.DialContext(ctx, endpoint, dialOpts...)
	if err != nil {
		return nil, err
	}

	// Verify connection is working by waiting for ready state
	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return conn, nil
		}
		if state == connectivity.TransientFailure || state == connectivity.Shutdown {
			conn.Close()
			return nil, fmt.Errorf("connection failed: state=%v", state)
		}
		if !conn.WaitForStateChange(dialCtx, state) {
			conn.Close()
			return nil, fmt.Errorf("timeout waiting for connection to %s", endpoint)
		}
	}
}

// MustSetupGRPCConnectionToEndpoint creates a gRPC connection or fails the test
func MustSetupGRPCConnectionToEndpoint(t *testing.T, ctx context.Context, endpoint string, opts ...grpc.DialOption) *grpc.ClientConn {
	conn, err := SetupGRPCConnectionToEndpoint(t, ctx, endpoint, opts...)
	require.NoError(t, err, "Failed to connect to gRPC server at %s", endpoint)
	return conn
}

// TestClient is a simple wrapper around quicbaas.Client for testing purposes.
// It provides convenience methods that match the old cli.Client API
// to minimize changes in integration tests.
type TestClient struct {
	client quicbaas.Client
}

// NewTestClient creates a new TestClient with the given server URL.
// The serverURL can be either an HTTP URL (e.g., https://test-node1:8080) or
// a gRPC address (e.g., test-node1:50051). If an HTTP URL is provided,
// it extracts the host and converts to the gRPC port (50051). The client acts
// as the tenant principal of the first REST endpoint's cluster.
func NewTestClient(t *testing.T, serverURL string) *TestClient {
	t.Helper()
	grpcAddr := serverURL

	// If the URL has a scheme, extract the host and use the gRPC port
	if strings.HasPrefix(serverURL, "http://") || strings.HasPrefix(serverURL, "https://") {
		parsed, err := url.Parse(serverURL)
		if err == nil && parsed.Hostname() != "" {
			// Use the hostname with gRPC port 50051
			grpcAddr = parsed.Hostname() + ":50051"
		}
	}

	tlsCfg, err := IntegrationTLSConfig()
	if err != nil {
		panic(fmt.Sprintf("failed to load integration TLS config: %v", err))
	}

	// Create a real SDK client connection. Its KV and transaction calls need
	// the grants go-qrdb gives the tenant's Operator role, not its Admin role.
	bearer := tenantOperatorAuth.mustBearer(t, GetRESTEndpoints()[0])
	client, err := quicbaas.NewClient(grpcAddr, quicbaas.WithTLSConfig(tlsCfg), quicbaas.WithAuthToken(bearer))
	if err != nil {
		panic(fmt.Sprintf("failed to create test client: %v", err))
	}
	return &TestClient{
		client: client,
	}
}

// Set stores a key-value pair using default settings.
func (tc *TestClient) Set(ctx context.Context, key string, value []byte) error {
	req := &service.PutRequest{
		EntityType:  "default",
		ID:          id.HashString(key),
		Data:        value,
		Consistency: service.ConsistencyLevelLinearizable,
	}

	_, err := tc.client.KV().Put(ctx, req)
	return err
}

// Get retrieves a value by key using default settings.
func (tc *TestClient) Get(ctx context.Context, key string) ([]byte, error) {
	req := &service.GetRequest{
		EntityType:  "default",
		ID:          id.HashString(key),
		Consistency: service.ConsistencyLevelLinearizable,
	}

	resp, err := tc.client.KV().Get(ctx, req)
	if err != nil {
		return nil, err
	}

	if !resp.Found {
		return nil, &ClientError{Message: "key not found"}
	}

	return resp.Data, nil
}

// Delete removes a key-value pair using default settings.
func (tc *TestClient) Delete(ctx context.Context, key string) error {
	req := &service.DeleteRequest{
		EntityType:  "default",
		ID:          id.HashString(key),
		Consistency: service.ConsistencyLevelLinearizable,
	}

	resp, err := tc.client.KV().Delete(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Deleted {
		return &ClientError{Message: "key not deleted"}
	}

	return nil
}

// Close closes the client connection.
func (tc *TestClient) Close() error {
	return tc.client.Close()
}

// Client returns the underlying quicbaas.Client.
func (tc *TestClient) Client() quicbaas.Client {
	return tc.client
}

// ClientError represents a client operation error.
type ClientError struct {
	Message string
}

// Error implements the error interface.
func (e *ClientError) Error() string {
	return e.Message
}
