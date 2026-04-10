# QuicBaaS IAM Deployment

## Deployment Models

QuicBaaS IAM supports multiple deployment patterns using the same binary with different configurations.

## Model 1: Integrated (Dev/Small Deployments)

Single node or cluster with storage + IAM combined.

```
                    ┌─────────────────────┐
                    │     QuicBaaS        │
                    │                     │
                    │  ┌───────────────┐  │
                    │  │  IAM Module   │  │
                    │  │   (enabled)   │  │
                    │  └───────────────┘  │
                    │  ┌───────────────┐  │
                    │  │    Storage    │  │
                    │  │   + Raft      │  │
                    │  └───────────────┘  │
                    └─────────────────────┘
                               │
               ┌───────────────┼───────────────┐
               ▼               ▼               ▼
         ┌──────────┐   ┌──────────┐   ┌──────────┐
         │ Service  │   │ Service  │   │  Users   │
         │    A     │   │    B     │   │          │
         └──────────┘   └──────────┘   └──────────┘
```

**Configuration:**
```yaml
# quicbaas.yaml
storage:
  enabled: true
  data_dir: /var/lib/quicbaas

iam:
  enabled: true
  mode: primary
```

**Use Cases:**
- Development environments
- Homelabs
- Small deployments (<1000 users)
- Single-region applications

## Model 2: Dedicated IAM Cluster (Enterprise)

Separate IAM cluster with data clusters connecting as PEP clients.

```
      ┌─────────────────────────────────────────────────────────┐
      │              QuicBaaS IAM Cluster                        │
      │           (3+ nodes, Raft consensus)                    │
      │                                                          │
      │  ┌─────────┐  ┌─────────┐  ┌─────────┐                 │
      │  │ Node 1  │  │ Node 2  │  │ Node 3  │                 │
      │  │IAM+Store│  │IAM+Store│  │IAM+Store│                 │
      │  └─────────┘  └─────────┘  └─────────┘                 │
      └──────────────────────────┬──────────────────────────────┘
                                 │
                   Policy sync (gRPC streams)
                                 │
      ┌──────────────────────────┼──────────────────────────┐
      │                          │                          │
      ▼                          ▼                          ▼
 ┌─────────────┐          ┌─────────────┐          ┌─────────────┐
 │  QuicBaaS   │          │  QuicBaaS   │          │   Other     │
 │Data Cluster │          │Data Cluster │          │  Services   │
 │   + PEP     │          │   + PEP     │          │   + PEP     │
 │             │          │             │          │             │
 │ IAM=remote  │          │ IAM=remote  │          │ (SDK/       │
 │             │          │             │          │  sidecar)   │
 └─────────────┘          └─────────────┘          └─────────────┘
```

**IAM Cluster Configuration:**
```yaml
# iam-cluster.yaml
storage:
  enabled: true
  data_dir: /var/lib/quicbaas-iam
  raft:
    cluster_id: "iam-prod"
    initial_members:
      - "iam-1.internal:9400"
      - "iam-2.internal:9400"
      - "iam-3.internal:9400"

iam:
  enabled: true
  mode: primary

  oidc:
    issuer: "https://iam.prod.example.com"

  pep:
    grpc_port: 9444
    sync_enabled: true
```

**Data Cluster Configuration:**
```yaml
# data-cluster.yaml
storage:
  enabled: true
  data_dir: /var/lib/quicbaas-data

iam:
  enabled: true
  mode: remote

  remote:
    endpoint: "iam-cluster.internal:9444"
    sync_interval: 30s
    tls:
      enabled: true
      ca_cert: /etc/quicbaas/certs/ca.pem
```

**Use Cases:**
- Enterprise deployments
- Multi-region applications
- High user counts (>10,000)
- Strict compliance requirements
- Dedicated IAM team

## Model 3: Hybrid

Mix of integrated and dedicated based on environment.

```
┌─────────────────────────────────────────────────────────────────┐
│                         PRODUCTION                               │
│                                                                  │
│   IAM Cluster ────────────▶ Data Clusters                       │
│   (dedicated)               (remote mode)                        │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                         STAGING                                  │
│                                                                  │
│   Single Node (integrated mode)                                 │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                         DEVELOPMENT                              │
│                                                                  │
│   Local Instance (integrated mode)                              │
└─────────────────────────────────────────────────────────────────┘
```

## Scaling Considerations

### IAM Cluster Sizing

| Users | Nodes | CPU | Memory | Storage |
|-------|-------|-----|--------|---------|
| <1,000 | 1-3 | 2 cores | 4 GB | 10 GB |
| 1,000-10,000 | 3 | 4 cores | 8 GB | 50 GB |
| 10,000-100,000 | 3-5 | 8 cores | 16 GB | 100 GB |
| >100,000 | 5-7 | 16 cores | 32 GB | 500 GB |

### PEP Connection Limits

```yaml
iam:
  pep:
    max_connections: 1000      # Per node
    connection_timeout: 30s
    max_message_size: 4MB
```

### Cache Tuning

```yaml
iam:
  abac:
    cache_size: 10000         # Compiled policies
    cache_ttl: 5m             # Cache lifetime

  sessions:
    max_per_user: 10          # Concurrent sessions
```

## High Availability

### IAM Cluster HA

1. Deploy 3+ nodes (odd number for Raft quorum)
2. Spread across availability zones
3. Use load balancer for OIDC endpoints
4. PEP clients auto-failover to healthy nodes

### Load Balancer Configuration

```nginx
# nginx.conf for IAM cluster
upstream iam_cluster {
    least_conn;
    server iam-1.internal:8443;
    server iam-2.internal:8443;
    server iam-3.internal:8443;
}

server {
    listen 443 ssl;
    server_name iam.example.com;

    location / {
        proxy_pass https://iam_cluster;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

## Kubernetes Deployment

### IAM StatefulSet

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: quicbaas-iam
spec:
  serviceName: quicbaas-iam
  replicas: 3
  selector:
    matchLabels:
      app: quicbaas-iam
  template:
    metadata:
      labels:
        app: quicbaas-iam
    spec:
      containers:
      - name: quicbaas
        image: quicbaas:latest
        ports:
        - containerPort: 8443  # REST/OIDC
        - containerPort: 9400  # Raft
        - containerPort: 9444  # PEP sync
        volumeMounts:
        - name: data
          mountPath: /var/lib/quicbaas
        - name: config
          mountPath: /etc/quicbaas
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 50Gi
```

### Data Cluster Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: quicbaas-data
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: quicbaas
        image: quicbaas:latest
        env:
        - name: QUICBAAS_IAM_MODE
          value: "remote"
        - name: QUICBAAS_IAM_ENDPOINT
          value: "quicbaas-iam.default.svc:9444"
```

## Monitoring

### Key Metrics

| Metric | Description | Alert Threshold |
|--------|-------------|-----------------|
| `iam_auth_latency_ms` | Authentication latency | >500ms |
| `iam_policy_eval_us` | Policy evaluation time | >10000μs |
| `iam_pep_connections` | Active PEP connections | Near max |
| `iam_cache_hit_ratio` | Policy cache hit rate | <80% |
| `iam_token_issued_total` | Tokens issued | Anomaly detection |

### Prometheus Config

```yaml
scrape_configs:
  - job_name: 'quicbaas-iam'
    static_configs:
      - targets: ['iam-1:9090', 'iam-2:9090', 'iam-3:9090']
```
