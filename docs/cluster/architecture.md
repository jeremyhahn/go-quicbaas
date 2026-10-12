# Cluster — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design, component boundaries and data flow. Feature folder: `docs/cluster/`.

QuicBaaS uses [go-qrdb](https://github.com/jeremyhahn/go-qrdb) as its
embedded Raft engine. In clustered mode each QuicBaaS node embeds a real
qrdb `*node.Node` (via `sdk/go/node.NewClusteredNode`) that starts a
[QuicRaft](https://github.com/jeremyhahn/go-quicraft) host internally.
QuicBaaS reimplements no Raft logic.

## Architecture: qrdb as the Raft engine

```
QuicBaaS process
  └── pkg/server.Server (mode = "cluster")
        └── initClusteredQRDBNode()
              └── qrdb/sdk/go/node.NewClusteredNode(cfg, members, opts)
                    ├── quicraft.Host  (QUIC/mTLS transport, multi-group Raft)
                    ├── MVCC state machine factory (store.NewMVCCCreateFuncWith)
                    ├── lifecycle.BuildServices (KV/MVCC/TS/Vector/…)
                    └── server.New (qrdb multi-protocol server, embedded transport)
```

QuicBaaS uses qrdb's own discovery (`config.Discovery`) for peer resolution.
It does not build a separate discovery layer. Peer configuration flows from
quicbaas `config.RaftConfig` → `buildQRDBConfig(cfg, clusterMode=true)` →
qrdb `config.DiscoveryConfig`.

## Server Modes

| Mode | `server.mode` | Engine |
|------|--------------|--------|
| Standalone | `standalone` | `qrdb.NewStandaloneNode` — LocalBackend, no Raft |
| Clustered | `cluster` | `qrdb.NewClusteredNode` — quicraft host, real Raft |
| DevCluster | `devcluster` | local multi-node dev cluster (3-node in-process) |

## Clustered Mode Bootstrap

```go
// QuicBaaS wires clustered mode like this (simplified):
qcfg := buildQRDBConfig(cfg, true) // Raft + Discovery populated
members := buildClusterMembers(cfg) // map[replicaID]raftAddress
serverOpts := XKMSCryptoOptions(composeCfg) // hardware crypto
nodeOpts := []node.NodeOption{
    node.WithLogger(logger),
    node.WithRaftMTLS(mtls), // PEM cert material for inter-node mTLS
}
qrdbNode = node.NewClusteredNode(ctx, qcfg, members, serverOpts, nodeOpts...)
```

## Peer Configuration

Peers are configured in quicbaas config as `raft.join_addresses` in
`"replicaID=host:port"` format. Example for a 3-node cluster:

```yaml
raft:
  node_id: 1
  raft_address: "10.0.0.1:63101"
  join_addresses:
    - "1=10.0.0.1:63101"
    - "2=10.0.0.2:63101"
    - "3=10.0.0.3:63101"

discovery:
  type: "static"   # or "multicast", "dns", "token"
```

Every multi-node replica must also name the **identity** of the other
replicas in `raft.member_identities` (`QUICBAAS_RAFT_MEMBER_IDENTITIES`,
comma-separated): go-quicraft admits a member by its identity or not at all.
An identity is `spki:` plus the hex SHA-256 of the replica certificate's
SubjectPublicKeyInfo, as `qrdbd node-identity --cert <node.crt>` prints it. The
local replica's own identity is derived from its certificate and may be omitted.

```yaml
raft:
  member_identities:
    - "2=spki:<sha256-hex>"
    - "3=spki:<sha256-hex>"
```

The integration stack issues its certificates at run time, so
`test/integration/node-entrypoint.sh` derives the variable from the peers'
`node<id>.crt` files on the shared certs volume before starting the daemon.

When no `id=`-prefixed addresses are present, QuicBaaS falls back to a
single-node clustered mode (`WithoutTransport`) which is suitable for
development and testing.

## mTLS Configuration

Inter-node communication uses mandatory mTLS (quicraft enforces it for all
multi-node clusters). Provide PEM certificate paths in the quicbaas Raft
config:

```yaml
raft:
  tls_cert_file: /etc/quicbaas/raft/node.crt
  tls_key_file:  /etc/quicbaas/raft/node.key
  tls_ca_file:   /etc/quicbaas/raft/ca.crt
```

All cluster nodes must trust the same CA. The CA used for Raft mTLS is
typically the same CA used for API TLS (issued via xkmsCA). Single-node
development clusters (no join_addresses with id= prefix) skip mTLS
entirely — QuicRaft's `WithoutTransport` option bypasses network I/O.

## Discovery Methods

QuicBaaS maps its discovery config to qrdb's discovery engine:

| quicbaas `discovery.type` | qrdb `discovery.method` | Use case |
|--------------------------|------------------------|----------|
| `static` | `static` | Fixed peer list (production) |
| `multicast` | `multicast` | Zero-config LAN clusters |
| `dns` | `dns` | DNS SRV-based discovery |
| `token` | `token` | Token-service discovery |
| `kubernetes`, `consul` | `static` | External orchestrator uses static IPs |

## Multi-Raft Architecture

### Why Multi-Raft?

Single Raft groups have limitations: all writes through one leader, single bottleneck, and failures affect the entire database.

Multi-raft provides: horizontal scaling across multiple leaders, independent failure domains, and different configurations per shard.

### Shard Assignment

```go
// Route entity to shard using consistent hashing
func (r *Router) Route(entityType string, id uint64) uint64 {
    key := fmt.Sprintf("%s:%d", entityType, id)
    hash := xxhash.Sum64([]byte(key))
    return (hash % r.numShards) + 1
}
```

## Phase F: 3-Node Integration Tests

Full 3-node cluster replication, failover, at-rest encryption, and dynamic
membership integration tests are planned for Phase F. Skeleton tests live in
`test/integration/cluster/qrdb_cluster_test.go`. They require the
`docker-compose.cluster.yml` environment.

## Legacy (pre-Phase C): deleted paths

Before Phase C, clustered mode used a no-op `standaloneRaftNode` and a
`baasTransport`-backed `ShardedClient` — no real Raft consensus ever ran.
Phase C replaced that path with `initClusteredQRDBNode`. The
`baasTransport*.go` files still compile (they are unreachable in cluster
mode) and will be deleted in Phase E.

## Original QuicRaft Integration Reference (pre-Phase C)

The original quicraft API notes are preserved below for the Phase F
docker-compose test setup that will drive multi-node test clusters directly
via the quicraft host API.

### Node Host Setup (Phase F reference)

```go
import "github.com/jeremyhahn/go-quicraft"

// Create NodeHost
nhc := quicraft.NodeHostConfig{
    NodeHostDir:    "/var/lib/quicbaas/raft",
    RTTMillisecond: 200,
    RaftAddress:    "localhost:63001",
    MutualTLS:      true,
    NotifyCommit:   true,
}

nh, err := quicraft.NewNodeHost(nhc)
if err != nil {
    return err
}
```

### Starting a Raft Group (Phase F reference)

```go
// Raft configuration
rc := quicraft.Config{
    NodeID:             1,
    ShardID:            1,
    ElectionRTT:        10,
    HeartbeatRTT:       1,
    CheckQuorum:        true,
    SnapshotEntries:    10000,
    CompactionOverhead: 5000,
}

// Initial members
members := map[uint64]string{
    1: "localhost:63001",
    2: "localhost:63002",
    3: "localhost:63003",
}

// Start Raft cluster
err := nh.StartCluster(members, false, NewStateMachine, rc)
if err != nil {
    return err
}
```

## Consistency Levels

### Quorum Reads (Linearizable via SyncRead)

Quorum reads provide linearizability - clients always see the most recent write:

```go
// SyncRead performs a quorum read
result, err := nh.SyncRead(ctx, shardID, query)
```

Process: leader verifies leadership, reads from state machine, returns result.

Guarantees: always returns latest committed value, no stale reads, requires majority of nodes available.

Use cases: critical reads, financial transactions, inventory management.

### LeaseRead (Linearizable, Lower Latency)

LeaseRead provides linearizability using leader lease for reduced latency:

```go
// LeaseRead uses leader lease for faster linearizable reads
result, err := nh.LeaseRead(ctx, shardID, query)
```

Process: leader checks lease validity, reads from state machine, returns result.

Guarantees: linearizable with clock-based lease, lower latency than SyncRead (~1-3ms), requires synchronized clocks.

Use cases: high-throughput linearizable reads where clock sync is available.

### Local Reads (Stale)

Local reads are much faster but may return stale data:

```go
// StaleRead performs a local read
result, err := nh.StaleRead(shardID, query)
```

Process: read directly from local state machine without leadership verification.

Guarantees: very low latency (<1ms), may return stale data, works during network partitions.

Use cases: read-heavy workloads, analytics and reporting, non-critical data access.

## Write Path

All writes go through Raft consensus. QuicRaft manages sessions internally:

```go
// Propose a write (no manual session management needed)
result, err := nh.SyncPropose(ctx, shardID, data)
```

Process: client proposes to leader, leader replicates to followers, majority acknowledgment triggers commit, state machine applies write, result returned to client.

With NotifyCommit enabled, WAL-durable early notification (~1-2ms) is available via dual-channel RequestState before the state machine applies the entry.

**Timeline:**
```
Client  Leader  Follower1  Follower2  StateMachine
  |       |         |          |          |
  |------>|         |          |          |  1. Propose
  |       |-------->|          |          |  2. Replicate
  |       |---------|--------->|          |  2. Replicate
  |       |<--------|          |          |  3. Ack
  |       |<--------|----------|          |  3. Ack
  |       |                    |          |  4. Majority reached
  |       |--------------------------->   |  5. Apply
  |       |<---------------------------|   |  6. Result
  |<------|                                |  7. Response
```

## Leader Election

Raft automatically elects a leader when:
- Cluster starts
- Current leader fails
- Network partition heals

**Election Process:**
1. Follower times out waiting for leader heartbeat
2. Follower becomes candidate and requests votes
3. Other nodes vote for candidate
4. Candidate with majority votes becomes leader
5. New leader sends heartbeats to establish authority

**Configuration:**
```go
rc := quicraft.Config{
    ElectionRTT:  10,  // 10 RTT intervals before election
    HeartbeatRTT: 1,   // Heartbeat every 1 RTT interval
}
```

**RTT (Round-Trip Time):**
- Base time unit for Raft timeouts
- Configured in NodeHostConfig
- Default: 200ms
- Election timeout: ElectionRTT x RTTMillisecond = 2 seconds
- Heartbeat interval: HeartbeatRTT x RTTMillisecond = 200ms

## Log Replication

### Log Structure

Each Raft log entry contains:
```go
type Entry struct {
    Index uint64  // Position in log
    Term  uint64  // Election term when created
    Cmd   []byte  // Serialized command
}
```

### Log Compaction (Snapshots)

Raft logs grow unbounded without compaction. Snapshots provide:
- **Space savings:** Delete old log entries
- **Faster recovery:** Restore from snapshot vs replaying log
- **Bounded memory:** Limit in-memory log size

**Configuration:**
```go
rc := quicraft.Config{
    SnapshotEntries:    10000,  // Snapshot after 10K entries
    CompactionOverhead: 5000,   // Keep 5K entries after snapshot
}
```

**Snapshot Process:**
1. State machine reaches entry threshold
2. State machine serializes current state
3. Raft saves snapshot to disk
4. Old log entries deleted
5. Snapshot replicated to lagging followers

## Membership Changes

Add or remove nodes from Raft cluster:

```go
// Add node
err := nh.SyncRequestAddNode(ctx, shardID, nodeID, address, 0)

// Remove node
err := nh.SyncRequestDeleteNode(ctx, shardID, nodeID, 0)
```

**Safety:**
- Changes processed through Raft consensus
- No split-brain scenarios
- Automatic configuration updates

**Process:**
1. Configuration change proposed to leader
2. Replicated to majority
3. New configuration activated
4. New node catches up (if adding)
5. Old node stops (if removing)

## Failure Scenarios

### Node Failure

**Impact:**
- Cluster remains available if quorum exists
- Reads/writes continue normally
- Failed node marked as unavailable

**Recovery:**
1. Failed node restarts
2. Rejoins cluster
3. Catches up from log or snapshot
4. Resumes normal operation

**Timeline:**
- Detection: ~2 seconds (election timeout)
- Recovery: <30 seconds (typical)

### Network Partition

**Scenario:** Cluster split into two groups

**Behavior:**
- Partition with majority continues operating
- Minority partition becomes read-only
- Writes to minority fail (no quorum)

**Recovery:**
1. Network partition heals
2. Minority nodes catch up from leader
3. Conflicting entries rolled back
4. Normal operation resumes

### Leader Failure

**Process:**
1. Followers detect missing heartbeats
2. Election triggered (2 seconds)
3. New leader elected
4. New leader establishes authority
5. Normal operation resumes

**Downtime:**
- Typical: 2-3 seconds
- Worst case: 4-6 seconds (multiple rounds)

## Performance Tuning

### RTT Configuration

```go
nhc := quicraft.NodeHostConfig{
    RTTMillisecond: 200,  // Adjust based on network
}
```

**Low-latency network (same datacenter):**
```go
RTTMillisecond: 50   // 50ms base RTT
ElectionRTT: 20      // 1 second election timeout
HeartbeatRTT: 2      // 100ms heartbeat
```

**High-latency network (cross-region):**
```go
RTTMillisecond: 500  // 500ms base RTT
ElectionRTT: 10      // 5 second election timeout
HeartbeatRTT: 1      // 500ms heartbeat
```

### Snapshot Configuration

```go
rc := quicraft.Config{
    SnapshotEntries:    10000,   // Larger = less frequent snapshots
    CompactionOverhead: 5000,    // Larger = more log entries kept
}
```

**High write throughput:**
```go
SnapshotEntries: 50000     // Less frequent snapshots
CompactionOverhead: 10000  // More log history
```

**Low memory:**
```go
SnapshotEntries: 5000      // More frequent snapshots
CompactionOverhead: 1000   // Less log history
```

### Batch Operations

```go
// Use batch API for higher throughput
entries := make([][]byte, 100)
for i := 0; i < 100; i++ {
    entries[i] = createEntry(i)
}

results := nh.SyncProposeBatch(ctx, shardID, entries)
```

**Benefits:**
- Amortize network overhead
- Better CPU utilization
- Higher throughput (10x improvement)

## Monitoring

### Raft Metrics

```go
type RaftMetrics struct {
    // Leadership
    IsLeader     bool
    LeaderID     uint64
    Term         uint64

    // Log
    CommittedIndex uint64
    AppliedIndex   uint64
    SnapshotIndex  uint64

    // Performance
    ProposalsMade  uint64
    ProposalsDropped uint64
    ProposalsDuration time.Duration
}
```

**Key Metrics to Monitor:**
- **Leader changes:** Frequent changes indicate instability
- **Proposal drop rate:** High rate indicates overload
- **Log gap:** Large gap between committed and applied indicates slow state machine
- **Snapshot frequency:** Too frequent indicates undersized configuration

### Health Checks

QuicRaft provides built-in health checks and graceful drain support:

```go
// Check if cluster is healthy
func (n *Node) IsHealthy(shardID uint64) bool {
    // Has leader
    leader := n.nh.GetLeaderID(shardID)
    if leader == 0 {
        return false
    }

    // Can propose (has quorum)
    ctx, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()

    _, err := n.nh.SyncPropose(ctx, shardID, []byte("healthcheck"))
    return err == nil
}
```

## Best Practices

### Cluster Sizing

**3-node cluster:**
- Tolerates 1 failure
- Good for development/small deployments
- Lower overhead

**5-node cluster:**
- Tolerates 2 failures
- Better for production
- Higher availability

**7+ nodes:**
- Rarely needed
- Higher overhead
- Consider multi-raft instead

### Write Patterns

**DO:**
- Batch related writes
- Use async propose when possible
- Set appropriate timeouts

**DON'T:**
- Make writes from multiple clients to same key
- Propose very large entries (>1MB)
- Ignore proposal failures

### Read Patterns

**DO:**
- Use local reads for non-critical data
- Use LeaseRead for high-throughput linearizable reads
- Use SyncRead for critical data requiring strictest consistency
- Cache frequently accessed data

**DON'T:**
- Use SyncRead for everything
- Make reads without timeouts
- Ignore read failures

## Troubleshooting

### Cluster Won't Start

**Check:**
- All nodes have same ShardID
- All nodes have unique NodeIDs
- All nodes have correct member addresses
- Data directories are writable
- mTLS certificates are valid and trusted

### Slow Writes

**Possible causes:**
- Network latency too high
- Disk I/O bottleneck
- State machine too slow
- RTT configuration too large

**Solutions:**
- Reduce RTT settings
- Use faster disks (SSD)
- Optimize state machine
- Enable batching

### Frequent Leader Changes

**Possible causes:**
- Network instability
- Node overload
- RTT settings too aggressive

**Solutions:**
- Increase ElectionRTT
- Reduce load
- Check network stability
- Add more resources

## References

- [Raft Paper](https://raft.github.io/raft.pdf) - Original Raft specification
- [QuicRaft Documentation](https://github.com/jeremyhahn/go-quicraft) - Library documentation
- [Raft Visualization](http://thesecretlivesofdata.com/raft/) - Interactive Raft demo

---

**Last Updated:** 2026-02-23


---

## Cluster Bootstrap

Automatic cluster discovery and bootstrap for QuicBaaS.

### Overview

QuicBaaS provides automatic cluster discovery eliminating manual peer configuration. The system supports multiple discovery methods for different deployment environments.

### Discovery Methods

| Method | Complexity | Use Case | Dynamic |
|--------|-----------|----------|---------|
| Static | Low | Fixed clusters, testing | No |
| DNS | Medium | Production with DNS | Yes |
| Kubernetes | Medium | K8s deployments | Yes |
| Multicast | Low | Development, LAN | Yes |

### Quick Start

#### 3-Node Cluster with Static Discovery

Configuration:

```yaml
discovery:
  methods:
    - type: static
      peers:
        - "1:node1:8001"
        - "2:node2:8001"
        - "3:node3:8001"

cluster:
  min_quorum: 2

membership:
  lease_ttl: 10s
  keep_alive_interval: 5s
  health_check_interval: 15s
```

Start nodes:

```bash
quicbaas --config=config.yaml --node-id=1 --address=node1:8001
quicbaas --config=config.yaml --node-id=2 --address=node2:8001
quicbaas --config=config.yaml --node-id=3 --address=node3:8001
```

Verify:

```bash
curl http://node1:18080/v1/cluster/members
```

### Static Discovery

Pre-defined peer list for fixed clusters.

```yaml
discovery:
  methods:
    - type: static
      peers:
        - "1:203.0.113.100:8001"
        - "2:203.0.113.101:8001"
        - "3:203.0.113.102:8001"
```

#### Adding a Node

Update configuration on all nodes:

```yaml
peers:
  - "1:node1:8001"
  - "2:node2:8001"
  - "3:node3:8001"
  - "4:node4:8001"  # NEW
```

Reload or restart existing nodes:

```bash
systemctl reload quicbaas
```

Start new node:

```bash
quicbaas --config=config.yaml --node-id=4 --address=node4:8001
```

### DNS Discovery

Dynamic peer discovery using DNS SRV records.

```yaml
discovery:
  methods:
    - type: dns
      srv_record: "_quicbaas._tcp.service.consul"
      refresh_interval: 30s
```

#### Consul Integration

Register service in Consul:

```json
{
  "service": {
    "name": "quicbaas",
    "tags": ["raft", "database"],
    "address": "node1.example.com",
    "port": 8001,
    "check": {
      "http": "http://node1.example.com:18080/health",
      "interval": "10s"
    }
  }
}
```

Configure QuicBaaS:

```yaml
discovery:
  methods:
    - type: dns
      srv_record: "_quicbaas._tcp.service.consul"
```

#### AWS Route 53

Create SRV records:

```
_quicbaas._tcp.cluster.internal. SRV 10 10 8001 node1.cluster.internal.
_quicbaas._tcp.cluster.internal. SRV 10 10 8001 node2.cluster.internal.
_quicbaas._tcp.cluster.internal. SRV 10 10 8001 node3.cluster.internal.
```

Configure:

```yaml
discovery:
  methods:
    - type: dns
      srv_record: "_quicbaas._tcp.cluster.internal"
      refresh_interval: 60s
```

### Kubernetes Discovery

Native Kubernetes deployment using StatefulSets.

#### ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: quicbaas-config
  namespace: quicbaas
data:
  config.yaml: |
    discovery:
      methods:
        - type: kubernetes
          namespace: quicbaas
          service: quicbaas
          refresh_interval: 30s
    cluster:
      min_quorum: 2
    membership:
      lease_ttl: 10s
      keep_alive_interval: 5s
```

#### Headless Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: quicbaas
  namespace: quicbaas
spec:
  clusterIP: None
  selector:
    app: quicbaas
  ports:
    - name: raft
      port: 8001
    - name: grpc
      port: 50051
    - name: http
      port: 18080
```

#### StatefulSet

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: quicbaas
  namespace: quicbaas
spec:
  serviceName: quicbaas
  replicas: 3
  selector:
    matchLabels:
      app: quicbaas
  template:
    metadata:
      labels:
        app: quicbaas
    spec:
      containers:
      - name: quicbaas
        image: quicbaas:latest
        ports:
          - containerPort: 8001
            name: raft
          - containerPort: 50051
            name: grpc
          - containerPort: 18080
            name: http
        volumeMounts:
          - name: config
            mountPath: /etc/quicbaas
          - name: data
            mountPath: /var/lib/quicbaas
        livenessProbe:
          httpGet:
            path: /health
            port: 18080
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 18080
          initialDelaySeconds: 10
          periodSeconds: 5
      volumes:
        - name: config
          configMap:
            name: quicbaas-config
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: 10Gi
```

Deploy:

```bash
kubectl apply -f quicbaas-configmap.yaml
kubectl apply -f quicbaas-service.yaml
kubectl apply -f quicbaas-statefulset.yaml
kubectl wait --for=condition=ready pod -l app=quicbaas -n quicbaas --timeout=300s
```

#### Scaling

```bash
kubectl scale statefulset quicbaas -n quicbaas --replicas=5
kubectl scale statefulset quicbaas -n quicbaas --replicas=3
```

Important: Always maintain quorum.

#### Production Recommendations

Resource limits:

```yaml
resources:
  requests:
    memory: "512Mi"
    cpu: "500m"
  limits:
    memory: "2Gi"
    cpu: "2000m"
```

Pod anti-affinity:

```yaml
affinity:
  podAntiAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      - labelSelector:
          matchExpressions:
            - key: app
              operator: In
              values: [quicbaas]
        topologyKey: kubernetes.io/hostname
```

### Troubleshooting

#### Discovery Fails

Symptoms:

```
WARN  Discovery via dns failed: no SRV records found
ERROR All discovery methods failed
```

Check:

```bash
dig +short SRV _quicbaas._tcp.example.com
nc -zv node1 8001
iptables -L -n | grep 8001
```

Solutions:
1. Verify DNS records exist
2. Check network connectivity
3. Verify firewall rules
4. Ensure min_quorum is achievable
5. Check configuration syntax

#### Quorum Not Reached

Symptoms:

```
INFO  Found 2 peers, waiting for 3 to achieve quorum
```

Check:

```bash
for i in {1..3}; do
  nc -zv node$i 8001 && echo "node$i: OK" || echo "node$i: FAIL"
done
```

Solutions:
1. Start additional nodes
2. Check if nodes are running
3. Verify discovery finds all nodes
4. Check node addresses are correct

#### Member Registration Fails

Symptoms:

```
ERROR Failed to register member: connection refused
```

Check:

```bash
curl http://node1:18080/health
curl http://node1:18080/v1/cluster/status
journalctl -u quicbaas -f
```

Solutions:
1. Ensure storage backend is healthy
2. Check cluster has elected a leader
3. Verify disk space available
4. Restart node if transient failure

#### Split-Brain

Symptoms:

```
ERROR Multiple leaders detected
```

Check:

```bash
for i in {1..3}; do
  curl http://node$i:18080/v1/cluster/leader
done
```

Solutions:
1. DO NOT restart all nodes at once
2. Check network connectivity between nodes
3. Ensure no network partitions exist

### Recovery Procedures

#### Single Node Failure

```bash
curl http://node1:18080/v1/cluster/members
systemctl restart quicbaas
journalctl -u quicbaas -f
```

#### Quorum Lost

```bash
# 1. Identify node with most recent data
curl http://node1:18080/v1/cluster/status | jq .last_applied

# 2. Start node with highest last_applied FIRST
systemctl start quicbaas

# 3. Wait for leader election
curl http://node1:18080/v1/cluster/leader

# 4. Start remaining nodes ONE BY ONE
systemctl start quicbaas
sleep 30
```

#### Network Partition Healed

```bash
# Verify connectivity
ping node1
ping node2

# Watch for automatic convergence
journalctl -u quicbaas -f

# Verify single leader
for node in node1 node2 node3; do
  curl -s http://$node:18080/v1/cluster/leader
done
```

### Best Practices

#### Production Deployment

- Use odd number of nodes (3, 5, 7)
- Use DNS discovery for dynamic environments
- Configure health checks and monitoring
- Use persistent storage volumes
- Set resource limits
- Enable TLS
- Implement regular backups

#### Cluster Sizing

- 3 nodes: Tolerates 1 failure (minimum production)
- 5 nodes: Tolerates 2 failures (recommended)
- 7 nodes: Tolerates 3 failures (high availability)

Formula: Tolerates `(N-1)/2` failures

#### Performance Tuning

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

#### Security

```yaml
security:
  tls:
    enabled: true
    cert_file: /etc/quicbaas/tls/server.crt
    key_file: /etc/quicbaas/tls/server.key
    ca_file: /etc/quicbaas/tls/ca.crt
```

### See Also

- [Cluster Management](user.md) - Cluster architecture
- [Operations Guide](engineering.md) - Day-to-day operations
- [Discovery Configuration](architecture.md) - Discovery methods

---

**Last Updated:** 2025-12-19


---

## Service Discovery

Service discovery — registering application service instances under a lease and
resolving them by name — is the **service registry**, documented in its own
feature folder: [docs/service-registry/](../service-registry/architecture.md).
It is served on every transport and reached from the Go SDK as
`client.Registry()` (or `client.ClusterMgmt().Registry()`).

> **Peer discovery has moved.** Discovering *Raft peers* to form or join a
> cluster (static / multicast / DNS SRV / token) is no longer a QuicBaaS
> concern — it lives in **go-qrdb** (`pkg/discovery` + `pkg/cluster`), the layer
> that owns the cluster. QuicBaaS operates as a qrdb client. See go-qrdb's
> `docs/discovery.md`.
