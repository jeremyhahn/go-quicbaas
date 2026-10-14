# Observability — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/observability/`.

This guide covers monitoring, metrics, distributed tracing, logging, and health checks for QuicBaaS clusters.

## Table of Contents

1. [Metrics Overview](#metrics-overview)
2. [Key Metrics to Monitor](#key-metrics-to-monitor)
3. [Alerting Recommendations](#alerting-recommendations)
4. [Distributed Tracing](#distributed-tracing)
5. [Logging](#logging)
6. [Health Checks](#health-checks)
7. [Grafana Dashboards](#grafana-dashboards)

---

## Metrics Overview

QuicBaaS exposes Prometheus-compatible metrics through a dedicated endpoint, providing comprehensive observability into cluster health, performance, and resource utilization.

### Prometheus Endpoint Configuration

QuicBaaS exposes metrics at the `/api/v1/metrics/` endpoint. The endpoint supports OpenMetrics format for enhanced compatibility.

**Basic Configuration:**

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'quicbaas'
    static_configs:
      - targets: ['quicbaas-node1:8080', 'quicbaas-node2:8080', 'quicbaas-node3:8080']
    metrics_path: '/api/v1/metrics/'
    scrape_interval: 15s
    scrape_timeout: 10s
```

**With TLS Enabled:**

```yaml
scrape_configs:
  - job_name: 'quicbaas'
    scheme: https
    tls_config:
      ca_file: /path/to/ca.crt
      cert_file: /path/to/client.crt
      key_file: /path/to/client.key
    static_configs:
      - targets: ['quicbaas-node1:8443']
    metrics_path: '/api/v1/metrics/'
```

**Kubernetes Service Discovery:**

```yaml
scrape_configs:
  - job_name: 'quicbaas-k8s'
    kubernetes_sd_configs:
      - role: pod
        namespaces:
          names: ['quicbaas']
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_label_app]
        action: keep
        regex: quicbaas
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_port]
        action: replace
        target_label: __address__
        regex: (.+)
        replacement: ${1}:8080
    metrics_path: '/api/v1/metrics/'
```

### Available Metric Types

QuicBaaS exposes four categories of Prometheus metrics:

| Type | Description | Example |
|------|-------------|---------|
| Counter | Monotonically increasing value | `quicbaas_request_duration_seconds_count` |
| Gauge | Value that can increase or decrease | `quicbaas_concurrent_requests` |
| Histogram | Distribution of values with buckets | `quicbaas_request_duration_seconds_bucket` |
| Summary | Quantile calculations | Go runtime metrics |

**Runtime Metrics (automatically included):**

- `go_goroutines` - Number of active goroutines
- `go_memstats_*` - Memory statistics
- `process_cpu_seconds_total` - CPU usage
- `process_resident_memory_bytes` - Memory usage
- `process_open_fds` - Open file descriptors

---

## Key Metrics to Monitor

### Raft Health Metrics

Raft consensus health is critical for cluster availability and data consistency.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `quicbaas_raft_proposal_latency_seconds` | Histogram | `shard` | Time from proposal to commit |
| `quicbaas_raft_snapshot_size_bytes` | Gauge | `shard` | Current snapshot size per shard |
| `quicbaas_raft_leader_id` | Gauge | `shard` | Current leader node ID (0 = no leader) |
| `quicbaas_raft_snapshot_failures_total` | Counter | `shard` | Snapshot operation failures |

**Recommended Queries:**

```promql
# P99 Raft proposal latency by shard
histogram_quantile(0.99, rate(quicbaas_raft_proposal_latency_seconds_bucket[5m]))

# Leader election rate (should be low in stable cluster)
changes(quicbaas_raft_leader_id[1h])

# Shards without leaders (critical)
count(quicbaas_raft_leader_id == 0)

# Snapshot size growth rate
rate(quicbaas_raft_snapshot_size_bytes[1h])
```

**Thresholds:**

| Metric | Warning | Critical |
|--------|---------|----------|
| Raft proposal P99 latency | > 250ms | > 500ms |
| Leader elections per hour | > 2 | > 5 |
| Shards without leader | - | > 0 for 30s |

### Storage Metrics

Monitor storage health to prevent disk exhaustion and performance degradation.

| Metric | Type | Description |
|--------|------|-------------|
| `quicbaas_storage_disk_usage_bytes` | Gauge | Total disk space used |
| `quicbaas_storage_compaction_pending_bytes` | Gauge | Pending compaction work |
| `quicbaas_storage_compaction_duration_seconds` | Histogram | Compaction operation duration |
| `quicbaas_storage_wal_size_bytes` | Gauge | Write-ahead log size |

**Recommended Queries:**

```promql
# Disk usage percentage (requires node_exporter)
quicbaas_storage_disk_usage_bytes / node_filesystem_size_bytes{mountpoint="/data"} * 100

# Compaction backlog in GB
quicbaas_storage_compaction_pending_bytes / (1024^3)

# Average compaction duration
rate(quicbaas_storage_compaction_duration_seconds_sum[10m]) /
rate(quicbaas_storage_compaction_duration_seconds_count[10m])

# Write amplification (compacted bytes vs written bytes)
rate(quicbaas_storage_compaction_bytes_total[1h]) /
rate(quicbaas_storage_write_bytes_total[1h])
```

**Thresholds:**

| Metric | Warning | Critical |
|--------|---------|----------|
| Disk usage | > 70% | > 90% |
| Compaction backlog | > 5GB | > 10GB |
| Average compaction time | > 60s | > 300s |

### Request Metrics

Track request performance to identify latency issues and capacity problems.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `quicbaas_request_duration_seconds` | Histogram | `operation`, `status` | Request latency |
| `quicbaas_request_size_bytes` | Histogram | `operation` | Request payload size |
| `quicbaas_concurrent_requests` | Gauge | - | Currently processing requests |

**Recommended Queries:**

```promql
# Request rate by operation
rate(quicbaas_request_duration_seconds_count[5m])

# P50/P95/P99 latency by operation
histogram_quantile(0.50, rate(quicbaas_request_duration_seconds_bucket{operation="get"}[5m]))
histogram_quantile(0.95, rate(quicbaas_request_duration_seconds_bucket{operation="get"}[5m]))
histogram_quantile(0.99, rate(quicbaas_request_duration_seconds_bucket{operation="get"}[5m]))

# Error rate
rate(quicbaas_request_duration_seconds_count{status="error"}[5m]) /
rate(quicbaas_request_duration_seconds_count[5m])

# Average request size
rate(quicbaas_request_size_bytes_sum[5m]) /
rate(quicbaas_request_size_bytes_count[5m])
```

**Thresholds:**

| Metric | Warning | Critical |
|--------|---------|----------|
| Read P99 latency | > 50ms | > 100ms |
| Write P99 latency | > 100ms | > 250ms |
| Error rate | > 1% | > 5% |
| Concurrent requests | > 80% capacity | > 95% capacity |

### Transaction Metrics

Monitor transaction behavior for multi-operation workloads.

| Metric | Type | Description |
|--------|------|-------------|
| `quicbaas_transaction_duration_seconds` | Histogram | Transaction execution time |
| `quicbaas_transaction_operations` | Histogram | Operations per transaction |
| `quicbaas_transaction_conflicts_total` | Counter | Optimistic locking failures |

**Recommended Queries:**

```promql
# Transaction conflict rate
rate(quicbaas_transaction_conflicts_total[5m]) /
rate(quicbaas_transaction_duration_seconds_count[5m])

# Average operations per transaction
rate(quicbaas_transaction_operations_sum[5m]) /
rate(quicbaas_transaction_operations_count[5m])

# Large transaction rate (>50 ops)
histogram_quantile(0.90, rate(quicbaas_transaction_operations_bucket[5m]))
```

### Connection Metrics

Monitor connection patterns for capacity planning.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `quicbaas_rest_api_active_connections` | Gauge | - | Active REST connections |
| `quicbaas_rest_api_connections_total` | Counter | - | Total connections over time |
| `quicbaas_routing_rest_requests_total` | Counter | `shard_id`, `entity_type`, `operation` | Requests by shard |

**Recommended Queries:**

```promql
# Connection rate
rate(quicbaas_rest_api_connections_total[5m])

# Requests per shard (distribution check)
sum by (shard_id) (rate(quicbaas_routing_rest_requests_total[5m]))

# Hot shard detection
topk(3, sum by (shard_id) (rate(quicbaas_routing_rest_requests_total[5m])))
```

### Cache Metrics

Monitor cache efficiency for memory-intensive workloads.

| Metric | Type | Description |
|--------|------|-------------|
| `quicbaas_cache_hits_total` | Counter | Cache hit count |
| `quicbaas_cache_misses_total` | Counter | Cache miss count |
| `quicbaas_cache_evictions_total` | Counter | Cache eviction count |
| `quicbaas_cache_size_bytes` | Gauge | Current cache memory usage |

**Recommended Queries:**

```promql
# Cache hit ratio
rate(quicbaas_cache_hits_total[5m]) /
(rate(quicbaas_cache_hits_total[5m]) + rate(quicbaas_cache_misses_total[5m]))

# Cache eviction rate
rate(quicbaas_cache_evictions_total[5m])

# Cache memory utilization
quicbaas_cache_size_bytes / quicbaas_cache_capacity_bytes
```

**Thresholds:**

| Metric | Warning | Critical |
|--------|---------|----------|
| Cache hit ratio | < 80% | < 60% |
| Eviction rate | > 50/s | > 100/s |

### Vector Database Metrics

Monitor vector operations for AI/ML workloads.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `quicbaas_vector_operations_total` | Counter | `tenant_id`, `namespace`, `operation`, `status` | Vector operations |
| `quicbaas_vector_operation_duration_seconds` | Histogram | `tenant_id`, `namespace`, `operation` | Operation latency |
| `quicbaas_vector_storage_bytes` | Gauge | `tenant_id`, `namespace` | Storage per namespace |
| `quicbaas_vector_count` | Gauge | `tenant_id`, `namespace` | Vector count |
| `quicbaas_vector_search_latency_seconds` | Histogram | `tenant_id`, `namespace`, `search_type` | Search latency |
| `quicbaas_vector_quota_usage_ratio` | Gauge | `tenant_id`, `namespace`, `quota_type` | Quota utilization |

**Recommended Queries:**

```promql
# Vector search P99 latency by tenant
histogram_quantile(0.99,
  sum by (tenant_id, le) (rate(quicbaas_vector_search_latency_seconds_bucket[5m]))
)

# Tenants approaching quota limits
quicbaas_vector_quota_usage_ratio > 0.8

# Vector ingestion rate by namespace
rate(quicbaas_vector_operations_total{operation="upsert"}[5m])
```

---

## Alerting Recommendations

### Critical Alerts

These alerts indicate immediate action is required to prevent data loss or service outage.

```yaml
# deployments/prometheus/alerts.yml
groups:
  - name: quicbaas_critical
    interval: 30s
    rules:
      - alert: ClusterQuorumLost
        expr: count(up{job="quicbaas"} == 1) < (count(up{job="quicbaas"}) / 2 + 1)
        for: 30s
        labels:
          severity: critical
        annotations:
          summary: "QuicBaaS cluster has lost quorum"
          description: "Less than majority of nodes are healthy. Data writes will fail."
          runbook: "Check node health, network connectivity, and disk space"

      - alert: RaftLeaderMissing
        expr: quicbaas_raft_leader_id == 0
        for: 30s
        labels:
          severity: critical
        annotations:
          summary: "Raft shard {{ $labels.shard }} has no leader"
          description: "Shard cannot accept writes without a leader"

      - alert: DiskSpaceCritical
        expr: (node_filesystem_avail_bytes{mountpoint="/data"} / node_filesystem_size_bytes) < 0.1
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Disk space below 10% on {{ $labels.instance }}"
          description: "Immediate action required to prevent write failures"

      - alert: QuicBaaSNodeDown
        expr: up{job="quicbaas"} == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "QuicBaaS node {{ $labels.instance }} is down"
```

### Warning Alerts

These alerts indicate degraded performance or approaching limits.

```yaml
      - alert: HighReadLatency
        expr: histogram_quantile(0.99, rate(quicbaas_request_duration_seconds_bucket{operation="get"}[5m])) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High read latency (P99 > 100ms)"
          description: "P99 read latency is {{ $value | humanizeDuration }}"

      - alert: HighWriteLatency
        expr: histogram_quantile(0.99, rate(quicbaas_request_duration_seconds_bucket{operation="set"}[5m])) > 0.25
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High write latency (P99 > 250ms)"

      - alert: ConnectionSpike
        expr: rate(quicbaas_rest_api_connections_total[1m]) > 100
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Connection spike detected"
          description: "Connection rate is {{ $value }}/sec"

      - alert: HighErrorRate
        expr: |
          rate(quicbaas_request_duration_seconds_count{status="error"}[5m]) /
          rate(quicbaas_request_duration_seconds_count[5m]) > 0.05
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Error rate above 5%"

      - alert: RaftHighProposalLatency
        expr: histogram_quantile(0.99, rate(quicbaas_raft_proposal_latency_seconds_bucket[5m])) > 0.5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High Raft proposal latency in shard {{ $labels.shard }}"

      - alert: HighCompactionBacklog
        expr: quicbaas_storage_compaction_pending_bytes > 10737418240
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Compaction backlog above 10GB"

      - alert: LowCacheHitRatio
        expr: |
          rate(quicbaas_cache_hits_total[5m]) /
          (rate(quicbaas_cache_hits_total[5m]) + rate(quicbaas_cache_misses_total[5m])) < 0.6
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: "Cache hit ratio below 60%"

      - alert: HighGoRoutines
        expr: go_goroutines{job="quicbaas"} > 10000
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Possible goroutine leak ({{ $value }} goroutines)"
```

---

## Distributed Tracing

QuicBaaS supports OpenTelemetry for distributed tracing, enabling end-to-end request visibility across services.

### OpenTelemetry Configuration

Enable tracing by building with the `otel` build tag and setting environment variables:

```bash
# Build with tracing support
go build -tags otel ./cmd/quicbaas

# Enable tracing at runtime
export QUICBAAS_TRACING_ENABLED=1
export OTEL_EXPORTER_OTLP_ENDPOINT=http://jaeger:4317
export OTEL_SERVICE_NAME=quicbaas
```

**Configuration Options:**

The `quicbaas` CLI also accepts each `QUICBAAS_*` setting below as a flag and a
config-file key; see `cli.md` for the names and precedence.

| Environment Variable | Description | Default |
|---------------------|-------------|---------|
| `QUICBAAS_TRACING_ENABLED` | Enable/disable tracing | `0` (disabled) |
| `QUICBAAS_TRACING_EXPORTER` | Exporter type (`otlpgrpc` or `stdout`) | `stdout` |
| `QUICBAAS_OTLP_ENDPOINT` | OTLP collector endpoint | `localhost:4317` |
| `QUICBAAS_TRACING_SERVICE_NAME` | Service name in traces | `quicbaas` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP collector endpoint (standard) | - |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | Protocol (grpc/http) | `grpc` |
| `OTEL_SERVICE_NAME` | Service name in traces | `quicbaas` |
| `OTEL_RESOURCE_ATTRIBUTES` | Additional resource attributes | - |

### TLS Configuration for Trace Export

For production environments, secure trace export requires TLS configuration. QuicBaaS supports both standard TLS and mutual TLS (mTLS) for OTLP exporters.

#### TLSConfig Structure

The TLS configuration uses the following structure:

| Field | Type | Description |
|-------|------|-------------|
| `CertFile` | string | Path to the client certificate file (PEM format) |
| `KeyFile` | string | Path to the client private key file (PEM format) |
| `CAFile` | string | Path to the CA certificate file for server verification |
| `ServerName` | string | Override server name for certificate verification |
| `InsecureSkipVerify` | bool | Disable server certificate verification (not recommended for production) |

#### Environment Variable Configuration

Configure TLS for OTLP export using environment variables:

```bash
# Basic TLS (server verification only)
export OTEL_EXPORTER_OTLP_ENDPOINT=https://collector.example.com:4317
export OTEL_EXPORTER_OTLP_CERTIFICATE=/path/to/ca.crt

# Mutual TLS (mTLS) with client authentication
export OTEL_EXPORTER_OTLP_ENDPOINT=https://collector.example.com:4317
export OTEL_EXPORTER_OTLP_CERTIFICATE=/path/to/ca.crt
export OTEL_EXPORTER_OTLP_CLIENT_KEY=/path/to/client.key
export OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE=/path/to/client.crt
```

**TLS Environment Variables:**

| Variable | Description | Required |
|----------|-------------|----------|
| `OTEL_EXPORTER_OTLP_CERTIFICATE` | CA certificate for verifying the collector | Yes (for TLS) |
| `OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE` | Client certificate for mTLS authentication | For mTLS |
| `OTEL_EXPORTER_OTLP_CLIENT_KEY` | Client private key for mTLS authentication | For mTLS |
| `OTEL_EXPORTER_OTLP_INSECURE` | Skip TLS verification (development only) | No |

#### Kubernetes Configuration

**Secret for TLS Certificates:**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: quicbaas-otel-tls
  namespace: quicbaas
type: kubernetes.io/tls
data:
  tls.crt: <base64-encoded-client-cert>
  tls.key: <base64-encoded-client-key>
  ca.crt: <base64-encoded-ca-cert>
```

**ConfigMap for Tracing Configuration:**

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: quicbaas-tracing
  namespace: quicbaas
data:
  QUICBAAS_TRACING_ENABLED: "1"
  QUICBAAS_TRACING_EXPORTER: "otlpgrpc"
  OTEL_EXPORTER_OTLP_ENDPOINT: "https://jaeger-collector.observability:4317"
  OTEL_SERVICE_NAME: "quicbaas"
  OTEL_RESOURCE_ATTRIBUTES: "deployment.environment=production,service.version=1.0.0"
```

**Pod Specification with mTLS:**

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: quicbaas
spec:
  containers:
    - name: quicbaas
      image: quicbaas:latest
      envFrom:
        - configMapRef:
            name: quicbaas-tracing
      env:
        - name: OTEL_EXPORTER_OTLP_CERTIFICATE
          value: /etc/otel-tls/ca.crt
        - name: OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE
          value: /etc/otel-tls/tls.crt
        - name: OTEL_EXPORTER_OTLP_CLIENT_KEY
          value: /etc/otel-tls/tls.key
      volumeMounts:
        - name: otel-tls
          mountPath: /etc/otel-tls
          readOnly: true
  volumes:
    - name: otel-tls
      secret:
        secretName: quicbaas-otel-tls
```

#### Programmatic TLS Configuration (Go SDK)

When using the QuicBaaS Go SDK, configure TLS programmatically:

```go
package main

import (
    "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

func main() {
    // Basic TLS with CA verification
    client, err := quicbaas.NewClient(
        "quicbaas.example.com:8443",
        quicbaas.WithTLS(quicbaas.TLSConfig{
            CAFile: "/path/to/ca.crt",
        }),
    )

    // Mutual TLS (mTLS) with client certificate
    client, err := quicbaas.NewClient(
        "quicbaas.example.com:8443",
        quicbaas.WithTLS(quicbaas.TLSConfig{
            CertFile:   "/path/to/client.crt",
            KeyFile:    "/path/to/client.key",
            CAFile:     "/path/to/ca.crt",
            ServerName: "quicbaas.example.com",
        }),
    )

    // Development only: skip certificate verification
    client, err := quicbaas.NewClient(
        "quicbaas.example.com:8443",
        quicbaas.WithTLS(quicbaas.TLSConfig{
            InsecureSkipVerify: true,
        }),
    )
}
```

#### mTLS for Trace Collectors

When your OTLP collector requires mutual TLS authentication:

**Jaeger Collector with mTLS:**

```yaml
# jaeger-collector configuration
collector:
  otlp:
    enabled: true
    grpc:
      host-port: ":4317"
      tls:
        enabled: true
        cert: /etc/jaeger/tls/tls.crt
        key: /etc/jaeger/tls/tls.key
        client-ca: /etc/jaeger/tls/ca.crt
        client-auth: require
```

**OpenTelemetry Collector with mTLS:**

```yaml
# otel-collector-config.yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
        tls:
          cert_file: /etc/otel/tls/server.crt
          key_file: /etc/otel/tls/server.key
          client_ca_file: /etc/otel/tls/ca.crt
          # Require client certificates
          # Options: NoClientCert, RequestClientCert, RequireAnyClientCert,
          #          VerifyClientCertIfGiven, RequireAndVerifyClientCert
          min_version: "1.2"

exporters:
  jaeger:
    endpoint: jaeger:14250
    tls:
      cert_file: /etc/otel/tls/client.crt
      key_file: /etc/otel/tls/client.key
      ca_file: /etc/otel/tls/ca.crt

service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [jaeger]
```

#### TLS Security Best Practices

| Practice | Description |
|----------|-------------|
| Use TLS 1.2+ | QuicBaaS enforces TLS 1.2 as minimum version |
| Rotate certificates | Implement certificate rotation before expiry |
| Avoid InsecureSkipVerify | Only use for development/testing |
| Use strong ciphers | Default cipher suites are production-ready |
| Secure key storage | Store private keys with restricted permissions (0600) |
| Use cert-manager | Automate certificate lifecycle in Kubernetes |

**Certificate File Permissions:**

```bash
# Set secure permissions for TLS files
chmod 644 /etc/quicbaas/tls/ca.crt
chmod 644 /etc/quicbaas/tls/client.crt
chmod 600 /etc/quicbaas/tls/client.key
```

### Trace Sampling Strategies

Control trace volume with sampling:

| Strategy | Use Case | Configuration |
|----------|----------|---------------|
| Always On | Development/debugging | `OTEL_TRACES_SAMPLER=always_on` |
| Always Off | Disable tracing | `OTEL_TRACES_SAMPLER=always_off` |
| Ratio | Production (1% sample) | `OTEL_TRACES_SAMPLER=traceidratio` + `OTEL_TRACES_SAMPLER_ARG=0.01` |
| Parent-based | Follow upstream decision | `OTEL_TRACES_SAMPLER=parentbased_traceidratio` |

**Recommended Production Configuration:**

```bash
# Sample 1% of traces in production
export OTEL_TRACES_SAMPLER=parentbased_traceidratio
export OTEL_TRACES_SAMPLER_ARG=0.01
```

### Span Attributes

QuicBaaS traces include the following attributes:

| Attribute | Description |
|-----------|-------------|
| `http.method` | HTTP method (GET, POST, etc.) |
| `http.route` | URL path |
| `quicbaas.shard_id` | Target shard ID |
| `quicbaas.operation` | Operation type (get, set, delete) |
| `quicbaas.entity_type` | Entity type being accessed |

---

## Logging

QuicBaaS uses structured logging via Go's `log/slog` package for consistent, machine-parseable log output.

### Log Levels

| Level | Description | Use Case |
|-------|-------------|----------|
| `trace` | Very detailed debugging | Development only |
| `debug` | Detailed operational info | Troubleshooting |
| `info` | Normal operational events | Production default |
| `warn` | Potentially problematic situations | Alerts review |
| `error` | Error conditions | Investigation required |
| `fatal` | Unrecoverable errors | Service restart |
| `panic` | Critical failures | Immediate attention |

### Configuration

Configure logging via config file or environment variables:

```yaml
# quicbaas.yaml
logging:
  level: info
  format: json      # json or console
  output: stdout    # stdout, stderr, or file
  file: /var/log/quicbaas/quicbaas.log
  component: server
```

**Environment Variables:**

```bash
export QUICBAAS_LOG_LEVEL=info
export QUICBAAS_LOG_FORMAT=json
export QUICBAAS_LOG_OUTPUT=stdout
```

### Structured Logging Format

JSON format includes consistent fields for log aggregation:

```json
{
  "time": "2025-01-07T10:30:45.123Z",
  "level": "INFO",
  "msg": "Request completed",
  "component": "rest-api",
  "request_id": "abc123",
  "operation": "get",
  "entity_type": "user",
  "duration_ms": 12.5,
  "status": 200
}
```

**Key Log Fields:**

| Field | Description |
|-------|-------------|
| `time` | ISO 8601 timestamp |
| `level` | Log level |
| `msg` | Log message |
| `component` | QuicBaaS component |
| `request_id` | Request correlation ID |
| `error` | Error details (if applicable) |
| `source` | Source file and line |

### Log Aggregation Setup

**Fluentd Configuration:**

```xml
<source>
  @type tail
  path /var/log/quicbaas/*.log
  pos_file /var/log/td-agent/quicbaas.pos
  tag quicbaas
  <parse>
    @type json
    time_key time
    time_format %Y-%m-%dT%H:%M:%S.%LZ
  </parse>
</source>

<match quicbaas>
  @type elasticsearch
  host elasticsearch.logging
  port 9200
  index_name quicbaas-logs
  <buffer>
    @type file
    path /var/log/td-agent/buffer/quicbaas
    flush_interval 5s
  </buffer>
</match>
```

**Promtail (Loki) Configuration:**

```yaml
scrape_configs:
  - job_name: quicbaas
    static_configs:
      - targets: [localhost]
        labels:
          job: quicbaas
          __path__: /var/log/quicbaas/*.log
    pipeline_stages:
      - json:
          expressions:
            level: level
            component: component
            request_id: request_id
      - labels:
          level:
          component:
```

---

## Health Checks

QuicBaaS provides three health check endpoints for monitoring and orchestration integration.

### Liveness Endpoint

**Endpoint:** `GET /api/v1/health/live`

Checks if the server process is running and responsive. Used by orchestrators to determine if the container needs to be restarted.

**Success Response (200 OK):**

```json
{
  "status": "healthy",
  "timestamp": "2025-01-07T10:30:45Z",
  "uptime": "24h30m15s",
  "checks": {
    "server": {
      "status": "pass",
      "message": "server is running"
    }
  }
}
```

**Failure Response (503 Service Unavailable):**

```json
{
  "status": "unhealthy",
  "timestamp": "2025-01-07T10:30:45Z",
  "uptime": "24h30m15s",
  "checks": {
    "server": {
      "status": "fail",
      "message": "server is not alive"
    }
  }
}
```

### Readiness Endpoint

**Endpoint:** `GET /api/v1/health/ready`

Checks if the server is ready to accept traffic. Used by load balancers to determine if traffic should be routed to this instance.

**Success Response (200 OK):**

```json
{
  "status": "healthy",
  "timestamp": "2025-01-07T10:30:45Z",
  "uptime": "24h30m15s",
  "checks": {
    "service": {
      "status": "pass",
      "message": "service is ready"
    }
  }
}
```

**Not Ready Response (503 Service Unavailable):**

```json
{
  "status": "unhealthy",
  "timestamp": "2025-01-07T10:30:45Z",
  "uptime": "5s",
  "checks": {
    "service": {
      "status": "fail",
      "message": "Raft cluster not ready: waiting for leader election"
    }
  }
}
```

### Detailed Health Endpoint

**Endpoint:** `GET /api/v1/health`

Provides comprehensive health information including all subsystem checks.

**Response:**

```json
{
  "status": "healthy",
  "timestamp": "2025-01-07T10:30:45Z",
  "uptime": "24h30m15s",
  "checks": {
    "storage": {
      "status": "pass",
      "message": "storage engine healthy"
    },
    "raft": {
      "status": "pass",
      "message": "all shards have leaders"
    },
    "memory": {
      "status": "pass",
      "message": "memory usage within limits"
    }
  }
}
```

### Integration with Load Balancers

**Kubernetes Probes:**

```yaml
apiVersion: v1
kind: Pod
spec:
  containers:
    - name: quicbaas
      livenessProbe:
        httpGet:
          path: /api/v1/health/live
          port: 8080
        initialDelaySeconds: 10
        periodSeconds: 10
        timeoutSeconds: 5
        failureThreshold: 3
      readinessProbe:
        httpGet:
          path: /api/v1/health/ready
          port: 8080
        initialDelaySeconds: 5
        periodSeconds: 5
        timeoutSeconds: 3
        failureThreshold: 2
      startupProbe:
        httpGet:
          path: /api/v1/health/live
          port: 8080
        initialDelaySeconds: 0
        periodSeconds: 2
        failureThreshold: 30
```

**AWS ALB Target Group:**

```terraform
resource "aws_lb_target_group" "quicbaas" {
  name     = "quicbaas"
  port     = 8080
  protocol = "HTTP"
  vpc_id   = var.vpc_id

  health_check {
    enabled             = true
    path                = "/api/v1/health/ready"
    port                = "traffic-port"
    protocol            = "HTTP"
    healthy_threshold   = 2
    unhealthy_threshold = 3
    timeout             = 5
    interval            = 10
    matcher             = "200"
  }
}
```

**HAProxy Configuration:**

```haproxy
backend quicbaas
    option httpchk GET /api/v1/health/ready
    http-check expect status 200
    server node1 quicbaas-node1:8080 check inter 5s fall 3 rise 2
    server node2 quicbaas-node2:8080 check inter 5s fall 3 rise 2
    server node3 quicbaas-node3:8080 check inter 5s fall 3 rise 2
```

---

## Grafana Dashboards

QuicBaaS includes a pre-built Grafana dashboard for monitoring cluster health and performance.

### Recommended Panels

The dashboard includes the following panel categories:

**1. Storage Health Row**

- Disk Usage (GB) - Gauge showing current disk utilization
- WAL Size - Time series of write-ahead log growth
- Compaction Backlog - Pending compaction work
- L0 File Count - Level 0 SSTable files

**2. Request Performance Row**

- Request Rate - Operations per second by type
- Request Latency (P50/P95/P99) - Histogram quantiles
- Error Rate - Percentage of failed requests
- Concurrent Requests - Current in-flight requests

**3. Raft Consensus Row**

- Leader Status - Per-shard leader indicator
- Proposal Latency - Time to commit by shard
- Snapshot Size - Current snapshot sizes
- Leader Elections - Election frequency over time

**4. Resource Utilization Row**

- CPU Usage - Per-node CPU consumption
- Memory Usage - Heap and resident memory
- Goroutine Count - Active goroutines
- Open File Descriptors - FD utilization

**5. Cache Performance Row**

- Cache Hit Ratio - Hit rate percentage
- Cache Size - Memory utilization
- Eviction Rate - Items evicted per second

### Dashboard Import

Import the pre-built dashboard from the repository:

1. Open Grafana and navigate to Dashboards > Import
2. Upload the JSON file from `deployments/grafana/dashboards/quicbaas/quicbaas-performance.json`
3. Select your Prometheus data source
4. Click Import

### Custom Dashboard Configuration

To create custom panels, use these example queries:

**Request Rate Panel:**

```json
{
  "targets": [
    {
      "expr": "sum(rate(quicbaas_request_duration_seconds_count[5m])) by (operation)",
      "legendFormat": "{{operation}}"
    }
  ],
  "title": "Request Rate by Operation"
}
```

**Latency Heatmap Panel:**

```json
{
  "targets": [
    {
      "expr": "sum(rate(quicbaas_request_duration_seconds_bucket[5m])) by (le)",
      "format": "heatmap"
    }
  ],
  "title": "Request Latency Distribution"
}
```

**Cluster Health Stat Panel:**

```json
{
  "targets": [
    {
      "expr": "count(up{job=\"quicbaas\"} == 1)",
      "instant": true
    }
  ],
  "thresholds": {
    "steps": [
      {"color": "red", "value": 0},
      {"color": "yellow", "value": 2},
      {"color": "green", "value": 3}
    ]
  },
  "title": "Healthy Nodes"
}
```

### Dashboard Variables

Configure these template variables for flexible dashboards:

| Variable | Query | Description |
|----------|-------|-------------|
| `$instance` | `label_values(up{job="quicbaas"}, instance)` | Filter by node |
| `$shard` | `label_values(quicbaas_raft_leader_id, shard)` | Filter by shard |
| `$operation` | `label_values(quicbaas_request_duration_seconds_count, operation)` | Filter by operation |
| `$interval` | `1m,5m,15m,1h` | Aggregation interval |

---

## Quick Reference

### Essential PromQL Queries

```promql
# Cluster health score (0-100)
100 * count(up{job="quicbaas"} == 1) / count(up{job="quicbaas"})

# Overall request rate
sum(rate(quicbaas_request_duration_seconds_count[5m]))

# P99 latency across all operations
histogram_quantile(0.99, sum(rate(quicbaas_request_duration_seconds_bucket[5m])) by (le))

# Error rate percentage
100 * sum(rate(quicbaas_request_duration_seconds_count{status="error"}[5m])) /
sum(rate(quicbaas_request_duration_seconds_count[5m]))

# Storage growth rate (bytes/hour)
rate(quicbaas_storage_disk_usage_bytes[1h]) * 3600
```

### Port Reference

| Port | Protocol | Purpose |
|------|----------|---------|
| 8080 | HTTP | REST API, metrics, health checks |
| 50051 | gRPC | gRPC API |
| 63001 | TCP | Raft inter-node communication |
| 4317 | gRPC | OpenTelemetry collector (external) |

### File Locations

| Path | Description |
|------|-------------|
| `/api/v1/metrics/` | Prometheus metrics endpoint |
| `/api/v1/health` | Detailed health check |
| `/api/v1/health/live` | Liveness probe |
| `/api/v1/health/ready` | Readiness probe |
| `deployments/prometheus/` | Prometheus configuration examples |
| `deployments/grafana/dashboards/` | Grafana dashboard JSON |
