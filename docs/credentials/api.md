# Credentials — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/credentials/`.


## Transport matrix

Every row below is derived from source, not from prose. The columns mean:

- **SDK method** — the method name `sdk/go/client/<pkg>` passes to
  `transport.Request(ctx, serviceName, "<method>", req)`.
- **REST route (transport)** — where `sdk/go/transport/rest` sends it.
- **Served?** — whether that exact method+path pair is registered in the
  server's route table (`pkg/api/rest/*.go`). A **no** is a live SDK-to-server
  mismatch, not a documentation gap.
- **gRPC** — whether `sdk/go/transport/grpc` routes the service through a typed
  handler. **Unix inherits this column exactly**: the Unix transport is gRPC
  over `AF_UNIX` and delegates every `Request` to it
  (`sdk/go/transport/unix/transport.go:114`).
- **QUIC** is likewise not a separate column: `sdk/go/transport/quic` embeds
  `*rest.Transport` and swaps only the `http.Client` for an `http3.Transport`
  (`sdk/go/transport/quic/transport.go:86`), so every QUIC row is its REST row
  carried over HTTP/3. See the caveat in
  [../multiprotocol-api/architecture.md](../multiprotocol-api/architecture.md):
  **no component in this repository serves HTTP/3 today.**
- **MCP** — whether the SDK routes the service to an MCP tool; the registered
  server-side tool names are listed above the table.
- **Embedded** — the `service/method` key registered in
  `sdk/go/transport/embedded/transport.go`.
- **Permission** — `resource:action` from
  `pkg/api/rest/routes_permissions_generated.go`.

## secret — NO SDK CLIENT PACKAGE (sdk/go/client/secret absent)


## REST API

Base URL: `/api/v1/credentials`

### Authentication

All endpoints require authentication via Bearer token:

```http
Authorization: Bearer <token>
```

---

### Issue Certificate

Issue a new certificate with automatic key generation.

```http
POST /api/v1/credentials/certificates
```

#### Request Body

