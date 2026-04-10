# Unified Sharding Architecture

This document describes QuicBaaS's unified sharding architecture, which provides a consistent approach to storage partitioning and Raft consensus across all deployment modes.

## Overview

QuicBaaS's unified sharding architecture separates physical storage organization (partitions) from distributed consensus (shards), enabling flexible deployment configurations from single-node development to multi-node production clusters.

The architecture provides:

- **Mode-based deployment**: Choose between standalone, cluster, or devcluster modes
- **Storage partitioning**: Organize data across multiple storage locations
- **Raft sharding**: Distribute consensus across multiple shard groups
- **Pluggable state machines**: Register custom state machines for specialized workloads

## Terminology

| Term | Definition |
|------|------------|
| **Partition** | A physical storage location with its own data directory. Partitions isolate data for performance, capacity, or operational reasons. |
| **Shard** | A Raft consensus group (QuicRaft ShardID). Each shard maintains its own replicated log and state machine. |
| **State Machine** | The application logic that processes Raft log entries. Different shards can use different state machine types. |
| **Mode** | The server deployment mode: standalone, cluster, or devcluster. |

## Benefits

1. **Performance isolation**: Place hot data on fast NVMe storage, cold data on slower drives
2. **Capacity scaling**: Grow storage by adding partitions to new devices
3. **Workload specialization**: Use different state machines for different data types (KV, vector, etc.)
4. **Operational flexibility**: Backup, restore, or migrate partitions independently
5. **Development simplicity**: DevCluster mode provides multi-node testing on a single machine

## Server Modes

QuicBaaS supports three server modes, configured via `server.mode`:

### Standalone Mode

Single-node deployment without Raft consensus. Ideal for development, testing, or applications that don't require high availability.

```yaml
server:
  mode: standalone
```

**Characteristics:**
- No Raft consensus overhead
- Direct storage access (PebbleDB, memory, or file)
- Single point of failure
- Simplest deployment model

**Use cases:**
- Local development
- CI/CD testing
- Edge deployments where HA is not required
- Embedded library usage

### Cluster Mode

Multi-node deployment with Raft consensus for distributed high availability. Production-ready for mission-critical workloads.

```yaml
server:
  mode: cluster
```

**Characteristics:**
- Full Raft consensus for strong consistency
- Automatic leader election and failover
- Replicated state across nodes
- Supports multiple shards per cluster

**Use cases:**
- Production deployments
- Distributed databases
- Multi-region replication
- High-availability requirements

### DevCluster Mode

Local multi-node cluster for development testing. Runs multiple nodes on a single machine with auto-configured ports.

```yaml
server:
  mode: devcluster
```

**Characteristics:**
- Multiple local nodes with Raft consensus
- Auto-incremented ports per node
- Simplified configuration
- Full cluster behavior for testing

**Use cases:**
- Testing cluster features locally
- Integration testing
- CI/CD cluster validation
- Learning Raft behavior

### Mode Selection Criteria

| Requirement | Recommended Mode |
|-------------|-----------------|
| Development/testing | `standalone` or `devcluster` |
| Production HA | `cluster` |
| Embedded library | `standalone` with `server.embedded: true` |
| CI/CD testing | `standalone` (unit tests) or `devcluster` (integration) |
| Multi-node local testing | `devcluster` |

## Storage Partitions

Partitions provide physical data isolation at the storage layer. Each partition has its own data directory and optional configuration overrides.

### Partition Configuration

```yaml
storage:
  backend: pebble
  partitions:
    default:
      data_dir: /var/lib/quicbaas/default
      wal_dir: /var/lib/quicbaas/default/wal
      cache_size: 268435456  # 256MB

    users:
      data_dir: /mnt/nvme1/users
      cache_size: 536870912  # 512MB for hot data

    vectors:
      data_dir: /mnt/nvme2/vectors
      cache_size: 1073741824  # 1GB for vector indexes
```

### Partition Fields

| Field | Type | Description |
|-------|------|-------------|
| `data_dir` | string | Absolute path to partition data directory (required) |
| `wal_dir` | string | Optional separate WAL directory (defaults to `data_dir/wal`) |
| `cache_size` | int64 | Cache size in bytes (overrides global default) |

