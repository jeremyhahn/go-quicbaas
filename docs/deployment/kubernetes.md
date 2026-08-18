# Kubernetes Deployment Guide

Deploy QuicBaaS in Kubernetes for production-grade distributed key-value storage with multi-Raft consensus, strong consistency, and horizontal scalability.

## Prerequisites

### Kubernetes Version Requirements

| Component | Minimum Version | Recommended Version |
|-----------|-----------------|---------------------|
| Kubernetes | 1.25+ | 1.28+ |
| kubectl | 1.25+ | 1.28+ |
| Helm | 3.10+ | 3.14+ |

QuicBaaS requires Kubernetes features:
- StatefulSets with stable network identities
- PersistentVolumeClaims with dynamic provisioning
- Pod anti-affinity for node distribution
- Headless Services for Raft peer discovery

### Storage Class Requirements

QuicBaaS requires persistent storage with specific characteristics:

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: quicbaas-storage
provisioner: kubernetes.io/gce-pd  # Or your cloud provider
parameters:
  type: pd-ssd  # SSD recommended for performance
  fsType: ext4
reclaimPolicy: Retain  # Prevent accidental data loss
allowVolumeExpansion: true  # Enable online volume expansion
volumeBindingMode: WaitForFirstConsumer
```

**Storage requirements:**
- SSD storage for low-latency Raft consensus
- `allowVolumeExpansion: true` for future capacity needs
- `reclaimPolicy: Retain` for production workloads
- Minimum 10Gi per node (20Gi+ recommended for production)

### Network Policies

QuicBaaS requires specific network communication paths:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: quicbaas-network-policy
  namespace: quicbaas
spec:
  podSelector:
    matchLabels:
      app: quicbaas
  policyTypes:
    - Ingress
    - Egress
  ingress:
    # Raft peer communication (internal)
    - from:
        - podSelector:
            matchLabels:
              app: quicbaas
      ports:
        - protocol: TCP
          port: 63001  # Raft port
    # Client connections
    - from:
        - namespaceSelector: {}
      ports:
        - protocol: TCP
          port: 8080   # HTTP/REST API
        - protocol: TCP
          port: 9090   # gRPC API
    # Metrics scraping
    - from:
        - namespaceSelector:
            matchLabels:
              name: monitoring
      ports:
        - protocol: TCP
          port: 8080
  egress:
    # Raft peer communication
    - to:
        - podSelector:
            matchLabels:
              app: quicbaas
      ports:
        - protocol: TCP
          port: 63001
    # DNS resolution
    - to:
        - namespaceSelector: {}
          podSelector:
            matchLabels:
              k8s-app: kube-dns
      ports:
        - protocol: UDP
          port: 53
```

**Required ports:**

| Port | Protocol | Purpose |
|------|----------|---------|
| 63001 | TCP | Raft consensus (peer-to-peer) |
| 8080 | TCP | HTTP/REST API |
| 9090 | TCP | gRPC API |
| 2379 | TCP | etcd-compatible client API |
| 2380 | TCP | etcd-compatible peer API |

---

## Deployment Options

### Helm Chart Installation (Recommended)

The recommended approach for production deployments.

**Add the QuicBaaS Helm repository:**

```bash
# Add repository (when available)
helm repo add quicbaas https://charts.quicbaas.io
helm repo update
```

**Install with default values:**

```bash
helm install quicbaas quicbaas/quicbaas \
  --namespace quicbaas \
  --create-namespace
```

**Install with custom values:**

```bash
helm install quicbaas quicbaas/quicbaas \
  --namespace quicbaas \
  --create-namespace \
  --set replicaCount=5 \
  --set resources.requests.memory=2Gi \
  --set persistence.size=50Gi \
  --set tls.enabled=true
```

**Using a values file:**

