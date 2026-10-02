# Certificate Authority — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/certificate-authority/`.


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

### `quicbaas.Certificate` — SDK package `sdk/go/client/certificate`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=yes; embedded routes registered: 15; MCP tools registered server-side: 6.

MCP tools: `quicbaas_certificate_get_ca`, `quicbaas_certificate_get_tls_config`, `quicbaas_certificate_issue`, `quicbaas_certificate_revoke`, `quicbaas_certificate_sign_csr`, `quicbaas_certificate_verify`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `CAInfo` | `GET /api/v1/certificate/ca/info` | yes | yes | yes | yes | `certs:read` |
| `GenerateCRL` | `POST /api/v1/certificate/generatecrl` *(default branch)* | **no** | yes | yes | yes | — |
| `GetCABundle` | `GET /api/v1/certificate/ca/bundle` | yes | yes | yes | yes | `certs:read` |
| `GetCACertificate` | `GET /api/v1/certificate/ca` | yes | yes | yes | yes | `certs:read` |
| `GetTLSConfig` | `POST /api/v1/certificate/tls-config` | yes | yes | yes | yes | `certs:generate` |
| `IsInitialized` | `POST /api/v1/certificate/isinitialized` *(default branch)* | **no** | yes | yes | yes | — |
| `IsRevoked` | `POST /api/v1/certificate/isrevoked` *(default branch)* | **no** | yes | yes | yes | — |
| `IssueCertificate` | `POST /api/v1/certificate/issue` | yes | yes | yes | yes | `certs:generate` |
| `ListCertificates` | `GET /api/v1/certificate/ca/list` | yes | yes | yes | yes | `certs:read` |
| `RevokeCertificate` | `POST /api/v1/certificate/revoke` | yes | yes | yes | yes | `certs:generate` |
| `SignCSR` | `POST /api/v1/certificate/sign-csr` | yes | yes | yes | yes | `certs:generate` |
| `VerifyCertificate` | `POST /api/v1/certificate/verify` | yes | yes | yes | yes | `certs:generate` |

### `quicbaas.ACME` — SDK package `sdk/go/client/acme`

Dedicated transport handlers: rest=no, grpc=no, mcp=no; embedded routes registered: 5; MCP tools registered server-side: 0.

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|

### `quicbaas.Trust` — SDK package `sdk/go/client/trust`

Dedicated transport handlers: rest=no, grpc=yes, mcp=no; embedded routes registered: 3; MCP tools registered server-side: 3.

MCP tools: `quicbaas_trust_auto_import_aia`, `quicbaas_trust_cross_signed_root_pool`, `quicbaas_trust_publish_dane`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|

### `quicbaas.Attestation` — SDK package `sdk/go/client/attestation`

Dedicated transport handlers: rest=no, grpc=yes, mcp=yes; embedded routes registered: 3; MCP tools registered server-side: 3.

MCP tools: `quicbaas_attestation_attest_container`, `quicbaas_attestation_verify`, `quicbaas_attestation_verify_chain`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|




## Certificate profiles

A caller selects a profile by name on `SignOptions.Profile`; the names are the
`ProfileName*` constants re-exported in `sdk/go/xkms/ca` from go-xkms. Two are
for the platform's overlay and service identity (go-xkms#1385):

| Profile | Constant | Certificate |
|---|---|---|
| `ipsec-vtep` | `ProfileNameIPsecVTEP` | IKEv2 tunnel endpoint: EKU `id-kp-ipsecIKE` (`OIDIPsecIKE`), IP SANs supplied by the issuing caller and never copied from the CSR, 30-day default lifetime. May carry the `tp-attestationEvidence` extension (`CreateAttestationEvidenceExtension`). |
| `x509-svid` | `ProfileNameX509SVID` | SPIFFE X.509-SVID: one caller-supplied SPIFFE URI SAN, serverAuth + clientAuth, not a CA, 1-hour default lifetime. |

Both are issued under a name-constrained subordinate CA for a caller-held key
(`SubordinateIssuer`, `SubordinateCARequest`, `NameConstraints`); a leaf
outside the constraints fails with `ErrCANameConstraintViolation`.