```json
{
  "common_name": "my-service",
  "organization": "My Organization",
  "organizational_unit": "Engineering",
  "country": "US",
  "province": "California",
  "locality": "San Francisco",
  "dns_names": ["my-service.local", "my-service.svc.cluster.local"],
  "ip_addresses": ["10.0.0.5", "203.0.113.100"],
  "email_addresses": ["admin@example.com"],
  "uris": ["spiffe://cluster.local/ns/default/sa/my-service"],
  "validity_days": 90,
  "usage": "server",
  "is_ca": false,
  "key_algorithm": "ecdsa",
  "key_size": 256,
  "metadata": {
    "environment": "production"
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `common_name` | string | Yes | Certificate common name |
| `organization` | string | No | Organization name |
| `organizational_unit` | string | No | Organizational unit |
| `country` | string | No | Two-letter country code |
| `province` | string | No | State or province |
| `locality` | string | No | City or locality |
| `dns_names` | []string | No | DNS Subject Alternative Names |
| `ip_addresses` | []string | No | IP Subject Alternative Names |
| `email_addresses` | []string | No | Email Subject Alternative Names |
| `uris` | []string | No | URI Subject Alternative Names |
| `validity_days` | int | No | Certificate validity (default: 365) |
| `usage` | string | No | `client`, `server`, or `both` (default: `client`) |
| `is_ca` | bool | No | Issue as CA certificate |
| `key_algorithm` | string | No | `ecdsa` or `rsa` (default: `ecdsa`) |
| `key_size` | int | No | Key size (default: 256 for ECDSA) |
| `metadata` | object | No | Custom key-value metadata |

#### Response

**201 Created**

```json
{
  "certificate_pem": "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
  "private_key_pem": "-----BEGIN EC PRIVATE KEY-----\nMHQ...\n-----END EC PRIVATE KEY-----",
  "ca_certificate_pem": "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
  "chain_pem": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----",
  "serial_number": "7890123456789012345",
  "not_before": "2024-01-15T10:30:00Z",
  "not_after": "2024-04-15T10:30:00Z"
}
```

#### Errors

| Status | Code | Description |
|--------|------|-------------|
| 400 | `INVALID_INPUT` | Missing common_name or invalid parameters |
| 503 | `NOT_INITIALIZED` | Certificate provider not initialized |
| 500 | `INTERNAL_ERROR` | Certificate generation failed |

---

### Sign CSR

Sign a Certificate Signing Request.

```http
POST /api/v1/credentials/certificates/sign
```

#### Request Body

```json
{
  "csr_pem": "-----BEGIN CERTIFICATE REQUEST-----\nMIH...\n-----END CERTIFICATE REQUEST-----",
  "validity_days": 90,
  "usage": "client"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `csr_pem` | string | Yes | PEM-encoded CSR |
| `validity_days` | int | No | Certificate validity (default: 365) |
| `usage` | string | No | `client`, `server`, or `both` |

#### Response

**201 Created**

```json
{
  "certificate_pem": "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
  "ca_certificate_pem": "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
  "chain_pem": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----",
  "serial_number": "1234567890123456789",
  "not_before": "2024-01-15T10:30:00Z",
  "not_after": "2024-04-15T10:30:00Z"
}
```

#### Errors

| Status | Code | Description |
|--------|------|-------------|
| 400 | `INVALID_INPUT` | Invalid or missing CSR |
| 503 | `NOT_INITIALIZED` | Certificate provider not initialized |
| 500 | `INTERNAL_ERROR` | CSR signing failed |

---

### Verify Certificate

Verify a certificate against the CA.

```http
POST /api/v1/credentials/certificates/verify
```

#### Request Body

```json
{
  "certificate_pem": "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `certificate_pem` | string | Yes | PEM-encoded certificate |

#### Response

**200 OK (Valid)**

```json
{
  "valid": true
}
```

**200 OK (Invalid)**

```json
{
  "valid": false,
  "error_message": "certificate has been revoked"
}
```

#### Errors

| Status | Code | Description |
|--------|------|-------------|
| 400 | `INVALID_INPUT` | Missing or malformed certificate |
| 503 | `NOT_INITIALIZED` | Certificate provider not initialized |

---

### Revoke Certificate

Revoke a certificate by serial number.

```http
POST /api/v1/credentials/certificates/revoke
```

#### Request Body

```json
{
  "serial_number": "7890123456789012345"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `serial_number` | string | Yes | Certificate serial number |

#### Response

**200 OK**

```json
{
  "success": true
}
```

#### Errors

| Status | Code | Description |
|--------|------|-------------|
| 400 | `INVALID_INPUT` | Missing serial number |
| 503 | `NOT_INITIALIZED` | Certificate provider not initialized |
| 500 | `INTERNAL_ERROR` | Revocation failed |

---

### Get CA Certificate

Retrieve the CA certificate and bundle.

```http
GET /api/v1/credentials/certificates/ca
```

#### Response

**200 OK**

```json
{
  "ca_certificate_pem": "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
  "ca_bundle_pem": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----",
  "identity": "quicbaas-ca"
}
```

#### Errors

| Status | Code | Description |
|--------|------|-------------|
| 503 | `NOT_INITIALIZED` | Certificate provider not initialized |

---

## gRPC API

### Service Definition

```protobuf
syntax = "proto3";

package quicbaas.credentials.v1;

service CertificateService {
  // Issue a new certificate
  rpc IssueCertificate(IssueCertificateRequest) returns (IssueCertificateResponse);

  // Sign a CSR
  rpc SignCSR(SignCSRRequest) returns (SignCSRResponse);

  // Verify a certificate
  rpc VerifyCertificate(VerifyCertificateRequest) returns (VerifyCertificateResponse);

  // Revoke a certificate
  rpc RevokeCertificate(RevokeCertificateRequest) returns (RevokeCertificateResponse);

  // Get CA certificate
  rpc GetCACertificate(GetCACertificateRequest) returns (GetCACertificateResponse);

  // Get TLS configuration
  rpc GetTLSConfig(GetTLSConfigRequest) returns (GetTLSConfigResponse);
}
```

### Messages

```protobuf
message IssueCertificateRequest {
  string common_name = 1;
  string organization = 2;
  string organizational_unit = 3;
  string country = 4;
  string province = 5;
  string locality = 6;
  repeated string dns_names = 7;
  repeated string ip_addresses = 8;
  repeated string email_addresses = 9;
  repeated string uris = 10;
  int32 validity_days = 11;
  string usage = 12;  // "client", "server", "both"
  bool is_ca = 13;
  int32 max_path_len = 14;
  string key_algorithm = 15;  // "ecdsa", "rsa"
  int32 key_size = 16;
  map<string, string> metadata = 17;
}

message IssueCertificateResponse {
  string certificate_pem = 1;
  string private_key_pem = 2;
  string ca_certificate_pem = 3;
  string chain_pem = 4;
  string serial_number = 5;
  google.protobuf.Timestamp not_before = 6;
  google.protobuf.Timestamp not_after = 7;
}

message SignCSRRequest {
  string csr_pem = 1;
  int32 validity_days = 2;
  string usage = 3;
}

message SignCSRResponse {
  string certificate_pem = 1;
  string ca_certificate_pem = 2;
  string chain_pem = 3;
  string serial_number = 4;
  google.protobuf.Timestamp not_before = 5;
  google.protobuf.Timestamp not_after = 6;
}

message VerifyCertificateRequest {
  string certificate_pem = 1;
}

message VerifyCertificateResponse {
  bool valid = 1;
  string error_message = 2;
}

message RevokeCertificateRequest {
  string serial_number = 1;
}

message RevokeCertificateResponse {
  bool success = 1;
}

message GetCACertificateRequest {}

message GetCACertificateResponse {
  string ca_certificate_pem = 1;
  string ca_bundle_pem = 2;
  string identity = 3;
}

message GetTLSConfigRequest {
  string server_name = 1;
  bool require_client_cert = 2;
  bool is_server = 3;
  string min_version = 4;  // "1.2", "1.3"
}

message GetTLSConfigResponse {
  // TLS config is returned as structured data
  // Application should construct tls.Config from these fields
  bytes root_ca_pem = 1;
  bytes cert_pem = 2;
  bytes key_pem = 3;
  string server_name = 4;
  bool client_auth_required = 5;
  string min_version = 6;
}
```

### gRPC Examples

```go
import (
    "context"
    pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// Connect to QuicBaaS
conn, _ := grpc.Dial("localhost:9090", grpc.WithInsecure())
client := pb.NewCertificateServiceClient(conn)

// Issue certificate
resp, err := client.IssueCertificate(context.Background(), &pb.IssueCertificateRequest{
    CommonName:   "my-service",
    Organization: "My Org",
    DnsNames:     []string{"my-service.local"},
    Usage:        "server",
    ValidityDays: 90,
})

// Sign CSR
signResp, err := client.SignCSR(context.Background(), &pb.SignCSRRequest{
    CsrPem:       csrPEM,
    ValidityDays: 30,
    Usage:        "client",
})

// Verify certificate
verifyResp, err := client.VerifyCertificate(context.Background(), &pb.VerifyCertificateRequest{
    CertificatePem: certPEM,
})

// Revoke certificate
revokeResp, err := client.RevokeCertificate(context.Background(), &pb.RevokeCertificateRequest{
    SerialNumber: "7890123456789",
})

// Get CA certificate
caResp, err := client.GetCACertificate(context.Background(), &pb.GetCACertificateRequest{})
```

---

## MCP API

Model Context Protocol tools for AI-assisted certificate management.

### credentials_cert_issue

Issue a new certificate.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "common_name": {
      "type": "string",
      "description": "Certificate common name"
    },
    "usage": {
      "type": "string",
      "enum": ["client", "server", "both"],
      "description": "Certificate usage type"
    },
    "dns_names": {
      "type": "array",
      "items": { "type": "string" },
      "description": "DNS Subject Alternative Names"
    },
    "ip_addresses": {
      "type": "array",
      "items": { "type": "string" },
      "description": "IP Subject Alternative Names"
    },
    "validity_days": {
      "type": "integer",
      "description": "Certificate validity in days"
    }
  },
  "required": ["common_name"]
}
```

**Example:**

```json
{
  "tool": "credentials_cert_issue",
  "arguments": {
    "common_name": "api-server",
    "usage": "server",
    "dns_names": ["api.example.com"],
    "validity_days": 90
  }
}
```

---

### credentials_cert_sign

Sign a Certificate Signing Request.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "csr_pem": {
      "type": "string",
      "description": "PEM-encoded CSR"
    },
    "usage": {
      "type": "string",
      "enum": ["client", "server", "both"]
    },
    "validity_days": {
      "type": "integer"
    }
  },
  "required": ["csr_pem"]
}
```

