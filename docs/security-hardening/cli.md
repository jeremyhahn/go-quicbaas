# Security Hardening — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Feature folder: `docs/security-hardening/`.

**No dedicated command group.** Hardening is not a verb; it is the posture a
deployment is configured into. There is no `quicbaas harden` and there should
not be one, because a command that "hardens" a running server would be a
configuration change applied at the wrong moment.

What exists at the command line is the small set of flags that select a
security-relevant mode at start, plus the verbs in other feature groups that
report or change posture.

## Flags that select posture

| Flag | Binary | Default | Effect |
|---|---|---|---|
| `--fips` | both | `false` | FIPS 140-3 mode (`cmd/quicbaas/main.go:163`, `cmd/quicbaasd/main.go:198`), bound to `security.fips_mode` |
| `--tls` | `quicbaas` | `false` | Enable TLS on the client transport |
| `--tls-ca` | `quicbaas` | auto-detected from `--data-dir` | Trust anchor |
| `--tls-cert` / `--tls-key` | `quicbaas` | none | mTLS client credentials |
| `--http-address` | `quicbaasd` | config, else `127.0.0.1` | Bind address for REST. **`0.0.0.0` publishes the API.** |

```bash
quicbaasd start --fips --http-port 18080 --http-address 127.0.0.1
quicbaas --tls --tls-ca /etc/quicbaas/ca.crt --tls-cert client.crt --tls-key client.key iam user list
```

The default bind is loopback on purpose. Changing it is the single
highest-consequence flag on either binary, and it is worth writing explicitly in
a unit file rather than inheriting from a config file somebody else edits.

## Verbs elsewhere that report or change posture

```bash
quicbaas barrier status              # sealed or not — ../barrier/
quicbaas iam session list            # live sessions — ../iam/
quicbaas iam audit query --outcome failure   # denials — ../audit-logging/
quicbaas iam tenant barrier status   # per-tenant isolation — ../multi-tenancy/
quicbaas dek set-safety <path>       # destructive-op interlock — ../data-encryption-keys/
```

## Where the actual settings live

Almost all of hardening is configuration, not command line:

- `security.*` — authz mode, TLS client auth, liveness cache TTL, and the
  `insecure_dev_mode` switch that must never be true outside a laptop. These
  **do** accept environment overrides: `QUICBAAS_SECURITY_AUTHZ_MODE`,
  `QUICBAAS_SECURITY_REQUIRE_AUTHZ`, `QUICBAAS_SECURITY_TLS_CLIENT_AUTH`,
  `QUICBAAS_SECURITY_INSECURE_DEV_MODE`, `QUICBAAS_SECURITY_LIVENESS_CACHE_TTL`
  are all in the explicit list `pkg/config/loader.go` reads.
- `server.host` — the one bind host for every API listener. A per-protocol
  `server.*.bind_address` is refused at load: the embedded node never honoured
  it, so isolate a listener at the network boundary instead.
- `server.sealed_bind_host` — the pre-unseal surface, bound independently and
  defaulting to loopback. See [`../bootstrap/user.md`](../bootstrap/user.md).
- `raft.*` mTLS — `QUICBAAS_RAFT_MUTUAL_TLS` and its cert/key/CA companions.

`engineering.md` is the reference for all of them.