```yaml
# values.yaml
replicaCount: 5

image:
  repository: quicbaas/quicbaas
  tag: latest
  pullPolicy: IfNotPresent

resources:
  requests:
    cpu: "500m"
    memory: "2Gi"
  limits:
    cpu: "2"
    memory: "4Gi"

persistence:
  enabled: true
  storageClass: "quicbaas-storage"
  size: 50Gi
  accessMode: ReadWriteOnce

tls:
  enabled: true
  certManager:
    enabled: true
    issuerRef:
      name: letsencrypt-prod
      kind: ClusterIssuer

raft:
  electionTimeout: "2s"
  heartbeatInterval: "200ms"
  snapshotEntries: 10000

metrics:
  enabled: true
  serviceMonitor:
    enabled: true
```

```bash
helm install quicbaas quicbaas/quicbaas \
  --namespace quicbaas \
  --create-namespace \
  -f values.yaml
```

### Manual Manifest Deployment

For environments without Helm or requiring full control over manifests.

**Create namespace:**

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: quicbaas
  labels:
    name: quicbaas
```

**Apply all manifests:**

```bash
kubectl apply -f namespace.yaml
kubectl apply -f rbac.yaml
kubectl apply -f configmap.yaml
kubectl apply -f headless-service.yaml
kubectl apply -f client-service.yaml
kubectl apply -f statefulset.yaml
```

### Operator-Based Deployment

For advanced lifecycle management, consider using a Kubernetes Operator pattern. While QuicBaaS does not currently ship with an operator, the StatefulSet approach with proper RBAC enables operator-like automation.

**Example CRD-style configuration (future roadmap):**

```yaml
apiVersion: quicbaas.io/v1alpha1
kind: QuicBaaSCluster
metadata:
  name: production
  namespace: quicbaas
spec:
  replicas: 5
  version: "1.0.0"
  storage:
    class: quicbaas-storage
    size: 100Gi
  resources:
    requests:
      cpu: "1"
      memory: "4Gi"
  tls:
    enabled: true
    issuerRef:
      name: cluster-ca
  backup:
    schedule: "0 2 * * *"
    retention: 7
```

---

## StatefulSet Configuration

QuicBaaS must be deployed as a StatefulSet for stable network identities required by Raft consensus.

### Complete StatefulSet Manifest

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: quicbaas
  namespace: quicbaas
  labels:
    app: quicbaas
    app.kubernetes.io/name: quicbaas
    app.kubernetes.io/component: database
spec:
  serviceName: quicbaas-headless
  replicas: 3  # Use odd numbers: 3, 5, 7
  podManagementPolicy: Parallel
  updateStrategy:
    type: RollingUpdate
  selector:
    matchLabels:
      app: quicbaas
  template:
    metadata:
      labels:
        app: quicbaas
        app.kubernetes.io/name: quicbaas
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "8080"
        prometheus.io/path: "/v1/metrics/"
    spec:
      serviceAccountName: quicbaas
      terminationGracePeriodSeconds: 60
      securityContext:
        runAsUser: 1000
        runAsGroup: 1000
        fsGroup: 1000
        runAsNonRoot: true

      # Pod anti-affinity for high availability
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchLabels:
                  app: quicbaas
              topologyKey: kubernetes.io/hostname
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                labelSelector:
                  matchLabels:
                    app: quicbaas
                topologyKey: topology.kubernetes.io/zone

      # Topology spread for zone distribution
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: topology.kubernetes.io/zone
          whenUnsatisfiable: ScheduleAnyway
          labelSelector:
            matchLabels:
              app: quicbaas

      containers:
        - name: quicbaas
          image: quicbaas/quicbaas:latest
          imagePullPolicy: IfNotPresent
          ports:
            - name: http
              containerPort: 8080
              protocol: TCP
            - name: grpc
              containerPort: 9090
              protocol: TCP
            - name: raft
              containerPort: 63001
              protocol: TCP
            - name: etcd-client
              containerPort: 2379
              protocol: TCP
            - name: etcd-peer
              containerPort: 2380
              protocol: TCP
          env:
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
            - name: QUICBAAS_NODE_ID
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: QUICBAAS_STORAGE_DATA_DIR
              value: "/data"
            - name: QUICBAAS_LOG_LEVEL
              value: "info"
            - name: QUICBAAS_LOG_FORMAT
              value: "json"
          args:
            - "--config=/etc/quicbaas/config.yaml"
          volumeMounts:
            - name: data
              mountPath: /data
            - name: config
              mountPath: /etc/quicbaas
              readOnly: true
            - name: tls
              mountPath: /etc/quicbaas/tls
              readOnly: true
          resources:
            requests:
              cpu: "500m"
              memory: "1Gi"
            limits:
              cpu: "2"
              memory: "4Gi"
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
          livenessProbe:
            httpGet:
              path: /v1/health
              port: 8080
            initialDelaySeconds: 30
            periodSeconds: 10
            timeoutSeconds: 5
            failureThreshold: 3
          readinessProbe:
            httpGet:
              path: /v1/ready
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 5
            timeoutSeconds: 3
            failureThreshold: 3
          startupProbe:
            httpGet:
              path: /v1/health
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 5
            timeoutSeconds: 3
            failureThreshold: 30

      volumes:
        - name: config
          configMap:
            name: quicbaas-config
        - name: tls
          secret:
            secretName: quicbaas-tls
            optional: true

  volumeClaimTemplates:
    - metadata:
        name: data
        labels:
          app: quicbaas
      spec:
        accessModes: ["ReadWriteOnce"]
        storageClassName: quicbaas-storage
        resources:
          requests:
            storage: 20Gi
```

