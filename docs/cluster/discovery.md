# Discovery Configuration

QuicBaaS supports multiple discovery methods for automatic peer detection and cluster formation. This guide covers configuration details for each method and multicast PSK authentication.

## Discovery Methods

### Static Discovery

Uses a pre-defined list of peers. Best for fixed-size clusters and testing.

```yaml
discovery:
  methods:
    - type: static
      peers:
        - "1:node1.example.com:8001"
        - "2:node2.example.com:8001"
        - "3:node3.example.com:8001"

cluster:
  min_quorum: 2
```

Advantages:
- Simple configuration
- No external dependencies
- Predictable behavior

Disadvantages:
- Requires configuration updates when adding/removing nodes
- Not suitable for dynamic environments

### DNS Discovery

Uses DNS SRV records for dynamic peer discovery. Recommended for production.

```yaml
discovery:
  methods:
    - type: dns
      srv_record: "_quicbaas._tcp.service.consul"
      refresh_interval: 30s
```

DNS SRV record format:

```
_quicbaas._tcp.service.consul. 300 IN SRV 0 0 8001 node1.service.consul.
_quicbaas._tcp.service.consul. 300 IN SRV 0 0 8001 node2.service.consul.
_quicbaas._tcp.service.consul. 300 IN SRV 0 0 8001 node3.service.consul.
```

Advantages:
- Dynamic node discovery
- Centralized configuration
- Works with service discovery systems (Consul, DNS, multicast)

Disadvantages:
- Requires DNS infrastructure
- Additional dependency

### Kubernetes Discovery

Native Kubernetes integration using StatefulSets and Headless Services.

```yaml
discovery:
  methods:
    - type: kubernetes
      namespace: default
      service: quicbaas
      refresh_interval: 30s
```

Kubernetes service configuration:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: quicbaas
spec:
  clusterIP: None  # Headless service
  selector:
    app: quicbaas
  ports:
    - port: 8001
      name: raft
```

Advantages:
- Native Kubernetes integration
- Automatic pod discovery
- Works with StatefulSets

Disadvantages:
- Kubernetes-only
- Requires RBAC permissions

### Multicast Discovery

LAN-based auto-discovery using UDP multicast. Best for development and edge deployments.

```yaml
discovery:
  methods:
    - type: multicast
      group: "239.255.77.77"
      port: 63777
      broadcast_interval: 2s
      peer_ttl: 10s
      cluster_secret: "optional-psk-for-security"
```

Advantages:
- Zero configuration
- Automatic node discovery
- Works on LAN without infrastructure

Disadvantages:
- LAN-only (no WAN/Internet)
- Network must support multicast
- Limited security without PSK

## Multicast PSK Authentication

Secure multicast discovery with Pre-Shared Key (PSK) authentication using HMAC-SHA256 signatures. This prevents unauthorized nodes from joining your cluster.

### Quick Start

Generate a secret:

```bash
SECRET=$(openssl rand -hex 32)
echo "cluster_secret: \"$SECRET\""
```

Configure all nodes with the same secret:

```yaml
discovery:
  methods:
    - type: multicast
      cluster_secret: "your-generated-secret-here"
```

Restart all nodes with the new configuration.

### Configuration Options

Production deployment (environment variable):

```yaml
discovery:
  methods:
    - type: multicast
      cluster_secret: "${CLUSTER_SECRET}"
```

Development (no authentication):

```yaml
discovery:
  methods:
    - type: multicast
      cluster_secret: ""  # Empty = no authentication
```

Multiple clusters on same network:

```yaml
# Cluster A
discovery:
  methods:
    - type: multicast
      group: "239.255.77.77"
      cluster_secret: "cluster-a-secret"

# Cluster B
discovery:
  methods:
    - type: multicast
      group: "239.255.77.78"
      cluster_secret: "cluster-b-secret"
