# Cluster — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/cluster/`.

Day-to-day operational procedures for managing QuicBaaS clusters in production.

## Health Monitoring

### Cluster Status

Check overall cluster health:

```bash
quicbaas cluster status
quicbaas cluster status --output json
quicbaas node list
```

Healthy cluster indicators:
- Single elected leader
- All nodes in "ready" state
- No network partitions
- Replication lag < 100ms

### Health Endpoints

```bash
curl http://node1:18080/health/live     # Liveness
curl http://node1:18080/health/ready    # Readiness
curl http://node1:18080/v1/cluster/members
```

### Key Metrics

```promql
# Cluster health
quicbaas_cluster_health_status

# Leader status
quicbaas_raft_leader_id

# Node availability
up{job="quicbaas"}

# Replication lag
quicbaas_raft_replication_lag_seconds
```

## Scaling Operations

### Adding Nodes

Static discovery:

```bash
# 1. Update config.yaml on all nodes with new peer
# 2. Restart existing nodes or reload configuration
systemctl reload quicbaas

# 3. Start new node
quicbaas --config=config.yaml --node-id=4 --address=node4:8001

# 4. Verify
quicbaas cluster status
```

Dynamic discovery (DNS/Kubernetes):

```bash
quicbaas node add node-4 10.0.0.4:50051
quicbaas node list
```

Kubernetes:

```bash
kubectl scale statefulset quicbaas --replicas=5
kubectl get pods -l app=quicbaas
kubectl exec quicbaas-0 -- quicbaas cluster status
```

### Removing Nodes

Graceful removal with data evacuation:

```bash
quicbaas node remove node-3
quicbaas node list
```

Force removal for failed nodes:

```bash
quicbaas node remove node-3 --verify-data=false
```

Kubernetes scale down (maintain quorum):

```bash
kubectl scale statefulset quicbaas --replicas=3
kubectl delete pvc data-quicbaas-3 data-quicbaas-4
```

Warning: Never scale below quorum. For 5-node cluster, minimum is 3 nodes.

### Node Replacement

```bash
quicbaas node replace node-2 node-5 10.0.0.5:50051 \
  --verify-data \
  --auto-replace \
  --evacuation-timeout 45m
```

## Maintenance Procedures

### Rolling Upgrades

```bash
#!/bin/bash
for node in node1 node2 node3; do
  echo "Upgrading $node..."

  ssh $node "systemctl stop quicbaas"
  scp quicbaas $node:/usr/local/bin/
  ssh $node "systemctl start quicbaas"

  until curl -sf http://$node:18080/health/ready; do
    sleep 5
  done

  sleep 10
  echo "$node upgraded successfully"
done
```

Important:
- Upgrade one node at a time
- Never upgrade majority simultaneously
- Verify health before proceeding to next node

### Configuration Changes

```bash
vim /etc/quicbaas/config.yaml
quicbaas config validate
systemctl reload quicbaas
```

### Data Rebalancing

```bash
quicbaas rebalance plan
quicbaas rebalance start --follow
quicbaas rebalance status
```

Custom settings:

```bash
quicbaas rebalance start \
  --target-imbalance 0.05 \
  --max-concurrent 2 \
  --rate-limit 10240
```

When to rebalance:
- After adding/removing nodes
- Significant data growth
- Uneven shard distribution
- Performance degradation

### Snapshot Management

```bash
# Manual snapshot
curl -X POST http://node1:18080/v1/cluster/snapshot

# List snapshots
ls /var/lib/quicbaas/raft/snapshots/

# Verify integrity
quicbaas snapshot verify /var/lib/quicbaas/raft/snapshots/latest
```

Automated snapshots (cron):

```bash
0 2 * * * curl -X POST http://localhost:18080/v1/cluster/snapshot
```

## Recovery Procedures

### Single Node Failure

When one node fails but quorum remains:

