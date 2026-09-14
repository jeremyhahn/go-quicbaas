# Multiprotocol API — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag, with runnable examples. Feature folder: `docs/multiprotocol-api/`.

Command-line interface for QuicBaaS administration and data operations.

## Installation

Install from source:
```bash
git clone https://github.com/jeremyhahn/go-quicbaas.git
cd go-quicbaas
make build
sudo cp ./bin/quicbaas /usr/local/bin/
```

Or using Go:
```bash
go install github.com/jeremyhahn/go-quicbaas/cmd/quicbaas@latest
```

## Quick Start

```bash
# Start server
quicbaas server start

# Basic operations
quicbaas data put user john '{"name":"John Doe","email":"john@example.com"}'
quicbaas data get user john
quicbaas data delete user john --yes
```

## Command Reference

### Core Operations

| Command | Description | Documentation |
|---------|-------------|---------------|
| server | Start and manage QuicBaaS server | Server startup, configuration, lifecycle |
| data | Data operations (put, get, delete, list, query) | CRUD operations with consistency levels |
| cluster | Cluster management and monitoring | Status, health, node information |
| version | Display version information | Version and build details |

### Security & Access Control

| Command | Description | Documentation |
|---------|-------------|---------------|
| auth | Authentication + N-factor management | Passwordless login, factor list/add/remove/rotate/relabel |
| barrier | Cryptographic barrier management (canonical) | status / init / init-shamir / unseal / unseal-shares / seal / rekey |
| ceremony | Initialisation ceremony state machine | status / claim-cert / claim-share (begin+complete handshake) / sign-csr / enroll-external-officer |
| bootstrap | Trust-bootstrap the CA bundle | auto / dane / noise / spki / direct |
| ca | Certificate Authority operations | info / bundle / list / issue / revoke / export-bundle |
| acme | ACME (RFC 8555) broker-as-CA operations | register-challenge / issue / new-order / finalize-order / renew |
| trust | Trust artifacts | publish-dane / cross-signed-roots / auto-import-aia |
| credential | Named credential service (versioned rotation) | submit / list / rotate / retrieve / versions / delete / gc |
| init | `quicbaasd init` daemon-side ceremony | First-boot bootstrap with persisted state |
| seal | _Deprecated alias_ — forwards to `barrier` | Migration guide only |
| [user](user.md) | User management | Create, delete, list, update users |
| role | RBAC role management | Roles, permissions, assignments |

### Cryptography & Secrets

| Command | Description | Documentation |
|---------|-------------|---------------|
| kms | Key Management System | Generate, rotate, encrypt, decrypt |
| secrets | Secrets management | Versioned, encrypted secret storage |
| crypto | Encryption-as-a-service | Transit encryption operations |
| dek | Data Encryption Key management | create / get / list / rotate / delete / status / set-default / set-safety / clear-safety / reencrypt / reencrypt-progress |
| dynamic-secrets | Dynamic credential roles and leases | role create/get/list/delete, generate, lease renew/revoke/list |

### Data Management

| Command | Description | Documentation |
|---------|-------------|---------------|
| backup | Backup and restore | Full/incremental backups, verification |
| migrate | Data migration | Move data between storage systems |
| transaction | Atomic operations | Compare-and-swap transactions |
| cache | Cache management | Cache statistics and control |

### Real-Time & Messaging

| Command | Description | Documentation |
|---------|-------------|---------------|
| msg | Messaging and notifications | See [Messaging CLI Reference](../messaging/cli.md) |
| scheduler | Job scheduling | See [Scheduler CLI Reference](../scheduler/cli.md) |

### Advanced Features

| Command | Description | Documentation |
|---------|-------------|---------------|
| vector | Vector similarity search | Embeddings, namespaces, search |
| embedding | Embedding generation | Multi-provider embedding generation |
| ollama | Ollama integration | Local embedding models via Ollama |
| rag | Retrieval-Augmented Generation | RAG queries and context building |
| objectstorage | Object storage | Binary blob and file storage |
| timeseries | Time series data | Time series operations |
| node | Node management | Add, remove, replace nodes |
| rebalance | Cluster rebalancing | Rebalance shard distribution |

### Identity & Access Management

| Command | Description | Documentation |
|---------|-------------|---------------|
| iam | Identity and Access Management | Multi-tenant IAM, RBAC, ABAC, OAuth |
| oauth2 | OAuth2 client lifecycle | create / list / show / update / rotate-secret / delete / pin-cert |
| ssh | xkms-backed SSH key store + known-hosts pinning | Generate, import, export-public, list, scopes |

### Orchestration & Fleet

| Command | Description | Documentation |
|---------|-------------|---------------|
| orchestrator | Sessions, prompts, jobs, workflows, **stateless inference** | All transports, streaming, host affinity |
| agent | Fleet agent management (deploy, list, inspect, update) | mTLS bootstrap, gRPC event ingest |
| project | Agentic project CRUD + per-agent roles and quotas | Workspace, git, quotas, `grant-role` / `revoke-role` |
| mcp | Model Context Protocol client | `serve --stdio` bridge for MCP hosts, `tools`, `call` |

