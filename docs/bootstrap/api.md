# Bootstrap — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The first-boot ceremony and trust-bootstrap surfaces. Feature folder:
> `docs/bootstrap/`.

Bootstrap has **no SDK service package** — there is no `sdk/go/client/bootstrap`
and no `quicbaas.Bootstrap` service name. That is a consequence of what it is:
these are the calls a node makes before it has an identity, a barrier or a
token, so routing them through the authenticated SDK client surface would be
circular.

They are therefore REST routes plus CLI verbs, and the transport matrix used by
other features does not apply. The rows below are the server route table in
`pkg/api/rest/routes.go`, which is the authority.

## `/api/v1/init` — the initialisation ceremony

| Route | Handler | Purpose |
|---|---|---|
| `GET /api/v1/init/status` | `HandleCeremonyStatus` | Where the ceremony has got to |
| `POST /api/v1/init/initialize` | `HandleCeremonyInitialize` | Start the ceremony |
| `GET /api/v1/init/officers` | `HandleCeremonyListOfficers` | Enrolled Shamir officers |
| `POST /api/v1/init/enroll-external-officer` | `HandleCeremonyEnrollExternalOfficer` | Enrol an officer who is not a local principal |
| `POST /api/v1/init/claim-cert/begin` | `HandleCeremonyClaimCertBegin` | Begin claiming an operator certificate |
| `POST /api/v1/init/claim-cert/complete` | `HandleCeremonyClaimCertComplete` | Complete it |
| `POST /api/v1/init/officer-share/begin` | `HandleCeremonyBeginClaimShare` | Begin claiming a Shamir share |
| `POST /api/v1/init/officer-share/complete` | `HandleCeremonyCompleteClaimShare` | Complete it |
| `POST /api/v1/init/sign-csr` | `HandleCeremonySignCSR` | Sign a CSR during the ceremony |

The two-phase `begin`/`complete` pairs exist because both flows involve a
hardware credential — a FIDO2 authenticator or a PIV card — and the ceremony
cannot be a single round trip when a human has to touch a device in the middle.

## `/api/v1/bootstrap` — trust bootstrap

| Route | Handler | Mechanism |
|---|---|---|
| `POST /api/v1/bootstrap/auto` | `HandleBootstrapAuto` | Pick a mechanism automatically |
| `POST /api/v1/bootstrap/dane` | `HandleBootstrapDane` | DANE / DNSSEC-anchored trust |
| `POST /api/v1/bootstrap/noise` | `HandleBootstrapNoise` | Noise_NK handshake |
| `POST /api/v1/bootstrap/spki` | `HandleBootstrapSpki` | SPKI-pinned TLS |

Trust bootstrap is the answer to "how does a node that trusts nothing obtain a
trust anchor it can verify". The three named mechanisms are alternatives, not
layers: `auto` selects among them.

## The sealed window

These routes are reachable **before** the barrier is unsealed, which is the
whole point and also the security consideration. `ServerConfig.SealedBindHost`
exists precisely for this: it binds the pre-unseal listeners independently of
every other `bind_address`, defaulting to loopback.

Set it to a routable address **only** when remote Shamir officers must reach the
ceremony during the sealed window. A deployment that binds its full API on
`0.0.0.0` does not thereby publish its pre-unseal surface, and should not.

## Authentication

The ceremony's authentication is the ceremony itself — hardware credentials,
Shamir shares, and the claim flows above. There is no bearer token yet, because
the thing that issues tokens has not started.

`pkg/api/sealedwindow/` holds the sealed-window request handling; see
`engineering.md`.

## What is not here

- **Per-tenant barrier initialisation** is not part of first boot. See
  [`../multi-tenancy/cli.md`](../multi-tenancy/cli.md).
- **Ongoing barrier operations** — unseal, seal, rekey, recovery — are
  [`../barrier/api.md`](../barrier/api.md). Bootstrap is the one-time path.
- **Certificate issuance in normal operation** is
  [`../certificate-authority/api.md`](../certificate-authority/api.md);
  `init/sign-csr` is the ceremony-time equivalent.