```bash
# Check cluster status
quicbaas cluster status

# Restart failed node
systemctl restart quicbaas

# Monitor rejoin
journalctl -u quicbaas -f

# Verify cluster restored
quicbaas cluster status
```

### Quorum Lost

Critical: Multiple nodes failed, no quorum.

```bash
# 1. Identify node with most recent data
curl http://node1:18080/v1/cluster/status | jq .last_applied
curl http://node2:18080/v1/cluster/status | jq .last_applied

# 2. Start node with highest last_applied FIRST
systemctl start quicbaas

# 3. Wait for leader election
curl http://node1:18080/v1/cluster/leader

# 4. Start remaining nodes ONE BY ONE
systemctl start quicbaas
sleep 30

# 5. Verify cluster reformed
quicbaas cluster status
```

### Network Partition Healed

```bash
# Verify connectivity
for node in node1 node2 node3; do
  ping -c 3 $node
  nc -zv $node 8001
done

# Watch for automatic convergence
journalctl -u quicbaas -f

# Verify single leader across all nodes
for node in node1 node2 node3; do
  echo -n "$node: "
  curl -s http://$node:18080/v1/cluster/leader
done
```

Raft automatically:
- Detects partition healed
- Steps down duplicate leader
- Re-elects single leader
- Replicates missing logs

### Data Corruption

```bash
# Stop affected node
systemctl stop quicbaas

# Backup corrupted data
cp -r /var/lib/quicbaas/data /backup/corrupted-$(date +%Y%m%d)

# Remove corrupted data
rm -rf /var/lib/quicbaas/data/*

# Restart node (will sync from cluster)
systemctl start quicbaas

# Monitor resync
journalctl -u quicbaas -f

# Verify integrity
quicbaas data verify
```

### Split-Brain Recovery

```bash
# Identify partition
for node in node1 node2 node3; do
  echo "$node: leader $(curl -s http://$node:18080/v1/cluster/leader)"
done

# Check network connectivity
traceroute node1

# Fix network partition
# Restore connectivity between nodes

# Wait for automatic resolution
# Raft will converge to single leader

# If manual intervention needed:
# Stop minority partition nodes
# Let majority partition continue
# Restart minority nodes to rejoin
```

## Performance Optimization

### Election Timeout Tuning

Low latency network (<5ms):

```yaml
cluster:
  election_timeout: 500ms
  heartbeat_interval: 50ms
```

High latency network (>50ms):

```yaml
cluster:
  election_timeout: 2000ms
  heartbeat_interval: 200ms
```

### Memory Optimization

```yaml
storage:
  cache_size: 134217728      # 128MB
  max_open_files: 1000

raft:
  snapshot_entries: 10000
  max_in_mem_log_size: 33554432  # 32MB
```

### Network Optimization

Large clusters:

```yaml
membership:
  keep_alive_interval: 10s
  health_check_interval: 30s
```

High throughput:

```yaml
cluster:
  batch_size: 1000
  batch_timeout: 10ms
```

### Disk I/O Optimization

```yaml
storage:
  sync_writes: true          # Durability
  wal_dir: /fast-ssd/wal     # Separate WAL
  data_dir: /ssd/data
```

## Backup and Restore

### Creating Backups

Manual:

```bash
quicbaas snapshot create --output /backup/snapshot-$(date +%Y%m%d)
```

Automated (daily):

```bash
cat > /etc/cron.daily/quicbaas-backup <<'EOF'
#!/bin/bash
quicbaas snapshot create --output /backup/daily-$(date +%Y%m%d)
find /backup -name "daily-*" -mtime +7 -delete
EOF
chmod +x /etc/cron.daily/quicbaas-backup
```

### Restoring from Backup

```bash
# Stop cluster
for node in node1 node2 node3; do
  ssh $node "systemctl stop quicbaas"
done

# Restore data
for node in node1 node2 node3; do
  ssh $node "rm -rf /var/lib/quicbaas/data/*"
  scp -r /backup/snapshot-20251219/* $node:/var/lib/quicbaas/data/
done

# Start cluster
for node in node1 node2 node3; do
  ssh $node "systemctl start quicbaas"
  sleep 10
done

# Verify
quicbaas cluster status
```

