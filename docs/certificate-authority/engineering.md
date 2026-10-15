# Certificate Authority — Engineering

> **Status: NORMATIVE** · Last verified: 2026-10-14
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/certificate-authority/`.

QuicBaaS's TLS configuration for clients, servers and cluster nodes. The CA
that issues the certificates is go-xkms's (see [Certificates](#certificates)).

## Overview

### Where TLS is Used

QuicBaaS uses TLS encryption across multiple communication channels:

| Channel | Description | Configuration Section |
|---------|-------------|----------------------|
| Client-Server (REST) | HTTPS connections to the REST API | `security.tls_*` |
| Client-Server (gRPC) | Secure gRPC connections | `server.grpc_*` |
| Inter-Node Raft | Cluster consensus communication | `raft.mtls.*` |
| QUIC Transport | UDP-based encrypted transport | `server.quic_*` |
| Redis L2 Cache | TLS to external Redis cache | `cache.l2_*` |
| Tracing/Telemetry | Secure export of traces/metrics | Per-exporter config |

### Supported TLS Versions

QuicBaaS enforces a minimum of TLS 1.3 for all connections:

| Version | Support | Notes |
|---------|---------|-------|
| TLS 1.0 | Not supported | Deprecated, security vulnerabilities |
| TLS 1.1 | Not supported | Deprecated, security vulnerabilities |
| TLS 1.2 | Not supported | Superseded by TLS 1.3 minimum |
| TLS 1.3 | Supported | Minimum version, required for all connections |

### FIPS-Approved Cipher Suites

When FIPS mode is enabled (`security.fips_mode: true`), QuicBaaS restricts to FIPS 140-3 approved cipher suites:

**TLS 1.3 Cipher Suites:**
- `TLS_AES_128_GCM_SHA256`
- `TLS_AES_256_GCM_SHA384`
- `TLS_CHACHA20_POLY1305_SHA256`

**TLS 1.2 Cipher Suites (ECDHE preferred):**
- `TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256`
- `TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384`
- `TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256`
- `TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384`

**TLS 1.2 Cipher Suites (RSA key exchange):**
- `TLS_RSA_WITH_AES_128_GCM_SHA256`
- `TLS_RSA_WITH_AES_256_GCM_SHA384`

## Certificates

QuicBaaS does not implement a CA. Certificate issuance, profiles, key types,
key usages and SAN rules are go-xkms's and are documented once there:

- [Certificate Authority — Engineering](../../../go-xkms/docs/certificate-authority/engineering.md)
- [Certificate types](../../../go-xkms/docs/ca/certificate-types.md)
- [CA configuration](../../../go-xkms/docs/ca/configuration.md)

Who creates a private key, and where, is QuicBaaS's own rule:
[key-custody.md](key-custody.md).

> **Being removed.** `quicbaas ca issue` and `/api/v1/certificate/issue` run on
> QuicBaaS's own certificate service, a parallel implementation removed on the
> owner's word ([SPEC](../SPEC.md) row `certificate-authority`). Until the
> go-xkms CA re-export replaces it, the rotation steps below use `ca issue`;
> any certificate your CA issues with the right SANs works the same way.

The rest of this page is QuicBaaS's own TLS configuration: which keys load
which certificate on which listener.

## Server TLS Configuration

### gRPC TLS Settings

Configure TLS for the gRPC API:

```yaml
server:
  host: "0.0.0.0"
  grpc_port: 9090

security:
  tls_enabled: true
  tls_cert_file: "/etc/quicbaas/certs/server.crt"
  tls_key_file: "/etc/quicbaas/certs/server.key"
  tls_ca_file: "/etc/quicbaas/certs/ca.crt"  # For client verification
  tls_client_auth: false  # Set to true for mTLS
```

**CLI flags:** `quicbaasd` has no TLS flags. Set the keys above in the
configuration file and start with `quicbaasd start --config <file>`.

### REST API TLS Settings

The REST API uses the same TLS configuration as gRPC:

```yaml
server:
  host: "0.0.0.0"
  port: 8080  # HTTPS when TLS enabled

security:
  tls_enabled: true
  tls_cert_file: "/etc/quicbaas/certs/server.crt"
  tls_key_file: "/etc/quicbaas/certs/server.key"
  tls_ca_file: "/etc/quicbaas/certs/ca.crt"
  tls_client_auth: false
```

**Verify TLS is working:**

```bash
# Check server certificate
openssl s_client -connect localhost:8080 -servername quicbaas.example.com

# Make HTTPS request
curl --cacert /etc/quicbaas/certs/ca.crt https://localhost:8080/health
```

### QUIC TLS Settings

QUIC transport requires TLS 1.3:

```yaml
server:
  quic_enabled: true
  quic_port: 443

security:
  tls_enabled: true
  tls_cert_file: "/etc/quicbaas/certs/server.crt"
  tls_key_file: "/etc/quicbaas/certs/server.key"
