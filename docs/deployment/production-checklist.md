# QuicBaaS Production Deployment Checklist

A step-by-step checklist for deploying QuicBaaS in production environments.

## 1. Pre-Deployment Validation

### 1.1 Hardware Requirements

| Resource | Minimum | Recommended | Notes |
|----------|---------|-------------|-------|
| CPU | 4 cores | 8+ cores | More cores for concurrent compactions |
| RAM | 8 GB | 32+ GB | Cache + memtables + application |
| Disk | 100 GB SSD | 500+ GB NVMe | SSD required for production |
| Network | 1 Gbps | 10 Gbps | Low latency between nodes |

**Checklist:**

- [ ] Verify CPU meets minimum requirements (4+ cores)
- [ ] Verify RAM meets minimum requirements (8+ GB)
- [ ] Verify disk is SSD/NVMe with adequate IOPS (recommend 3000+ IOPS)
- [ ] Verify disk space sufficient for data + 3x space amplification during compaction
- [ ] Verify network bandwidth between cluster nodes (minimum 1 Gbps)
- [ ] Verify network latency between nodes (<10ms for same datacenter)

### 1.2 Software Prerequisites

- [ ] Operating system: Linux (kernel 4.15+), or macOS for development
- [ ] Go runtime not required (statically compiled binary)
- [ ] Required utilities installed: `curl`, `openssl`
- [ ] System time synchronized via NTP
- [ ] File descriptor limits increased (see below)
- [ ] Docker installed if using container deployment

**System Tuning:**

```bash
# /etc/security/limits.conf
quicbaas soft nofile 65535
quicbaas hard nofile 65535
quicbaas soft nproc 32768
quicbaas hard nproc 32768

# /etc/sysctl.conf
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
vm.swappiness = 1
vm.dirty_ratio = 40
vm.dirty_background_ratio = 10
```

- [ ] File descriptor limits set (minimum 65535)
- [ ] TCP connection limits configured
- [ ] Swap minimized (`vm.swappiness=1`)
- [ ] Dirty page settings optimized for SSDs

### 1.3 Network Configuration

**Required Ports:**

| Port | Protocol | Purpose | Exposure |
|------|----------|---------|----------|
| 8080 | TCP | REST API | External/Load Balancer |
| 9090 | TCP | gRPC API | External/Load Balancer |
| 63000 | TCP | Raft consensus | Internal only |
| 2379 | TCP | etcd-compatible API | External/Load Balancer |
| 2380 | TCP | etcd peer communication | Internal only |

**Firewall Rules Checklist:**

- [ ] Allow port 8080 (REST API) from application networks
- [ ] Allow port 9090 (gRPC API) from application networks
- [ ] Allow port 63000 (Raft) between cluster nodes only
- [ ] Allow port 2379 (etcd API) from application networks if using etcd compatibility
- [ ] Allow port 2380 (etcd peer) between cluster nodes only
- [ ] Block all other inbound traffic
- [ ] Configure outbound rules for object storage backends (if used)

```bash
# Example iptables rules for node-to-node communication
iptables -A INPUT -p tcp --dport 63000 -s <node-cidr> -j ACCEPT
iptables -A INPUT -p tcp --dport 2380 -s <node-cidr> -j ACCEPT
```

- [ ] DNS resolution working for all node hostnames
- [ ] Static IPs or stable DNS names for all cluster nodes
- [ ] Load balancer configured (if using external access)

---

## 2. Security Checklist

### 2.1 TLS/mTLS Configuration

**Client TLS (API endpoints):**

- [ ] Generate or obtain TLS certificates for client connections
- [ ] Configure certificate chain (server cert + intermediate + root CA)
- [ ] Set minimum TLS version to 1.2 or higher
- [ ] Select secure cipher suites

