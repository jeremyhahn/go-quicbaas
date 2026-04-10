# Cluster Bootstrap

Automatic cluster discovery and bootstrap for QuicBaaS.

## Overview

QuicBaaS provides automatic cluster discovery eliminating manual peer configuration. The system supports multiple discovery methods for different deployment environments.

## Discovery Methods

| Method | Complexity | Use Case | Dynamic |
|--------|-----------|----------|---------|
| Static | Low | Fixed clusters, testing | No |
| DNS | Medium | Production with DNS | Yes |
| Kubernetes | Medium | K8s deployments | Yes |
| Multicast | Low | Development, LAN | Yes |

## Quick Start

### 3-Node Cluster with Static Discovery

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

## Static Discovery

Pre-defined peer list for fixed clusters.

```yaml
discovery:
  methods:
    - type: static
      peers:
        - "1:192.168.1.100:8001"
        - "2:192.168.1.101:8001"
        - "3:192.168.1.102:8001"
```

### Adding a Node

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

## DNS Discovery

Dynamic peer discovery using DNS SRV records.

```yaml
discovery:
  methods:
    - type: dns
      srv_record: "_quicbaas._tcp.service.consul"
      refresh_interval: 30s
```

### Consul Integration

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

### AWS Route 53

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

## Kubernetes Discovery

Native Kubernetes deployment using StatefulSets.

### ConfigMap

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

### Headless Service

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

### StatefulSet

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

### Scaling

```bash
kubectl scale statefulset quicbaas -n quicbaas --replicas=5
kubectl scale statefulset quicbaas -n quicbaas --replicas=3
```

Important: Always maintain quorum.

### Production Recommendations

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

## Troubleshooting

### Discovery Fails

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

### Quorum Not Reached

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

### Member Registration Fails

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

### Split-Brain

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

## Recovery Procedures

### Single Node Failure

```bash
curl http://node1:18080/v1/cluster/members
systemctl restart quicbaas
journalctl -u quicbaas -f
```

### Quorum Lost

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

### Network Partition Healed

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

## Best Practices

### Production Deployment

- Use odd number of nodes (3, 5, 7)
- Use DNS discovery for dynamic environments
- Configure health checks and monitoring
- Use persistent storage volumes
- Set resource limits
- Enable TLS
- Implement regular backups

### Cluster Sizing

- 3 nodes: Tolerates 1 failure (minimum production)
- 5 nodes: Tolerates 2 failures (recommended)
- 7 nodes: Tolerates 3 failures (high availability)

Formula: Tolerates `(N-1)/2` failures

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

### Security

```yaml
security:
  tls:
    enabled: true
    cert_file: /etc/quicbaas/tls/server.crt
    key_file: /etc/quicbaas/tls/server.key
    ca_file: /etc/quicbaas/tls/ca.crt
```

## See Also

- [Cluster Management](README.md) - Cluster architecture
- [Operations Guide](operations.md) - Day-to-day operations
- [Discovery Configuration](discovery.md) - Discovery methods

---

**Last Updated:** 2025-12-19
