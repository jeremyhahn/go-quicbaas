# Multi-Protocol Integration Test Guide

> **Status: REFERENCE** · Last verified: 2026-08-18
> Writing multi-protocol integration tests for QuicBaaS features.

This guide describes how to write multi-protocol integration tests for QuicBaaS features.

## Overview

QuicBaaS supports 6 protocols that must be tested consistently:

| Protocol | Transport | Use Case |
|----------|-----------|----------|
| **Embedded** | In-process | Unit tests, embedded deployments |
| **REST** | HTTP/JSON | Web applications, debugging |
| **gRPC** | HTTP/2 + Protobuf | High-performance services |
| **QUIC** | UDP + Binary | Low-latency, lossy networks |
| **Unix Socket** | gRPC over UDS | Local IPC, container sidecars |
| **MCP** | JSON-RPC 2.0 | AI/LLM tool integration |

## Authenticating Data-Plane Calls

`helpers.IntegrationTLSConfig()` gives you server trust only. It sends no client
certificate and no bearer. Every entity and batch RPC or route is
permission-gated, so a call made over it alone is refused as `Unauthenticated`
(gRPC) or `401` (REST) before any handler runs. Only public methods such as
Health succeed.

Suites that read or write entities act as the **data-plane principal**:

- `helpers.DataPlaneGRPCCredentials(t)`: pass it to the dial as
  `grpc.WithPerRPCCredentials(...)`.
- `helpers.NewDataPlaneHTTPClient(t, timeout)`: the `NewHTTPClient` client
  with the bearer set on every request.
- `helpers.AuthDataPlane(t, baseURL)`: the raw bearer.

On first use in a test process, the system admin creates three things on the
first REST endpoint's cluster:

1. a tenant named `itest-dp-<unixnano>`;
2. a role named `integration-data-plane` (id `integration-data-plane-<tenant>`,
   because qrdb claims role ids across all tenants) holding exactly
   `dataplane.Permissions`
   (`kv:get`, `kv:put`, `kv:delete`, `kv:list`, `kv:range`, `batch:execute`);
3. a PIV user bound to that role.

The bearer is cached and renewed by PIV login when it expires. The principal
is neither anonymous nor the system admin, so a test still fails if a route
starts requiring a permission outside that set.
`pkg/api/grpc`'s `TestAuthorizationInterceptor_IntegrationDataPlaneRole` pins
`dataplane.Permissions` to the server's authorization mapping in the unit tier.

## Protocol Adapter Template

### File Structure

```
test/integration/myfeature/
├── main_test.go           # TestMain setup
├── multiprotocol_test.go  # Multi-protocol tests (all 6 protocols)
└── helpers.go             # Feature-specific helpers (optional)
```

### Complete Template