```

QUIC automatically uses TLS 1.3 and does not support older versions.

## Mutual TLS (mTLS)

Mutual TLS requires both server and client to present certificates, providing stronger authentication.

### Client Certificate Requirements

Client certificates must:
1. Be signed by a trusted CA (configured in `tls_ca_file`)
2. Include `TLS Web Client Authentication` in Extended Key Usage
3. Not be expired or revoked
4. Match the identity expected by the server (if CN/SAN verification is enabled)

### Raft Inter-Node mTLS

Secure cluster communication with mutual TLS:

```yaml
raft:
  node_id: 1
  raft_address: "10.0.0.1:63000"
  listen_address: "0.0.0.0:63000"
  mutual_tls: true

  mtls:
    enabled: true
    cert_file: "/etc/quicbaas/certs/raft-node1.crt"
    key_file: "/etc/quicbaas/certs/raft-node1.key"
    ca_file: "/etc/quicbaas/certs/ca.crt"
    min_version: "1.3"

    # Optional: Restrict cipher suites
    cipher_suites:
      - TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384
      - TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
      - TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256
      - TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
```

**Certificate SANs for Raft nodes:**

Each node certificate should include:
- The node's hostname
- The node's IP address(es)
- Any DNS aliases used for discovery

Example for node1:
```
DNS.1 = node1.quicbaas.local
DNS.2 = quicbaas-0.quicbaas-headless.default.svc
IP.1 = 10.0.0.1
IP.2 = 203.0.113.10
```

### Configuring Client Certificate Verification

**Enable mTLS on server:**

```yaml
security:
  tls_enabled: true
  tls_cert_file: "/etc/quicbaas/certs/server.crt"
  tls_key_file: "/etc/quicbaas/certs/server.key"
  tls_ca_file: "/etc/quicbaas/certs/ca.crt"
  tls_client_auth: true  # Require client certificates
```

**Connect with client certificate:**

```bash
# Using curl
curl --cacert /etc/quicbaas/certs/ca.crt \
     --cert /etc/quicbaas/certs/client.crt \
     --key /etc/quicbaas/certs/client.key \
     https://quicbaas.example.com:8080/v1/kv/mykey

# Using quicbaas CLI
quicbaas --tls-enabled \
         --tls-ca=/etc/quicbaas/certs/ca.crt \
         --tls-cert=/etc/quicbaas/certs/client.crt \
         --tls-key=/etc/quicbaas/certs/client.key \
         get mykey
```

**Programmatic client with mTLS (Go):**

```go
import (
    "crypto/tls"
    "crypto/x509"
    "os"
)

func createMTLSConfig() (*tls.Config, error) {
    // Load client certificate
    cert, err := tls.LoadX509KeyPair("client.crt", "client.key")
    if err != nil {
        return nil, err
    }

    // Load CA certificate
    caCert, err := os.ReadFile("ca.crt")
    if err != nil {
        return nil, err
    }

    caCertPool := x509.NewCertPool()
    caCertPool.AppendCertsFromPEM(caCert)

    return &tls.Config{
        Certificates: []tls.Certificate{cert},
        RootCAs:      caCertPool,
        MinVersion:   tls.VersionTLS13,
    }, nil
}
```

## Certificate Rotation

### Planned Rotation Procedures

Certificate rotation should be performed before certificates expire. QuicBaaS supports online rotation without downtime.

**Step 1: Generate new certificates**

```bash
# Issue a new server certificate
quicbaas ca issue \
  --common-name "quicbaas-server" \
  --usage server \
  --dns localhost --dns quicbaas.example.com \
  --ip 127.0.0.1 --ip 192.0.2.10 \
  --validity-days 365 \
  --output json > server-cert-new.json
```

Write the certificate and private key PEM from that output to
`/etc/quicbaas/certs-new/server.crt` and `/etc/quicbaas/certs-new/server.key`.

**Step 2: Update configuration**

```yaml
security:
  tls_cert_file: "/etc/quicbaas/certs-new/server.crt"
  tls_key_file: "/etc/quicbaas/certs-new/server.key"
```

**Step 3: Reload server configuration**

There is no reload verb and `quicbaasd` does not reload on SIGHUP; restart it
(one node at a time in a cluster):

```bash
systemctl restart quicbaas
```

**Step 4: Verify new certificate**

```bash
# Check certificate expiration
openssl s_client -connect localhost:8080 < /dev/null 2>/dev/null | \
  openssl x509 -noout -dates
```

**Rolling rotation for Raft cluster:**

1. Generate new certificates for all nodes
2. Update and restart each node one at a time
3. Wait for cluster health before proceeding to next node
4. Verify all nodes are using new certificates

```bash
# Check cluster health before proceeding
quicbaas cluster status