### Pod Anti-Affinity Rules

Anti-affinity ensures QuicBaaS pods are distributed across nodes and availability zones:

```yaml
affinity:
  podAntiAffinity:
    # Hard requirement: never schedule two QuicBaaS pods on same node
    requiredDuringSchedulingIgnoredDuringExecution:
      - labelSelector:
          matchLabels:
            app: quicbaas
        topologyKey: kubernetes.io/hostname
    # Soft preference: distribute across availability zones
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchLabels:
              app: quicbaas
          topologyKey: topology.kubernetes.io/zone
```

### Persistent Volume Claims

The StatefulSet uses `volumeClaimTemplates` for automatic PVC creation:

```yaml
volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: ["ReadWriteOnce"]
      storageClassName: quicbaas-storage
      resources:
        requests:
          storage: 20Gi
```

**PVC naming convention:**
- `data-quicbaas-0`
- `data-quicbaas-1`
- `data-quicbaas-2`

### Resource Requests and Limits

| Environment | CPU Request | CPU Limit | Memory Request | Memory Limit |
|-------------|-------------|-----------|----------------|--------------|
| Development | 250m | 500m | 512Mi | 1Gi |
| Staging | 500m | 1 | 1Gi | 2Gi |
| Production | 1 | 4 | 2Gi | 8Gi |
| High-Performance | 2 | 8 | 4Gi | 16Gi |

---

## Service Configuration

### Headless Service for Raft Discovery

Required for StatefulSet DNS-based peer discovery:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: quicbaas-headless
  namespace: quicbaas
  labels:
    app: quicbaas
spec:
  type: ClusterIP
  clusterIP: None  # Headless service
  publishNotReadyAddresses: true  # Required for initial cluster formation
  selector:
    app: quicbaas
  ports:
    - name: raft
      port: 63001
      targetPort: 63001
      protocol: TCP
    - name: etcd-peer
      port: 2380
      targetPort: 2380
      protocol: TCP
```

**DNS entries created:**
- `quicbaas-0.quicbaas-headless.quicbaas.svc.cluster.local`
- `quicbaas-1.quicbaas-headless.quicbaas.svc.cluster.local`
- `quicbaas-2.quicbaas-headless.quicbaas.svc.cluster.local`

### Client Service (LoadBalancer/NodePort)

For external client access:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: quicbaas
  namespace: quicbaas
  labels:
    app: quicbaas
  annotations:
    # Cloud provider specific annotations
    service.beta.kubernetes.io/aws-load-balancer-type: nlb
    service.beta.kubernetes.io/aws-load-balancer-internal: "true"
spec:
  type: LoadBalancer
  selector:
    app: quicbaas
  ports:
    - name: http
      port: 8080
      targetPort: 8080
      protocol: TCP
    - name: grpc
      port: 9090
      targetPort: 9090
      protocol: TCP
    - name: etcd-client
      port: 2379
      targetPort: 2379
      protocol: TCP
```

