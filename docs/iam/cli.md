# IAM — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag, with runnable examples. Feature folder: `docs/iam/`.

Identity and Access Management commands for QuicBaaS.

## Overview

The IAM subsystem provides comprehensive identity and access management capabilities including multi-tenancy, role-based access control, OAuth 2.0 / OIDC authentication, and attribute-based access control (ABAC).

## Global Flags

All IAM commands support these flags:

| Flag | Description | Default |
|------|-------------|---------|
| `--tenant` | Tenant ID (required for most operations) | - |
| `--output` | Output format (json, yaml, table) | `table` |
| `-y, --yes` | Skip confirmation prompts | `false` |

## Tenant Management

### tenant create

Create a new tenant.

```bash
quicbaas iam tenant create <id>
```

#### Flags
- `--name string`: Tenant display name (required)
- `--max-users int`: Maximum number of users (0 = unlimited)
- `--mfa-required`: Require MFA for all users

#### Examples

```bash
quicbaas iam tenant create acme-corp --name "Acme Corporation"
quicbaas iam tenant create acme-corp --name "Acme Corp" --max-users 1000 --mfa-required
```

### tenant get

Get tenant details.

```bash
quicbaas iam tenant get <id>
```

### tenant update

Update tenant settings.

```bash
quicbaas iam tenant update <id>
```

#### Flags
- `--name string`: New tenant name
- `--max-users int`: Maximum number of users
- `--mfa-required`: Require MFA for all users

### tenant delete

Delete a tenant. WARNING: This deletes all associated resources.

```bash
quicbaas iam tenant delete <id>
```

### tenant list

List all tenants.

```bash
quicbaas iam tenant list
```

## User Management

### user create

Create a new user within a tenant in `pending` status. The user must activate by enrolling a hardware authenticator (WebAuthn passkey, security key, or PIV smartcard).

```bash
quicbaas iam user create <email>
```

#### Flags
- `--tenant string`: Tenant ID (required)
- `--name string`: User display name (required)
- `--roles stringSlice`: Roles to assign (comma-separated)

#### Examples

```bash
quicbaas iam user create alice@acme.com --tenant acme-corp --name "Alice Smith"
quicbaas iam user create alice@acme.com --tenant acme-corp --name "Alice" --roles admin,developer
```

**No Password Field:** QuicBaaS is passwordless. Users enroll hardware authenticators instead. See [Passwordless Authentication](architecture.md).

### user get

Get user details.

```bash
quicbaas iam user get <user-id> --tenant <tenant-id>
```

### user update

Update user information.

```bash
quicbaas iam user update <user-id> --tenant <tenant-id>
```

#### Flags
- `--name string`: New user name
- `--roles stringSlice`: New roles for user

### user delete

Delete a user.

```bash
quicbaas iam user delete <user-id> --tenant <tenant-id>
```

### user list

List users in a tenant.

```bash
quicbaas iam user list --tenant <tenant-id>
```

#### Flags
- `--role string`: Filter by role

### user role assign

Assign a role to a user.

```bash
quicbaas iam user role assign --tenant <tenant-id> --user <user-id> --role <role-id>
```

### user role revoke

Revoke a role from a user.

```bash
quicbaas iam user role revoke --tenant <tenant-id> --user <user-id> --role <role-id>
```

## Role Management

### role create

Create a new role.

```bash
quicbaas iam role create <id> --tenant <tenant-id>
```

#### Flags
- `--name string`: Role display name (required)
- `--description string`: Role description

### role get

Get role details.

```bash
quicbaas iam role get <id> --tenant <tenant-id>
```

### role update

Update role information.

```bash
quicbaas iam role update <id> --tenant <tenant-id>
```

#### Flags
- `--name string`: New role name
- `--description string`: New role description

### role delete

Delete a role.

```bash
quicbaas iam role delete <id> --tenant <tenant-id>
```

### role list

List roles in a tenant.

```bash
quicbaas iam role list --tenant <tenant-id>
```

### role assign

Assign a role to a user.

```bash
quicbaas iam role assign <user-id> <role-id> --tenant <tenant-id>
```

### role revoke

Revoke a role from a user.

```bash
quicbaas iam role revoke <user-id> <role-id> --tenant <tenant-id>
```

## OAuth Application Management

### app register

Register a new OAuth application.

```bash
quicbaas iam app register --name <app-name> --tenant <tenant-id>
```

#### Flags
- `--name string`: Application name (required)
- `--redirect-uri string`: OAuth redirect URI (single URI)
- `--redirect-uris stringSlice`: OAuth redirect URIs (comma-separated)
- `--resource-prefix string`: Resource prefix for ABAC policies

#### Examples

```bash
quicbaas iam app register --name my-app --tenant acme-corp
quicbaas iam app register --name my-app --tenant acme-corp --redirect-uri "https://app.com/callback"
```