## Global Flags

All commands support these flags:

| Flag | Description | Default |
|------|-------------|---------|
| `--address` | Server address (URL form for REST/QUIC/gRPC, path for Unix socket) | `/home/$USER/quicbaas.sock` |
| `--transport` | Protocol (`rest`, `grpc`, `quic`, `unix`, `mcp`, `embedded`) | auto-detected from `--address` scheme |
| `--tenant` | Tenant ID for multi-tenant operations | `default` |
| `--timeout` | Request timeout | `30s` |
| `--output` | Format (json, yaml, table, raw) | `table` |
| `--consistency` | Consistency level | `linearizable` |
| `--tls` | Enable TLS | `false` |
| `--tls-cert` | TLS client certificate (mTLS) | - |
| `--tls-key` | TLS client key (mTLS) | - |
| `--tls-ca` | TLS CA certificate | auto-detected from `--data-dir/certs/ca-chain.crt` |
| `--tls-skip-verify` | Skip TLS cert verification (operator opt-in only) | `false` |
| `--max-retries` | Maximum retry attempts | `3` |
| `--retry-backoff` | Initial retry backoff | `100ms` |
| `--fips` | Enable FIPS 140-3 mode | `false` |

### Transport selection

The CLI auto-selects a transport from `--address`:

| Address pattern | Transport |
|-----------------|-----------|
| `https://host:port` | REST |
| `host:port` (no scheme) | gRPC over TLS |
| `quic://host:port` or `--transport quic` | QUIC (multiplexed streams) |
| `mcp://host:port` or `--transport mcp` | MCP (JSON-RPC over WSS) |
| `/path/to/socket` (starts with `/`) | gRPC over Unix domain socket |

Pass `--transport <name>` explicitly to override. Every CLI subcommand
works on every transport — see the [multi-protocol parity guide](architecture.md)
and the integration tests under `test/integration/cli/`.

## Configuration

Create `.quicbaas.yaml` in your home directory:

```yaml
server:
  address: localhost:50051
  timeout: 30s

output:
  format: table
  color: true

consistency:
  default: linearizable

tls:
  enabled: false
  cert: /path/to/client.crt
  key: /path/to/client.key
  ca: /path/to/ca.crt
```

Environment variables:
```bash
export QUICBAAS_ADDRESS=localhost:50051
export QUICBAAS_TIMEOUT=30s
export QUICBAAS_OUTPUT=json
export QUICBAAS_TLS_ENABLED=true
```

Priority: Command-line flags > Environment variables > Config file > Defaults

## Shell Completion

```bash
# Bash
quicbaas completion bash > /etc/bash_completion.d/quicbaas

# Zsh
quicbaas completion zsh > "${fpath[1]}/_quicbaas"

# Fish
quicbaas completion fish > ~/.config/fish/completions/quicbaas.fish
```

## See Also

- [Architecture](architecture.md) - System design and components
- [Security](../security-hardening/user.md) - Security features and best practices
- [Vector Database](../vector/user.md) - Vector search capabilities
- [Messaging](../messaging/architecture.md) - Real-time messaging system
- [WebSocket](../websocket/architecture.md) - WebSocket server and subprotocols


---

## quicbaas server

Server management commands for QuicBaaS.

### Commands

#### start

Start the QuicBaaS server with HTTP, gRPC, and Raft interfaces.

```bash
quicbaas start
```

##### Flags
- `--http-port int`: HTTP server port (default: 8080)
- `--grpc-port int`: gRPC server port (default: 9090)
- `--raft-port int`: Raft communication port (default: 63000)
- `--raft-address string`: Raft node address (default: <hostname>:<raft-port>)
- `--node-id uint64`: Raft node ID (default: 1)
- `--config string`: Path to configuration file
- `--fips`: Enable FIPS mode
- `--log-level string`: Log level (debug, info, warn, error) (default: "info")

##### Examples

```bash
# Start server with defaults
quicbaas start

# Start with custom ports
quicbaas start --http-port 8080 --grpc-port 9090 --raft-port 63000

# Start in FIPS mode
quicbaas start --fips --log-level debug

# Start with custom configuration file
quicbaas start --config /etc/quicbaas/config.yaml

# Start as a cluster node
quicbaas start --node-id 2 --raft-address 203.0.113.2:63000
```

### Server Features

#### HTTP REST API
- RESTful API for data operations
- OpenAPI/Swagger documentation
- JSON request/response format
- CORS support for browser clients

#### gRPC API
- High-performance RPC interface
- Protocol Buffers serialization
- Streaming support
- Native language bindings

#### Raft Consensus
- Multi-Raft for horizontal scalability
- Strong consistency guarantees
- Automatic leader election
- Log replication and snapshotting

#### Storage Engine
- PebbleDB (RocksDB fork) backend
- LSM-tree architecture
- Automatic compaction
- Write-ahead logging (WAL)

#### Security
- TLS/mTLS support
- RBAC authorization
- Encryption at rest
- Audit logging

