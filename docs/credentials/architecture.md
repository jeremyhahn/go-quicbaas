# Credentials — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design, component boundaries and data flow. Feature folder: `docs/credentials/`.

## Overview

The QuicBaaS Credentials Engine provides dynamic credential generation through a pluggable provider architecture. The design separates the service layer from provider implementations, allowing external CA systems to replace the built-in lightweight CA.

## Component Layers

```
┌──────────────────────────────────────────────────────────────────────┐
│                           API Layer                                  │
│  ┌───────────────┐  ┌───────────────┐  ┌───────────────────────────┐ │
│  │   REST API    │  │   gRPC API    │  │         MCP API           │ │
│  │ /api/v1/certs │  │ CertService   │  │   credentials_* tools     │ │
│  └───────┬───────┘  └───────┬───────┘  └─────────────┬─────────────┘ │
│          │                  │                        │               │
└──────────┼──────────────────┼────────────────────────┼───────────────┘
           │                  │                        │
           ▼                  ▼                        ▼
┌──────────────────────────────────────────────────────────────────────┐
│                        Service Layer                                 │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │                    CertificateService                           │ │
│  │  - Request/Response translation                                 │ │
│  │  - Input validation                                             │ │
│  │  - Error wrapping (QRDBError)                                   │ │
│  │  - Metrics and logging                                          │ │
│  └─────────────────────────────┬───────────────────────────────────┘ │
│                                │                                     │
└────────────────────────────────┼─────────────────────────────────────┘
                                 │
                                 ▼
┌──────────────────────────────────────────────────────────────────────┐
│                       Provider Interface                             │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │                   CertificateProvider                           │ │
│  │  interface {                                                    │ │
│  │    IssueCertificate(ctx, req) (*IssuedCertificate, error)       │ │
│  │    SignCSR(ctx, csrPEM, req) (*IssuedCertificate, error)        │ │
│  │    Revoke(ctx, cert) error                                      │ │
│  │    Verify(ctx, cert) error                                      │ |
│  │    CACertificate() (*x509.Certificate, error)                   │ │
│  │    CACertificatePEM() ([]byte, error)                           │ │
│  │    CABundle() ([]byte, error)                                   │ │
│  │    TLSConfig(ctx, req) (*tls.Config, error)                     │ │
│  │    Identity() string                                            │ │
│  │    IsInitialized() bool                                         │ │
│  │    Signer() crypto.Signer                                       │ │ 
│  │  }                                                              │ │ 
│  └─────────────────────────────────────────────────────────────────┘ │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
                                 │
           ┌─────────────────────┴─────────────────────┐
           ▼                                           ▼
┌─────────────────────────────────┐     ┌─────────────────────────────┐
│      BuiltinProvider            │     │    External Provider        │
│  (pkg/credentials/certificate)  │     │    (go-xkms TrustedCA)      │
├─────────────────────────────────┤     ├─────────────────────────────┤
│  - Self-signed CA               │     │  - Full PKI integration     │
│  - In-memory revocation         │     │  - HSM/TPM support          │
│  - ECDSA/RSA key generation     │     │  - CRL/OCSP                 │
│  - Basic mTLS support           │     │  - Certificate policies     │
│  - Standalone deployments       │     │  - Enterprise features      │
└─────────────────────────────────┘     └─────────────────────────────┘
```

## Built-in Provider

The `BuiltinProvider` is a lightweight CA for standalone QuicBaaS deployments. It provides basic mTLS certificate issuance without external dependencies.

### Capabilities

| Capability | Supported |
|------------|-----------|
| Self-signed CA generation | Yes |
| Load existing CA from PEM | Yes |
| ECDSA key generation (P-256, P-384, P-521) | Yes |
| RSA key generation (2048, 4096) | Yes |
| CSR signing | Yes |
| Certificate revocation (in-memory) | Yes |
| Certificate verification | Yes |
| TLS configuration generation | Yes |
| CRL generation | No |
| OCSP | No |
| Certificate policies | No |

### Internal Structure

