# Certificate Authority — Architecture

> **Status: NORMATIVE** · Last verified: 2026-10-14
> How QuicBaaS reaches the go-xkms CA, ACME and trust services. Feature folder:
> `docs/certificate-authority/`.

## Owner

The CA (`TrustedCA`, `CAService`, `TCGCAService`), the ACME server and client
(RFC 8555, ARI, cross-sign, TPM enrollment), DANE, cross-signed roots and AIA
import all belong to go-xkms. Their design is documented there:

- [Certificate Authority — Architecture](../../../go-xkms/docs/certificate-authority/architecture.md)
- [ACME Issuance — Architecture](../../../go-xkms/docs/acme-issuance/architecture.md)
- [Trust Bootstrap — Architecture](../../../go-xkms/docs/trust-bootstrap/architecture.md)
  and [DANE](../../../go-xkms/docs/ca/dane.md)

QuicBaaS implements none of it. It re-exports these services through `sdk/go`
on every transport, for go-trusted-platform ([SPEC](../SPEC.md) rows
`certificate-authority` and `acme`).

## What lives in this repository

| Surface | SDK accessor | Route family | CLI group |
|---|---|---|---|
| Certificate | `Security().Certificate()` | `/api/v1/certificate` | `ca` |
| ACME | `Security().ACME()` | `/api/v1/acme` | `acme` |
| Trust | `Security().Trust()` | `/api/v1/trust` | `trust` |

Issuance (Certificate, ACME) and acceptance (Trust) stay separate surfaces: a
deployment can issue for its own nodes while accepting a different set of
anchors for outbound connections.

**Being removed.** `Security().Certificate()` and `Security().ACME()` are
served today by QuicBaaS's own certificate service, its CA adapters and its own
`/api/v1/certificate` and `/api/v1/acme/*` verbs. That is a parallel
implementation, removed on the owner's word (SPEC rows `certificate-authority`
and `acme`). The replacement re-exports the go-xkms `CAService`,
`TCGCAService` and ACME service under go-xkms's own names and types.

## Component boundaries

- **go-xkms** — the CA, ACME, the signing keys and every hardware or cloud
  backend they can live in. Consumed through `go-xkms/sdk/go` via the
  composition seam ([`../composition/`](../composition/architecture.md)).
- **This repository** — the `Security()` accessors, the route families and the
  CLI groups above, and the one requester-key generator
  ([key-custody.md](key-custody.md)).
- **The barrier** — the CA signing key rests under it, so a sealed server
  cannot issue ([`../barrier/`](../barrier/architecture.md)).
- **Trust bootstrap** — a node with no anchor at all goes through
  [`../bootstrap/`](../bootstrap/architecture.md) first.

## Trust boundaries

- **`certs:read` and `certs:generate` are separate.** A service that needs the
  CA bundle to verify peers does not need the ability to mint identities.
- **Issuance fails closed when the barrier is sealed.** It does not fall back
  to a software key and does not queue.

## Known defect: three methods do not reach the server over REST

`GenerateCRL`, `IsInitialized` and `IsRevoked` are not in
`mapCertificateMethod`'s route table, so the REST transport falls to its
default branch and issues `POST /api/v1/certificate/generatecrl`,
`.../isinitialized` and `.../isrevoked`, none of which the server registers.
They work over gRPC, Unix, MCP and embedded, and fail over REST and QUIC. The
defect is in the certificate service being removed; see [api.md](api.md).