# Rotate node 1
scp new-certs/* node1:/etc/quicbaas/certs/
ssh node1 'systemctl restart quicbaas'

# Wait and verify
sleep 10
quicbaas cluster status

# Repeat for remaining nodes
```

### Emergency Certificate Replacement

For compromised or expired certificates:

**Step 1: Generate emergency certificates**

```bash
# Quickly issue with short validity
quicbaas ca issue \
  --common-name "quicbaas-server-emergency" \
  --usage server \
  --dns localhost --dns quicbaas.example.com \
  --ip 127.0.0.1 --ip 192.0.2.10 \
  --validity-days 7 \
  --output json > server-cert-emergency.json
```

Write the certificate and private key PEM from that output to
`/etc/quicbaas/certs-emergency/server.crt` and
`/etc/quicbaas/certs-emergency/server.key`.

**Step 2: Replace the certificates and restart**

```bash
# Stop server
systemctl stop quicbaas

# Replace certificates
cp /etc/quicbaas/certs-emergency/* /etc/quicbaas/certs/

# Start server
systemctl start quicbaas
```

**Step 3: For compromised CA, regenerate entire chain**

```bash
# There is no CA-generation verb: the CA comes from the `ca` section of the
# configuration file at first boot. Replacing it is a configuration change.

# Generate new server and client certificates with new CA
# Distribute new CA to all clients
# Update all nodes and restart cluster
```

## Troubleshooting

### Common TLS Errors

**Error: `x509: certificate signed by unknown authority`**

Cause: Client does not trust the server's CA certificate.

Solution:
```bash
# Ensure CA certificate is correctly specified
curl --cacert /path/to/correct/ca.crt https://quicbaas:8080/health

# Or add CA to system trust store
sudo cp ca.crt /usr/local/share/ca-certificates/quicbaas-ca.crt
sudo update-ca-certificates
```

**Error: `x509: certificate is valid for X, not Y`**

Cause: Hostname/IP not in certificate's SAN.

Solution:
```bash
# Check certificate SANs
openssl x509 -in server.crt -noout -text | grep -A1 "Subject Alternative Name"

# Reissue the certificate with the correct SANs
quicbaas ca issue --common-name quicbaas-server --usage server \
  --dns correct-hostname.example.com --ip 192.0.2.10
```

**Error: `x509: certificate has expired or is not yet valid`**

Cause: System clock skew or expired certificate.

Solution:
```bash
# Check certificate validity dates
openssl x509 -in server.crt -noout -dates

# Check system time
date

# If clock skew, sync NTP
sudo ntpdate pool.ntp.org
```

**Error: `tls: bad certificate`**

Cause: Client certificate not trusted by server (mTLS) or malformed certificate.

Solution:
```bash
# Verify client certificate is signed by trusted CA
openssl verify -CAfile ca.crt client.crt

# Check certificate chain
openssl verify -verbose -CAfile ca.crt -untrusted intermediate.crt client.crt
```

**Error: `tls: protocol version not supported`**

Cause: Client using TLS version below minimum (1.3).

Solution:
```bash
# Force TLS 1.3 minimum on client
curl --tlsv1.3 --cacert ca.crt https://quicbaas:8080/health
```

### Certificate Validation Issues

**Validate certificate chain:**

```bash
# Check certificate details
openssl x509 -in server.crt -noout -text

# Verify chain
openssl verify -CAfile ca.crt server.crt

# Check key matches certificate
openssl x509 -noout -modulus -in server.crt | openssl md5
openssl rsa -noout -modulus -in server.key | openssl md5
# (For ECDSA: openssl ec -noout -text -in server.key)

# Test TLS connection
openssl s_client -connect localhost:8080 \
  -CAfile ca.crt \
  -servername quicbaas.example.com
```

**Debug TLS handshake:**

```bash
# Verbose connection test
openssl s_client -connect localhost:8080 \
  -CAfile ca.crt \
  -debug \
  -state
```

**Check server cipher suites:**

```bash
# List supported ciphers
nmap --script ssl-enum-ciphers -p 8080 localhost
```

**Monitor certificate expiration:**

```bash
# Check days until expiration
openssl x509 -in server.crt -noout -checkend 2592000
# Returns 0 if valid for 30+ days, 1 if expiring sooner

# Get exact expiration date
openssl x509 -in server.crt -noout -enddate
```

**QuicBaaS health check with TLS:**

```bash
# Health endpoint should work with TLS (there is no health verb)
curl --cacert /etc/quicbaas/certs/ca.crt \
     https://localhost:8080/health
```

## See Also

- [Key Custody](key-custody.md) - who creates a private key, where it lives, and the build gate that enforces it
- Raft mTLS - Detailed Raft cluster mTLS configuration
- Authentication - JWT-based authentication
- Security Overview - Security architecture overview
- KMS - Key management for data encryption