```
┌──────────────────────────────────────────────────────────────────┐
│                      BuiltinProvider                             │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │                    CA Key Material                          │ │
│  │  caCert    *x509.Certificate  // Parsed CA certificate      │ │
│  │  caKey     crypto.PrivateKey  // CA private key (ECDSA/RSA) │ │
│  │  caCertPEM []byte             // PEM-encoded CA cert        │ │
│  │  caKeyPEM  []byte             // PEM-encoded CA key         │ │
│  └─────────────────────────────────────────────────────────────┘ │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │                  Revocation Tracking                        │ │
│  │  revoked   map[string]time.Time  // serial -> revocation    │ │
│  │  mu        sync.RWMutex          // Thread-safe access      │ │
│  └─────────────────────────────────────────────────────────────┘ │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │                    Configuration                            │ │
│  │  config    *ProviderConfig       // Provider settings       │ │
│  │  logger    interfaces.Logger     // Logging interface       │ │
│  └─────────────────────────────────────────────────────────────┘ │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

## Certificate Issuance Flow

```
┌─────────┐      ┌──────────────────┐      ┌────────────────────┐
│ Client  │      │CertificateService│      │ CertificateProvider│
└────┬────┘      └────────┬─────────┘      └────────┬───────────┘
     │                    │                         │
     │ IssueCertificate   │                         │
     │ (CommonName,       │                         │
     │  Organization,     │                         │
     │  DNSNames, etc.)   │                         │
     │───────────────────>│                         │
     │                    │                         │
     │                    │ Validate Request        │
     │                    │ (nil check, CN check)   │
     │                    ├────────────>            │
     │                    │                         │
     │                    │ Build CertificateRequest│
     │                    │ (map service types to   │
     │                    │  provider types)        │
     │                    ├────────────>            │
     │                    │                         │
     │                    │ IssueCertificate()      │
     │                    │────────────────────────>│
     │                    │                         │
     │                    │                         │ Generate Key Pair
     │                    │                         │ (ECDSA P-256 or RSA)
     │                    │                         ├─────────────>
     │                    │                         │
     │                    │                         │ Create Certificate
     │                    │                         │ Template
     │                    │                         ├─────────────>
     │                    │                         │
     │                    │                         │ Sign with CA Key
     │                    │                         ├─────────────>
     │                    │                         │
     │                    │   IssuedCertificate     │
     │                    │<────────────────────────│
     │                    │                         │
     │                    │ Wrap Response           │
     │                    │ (convert to service     │
     │                    │  response types)        │
     │                    ├────────────>            │
     │                    │                         │
     │ IssueCertificateResponse                     │
     │ (CertificatePEM,   │                         │
     │  PrivateKeyPEM,    │                         │
     │  SerialNumber)     │                         │
     │<───────────────────│                         │
     │                    │                         │
```

## CSR Signing Flow

```
┌─────────┐      ┌──────────────────┐      ┌────────────────────┐
│ Client  │      │CertificateService│      │ CertificateProvider│
└────┬────┘      └────────┬─────────┘      └────────┬───────────┘
     │                    │                         │
     │ SignCSR            │                         │
     │ (CSRPEM,           │                         │
     │  ValidityDays,     │                         │
     │  Usage)            │                         │
     │───────────────────>│                         │
     │                    │                         │
     │                    │ Validate CSR PEM        │
     │                    ├────────────>            │
     │                    │                         │
     │                    │ SignCSR()               │
     │                    │────────────────────────>│
     │                    │                         │
     │                    │                         │ Parse CSR
     │                    │                         ├─────────────>
     │                    │                         │
     │                    │                         │ Verify CSR Signature
     │                    │                         ├─────────────>
     │                    │                         │
     │                    │                         │ Create Certificate
     │                    │                         │ (use CSR subject
     │                    │                         │  and public key)
     │                    │                         ├─────────────>
     │                    │                         │
     │                    │                         │ Sign with CA Key
     │                    │                         ├─────────────>
     │                    │                         │
     │                    │   IssuedCertificate     │
     │                    │   (no private key)      │
     │                    │<────────────────────────│
     │                    │                         │
     │ SignCSRResponse    │                         │
     │<───────────────────│                         │
     │                    │                         │