### app get

Get application details.

```bash
quicbaas iam app get <client-id>
```

### app update

Update application settings.

```bash
quicbaas iam app update <client-id>
```

#### Flags
- `--name string`: New application name
- `--redirect-uri string`: New redirect URI
- `--redirect-uris stringSlice`: New redirect URIs

### app delete

Delete an application.

```bash
quicbaas iam app delete <client-id>
```

### app list

List applications in a tenant.

```bash
quicbaas iam app list --tenant <tenant-id>
```

### app rotate-secret

Rotate application client secret.

```bash
quicbaas iam app rotate-secret <client-id>
```

## Policy Management (ABAC)

### policy apply

Apply an ABAC policy from a YAML file.

```bash
quicbaas iam policy apply -f <policy-file>
```

#### Example Policy File

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: order-access
  tenant: acme-corp
spec:
  rules:
    - name: read-own
      effect: allow
      actions: [read]
      resources:
        - orders://orders/${{subject.id}}/**
      subjects:
        authenticated: true
```

### policy get

Get policy details.

```bash
quicbaas iam policy get <name> --tenant <tenant-id>
```

### policy delete

Delete a policy.

```bash
quicbaas iam policy delete <name> --tenant <tenant-id>
```

### policy list

List policies in a tenant.

```bash
quicbaas iam policy list --tenant <tenant-id>
```

### policy validate

Validate a policy file (dry run).

```bash
quicbaas iam policy validate -f <policy-file>
```

### policy test

Test an authorization decision.

```bash
quicbaas iam policy test --tenant <tenant-id> \
  --subject '{"id":"user1","roles":["admin"]}' \
  --action read \
  --resource "keys/foo"
