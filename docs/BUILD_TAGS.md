# Build Tags Reference

QuicBaaS uses Go build tags for modular compilation. This allows you to customize the binary size, attack surface, and feature set for your deployment.

## Quick Reference

| Tag | Default | Description |
|-----|---------|-------------|
| `raft` | ON | Distributed consensus and clustering |
| `timeseries` | ON | Time series metrics storage (paired with qrdb's `qrdb_ts` engine tag) |
| `vector` | ON | Vector database (HNSW indexing; paired with qrdb's `qrdb_vector` engine tag) |
| `rag` | ON | Retrieval Augmented Generation (requires vector; paired with `qrdb_vector`) |
| `rest` | ON | REST/HTTP API server |
| `quic` | ON | QUIC protocol support |
| `mcp` | ON | Model Context Protocol (AI agent tools) |
| `unix` | OFF | Unix socket server |
| `otel` | OFF | OpenTelemetry tracing |
| `l2_redis` | OFF | Redis L2 cache backend |
| `fips` | OFF | FIPS 140-3 compliant cryptography |

### KMS Backend Tags

| Tag | Default | Description |
|-----|---------|-------------|
| `awskms` | OFF | AWS KMS integration |
| `gcpkms` | OFF | GCP Cloud KMS integration |
| `azurekv` | OFF | Azure Key Vault integration |
| `vault` | OFF | HashiCorp Vault integration |
| `pkcs11` | OFF | PKCS#11 HSM support (requires CGO) |
| `quantum` | OFF | Quantum-safe cryptography (ML-DSA, ML-KEM) |

### Codec Tags

| Tag | Default | Description |
|-----|---------|-------------|
| `codec_json` | ON | JSON codec support |
| `codec_yaml` | ON | YAML codec support |
| `codec_toml` | ON | TOML codec support |
| `codec_msgpack` | ON | MessagePack codec support |
| `codec_bson` | ON | BSON codec support |
| `codec_cbor` | ON | CBOR codec support |
| `codec_protobuf` | ON | Protobuf codec support |

### Object Storage Tags

| Tag | Default | Description |
|-----|---------|-------------|
| `awss3` | OFF | AWS S3 object storage |
| `gcpstorage` | OFF | GCP Cloud Storage |
| `azureblob` | OFF | Azure Blob Storage |
| `minio` | OFF | MinIO object storage |

### Test Tags

| Tag | Description |
|-----|-------------|
| `integration` | Integration tests (run in Docker) |
| `benchmark` | Performance benchmark tests |

## IAM Is Always Compiled

QuicBaaS is the central identity broker for the stack. The IAM subsystem
(ABAC, multi-tenant identity, OIDC/OAuth2, federation, GDPR, audit) is always
compiled into every binary; there is no `iam` build tag and a no-IAM artifact
is not supported. The IAM CLI surface (`quicbaas iam tenant|user|group|role|
app|policy|mfa|session|pep|audit|admin|gdpr|provider|identity`) is always
available.

## Excluded Commands by Tag

### Without `timeseries` tag (~7 commands excluded)

- `quicbaas timeseries write` - Write single data point
- `quicbaas timeseries write-batch` - Write batch data points
- `quicbaas timeseries query` - Query time series data
- `quicbaas timeseries delete` - Delete time series data
- `quicbaas timeseries get-retention` - Get retention policy
- `quicbaas timeseries set-retention` - Set retention policy
- `quicbaas timeseries list-metrics` - List available metrics

## Build Examples

### Using Makefile (Recommended)

```bash
# Full-featured build (default)
make build

# Build without specific features
make build WITH_TIMESERIES=0       # Exclude time series
make build WITH_VECTOR=0 WITH_RAG=0  # Exclude vector/RAG

# Pre-configured build profiles
make build-minimal   # Core + gRPC only
make build-embedded  # Single-node, gRPC only
make build-standard  # Cluster, no AI features
make build-cluster   # All features, no cloud KMS
make build-enterprise  # All features + cloud KMS
make build-full      # Everything including HSM, quantum

# FIPS 140-3 compliant build
make build-fips
```

### Using go build directly

```bash
# Full-featured build (IAM is always compiled)
go build -tags "raft timeseries vector rag rest quic mcp codec_json" ./cmd/quicbaas

# Minimal core (KV + gRPC only; IAM still compiled)
go build ./cmd/quicbaas

# Without time series
go build -tags "raft vector rag rest quic mcp codec_json" ./cmd/quicbaas

# With cloud KMS backends
go build -tags "raft vector rag rest quic mcp awskms gcpkms azurekv" ./cmd/quicbaas

# With FIPS mode
GOFIPS140=latest go build -tags "fips raft rest quic mcp codec_json" ./cmd/quicbaas
```

## Build Profiles Matrix

| Profile | raft | ts | vector | rag | rest | quic | mcp | Cloud KMS |
|---------|------|----|--------|-----|------|------|-----|-----------|
| `minimal` | - | - | - | - | - | - | - | - |
| `embedded` | - | - | - | - | - | - | - | - |
| `standard` | Y | Y | - | - | Y | Y | - | - |
| `cluster` | Y | Y | Y | Y | Y | Y | Y | - |
| `enterprise` | Y | Y | Y | Y | Y | Y | Y | Y |
| `full` | Y | Y | Y | Y | Y | Y | Y | Y + HSM |

Legend: Y = included, - = excluded, ts = timeseries. IAM is always compiled.

## Makefile Variables

Control features via environment or make arguments:

| Variable | Default | Description |
|----------|---------|-------------|
| `WITH_RAFT` | 1 | Include Raft clustering |
| `WITH_TIMESERIES` | 1 | Include time series |
| `WITH_VECTOR` | 1 | Include vector database |
| `WITH_RAG` | 1 | Include RAG (requires VECTOR) |
| `WITH_REST` | 1 | Include REST API |
| `WITH_QUIC` | 1 | Include QUIC protocol |
| `WITH_MCP` | 1 | Include MCP protocol |
| `WITH_L2_REDIS` | 0 | Include Redis L2 cache |
| `WITH_AWS` | 0 | Include AWS KMS/S3 |
| `WITH_GCP` | 0 | Include GCP KMS/GCS |
| `WITH_AZURE` | 0 | Include Azure KV/Blob |
| `WITH_VAULT` | 0 | Include HashiCorp Vault |
| `WITH_PKCS11` | 0 | Include PKCS#11 (CGO) |
| `WITH_QUANTUM` | 0 | Include quantum-safe crypto |

### Codec Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `WITH_CODEC_JSON` | 1 | JSON codec |
| `WITH_CODEC_YAML` | 1 | YAML codec |
| `WITH_CODEC_TOML` | 1 | TOML codec |
| `WITH_CODEC_MSGPACK` | 1 | MessagePack codec |
| `WITH_CODEC_BSON` | 1 | BSON codec |
| `WITH_CODEC_CBOR` | 1 | CBOR codec |
| `WITH_CODEC_PROTOBUF` | 1 | Protobuf codec |

## File Organization

Build tags use conditional compilation with stub files:

```
cmd/quicbaas/
  timeseries.go           # //go:build timeseries
  timeseries_stub.go      # //go:build !timeseries
  tracing.go              # //go:build otel
  tracing_no_otel.go      # //go:build !otel
```

Stub files provide placeholder commands that inform users an optional feature
is disabled. IAM has no stub: it is always compiled, so `quicbaas iam ...` is
always present.

## Dependencies

Some build tags have dependencies:

| Tag | Requires |
|-----|----------|
| `rag` | `vector` |
| `pkcs11` | CGO_ENABLED=1 |
| `awskms` | AWS credentials |
| `gcpkms` | GCP credentials |
| `azurekv` | Azure credentials |

The Makefile validates dependencies at build time:

```makefile
ifeq ($(WITH_RAG),1)
  ifneq ($(WITH_VECTOR),1)
    $(error WITH_RAG=1 requires WITH_VECTOR=1)
  endif
endif
```

## Binary Size Impact

Approximate binary size by profile (Linux amd64):

| Profile | Size | Reduction |
|---------|------|-----------|
| full | ~95 MB | baseline |
| enterprise | ~85 MB | ~10% |
| cluster | ~75 MB | ~20% |
| standard | ~55 MB | ~40% |
| embedded | ~45 MB | ~50% |
| minimal | ~35 MB | ~60% |

Use `-ldflags="-s -w"` for additional ~20% reduction by stripping debug info:

```bash
go build -ldflags="-s -w" -tags "raft rest" ./cmd/quicbaas
```

## Testing with Build Tags

Run tests for specific features:

```bash
# Run IAM tests only (no special tag needed; IAM is always compiled)
go test ./pkg/iam/... ./cmd/quicbaas/... -run IAM

# Run timeseries tests
go test -tags=timeseries ./cmd/quicbaas/... -run TimeSeries

# Run integration tests (requires Docker)
make integration-test

# Run benchmarks
make bench-cmd
```