**NodePort alternative:**

```yaml
apiVersion: v1
kind: Service
metadata:
  name: quicbaas-nodeport
  namespace: quicbaas
spec:
  type: NodePort
  selector:
    app: quicbaas
  ports:
    - name: http
      port: 8080
      targetPort: 8080
      nodePort: 30080
    - name: grpc
      port: 9090
      targetPort: 9090
      nodePort: 30090
```

### Ingress Configuration

For HTTP/HTTPS access via Ingress controller:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: quicbaas
  namespace: quicbaas
  annotations:
    kubernetes.io/ingress.class: nginx
    nginx.ingress.kubernetes.io/backend-protocol: "HTTP"
    nginx.ingress.kubernetes.io/proxy-body-size: "100m"
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  tls:
    - hosts:
        - quicbaas.example.com
      secretName: quicbaas-ingress-tls
  rules:
    - host: quicbaas.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: quicbaas
                port:
                  number: 8080
```

**gRPC Ingress (requires gRPC-capable ingress controller):**

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: quicbaas-grpc
  namespace: quicbaas
  annotations:
    kubernetes.io/ingress.class: nginx
    nginx.ingress.kubernetes.io/backend-protocol: "GRPC"
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
spec:
  tls:
    - hosts:
        - grpc.quicbaas.example.com
      secretName: quicbaas-grpc-tls
  rules:
    - host: grpc.quicbaas.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: quicbaas
                port:
                  number: 9090
```

---

## TLS in Kubernetes

### Using cert-manager

cert-manager automates certificate provisioning and renewal.

**Install cert-manager:**

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.0/cert-manager.yaml
```

**Create a ClusterIssuer for internal CA:**

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: quicbaas-ca-issuer
spec:
  ca:
    secretName: quicbaas-ca-secret
```

**Generate the CA secret:**

```bash
# Generate CA key and certificate
openssl genrsa -out ca.key 4096
openssl req -new -x509 -days 3650 -key ca.key -out ca.crt \
  -subj "/CN=QuicBaaS Cluster CA/O=QuicBaaS"

# Create Kubernetes secret
kubectl create secret tls quicbaas-ca-secret \
  --cert=ca.crt \
  --key=ca.key \
  -n cert-manager
```

**Certificate for client connections:**

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: quicbaas-client-tls
  namespace: quicbaas
spec:
  secretName: quicbaas-client-tls
  issuerRef:
    name: quicbaas-ca-issuer
    kind: ClusterIssuer
  commonName: quicbaas
  dnsNames:
    - quicbaas
    - quicbaas.quicbaas.svc
    - quicbaas.quicbaas.svc.cluster.local
    - "*.quicbaas-headless.quicbaas.svc.cluster.local"
  usages:
    - digital signature
    - key encipherment
    - server auth
    - client auth
  duration: 8760h  # 1 year
  renewBefore: 720h  # Renew 30 days before expiry
```

**Certificate for Raft mTLS:**

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: quicbaas-raft-tls
  namespace: quicbaas
spec:
  secretName: quicbaas-raft-tls
  issuerRef:
    name: quicbaas-ca-issuer
    kind: ClusterIssuer
  commonName: quicbaas-raft
  dnsNames:
    - quicbaas-0.quicbaas-headless.quicbaas.svc.cluster.local
    - quicbaas-1.quicbaas-headless.quicbaas.svc.cluster.local
    - quicbaas-2.quicbaas-headless.quicbaas.svc.cluster.local
    - quicbaas-3.quicbaas-headless.quicbaas.svc.cluster.local
    - quicbaas-4.quicbaas-headless.quicbaas.svc.cluster.local
  usages:
    - digital signature
    - key encipherment
    - server auth
    - client auth
  duration: 8760h
  renewBefore: 720h
```