### Point-in-Time Recovery

```bash
quicbaas restore --snapshot /backup/snapshot-20251219 \
  --replay-to-index 12345

quicbaas data verify
```

## Troubleshooting

### High Replication Lag

```bash
curl http://node1:18080/metrics | grep replication_lag

# Increase heartbeat frequency
cluster:
  heartbeat_interval: 25ms

# Check network latency
ping -c 100 node2
```

### Slow Queries

```bash
# Enable query logging
logging:
  level: debug
  query_logging: true

# Check slow query log
tail -f /var/log/quicbaas/slow-queries.log

# Analyze query patterns
quicbaas query analyze
```

### Memory Leaks

```bash
# Monitor memory usage
watch -n 1 'ps aux | grep quicbaas'

# Heap profiling
curl http://localhost:18080/debug/pprof/heap > heap.prof
go tool pprof heap.prof

# Restart affected node
systemctl restart quicbaas
```

### Disk Space Issues

```bash
# Check usage
df -h /var/lib/quicbaas

# Compact data
quicbaas compact --path /var/lib/quicbaas/data

# Clean old snapshots
find /var/lib/quicbaas/raft/snapshots -mtime +7 -delete

# Archive old logs
quicbaas logs archive --older-than 30d
```

## Operational Checklist

### Daily

- Check cluster health status
- Review error logs
- Monitor disk usage
- Verify backup completion
- Check replication lag

### Weekly

- Review performance metrics
- Analyze slow queries
- Check data distribution balance
- Review security logs
- Test restore procedure

### Monthly

- Review capacity planning
- Update documentation
- Test disaster recovery
- Review and rotate credentials
- Evaluate upgrade needs

## Getting Help

Enable debug logging:

```yaml
logging:
  level: debug
  format: json
```

Collect diagnostics:

```bash
quicbaas diagnostics collect --output diagnostics.tar.gz
journalctl -u quicbaas --since "1 hour ago" > logs.txt
```

Check health:

```bash
curl http://localhost:18080/v1/health/live
curl http://localhost:18080/v1/cluster/status
```

Report issues with:
- QuicBaaS version
- Configuration (sanitized)
- Logs and diagnostics
- Steps to reproduce

GitHub: https://github.com/jeremyhahn/go-quicbaas/issues

## See Also

- [Cluster Management](user.md) - Cluster architecture and discovery
- [Discovery Configuration](architecture.md) - Discovery methods and PSK authentication
- CLI Reference - cluster - Cluster commands
- [CLI Reference - node](cli.md) - Node management
- [CLI Reference - rebalance](cli.md) - Rebalancing operations

---

**Last Updated:** 2025-12-19
**Version:** 0.3.0


---

## Raft mTLS

Mutual TLS authentication for securing Raft cluster communication.

### Overview

Raft mTLS provides:
- Encrypted communication between nodes
- Mutual authentication (both client and server verified)
- Protection against man-in-the-middle attacks
- Certificate-based identity verification

### Configuration

```yaml
raft:
  mtls:
    enabled: true
    cert_file: /etc/quicbaas/certs/raft.crt
    key_file: /etc/quicbaas/certs/raft.key
    ca_file: /etc/quicbaas/certs/ca.crt
    min_version: "1.2"

    # Optional cipher suites
    cipher_suites:
      - TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
      - TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256

  addresses:
    - "node1.example.com:5001"
    - "node2.example.com:5001"
    - "node3.example.com:5001"
```

### Certificate Requirements

Each node certificate must:
- Be signed by the cluster CA
- Include node hostname/IP in SAN (Subject Alternative Name)
- Have appropriate key usage (digital signature, key encipherment)