### Configuration File

The configuration file supports YAML format with sections for:
- Server settings (ports, timeouts, limits)
- Raft cluster configuration
- Storage settings (data directory, cache size)
- Security settings (TLS, encryption, FIPS)
- Logging configuration

Example configuration:
```yaml
server:
  port: 8080
  grpc_port: 9090
  shutdown_timeout: 30s

raft:
  node_id: 1
  raft_address: "localhost:63000"
  data_dir: "/var/lib/quicbaas/raft"

storage:
  data_dir: "/var/lib/quicbaas/data"
  cache_size: 512MB

security:
  tls_enabled: true
  tls_cert: "/etc/quicbaas/server.crt"
  tls_key: "/etc/quicbaas/server.key"
  tls_ca: "/etc/quicbaas/ca.crt"
```

### Startup Process

1. **Configuration**: Load config from file and CLI flags
2. **Data Directory**: Create/verify data directory
3. **Server Instance**: Create server with config
4. **Raft Initialization**: Initialize Raft node
5. **Storage**: Initialize storage engine
6. **API Servers**: Start HTTP and gRPC servers
7. **Ready**: Server ready to accept requests

### Graceful Shutdown

The server handles shutdown signals (SIGTERM, SIGINT) gracefully:
1. Stop accepting new requests
2. Complete in-flight requests
3. Flush pending writes
4. Close Raft connections
5. Shutdown storage engine

### Environment Variables

- `QUICBAAS_CONFIG`: Path to configuration file
- `QUICBAAS_DATA_DIR`: Data directory
- `QUICBAAS_LOG_LEVEL`: Log level
- `QUICBAAS_FIPS`: Enable FIPS mode (true/false)

### Notes

- Server requires write access to data directory
- Default data directory: `/var/lib/quicbaas` or `./data`
- HTTP and gRPC servers run concurrently
- Raft address must be reachable by other cluster nodes
- Node ID must be unique within the cluster
- FIPS mode enforces cryptographic standards


---

## quicbaas version

Display version information for QuicBaaS.

### Command

#### version

Display detailed version information.

```bash
quicbaas version
```

##### Flags
- `-s, --short`: Print only the version number

##### Examples

```bash
# Display full version information
quicbaas version

# Display only version number
quicbaas version --short

# Display version number (short flag)
quicbaas version -s
```

### Output

#### Full Version (default)

```
QuicBaaS Version: 1.0.0
Git Commit: a1b2c3d4e5f6
Build Timestamp: 2025-12-19T10:30:00Z
Go Version: go1.21.5
Platform: linux/amd64
```

#### Short Version

```
1.0.0
```

### Version Information

The version command displays:
- **Version Number**: Semantic version (e.g., 1.0.0)
- **Git Commit**: Git commit hash from build
- **Build Timestamp**: When the binary was built
- **Go Version**: Go compiler version used
- **Platform**: Operating system and architecture

### Notes

- Version information is embedded at build time
- Useful for bug reports and compatibility checking
- Short flag is useful for scripting and automation
- Platform shows OS (linux, darwin, windows) and architecture (amd64, arm64)


---

## QuicBaaS Command Coverage Matrix

This document provides a comprehensive overview of all CLI commands in QuicBaaS, their protocol support, and test coverage status.

### Summary

| Metric | Count |
|--------|-------|
| Total CLI Commands | 308 |
| QUIC Protocol Operations | 150+ |
| REST API Endpoints | 180+ |
| MCP Tools | 270+ |
| Unix Socket Operations | 35+ |

### Protocol Legend

| Symbol | Meaning |
|--------|---------|
| Y | Full support |
| P | Partial support |
| - | Not implemented |
| N/A | Not applicable |

### Test Coverage Legend

| Symbol | Meaning |
|--------|---------|
| U | Unit tests |
| I | Integration tests |
| B | Both unit and integration |
| - | No tests |

---

### Auth Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `auth login` | Y | Y | Y | - | Y | B | Complete |
| `auth logout` | Y | Y | Y | - | Y | B | Complete |
| `auth status` | Y | Y | Y | - | Y | B | Complete |

### Backup Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `backup create` | Y | Y | Y | - | Y | B | Complete |
| `backup list` | Y | Y | Y | - | Y | B | Complete |
| `backup restore` | Y | Y | Y | - | Y | B | Complete |
| `backup delete` | Y | Y | Y | - | Y | B | Complete |
| `backup verify` | Y | Y | Y | - | Y | B | Complete |
| `backup status` | Y | Y | Y | - | Y | B | Complete |

### Cache Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `cache stats` | Y | Y | Y | - | Y | B | Complete |
| `cache clear` | Y | Y | Y | - | Y | B | Complete |
| `cache config` | Y | Y | Y | - | Y | B | Complete |
| `cache enable` | Y | Y | Y | - | Y | U | Complete |
| `cache disable` | Y | Y | Y | - | Y | U | Complete |
| `cache warm` | Y | Y | Y | - | Y | U | Complete |

### Cluster Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `cluster status` | Y | Y | Y | - | Y | B | Complete |