### Secret Management for Certificates

Mount TLS secrets in the StatefulSet:

```yaml
volumes:
  - name: tls
    secret:
      secretName: quicbaas-client-tls
      items:
        - key: tls.crt
          path: server.crt
        - key: tls.key
          path: server.key
        - key: ca.crt
          path: ca.crt
  - name: raft-tls
    secret:
      secretName: quicbaas-raft-tls
      items:
        - key: tls.crt
          path: raft.crt
        - key: tls.key
          path: raft.key
        - key: ca.crt
          path: ca.crt

volumeMounts:
  - name: tls
    mountPath: /etc/quicbaas/tls
    readOnly: true
  - name: raft-tls
    mountPath: /etc/quicbaas/raft-tls
    readOnly: true
```

### mTLS Between Pods

Configure QuicBaaS for Raft mTLS:

```yaml
# ConfigMap for QuicBaaS configuration
apiVersion: v1
kind: ConfigMap
metadata:
  name: quicbaas-config
  namespace: quicbaas
data:
  config.yaml: |
    server:
      http_address: "0.0.0.0:8080"
      grpc_address: "0.0.0.0:9090"
      tls:
        enabled: true
        cert_file: /etc/quicbaas/tls/server.crt
        key_file: /etc/quicbaas/tls/server.key
        ca_file: /etc/quicbaas/tls/ca.crt
        min_version: "1.2"

    raft:
      address: "0.0.0.0:63001"
      mtls:
        enabled: true
        cert_file: /etc/quicbaas/raft-tls/raft.crt
        key_file: /etc/quicbaas/raft-tls/raft.key
        ca_file: /etc/quicbaas/raft-tls/ca.crt
        min_version: "1.2"

    discovery:
      type: kubernetes
      kubernetes:
        namespace: quicbaas
        service_name: quicbaas-headless
        port_name: raft
        label_selector: "app=quicbaas"
```

---

## Secrets Management

### Using QuicBaaS Secrets Fetch

QuicBaaS provides a secrets fetch CLI for injecting secrets into application pods.

**Init container pattern:**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  template:
    spec:
      serviceAccountName: myapp
      initContainers:
        - name: fetch-secrets
          image: quicbaas/quicbaas:latest
          command:
            - quicbaas
            - secrets
            - fetch
            - apps/myapp/
            - --output-dir
            - /secrets
            - --format
            - file
            - --flatten
          env:
            - name: QUICBAAS_ENDPOINT
              value: "quicbaas.quicbaas.svc:9090"
            - name: QUICBAAS_TOKEN
              valueFrom:
                secretKeyRef:
                  name: quicbaas-credentials
                  key: token
          volumeMounts:
            - name: secrets
              mountPath: /secrets
          securityContext:
            runAsNonRoot: true
            runAsUser: 65534
            readOnlyRootFilesystem: true
            allowPrivilegeEscalation: false
          resources:
            requests:
              cpu: "50m"
              memory: "32Mi"
            limits:
              cpu: "100m"
              memory: "64Mi"

      containers:
        - name: myapp
          image: myapp:latest
          volumeMounts:
            - name: secrets
              mountPath: /secrets
              readOnly: true

      volumes:
        - name: secrets
          emptyDir:
            medium: Memory  # RAM-backed for security
            sizeLimit: 10Mi
