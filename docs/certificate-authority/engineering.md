# Certificate Authority — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/certificate-authority/`.

Comprehensive guide for configuring Transport Layer Security (TLS) in QuicBaaS to secure communications between clients, servers, and cluster nodes.

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

## Certificate Generation

### Using the Built-in Certificate Generator

QuicBaaS includes a built-in certificate provider for development and standalone deployments.

**Via CLI:**

```bash
# Generate a CA certificate
quicbaas cert generate-ca \
  --common-name "QuicBaaS CA" \
  --organization "My Organization" \
  --validity-days 3650 \
  --output-dir /etc/quicbaas/certs

# Generate a server certificate
quicbaas cert issue \
  --common-name "quicbaas-server" \
  --usage server \
  --dns-names "localhost,quicbaas.example.com" \
  --ip-addresses "127.0.0.1,10.0.0.1" \
  --validity-days 365 \
  --ca-cert /etc/quicbaas/certs/ca.crt \
  --ca-key /etc/quicbaas/certs/ca.key \
  --output-dir /etc/quicbaas/certs

# Generate a client certificate
quicbaas cert issue \
  --common-name "quicbaas-client" \
  --usage client \
  --validity-days 365 \
  --ca-cert /etc/quicbaas/certs/ca.crt \
  --ca-key /etc/quicbaas/certs/ca.key \
  --output-dir /etc/quicbaas/certs
```

**Via API:**

```bash
# Issue certificate via REST API
curl -X POST https://quicbaas:8080/v1/certificates/issue \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "common_name": "my-service",
    "organization": "My Org",
    "dns_names": ["my-service.example.com"],
    "ip_addresses": ["10.0.0.50"],
    "usage": "both",
    "validity_days": 365
  }'
```

### Manual Certificate Creation with OpenSSL

For production environments or integration with existing PKI:

**Step 1: Create a Certificate Authority**

```bash
# Generate CA private key (ECDSA P-256 recommended)
openssl ecparam -genkey -name prime256v1 -out ca.key

# For RSA (2048-bit minimum for FIPS):
# openssl genrsa -out ca.key 4096

# Generate self-signed CA certificate
openssl req -new -x509 -days 3650 -key ca.key -out ca.crt \
  -subj "/C=US/ST=State/L=City/O=Organization/CN=QuicBaaS CA"
```

**Step 2: Create Server Certificate**

```bash
# Create OpenSSL config for server
cat > server.cnf <<EOF
[req]
distinguished_name = req_dn
req_extensions = v3_req
prompt = no

[req_dn]
C = US
ST = State
L = City
O = Organization
CN = quicbaas-server

[v3_req]
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
DNS.2 = quicbaas.example.com
DNS.3 = *.quicbaas.example.com
IP.1 = 127.0.0.1
IP.2 = 10.0.0.1
EOF

# Generate server key
openssl ecparam -genkey -name prime256v1 -out server.key

# Generate CSR
openssl req -new -key server.key -out server.csr -config server.cnf

# Sign with CA
openssl x509 -req -days 365 -in server.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out server.crt -extensions v3_req -extfile server.cnf
```

**Step 3: Create Client Certificate (for mTLS)**

```bash
# Create OpenSSL config for client
cat > client.cnf <<EOF
[req]
distinguished_name = req_dn
req_extensions = v3_req
prompt = no

[req_dn]
C = US
ST = State
L = City
O = Organization
CN = quicbaas-client

[v3_req]
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
EOF

# Generate client key
openssl ecparam -genkey -name prime256v1 -out client.key

# Generate CSR
openssl req -new -key client.key -out client.csr -config client.cnf

# Sign with CA
openssl x509 -req -days 365 -in client.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out client.crt -extensions v3_req -extfile client.cnf
```

### Certificate Requirements

**Subject Alternative Names (SANs):**
- Server certificates must include all hostnames and IP addresses used to access the server
- Include both DNS names and IP addresses
- Wildcard certificates (`*.example.com`) are supported

**Key Types:**
| Type | Size | FIPS Approved | Notes |
|------|------|---------------|-------|
| ECDSA | P-256 | Yes | Recommended, fast |
| ECDSA | P-384 | Yes | Higher security |
| ECDSA | P-521 | Yes | Maximum security |
| RSA | 2048 | Yes | Minimum for FIPS |
| RSA | 3072 | Yes | Good balance |
| RSA | 4096 | Yes | Maximum security |