### Crypto Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `crypto encrypt` | Y | Y | Y | - | Y | B | Complete |
| `crypto decrypt` | Y | Y | Y | - | Y | B | Complete |
| `crypto rewrap` | Y | Y | Y | - | Y | B | Complete |
| `crypto sign` | Y | Y | Y | - | Y | B | Complete |
| `crypto verify` | Y | Y | Y | - | Y | B | Complete |
| `crypto hash` | Y | Y | Y | - | Y | B | Complete |
| `crypto random` | Y | Y | Y | - | Y | B | Complete |
| `crypto datakey` | Y | Y | Y | - | Y | B | Complete |

### Data Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `data put` | Y | Y | Y | - | Y | B | Complete |
| `data get` | Y | Y | Y | - | Y | B | Complete |
| `data delete` | Y | Y | Y | - | Y | B | Complete |
| `data list` | Y | Y | Y | - | Y | B | Complete |
| `data query` | Y | Y | Y | - | Y | B | Complete |

### Embedding Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `embedding generate` | Y | Y | Y | - | Y | B | Complete |
| `embedding batch` | Y | Y | Y | - | Y | B | Complete |
| `embedding providers` | Y | Y | Y | - | Y | B | Complete |
| `embedding provider info` | Y | Y | Y | - | Y | B | Complete |
| `embedding provider set-default` | Y | Y | Y | - | Y | U | Complete |
| `embedding cache stats` | Y | Y | Y | - | Y | B | Complete |
| `embedding cache clear` | Y | Y | Y | - | Y | B | Complete |

### IAM Module

#### IAM Admin

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam admin assignment create` | Y | Y | Y | - | Y | B | Complete |
| `iam admin assignment get` | Y | Y | Y | - | Y | B | Complete |
| `iam admin assignment update` | Y | Y | Y | - | Y | B | Complete |
| `iam admin assignment delete` | Y | Y | Y | - | Y | B | Complete |
| `iam admin assignment list` | Y | Y | Y | - | Y | B | Complete |
| `iam admin check-permission` | Y | Y | Y | - | Y | B | Complete |
| `iam admin impersonate start` | Y | Y | Y | - | Y | U | Complete |
| `iam admin impersonate end` | Y | Y | Y | - | Y | U | Complete |
| `iam admin impersonate list` | Y | Y | Y | - | Y | U | Complete |

#### IAM App

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam app register` | Y | Y | Y | - | Y | B | Complete |
| `iam app get` | Y | Y | Y | - | Y | B | Complete |
| `iam app update` | Y | Y | Y | - | Y | B | Complete |
| `iam app delete` | Y | Y | Y | - | Y | B | Complete |
| `iam app list` | Y | Y | Y | - | Y | B | Complete |
| `iam app rotate-secret` | Y | Y | Y | - | Y | U | Complete |

#### IAM Audit

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam audit query` | Y | Y | Y | - | Y | U | Complete |
| `iam audit get` | Y | Y | Y | - | Y | U | Complete |

#### IAM Federation

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam federation provider create` | Y | Y | Y | - | Y | B | Complete |
| `iam federation provider get` | Y | Y | Y | - | Y | B | Complete |
| `iam federation provider update` | Y | Y | Y | - | Y | B | Complete |
| `iam federation provider delete` | Y | Y | Y | - | Y | B | Complete |
| `iam federation provider list` | Y | Y | Y | - | Y | B | Complete |
| `iam federation identity link` | Y | Y | Y | - | Y | U | Complete |
| `iam federation identity unlink` | Y | Y | Y | - | Y | U | Complete |
| `iam federation identity list` | Y | Y | Y | - | Y | U | Complete |

#### IAM GDPR

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam gdpr consent grant` | Y | Y | Y | - | Y | B | Complete |
| `iam gdpr consent withdraw` | Y | Y | Y | - | Y | B | Complete |
| `iam gdpr consent get` | Y | Y | Y | - | Y | B | Complete |
| `iam gdpr consent list` | Y | Y | Y | - | Y | B | Complete |
| `iam gdpr export request` | Y | Y | Y | - | Y | B | Complete |
| `iam gdpr export status` | Y | Y | Y | - | Y | B | Complete |
| `iam gdpr deletion request` | Y | Y | Y | - | Y | B | Complete |
| `iam gdpr deletion cancel` | Y | Y | Y | - | Y | B | Complete |
| `iam gdpr deletion status` | Y | Y | Y | - | Y | B | Complete |

#### IAM Group

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam group create` | Y | Y | Y | - | Y | B | Complete |
| `iam group get` | Y | Y | Y | - | Y | B | Complete |
| `iam group update` | Y | Y | Y | - | Y | B | Complete |
| `iam group delete` | Y | Y | Y | - | Y | B | Complete |
| `iam group list` | Y | Y | Y | - | Y | B | Complete |
| `iam group add-member` | Y | Y | Y | - | Y | B | Complete |
| `iam group remove-member` | Y | Y | Y | - | Y | B | Complete |
| `iam group members` | Y | Y | Y | - | Y | B | Complete |

