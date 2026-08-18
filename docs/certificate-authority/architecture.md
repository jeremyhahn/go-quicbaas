# Certificate Authority — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design and boundaries of the CA, ACME and trust surfaces. Feature folder:
> `docs/certificate-authority/`.

## The problem

Everything in a zero-trust deployment needs an identity it can prove: nodes
authenticating to each other over mTLS, clients authenticating to the API,
agents authenticating to the orchestrator, servers presenting a certificate a
browser will accept. Without an in-deployment issuer, all of that becomes either
a manual PKI or a dependency on a public CA that cannot see a private network.

## The shape chosen

**Consume the CA; do not implement one.** The `TrustedCA` and ACME (RFC 8555)
implementation belongs to go-xkms. What lives in this repository is the service
surface over it: the SDK services, the REST/gRPC/MCP routes, and the CLI.

That boundary is the design decision. The alternative — a second CA
implementation here — was rejected for the reason every duplicated
implementation is rejected in this portfolio: two issuers diverge, and the
divergence surfaces as a certificate one half of the deployment accepts and the
other does not.

Three related but distinct surfaces sit in this folder:

| Surface | SDK service | What it decides |
|---|---|---|
| Certificate | `quicbaas.Certificate` | what this deployment **issues** |
| ACME | `quicbaas.ACME` | certificates obtained by RFC 8555 protocol flow |
| Trust | `quicbaas.Trust` | what this deployment **accepts** |

Issuance and acceptance being separate is not an accident of layout. A
deployment can be an issuer for its own nodes while accepting an entirely
different set of anchors for outbound connections, and conflating them is how a
deployment ends up trusting whatever it happens to issue.

## Component boundaries

- **go-xkms** — `TrustedCA`, the ACME server implementation, the signing keys
  and every hardware/cloud backend they can live in. Consumed through
  `go-xkms/sdk/go` via the composition seam
  ([`../composition/`](../composition/architecture.md)).
- **This repository** — `Security().Certificate()`, `.ACME()` and `.Trust()`
  on the SDK; the `/api/v1/certificate`, `/api/v1/acme` and `/api/v1/trust`
  route families; the `ca`, `acme` and `trust` CLI groups.
- **go-truststrap** — the trust-bootstrap primitives (DANE, Noise_NK,
  SPKI-pinned TLS) that a node uses before it has a certificate. Reached during
  bootstrap, not here: [`../bootstrap/`](../bootstrap/architecture.md).
- **The barrier** — the CA signing key rests under it. This is the coupling
  that makes a sealed server unable to issue
  ([`../barrier/`](../barrier/architecture.md)).

## Data flow — issuing a certificate

```
caller (CLI, SDK, MCP tool)
  │
  ▼
/api/v1/certificate/issue           permission: certs:generate
  │
  ▼
composition layer resolves the CA backend      ../composition/
  │
  ▼
go-xkms TrustedCA  ── signing key unwrapped through the barrier ──┐
  │                                                               │
  ▼                                                               ▼
certificate returned                                    sealed → refused
```

The right-hand branch is the one to internalise: **issuance fails closed when
the barrier is sealed.** It does not fall back to a software key and does not
queue.

## The two ACME shapes

The CLI exposes both a single-shot and an explicit flow
(`cmd/quicbaas/acme.go`):

- `acme issue` — one command, order to certificate.
- `acme new-order` → `acme finalize-order` — the same flow with the step between
  them visible, which is what you need when the challenge must be served
  somewhere the tool cannot reach.

`acme register-challenge <name>` registers a challenge responder;
`acme renew <cert-url>` renews by the order's certificate URL, not by name.

## Trust boundaries

- **The CA signing key is the deployment's trust root.** Compromising it means
  every certificate it ever issued is suspect, including ones already
  distributed. This is why it lives behind the barrier and behind a hardware or
  cloud backend where one is configured.
- **`certs:read` and `certs:generate` are deliberately separate.** A service
  that needs the CA bundle to verify peers does not need the ability to mint
  identities. Granting the pair together is the most common over-grant in this
  feature.
- **`trust auto-import-aia` follows Authority Information Access pointers.**
  That is an automatic trust-anchor acquisition path; understand what it will
  import before enabling it.
- **Revocation is only as strong as CRL distribution.** The CA can revoke; it
  cannot make verifiers check.

## Known defect: three methods do not reach the server over REST

`GenerateCRL`, `IsInitialized` and `IsRevoked` are not in
`mapCertificateMethod`'s route table, so the REST transport falls to its default
branch and issues `POST /api/v1/certificate/generatecrl`,
`.../isinitialized` and `.../isrevoked` — none of which the server registers.
They work over gRPC, Unix, MCP and embedded, and fail over REST and QUIC.

Recorded here rather than fixed: the remedy is either a route-map entry or a
server route, and both are behaviour changes that go through the SDLC. See
[api.md](api.md) for the generated matrix showing each row.

## What this feature deliberately does not do

- **It does not implement PKI primitives.** Key generation, signing algorithms
  and hardware backends are go-xkms's.
- **It is not a public CA.** Certificates it issues are trusted by whoever
  installs its bundle, and by nobody else.
- **It does not distribute the bundle.** `ca bundle` and `ca export-bundle`
  hand it to you; getting it onto every host is configuration management.
- **It does not bootstrap trust from nothing.** A node with no anchor at all
  goes through [`../bootstrap/`](../bootstrap/architecture.md) first.