### Partition Routing

The partition manager routes operations to partitions based on entity type prefixes. The DAO layer automatically directs entities to their configured partitions.

```go
// Example: Configure entity routing
factoryConfig := &dao.FactoryConfig{
    PartitionedProvider: dao.CreatePartitionedProvider(partitionManager),
    DefaultPartition:    "default",
    EntityRouting: map[string]string{
        "user":   "users",
        "vector": "vectors",
    },
}
```

### Per-Partition Characteristics

- **Independent directories**: Each partition writes to its own directory tree
- **Separate WAL**: Optional per-partition write-ahead log
- **Cache isolation**: Per-partition cache sizing for workload optimization
- **Snapshot granularity**: Partitions can be snapshotted independently

## Raft Shards (Cluster Mode)

In cluster mode, shards provide distributed consensus. Each shard is a Raft group with its own leader election and replicated state.

### Shard Configuration

```yaml
shards:
  - id: 1
    partition: primary
    state_machine: fast

  - id: 2
    partition: vectors
    state_machine: vector

  - id: 3
    partition: primary
    state_machine: minimal
```

### Shard Fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | uint64 | QuicRaft ShardID (must be > 0, unique per cluster) |
| `partition` | string | Name of the partition this shard uses for storage |
| `state_machine` | string | State machine type: `fast`, `vector`, `minimal`, or custom |

### Shard-to-Partition Mapping

Multiple shards can share a partition, or each shard can have its own partition:

```
Shard 1 (fast)    ----\
                       >---- Partition: primary (/mnt/nvme1/primary)
Shard 3 (minimal) ----/

Shard 2 (vector)  --------- Partition: vectors (/mnt/nvme2/vectors)
```

### State Machine Registry

QuicBaaS includes a state machine registry with built-in implementations:

| Name | Description | Use Case |
|------|-------------|----------|
| `fast` | High-performance KV operations | Default, general-purpose |
| `vector` | Vector operations with HNSW indexing | Similarity search, embeddings |
| `minimal` | Simple in-memory with optional persistence | Testing, lightweight workloads |

### Custom State Machine Registration

State machines are managed by the qrdb database engine. Custom state machine registration is performed through the qrdb SDK's admin client. QuicBaaS delegates all Raft consensus and state machine lifecycle management to the qrdb engine.

```yaml
# Use in configuration
shards:
  - id: 4
    partition: custom_data
    state_machine: custom
```

## Configuration Examples

### Standalone Mode

Minimal configuration for single-node deployment:

```yaml
server:
  mode: standalone
  grpc:
    enabled: true
    port: 9090
  rest:
    enabled: true
    port: 8080

storage:
  backend: pebble
  partitions:
    default:
      data_dir: /var/lib/quicbaas/default
```

### Cluster Mode

Production multi-node configuration with multiple partitions and shards:

```yaml
server:
  mode: cluster
  grpc:
    enabled: true
    port: 9090
  rest:
    enabled: true
    port: 8080

storage:
  backend: pebble
  partitions:
    primary:
      data_dir: /mnt/nvme1/primary
      cache_size: 536870912  # 512MB
    vectors:
      data_dir: /mnt/nvme2/vectors
      cache_size: 1073741824  # 1GB

shards:
  - id: 1
    partition: primary
    state_machine: fast
  - id: 2
    partition: vectors
    state_machine: vector

raft:
  node_id: 1
  raft_address: "10.0.1.1:63001"
  listen_address: "10.0.1.1:63001"
  join_addresses:
    - "10.0.1.2:63001"
    - "10.0.1.3:63001"
  mutual_tls: true
  election_rtt: 10
  heartbeat_rtt: 1
  snapshot_entries: 10000
```

### DevCluster Mode

Local multi-node testing configuration:

```yaml
server:
  mode: devcluster
  grpc:
    enabled: true
  rest:
    enabled: true

devcluster:
  node_count: 3
  data_dir: ./dev-data
  base_ports:
    grpc: 9090  # Nodes: 9090, 9091, 9092
    rest: 8080  # Nodes: 8080, 8081, 8082
    quic: 10443
    mcp: 11443
    raft: 63001  # Nodes: 63001, 63002, 63003
  unix_socket_pattern: /tmp/quicbaas-node-{n}.sock
```