```yaml
# quicbaas.yaml
security:
  tls_enabled: true
  tls_cert_file: /etc/quicbaas/certs/server.crt
  tls_key_file: /etc/quicbaas/certs/server.key
  tls_ca_file: /etc/quicbaas/certs/ca.crt
  tls_client_auth: true  # Enable mTLS for clients
```

- [ ] TLS certificates generated and deployed to all nodes
- [ ] Certificate expiration monitoring configured
- [ ] Certificate rotation plan documented

**Raft mTLS (cluster communication):**

- [ ] Generate separate certificates for Raft communication
- [ ] Configure mutual TLS for all Raft traffic
- [ ] Verify all nodes use same CA for validation

```yaml
# quicbaas.yaml
raft:
  mutual_tls: true
  # Certificate paths configured separately for Raft
```

- [ ] Raft mTLS enabled (`mutual_tls: true`)
- [ ] Raft certificates deployed to all nodes
- [ ] Certificate CN/SAN matches node addresses

### 2.2 Certificate Management

- [ ] Certificate Authority (CA) established or external CA selected
- [ ] Private keys protected with restrictive permissions (`chmod 600`)
- [ ] Certificate rotation procedure documented
- [ ] Automated certificate renewal configured (Let's Encrypt, cert-manager)
- [ ] Certificate expiration alerts configured (minimum 30 days before expiry)

```bash
# Generate CA (if self-signed)
openssl genrsa -out ca.key 4096
openssl req -x509 -new -nodes -key ca.key -sha256 -days 1826 -out ca.crt

# Generate server certificate
openssl genrsa -out server.key 2048
openssl req -new -key server.key -out server.csr
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt -days 365
```

### 2.3 FIPS Mode Considerations

For deployments requiring FIPS 140-2 compliance:

- [ ] Build QuicBaaS with FIPS-enabled Go toolchain
- [ ] Use FIPS-approved cryptographic algorithms only
- [ ] Enable FIPS mode in configuration

```yaml
security:
  fips_mode: true
```

- [ ] Verify AES-256-GCM used for encryption
- [ ] Verify SHA-256 or higher used for hashing
- [ ] Document FIPS compliance boundaries

### 2.4 Authentication Setup

**JWT Authentication:**

- [ ] Generate strong JWT signing secret (minimum 256-bit)
- [ ] Configure token TTL (recommended 1 hour or less)
- [ ] Set up refresh token mechanism
- [ ] Configure token revocation

```yaml
# quicbaas.yaml (when IAM module enabled)
iam:
  enabled: true
  auth:
    local:
      enabled: true
      jwt_secret: ${JWT_SECRET}  # Use environment variable
      token_ttl: 1h
```

**OIDC Integration:**

- [ ] Register QuicBaaS with identity provider (Okta, Azure AD, etc.)
- [ ] Configure redirect URIs
- [ ] Set up token validation
- [ ] Test SSO flow end-to-end

```yaml
iam:
  oidc:
    issuer: "https://identity.example.com"
    client_id: ${OIDC_CLIENT_ID}
    client_secret: ${OIDC_CLIENT_SECRET}
```

- [ ] JWT secret securely stored (not in config file, use env var or secrets manager)
- [ ] API keys rotated if used
- [ ] Default credentials changed/removed

### 2.5 RBAC/ABAC Configuration

**RBAC Setup:**

- [ ] Define roles based on principle of least privilege
- [ ] Create admin, operator, and read-only roles at minimum
- [ ] Disable anonymous access for production

```yaml
security:
  authorization:
    provider: "rbac"
    rbac:
      enabled: true
      default_role: ""  # Empty = deny unauthenticated
      allow_anonymous: false  # Disable for production
```

**ABAC Setup (Advanced):**

- [ ] Define attribute-based policies
- [ ] Test policies with representative scenarios
- [ ] Enable policy caching for performance

```yaml
security:
  authorization:
    provider: "abac"
    abac:
      enabled: true
      cache_ttl: "5m"
      policy_dir: /etc/quicbaas/policies
```

- [ ] Default deny policy in place
- [ ] Admin accounts use MFA (if IAM enabled)
- [ ] Service accounts have minimal required permissions

### 2.6 Audit Logging Setup

- [ ] Enable audit logging for security events
- [ ] Configure log destination (file, syslog, SIEM)
- [ ] Set retention policy
- [ ] Test log completeness

```yaml
security:
  audit_log_enabled: true
  audit_log_file: /var/log/quicbaas/audit.log
  authorization:
    audit:
      enabled: true
      log_denied: true
      log_allowed: false  # Enable for verbose logging
```

- [ ] Audit logs written to persistent storage
- [ ] Log rotation configured
- [ ] Log shipping to central SIEM (if applicable)
- [ ] Audit log access restricted to security team

---

## 3. High Availability Setup

### 3.1 Minimum Node Count

| Configuration | Nodes | Fault Tolerance | Use Case |
|---------------|-------|-----------------|----------|
| Minimum | 3 | 1 node failure | Small production |
| Recommended | 5 | 2 node failures | Standard production |
| High Availability | 7 | 3 node failures | Critical systems |

**Checklist:**

- [ ] Minimum 3 nodes deployed for Raft quorum
- [ ] Odd number of nodes configured
- [ ] Nodes distributed across failure domains (racks, availability zones)
- [ ] Node IDs assigned (unique positive integers)

### 3.2 Raft Configuration

```yaml
# quicbaas.yaml
raft:
  node_id: 1  # Unique per node
  election_rtt: 10
  heartbeat_rtt: 1
  rtt_millisecond: 200  # Base RTT (tune for network)
  snapshot_entries: 10000
  compaction_overhead: 5000
  request_timeout: 5s
  raft_address: "node1.example.com:63000"
  max_in_mem_log_size: 67108864  # 64MB
```

**Tuning for Environment:**

Same datacenter (low latency):
```yaml
raft:
  rtt_millisecond: 50
  election_rtt: 20
  heartbeat_rtt: 2
```

Cross-region (high latency):
```yaml
raft:
  rtt_millisecond: 500
  election_rtt: 10
  heartbeat_rtt: 1
```

- [ ] RTT configured appropriately for network latency
- [ ] Election timeout allows for 2-3 missed heartbeats
- [ ] Snapshot entries tuned for write volume
- [ ] All nodes have consistent Raft configuration

### 3.3 Cluster Discovery Configuration

**Static Discovery:**

```yaml
discovery:
  methods:
    - type: static
      peers:
        - "1:node1.example.com:63000"
        - "2:node2.example.com:63000"
        - "3:node3.example.com:63000"
cluster:
  min_quorum: 2
```

**DNS Discovery:**

```yaml
discovery:
  methods:
    - type: dns
      srv_record: "_quicbaas._tcp.example.com"
      refresh_interval: 30s
```

**Kubernetes Discovery:**

```yaml
discovery:
  methods:
    - type: kubernetes
      namespace: quicbaas
      service: quicbaas-headless
      refresh_interval: 30s
```

- [ ] Discovery method selected and configured
- [ ] All nodes discoverable via chosen method
- [ ] Minimum quorum configured correctly

### 3.4 Load Balancer Setup

- [ ] Layer 4 (TCP) load balancer configured for gRPC (port 9090)
- [ ] Layer 7 (HTTP) load balancer configured for REST (port 8080)
- [ ] Health checks configured for each endpoint
- [ ] Session affinity disabled (stateless protocols)
- [ ] Connection draining enabled for graceful shutdown

**Health Check Endpoints:**

```
GET /health          # Basic health check
GET /health/ready    # Readiness (accepting traffic)
GET /health/live     # Liveness (process running)
```

**Load Balancer Health Check Configuration:**

| Setting | Value |
|---------|-------|
| Protocol | HTTP |
| Path | /health/ready |
| Port | 8080 |
| Interval | 10 seconds |
| Timeout | 5 seconds |
| Healthy threshold | 2 |
| Unhealthy threshold | 3 |

- [ ] Load balancer health checks passing
- [ ] Traffic distributes across healthy nodes
- [ ] Failover tested with node shutdown

---

## 4. Backup and Recovery

### 4.1 Backup Schedule Configuration

```yaml
# quicbaas.yaml
backup:
  enabled: true
  storage_backend: "s3"  # Reference to object_storage backend
  default_compression: "zstd"
  default_encryption: "aes-256-gcm"
  encryption_key: ${BACKUP_ENCRYPTION_KEY}
  work_dir: /tmp/quicbaas-backup
  timeout: 30m
```

**Backup Commands:**

```bash
# Manual full backup
quicbaas backup create --type full --name daily-backup

# Incremental backup
quicbaas backup create --type incremental --name hourly-backup

# List backups
quicbaas backup list

# Verify backup integrity
quicbaas backup verify --name daily-backup
```

**Recommended Schedule:**

| Backup Type | Frequency | Retention |
|-------------|-----------|-----------|
| Full | Daily | 30 days |
| Incremental | Hourly | 7 days |
| Point-in-time | Continuous | 24 hours |

- [ ] Backup storage backend configured (S3, GCS, Azure, local)
- [ ] Backup encryption enabled
- [ ] Backup schedule automated (cron, Kubernetes CronJob)
- [ ] Backup location geographically separate from cluster

### 4.2 Retention Policies

- [ ] Define retention periods per backup type
- [ ] Configure automatic cleanup of expired backups
- [ ] Verify backup storage capacity for retention period
- [ ] Document compliance requirements for retention

### 4.3 Recovery Testing

Recovery should be tested regularly:

- [ ] Document step-by-step recovery procedure
- [ ] Test recovery to staging environment quarterly
- [ ] Measure and document Recovery Time Objective (RTO)
- [ ] Measure and document Recovery Point Objective (RPO)
- [ ] Test partial recovery (single node failure)
- [ ] Test full recovery (complete cluster loss)

**Recovery Commands:**

```bash
# Restore from backup
quicbaas backup restore --name daily-backup --target /var/lib/quicbaas/data

# Verify data integrity after restore
quicbaas server verify

# Point-in-time recovery
quicbaas backup restore --name daily-backup --target /var/lib/quicbaas/data --timestamp "2025-01-07T10:00:00Z"
```

- [ ] Recovery tested within last 90 days
- [ ] Recovery documentation current
- [ ] Recovery time meets SLA requirements

---

## 5. Monitoring and Alerting

### 5.1 Prometheus Metrics Endpoints

QuicBaaS exposes Prometheus metrics at `/metrics` on the REST API port (8080).

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'quicbaas'
    scrape_interval: 15s
    static_configs:
      - targets:
        - 'node1.example.com:8080'
        - 'node2.example.com:8080'
        - 'node3.example.com:8080'
    scheme: https  # If TLS enabled
    tls_config:
      ca_file: /etc/prometheus/ca.crt
```

- [ ] Prometheus scraping all nodes
- [ ] Metrics endpoint accessible
- [ ] Grafana dashboards deployed

### 5.2 Key Metrics to Monitor

**Availability Metrics:**

| Metric | Description | Query |
|--------|-------------|-------|
| Node health | Node up/down status | `up{job="quicbaas"}` |
| Leader presence | Raft leader elected | `quicbaas_raft_leader_id != 0` |
| Cluster quorum | Healthy node count | `count(up{job="quicbaas"} == 1)` |

**Performance Metrics:**

| Metric | Description | Query |
|--------|-------------|-------|
| Read latency P99 | 99th percentile read time | `histogram_quantile(0.99, rate(quicbaas_request_duration_seconds_bucket{operation="get"}[5m]))` |
| Write latency P99 | 99th percentile write time | `histogram_quantile(0.99, rate(quicbaas_request_duration_seconds_bucket{operation="set"}[5m]))` |
| Throughput | Operations per second | `rate(quicbaas_request_duration_seconds_count[1m])` |
| Error rate | Percentage of failed requests | `rate(quicbaas_request_duration_seconds_count{status="error"}[5m]) / rate(quicbaas_request_duration_seconds_count[5m])` |

**Resource Metrics:**

| Metric | Description | Query |
|--------|-------------|-------|
| Memory usage | Process memory | `process_resident_memory_bytes{job="quicbaas"}` |
| CPU usage | CPU utilization | `rate(process_cpu_seconds_total{job="quicbaas"}[5m])` |
| Disk usage | Storage utilization | `node_filesystem_avail_bytes{mountpoint="/data"}` |
| Goroutines | Go routine count | `go_goroutines{job="quicbaas"}` |

**Storage Metrics:**

| Metric | Description | Query |
|--------|-------------|-------|
| Cache hit rate | Cache effectiveness | `rate(quicbaas_storage_cache_hits_total[5m]) / (rate(quicbaas_storage_cache_hits_total[5m]) + rate(quicbaas_storage_cache_misses_total[5m]))` |
| Compaction backlog | Pending compaction bytes | `quicbaas_storage_compaction_pending_bytes` |

- [ ] Dashboard showing all key metrics
- [ ] Historical data retention configured (minimum 30 days)

### 5.3 Recommended Alert Thresholds

**Critical Alerts (Immediate Action Required):**

| Alert | Condition | Threshold |
|-------|-----------|-----------|
| Node Down | Node unreachable | 1 minute |
| Leader Missing | No Raft leader | 30 seconds |
| Quorum Lost | Healthy nodes < quorum | 30 seconds |
| Disk Full | Available space < 10% | 5 minutes |

**Warning Alerts (Action Within Hours):**

| Alert | Condition | Threshold |
|-------|-----------|-----------|
| High Read Latency | P99 > 100ms | 5 minutes |
| High Write Latency | P99 > 250ms | 5 minutes |
| High Error Rate | Errors > 5% | 5 minutes |
| High Memory | Memory > 90% | 5 minutes |
| High CPU | CPU > 80% | 10 minutes |
| Backup Failed | Backup error | Immediate |
| Backup Too Old | Last backup > 24h | 1 hour |

**Alert Rules (deploy to Prometheus):**

```yaml
# See deployments/prometheus/alerts.yml for complete rules
```

- [ ] Alert rules deployed to Prometheus
- [ ] Alert routing configured (PagerDuty, Slack, email)
- [ ] On-call rotation established
- [ ] Runbooks created for each alert

---

## 6. Performance Tuning

### 6.1 Cache Sizing

The cache improves read performance by keeping frequently accessed data in memory.

**Formula:**

```
cache_size = (working_set_size * 0.1) to (working_set_size * 0.2)
```

**Configuration:**

```yaml
# quicbaas.yaml
storage:
  cache_size: 10737418240  # 10 GB for production

cache:
  enabled: true
  default_ttl: 5m
  max_size: 10000
  cleanup_interval: 1m
  write_through: true
```

| Environment | RAM | Recommended Cache |
|-------------|-----|-------------------|
| Development | 8 GB | 128 MB |
| Small Production | 32 GB | 2-4 GB |
| Medium Production | 64 GB | 10-15 GB |
| Large Production | 128+ GB | 20-30 GB |

- [ ] Cache size configured based on available RAM
- [ ] Cache hit rate monitored (target > 80%)
- [ ] L2 cache (Redis) configured if needed for distributed caching

### 6.2 Connection Limits

```yaml
# quicbaas.yaml
server:
  max_connections: 1000
  read_timeout: 30s
  write_timeout: 30s
  idle_timeout: 60s
  shutdown_timeout: 30s

storage:
  max_open_files: 1000
  max_concurrent_reads: 100
  max_concurrent_writes: 100
```

**Tuning Guidelines:**

| Setting | Small | Medium | Large |
|---------|-------|--------|-------|
| max_connections | 500 | 1000 | 5000 |
| max_open_files | 500 | 1000 | 5000 |
| max_concurrent_reads | 50 | 100 | 500 |
| max_concurrent_writes | 50 | 100 | 500 |

- [ ] Connection limits set based on expected load
- [ ] File descriptor limits support max_open_files
- [ ] Timeout values appropriate for network latency

### 6.3 Graceful Shutdown (durability-critical)

`quicbaasd` must terminate through its own SIGINT/SIGTERM handler. That handler
runs `Server.Shutdown`, which closes the embedded qrdb node and therefore its
PebbleDB. Closing the store is what flushes writes still held in the memtable
and the unsynced WAL block (`SyncWrites: false` — the store is not fsynced per
write). A process that exits without it silently loses every write made since
the last flush.

- [ ] Supervisor sends `SIGTERM` (not `SIGKILL`) and waits for the process to
      exit before restarting. `server.shutdown_timeout` bounds the drain; give
      the supervisor a stop timeout that exceeds it
      (systemd `TimeoutStopSec`, Kubernetes `terminationGracePeriodSeconds`,
      `docker stop -t`).
- [ ] A graceful stop exits **0**. A non-zero exit on `SIGTERM` means something
      terminated the process from inside a signal handler and the store was not
      closed — treat it as a data-loss defect, not a cosmetic exit code.
- [ ] The shutdown log contains `Performing graceful shutdown...` followed by
      `Embedded qrdb node shutdown complete`.

`main()` arms the secure-memory subsystem with `secure.EnablePurge()`, which
wipes mlocked key material via the deferred `Purge` without installing a signal
handler. Do **not** switch it to `secure.CatchInterrupt()`: that handler calls
`os.Exit(1)` from inside the signal handler, always beats the graceful path, and
reintroduces the data loss. The invariant is pinned by
`test/integration/durability`.

### 6.4 Raft Timeouts

**Same Datacenter (low latency <5ms):**

```yaml
raft:
  rtt_millisecond: 50
  election_rtt: 20      # 1 second election timeout
  heartbeat_rtt: 2      # 100ms heartbeat
  request_timeout: 2s
```

**Cross-Region (high latency 50-200ms):**

```yaml
raft:
  rtt_millisecond: 500
  election_rtt: 10      # 5 second election timeout
  heartbeat_rtt: 1      # 500ms heartbeat
  request_timeout: 10s
```

**Hybrid (mixed latency):**

```yaml
raft:
  rtt_millisecond: 200
  election_rtt: 15      # 3 second election timeout
  heartbeat_rtt: 1      # 200ms heartbeat
  request_timeout: 5s
```

- [ ] RTT configured based on measured network latency
- [ ] Election timeout prevents unnecessary elections
- [ ] Heartbeat interval keeps followers synced

### 6.5 Storage Engine Tuning

**Write-Heavy Workloads:**

```yaml
storage:
  mem_table_size: 268435456  # 256 MB
  l0_compaction_threshold: 8
  sync_writes: false  # Async for performance (data loss risk)
```

**Read-Heavy Workloads:**

```yaml
storage:
  cache_size: 21474836480  # 20 GB
  compression_type: "zstd"
  max_concurrent_reads: 500
```

**Balanced Workloads:**

```yaml
storage:
  cache_size: 10737418240  # 10 GB
  mem_table_size: 67108864  # 64 MB
  l0_compaction_threshold: 4
  sync_writes: true
  compression_type: "snappy"
```

- [ ] Storage configuration matches workload profile
- [ ] Compaction keeping pace with writes (L0 files < 12)
- [ ] Write amplification within acceptable range (10-30x)

---

## 7. Final Verification

### 7.1 Pre-Launch Checklist

**Infrastructure:**

- [ ] All nodes provisioned with correct specifications
- [ ] Storage volumes attached and formatted
- [ ] Network connectivity verified between all nodes
- [ ] DNS resolution working
- [ ] Time synchronized across all nodes

**Security:**

- [ ] TLS certificates deployed and valid
- [ ] mTLS enabled for Raft communication
- [ ] Authentication configured and tested
- [ ] Authorization policies deployed
- [ ] Audit logging enabled
- [ ] Firewall rules applied

**High Availability:**

- [ ] Minimum 3 nodes deployed
- [ ] Raft cluster formed and leader elected
- [ ] Cluster status shows all nodes healthy
- [ ] Load balancer health checks passing

**Backup and Recovery:**

- [ ] Backup storage configured
- [ ] Initial backup completed successfully
- [ ] Recovery procedure documented
- [ ] Recovery tested successfully

**Monitoring:**

- [ ] Prometheus scraping all nodes
- [ ] Dashboards deployed and showing data
- [ ] Alert rules configured
- [ ] Alert routing tested

**Performance:**

- [ ] Cache sized appropriately
- [ ] Connection limits set
- [ ] Raft timeouts tuned
- [ ] Load testing completed

### 7.2 Launch Verification Commands

```bash
# Check cluster health
quicbaas cluster status

# Verify all nodes
quicbaas node list

# Check Raft status
quicbaas cluster status --output json | jq '.raft'

# Test basic operations
quicbaas data put test key1 "value1"
quicbaas data get test key1

# Verify replication
# (run on follower node)
quicbaas data get test key1

# Check metrics endpoint
curl -s http://localhost:8080/metrics | head -50

# Verify health endpoints
curl -s http://localhost:8080/health
curl -s http://localhost:8080/health/ready
curl -s http://localhost:8080/health/live
```

### 7.3 Post-Launch Monitoring (First 24 Hours)

- [ ] Monitor error rates (should be < 0.1%)
- [ ] Monitor latency (P99 within expected range)
- [ ] Monitor resource utilization (CPU < 70%, Memory < 80%)
- [ ] Verify no leader elections (indicates stability)
- [ ] Confirm backup completed successfully
- [ ] Review audit logs for unexpected access

---

## Quick Reference

### Essential Configuration File

```yaml
# /etc/quicbaas/quicbaas.yaml - Production configuration
server:
  host: "0.0.0.0"
  port: 8080
  grpc_port: 9090
  max_connections: 1000

raft:
  node_id: 1  # Change per node
  rtt_millisecond: 200
  election_rtt: 10
  heartbeat_rtt: 1
  raft_address: "node1.example.com:63000"  # Change per node
  mutual_tls: true

storage:
  data_dir: /var/lib/quicbaas/data
  cache_size: 10737418240  # 10 GB
  sync_writes: true

security:
  tls_enabled: true
  tls_cert_file: /etc/quicbaas/certs/server.crt
  tls_key_file: /etc/quicbaas/certs/server.key
  tls_ca_file: /etc/quicbaas/certs/ca.crt
  audit_log_enabled: true
  authorization:
    provider: "rbac"
    rbac:
      enabled: true
      allow_anonymous: false

discovery:
  methods:
    - type: static
      peers:
        - "1:node1.example.com:63000"
        - "2:node2.example.com:63000"
        - "3:node3.example.com:63000"

backup:
  enabled: true
  storage_backend: "s3"
  default_compression: "zstd"
  default_encryption: "aes-256-gcm"

logging:
  level: "info"
  format: "json"
```

### Emergency Contacts

| Role | Contact |
|------|---------|
| On-call Engineer | (configure) |
| Database Admin | (configure) |
| Security Team | (configure) |

---

Last Updated: 2026-02-03