```go
//go:build integration && quic

package myfeature

import (
	"bytes"
	"context"
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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// Protocol Interface
// =============================================================================

// myFeatureProtocol abstracts protocol differences for testing.
type myFeatureProtocol interface {
	Setup(t *testing.T, ctx context.Context) error
	Teardown(t *testing.T) error
	DoOperation(ctx context.Context, req *MyRequest) (*MyResponse, error)
	Name() string
}

// =============================================================================
// Result Types (protocol-agnostic)
// =============================================================================

type MyRequest struct {
	// Request fields
}

type MyResponse struct {
	// Response fields
}

// =============================================================================
// Error Types
// =============================================================================

type ErrEmbeddedNotAvailable struct{}

func (e ErrEmbeddedNotAvailable) Error() string {
	return "embedded protocol not available in integration test context (requires in-process service instance)"
}

type ErrClientNotInitialized struct{}

func (e ErrClientNotInitialized) Error() string {
	return "client not initialized"
}

// =============================================================================
// Main Test Function
// =============================================================================

func TestMyFeatureMultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	protocols := getAllProtocols()

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			if err != nil {
				t.Skipf("Skipping %s: setup failed: %v", protocol.Name(), err)
				return
			}
			defer func() {
				if teardownErr := protocol.Teardown(t); teardownErr != nil {
					t.Logf("Warning: teardown error for %s: %v", protocol.Name(), teardownErr)
				}
			}()

			// Run subtests
			t.Run("BasicOperation", func(t *testing.T) {
				testBasicOperation(t, ctx, protocol)
			})

			t.Run("ErrorHandling", func(t *testing.T) {
				testErrorHandling(t, ctx, protocol)
			})
		})
	}
}

func testBasicOperation(t *testing.T, ctx context.Context, protocol myFeatureProtocol) {
	t.Helper()
	// Test implementation
}

func testErrorHandling(t *testing.T, ctx context.Context, protocol myFeatureProtocol) {
	t.Helper()
	// Test implementation
}

// =============================================================================
// Protocol List
// =============================================================================

func getAllProtocols() []myFeatureProtocol {
	return []myFeatureProtocol{
		&embeddedClient{},
		&restClient{},
		&grpcClient{},
		&quicClient{},
		&unixSocketClient{},
		&mcpClient{},
	}
}

// =============================================================================
// 1. Embedded Protocol Client
// =============================================================================

type embeddedClient struct {
	client quicbaas.Client
}

func (c *embeddedClient) Name() string { return "Embedded" }

func (c *embeddedClient) Setup(t *testing.T, ctx context.Context) error {
	// Embedded requires an in-process service.QuicBaaS instance.
	// In integration tests against a remote server, skip gracefully.
	return ErrEmbeddedNotAvailable{}
}

func (c *embeddedClient) Teardown(t *testing.T) error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *embeddedClient) DoOperation(ctx context.Context, req *MyRequest) (*MyResponse, error) {
	if c.client == nil {
		return nil, ErrClientNotInitialized{}
	}
	// Use SDK client: c.client.MyFeature().DoOperation(ctx, req)
	return nil, nil
}

// =============================================================================
// 2. REST Protocol Client
// =============================================================================

type restClient struct {
	endpoint   string
	httpClient *http.Client
}

func (c *restClient) Name() string { return "REST" }

func (c *restClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetRESTEndpoints()
	if len(endpoints) == 0 {
		return fmt.Errorf("no REST endpoints available")
	}
	c.endpoint = endpoints[0]
	c.httpClient = &http.Client{Timeout: 30 * time.Second}

	// Verify endpoint is reachable
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.endpoint+"/api/v1/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("REST endpoint not reachable: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

func (c *restClient) Teardown(t *testing.T) error {
	c.httpClient = nil
	return nil
}

func (c *restClient) DoOperation(ctx context.Context, req *MyRequest) (*MyResponse, error) {
	url := fmt.Sprintf("%s/api/v1/myfeature/operation", c.endpoint)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		bytes.NewReader(body))
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

	var result MyResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// =============================================================================
// 3. gRPC Protocol Client
// =============================================================================

type grpcClient struct {
	conn   *grpc.ClientConn
	client proto.QuicBaaSClient
}

func (c *grpcClient) Name() string { return "gRPC" }

func (c *grpcClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetClusterEndpoints()
	if len(endpoints) == 0 {
		return fmt.Errorf("no gRPC endpoints available")
	}

	conn, err := helpers.SetupGRPCConnectionToEndpoint(t, ctx, endpoints[0])
	if err != nil {
		return err
	}
	c.conn = conn
	c.client = proto.NewQuicBaaSClient(conn)
	return nil
}

func (c *grpcClient) Teardown(t *testing.T) error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *grpcClient) DoOperation(ctx context.Context, req *MyRequest) (*MyResponse, error) {
	// protoReq := &proto.MyOperationRequest{...}
	// resp, err := c.client.MyOperation(ctx, protoReq)
	return nil, nil
}

// =============================================================================
// 4. QUIC Protocol Client
// =============================================================================

type quicClient struct {
	// QUIC connection fields
}

func (c *quicClient) Name() string { return "QUIC" }

func (c *quicClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetQUICEndpoints()
	if len(endpoints) == 0 {
		return fmt.Errorf("no QUIC endpoints available")
	}
	// Setup QUIC connection
	return nil
}

func (c *quicClient) Teardown(t *testing.T) error {
	return nil
}

func (c *quicClient) DoOperation(ctx context.Context, req *MyRequest) (*MyResponse, error) {
	// Use QUIC binary protocol
	return nil, nil
}

// =============================================================================
// 5. Unix Socket Protocol Client
// =============================================================================

type unixSocketClient struct {
	conn   *grpc.ClientConn
	client proto.QuicBaaSClient
}

func (c *unixSocketClient) Name() string { return "UnixSocket" }

func (c *unixSocketClient) Setup(t *testing.T, ctx context.Context) error {
	socketPath := os.Getenv("QUICBAAS_NODE1_UNIX")
	if socketPath == "" {
		socketPath = "/var/run/quicbaas/node1.sock"
	}

	// Check socket exists
	if _, err := net.Dial("unix", socketPath); err != nil {
		return fmt.Errorf("unix socket not available: %w", err)
	}

	conn, err := grpc.DialContext(ctx,
		"unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return err
	}
	c.conn = conn
	c.client = proto.NewQuicBaaSClient(conn)
	return nil
}

func (c *unixSocketClient) Teardown(t *testing.T) error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *unixSocketClient) DoOperation(ctx context.Context, req *MyRequest) (*MyResponse, error) {
	// Same as gRPC but over Unix socket
	return nil, nil
}

// =============================================================================
// 6. MCP Protocol Client
// =============================================================================

type mcpClient struct {
	endpoint   string
	httpClient *http.Client
	nextID     int
}

func (c *mcpClient) Name() string { return "MCP" }

func (c *mcpClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetMCPEndpoints()
	if len(endpoints) == 0 {
		return fmt.Errorf("no MCP endpoints available")
	}
	c.endpoint = endpoints[0]
	c.httpClient = &http.Client{Timeout: 30 * time.Second}
	c.nextID = 1

	// Initialize MCP session
	initReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "integration-test",
				"version": "1.0.0",
			},
		},
		"id": c.nextID,
	}
	c.nextID++

	body, _ := json.Marshal(initReq)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("MCP endpoint not reachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("MCP initialize failed: status %d", resp.StatusCode)
	}
	return nil
}

func (c *mcpClient) Teardown(t *testing.T) error {
	c.httpClient = nil
	return nil
}

func (c *mcpClient) callTool(ctx context.Context, toolName string, args map[string]interface{}) (json.RawMessage, error) {
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

	body, _ := json.Marshal(mcpReq)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var mcpResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &mcpResp); err != nil {
		return nil, err
	}

	if mcpResp.Error != nil {
		return nil, fmt.Errorf("MCP error: %s", mcpResp.Error.Message)
	}

	if len(mcpResp.Result.Content) > 0 {
		return json.RawMessage(mcpResp.Result.Content[0].Text), nil
	}
	return nil, nil
}

func (c *mcpClient) DoOperation(ctx context.Context, req *MyRequest) (*MyResponse, error) {
	args := map[string]interface{}{
		// Convert request to MCP tool arguments
	}
	result, err := c.callTool(ctx, "quicbaas_myfeature_operation", args)
	if err != nil {
		return nil, err
	}

	var resp MyResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
```

