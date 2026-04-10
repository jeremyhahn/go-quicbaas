# Cluster Management

QuicBaaS clusters use multi-Raft consensus for strong consistency, automatic failover, and horizontal scalability. The cluster management system handles node discovery, membership registration, health monitoring, and data distribution.

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
│  │       Membership & Discovery Service           │  │
│  │  • Peer Discovery  • Health Monitoring         │  │
│  │  • Lease Management  • Failure Detection       │  │
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

## Discovery Methods

QuicBaaS supports multiple discovery mechanisms for different deployment environments.

### Static Discovery

Pre-defined peer list for fixed-size clusters.

```yaml
discovery:
  methods:
    - type: static
      peers:
        - "1:node1.example.internal:8001"
        - "2:node2.example.internal:8001"
        - "3:node3.example.internal:8001"

cluster:
  min_quorum: 2
```

Best for: Fixed infrastructure, testing environments.

### DNS Discovery

Dynamic peer discovery using DNS SRV records.

```yaml
discovery:
  methods:
    - type: dns
      srv_record: "_quicbaas._tcp.service.example.internal"
      refresh_interval: 30s
```

Best for: Production deployments, cloud environments with DNS.

### Kubernetes Discovery

Native integration with Kubernetes StatefulSets.

```yaml
discovery:
  methods:
    - type: kubernetes
      namespace: default
      service: quicbaas
      refresh_interval: 30s
```

Best for: Kubernetes deployments, container orchestration.

### Multicast Discovery

LAN-based auto-discovery for local networks.

```yaml
discovery:
  methods:
    - type: multicast
      group: "239.255.77.77"
      port: 63777
      cluster_secret: "optional-psk-for-security"
```

Best for: Development, LAN deployments, edge computing.

See [Discovery Configuration](discovery.md) for detailed setup and PSK authentication.

## Membership Management

### Automatic Registration

Nodes automatically join the cluster on startup:

1. Discover peers via configured discovery method
2. Wait for minimum quorum
3. Register with membership service
4. Join Raft cluster via leader
5. Begin participating in consensus

### Lease-Based Health Monitoring

Each node maintains a lease in the membership registry. Failed lease renewals trigger automatic health checks and eventual removal.

```yaml
membership:
  lease_ttl: 10s              # Registration lifetime
  keep_alive_interval: 5s      # Renewal frequency
  health_check_interval: 15s   # Stale member detection
```

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

See [Operations Guide](operations.md) for detailed procedures.

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

- Raft: 8001 (default, configurable)
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

- [Operations Guide](operations.md) - Day-to-day cluster operations
- [Discovery Configuration](discovery.md) - Discovery methods and PSK authentication
- [CLI Reference - cluster](../cli/cluster.md) - Cluster commands
- [CLI Reference - node](../cli/node.md) - Node management
- [CLI Reference - rebalance](../cli/rebalance.md) - Rebalancing operations

---

**Last Updated:** 2026-01-14
**Version:** 0.3.0