```

## Certificate Verification Flow

```
┌─────────┐      ┌──────────────────┐      ┌────────────────────┐
│ Client  │      │CertificateService│      │ CertificateProvider│
└────┬────┘      └────────┬─────────┘      └────────┬───────────┘
     │                    │                         │
     │ VerifyCertificate  │                         │
     │ (CertificatePEM)   │                         │
     │───────────────────>│                         │
     │                    │                         │
     │                    │ Parse PEM               │
     │                    │ (check block type)      │
     │                    ├────────────>            │
     │                    │                         │
     │                    │ Parse x509.Certificate  │
     │                    ├────────────>            │
     │                    │                         │
     │                    │ Verify()                │
     │                    │────────────────────────>│
     │                    │                         │
     │                    │                         │ Check revocation
     │                    │                         │ (in-memory map)
     │                    │                         ├─────────────>
     │                    │                         │
     │                    │                         │ Verify chain to CA
     │                    │                         │ (x509.Verify)
     │                    │                         ├─────────────>
     │                    │                         │
     │                    │                         │ Check expiration
     │                    │                         ├─────────────>
     │                    │                         │
     │                    │     nil or error        │
     │                    │<────────────────────────│
     │                    │                         │
     │ VerifyResponse     │                         │
     │ (Valid, Error)     │                         │
     │<───────────────────│                         │
     │                    │                         │
```

## Error Handling

All errors are wrapped as structured `QRDBError` values (see `pkg/errors`) for consistent API responses:

```
┌───────────────────────────────────────────────────────────────────┐
│                     Error Categorization                          │
├───────────────────────────────────────────────────────────────────┤
│                                                                   │
│  Provider Errors          QRDBError Code      HTTP Status         │
│  ────────────────────     ────────────────    ───────────         │
│  ErrProviderNotInitialized → ErrNotInitialized → 503              │
│  ErrMissingCACert          → ErrNotInitialized → 503              │
│  ErrInvalidCSR             → ErrInvalidInput   → 400              │
│  ErrMissingCommonName      → ErrInvalidInput   → 400              │
│  ErrCertificateRevoked     → ErrRevoked        → 403              │
│  ErrCertificateExpired     → ErrExpired        → 403              │
│  ErrCertificateNotYetValid → ErrInvalidInput   → 400              │
│  ErrVerificationFailed     → ErrInternal       → 500              │
│                                                                   │
└───────────────────────────────────────────────────────────────────┘
```

## Service Registration

The certificate provider is registered with the QuicBaaS service builder:

```go
// Create provider
provider, err := certificate.NewBuiltinProvider(&certificate.ProviderConfig{
    CommonName:   "My CA",
    Organization: "My Org",
}, logger)

// Register with service
svc, err := service.NewServiceBuilder().
    WithConfig(cfg).
    WithRaftNode(raftNode).
    WithCertificateProvider(provider).  // <-- Register here
    Build()

// Access via service
certSvc := svc.Certificate()
resp, err := certSvc.IssueCertificate(ctx, req)
```

## Thread Safety

The `BuiltinProvider` uses `sync.RWMutex` for thread-safe operations:

| Operation | Lock Type |
|-----------|-----------|
| IssueCertificate | RLock (read CA) |
| SignCSR | RLock (read CA) |
| Revoke | Lock (write revoked map) |
| Verify | RLock (read CA + revoked map) |
| CACertificate | RLock |
| TLSConfig | RLock |

## External Provider Integration

To integrate an external CA (e.g., go-xkms TrustedCA):

```go
// External provider must implement CertificateProvider
type TrustedCAProvider struct {
    client *trustedca.Client
}

func (p *TrustedCAProvider) IssueCertificate(ctx context.Context,
    req *certificate.CertificateRequest) (*certificate.IssuedCertificate, error) {
    // Delegate to go-xkms TrustedCA
    return p.client.Issue(ctx, req)
}

// ... implement remaining interface methods ...

// Register with QuicBaaS
svc, err := service.NewServiceBuilder().
    WithCertificateProvider(&TrustedCAProvider{client: tcaClient}).
    Build()
```
