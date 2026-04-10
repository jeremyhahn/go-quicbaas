# KMS Integration Tests

Comprehensive integration tests for QuicBaaS Key Management Service (KMS) across all API protocols.

## Overview

This test suite validates KMS functionality through four different API protocols:
- **gRPC**: Native high-performance RPC protocol
- **REST**: HTTP/JSON API
- **QUIC**: Low-latency UDP-based protocol (tests prepared, implementation pending)
- **MCP**: Model Context Protocol for AI/LLM integration (tests prepared, implementation pending)

## Test Files

### Core Files

- **`main_test.go`**: Shared setup, utilities, and cross-protocol lifecycle tests
- **`helpers.go`**: Common helper functions, endpoint configuration, and client wrappers

### Protocol-Specific Tests

- **`grpc_test.go`**: gRPC API tests (fully implemented)
- **`rest_test.go`**: REST API tests (fully implemented)
- **`quic_test.go`**: QUIC protocol tests (placeholder, awaiting QUIC client)
- **`mcp_test.go`**: MCP protocol tests (placeholder, awaiting MCP KMS tools)

## Test Coverage

Each protocol tests the following KMS operations:

### Key Management Operations

1. **Key Generation** (`kms key generate`)
   - AES-256-GCM symmetric keys
   - ChaCha20-Poly1305 symmetric keys
   - RSA-4096 asymmetric keys
   - ECDSA-P256 asymmetric keys
   - Key with custom metadata and tags

2. **Key Retrieval** (`kms key get`)
   - Get key by ID
   - Get specific key version
   - Get non-existent key (error case)

3. **Key Listing** (`kms key list`)
   - List all keys with pagination
   - List with prefix filter
   - List with recursive traversal
   - List with limit

4. **Key Rotation** (`kms key rotate`)
   - Rotate key with version retention
   - List key versions after rotation
   - Rotate non-existent key (error case)

5. **Key Deletion** (`kms key delete`)
   - Schedule deletion with pending window
   - Force delete immediately
   - Delete with purge flag
   - Delete non-existent key (error case)

6. **Key Status Updates** (`kms key enable/disable`)
   - Enable key
   - Disable key
   - Verify status after update

7. **Key Versions** (`kms key versions`)
   - List all versions of a key
   - Verify version ordering
   - Check version metadata

### Cryptographic Operations

1. **Encryption/Decryption** (`kms encrypt/decrypt`)
   - Encrypt and decrypt data
   - Encrypt/decrypt with Additional Authenticated Data (AAD)
   - Test various data sizes (1B, 100B, 1KB, 10KB)
   - Decrypt with wrong key (error case)
   - Decrypt invalid ciphertext (error case)
   - Encrypt empty plaintext (error case)

### Advanced Features

1. **Hierarchical Keys**
   - Create keys with path-like names (e.g., `tenant-123/prod/api-key`)
   - List keys with path prefix filter
   - Recursive path traversal

2. **Concurrent Operations**
   - Concurrent encryptions on same key
   - Concurrent key generations
   - Concurrent key rotations

3. **Cluster Consistency**
   - Key created on node1 visible on node2
   - Encrypt on node1, decrypt on node2
   - Cross-node key operations

## Running Tests

### Prerequisites

1. **Docker Compose Setup**:
   ```bash
   cd test/integration
   docker-compose -f docker-compose.integration.yml up -d
   ```

2. **Environment Variables** (optional, defaults provided):
   ```bash
   # gRPC endpoints
   export QUICBAAS_NODE1_GRPC=localhost:50051
   export QUICBAAS_NODE2_GRPC=localhost:50052
   export QUICBAAS_NODE3_GRPC=localhost:50053

   # REST endpoints
   export QUICBAAS_NODE1_URL=http://test-node1:8080
   export QUICBAAS_NODE2_URL=http://test-node2:8080
   export QUICBAAS_NODE3_URL=http://test-node3:8080

   # QUIC endpoints
   export QUICBAAS_NODE1_QUIC=localhost:8443
   export QUICBAAS_NODE2_QUIC=localhost:8444
   export QUICBAAS_NODE3_QUIC=localhost:8445

   # MCP endpoint
   export QUICBAAS_MCP_ENDPOINT=localhost:50051
   ```

### Run All KMS Integration Tests

```bash
# From repository root
make integration-test-kms

# Or directly with go test
go test -tags integration -v ./test/integration/kms/...
```

### Run Specific Protocol Tests