---

### credentials_cert_verify

Verify a certificate.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "certificate_pem": {
      "type": "string",
      "description": "PEM-encoded certificate"
    }
  },
  "required": ["certificate_pem"]
}
```

---

### credentials_cert_revoke

Revoke a certificate.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "serial_number": {
      "type": "string",
      "description": "Certificate serial number"
    }
  },
  "required": ["serial_number"]
}
```

---

### credentials_cert_ca

Get the CA certificate.

**Input Schema:**

```json
{
  "type": "object",
  "properties": {
    "include_bundle": {
      "type": "boolean",
      "description": "Include full CA bundle"
    }
  }
}
```

---

## Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `INVALID_INPUT` | 400 | Invalid or missing request parameters |
| `NOT_FOUND` | 404 | Resource not found |
| `REVOKED` | 403 | Certificate has been revoked |
| `EXPIRED` | 403 | Certificate has expired |
| `NOT_INITIALIZED` | 503 | Provider not initialized |
| `INTERNAL_ERROR` | 500 | Internal server error |

## Rate Limiting

Certificate operations are rate-limited to prevent abuse:

| Operation | Default Limit |
|-----------|---------------|
| Issue | 100/minute |
| Sign | 100/minute |
| Verify | 1000/minute |
| Revoke | 50/minute |
| Get CA | 1000/minute |

Rate limit headers are included in responses:

```http
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1705320000
```