```

### Sidecar Pattern for Dynamic Updates

For applications requiring real-time secret updates:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
spec:
  template:
    spec:
      initContainers:
        - name: fetch-secrets
          image: quicbaas/quicbaas:latest
          command:
            - quicbaas
            - secrets
            - fetch
            - apps/myapp/
            - --output-dir
            - /secrets
          env:
            - name: QUICBAAS_ENDPOINT
              value: "quicbaas.quicbaas.svc:9090"
            - name: QUICBAAS_TOKEN
              valueFrom:
                secretKeyRef:
                  name: quicbaas-credentials
                  key: token
          volumeMounts:
            - name: secrets
              mountPath: /secrets

      containers:
        - name: myapp
          image: myapp:latest
          volumeMounts:
            - name: secrets
              mountPath: /secrets
              readOnly: true

        - name: secrets-sidecar
          image: quicbaas/quicbaas:latest
          command:
            - quicbaas
            - secrets
            - watch
            - apps/myapp/
            - --output-dir
            - /secrets
            - --refresh-interval
            - "30s"
          env:
            - name: QUICBAAS_ENDPOINT
              value: "quicbaas.quicbaas.svc:9090"
            - name: QUICBAAS_TOKEN
              valueFrom:
                secretKeyRef:
                  name: quicbaas-credentials
                  key: token
          volumeMounts:
            - name: secrets
              mountPath: /secrets
          resources:
            requests:
              cpu: "25m"
              memory: "16Mi"
            limits:
              cpu: "50m"
              memory: "32Mi"

      volumes:
        - name: secrets
          emptyDir:
            medium: Memory
            sizeLimit: 10Mi
```

### Helm Chart for Secrets Integration

Use the provided Helm chart for simplified secrets integration:

```bash
helm install quicbaas-secrets ./deployments/helm/quicbaas-k8s-secrets \
  --namespace myapp \
  --set quicbaas.endpoint=quicbaas.quicbaas.svc:9090 \
  --set quicbaas.auth.method=token \
  --set quicbaas.auth.token.secretRef.name=quicbaas-credentials \
  --set quicbaas.auth.token.secretRef.key=token \
  --set secrets.prefix=apps/myapp/ \
  --set initContainer.enabled=true \
  --set sidecar.enabled=true
```

---

## Monitoring in Kubernetes

### ServiceMonitor for Prometheus

If using Prometheus Operator:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: quicbaas
  namespace: quicbaas
  labels:
    app: quicbaas
    release: prometheus  # Match your Prometheus Operator release
spec:
  selector:
    matchLabels:
      app: quicbaas
  namespaceSelector:
    matchNames:
      - quicbaas
  endpoints:
    - port: http
      path: /v1/metrics/
      interval: 15s
      scrapeTimeout: 10s
      scheme: http
      relabelings:
        - sourceLabels: [__meta_kubernetes_pod_name]
          targetLabel: pod
        - sourceLabels: [__meta_kubernetes_namespace]
          targetLabel: namespace
```

### Pod Annotations for Metrics Scraping

For Prometheus without the Operator:

```yaml
metadata:
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "8080"
    prometheus.io/path: "/v1/metrics/"
```

### PrometheusRule for Alerts

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: quicbaas-alerts
  namespace: quicbaas
  labels:
    app: quicbaas
    release: prometheus
spec:
  groups:
    - name: quicbaas.availability
      interval: 30s
      rules:
        - alert: QuicBaaSNodeDown
          expr: up{job="quicbaas"} == 0
          for: 1m
          labels:
            severity: critical
          annotations:
            summary: "QuicBaaS node {{ $labels.pod }} is down"
            description: "QuicBaaS node {{ $labels.pod }} has been down for more than 1 minute."

        - alert: QuicBaaSNoLeader
          expr: quicbaas_raft_leader_id == 0
          for: 30s
          labels:
            severity: critical
          annotations:
            summary: "Raft shard {{ $labels.shard }} has no leader"
            description: "Shard {{ $labels.shard }} has been without a leader for more than 30 seconds."

        - alert: QuicBaaSQuorumLost
          expr: count(up{job="quicbaas"} == 1) < (count(up{job="quicbaas"}) / 2 + 1)
          for: 30s
          labels:
            severity: critical
          annotations:
            summary: "QuicBaaS cluster has lost quorum"
            description: "Cluster has {{ $value }} healthy nodes, below quorum threshold."

    - name: quicbaas.performance
      interval: 1m
      rules:
        - alert: QuicBaaSHighLatency
          expr: histogram_quantile(0.99, rate(quicbaas_request_duration_seconds_bucket[5m])) > 0.1
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "High request latency detected"
            description: "P99 latency is {{ $value | humanizeDuration }} (threshold: 100ms)."

        - alert: QuicBaaSHighErrorRate
          expr: (rate(quicbaas_request_duration_seconds_count{status="error"}[5m]) / rate(quicbaas_request_duration_seconds_count[5m])) > 0.05
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "High error rate detected"
            description: "Error rate is {{ $value | humanizePercentage }} (threshold: 5%)."

    - name: quicbaas.resources
      interval: 1m
      rules:
        - alert: QuicBaaSHighMemory
          expr: container_memory_usage_bytes{container="quicbaas"} / container_spec_memory_limit_bytes{container="quicbaas"} > 0.9
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "High memory usage on {{ $labels.pod }}"
            description: "Memory usage is {{ $value | humanizePercentage }} of limit."

        - alert: QuicBaaSPVCNearFull
          expr: kubelet_volume_stats_used_bytes{persistentvolumeclaim=~"data-quicbaas-.*"} / kubelet_volume_stats_capacity_bytes > 0.85
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "PVC {{ $labels.persistentvolumeclaim }} is nearly full"
            description: "PVC usage is {{ $value | humanizePercentage }} of capacity."
```