```bash
# gRPC tests only
go test -tags integration -v ./test/integration/kms -run TestGRPC

# REST tests only
go test -tags integration -v ./test/integration/kms -run TestREST

# QUIC tests only (currently skipped)
go test -tags integration -v ./test/integration/kms -run TestQUIC

# MCP tests only (currently skipped)
go test -tags integration -v ./test/integration/kms -run TestMCP
```

### Run Specific Test Cases

```bash
# Test key generation across all protocols
go test -tags integration -v ./test/integration/kms -run KeyGeneration

# Test encryption/decryption
go test -tags integration -v ./test/integration/kms -run EncryptionDecryption

# Test key lifecycle
go test -tags integration -v ./test/integration/kms -run KeyLifecycle

# Test concurrent operations
go test -tags integration -v ./test/integration/kms -run Concurrent

# Test cluster consistency
go test -tags integration -v ./test/integration/kms -run ClusterConsistency
```

## Test Structure

### Helper Functions

Located in `helpers.go`:

- **Endpoint Configuration**:
  - `getGRPCEndpoints()`: Returns gRPC endpoint addresses
  - `getRESTEndpoints()`: Returns REST API endpoint URLs
  - `getQUICEndpoints()`: Returns QUIC endpoint addresses
  - `getMCPEndpoint()`: Returns MCP endpoint address

- **Skip Helpers**:
  - `skipIfKMSUnavailable()`: Skip test if KMS service not available
  - `skipIfCryptoUnavailable()`: Skip test if Crypto service not available
  - `skipIfProtocolUnavailable()`: Skip test if protocol not available

- **Test Data Generators**:
  - `generateUniqueKeyName(prefix)`: Generate unique key name with timestamp
  - `generateTestPlaintext(size)`: Generate test data of specified size

- **REST Client Wrapper**:
  - `RESTClient`: HTTP client with KMS-specific methods
  - `GenerateKey()`, `GetKey()`, `ListKeys()`, etc.

- **Cleanup and Assertions**:
  - `cleanupKey()`: Delete test key
  - `assertKeyExists()`: Verify key exists
  - `assertKeyNotFound()`: Verify key doesn't exist

### Test Patterns

#### gRPC Tests (`grpc_test.go`)

```go
func TestGRPCKeyGeneration(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    endpoints := getGRPCEndpoints()
    client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithInsecure())
    require.NoError(t, err)
    defer client.Close()

    skipIfKMSUnavailable(t, client, ctx)

    // Test implementation...
}
```

#### REST Tests (`rest_test.go`)

```go
func TestRESTKeyGeneration(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    endpoints := getRESTEndpoints()
    client := NewRESTClient(endpoints[0])

    // Test implementation...
}
```

## Expected Test Output

### Successful Test Run

```
=== RUN   TestKMSAvailability
=== RUN   TestKMSAvailability/gRPC_availability
=== RUN   TestKMSAvailability/REST_availability
--- PASS: TestKMSAvailability (0.15s)
    --- PASS: TestKMSAvailability/gRPC_availability (0.08s)
    --- PASS: TestKMSAvailability/REST_availability (0.07s)

=== RUN   TestGRPCKeyGeneration
=== RUN   TestGRPCKeyGeneration/AES-256-GCM
=== RUN   TestGRPCKeyGeneration/ChaCha20-Poly1305
=== RUN   TestGRPCKeyGeneration/RSA-4096
=== RUN   TestGRPCKeyGeneration/ECDSA-P256
--- PASS: TestGRPCKeyGeneration (1.23s)
    --- PASS: TestGRPCKeyGeneration/AES-256-GCM (0.31s)
    --- PASS: TestGRPCKeyGeneration/ChaCha20-Poly1305 (0.28s)
    --- PASS: TestGRPCKeyGeneration/RSA-4096 (0.34s)
    --- PASS: TestGRPCKeyGeneration/ECDSA-P256 (0.30s)

...

PASS
ok      github.com/jeremyhahn/go-quicbaas/test/integration/kms    45.678s
```

### Skipped Tests

```
=== RUN   TestQUICKMSAvailability
--- SKIP: TestQUICKMSAvailability (0.00s)
    quic_test.go:25: QUIC KMS tests not yet implemented - QUIC client needs to be created

=== RUN   TestMCPKMSAvailability
--- SKIP: TestMCPKMSAvailability (0.00s)
    mcp_test.go:25: MCP KMS tests not yet implemented - MCP client with KMS tools needs to be created
```

## Implementation Status

### Fully Implemented ✅

- **gRPC Protocol**: All tests implemented and passing
- **REST API**: All tests implemented and passing

### Pending Implementation 🚧

