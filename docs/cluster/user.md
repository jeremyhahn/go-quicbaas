# Cluster — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented usage, in the order tasks are done. Feature folder: `docs/cluster/`.

QuicBaaS clusters use multi-Raft consensus for strong consistency, automatic failover, and horizontal scalability. Cluster formation and Raft membership are owned by go-qrdb (`pkg/cluster`); QuicBaaS consumes a qrdb cluster and adds application-level **service discovery** on top of qrdb's lease + watch + KV primitives.

## Architecture

```
┌──────────────────────────────────────────────────────┐
│                    QuicBaaS Cluster                  │
│                                                      │
│  ┌──────────┐      ┌──────────┐      ┌──────────┐    │
│  │  Node 1  │──────│  Node 2  │──────│  Node 3  │    │
│  │ (Leader) │      │(Follower)│      │(Follower)│    │
│  └────┬─────┘      └────┬─────┘      └────┬─────┘    │
│       │                 │                 │          │
│  ┌────┴─────────────────┴─────────────────┴─────┐    │
│  │         Multi-Raft Consensus Layer           │    │
│  │  • Shard 1  • Shard 2  • Shard N             │    │
│  └───────────────────────┬──────────────────────┘    │
│                          │                           │
│  ┌───────────────────────┴────────────────────────┐  │
│  │   Cluster formation + membership: go-qrdb        │  │
│  │   Service discovery (lease+watch+KV): QuicBaaS   │  │
│  └────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────┘
```

## Cluster Topology

### Sizing Guidelines

Choose cluster size based on fault tolerance requirements:

| Size | Tolerates | Use Case |
|------|-----------|----------|
| 3 nodes | 1 failure | Minimum production, testing |
| 5 nodes | 2 failures | Recommended production |
| 7 nodes | 3 failures | High availability, critical systems |

Formula: Tolerates `(N-1)/2` simultaneous failures. Always use odd number of nodes for proper Raft quorum.

### Node States

- **Healthy** - Operational and participating in consensus
- **Degraded** - Experiencing issues but functional
- **Failed** - Not responding to health checks
- **Evacuating** - Data being drained before removal
- **Removed** - No longer part of cluster

## Discovery

There are two distinct concerns, with distinct homes:

- **Peer discovery** — finding Raft peers to form or join a cluster
  (static / multicast / DNS SRV / token). This lives in **go-qrdb**
  (`pkg/discovery` + `pkg/cluster`), the layer that owns the cluster. QuicBaaS
  embeds the go-qrdb node and does not perform peer discovery itself. See go-qrdb's
  `docs/discovery.md` and `docs/12-configuration.md` for configuration.
- **Service discovery** — registering service instances and resolving them by
  name at runtime. QuicBaaS provides this via the registry, built on qrdb's
  lease + watch + KV primitives. See [the service registry](../service-registry/user.md).

## Cluster Membership

Raft cluster formation and membership (peer discovery, quorum validation,
joining the Raft group, and lease-driven dynamic add/remove) are owned by
**go-qrdb** (`pkg/cluster`). QuicBaaS consumes a qrdb cluster rather than
forming one. For peer discovery, bootstrap, and the opt-in dynamic membership
manager, see go-qrdb's `docs/discovery.md`.

For application-level **service** membership (instances of a named service
joining and leaving at runtime), use the QuicBaaS [service registry](../service-registry/user.md),
which is lease-based: each instance holds a lease and is removed automatically
when its lease expires.

## Core Operations

View cluster status:

```bash
quicbaas cluster status
quicbaas node list
```

Add a node:

```bash
quicbaas node add node-4 10.0.0.4:50051
```

Remove a node:

```bash
quicbaas node remove node-3
```

Replace a failed node:

```bash
quicbaas node replace node-2 node-5 10.0.0.5:50051
```

Rebalance data across shards:

```bash
quicbaas rebalance start
quicbaas rebalance status
```

Upgrading a node is done by replacing its binary or image and restarting it.
`quicbaas upgrade start` only records an upgrade job on the server and does
not touch any node; see [cli.md](cli.md#quicbaas-upgrade).

See [Operations Guide](engineering.md) for detailed procedures.

## Quick Start

QuicBaaS provides ready-to-use configurations in the `configs/` directory. Choose based on your needs:

| Configuration | Use Case |
|---------------|----------|
| `configs/memory.yaml` | Testing, ephemeral data |
| `configs/pebble-local.yaml` | Single-node with persistence |
| `configs/raft-single-node.yaml` | Raft testing, CI/CD |
| `configs/raft-3-node/` | Production HA cluster |

### Single-Node Development

For local development with persistence:

```bash
# Native
quicbaasd --config configs/pebble-local.yaml

# Docker
docker compose -f configs/docker/docker-compose.pebble-local.yaml up
```

### 3-Node Cluster

The fastest way to start a 3-node cluster is with Docker Compose:

```bash
docker compose -f configs/docker/docker-compose.raft-3-node.yaml up
```

This starts three nodes with static peer discovery:
- **Node 1**: REST http://localhost:8081, gRPC localhost:9091
- **Node 2**: REST http://localhost:8082, gRPC localhost:9092
- **Node 3**: REST http://localhost:8083, gRPC localhost:9093

To run natively, start each node with its configuration:

```bash
# Terminal 1
quicbaasd --config configs/raft-3-node/node1.yaml

# Terminal 2
quicbaasd --config configs/raft-3-node/node2.yaml

# Terminal 3
quicbaasd --config configs/raft-3-node/node3.yaml
```

Verify cluster formation:

```bash
curl http://localhost:8081/v1/cluster/members
quicbaas cluster status
```

See [configs/README.md](../../configs/README.md) for all available configurations.

## Best Practices

### Production Deployment

1. Use odd number of nodes (3, 5, 7) for Raft quorum
2. Enable DNS discovery for dynamic environments
3. Configure health checks and monitoring
4. Use persistent storage (StatefulSet volumes)
5. Set resource limits to prevent exhaustion
6. Enable TLS for all cluster communication
7. Implement regular backup snapshots

### Network Requirements

Required ports:

- Raft: 63000 (default; see [Raft Addresses](architecture.md#raft-addresses))
- gRPC: 50051 (default, configurable)
- HTTP: 18080 (default, configurable)
- Metrics: 9090+ (per node)

### Performance Tuning

Low latency networks:

```yaml
cluster:
  election_timeout: 500ms
  heartbeat_interval: 50ms
```

Large clusters:

```yaml
membership:
  keep_alive_interval: 10s
  health_check_interval: 30s
```

High write throughput:

```yaml
cluster:
  batch_size: 1000
  batch_timeout: 10ms
```

## Monitoring

Key metrics to monitor:

- Cluster health status
- Leader stability
- Node availability
- Raft replication lag
- Data distribution balance
- Network latency
- Disk usage

## Troubleshooting

Common issues and solutions:

- **Nodes not discovering** - Verify network connectivity, DNS records, or static peer configuration
- **Quorum not reached** - Ensure minimum healthy nodes exist, check connection logs
- **Split-brain** - Check for network partitions, verify single leader
- **Slow elections** - Reduce election timeout, check network latency
- **High memory** - Tune PebbleDB cache and Raft snapshot settings

## See Also

- [Operations Guide](engineering.md) - Day-to-day cluster operations
- [Discovery Configuration](architecture.md) - Discovery methods and PSK authentication
- CLI Reference - cluster - Cluster commands
- [CLI Reference - node](cli.md) - Node management
- [CLI Reference - rebalance](cli.md) - Rebalancing operations

---

**Last Updated:** 2026-01-14
**Version:** 0.3.0