#### IAM MFA

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam mfa status` | Y | Y | Y | - | Y | B | Complete |
| `iam mfa enroll` | Y | Y | Y | - | Y | B | Complete |
| `iam mfa verify` | Y | Y | Y | - | Y | B | Complete |
| `iam mfa disable` | Y | Y | Y | - | Y | U | Complete |
| `iam mfa backup-codes status` | Y | Y | Y | - | Y | U | Complete |
| `iam mfa backup-codes generate` | Y | Y | Y | - | Y | U | Complete |

#### IAM PEP (Policy Enforcement Point)

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam pep check` | Y | Y | Y | - | Y | B | Complete |
| `iam pep check-batch` | Y | Y | Y | - | Y | B | Complete |

#### IAM Policy

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam policy apply` | Y | Y | Y | - | Y | B | Complete |
| `iam policy get` | Y | Y | Y | - | Y | B | Complete |
| `iam policy delete` | Y | Y | Y | - | Y | B | Complete |
| `iam policy list` | Y | Y | Y | - | Y | B | Complete |
| `iam policy validate` | Y | Y | Y | - | Y | U | Complete |
| `iam policy test` | Y | Y | Y | - | Y | U | Complete |

#### IAM Role

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam role create` | Y | Y | Y | - | Y | B | Complete |
| `iam role get` | Y | Y | Y | - | Y | B | Complete |
| `iam role update` | Y | Y | Y | - | Y | B | Complete |
| `iam role delete` | Y | Y | Y | - | Y | B | Complete |
| `iam role list` | Y | Y | Y | - | Y | B | Complete |
| `iam role assign` | Y | Y | Y | - | Y | B | Complete |
| `iam role revoke` | Y | Y | Y | - | Y | B | Complete |
| `iam role reconcile` | Y | Y | Y | Y | Y | B | Complete |