DevCluster automatically generates per-node configurations:
- Node 1: gRPC=9090, REST=8080, Raft=63001, Data=./dev-data/node-1
- Node 2: gRPC=9091, REST=8081, Raft=63002, Data=./dev-data/node-2
- Node 3: gRPC=9092, REST=8082, Raft=63003, Data=./dev-data/node-3

## Embedding QuicBaaS

QuicBaaS can be embedded as a library in Go applications, providing direct access to the service layer without network overhead.

### Basic Embedded Usage

```go
package main

import (
    "context"
    "log"

    "github.com/jeremyhahn/go-quicbaas/pkg/config"
    "github.com/jeremyhahn/go-quicbaas/pkg/server"
)

func main() {
    // Use embedded configuration (no network listeners)
    cfg := config.EmbeddedConfig()
    cfg.Storage.DataDir = "./myapp-data"
    cfg.Storage.WALDir = "./myapp-data/wal"

    // Create and start server
    srv, err := server.NewServer(cfg)
    if err != nil {
        log.Fatal(err)
    }

    if err := srv.Start(); err != nil {
        log.Fatal(err)
    }
    defer srv.Shutdown(context.Background())

    // Access the service directly
    svc := srv.Service()

    // Use the service
    ctx := context.Background()
    _, err = svc.Put(ctx, &service.PutRequest{
        Key:   []byte("mykey"),
        Value: []byte("myvalue"),
    })
    if err != nil {
        log.Fatal(err)
    }
}
```

### Embedded with Custom Shard Configuration

```go
package main

import (
    "context"
    "log"

    "github.com/jeremyhahn/go-quicbaas/pkg/config"
    "github.com/jeremyhahn/go-quicbaas/pkg/server"
)

func main() {
    // Configure with shard assignments
    // State machines are managed by the qrdb engine
    cfg := config.EmbeddedConfig()
    cfg.Server.Mode = config.ServerModeCluster
    cfg.Shards = []*config.ShardConfig{
        {
            ID:           1,
            Partition:    "default",
            StateMachine: "fast",
        },
    }

    // Create and start server
    srv, err := server.NewServer(cfg)
    if err != nil {
        log.Fatal(err)
    }

    if err := srv.Start(); err != nil {
        log.Fatal(err)
    }
    defer srv.Shutdown(context.Background())
}
```

## Configuration Reference

### Required Server Mode

QuicBaaS requires an explicit `server.mode` setting:

- `standalone` - Single-node operation without Raft consensus
- `cluster` - Multi-node operation with Raft consensus
- `devcluster` - Local multi-node development cluster

**Cluster Configuration Example:**
```yaml
server:
  mode: cluster

storage:
  backend: pebble
  partitions:
    default:
      data_dir: /var/lib/quicbaas

raft:
  node_id: 1
  raft_address: "10.0.1.1:63001"
```

### Configuration Steps

1. **Set server mode** explicitly via `server.mode`
2. **Choose storage backend** (`memory`, `file`, or `pebble`)
3. **Define partitions** in `storage.partitions` for data isolation
4. **Configure shards** if using multiple state machines (cluster mode)
5. **Set Raft parameters** for cluster/devcluster modes

## API Reference

### Key Packages

| Package | Description |
|---------|-------------|
| `pkg/server/devcluster` | DevCluster node manager |
| `pkg/dao` | Data access objects |
| `pkg/config` | Configuration structures |
| `github.com/jeremyhahn/go-qrdb/sdk/go` | qrdb SDK for storage, Raft, and state machine operations |

Storage partitions, state machine registries, and Raft consensus are managed by the qrdb database engine, accessed via the qrdb SDK.

### DevCluster Manager

```go
// pkg/server/devcluster/manager.go

// Manager manages local multi-node clusters
type Manager struct {
    // ...
}

// NodeInstance represents a local cluster node
type NodeInstance struct {
    NodeID     uint64
    GRPCPort   int
    RESTPort   int
    RaftPort   int
    DataDir    string
    Config     *config.Config
}

// Nodes returns all node instances
func (m *Manager) Nodes() []*NodeInstance
```
