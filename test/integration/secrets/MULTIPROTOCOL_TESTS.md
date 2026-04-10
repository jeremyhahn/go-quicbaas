# Secrets Multi-Protocol Integration Tests

This document describes the multi-protocol integration tests for QuicBaaS's secrets management functionality.

## Overview

The multi-protocol tests ensure that all secrets operations work identically across QuicBaaS's supported protocols:

1. **REST** - HTTP/JSON API
2. **gRPC** - Protocol Buffers over HTTP/2
3. **QUIC** - Binary protocol over QUIC transport
4. **Unix Socket** - gRPC over Unix domain sockets
5. **MCP** - Model Context Protocol (tool-based interface)

## Test Files

### `multiprotocol_test.go`

Main test file that implements a protocol abstraction layer and tests all core operations across protocols.

#### Tested Operations

Each protocol is tested against the following operations:

- **Store Secret** - Create new secret with data and metadata
- **Get Secret** - Retrieve secret by ID
- **List Secrets** - List all accessible secrets with filtering
- **Delete Secret** - Soft delete (mark as deleted)
- **Destroy Secret** - Permanent deletion
- **List Versions** - Get all versions of a secret
- **Rollback** - Rollback to a previous version
- **Rotate** - Rotate secret to new value

#### Protocol Implementations

**REST Protocol** (`restProtocolClient`)
- Uses HTTP/JSON API via existing REST helpers
- Endpoints: `/api/v1/secrets/*`
- Status: **Fully Implemented**

**gRPC Protocol** (`grpcProtocolClient`)
- Uses Protocol Buffers over HTTP/2
- Client: `pkg/api/grpc/client_secrets.go`
- Status: **Fully Implemented**

**QUIC Protocol** (`quicProtocolClient`)
- Uses binary protocol over QUIC transport
- Operations: Defined in `pkg/api/quic/protocol.go`
- Status: **Skipped** (no client wrapper available yet)
- Future work: Implement QUIC client wrapper

**Unix Socket Protocol** (`unixSocketProtocolClient`)
- Uses gRPC over Unix domain sockets
- Socket path: `/tmp/quicbaas.sock`
- Protocol: gRPC (same as TCP, different transport)
- Authentication: Peer credentials via SO_PEERCRED (kernel-verified UID/GID/PID)
- Status: **Implemented** (skipped if socket not available)

### `mcp_test.go`

Separate test file for MCP protocol due to its unique tool-based interface.

#### MCP Test Coverage

- **Tool Registration** - Verify all 7 secrets tools are registered
- **Store/Get/List/Delete** - Basic CRUD operations via MCP tools
- **Versions/Rollback/Rotate** - Advanced operations via MCP tools

#### MCP Tool Names

```
quicbaas_secrets_store
quicbaas_secrets_get
quicbaas_secrets_list
quicbaas_secrets_delete
quicbaas_secrets_versions
quicbaas_secrets_rollback
quicbaas_secrets_rotate
```

**Status**: Tests are documented but currently skipped (require running QuicBaaS service instance)

## Running the Tests

### Prerequisites

1. QuicBaaS cluster must be running (typically via docker-compose)
2. KMS encryption key must be created (done automatically in `main_test.go`)
3. All protocols must be enabled and listening

### Run All Secrets Tests

```bash
# Run all secrets integration tests (including multi-protocol)
make integration-test-secrets

# Or directly with go test
go test -v -tags integration ./test/integration/secrets/
```

### Run Specific Protocol Tests

```bash
# Run only multi-protocol tests
go test -v -tags integration -run TestSecretsMultiProtocol ./test/integration/secrets/

# Run specific protocol
go test -v -tags integration -run TestSecretsMultiProtocol/REST ./test/integration/secrets/
go test -v -tags integration -run TestSecretsMultiProtocol/gRPC ./test/integration/secrets/
```

### Run MCP Tests

```bash
# Run MCP tool registry test
go test -v -tags integration -run TestMCPToolRegistry ./test/integration/secrets/

# Run full MCP tests (currently skipped)
go test -v -tags integration -run TestSecretsMCP ./test/integration/secrets/
```

## Test Architecture

### Protocol Abstraction