Example extensions:
```
X509v3 Key Usage: critical
    Digital Signature, Key Encipherment
X509v3 Extended Key Usage:
    TLS Web Server Authentication, TLS Web Client Authentication
X509v3 Subject Alternative Name:
    DNS:node1.example.com, IP:203.0.113.10
```

### Certificate Generation

#### Using OpenSSL

```bash
# Generate CA
openssl genrsa -out ca.key 4096
openssl req -new -x509 -days 3650 -key ca.key -out ca.crt \
  -subj "/CN=QuicBaaS Cluster CA"

# Generate node key
openssl genrsa -out node1.key 2048

# Create config with SAN
cat > node1.cnf <<EOF
[req]
req_extensions = v3_req

[v3_req]
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth, clientAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = node1.example.com
IP.1 = 203.0.113.10
EOF

# Generate CSR and sign
openssl req -new -key node1.key -out node1.csr -config node1.cnf
openssl x509 -req -days 365 -in node1.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out node1.crt -extensions v3_req -extfile node1.cnf
```

#### Using cfssl

```bash
# Initialize CA
cfssl gencert -initca ca-csr.json | cfssljson -bare ca

# Generate node certificate
cfssl gencert \
  -ca=ca.pem \
  -ca-key=ca-key.pem \
  -config=ca-config.json \
  -profile=peer \
  node1-csr.json | cfssljson -bare node1
```

### CLI Usage

```bash
# Start with mTLS
quicbaas server start \
  --raft-mtls-enabled=true \
  --raft-cert=/path/to/raft.crt \
  --raft-key=/path/to/raft.key \
  --raft-ca=/path/to/ca.crt

# Check cluster status
quicbaas cluster status

# Rotate certificates (requires restart)
quicbaas config set raft.mtls.cert_file /path/to/new-raft.crt
quicbaas server reload
```

### Deployment Patterns

#### Self-Signed CA (Development)

```yaml
raft:
  mtls:
    enabled: true
    cert_file: ./certs/raft.crt
    key_file: ./certs/raft.key
    ca_file: ./certs/ca.crt
```

#### Enterprise PKI (Production)

```yaml
raft:
  mtls:
    enabled: true
    cert_file: /etc/pki/tls/certs/quicbaas-raft.crt
    key_file: /etc/pki/tls/private/quicbaas-raft.key
    ca_file: /etc/pki/ca-trust/source/anchors/enterprise-ca.crt
```

#### Kubernetes with cert-manager

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: quicbaas-raft-tls
spec:
  secretName: quicbaas-raft-tls
  issuerRef:
    name: cluster-ca
    kind: ClusterIssuer
  commonName: quicbaas-raft
  dnsNames:
    - quicbaas-0.quicbaas-headless.default.svc
    - quicbaas-1.quicbaas-headless.default.svc
    - quicbaas-2.quicbaas-headless.default.svc
  usages:
    - digital signature
    - key encipherment
    - server auth
    - client auth
```

### Troubleshooting

#### Certificate Verification Error
```
Error: x509: certificate signed by unknown authority
Solution: Ensure CA certificate is correctly configured on all nodes
```

#### Hostname Mismatch
```
Error: x509: certificate is valid for node1.example.com, not node1
Solution: Add all hostnames/IPs to certificate SAN
```

#### Expired Certificate
```
Error: x509: certificate has expired
Solution: Rotate certificates before expiration
```

### Security Best Practices

1. Use strong key sizes (RSA 2048+ or ECDSA P-256+)
2. Short certificate lifetime (annual rotation)
3. Protect private keys with file permissions
4. Use dedicated CA for Raft communication
5. Monitor certificate expiration
6. Plan for certificate revocation (CRL or OCSP)

### Performance

- mTLS adds ~1-2ms latency for connection establishment
- Session resumption reduces overhead
- TLS 1.3 is faster than TLS 1.2
- Hardware acceleration (AES-NI) improves throughput

### See Also

- Cluster DEK - Data encryption
- [Security Overview](user.md)