```

## MFA Management

### mfa status

Get MFA status for a user.

```bash
quicbaas iam mfa status --tenant <tenant-id> --user <user-id>
```

### mfa enroll

Begin MFA enrollment for a user.

```bash
quicbaas iam mfa enroll --tenant <tenant-id> --user <user-id>
```

#### Flags
- `--method string`: MFA method (default: totp)

### mfa verify

Complete MFA enrollment by verifying a code.

```bash
quicbaas iam mfa verify <enrollment-id> --code <totp-code>
```

### mfa disable

Disable MFA for a user.

```bash
quicbaas iam mfa disable --tenant <tenant-id> --user <user-id>
```

### mfa backup-codes status

Get backup code status.

```bash
quicbaas iam mfa backup-codes status --tenant <tenant-id> --user <user-id>
```

### mfa backup-codes generate

Generate new backup codes.

```bash
quicbaas iam mfa backup-codes generate --tenant <tenant-id> --user <user-id>
```

## Session Management

### session list

List all active sessions for the current user.

```bash
quicbaas iam session list --tenant <tenant-id>
```

#### Flags
- `--limit int`: Maximum number of sessions to return

### session get

Get details about a specific session.

```bash
quicbaas iam session get <session-id> --tenant <tenant-id>
```

### session revoke

Revoke a specific session.

```bash
quicbaas iam session revoke <session-id> --tenant <tenant-id>
```

### session revoke-all

Revoke all sessions (logout everywhere).

```bash
quicbaas iam session revoke-all --tenant <tenant-id>
```

#### Flags
- `--keep-current`: Keep the current session active

## Notes

- Most operations require the `--tenant` flag to specify tenant context
- Tenant-level operations (create, delete, list tenants) do not require `--tenant`
- Use `--yes` to skip confirmation prompts for destructive operations
- IAM commands require the `iam` build tag to be enabled


---

## Users and authenticators

There is no top-level `quicbaas user`; users are managed with `quicbaas iam user`
above. QuicBaaS uses **passwordless authentication only**: a new user is
`pending` until they enroll a hardware authenticator.

### Built-in Roles

- **admin**: Full administrative access to all resources
- **readwrite**: Read and write access to database operations
- **readonly**: Read-only access to database operations

For custom roles, see `quicbaas iam role` above.

### Authenticator Management

Users enroll and manage hardware authenticators via the Web UI or REST API:

- **WebAuthn (Passkey)**: Platform authenticators (Touch ID, Windows Hello) or roaming keys (YubiKey, Titan)
- **PIV Smartcard**: If PIV support is enabled
- **mTLS Certificate**: For service accounts (see [Passwordless Authentication](architecture.md))

Users can register **multiple authenticators** as backups. See the [WebAuthn API](engineering.md) or GUI for enrolling and managing credentials.

### Security Best Practices

1. **Passkey or Security Key**: Recommend users enroll a platform passkey + a backup hardware key
2. **Least Privilege**: Assign minimal required roles
3. **Regular Audit**: Monitor user creation and deletion events; review inactive users
4. **TLS**: Use TLS for all API calls
5. **Backup Authenticators**: Encourage users to register multiple authenticators to prevent lockout
6. **See Also**: [Passwordless Authentication](architecture.md) for the authentication model

### Notes

- User management requires admin privileges
- Deleting a user revokes all their tokens immediately
- Users can be assigned multiple roles
- Role permissions are cumulative (union of all role permissions)
- Created timestamp is recorded for audit purposes
- User IDs are assigned by the store — the go-qrdb Snowflake, rendered in decimal. A caller cannot supply one.


---

## quicbaas auth

Authentication and N-factor management for QuicBaaS sessions.

### Overview

The `auth` tree manages operator authentication sessions and the
multi-factor enrolment state behind them. Login is the unified
ceremony — `Security().Auth().BeginLogin` / `FinishLogin` — and the
`auth factor` subgroup mirrors qrdb's User N-AuthFactor enrolment
model verb-for-verb (add / list / remove / rotate / relabel).

All verbs route through the SDK `Security().Auth()` accessor (see
[`docs/ssh/engineering.md`](../ssh/engineering.md#auth--authservice)).
Transport selection is automatic per the global `--transport` flag.

Source:
[`cmd/quicbaas/canonical.go:128`](../../cmd/quicbaas/canonical.go)
(wiring) and the per-verb files
`cmd/quicbaas/auth_login.go` +
`cmd/quicbaas/auth_factor.go`.

### Verb summary

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

### Authentication methods

QuicBaaS authentication is **passwordless** — there is no
username/password verb on the canonical surface. The supported
factor types are:

- **FIDO2 / WebAuthn (passkey)**: local CTAP2 authenticator over
  USB-HID (YubiKey, SoloKey, Nitrokey, ...), driven with the
  server-issued WebAuthn options. Admin passkey enrolment runs the
  server's add-credential ceremony, so the server verifies the
  attestation and stores the credential before the factor is reported.
- **PIV**: client-local go-xkms PIV backend (`file` / `pkcs11` /
  `tpm2`); physical smart cards are reachable through the `pkcs11`
  backend. There is no PC/SC reader model.
- **mTLS**: TLS client certificate (service accounts).

The `factor` subgroup manages enrolment for all three factor types.

### login

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
| `--show-token` | `false` | Print the issued session token to stdout |
| `--save-token` | `false` | **Deprecated and ignored** — the token is always stored |

`login` **stores the session token and prints nothing secret by default**
(owner ruling on #58). The credential is written to `--token-path`
(default `~/.quicbaas/token`, which is what `cli.GetDefaultTokenPath` returns)
on every successful login. A login whose credential cannot be stored fails
rather than reporting success — with the token no longer on stdout, an unstored
token is unreachable. Scripts that need the raw bearer pass `--show-token`.

Internally `login` drives the unified ceremony:
`Security().Auth().BeginLogin(...)` obtains the factor-typed
challenge, the client produces the proof locally (mTLS handshake /
CTAP2 assertion / local PIV signature over the nonce), and
`Security().Auth().FinishLogin(...)` verifies it server-side and
returns the bearer + refresh token.

### logout

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

### status

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

### factor list

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

### factor add

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

### factor remove

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

### factor rotate

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

### factor relabel

Update the human-readable label of an enrolled factor.

```bash
quicbaas auth factor relabel fctr_01HXY... --label "New label"
```

| Flag | Default | Description |
|------|---------|-------------|
| `--label <string>` | (required) | New label |

### Token storage

Tokens are stored at `~/.quicbaas/token` by default, with mode
`0o600`. The file is a JSON object with `access_token`,
`refresh_token`, `token_type`, and `expires_at` fields. Subsequent
CLI commands that require authentication read the file
automatically; explicit `--token-file` overrides the default
location.

### Errors

| Type | When |
|------|------|
| `NoFactorsAvailableError` | `login` auto-discovery finds no client cert, no FIDO2 device, and no local PIV certificate |
| `InvalidFactorTypeError` | `factor add` invoked without `--type` or with an unknown type |
| `MissingLabelError` | `factor add` / `rotate` invoked without `--label` |
| `MissingFactorIDError` | `factor remove` / `rotate` invoked without a factor ID |
| `InvalidPIVBackendError` | `--piv-backend` is not `file` / `pkcs11` / `tpm2` |
| `PIVPINRequiredError` | `--method piv` on the `pkcs11` backend without `--pin` / `QUICBAAS_USER_PIN` |

### See also

- [`docs/sdk/security.md`](../ssh/engineering.md) — full `Security().Auth()`
  surface including FIDO2 / PIV / Devices / Factors sub-services.
- `docs/security/authentication.md` —
  server-side authentication architecture.
- `docs/cli/iam.md` — user and role management.
- [`docs/auth/passwordless.md`](architecture.md) — passwordless
  authentication background.
