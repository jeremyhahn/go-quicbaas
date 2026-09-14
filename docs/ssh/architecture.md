# SSH — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design and boundaries of the server-held SSH key surface. Feature folder:
> `docs/ssh/`.

## The problem

Agent deployment and fleet operations need SSH keys. The naive shape — write a
private key to disk on the machine that runs the deploy — puts long-lived
credential material on whichever host happens to be orchestrating, and leaves it
there.

This feature exists so the private half never leaves the key store. A caller
asks for a key to be *generated* and later for a digest to be *signed*; it never
holds the key.

## The shape chosen

A vault-side key store with a signing operation, not a key-handing-out service.

`quicbaas.SSH` (`sdk/go/client/ssh/types.go:155`) carries fourteen methods
across three resource groups:

| Group | Methods |
|---|---|
| Keys | `GenerateKey`, `ImportKey`, `GetKey`, `ListKeys`, `DeleteKey`, `ExportPublicKey`, `SignDigest` |
| Known hosts | `AddKnownHost`, `ListKnownHosts`, `RemoveKnownHost` |
| Authorized keys | `AddAuthorizedKey`, `ListAuthorizedKeys`, `RemoveAuthorizedKey`, `EmitAuthorizedKey` |

Note what is present and what is not: `ExportPublicKey` exists; there is no
`ExportPrivateKey`. `SignDigest` exists; there is no "give me the key so I can
sign locally". That asymmetry is the whole design.

### Alternatives rejected

- **Handing the private key to the caller.** Rejected: it makes every consumer a
  place a key can leak from, and it makes revocation a question of trusting
  every host to have deleted its copy.
- **A generic crypto signing surface with SSH as a key type.** Rejected because
  the SSH resources — known hosts, authorized-keys lines — are not crypto
  operations and have nowhere to live in such a surface. Generic signing is
  `../kms/`'s; this feature is the SSH-shaped surface on top.
- **An SSH jump host.** `Session().Dial` and `Console` are deliberately
  unimplemented. Making the vault a jump host would put an interactive shell
  path inside the process holding every key, which is the opposite of what the
  key store is for. This is a stated non-goal, not a gap.

## Component boundaries

- **This repository** — the SSH service surface: the API, the CLI, the tenanted
  storage of keys and host records.
- **go-xkms** — the crypto. Keys can be held by a hardware or cloud backend
  through the composed stack; this feature does not implement key storage
  primitives. Reached through the composition seam (`../composition/`).
- **`sdk/go/transport/rest/ssh_handler.go`** — mounts everything under
  `/api/v1/ssh` (`sshRESTBasePath`, `:28`), the same mount the go-xkms REST SSH
  client alias targets, so the SDK and that client observe one server surface
  rather than two that drift.
- **`../software-factory/`** — the largest consumer. `quicbaas agent deploy
  --ssh-key-name` names a key held here; the private half stays here for the
  whole deployment.

## Data flow — deploying with a server-held key

```
operator                     quicbaasd                        target host
   │                             │                                 │
   │ ssh keys generate ─────────►│ key created, private half        │
   │                             │ stored server-side               │
   │ ssh keys export-public ────►│                                  │
   │◄──── public key ────────────│                                  │
   │                                                                │
   │ (operator installs the public key in authorized_keys) ────────►│
   │                                                                │
   │ agent deploy --ssh-key-name │                                  │
   │ ───────────────────────────►│ SignDigest during the handshake  │
   │                             │ ────────────────────────────────►│
```

The operator never sees the private key, and neither does the deploy path: the
handshake's signature is produced by the server.

## Trust boundaries

- **The key store is the trust root for fleet access.** Compromising it is
  equivalent to compromising every host that trusts a key it holds.
- **`SignDigest` is a signing oracle.** Any principal permitted to call it can
  authenticate as that key without ever holding it. The permission on that route
  is therefore the real access-control boundary, not possession of key material.
  `api.md` records the `resource:action` for every route.
- **Known hosts are an integrity boundary.** A writable known-hosts store means
  an attacker who can add a record can present a substituted host without
  tripping verification.
- **Tenancy.** Keys and host records are tenant-scoped; see
  `../multi-tenancy/`.

## What this feature deliberately does not do

- **No interactive SSH.** No `Dial`, no `Console`, no port forwarding. The vault
  is not a jump host.
- **No agent forwarding.** There is no `ssh-agent` protocol surface.
- **No key rotation policy engine.** Rotation is generate-plus-redistribute,
  driven by the operator. Automatic rotation belongs to `../kms/` for the key
  types it manages.
- **No config management.** Installing a public key into a target host's
  `authorized_keys` is the operator's step; `EmitAuthorizedKey` renders the
  line, it does not deliver it.
