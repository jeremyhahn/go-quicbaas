# quicbaas ceremony

Drive the QuicBaaS initialisation ceremony state machine.

## Overview

The ceremony subcommand exposes the same verb set as the top-level
`quicbaas init` command. Both reach the same SDK
`Security().Ceremony()` backend; the two surfaces give operators a
choice of whether to think of the flow as "initialise this daemon"
(`init`) or "drive the ceremony state machine" (`ceremony`).

Use `ceremony` when:

- The data dir was initialised with `--barrier-strategy shamir` and
  the daemon is now running with `pending_ceremony.json` waiting for
  officer participation.
- You are completing or auditing an in-progress ceremony from an
  operator workstation other than the daemon host.

Source:
[`cmd/quicbaas/canonical.go:69`](../../cmd/quicbaas/canonical.go)
and [`cmd/quicbaas/init.go`](../../cmd/quicbaas/init.go) (the two
share Cobra builders so the verb trees stay byte-for-byte identical).

## Verb summary

| Verb | Purpose |
|------|---------|
| `status` | Show the current ceremony state |
| `claim-cert` | Two-step certificate claim (`begin`, then `complete`) |
| `claim-share` | Two-step Shamir share claim (`begin`, sign nonce, `complete`) |
| `sign-csr` | SO-authorised CSR signing |
| `enroll-external-officer` | Enrol an officer whose key material is external |

## Global flags

`ceremony` accepts `--output <json|yaml|table>` (default `table`)
through the persistent flag.

## status

Display the current ceremony state. Returns the active strategy, the
expected officer count, the number of officers that have completed
each step (cert claim, share claim, CSR sign), and any pending steps
that block forward progress.

```bash
quicbaas ceremony status
quicbaas ceremony status --output json
```

Empty / "no ceremony in progress" state returns a clear "not
initialised" message rather than a zero-valued struct.

## claim-cert

Two-step certificate claim. The officer presents the material that
matches the `KIND` they were enrolled under (`csr`, `cert`, or
`pubkey`) and the daemon issues a signed officer certificate.

### claim-cert begin

```bash
quicbaas ceremony claim-cert begin \
  --officer alice \
  --material /etc/quicbaas/officers/alice.csr
```

`begin` validates the material against the recorded officer kind,
emits a server-side challenge nonce, and returns a claim token the
operator carries to `complete`.

### claim-cert complete

```bash
quicbaas ceremony claim-cert complete \
  --officer alice \
  --token <opaque-from-begin> \
  --proof <signed-challenge>
```

`complete` verifies the proof against the claim token and, on
success, persists the officer's certificate to the ceremony state
and emits the issued certificate PEM on stdout.

## claim-share

Claim the Shamir share assigned to a specific officer via the
challenge-response handshake. The daemon issues a challenge nonce,
the CLI signs it with the officer's certificate private key
(`--tls-key`), and the daemon verifies the signature (and the mTLS
certificate thumbprint against the enrolment record) before
releasing the share exactly once.

```bash
quicbaas init claim-share     --username alice     --tls-cert alice.crt     --tls-key alice.key     --output alice.share.json
```

The share JSON is written to `--output`. Storage is the officer's
responsibility — the daemon does not retain a claimable copy after
the share has been released.

## sign-csr

Submit a CSR for SO-authorised signing. The SO must have completed
their own claim flow; the CSR is signed by the daemon CA and the
issued certificate is returned on stdout.

```bash
quicbaas ceremony sign-csr --csr /etc/quicbaas/csr/svc.csr
```

## enroll-external-officer

Enrol an additional officer post-init. The flag set is the same
shape as `quicbaasd init --so`:

```bash
quicbaas ceremony enroll-external-officer \
  --username dan \
  --kind cert \
  --material /etc/quicbaas/officers/dan.pem
```

The new officer participates in subsequent claim flows but cannot
retroactively change the share threshold. Use this when a security
officer rotates off the team and you need to issue a replacement
without rerunning init.

## Transport behaviour

| Transport | Behaviour |
|-----------|-----------|
| REST | `POST /api/v1/ceremony/*` JSON endpoints |
| gRPC | `quicbaas.v1.CeremonyService` RPCs |
| QUIC | Native QUIC ceremony handler (Phase 8b/c/d) |
| MCP | `quicbaas.ceremony.*` tools (Phase 8b/c/d) |
| Unix | gRPC over Unix domain socket |
| Embedded | In-process via `Security().Ceremony()` (the bootstrap ceremony stack is wired automatically — see [`docs/architecture/embedded-mode.md`](../architecture/embedded-mode.md)) |

## See also

- [`docs/sdk/security.md`](../sdk/security.md) —
  `Security().Ceremony()` accessor.
- [`docs/cli/init.md`](init.md) — `quicbaasd init` (daemon-side
  ceremony setup at first boot).
- [`docs/cli/barrier.md`](barrier.md) — barrier verbs the ceremony
  ultimately unlocks.
- [Locked architecture spec](../../../automatethethings/software/docs/08-bootstrap-auth-architecture.md)
  §6 — canonical verb set the four operator binaries share.
