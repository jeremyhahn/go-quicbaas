# Certificate Authority — API

> **Status: NORMATIVE** · Last verified: 2026-10-14
> The capability on every transport, with permissions. Feature folder: `docs/certificate-authority/`.

The CA, ACME and trust APIs are go-xkms's and are documented there:
[Certificate Authority — API](../../../go-xkms/docs/certificate-authority/api.md),
[ACME Issuance — API](../../../go-xkms/docs/acme-issuance/api.md),
[Trust Bootstrap — API](../../../go-xkms/docs/trust-bootstrap/api.md). This
page records only how QuicBaaS's `sdk/go` reaches them on each transport.

> **Being removed.** `quicbaas.Certificate` (`/api/v1/certificate`) and
> `quicbaas.ACME` (`/api/v1/acme/*`) below are QuicBaaS's own certificate
> service and ACME handling, a parallel implementation removed on the owner's
> word ([SPEC](../SPEC.md) rows `certificate-authority` and `acme`). The
> matrix records the code as it stands until the go-xkms re-export replaces it.

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

The attestation service — statement and chain verification, the container
attestation seam, and the RFC 9334 Verifier (endorsements, signed reference
values, quote appraisal) — has its own feature folder:
[docs/attestation/api.md](../attestation/api.md). Dedicated transport handlers:
rest=yes, grpc=yes, mcp=yes; embedded routes registered: 11; MCP tools
registered server-side: 11.




## Certificate profiles

Profiles, including `ipsec-vtep` and `x509-svid` and the name-constrained
subordinate CA they are issued under, are go-xkms's. `sdk/go/xkms/ca`
re-exports their `ProfileName*` constants unchanged. See
[Certificate Authority — API](../../../go-xkms/docs/certificate-authority/api.md)
and [Certificate types](../../../go-xkms/docs/ca/certificate-types.md).
