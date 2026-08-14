# quicbaas auth

Authentication and N-factor management for QuicBaaS sessions.

## Overview

The `auth` tree manages operator authentication sessions and the
multi-factor enrolment state behind them. Login is the unified
ceremony — `Security().Auth().BeginLogin` / `FinishLogin` — and the
`auth factor` subgroup mirrors qrdb's User N-AuthFactor enrolment
model verb-for-verb (add / list / remove / rotate / relabel).

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

- **FIDO2 / WebAuthn (passkey)**: local CTAP2 authenticator over
  USB-HID (YubiKey, SoloKey, Nitrokey, ...), driven with the
  server-issued WebAuthn options.
- **PIV**: client-local go-xkms PIV backend (`file` / `pkcs11` /
  `tpm2`); physical smart cards are reachable through the `pkcs11`
  backend. There is no PC/SC reader model.
- **mTLS**: TLS client certificate (service accounts).

The `factor` subgroup manages enrolment for all three factor types.

## login

Authenticate with a QuicBaaS server and obtain an access token using
a hardware authenticator.

```bash
# Login with auto-discovery (client cert, then FIDO2 device, then local PIV slot)
quicbaas auth login --username alice@example.com

# Login with a FIDO2 authenticator explicitly
quicbaas auth login --username alice@example.com --method fido2

# Login with the local PIV backend (file backend by default)
quicbaas auth login --username alice@example.com --method piv \
  --piv-backend file --piv-dir ~/.quicbaas/piv --slot 9a

# Login with a PKCS#11 smart card
quicbaas auth login --username alice@example.com --method piv \
  --piv-backend pkcs11 --pkcs11-module /usr/lib/libykcs11.so --pin 123456

# Login with mTLS client certificate (service account)
quicbaas auth login --method mtls --client-cert client.pem
```

| Flag | Default | Description |
|------|---------|-------------|
| `--method <name>` | (auto-discover) | `mtls` / `fido2` / `piv` |
| `--username <s>` | (none) | Username (subject) for login |
| `--factor <id>` | (auto-select) | Pin login to a specific enrolled factor |
| `--client-cert <path>` | `~/.quicbaas/admin.pem` | Client certificate PEM (`--method mtls`) |
| `--device <path>` | (first connected) | FIDO2 local HID device path (`--method fido2`) |
| `--origin <url>` | `https://<rpId>` | WebAuthn origin override (`--method fido2`) |
| `--piv-backend <name>` | `file` | Local PIV backend: `file` / `pkcs11` / `tpm2` (`--method piv`) |
| `--piv-dir <path>` | `~/.quicbaas/piv` | Local PIV key/cert dir (`file` backend) |
| `--pkcs11-module <path>` | (none) | PKCS#11 library path (`pkcs11` backend) |
| `--pkcs11-token <label>` | (none) | PKCS#11 token label (`pkcs11` backend) |
| `--slot <id>` | `9a` | PIV slot id (`--method piv`) |
| `--pin <s>` | `$QUICBAAS_USER_PIN` | PKCS#11 User PIN (`pkcs11` backend) |
| `--save-token` | `false` | Persist the issued session token to `--token-path` |

Internally `login` drives the unified ceremony:
`Security().Auth().BeginLogin(...)` obtains the factor-typed
challenge, the client produces the proof locally (mTLS handshake /
CTAP2 assertion / local PIV signature over the nonce), and
`Security().Auth().FinishLogin(...)` verifies it server-side and
returns the bearer + refresh token.

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

Each row shows the factor ID, factor type (`fido2`, `piv`,
`mtls`), label, and enrolled-at timestamp.

## factor add

Enrol a new factor.

```bash
# Enrol a FIDO2 passkey (drives the connected CTAP2 authenticator
# through the server-issued WebAuthn registration ceremony)
quicbaas auth factor add --type fido2 --label "Laptop passkey" --user alice@example.com

# Enrol a PIV factor from the client-local PIV backend slot
quicbaas auth factor add --type piv --label "Corporate ID" \
  --piv-backend file --piv-dir ~/.quicbaas/piv --slot 9a

# Enrol an mTLS client certificate (cert pre-issued)
quicbaas auth factor add --type mtls \
  --label "Service: ingest" \
  --cert /etc/quicbaas/svc-ingest.crt
```

| Flag | Default | Description |
|------|---------|-------------|
| `--type <factor>` | (required) | `fido2` / `piv` / `mtls` |
| `--label <string>` | (required) | Human-readable label for the factor |
| `--user <id>` | (current user) | Subject to enrol the factor for |
| `--tenant <id>` | (none) | Tenant scope |
| `--cert <path>` | (none) | Certificate PEM path (`--type mtls`) |
| `--device <path>` | (first connected) | FIDO2 local HID device (`--type fido2`) |
| `--origin <url>` | `https://<rpId>` | WebAuthn origin override (`--type fido2`) |
| `--piv-backend <name>` | `file` | Local PIV backend: `file` / `pkcs11` / `tpm2` (`--type piv`) |
| `--piv-dir <path>` | `~/.quicbaas/piv` | Local PIV key/cert dir (`file` backend) |
| `--slot <id>` | `9a` | PIV slot id (`--type piv`) |
| `--pin <s>` | (none) | PKCS#11 User PIN (`pkcs11` backend) |
| `--pkcs11-module <path>` | (none) | PKCS#11 library path (`pkcs11` backend) |
| `--pkcs11-token <label>` | (none) | PKCS#11 token label (`pkcs11` backend) |

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
| `NoFactorsAvailableError` | `login` auto-discovery finds no client cert, no FIDO2 device, and no local PIV certificate |
| `InvalidFactorTypeError` | `factor add` invoked without `--type` or with an unknown type |
| `MissingLabelError` | `factor add` / `rotate` invoked without `--label` |
| `MissingFactorIDError` | `factor remove` / `rotate` invoked without a factor ID |
| `InvalidPIVBackendError` | `--piv-backend` is not `file` / `pkcs11` / `tpm2` |
| `PIVPINRequiredError` | `--method piv` on the `pkcs11` backend without `--pin` / `QUICBAAS_USER_PIN` |

## See also

- [`docs/sdk/security.md`](../sdk/security.md) — full `Security().Auth()`
  surface including FIDO2 / PIV / Devices / Factors sub-services.
- [`docs/security/authentication.md`](../security/authentication.md) —
  server-side authentication architecture.
- [`docs/cli/iam.md`](iam.md) — user, role, and group management.
- [`docs/auth/passwordless.md`](../auth/passwordless.md) — passwordless
  authentication background.
