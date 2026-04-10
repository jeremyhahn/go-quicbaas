# QuicBaaS Kubernetes Integration

QuicBaaS provides a simple, cross-platform secrets solution that works with Kubernetes and any other container runtime.

## Overview

| Component | Description | Status |
|-----------|-------------|--------|
| **Service Discovery** | Kubernetes-native node discovery | ✅ Implemented |
| **Secrets Fetch CLI** | Init container/sidecar for secret injection | ✅ Implemented |
| **Helm Chart** | Kubernetes deployment templates | ✅ Implemented |
| **Path Encryption** | Transparent encryption at rest | ✅ Implemented |

## Secrets Integration

QuicBaaS uses a simple CLI-based approach that works identically across Docker, Podman, and Kubernetes:

```yaml
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
    volumeMounts:
      - name: secrets
        mountPath: /secrets
        readOnly: true

volumes:
  - name: secrets
    emptyDir:
      medium: Memory  # RAM-backed for security
```

See [Container Secrets Integration](./ROADMAP.md) for complete documentation.

## Kubernetes Service Discovery

QuicBaaS can discover cluster nodes using Kubernetes APIs:

```yaml
# quicbaas.yaml
discovery:
  type: kubernetes
  kubernetes:
    namespace: quicbaas
    service_name: quicbaas-headless
    port_name: raft
    label_selector: "app=quicbaas"
```

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/discovery"

disc := discovery.NewKubernetesDiscovery(config, logger)
nodes, err := disc.Discover(ctx)
```

See `pkg/discovery/kubernetes.go` for the complete implementation.

## Deployment

### Helm Chart (Recommended)

```bash
# Install QuicBaaS secrets integration
helm install quicbaas-secrets ./deployments/helm/quicbaas-k8s-secrets \
  --set quicbaas.endpoint=quicbaas.default.svc:9090 \
  --set secrets.prefix=apps/myapp/
```

### StatefulSet

QuicBaaS should be deployed as a StatefulSet for stable network identities:

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: quicbaas
spec:
  serviceName: quicbaas-headless
  replicas: 3
  selector:
    matchLabels:
      app: quicbaas
  template:
    spec:
      containers:
      - name: quicbaas
        image: quicbaas/quicbaas:latest
        ports:
        - containerPort: 2379  # Client
        - containerPort: 2380  # Peer
        - containerPort: 8080  # REST API
        - containerPort: 9090  # gRPC
```

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `QUICBAAS_TOKEN` | Authentication token | Required |
| `QUICBAAS_K8S_NAMESPACE` | Kubernetes namespace | `default` |
| `QUICBAAS_K8S_SERVICE_ACCOUNT` | Service account for auth | `quicbaas` |
| `QUICBAAS_K8S_DISCOVERY_ENABLED` | Enable K8s discovery | `true` |

### RBAC Requirements

QuicBaaS needs minimal RBAC permissions for discovery:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: quicbaas-discovery
rules:
- apiGroups: [""]
  resources: ["endpoints", "pods"]
  verbs: ["get", "list", "watch"]
```

## Security Considerations

1. **Memory-backed volumes**: Use `emptyDir` with `medium: Memory` for secrets
2. **Service Account Tokens**: Use projected volumes for short-lived tokens
3. **Network Policies**: Restrict cluster communication to QuicBaaS namespace
4. **Pod Security**: Run QuicBaaS with non-root user
5. **Path Encryption**: Enable path-based encryption for secrets at rest

## Related Documentation

- [Container Secrets Integration](./ROADMAP.md)
- [Dynamic Secrets](../secrets/dynamic.md)
- [Security Best Practices](../security/README.md)
