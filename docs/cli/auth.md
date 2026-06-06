# quicbaas auth

Authentication and N-factor management for QuicBaaS sessions.

## Overview

The `auth` tree manages operator authentication sessions and the
multi-factor enrolment state behind them. Phase 8e replaced the
legacy single-factor login stub with the canonical
`Security().Auth().Factors().Login()` dispatcher, and added the
`auth factor` subgroup that mirrors qrdb's User N-AuthFactor model
verb-for-verb.

All verbs route through the SDK `Security().Auth()` accessor (see
[`docs/sdk/security.md`](../sdk/security.md#auth--authservice`)).
Transport selection is automatic per the global `--transport` flag.

Source:
[`cmd/quicbaas/canonical.go:128`](../../cmd/quicbaas/canonical.go)
(wiring) and the per-verb files
[`cmd/quicbaas/auth_login.go`](../../cmd/quicbaas/auth_login.go) +
[`cmd/quicbaas/auth_factor.go`](../../cmd/quicbaas/auth_factor.go).

## Verb summary

| Verb | Purpose |
|------|---------|
| `login <username>` | Canonical N-factor login dispatcher |
| `logout` | Invalidate the current token and remove it from local storage |
| `status` | Display information about the current authentication session |
| `factor list [--user <id>]` | Enumerate enrolled factors |
| `factor add` | Enrol a new factor |
| `factor remove <id> [--force]` | Revoke an enrolment |
| `factor rotate <old-id>` | Replace an existing factor with a fresh credential |
| `factor relabel <id> --label <s>` | Update the human-readable label of a factor |
| `login-info` | Legacy informational help text (retained for older runbooks) |

## Authentication methods

QuicBaaS authentication is **passwordless** — there is no
username/password verb on the canonical surface. The supported
factor types are:

- **WebAuthn (passkey)**: platform authenticator (Touch ID, Windows
  Hello, biometric) or roaming security key (YubiKey, Titan).
- **FIDO2 / CTAP2**: direct device interaction (USB-HID, PC/SC, BLE).
- **PIV smartcard**: insert a smartcard and enter PIN.
- **mTLS**: provide a TLS client certificate (for service accounts).

The `factor` subgroup manages enrolment for the first three; mTLS is
configured via the global `--tls-cert` / `--tls-key` flags on every
verb.

## login

Authenticate with a QuicBaaS server and obtain an access token using
a hardware authenticator.

```bash
# Login with auto-detected method (the server picks based on enrolled factors)
quicbaas auth login alice@example.com

# Login with WebAuthn explicitly
quicbaas auth login alice@example.com --method webauthn

# Login with PIV smartcard
quicbaas auth login alice@example.com --method smartcard

# Store the token in a custom location
quicbaas auth login alice@example.com --token-file /path/to/token

# Print the token to stdout without storing
quicbaas auth login alice@example.com --no-store

# Login with mTLS client certificate (service account)
quicbaas auth login --address quicbaas.example.com:50051 \
  --tls --tls-cert client.crt --tls-key client.key --tls-ca ca.crt
```

| Flag | Default | Description |
|------|---------|-------------|
| `--token-file <path>` | `~/.quicbaas/token` | Custom token file location |
| `--no-store` | `false` | Don't store token; print to stdout instead |
| `--method <name>` | (auto-detect) | `webauthn` / `fido2` / `smartcard` / `mtls` |
| `--factor-id <id>` | (auto-select) | Pin login to a specific enrolled factor |

Internally `login` calls `Security().Auth().Factors().Login(...)`
with the supplied username and method. The server issues a
challenge, the client signs it with the matching factor, and the
server returns an access + refresh token pair.

## logout

Invalidate the current authentication token and remove it from local
storage.

```bash
quicbaas auth logout
quicbaas auth logout --token-file /path/to/token
```

| Flag | Default | Description |
|------|---------|-------------|
| `--token-file <path>` | `~/.quicbaas/token` | Custom token file location |

The server is notified so the token is revoked server-side as well —
logout is not just a local-file delete.

## status

Display information about the current authentication session.

```bash
quicbaas auth status
quicbaas auth status --output json
```

| Flag | Default | Description |
|------|---------|-------------|
| `--token-file <path>` | `~/.quicbaas/token` | Custom token file location |
| `--output <format>` | `table` | `json` or `table` |

Returns user ID, username, the active factor name, the issued-at and
expires-at timestamps, and the token type.

## factor list

Enumerate enrolled factors for the authenticated user (or a specific
user with `--user`, subject to RBAC).

```bash
quicbaas auth factor list
quicbaas auth factor list --user alice@example.com
quicbaas auth factor list --output json
```

| Flag | Default | Description |
|------|---------|-------------|
| `--user <id>` | (current user) | List factors for a different user (requires permission) |
| `--output <format>` | `table` | `json` or `table` |

Each row shows the factor ID, factor name (`webauthn`, `fido2`,
`piv`, `mtls`), label, and enrolled-at timestamp.

## factor add

Enrol a new factor.

```bash
# Enrol a WebAuthn passkey (interactive — server prompts via the auth ceremony)
quicbaas auth factor add --name webauthn --label "Laptop passkey"

# Enrol a PIV smartcard
quicbaas auth factor add --name piv --label "Corporate ID"

# Enrol an mTLS client certificate (cert pre-issued)
quicbaas auth factor add --name mtls \
  --label "Service: ingest" \
  --proof @/etc/quicbaas/svc-ingest.crt
```

| Flag | Default | Description |
|------|---------|-------------|
| `--name <factor>` | (required) | `webauthn` / `fido2` / `piv` / `mtls` |
| `--label <string>` | (none) | Human-readable label for the factor |
| `--proof @<path>` or `--proof <inline>` | (factor-specific) | Factor-specific proof material |
| `--user <id>` | (current user) | Enrol the factor for a different user (admin-only) |

The canonical SDK call:

```go
sdk.Security().Auth().Factors().Add(ctx, &quicbaas.AuthFactorEnrollRequest{
    FactorName: "webauthn",
    Subject:    userID,
    Label:      "Laptop passkey",
    // ... factor-specific metadata
})
```

## factor remove

Revoke an enrolment.

```bash
quicbaas auth factor remove fctr_01HXY...
quicbaas auth factor remove fctr_01HXY... --force   # allow removing the last factor
```

| Flag | Default | Description |
|------|---------|-------------|
| `--force` | `false` | Allow removing the last remaining factor (puts the user in inactive state) |

Without `--force`, removing the user's last factor is rejected with a
`LastFactorRemovalError` to prevent accidental lockout.

## factor rotate

Replace an existing factor with a fresh credential in a single
transaction. Use when an authenticator is lost or compromised — the
old credential is revoked atomically as the new one is enrolled.

```bash
quicbaas auth factor rotate fctr_01HXY... \
  --new-name webauthn \
  --new-label "Replacement passkey"
```

| Flag | Default | Description |
|------|---------|-------------|
| `--new-name <factor>` | (required) | Factor name for the replacement |
| `--new-label <string>` | (none) | Label for the replacement |
| `--new-proof @<path>` | (factor-specific) | Proof material for the replacement |

## factor relabel

Update the human-readable label of an enrolled factor.

```bash
quicbaas auth factor relabel fctr_01HXY... --label "New label"
```

| Flag | Default | Description |
|------|---------|-------------|
| `--label <string>` | (required) | New label |

## Token storage

Tokens are stored at `~/.quicbaas/token` by default, with mode
`0o600`. The file is a JSON object with `access_token`,
`refresh_token`, `token_type`, and `expires_at` fields. Subsequent
CLI commands that require authentication read the file
automatically; explicit `--token-file` overrides the default
location.

## Errors

| Type | When |
|------|------|
| `AuthLoginNoMethodError` | `login` cannot determine a method and the user has no enrolled factors |
| `AuthFactorNameRequiredError` | `factor add` invoked without `--name` |
| `AuthFactorIDRequiredError` | `factor remove` / `rotate` / `relabel` invoked without a factor ID |
| `LastFactorRemovalError` | `factor remove` would leave the user with no enrolled factors and `--force` was not supplied |

## See also

- [`docs/sdk/security.md`](../sdk/security.md) — full `Security().Auth()`
  surface including FIDO2 / WebAuthn / PIV / Factors sub-services.
- [`docs/security/authentication.md`](../security/authentication.md) —
  server-side authentication architecture.
- [`docs/cli/iam.md`](iam.md) — user, role, and group management.
- [`docs/auth/passwordless.md`](../auth/passwordless.md) — passwordless
  authentication background.
