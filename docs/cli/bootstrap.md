# quicbaas bootstrap

Trust-bootstrap the CA bundle from a QuicBaaS server without prior
trust.

## Overview

The `bootstrap` tree fetches a server's CA bundle from a fresh client
that has no a-priori knowledge of the server's PKI. Verbs mirror
`xkmsctl bootstrap` so operators have a single consistent surface;
under the hood every verb delegates to the canonical
`xkms/pkg/trust/bootstrap` library through the SDK re-export
[`sdk/go/xkms/trust/bootstrap`](../../sdk/go/xkms/trust/bootstrap)
and routes through `Security().Trust().Bootstrap()`.

There is exactly **one** trust-bootstrap implementation across the
entire stack — Phase 7 collapsed the three previously-diverging
copies in xkms, the xkey GUI, and platform into this single library.

## When to use bootstrap

Bootstrap is for the very first connection from a new client to a
QuicBaaS deployment. Once you have the CA bundle, mTLS handles the
rest. Typical scenarios:

- A new node joining an existing cluster.
- A new operator workstation that has not yet pulled the bundle.
- Disaster recovery against a known-good DNS / Noise channel.
- Tooling (CI runners, ephemeral containers) that fetches the bundle
  at runtime.

## Verb summary

| Verb | Purpose |
|------|---------|
| `auto` | Try methods in priority order (DANE → Noise → SPKI → Direct) |
| `dane` | DANE TLSA verification (DNSSEC required) |
| `noise` | Noise IK channel with pre-shared static public key |
| `spki` | SPKI-pin verification against a known SHA-256 pin |
| `direct` | Direct HTTPS fetch (warning: trust-on-first-use) |

(Verb-level flag pages are emitted by `--help`; this page documents
the cross-cutting behaviour.)

## auto

Trust-bootstrap by trying methods in priority order:

```
DANE  →  Noise  →  SPKI  →  Direct
```

Each method is attempted with its own configuration; the first to
succeed wins. The verb supports `--method` to pin a single method,
which is equivalent to running the per-method verb directly.

```bash
quicbaas bootstrap auto --server https://quicbaas.local
```

## dane

DANE TLSA verification requires DNSSEC and a published `_443._tcp.<host>`
TLSA record. The bootstrap client resolves the TLSA record via the
configured resolver, then verifies the TLS certificate the server
presents against the TLSA hash.

```bash
quicbaas bootstrap dane \
  --server https://quicbaas.local \
  --resolver 8.8.8.8:53
```

## noise

Noise IK uses a pre-shared static public key out-of-band; the
bootstrap channel dials a Noise listener (separate from the main
TLS listener) and verifies the server identity against the
pre-shared key before downloading the bundle.

```bash
quicbaas bootstrap noise \
  --server https://quicbaas.local \
  --static-key <base64-noise-public-key>
```

Use `Security().Trust().Bootstrap().DeriveNoiseAddr(serverURL)` to
compute the canonical Noise host:port from the server URL when
scripting; the CLI does the same derivation internally.

## spki

SPKI-pin verification compares the SHA-256 of the server's
Subject Public Key Info against an operator-supplied pin.

```bash
quicbaas bootstrap spki \
  --server https://quicbaas.local \
  --pin sha256:abcd1234...
```

The pin is typically published alongside the server hostname (in
documentation, a configuration management system, or a previous
bundle exchange).

## direct

Direct HTTPS fetch with no out-of-band verification. **Trust on
first use** — the client accepts whatever certificate the server
presents on the first call and pins it for the lifetime of the
local bundle store.

```bash
quicbaas bootstrap direct --server https://quicbaas.local
```

The CLI emits a `WARNING: trust-on-first-use` line to stderr so the
operator notices.

## TrustBootstrapClient SDK contract

The CLI is a thin wrapper around `Security().Trust().Bootstrap()`:

```go
boot := sdk.Security().Trust().Bootstrap()
bundle, err := boot.Fetch(ctx, trustbootstrap.Config{
    ServerURL:  "https://quicbaas.local",
    Method:     trustbootstrap.MethodDANE,
    DANEConfig: &trustbootstrap.DANEConfig{ Resolver: "8.8.8.8:53" },
})
```

`bundle` is a `*trustbootstrap.CABundle` carrying the PEM-encoded
chain, the verification method that succeeded, and a timestamp. The
caller is responsible for writing the bundle to disk (the CLI does
this under `~/.quicbaas/trust/` by default; programmatic callers
pick their own destination).

`Fetch` is a **local-only** call: it performs a direct outbound
network fetch from the caller, not a transport hop through the
configured QuicBaaS backend. The SDK acts as a library facade here.

## Output

Successful runs print the bundle path and the method that succeeded:

```
bundle: /home/operator/.quicbaas/trust/quicbaas.local/ca-bundle.pem
method: dane
fetched_at: 2026-05-25T10:14:33Z
```

`--output json` returns the same fields as a JSON object.

## See also

- [`docs/sdk/security.md`](../sdk/security.md) —
  `Security().Trust().Bootstrap()` SDK contract.
- xkms `pkg/trust/bootstrap` — the canonical implementation
  (single-source-of-truth across the stack).
- [Locked architecture spec](../../../automatethethings/software/docs/08-bootstrap-auth-architecture.md)
  §7 — trust-bootstrap mode descriptions and security analysis.
