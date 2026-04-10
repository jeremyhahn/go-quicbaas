# Integration Tests

End-to-end integration tests for QuicBaaS running against a real 3-node cluster in Docker.

## Test Categories

| Category | Directory | Description |
|----------|-----------|-------------|
| api | `api/` | API layer tests |
| auth | `auth/` | Authentication/authorization |
| backup | `backup/` | Backup/restore operations |
| batch | `batch/` | Batch processing |
| cache | `cache/` | Caching layer (L1/L2) |
| catalog | `catalog/` | Catalog service |
| cli | `cli/` | CLI binary E2E tests |
| cluster | `cluster/` | Cluster operations |
| dao | `dao/` | Generic DAO layer |
| dek | `dek/` | Data encryption keys |
| discovery | `discovery/` | Service discovery |
| embedding | `embedding/` | Vector embeddings |
| iam | `iam/` | Identity & access management |
| kms | `kms/` | Key management service |
| kvstore | `kvstore/` | Key-value store |
| mcp | `mcp/` | Model Context Protocol |
| objectstore | `objectstore/` | Object storage |
| observability | `observability/` | Metrics/tracing |
| parameter_store | `parameter_store/` | Parameter store |
| raft | `raft/` | Raft consensus |
| rag | `rag/` | RAG (retrieval-augmented generation) |
| routing | `routing/` | Request routing |
| scheduler | `scheduler/` | Job scheduler |
| seal | `seal/` | Seal/unseal operations |
| secrets | `secrets/` | Secrets management |
| secrets-resolver | `secrets-resolver/` | Secret resolution |
| security | `security/` | Security features |
| server | `server/` | Server lifecycle |
| service | `service/` | Service layer |
| storage | `storage/` | Storage backends |
| timeseries | `timeseries/` | Time series data |
| transactions | `transactions/` | Transaction support |
| vector | `vector/` | Vector search |

## Running Tests

### Run All Integration Tests

```bash
make integration-test
```

### Run Specific Package Tests

```bash
# Pattern: make integration-test-<package>
make integration-test-kms
make integration-test-secrets
make integration-test-scheduler
make integration-test-cache
make integration-test-vector
make integration-test-rag
make integration-test-timeseries
make integration-test-backup
make integration-test-dao
```

### Run with go test (inside Docker)

```bash
# All tests
go test -v -tags integration ./test/integration/...

# Specific package
go test -v -tags integration ./test/integration/kms/...

# Specific test
go test -v -tags integration -run TestKMSKeyGeneration ./test/integration/kms/...

# Specific protocol
go test -v -tags integration -run TestSecretsMultiProtocol/REST ./test/integration/secrets/
go test -v -tags integration -run TestSecretsMultiProtocol/gRPC ./test/integration/secrets/
```

### SDK Integration Tests

```bash
# Go SDK
make integration-test-sdk-go
make integration-test-sdk-go-scheduler

# JavaScript SDK
make integration-test-sdk-js
make integration-test-sdk-js-scheduler
```

## Docker Setup

Tests run in Docker containers using `docker-compose.integration.yml`.

### Start Cluster Manually

```bash
cd test/integration
docker compose -f docker-compose.integration.yml up -d test-node1 test-node2 test-node3
```

### View Logs

```bash
docker compose -f docker-compose.integration.yml logs -f test-node1
```

### Cleanup

```bash
docker compose -f docker-compose.integration.yml down -v
```

### Cluster Architecture

- **test-node1**: Primary node (REST :18080, gRPC :15051, QUIC :14433, MCP :18888)
- **test-node2**: Secondary node (REST :18081, gRPC :15052, QUIC :14434, MCP :18889)
- **test-node3**: Secondary node (REST :18082, gRPC :15053, QUIC :14435, MCP :18890)
- **ollama**: Embedding service for vector/RAG tests

## Environment Variables

Set by `docker-compose.integration.yml`:

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

# MCP endpoints
QUICBAAS_NODE1_MCP=http://test-node1:8888/mcp
QUICBAAS_MCP_ENDPOINT=http://test-node1:8888/mcp

# Unix socket endpoints
QUICBAAS_NODE1_UNIX=/var/run/quicbaas/node1.sock

# Test configuration
QUICBAAS_TEST_MODE=integration
QUICBAAS_HEALTH_TIMEOUT=180
QUICBAAS_TEST_TIMEOUT=30m