The `secretsProtocol` interface provides a unified API for testing all protocols:

```go
type secretsProtocol interface {
    Setup(t *testing.T, ctx context.Context) error
    Teardown(t *testing.T) error
    StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error)
    GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error)
    ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error)
    DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error)
    ListVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error)
    Rollback(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error)
    Rotate(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error)
    Name() string
}
```

### Test Flow

1. **Setup** - Initialize protocol client (connections, credentials, etc.)
2. **Execute Tests** - Run all operation tests via protocol interface
3. **Assertions** - Verify responses match expected structure and data
4. **Teardown** - Clean up connections and resources

### Error Handling

- All protocol implementations handle errors consistently
- Soft deletes return 404 on subsequent reads
- Permanent deletes (destroy) are irreversible
- Version operations maintain referential integrity

## Environment Variables

The tests use the following environment variables (set by docker-compose):

```bash
# REST endpoints
QUICBAAS_NODE1_URL=http://test-node1:8080
QUICBAAS_NODE2_URL=http://test-node2:8080
QUICBAAS_NODE3_URL=http://test-node3:8080

# gRPC endpoints
QUICBAAS_NODE1_GRPC=test-node1:50051
QUICBAAS_NODE2_GRPC=test-node2:50051
QUICBAAS_NODE3_GRPC=test-node3:50051

# QUIC endpoints
QUICBAAS_NODE1_QUIC=test-node1:4433
QUICBAAS_NODE2_QUIC=test-node2:4433
QUICBAAS_NODE3_QUIC=test-node3:4433
```

## Future Enhancements

### QUIC Client

Implement a QUIC client wrapper to enable QUIC protocol tests:

```go
type QUICClient struct {
    conn quic.Connection
    // ...
}

func (c *QUICClient) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
    // 1. Encode request using MessagePack
    // 2. Send via QUIC stream
    // 3. Receive and decode response
    // 4. Return typed response
}
```

### MCP Integration Tests

Enable full MCP integration tests by:

1. Starting QuicBaaS service in test environment
2. Implementing MCP client or using stdio transport
3. Executing actual MCP tools and verifying results

### Performance Benchmarks

Add protocol-specific performance benchmarks:

```go
func BenchmarkSecretsStoreREST(b *testing.B) {
    // Benchmark REST store operations
}

func BenchmarkSecretsStoregRPC(b *testing.B) {
    // Benchmark gRPC store operations
}
```

### Protocol Parity Testing

Add tests to verify exact parity between protocols:

```go
func TestProtocolParity(t *testing.T) {
    // Store via REST, retrieve via gRPC
    // Store via gRPC, retrieve via REST
    // Verify identical results
}
```

## Troubleshooting

### Connection Failures

If tests fail with connection errors:

1. Verify docker-compose is running: `docker-compose ps`
2. Check container logs: `docker-compose logs test-node1`
3. Verify ports are exposed: `docker-compose port test-node1 8080`

### Protocol Not Available

If a protocol test is skipped:

- **Unix Socket**: Socket may not be exposed in Docker environment
- **QUIC**: Client wrapper not yet implemented
- **MCP**: Requires running service instance

### Assertion Failures

If data assertions fail:

1. Check encryption key is available
2. Verify service logs for errors
3. Ensure no conflicting tests are running
4. Check for timing issues (increase timeouts)

## Related Documentation

- [Secrets Integration Tests](./secrets_integration_test.go) - Existing REST-only tests
- [API Documentation](../../../docs/api/) - Protocol specifications
- [gRPC Client](../../../pkg/api/grpc/client_secrets.go) - gRPC implementation
- [QUIC Protocol](../../../pkg/api/quic/protocol.go) - QUIC operation codes
- [MCP Tools](../../../pkg/api/mcp/tools_secrets.go) - MCP tool definitions

## Contributing

When adding new secrets operations:

1. Add operation to `secretsProtocol` interface
2. Implement in all protocol clients
3. Add test function (e.g., `testNewOperation`)
4. Call from `testSecretsProtocol`
5. Update this documentation

When adding new protocols:

1. Implement `secretsProtocol` interface
2. Add to `TestSecretsMultiProtocol` test
3. Add environment variables if needed
4. Document in this file
