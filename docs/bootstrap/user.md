# Bootstrap — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented guide to bringing a new deployment from nothing to a running,
> unsealed server. Feature folder: `docs/bootstrap/`.

Bootstrap is the one-time path: the initialisation ceremony that creates the
barrier, enrols officers, issues the first certificates, and establishes a trust
anchor a node can verify. Everything after that is
[`../barrier/`](../barrier/user.md).

## Prerequisites

- A host with the data directory you intend to keep. `--data-dir` is where the
  barrier and its state live; moving it later is a migration, not a config
  change.
- A decision on seal strategy: passphrase, or Shamir with a named threshold and
  named officers. Make it now — see [`../barrier/user.md`](../barrier/user.md).
- For Shamir, the officers reachable during the ceremony, with their hardware
  credentials in hand.

## First run

**The operator CLI does not bootstrap-initialise a remote daemon.** That is
worth stating up front because it is the opposite of what the command names
suggest. `quicbaas init` is an *operator-side* group with five verbs — `status`,
`claim-cert`, `claim-share`, `sign-csr`, `enroll-external-officer`
(`cmd/quicbaas/init.go`) — matching `xkmsctl init` verb for verb. The
single-call bootstrap form it once had was retired; initialisation is driven on
the daemon side by `quicbaasd init` (`cmd/quicbaas/canonical.go:25-37`).

```bash
# 1. Initialise, on the daemon, on the host that will hold the data.
quicbaasd init --data-dir /var/lib/quicbaas --barrier-strategy passphrase

# 2. Start it. Before initialisation it serves only the sealed-window surface.
quicbaasd start --data-dir /var/lib/quicbaas --http-port 18080

# 3. From an operator shell: where did the ceremony get to?
quicbaas init status
```

`quicbaasd init` carries the ceremony flags
(`cmd/quicbaasd/init.go:122-151`): `--barrier-strategy` (default `passphrase`),
`--passphrase`, `--shares`, `--threshold`, `--so-pin`, `--user-pin`,
`--credential-strategy` (default `barrier`), `--admin-factor`,
`--passkey-device`, `--hostname`, `--sans`, `--init-data-dir`.

## The sealed window

Between process start and a successful unseal, the server is in the **sealed
window**. Only the ceremony and unseal surfaces answer.

`ServerConfig.SealedBindHost` binds those listeners independently of every other
`bind_address`, and defaults to loopback. **Leave it there unless remote Shamir
officers must reach the ceremony**, in which case set it to a routable address
deliberately and narrow it again afterwards. Binding the full API on `0.0.0.0`
does not expose the pre-unseal surface — that separation is the point of the
setting.

## Common tasks

### Enrol officers and distribute shares

```bash
quicbaas init enroll-external-officer
quicbaas init claim-share
```

These are operator-side verbs against a daemon that `quicbaasd init` has already
initialised.

`claim-share` is two-phase on the wire (`officer-share/begin` then
`complete`) because the officer touches a hardware credential in between. From
the CLI it is one command; from the API it is two calls.

### Claim an operator certificate

```bash
quicbaas init claim-cert
```

Also two-phase for the same reason.

### Sign a CSR during the ceremony

```bash
quicbaas init sign-csr
```

This is the ceremony-time path. In normal operation, certificate issuance is
[`../certificate-authority/`](../certificate-authority/user.md).

### Establish trust from a node that trusts nothing

```bash
quicbaas bootstrap auto
```

`auto` selects among three mechanisms; you can name one instead — DANE,
Noise_NK, or SPKI-pinned TLS. They are alternatives, not layers. Which is right
depends on what your environment can already vouch for: DNSSEC, a pre-shared
static key, or a pinned public key.

### Migrate an existing deployment

```bash
quicbaas migrate
```

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Every API call 503s on a fresh server | Correct: the barrier is not initialised. Run the ceremony. |
| A remote officer cannot reach the ceremony | `sealed_bind_host` defaults to loopback. It is separate from every other bind address. |
| Ceremony completes but the server stays sealed | Initialisation and unseal are different steps. `quicbaas barrier unseal`. |
| `quicbaas init` offers no way to initialise | Correct. It has five verbs and none of them bootstrap. Initialisation is `quicbaasd init`, on the daemon. |
| `quicbaasd init` says already initialised | It is once-only. You want `barrier unseal` or `barrier status`. |
| Shamir ceremony stalls partway | A two-phase claim was begun and not completed — the officer did not touch the device. `init status` shows where it stopped. |
| Data directory moved and nothing works | The barrier state lives there. Move it back or treat it as a restore. |

## What this feature does not do

- **It does not run twice.** Initialisation is once per barrier. There is no
  re-init that preserves data.
- **It does not unseal.** Ceremony creates; unseal opens. Different verbs,
  different feature.
- **It does not provision hosts.** Getting the binary onto a machine is outside
  this repository.
