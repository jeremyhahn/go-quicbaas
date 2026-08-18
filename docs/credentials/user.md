# Credentials — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented usage, in the order tasks are done. Feature folder: `docs/credentials/`.

Dynamic credential generation with pluggable providers, certificate authority, and database credentials.

## Overview

QuicBaaS Credentials Engine provides dynamic credential generation for certificates, database credentials, and other short-lived secrets. It features a pluggable provider architecture allowing external implementations (such as go-xkms TrustedCA) to replace the built-in lightweight CA.

## Key Features

| Feature | Description |
|---------|-------------|
| **Certificate Provider** | Pluggable CA interface for mTLS certificates |
| **Built-in CA** | Lightweight self-signed CA for standalone deployments |
| **External CA Support** | Integration with go-xkms TrustedCA for full PKI |
| **Database Credentials** | Dynamic PostgreSQL and MySQL credential generation |
| **Short-Lived Secrets** | Automatic expiration and renewal |
| **Lease Management** | TTL-based credential lifecycle |

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                    Credentials Service                           │
├──────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐   │
│  │  REST API   │  │  gRPC API   │  │        CLI              │   │
│  └──────┬──────┘  └──────┬──────┘  └───────────┬─────────────┘   │
│         │                │                      │                │
│         └────────────────┼──────────────────────┘                │
│                          ▼                                       │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │                 CertificateService                         │  │
│  │           (Service Layer Wrapper)                          │  │
│  └───────────────────────┬────────────────────────────────────┘  │
│                          ▼                                       │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │              CertificateProvider Interface                 │  │
│  │  ┌──────────────────┐     ┌─────────────────────────────┐  │  │
│  │  │  BuiltinProvider │     │  External Provider          │  │  │
│  │  │  (Self-signed CA)│     │  (go-xkms TrustedCA)        │  │  │
│  │  └──────────────────┘     └─────────────────────────────┘  │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌───────────────────────────────────────────────────────────┐   │
│  │              Database Credential Generators               │   │
│  │  ┌─────────────────┐     ┌─────────────────────────────┐  │   │
│  │  │   PostgreSQL    │     │         MySQL               │  │   │
│  │  └─────────────────┘     └─────────────────────────────┘  │   │
│  └───────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────┘
```

## Quick Start

### Issue a Certificate

```bash
# Issue client certificate
quicbaas credentials cert issue my-client \
  --usage client \
  --validity 30

# Issue server certificate with SANs
quicbaas credentials cert issue api-server \
  --usage server \
  --dns "api.example.com,api.internal" \
  --ip "10.0.0.1"

# Issue certificate for both client and server auth
quicbaas credentials cert issue mtls-service \
  --usage both \
  --validity 365
```

### Get CA Certificate

```bash
# Get CA certificate PEM
quicbaas credentials cert ca

# Get CA bundle (full chain)
quicbaas credentials cert ca --bundle
```

### Sign a CSR

```bash
# Sign certificate signing request
quicbaas credentials cert sign --csr ./request.csr --validity 30
```

### Verify a Certificate

```bash
# Verify certificate against CA
quicbaas credentials cert verify --cert ./certificate.pem
```

### Revoke a Certificate

```bash
# Revoke by serial number
quicbaas credentials cert revoke --serial 12345
```

## Certificate Flow

```
┌──────────────┐     ┌─────────────────────┐     ┌────────────────┐
│   Client     │     │  CertificateService │     │    Provider    │
└──────┬───────┘     └──────────┬──────────┘     └───────┬────────┘
       │                        │                        │
       │  IssueCertificate()    │                        │
       │───────────────────────>│                        │
       │                        │  IssueCertificate()    │
       │                        │───────────────────────>│
       │                        │                        │
       │                        │     Generate Key       │
       │                        │<──────────────────────>│
       │                        │                        │
       │                        │     Sign with CA       │
       │                        │<──────────────────────>│
       │                        │                        │
       │                        │  IssuedCertificate     │
       │                        │<───────────────────────│
       │  Certificate + Key     │                        │
       │<───────────────────────│                        │
       │                        │                        │
```

## Provider Interface

The `CertificateProvider` interface allows pluggable CA implementations:

```go
type CertificateProvider interface {
    // Certificate operations
    IssueCertificate(ctx context.Context, req *CertificateRequest) (*IssuedCertificate, error)
    SignCSR(ctx context.Context, csrPEM []byte, req *CertificateRequest) (*IssuedCertificate, error)
    Revoke(ctx context.Context, cert *IssuedCertificate) error
    Verify(ctx context.Context, cert *x509.Certificate) error

    // CA access
    CACertificate() (*x509.Certificate, error)
    CACertificatePEM() ([]byte, error)
    CABundle() ([]byte, error)

    // TLS configuration
    TLSConfig(ctx context.Context, req *TLSConfigRequest) (*tls.Config, error)

    // Identity and status
    Identity() string
    IsInitialized() bool

    // Low-level signing (for advanced use)
    Signer() crypto.Signer
}
```

## Configuration

### Built-in Provider

```yaml
credentials:
  certificates:
    enabled: true
    provider: builtin

    builtin:
      # Auto-generate self-signed CA (default)
      auto_generate: true

      # Or use existing CA
      ca_cert_file: /path/to/ca.crt
      ca_key_file: /path/to/ca.key

      # CA subject (for auto-generation)
      subject:
        common_name: "QuicBaaS CA"
        organization: "My Organization"

      # Default certificate settings
      defaults:
        validity_days: 365
        key_algorithm: ecdsa  # ecdsa or rsa
        key_size: 256         # 256 for ECDSA P-256, 2048/4096 for RSA
```

### External Provider (go-xkms TrustedCA)

```yaml
credentials:
  certificates:
    enabled: true
    provider: external

    external:
      # go-xkms TrustedCA integration
      type: xkms   # go-xkms TrustedCA
      address: "localhost:8443"
      ca_bundle: /path/to/ca-bundle.pem
      client_cert: /path/to/client.crt
      client_key: /path/to/client.key
```

## Documentation

| Document | Description |
|----------|-------------|
| [Architecture](architecture.md) | Component design and provider interface |
| Configuration | Complete configuration reference |
| [CLI Usage](cli.md) | Command-line interface reference |
| [API Reference](api.md) | REST, gRPC, and MCP API documentation |

## Use Cases

### mTLS for Services

```bash
# Issue server certificate
quicbaas credentials cert issue api-server \
  --usage server \
  --dns "api.example.com" \
  --validity 90

# Issue client certificate for service-to-service auth
quicbaas credentials cert issue backend-client \
  --usage client \
  --validity 30
```

### Short-Lived Certificates

```bash
# Issue certificate with 1-day validity
quicbaas credentials cert issue ephemeral-client \
  --usage client \
  --validity 1
```

### Programmatic Usage

```go
// Get the certificate service
certSvc := quicBaaS.Certificate()

// Issue a certificate
resp, err := certSvc.IssueCertificate(ctx, &service.IssueCertificateRequest{
    CommonName:   "my-service",
    Organization: "My Org",
    DNSNames:     []string{"my-service.local"},
    Usage:        "server",
    ValidityDays: 30,
})
if err != nil {
    log.Fatal(err)
}

// Use the certificate
fmt.Println(resp.CertificatePEM)
fmt.Println(resp.PrivateKeyPEM)
```