## Testing Patterns

### Table-Driven Tests

```go
func testBasicOperation(t *testing.T, ctx context.Context, protocol myFeatureProtocol) {
	tests := []struct {
		name    string
		req     *MyRequest
		wantErr bool
	}{
		{
			name:    "valid request",
			req:     &MyRequest{Field: "value"},
			wantErr: false,
		},
		{
			name:    "empty request",
			req:     &MyRequest{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := protocol.DoOperation(ctx, tt.req)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
		})
	}
}
```

### Concurrent Operations

```go
func testConcurrentOperations(t *testing.T, ctx context.Context, protocol myFeatureProtocol) {
	const concurrency = 10
	var wg sync.WaitGroup
	errors := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, err := protocol.DoOperation(ctx, &MyRequest{ID: id})
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent operation failed: %v", err)
	}
}
```

## Best Practices

1. **Always include all 6 protocols** - Even if some skip, include them for completeness
2. **Use typed errors** - Never use inline `fmt.Errorf` for error returns
3. **Graceful skipping** - Setup should return errors that cause `t.Skip()`
4. **Protocol-agnostic assertions** - Test logic should be identical across protocols
5. **Cleanup in Teardown** - Always close connections/clients properly
6. **Use helpers** - Leverage `test/integration/helpers` for endpoint discovery
7. **Context timeouts** - Always use context with timeouts (5 minutes typical)

## Embedded Protocol Notes

The embedded protocol is special:

- **Requires in-process service**: Needs `service.QuicBaaS` instance
- **Always skips in integration tests**: Integration tests run against remote servers
- **Used for unit tests**: Enables testing without network overhead
- **SDK usage**: Create client with `quicbaas.WithEmbedded(svc)`

```go
// For unit tests with embedded protocol:
svc := myTestService() // implements service.QuicBaaS
client, err := quicbaas.New(quicbaas.WithEmbedded(svc))
```

## See Also

- test/integration/CLAUDE.md - Integration test conventions
- test/integration/README.md - Integration test overview
- docs/ROADMAP-cleanup.md - Protocol compliance status
