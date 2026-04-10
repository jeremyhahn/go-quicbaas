# QuicBaaS Configuration Files

This directory contains pre-configured deployment templates for different use cases.

## Configuration Options

| Config | Storage Backend | Raft | Persistence | Use Case |
|--------|-----------------|------|-------------|----------|
| `memory.yaml` | memory | No | No | Testing, ephemeral data |
| `file-local.yaml` | file | No | Yes | Development, debugging |
| `pebble-local.yaml` | pebble | No | Yes | Single-node, performance |
| `raft-single-node.yaml` | raft | Yes (1 node) | Yes | Raft testing, CI/CD |
| `raft-3-node/` | raft | Yes (3 nodes) | Yes | Production, HA |

## Quick Start

### In-Memory (Testing)

No persistence, fastest startup:

```bash
# Native
quicbaasd --config configs/memory.yaml

# Docker
docker compose -f configs/docker/docker-compose.memory.yaml up
```

### File-Based Local (Development/Debugging)

Simple file-based persistence (each key is a file):

```bash
# Native
quicbaasd --config configs/file-local.yaml

# Docker
docker compose -f configs/docker/docker-compose.file-local.yaml up
```

### PebbleDB Local (Performance)

Single-node with persistence, no Raft overhead:

```bash
# Native
quicbaasd --config configs/pebble-local.yaml

# Docker
docker compose -f configs/docker/docker-compose.pebble-local.yaml up
```

### Single-Node Raft (Development/CI)

Raft consensus with single node for testing distributed behavior:

```bash
# Native
quicbaasd --config configs/raft-single-node.yaml

# Docker
docker compose -f configs/docker/docker-compose.raft-single-node.yaml up
```

### 3-Node Raft Cluster (Production)

Production-ready cluster tolerating 1 node failure:

```bash
# Docker (recommended)
docker compose -f configs/docker/docker-compose.raft-3-node.yaml up

# Native (start each node separately)
quicbaasd --config configs/raft-3-node/node1.yaml
quicbaasd --config configs/raft-3-node/node2.yaml
quicbaasd --config configs/raft-3-node/node3.yaml
```

## Docker Compose Files

Located in `configs/docker/`:

| File | Description |
|------|-------------|
| `docker-compose.memory.yaml` | In-memory single node |
| `docker-compose.file-local.yaml` | File-based single node |
| `docker-compose.pebble-local.yaml` | PebbleDB single node |
| `docker-compose.raft-single-node.yaml` | Single-node Raft |
| `docker-compose.raft-3-node.yaml` | 3-node Raft cluster |

## Endpoints

### Single Node Configurations

- REST API: https://localhost:8443
- gRPC: localhost:9443
- QUIC: localhost:10443
- MCP: localhost:11443

### 3-Node Cluster

Each node exposes different ports:

| Node | REST | gRPC |
|------|------|------|
| node1 | https://localhost:8443 | localhost:9443 |
| node2 | https://localhost:8444 | localhost:9444 |
| node3 | https://localhost:8445 | localhost:9445 |

All nodes can handle client requests; the cluster will route to the appropriate leader.

## Storage Backends

QuicBaaS supports four storage backends configured via `storage.backend`:

- **memory**: In-memory storage. No persistence. Fastest startup, ideal for testing.
- **file**: Simple file-based storage. Each key is a file. For debugging and development.
- **pebble**: Direct PebbleDB backend. Fast, single-node. For performance-critical single-node deployments.
- **raft**: Raft consensus + PebbleDB. Multi-node cluster with strong consistency and fault tolerance.

## Customization

Copy any configuration file and modify as needed:

```bash
cp configs/pebble-local.yaml my-config.yaml
# Edit my-config.yaml
quicbaasd --config my-config.yaml
```

Key settings to customize:

- `storage.backend` - Storage backend type (memory, file, pebble, raft)
- `server.rest.port` / `server.grpc.port` - API ports
- `server.quic.port` / `server.mcp.port` - Additional protocol ports
- `storage.data_dir` - Data directory path
- `storage.cache_size` - Memory cache size
- `logging.level` - Log verbosity (trace, debug, info, warn, error)
- `security.*` - TLS, authentication, encryption

## See Also

- [Cluster Documentation](../docs/cluster/README.md)
- [Full Configuration Reference](../.quicbaas.example.yaml)