- **QUIC Protocol**:
  - Test structure defined
  - Awaiting QUIC client implementation
  - See implementation notes in `quic_test.go`

- **MCP Protocol**:
  - Test structure defined
  - Awaiting MCP KMS tools implementation
  - See implementation notes in `mcp_test.go`

## Adding New Tests

### Adding a New Test Case

1. Choose the appropriate protocol test file
2. Add a new test function following the naming convention:
   ```go
   func TestGRPC<FeatureName>(t *testing.T) {
       // Test implementation
   }
   ```

3. Use the helper functions for setup and cleanup:
   ```go
   ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
   defer cancel()

   endpoints := getGRPCEndpoints()
   client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithInsecure())
   require.NoError(t, err)
   defer client.Close()

   skipIfKMSUnavailable(t, client, ctx)

   keyName := generateUniqueKeyName("test-prefix")
   // ... test logic ...
   defer cleanupKey(t, client, ctx, keyID)
   ```

### Adding QUIC Tests

When QUIC client is ready:

1. Create `QUICClient` struct in `helpers.go`
2. Implement KMS methods (GenerateKey, GetKey, etc.)
3. Remove `t.Skip()` calls from `quic_test.go`
4. Implement actual test logic

### Adding MCP Tests

When MCP KMS tools are ready:

1. Implement KMS tools in `pkg/api/mcp/tools_kms.go`
2. Register tools in `pkg/api/mcp/server.go`
3. Remove `t.Skip()` calls from `mcp_test.go`
4. Implement actual test logic using MCP client

## Troubleshooting

### Tests Skipped Due to Service Unavailable

If tests are being skipped with "KMS service is not enabled":

1. Verify KMS is enabled in server configuration
2. Check server logs for KMS initialization errors
3. Ensure required environment variables are set
4. Verify network connectivity to test endpoints

### Connection Refused Errors

1. Verify Docker containers are running:
   ```bash
   docker-compose -f test/integration/docker-compose.integration.yml ps
   ```

2. Check endpoint configuration matches container ports
3. Verify firewall rules allow connections

### Test Timeouts

1. Increase timeout in test context:
   ```go
   ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
   ```

2. Check cluster replication lag (may need longer waits)
3. Verify no resource exhaustion on test nodes

### Cluster Consistency Test Failures

1. Ensure cluster has at least 2 nodes running
2. Increase replication wait time:
   ```go
   time.Sleep(5 * time.Second) // Increase from 2s
   ```

3. Check Raft logs for replication issues

## CI/CD Integration

### Makefile Targets

Add to `Makefile`:

```makefile
.PHONY: integration-test-kms
integration-test-kms:
	@echo "Running KMS integration tests..."
	go test -tags integration -v ./test/integration/kms/...

.PHONY: integration-test-kms-grpc
integration-test-kms-grpc:
	@echo "Running KMS gRPC integration tests..."
	go test -tags integration -v ./test/integration/kms -run TestGRPC

.PHONY: integration-test-kms-rest
integration-test-kms-rest:
	@echo "Running KMS REST integration tests..."
	go test -tags integration -v ./test/integration/kms -run TestREST

.PHONY: coverage-kms-integration
coverage-kms-integration:
	@echo "Running KMS integration tests with coverage..."
	go test -tags integration -coverprofile=coverage-kms-integration.out ./test/integration/kms/...
	go tool cover -html=coverage-kms-integration.out -o coverage-kms-integration.html
```

### GitHub Actions Workflow

```yaml
name: KMS Integration Tests

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main, develop]

jobs:
  kms-integration:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Start test infrastructure
        run: |
          cd test/integration
          docker-compose -f docker-compose.integration.yml up -d
          sleep 10

      - name: Run KMS integration tests
        run: make integration-test-kms

      - name: Cleanup
        if: always()
        run: |
          cd test/integration
          docker-compose -f docker-compose.integration.yml down -v
```

## Contributing

When contributing new KMS tests:

1. Follow existing test patterns
2. Use meaningful test names
3. Add proper cleanup (defer statements)
4. Include both success and error cases
5. Document any special setup requirements
6. Ensure tests are idempotent
7. Use unique key names to avoid conflicts
8. Add appropriate timeouts
9. Skip gracefully when services unavailable
10. Update this README with new test coverage

## References

- [KMS API Documentation](../../docs/kms/README.md)
- [KMS Implementation](../../pkg/kms/)
- [gRPC Client](../../pkg/api/grpc/)
- [REST Handlers](../../pkg/api/rest/)
- [Integration Test Guide](../README.md)
