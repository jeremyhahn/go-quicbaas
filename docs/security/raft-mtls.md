# Raft mTLS

Mutual TLS authentication for securing Raft cluster communication.

## Overview

Raft mTLS provides:
- Encrypted communication between nodes
- Mutual authentication (both client and server verified)
- Protection against man-in-the-middle attacks
- Certificate-based identity verification

## Configuration

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

## Certificate Requirements

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
    DNS:node1.example.com, IP:192.168.1.10
```

## Certificate Generation

### Using OpenSSL

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
IP.1 = 192.168.1.10
EOF

# Generate CSR and sign
openssl req -new -key node1.key -out node1.csr -config node1.cnf
openssl x509 -req -days 365 -in node1.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out node1.crt -extensions v3_req -extfile node1.cnf
```

### Using cfssl

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

## CLI Usage

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

## Deployment Patterns

### Self-Signed CA (Development)

```yaml
raft:
  mtls:
    enabled: true
    cert_file: ./certs/raft.crt
    key_file: ./certs/raft.key
    ca_file: ./certs/ca.crt
```

### Enterprise PKI (Production)

```yaml
raft:
  mtls:
    enabled: true
    cert_file: /etc/pki/tls/certs/quicbaas-raft.crt
    key_file: /etc/pki/tls/private/quicbaas-raft.key
    ca_file: /etc/pki/ca-trust/source/anchors/enterprise-ca.crt
```

### Kubernetes with cert-manager

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

## Troubleshooting

### Certificate Verification Error
```
Error: x509: certificate signed by unknown authority
Solution: Ensure CA certificate is correctly configured on all nodes
```

### Hostname Mismatch
```
Error: x509: certificate is valid for node1.example.com, not node1
Solution: Add all hostnames/IPs to certificate SAN
```

### Expired Certificate
```
Error: x509: certificate has expired
Solution: Rotate certificates before expiration
```

## Security Best Practices

1. Use strong key sizes (RSA 2048+ or ECDSA P-256+)
2. Short certificate lifetime (annual rotation)
3. Protect private keys with file permissions
4. Use dedicated CA for Raft communication
5. Monitor certificate expiration
6. Plan for certificate revocation (CRL or OCSP)

## Performance

- mTLS adds ~1-2ms latency for connection establishment
- Session resumption reduces overhead
- TLS 1.3 is faster than TLS 1.2
- Hardware acceleration (AES-NI) improves throughput

## See Also

- [Cluster DEK](cluster-dek.md) - Data encryption
- [Security Overview](README.md)