# Embedding service
OLLAMA_URL=http://ollama:11434
```

## Multi-Protocol Test Pattern

Tests validate functionality across all 6 supported protocols:
- **Embedded** - Direct in-process SDK access (skipped in integration tests)
- **REST** - HTTP/JSON API
- **gRPC** - Protocol Buffers over HTTP/2
- **QUIC** - Binary protocol over QUIC transport
- **Unix Socket** - gRPC over Unix domain sockets
- **MCP** - Model Context Protocol (JSON-RPC 2.0 tool interface)

### Protocol Compliance Status

Directories with CLI support must implement multi-protocol tests. Current status:

| Directory | Embedded | REST | gRPC | QUIC | Unix | MCP | Status |
|-----------|----------|------|------|------|------|-----|--------|
| auth | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | COMPLIANT |
| backup | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | COMPLIANT |
| batch | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | COMPLIANT |
| cache | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | COMPLIANT |
| cluster | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | COMPLIANT |
| dek | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | COMPLIANT |
| iam | ✅ | ✅ | ✅ | ⏳ | ✅ | ⏳ | PARTIAL |
| kms | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | COMPLIANT |
| parameter_store | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | COMPLIANT |
| scheduler | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | COMPLIANT |
| seal | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | COMPLIANT |
| secrets | ⏳ | ✅ | ✅ | ✅ | ✅ | ✅ | PARTIAL |
| secrets-resolver | ⏳ | ✅ | ✅ | ✅ | ✅ | ✅ | PARTIAL |
| timeseries | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | COMPLIANT |
| transactions | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | COMPLIANT |
| vector | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | COMPLIANT |

Legend: ✅ Implemented | ⏳ Placeholder/Pending | ❌ Missing

**Note:** Embedded protocol adapters skip gracefully in integration tests since they require an in-process `service.QuicBaaS` instance. Integration tests run against containerized servers via network protocols.

### Protocol Interface Pattern

Each multiprotocol test defines an interface abstracting protocol differences:

```go
type featureProtocol interface {
    Setup(t *testing.T, ctx context.Context) error
    Teardown(t *testing.T) error
    // Feature-specific methods
    DoOperation(ctx context.Context, req *Request) (*Response, error)
    Name() string
}
```

### Test Execution Pattern

```go
func TestFeatureMultiProtocol(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()

    protocols := getAllProtocols() // Returns Embedded, REST, gRPC, QUIC, Unix, MCP

    for _, protocol := range protocols {
        t.Run(protocol.Name(), func(t *testing.T) {
            err := protocol.Setup(t, ctx)
            if err != nil {
                t.Skipf("Skipping %s: setup failed: %v", protocol.Name(), err)
                return
            }
            defer protocol.Teardown(t)

            // Run tests against this protocol
            t.Run("Operation", func(t *testing.T) {
                result, err := protocol.DoOperation(ctx, req)
                require.NoError(t, err)
                // assertions...
            })
        })
    }
}
```

## Adding New Tests

### 1. Create Test Directory

```bash
mkdir test/integration/myfeature
```

### 2. Create main_test.go

```go
//go:build integration

package myfeature

import (
    "os"
    "testing"
)

func TestMain(m *testing.M) {
    // Setup (if needed)
    code := m.Run()
    // Teardown (if needed)
    os.Exit(code)
}
```

### 3. Create Feature Tests

```go
//go:build integration

package myfeature

import (
    "context"
    "testing"
    "time"

    "github.com/stretchr/testify/require"
    "github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

func TestMyFeature(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    endpoints := helpers.GetClusterEndpoints()
    // Test implementation...
}
```

### 4. Create Multi-Protocol Tests (if applicable)

```go
//go:build integration && quic

package myfeature

import (
    "context"
    "fmt"
    "testing"

    quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// Define protocol interface
type myProtocol interface {
    Setup(t *testing.T, ctx context.Context) error
    Teardown(t *testing.T) error
    MyOperation(ctx context.Context) error
    Name() string
}

// Implement for each protocol (all 6 required)
type embeddedProtocol struct { client quicbaas.Client }
type restProtocol struct { /* ... */ }
type grpcProtocol struct { /* ... */ }
type quicProtocol struct { /* ... */ }
type unixSocketProtocol struct { /* ... */ }
type mcpProtocol struct { /* ... */ }

func getAllProtocols() []myProtocol {
    return []myProtocol{
        &embeddedProtocol{},   // Skips in integration tests
        &restProtocol{},
        &grpcProtocol{},
        &quicProtocol{},
        &unixSocketProtocol{},
        &mcpProtocol{},
    }
}

// Embedded protocol Setup returns error to skip in integration context
func (p *embeddedProtocol) Setup(t *testing.T, ctx context.Context) error {
    return fmt.Errorf("embedded protocol not available in integration test context")
}
```

### 5. Add Makefile Target (automatic)

The Makefile automatically discovers packages via pattern matching:

```makefile
# Pattern: make integration-test-<package>
# Automatically runs: ./test/integration/<package>/...
```

## Test Conventions

Per `CLAUDE.md`:

1. **CLI packages**: Test E2E using the CLI binary across all protocols
2. **Non-CLI packages**: Test service layer API with positive, negative, and edge cases
3. **All tests**: Must include success and error handling tests
4. **Coverage**: Target 90%+ code coverage
5. **No mocks**: Use real services in Docker containers

## Helper Utilities

Located in `test/integration/helpers/`:

| File | Purpose |
|------|---------|
| `endpoints.go` | Environment variable helpers for endpoints |
| `cluster_builder.go` | Cluster setup utilities |
| `data_generator.go` | Test data generation |
| `docker.go` | Docker container management |
| `verification.go` | Response verification helpers |
| `test_helpers.go` | Common test utilities |

### Endpoint Helpers

```go
import "github.com/jeremyhahn/go-quicbaas/test/integration/helpers"

// Get endpoints
grpcEndpoints := helpers.GetClusterEndpoints()   // gRPC
restEndpoints := helpers.GetRESTEndpoints()      // REST
quicEndpoints := helpers.GetQUICEndpoints()      // QUIC
mcpEndpoints := helpers.GetMCPEndpoints()        // MCP
unixEndpoints := helpers.GetUnixSocketEndpoints() // Unix sockets
```

## Troubleshooting

### Connection Refused

1. Verify containers are running: `docker compose ps`
2. Check container logs: `docker compose logs test-node1`
3. Verify health checks passed before running tests

### Tests Skipped

- **Embedded**: Always skipped in integration tests (requires in-process service)
- **Unix Socket**: Socket may not be exposed in Docker
- **QUIC**: Some QUIC clients pending implementation
- **MCP**: Requires MCP tools to be implemented for the feature

### Timeouts

1. Increase context timeout in test
2. Check cluster health: `curl http://localhost:18080/api/v1/health/ready`
3. Verify no resource exhaustion

### Cluster Not Ready

Wait for health checks:
```bash
./test/integration/health_check.sh
```