**Key Usage Extensions:**

For server certificates:
```
X509v3 Key Usage: critical
    Digital Signature, Key Encipherment
X509v3 Extended Key Usage:
    TLS Web Server Authentication
```

For client certificates:
```
X509v3 Key Usage: critical
    Digital Signature, Key Encipherment
X509v3 Extended Key Usage:
    TLS Web Client Authentication
```

For dual-purpose certificates:
```
X509v3 Key Usage: critical
    Digital Signature, Key Encipherment
X509v3 Extended Key Usage:
    TLS Web Server Authentication, TLS Web Client Authentication
```

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

**CLI flags:**

```bash
quicbaas server start \
  --tls-enabled=true \
  --tls-cert=/etc/quicbaas/certs/server.crt \
  --tls-key=/etc/quicbaas/certs/server.key \
  --tls-ca=/etc/quicbaas/certs/ca.crt \
  --tls-client-auth=false
```

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
# Generate new server certificate
quicbaas cert issue \
  --common-name "quicbaas-server" \
  --usage server \
  --dns-names "localhost,quicbaas.example.com" \
  --ip-addresses "127.0.0.1,10.0.0.1" \
  --validity-days 365 \
  --ca-cert /etc/quicbaas/certs/ca.crt \
  --ca-key /etc/quicbaas/certs/ca.key \
  --output-dir /etc/quicbaas/certs-new
```

**Step 2: Update configuration**

```yaml
security:
  tls_cert_file: "/etc/quicbaas/certs-new/server.crt"
  tls_key_file: "/etc/quicbaas/certs-new/server.key"
```

**Step 3: Reload server configuration**

```bash
# Graceful reload (no downtime)
quicbaas server reload

# Or send SIGHUP
kill -HUP $(pidof quicbaas)
```

**Step 4: Verify new certificate**

```bash
# Check certificate expiration
openssl s_client -connect localhost:8080 < /dev/null 2>/dev/null | \
  openssl x509 -noout -dates
```

**Rolling rotation for Raft cluster:**

1. Generate new certificates for all nodes
2. Update and reload each node one at a time
3. Wait for cluster health before proceeding to next node
4. Verify all nodes are using new certificates

```bash
# Check cluster health before proceeding
quicbaas cluster status

# Rotate node 1
scp new-certs/* node1:/etc/quicbaas/certs/
ssh node1 'quicbaas server reload'

# Wait and verify
sleep 10
quicbaas cluster status

# Repeat for remaining nodes
```

### Emergency Certificate Replacement

For compromised or expired certificates:

**Step 1: Generate emergency certificates**

```bash
# Quickly generate with short validity
quicbaas cert issue \
  --common-name "quicbaas-server-emergency" \
  --usage server \
  --dns-names "localhost,quicbaas.example.com" \
  --ip-addresses "127.0.0.1,10.0.0.1" \
  --validity-days 7 \
  --ca-cert /etc/quicbaas/certs/ca.crt \
  --ca-key /etc/quicbaas/certs/ca.key \
  --output-dir /etc/quicbaas/certs-emergency
```

**Step 2: Force restart if reload fails**

```bash
# Stop server
quicbaas server stop

# Replace certificates
cp /etc/quicbaas/certs-emergency/* /etc/quicbaas/certs/

# Start server
quicbaas server start
```

**Step 3: For compromised CA, regenerate entire chain**

```bash
# Generate new CA
quicbaas cert generate-ca \
  --common-name "QuicBaaS CA (Replacement)" \
  --organization "My Organization" \
  --validity-days 3650 \
  --output-dir /etc/quicbaas/certs-new-ca

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

# Regenerate certificate with correct SANs
quicbaas cert issue \
  --dns-names "correct-hostname.example.com" \
  --ip-addresses "correct.ip.address" \
  ...
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
# Health endpoint should work with TLS
quicbaas health --tls-enabled --tls-ca=/etc/quicbaas/certs/ca.crt

# Or via curl
curl --cacert /etc/quicbaas/certs/ca.crt \
     https://localhost:8080/health
```

## See Also

- [Key Custody](key-custody.md) - who creates a private key, where it lives, and the build gate that enforces it
- Raft mTLS - Detailed Raft cluster mTLS configuration
- Authentication - JWT-based authentication
- Security Overview - Security architecture overview
- KMS - Key management for data encryption
