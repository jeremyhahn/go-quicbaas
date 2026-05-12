# QuicBaaS CLI Documentation

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
| [server](server.md) | Start and manage QuicBaaS server | Server startup, configuration, lifecycle |
| [data](data.md) | Data operations (put, get, delete, list, query) | CRUD operations with consistency levels |
| [cluster](cluster.md) | Cluster management and monitoring | Status, health, node information |
| [version](version.md) | Display version information | Version and build details |

### Security & Access Control

| Command | Description | Documentation |
|---------|-------------|---------------|
| [auth](auth.md) | Authentication (login, logout, status) | Session management, token handling |
| [user](user.md) | User management | Create, delete, list, update users |
| [role](role.md) | RBAC role management | Roles, permissions, assignments |
| [seal](seal.md) | Seal/unseal operations | Shamir's Secret Sharing |

### Cryptography & Secrets

| Command | Description | Documentation |
|---------|-------------|---------------|
| [kms](kms.md) | Key Management System | Generate, rotate, encrypt, decrypt |
| [secrets](secrets.md) | Secrets management | Versioned, encrypted secret storage |
| [crypto](crypto.md) | Encryption-as-a-service | Transit encryption operations |

### Data Management

| Command | Description | Documentation |
|---------|-------------|---------------|
| [backup](backup.md) | Backup and restore | Full/incremental backups, verification |
| [migrate](migrate.md) | Data migration | Move data between storage systems |
| [transaction](transaction.md) | Atomic operations | Compare-and-swap transactions |
| [cache](cache.md) | Cache management | Cache statistics and control |

### Real-Time & Messaging

| Command | Description | Documentation |
|---------|-------------|---------------|
| msg | Messaging and notifications | See [Messaging CLI Reference](../messaging/cli-reference.md) |
| scheduler | Job scheduling | See [Scheduler CLI Reference](../scheduler/cli-reference.md) |

### Advanced Features

| Command | Description | Documentation |
|---------|-------------|---------------|
| [vector](vector.md) | Vector similarity search | Embeddings, namespaces, search |
| [embedding](embedding.md) | Embedding generation | Multi-provider embedding generation |
| [ollama](ollama.md) | Ollama integration | Local embedding models via Ollama |
| [rag](rag.md) | Retrieval-Augmented Generation | RAG queries and context building |
| [objectstorage](objectstorage.md) | Object storage | Binary blob and file storage |
| [timeseries](timeseries.md) | Time series data | Time series operations |
| [node](node.md) | Node management | Add, remove, replace nodes |
| [rebalance](rebalance.md) | Cluster rebalancing | Rebalance shard distribution |

### Identity & Access Management

| Command | Description | Documentation |
|---------|-------------|---------------|
| [iam](iam.md) | Identity and Access Management | Multi-tenant IAM, RBAC, ABAC, OAuth |
| [ssh](ssh.md) | xkms-backed SSH key store + known-hosts pinning | Generate, import, export-public, list, scopes |

### Orchestration & Fleet

| Command | Description | Documentation |
|---------|-------------|---------------|
| [orchestrator](orchestrator.md) | Sessions, prompts, jobs, workflows, **stateless inference** | All transports, streaming, host affinity |
| [agent](agent.md) | Fleet agent management (deploy, list, inspect, update) | mTLS bootstrap, gRPC event ingest |

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
works on every transport — see the [multi-protocol parity guide](../api/multiprotocol.md)
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

- [Architecture](../architecture/README.md) - System design and components
- [Security](../security/README.md) - Security features and best practices
- [Vector Database](../vector/README.md) - Vector search capabilities
- [Messaging](../messaging/architecture.md) - Real-time messaging system
- [WebSocket](../websocket/architecture.md) - WebSocket server and subprotocols