### Grafana Dashboard ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: quicbaas-grafana-dashboard
  namespace: monitoring
  labels:
    grafana_dashboard: "1"
data:
  quicbaas.json: |
    {
      "dashboard": {
        "title": "QuicBaaS Cluster",
        "panels": [
          {
            "title": "Request Rate",
            "type": "graph",
            "targets": [
              {
                "expr": "rate(quicbaas_request_duration_seconds_count[5m])",
                "legendFormat": "{{pod}} - {{operation}}"
              }
            ]
          },
          {
            "title": "P99 Latency",
            "type": "graph",
            "targets": [
              {
                "expr": "histogram_quantile(0.99, rate(quicbaas_request_duration_seconds_bucket[5m]))",
                "legendFormat": "{{pod}} - {{operation}}"
              }
            ]
          },
          {
            "title": "Raft Leader Status",
            "type": "stat",
            "targets": [
              {
                "expr": "quicbaas_raft_is_leader",
                "legendFormat": "{{pod}}"
              }
            ]
          }
        ]
      }
    }
```

---

## Scaling Considerations

### Adding Nodes

To scale up the cluster:

```bash
# Scale StatefulSet
kubectl scale statefulset quicbaas --replicas=5 -n quicbaas

# Verify new pods
kubectl get pods -n quicbaas -w

# Check cluster membership
kubectl exec -n quicbaas quicbaas-0 -- quicbaas cluster status
```

**Important considerations:**
- Always use odd replica counts (3, 5, 7) for proper Raft quorum
- New nodes automatically join the cluster via Kubernetes discovery
- Data rebalancing happens automatically

### Removing Nodes

To scale down safely:

```bash
# First, gracefully remove from Raft cluster
kubectl exec -n quicbaas quicbaas-0 -- quicbaas node remove quicbaas-4

# Wait for data migration
kubectl exec -n quicbaas quicbaas-0 -- quicbaas rebalance status

# Then scale down StatefulSet
kubectl scale statefulset quicbaas --replicas=3 -n quicbaas

# Optionally delete orphaned PVCs
kubectl delete pvc data-quicbaas-3 data-quicbaas-4 -n quicbaas
```

### Storage Expansion

To expand persistent storage:

```bash
# Verify StorageClass supports expansion
kubectl get storageclass quicbaas-storage -o jsonpath='{.allowVolumeExpansion}'

# Edit each PVC
kubectl patch pvc data-quicbaas-0 -n quicbaas \
  -p '{"spec":{"resources":{"requests":{"storage":"100Gi"}}}}'

kubectl patch pvc data-quicbaas-1 -n quicbaas \
  -p '{"spec":{"resources":{"requests":{"storage":"100Gi"}}}}'