```

### How It Works

When PSK is enabled:

1. Each discovery message is signed with HMAC-SHA256
2. Signature is prepended to the message payload
3. Receiving nodes verify the signature before processing
4. Invalid signatures are rejected silently

Wire protocol with PSK:

```
[magic:4][version:1][sig_len:2][signature:sig_len][json_payload]
```

Where:
- `magic`: 4 bytes, value `0x44524147` ("DRAG")
- `version`: 1 byte, protocol version (1)
- `sig_len`: 2 bytes (big-endian), signature length (32 for SHA256)
- `signature`: HMAC-SHA256 signature of the JSON payload
- `json_payload`: JSON-encoded discovery message

### Security Considerations

PSK authentication protects against:
- Unauthorized nodes joining the cluster
- Message spoofing and tampering
- Replay attacks (via timestamp validation)

PSK authentication does NOT protect against:
- Eavesdropping (messages are not encrypted)
- Network-level attacks (DDoS)
- Compromised secrets

For full confidentiality, combine with network-level encryption (VPN, WireGuard, IPsec).

### Best Practices

1. Use strong secrets (minimum 32 random bytes):

```bash
openssl rand -hex 32
```

2. Store secrets securely:
   - Use environment variables
   - Never commit secrets to version control
   - Set file permissions to 0600 for config files
   - Use secret management systems (Vault, K8s Secrets)

3. Rotate secrets periodically:
   - Plan maintenance window
   - Update all nodes simultaneously
   - Verify cluster health after rotation

4. Network isolation:
   - Use firewall rules to restrict multicast traffic
   - Implement network segmentation
   - Consider VPN for additional security

### Performance Impact

Benchmarks on 24-core system:

| Operation | Without PSK | With PSK | Overhead |
|-----------|-------------|----------|----------|
| Encode Message | 2.3 µs | 5.4 µs | +3.1 µs |
| Parse Message | 2.8 µs | 5.4 µs | +2.6 µs |

- Signing overhead: ~2.2 µs per message
- Verification overhead: ~2.5 µs per message
- Total round-trip overhead: ~5.5 µs
- Impact: Negligible for typical discovery intervals (2s default)

### Verification

Check that peers are discovered:

```bash
curl http://localhost:18080/v1/cluster/members | jq
quicbaas node list
```

Check logs for authentication issues:

```bash
journalctl -u quicbaas | grep "rejected invalid"
```

### Troubleshooting

Common issues and solutions:

| Symptom | Likely Cause | Solution |
|---------|--------------|----------|
| No peers discovered | Mismatched secrets | Verify all nodes use same secret |
| Some nodes missing | Mixed PSK/no-PSK | Enable PSK on all nodes |
| "rejected invalid" logs | Wrong secret on sender | Update sender configuration |
| Multicast not working | Network restrictions | Check firewall, router settings |

Debugging steps:

1. Verify same secret on all nodes:

```bash
grep cluster_secret /etc/quicbaas/config.yaml
```

2. Test multicast connectivity:

```bash
# Receiver
socat UDP4-RECV:63777,ip-add-membership=239.255.77.77:eth0,fork -

# Sender (from another node)
echo "test" | socat - UDP4-DATAGRAM:239.255.77.77:63777
```

3. Check logs for signature errors:

```bash
journalctl -u quicbaas --since "5 minutes ago" | grep signature
```

4. Verify network interface:

```bash
ip maddr show
ip route get 239.255.77.77
```

### Migration Guide

Enabling PSK on existing cluster:

Warning: All nodes must be updated simultaneously. PSK-enabled nodes reject unauthenticated messages.

1. Update configuration on all nodes with same secret
2. Restart all nodes within the peer TTL window (default 10s)
3. Verify cluster formation

```bash
# Check cluster status
quicbaas cluster status

# Verify all nodes present
quicbaas node list
```

Disabling PSK:

Remove or set `cluster_secret: ""` and restart nodes.

## Method Selection Guide

Choose discovery method based on deployment environment:

| Environment | Recommended Method | Alternative |
|-------------|-------------------|-------------|
| Kubernetes | kubernetes | dns |
| Cloud (AWS/GCP/Azure) | dns | static |
| On-premise datacenter | dns | static |
| Development/Testing | multicast | static |
| Edge/IoT | multicast + PSK | static |
| Fixed infrastructure | static | - |

## Configuration Examples

### Production (Kubernetes)

```yaml
discovery:
  methods:
    - type: kubernetes
      namespace: quicbaas
      service: quicbaas-headless
      refresh_interval: 30s

cluster:
  min_quorum: 3
```

### Production (Consul)

```yaml
discovery:
  methods:
    - type: dns
      srv_record: "_quicbaas._tcp.service.consul"
      refresh_interval: 30s

cluster:
  min_quorum: 3
```

### Development

```yaml
discovery:
  methods:
    - type: multicast
      group: "239.255.77.77"
      port: 63777
      cluster_secret: ""  # No auth for dev

cluster:
  min_quorum: 2
```

### Edge/IoT (Secure)

```yaml
discovery:
  methods:
    - type: multicast
      group: "239.255.77.77"
      port: 63777
      cluster_secret: "${CLUSTER_SECRET}"  # From environment

cluster:
  min_quorum: 2
```

## See Also

- [Cluster Management](README.md) - Cluster architecture and operations
- [Operations Guide](operations.md) - Day-to-day cluster management
- [CLI Reference - cluster](../cli/cluster.md) - Cluster commands
- [Example Configurations](../../examples/) - Sample configuration files

---

**Last Updated:** 2025-12-19
**Version:** 0.3.0