The Unix column reads `Y` for `reconcile` and `-` for its siblings because
`reconcile` was verified over every transport when it was added
(go-quicbaas#573). The Unix transport is the gRPC transport over a socket
(`sdk/go/transport/unix`), so the sibling verbs are very likely reachable there
too and their `-` is stale — but that is an unverified claim, and this table
records what has been exercised, not what ought to work.

#### IAM Session

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam session list` | Y | Y | Y | - | Y | U | Complete |
| `iam session get` | Y | Y | Y | - | Y | U | Complete |
| `iam session revoke` | Y | Y | Y | - | Y | U | Complete |
| `iam session revoke-all` | Y | Y | Y | - | Y | U | Complete |

#### IAM Tenant

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam tenant create` | Y | Y | Y | - | Y | B | Complete |
| `iam tenant get` | Y | Y | Y | - | Y | B | Complete |
| `iam tenant update` | Y | Y | Y | - | Y | B | Complete |
| `iam tenant delete` | Y | Y | Y | - | Y | B | Complete |
| `iam tenant list` | Y | Y | Y | - | Y | B | Complete |

#### IAM User

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam user create` | Y | Y | Y | - | Y | B | Complete |
| `iam user get` | Y | Y | Y | - | Y | B | Complete |
| `iam user update` | Y | Y | Y | - | Y | B | Complete |
| `iam user delete` | Y | Y | Y | - | Y | B | Complete |
| `iam user list` | Y | Y | Y | - | Y | B | Complete |
| `iam user role assign` | Y | Y | Y | - | Y | B | Complete |
| `iam user role revoke` | Y | Y | Y | - | Y | B | Complete |

### KMS Module

#### KMS Key Management

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `kms key generate` | Y | Y | Y | - | Y | B | Complete |
| `kms key get` | Y | Y | Y | - | Y | B | Complete |
| `kms key list` | Y | Y | Y | - | Y | B | Complete |
| `kms key rotate` | Y | Y | Y | - | Y | B | Complete |
| `kms key delete` | Y | Y | Y | - | Y | B | Complete |
| `kms key import` | Y | Y | Y | - | Y | B | Complete |
| `kms key enable` | Y | Y | Y | - | Y | B | Complete |
| `kms key disable` | Y | Y | Y | - | Y | B | Complete |
| `kms key versions` | Y | Y | Y | - | Y | B | Complete |
| `kms key export` | Y | Y | Y | - | Y | B | Complete |
| `kms key unwrap` | Y | Y | Y | - | Y | B | Complete |
| `kms key set-safety` | Y | Y | Y | - | Y | B | Complete |
| `kms encrypt` | Y | Y | Y | - | Y | B | Complete |
| `kms decrypt` | Y | Y | Y | - | Y | B | Complete |

#### KMS DEK Management

`kms dek` is a deprecated alias that mounts the canonical `dek` tree below;
every verb, transport and test in that table applies unchanged. The legacy
`kms dek generate` verb survives as an alias of `dek create`.

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `kms dek *` | Y | Y | Y | Y | Y | B | Deprecated alias of `dek` |

#### DEK Management (canonical `dek` tree)

All verbs route through `Security().DEK()` on the active SDK transport.

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `dek create` (alias `generate`) | Y | Y | Y | Y | Y | U | Complete |
| `dek get` | Y | Y | Y | Y | Y | U | Complete |
| `dek list` | Y | Y | Y | Y | Y | U | Complete |
| `dek rotate` | Y | Y | Y | Y | Y | U | Complete |
| `dek delete` | Y | Y | Y | Y | Y | U | Complete |
| `dek status` | Y | Y | Y | Y | Y | U | Complete |
| `dek set-default` | Y | Y | Y | Y | Y | U | Complete |
| `dek set-safety` | Y | Y | Y | Y | Y | U | Complete |
| `dek clear-safety` | Y | Y | Y | Y | Y | U | Complete |
| `dek reencrypt` | Y | Y | Y | Y | Y | U | Complete |
| `dek reencrypt-progress` | Y | Y | Y | Y | Y | U | Complete |

#### KMS Backend

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `kms backend status` | Y | Y | Y | - | Y | B | Complete |
| `kms backend test` | Y | Y | Y | - | Y | B | Complete |

#### KMS Safety

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `kms safety check` | Y | Y | Y | - | Y | U | Complete |
| `kms safety report` | Y | Y | Y | - | Y | U | Complete |

### Migrate Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `migrate start` | Y | Y | Y | - | Y | B | Complete |
| `migrate status` | Y | Y | Y | - | Y | B | Complete |
| `migrate cancel` | Y | Y | Y | - | Y | U | Complete |

### Node Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `node add` | Y | Y | Y | - | Y | B | Complete |
| `node remove` | Y | Y | Y | - | Y | B | Complete |
| `node replace` | Y | Y | Y | - | Y | B | Complete |
| `node list` | Y | Y | Y | - | Y | B | Complete |

### Object Storage Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `object put` | Y | Y | Y | Y | Y | B | Complete |
| `object get` | Y | Y | Y | Y | Y | B | Complete |
| `object delete` | Y | Y | Y | Y | Y | B | Complete |
| `object head` | Y | Y | Y | Y | Y | B | Complete |
| `object list` | Y | Y | Y | Y | Y | B | Complete |
| `object list-by-tag` | Y | Y | Y | Y | Y | B | Complete |
| `object list-by-tenant` | Y | Y | Y | Y | Y | B | Complete |
| `object copy` | Y | Y | Y | Y | Y | B | Complete |
| `object archive` | Y | Y | Y | Y | Y | U | Complete |
| `object restore` | Y | Y | Y | Y | Y | U | Complete |
| `object verify` | Y | Y | Y | Y | Y | U | Complete |
| `object replicate` | Y | Y | Y | Y | Y | U | Complete |
| `object migrate` | Y | Y | Y | Y | Y | U | Complete |
| `object metadata get` | Y | Y | Y | Y | Y | B | Complete |
| `object metadata update` | Y | Y | Y | Y | Y | B | Complete |
| `object stats` | Y | Y | Y | Y | Y | U | Complete |
| `object lifecycle set` | Y | Y | Y | Y | Y | U | Complete |
| `object lifecycle get` | Y | Y | Y | Y | Y | U | Complete |

### Ollama Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `ollama health` | Y | Y | Y | - | Y | B | Complete |
| `ollama models` | Y | Y | Y | - | Y | B | Complete |
| `ollama model info` | Y | Y | Y | - | Y | B | Complete |
| `ollama embed` | Y | Y | Y | - | Y | B | Complete |
| `ollama embed-batch` | Y | Y | Y | - | Y | B | Complete |

### RAG Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `rag query` | Y | Y | Y | - | Y | B | Complete |
| `rag context` | Y | Y | Y | - | Y | B | Complete |

### Rebalance Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `rebalance start` | Y | Y | Y | - | Y | B | Complete |
| `rebalance status` | Y | Y | Y | - | Y | B | Complete |
| `rebalance plan` | Y | Y | Y | - | Y | U | Complete |

### Role Module (Legacy)

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `role create` | Y | Y | Y | - | Y | B | Complete |
| `role delete` | Y | Y | Y | - | Y | B | Complete |
| `role list` | Y | Y | Y | - | Y | B | Complete |
| `role show` | Y | Y | Y | - | Y | U | Complete |
| `role assign` | Y | Y | Y | - | Y | B | Complete |
| `role unassign` | Y | Y | Y | - | Y | U | Complete |
| `role permissions` | Y | Y | Y | - | Y | U | Complete |
| `role grant` | Y | Y | Y | - | Y | U | Complete |
| `role revoke` | Y | Y | Y | - | Y | U | Complete |
| `role list-permissions` | Y | Y | Y | - | Y | U | Complete |

### Scheduler Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `scheduler create` | Y | Y | Y | Y | Y | B | Complete |
| `scheduler list` | Y | Y | Y | Y | Y | B | Complete |
| `scheduler get` | Y | Y | Y | Y | Y | B | Complete |
| `scheduler delete` | Y | Y | Y | Y | Y | B | Complete |
| `scheduler trigger` | Y | Y | Y | Y | Y | B | Complete |
| `scheduler pause` | Y | Y | Y | Y | Y | B | Complete |
| `scheduler resume` | Y | Y | Y | Y | Y | B | Complete |
| `scheduler cancel` | Y | Y | Y | Y | Y | B | Complete |
| `scheduler history` | Y | Y | Y | Y | Y | U | Complete |
| `scheduler status` | Y | Y | Y | Y | Y | B | Complete |

### Seal Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `seal status` | Y | Y | Y | - | Y | B | Complete |
| `seal init` | Y | Y | Y | - | Y | B | Complete |
| `seal unseal` | Y | Y | Y | - | Y | B | Complete |
| `seal seal` | Y | Y | Y | - | Y | B | Complete |
| `seal rekey` | Y | Y | Y | - | Y | B | Complete |

### Secrets Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `secrets put` | Y | Y | Y | - | Y | B | Complete |
| `secrets get` | Y | Y | Y | - | Y | B | Complete |
| `secrets list` | Y | Y | Y | - | Y | B | Complete |
| `secrets delete` | Y | Y | Y | - | Y | B | Complete |
| `secrets destroy` | Y | Y | Y | - | Y | U | Complete |
| `secrets versions` | Y | Y | Y | - | Y | B | Complete |
| `secrets rollback` | Y | Y | Y | - | Y | U | Complete |
| `secrets rotate` | Y | Y | Y | - | Y | U | Complete |
| `secrets enable` | Y | Y | Y | - | Y | U | Complete |
| `secrets disable` | Y | Y | Y | - | Y | U | Complete |
| `secrets get-prefix` | Y | Y | Y | - | Y | B | Complete |
| `secrets watch` | Y | Y | Y | - | Y | U | Complete |
| `secrets fetch` | Y | Y | Y | - | Y | U | Complete |

### Dynamic Secrets Module

Alias: `quicbaas dynsec`. All eight verbs route through
`Security().DynamicSecrets()`; Unix delegates to the gRPC handler verbatim.

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `dynamic-secrets role create` | Y | Y | Y | Y | Y | U | Complete |
| `dynamic-secrets role get` | Y | Y | Y | Y | Y | U | Complete |
| `dynamic-secrets role list` | Y | Y | Y | Y | Y | U | Complete |
| `dynamic-secrets role delete` | Y | Y | Y | Y | Y | U | Complete |
| `dynamic-secrets generate` | Y | Y | Y | Y | Y | U | Complete |
| `dynamic-secrets lease renew` | Y | Y | Y | Y | Y | U | Complete |
| `dynamic-secrets lease revoke` | Y | Y | Y | Y | Y | U | Complete |
| `dynamic-secrets lease list` | Y | Y | Y | Y | Y | U | Complete |

### Time Series Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `timeseries write` | Y | Y | Y | - | Y | B | Complete |
| `timeseries write-batch` | Y | Y | Y | - | Y | B | Complete |
| `timeseries query` | Y | Y | Y | - | Y | B | Complete |
| `timeseries delete` | Y | Y | Y | - | Y | U | Complete |
| `timeseries get-retention` | Y | Y | Y | - | Y | U | Complete |
| `timeseries set-retention` | Y | Y | Y | - | Y | U | Complete |
| `timeseries list-metrics` | Y | Y | Y | - | Y | U | Complete |

### Transaction Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `txn compare` | Y | Y | Y | - | Y | B | Complete |
| `txn success` | Y | Y | Y | - | Y | B | Complete |
| `txn failure` | Y | Y | Y | - | Y | B | Complete |
| `txn execute` | Y | Y | Y | - | Y | B | Complete |
| `txn reset` | Y | Y | Y | - | Y | U | Complete |
| `txn show` | Y | Y | Y | - | Y | U | Complete |

### Upgrade Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `upgrade start` | Y | Y | Y | - | Y | U | Complete |
| `upgrade status` | Y | Y | Y | - | Y | U | Complete |

### User Module (Legacy)

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `user create` | Y | Y | Y | - | Y | B | Complete |
| `user delete` | Y | Y | Y | - | Y | B | Complete |
| `user list` | Y | Y | Y | - | Y | B | Complete |
| `user show` | Y | Y | Y | - | Y | U | Complete |
| `user update` | Y | Y | Y | - | Y | U | Complete |

### Vector Module

#### Vector Namespace

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `vector namespace create` | Y | Y | Y | - | Y | B | Complete |
| `vector namespace delete` | Y | Y | Y | - | Y | B | Complete |
| `vector namespace list` | Y | Y | Y | - | Y | B | Complete |
| `vector namespace info` | Y | Y | Y | - | Y | B | Complete |

#### Vector Index

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `vector index upsert` | Y | Y | Y | - | Y | B | Complete |
| `vector index batch-upsert` | Y | Y | Y | - | Y | B | Complete |
| `vector index get` | Y | Y | Y | - | Y | B | Complete |
| `vector index delete` | Y | Y | Y | - | Y | B | Complete |
| `vector index batch-delete` | Y | Y | Y | - | Y | B | Complete |
| `vector index list` | Y | Y | Y | - | Y | B | Complete |
| `vector index update-metadata` | Y | Y | Y | - | Y | U | Complete |

#### Vector Search

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `vector search similar` | Y | Y | Y | - | Y | B | Complete |
| `vector search by-id` | Y | Y | Y | - | Y | B | Complete |
| `vector search hybrid` | Y | Y | Y | - | Y | B | Complete |
| `vector search by-metadata` | Y | Y | Y | - | Y | B | Complete |

#### Vector Admin

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `vector admin rebuild` | Y | Y | Y | - | Y | B | Complete |
| `vector admin progress` | Y | Y | Y | - | Y | U | Complete |
| `vector admin stats` | Y | Y | Y | - | Y | B | Complete |
| `vector admin export` | Y | Y | Y | - | Y | U | Complete |
| `vector admin import` | Y | Y | Y | - | Y | U | Complete |
| `vector admin compact` | Y | Y | Y | - | Y | U | Complete |

### Version Command

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `version` | N/A | N/A | N/A | N/A | N/A | U | Complete |

---

### Protocol Support Summary by Module

| Module | gRPC | REST | QUIC | Unix | MCP |
|--------|------|------|------|------|-----|
| Auth | 100% | 100% | 100% | - | 100% |
| Backup | 100% | 100% | 100% | - | 100% |
| Cache | 100% | 100% | 100% | - | 100% |
| Cluster | 100% | 100% | 100% | - | 100% |
| Crypto | 100% | 100% | 100% | - | 100% |
| Data | 100% | 100% | 100% | - | 100% |
| Embedding | 100% | 100% | 100% | - | 100% |
| IAM | 100% | 100% | 100% | - | 100% |
| KMS | 100% | 100% | 100% | - | 100% |
| Migrate | 100% | 100% | 100% | - | 100% |
| Node | 100% | 100% | 100% | - | 100% |
| Object Storage | 100% | 100% | 100% | 100% | 100% |
| Ollama | 100% | 100% | 100% | - | 100% |
| RAG | 100% | 100% | 100% | - | 100% |
| Rebalance | 100% | 100% | 100% | - | 100% |
| Role | 100% | 100% | 100% | - | 100% |
| Scheduler | 100% | 100% | 100% | 100% | 100% |
| Seal | 100% | 100% | 100% | - | 100% |
| Secrets | 100% | 100% | 100% | - | 100% |
| Time Series | 100% | 100% | 100% | - | 100% |
| Transaction | 100% | 100% | 100% | - | 100% |
| Upgrade | 100% | 100% | 100% | - | 100% |
| User | 100% | 100% | 100% | - | 100% |
| Vector | 100% | 100% | 100% | - | 100% |

### Test Coverage Summary

| Category | Unit Tests | Integration Tests |
|----------|-----------|-------------------|
| CLI Commands | 308/308 | 250+ |
| gRPC Client | 100% | 100% |
| REST Handlers | 100% | 100% |
| QUIC Protocol | 100% | 100% |
| MCP Tools | 100% | 100% |
| Unix Socket | 100% | 100% |

### Integration Test Locations

| Module | Test Directory |
|--------|---------------|
| API (gRPC) | `test/integration/api/grpc/` |
| API (REST) | `test/integration/api/rest/` |
| API (QUIC) | `test/integration/api/quic/` |
| CLI | `test/integration/cli/` |
| Auth | `test/integration/auth/` |
| Backup | `test/integration/backup/` |
| Batch | `test/integration/batch/` |
| Cache | `test/integration/cache/` |
| Cluster | `test/integration/cluster/` |
| DAO | `test/integration/dao/` |
| DEK | `test/integration/dek/` |
| Embedding | `test/integration/embedding/` |
| IAM | `test/integration/iam/` |
| KMS | `test/integration/kms/` |
| KVStore | `test/integration/kvstore/` |
| MCP | `test/integration/mcp/` |
| Object Store | `test/integration/objectstore/` |
| Parameter Store | `test/integration/parameter_store/` |
| Raft | `test/integration/raft/` |
| RAG | `test/integration/rag/` |
| Scheduler | `test/integration/scheduler/` |
| Seal | `test/integration/seal/` |
| Secrets | `test/integration/secrets/` |
| Time Series | `test/integration/timeseries/` |
| Transactions | `test/integration/transactions/` |
| Vector | `test/integration/vector/` |

### Notes

1. **Unix Socket Support**: Currently implemented for Object Storage and Scheduler modules. These use high-performance binary protocols for inter-node communication.

2. **MCP (Model Context Protocol)**: Full tool support for AI agent integration. All commands are available as MCP tools with proper input validation and JSON responses.

3. **QUIC Protocol**: Uses binary opcodes for high-performance communication. All 150+ operations are implemented with full feature parity.

4. **REST API**: Complete OpenAPI documentation available at `/docs/openapi.yaml`. Supports both JSON and protocol buffer encoding.

5. **gRPC**: Default protocol for CLI communication. Supports Unix socket, TCP, and TLS connections.