kubectl patch pvc data-quicbaas-2 -n quicbaas \
  -p '{"spec":{"resources":{"requests":{"storage":"100Gi"}}}}'

# Some storage providers require pod restart
kubectl rollout restart statefulset quicbaas -n quicbaas
```

### Horizontal Pod Autoscaling

QuicBaaS does not support HPA due to Raft consensus requirements. Cluster size must be managed manually with odd replica counts.

For read-heavy workloads, consider:
- Read replicas with stale reads
- Caching layers (Redis, Memcached)
- Application-level caching

### Rolling Updates

Update QuicBaaS with zero downtime:

```bash
# Update image
kubectl set image statefulset/quicbaas quicbaas=quicbaas/quicbaas:v1.1.0 -n quicbaas

# Monitor rollout
kubectl rollout status statefulset quicbaas -n quicbaas

# Rollback if needed
kubectl rollout undo statefulset quicbaas -n quicbaas
```

**Update strategy:**

```yaml
updateStrategy:
  type: RollingUpdate
  rollingUpdate:
    partition: 0  # Update all pods
```

---

## RBAC Configuration

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: quicbaas
  namespace: quicbaas

---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: quicbaas
  namespace: quicbaas
rules:
  - apiGroups: [""]
    resources: ["endpoints", "pods"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: quicbaas
  namespace: quicbaas
subjects:
  - kind: ServiceAccount
    name: quicbaas
    namespace: quicbaas
roleRef:
  kind: Role
  name: quicbaas
  apiGroup: rbac.authorization.k8s.io
```

---

## Best Practices

### Production Checklist

- [ ] Use odd replica count (3, 5, or 7 nodes)
- [ ] Configure pod anti-affinity for node distribution
- [ ] Enable TLS for client connections
- [ ] Enable mTLS for Raft communication
- [ ] Use SSD-backed persistent storage
- [ ] Set appropriate resource requests and limits
- [ ] Configure PodDisruptionBudget
- [ ] Enable monitoring with ServiceMonitor
- [ ] Set up alerting rules
- [ ] Configure regular backups
- [ ] Test disaster recovery procedures
- [ ] Document operational runbooks

### PodDisruptionBudget

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: quicbaas
  namespace: quicbaas
spec:
  minAvailable: 2  # Maintain quorum during disruptions
  selector:
    matchLabels:
      app: quicbaas
```

### Resource Quotas

```yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: quicbaas-quota
  namespace: quicbaas
spec:
  hard:
    requests.cpu: "10"
    requests.memory: "20Gi"
    limits.cpu: "20"
    limits.memory: "40Gi"
    persistentvolumeclaims: "10"
    requests.storage: "500Gi"
```

---

## Troubleshooting

### Common Issues

**Pods stuck in Pending:**
```bash
kubectl describe pod quicbaas-0 -n quicbaas
# Check for PVC binding, node affinity, or resource constraints
```

**Cluster not forming:**
```bash
# Check logs
kubectl logs quicbaas-0 -n quicbaas

# Verify DNS resolution
kubectl exec -n quicbaas quicbaas-0 -- nslookup quicbaas-headless

# Check network connectivity
kubectl exec -n quicbaas quicbaas-0 -- nc -zv quicbaas-1.quicbaas-headless 63001
```

**Certificate issues:**
```bash
# Check certificate validity
kubectl get certificate -n quicbaas
kubectl describe certificate quicbaas-tls -n quicbaas

# View certificate details
kubectl get secret quicbaas-tls -n quicbaas -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -text -noout
```

**Performance issues:**
```bash
# Check resource usage
kubectl top pods -n quicbaas

# View metrics
kubectl port-forward svc/quicbaas 8080:8080 -n quicbaas
curl http://localhost:8080/v1/metrics/
```

---

## See Also

- [Cluster Management](../cluster/user.md) - Cluster operations and configuration
- Raft Consensus - Raft implementation details
- [Security Guide](../security-hardening/user.md) - Security configuration
- [Secrets Engine](../secrets/user.md) - Secrets management
- CLI Reference - Command-line tools
