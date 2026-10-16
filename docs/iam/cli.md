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

These commands manage the tenant's policy documents. Decisions are made by
go-qrdb's ABAC authorizer
([ABAC Authorization Decorator](../../../go-qrdb/docs/auth/abac.md)); see
[Authorization (ABAC) and the PEP](architecture.md#authorization-abac-and-the-pep).

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

### pep check and pep check-batch

Ask the PEP for a decision (`POST /api/v1/iam/check` and `/check/batch`).

```bash
quicbaas iam pep check --tenant <tenant-id> \
  --subject '{"id":"user1","roles":["admin"]}' \
  --action read --resource "keys/foo" [--context '<json>']

quicbaas iam pep check-batch --tenant <tenant-id> -f requests.json
```

`requests.json` holds `{"requests": [{"subject": {...}, "action": "...", "resource": "..."}]}`.

## MFA Management

TOTP MFA is QuicBaaS's own; the routes are in [API: MFA API](api.md#mfa-api).
`--user` is required on every `mfa` verb, and `mfa verify` requires `--code`.

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

Users, built-in roles and role bindings are go-qrdb's; `quicbaas iam user`
and `quicbaas iam role` above drive them through the SDK. See
[go-qrdb identity and RBAC CLI](../../../go-qrdb/docs/identity-rbac/cli.md)
and [go-qrdb roles](../../../go-qrdb/docs/auth/roles.md). A new user is
`pending` until they enroll a hardware authenticator.

### iam webauthn

Lists or deletes a user's WebAuthn credentials through the per-user routes in
[API: Credential Management](api.md#credential-management)
(`cmd/quicbaas/iam_webauthn.go`).

```bash
quicbaas iam webauthn list --tenant <tenant-id> --user <user-id>
quicbaas iam webauthn delete --tenant <tenant-id> --user <user-id> --credential <id>
quicbaas iam webauthn delete --tenant <tenant-id> --user <user-id> --all
```

| Flag | Verb | Description |
|------|------|-------------|
| `--user <id>` | both | User ID (required) |
| `--credential <id>` | `delete` | Credential to delete |
| `--all` | `delete` | Delete every credential registered to the user |

---

## quicbaas auth

`quicbaas auth` mirrors go-qrdb's `qrdb auth` verb for verb; the factor model,
the verbs and the last-factor rule are documented once in
[go-qrdb auth-factors CLI](../../../go-qrdb/docs/auth-factors/cli.md). The
client-side FIDO2 and PIV proofs come from go-xkms
([WebAuthn CLI](../../../go-xkms/docs/webauthn/cli.md),
[PIV smart card CLI](../../../go-xkms/docs/piv-smartcard/cli.md)). This
section lists QuicBaaS's flags.

All verbs go through `Security().Auth()`; the global `--transport` flag picks
the transport. Source: `cmd/quicbaas/canonical.go` (wiring),
`cmd/quicbaas/canonical_auth_login.go` and `cmd/quicbaas/canonical_auth.go`.

### Verb summary

| Verb | Purpose |
|------|---------|
| `login` | Unified login (`BeginLogin` / `FinishLogin`) |
| `logout` | Revoke the current token server-side and remove the local file |
| `status` | Show the current session |
| `factor list [--user <id>]` | List enrolled factors |
| `factor add` | Enroll a factor |
| `factor remove <id> [--force]` | Remove a factor; `--force` allows the last one |
| `factor rotate <id>` | Replace a factor; takes the `factor add` flags |
| `factor relabel <id> --label <s>` | Change a factor's label |
| `login-info` | Legacy informational login (`cmd/quicbaas/auth.go`; `--token-file`, `--no-store`) |

Factor types are `fido2`, `piv` (go-xkms local backend: `file`, `pkcs11` or
`tpm2`; smart cards through `pkcs11`, no PC/SC) and `mtls`.

### login

```bash
# Auto-discovery: client cert, then FIDO2 device, then local PIV slot
quicbaas auth login --username alice@example.com

quicbaas auth login --username alice@example.com --method fido2

quicbaas auth login --username alice@example.com --method piv \
  --piv-backend pkcs11 --pkcs11-module /usr/lib/libykcs11.so --pin 123456

quicbaas auth login --method mtls --client-cert client.pem
```

| Flag | Default | Description |
|------|---------|-------------|
| `--method <name>` | auto-discover | `mtls` / `fido2` / `piv` |
| `--username <s>` | (none) | Username (subject) |
| `--factor <id>` | (none) | Enrolled factor id |
| `--client-cert <path>` | first of `~/.quicbaas/admin.pem`, `~/.platformctl/admin.pem`, `~/.config/platformctl/admin.pem` | Client certificate PEM (`mtls`) |
| `--device <path>` | first connected | FIDO2 HID device (`fido2`) |
| `--origin <url>` | `https://<server-issued rpId>` | WebAuthn origin (`fido2`) |
| `--piv-backend <name>` | `file` | `file` / `pkcs11` / `tpm2` (`piv`) |
| `--piv-dir <path>` | `~/.quicbaas/piv` | Key/cert directory (`file` backend) |
| `--pkcs11-module <path>` | (none) | PKCS#11 library (`pkcs11` backend) |
| `--pkcs11-token <label>` | (none) | PKCS#11 token label (`pkcs11` backend) |
| `--slot <id>` | `9a` | PIV slot (`piv`) |
| `--pin <s>` | `$QUICBAAS_USER_PIN` | PKCS#11 User PIN (`pkcs11` backend) |
| `--token-path <path>` | `~/.quicbaas/token` | Where the session token is written |
| `--show-token` | `false` | Also print the token to stdout |
| `--save-token` | `false` | Deprecated and ignored; the token is always stored |

`login` always stores the token and prints nothing secret by default (owner
ruling on #58). A login whose token cannot be stored fails.

### logout and status

| Verb | Flag | Default | Description |
|------|------|---------|-------------|
| `logout`, `status` | `--token-file <path>` | `~/.quicbaas/token` | Token file |
| `status` | `--output <format>` | `table` | `json` or `table` |

### factor add and factor rotate

```bash
quicbaas auth factor add --type fido2 --label "Laptop passkey"
quicbaas auth factor add --type piv --label "Corporate ID" --piv-backend file --slot 9a
quicbaas auth factor add --type mtls --label "Service: ingest" --cert svc-ingest.crt
quicbaas auth factor rotate <factor-id> --type fido2 --label "Replacement passkey"
```

| Flag | Default | Description |
|------|---------|-------------|
| `--type <factor>` | (required) | `fido2` / `piv` / `mtls` |
| `--label <string>` | (required) | Factor label |
| `--user <id>` | current user | Subject to enroll for |
| `--tenant <id>` | (none) | Tenant ID |
| `--cert <path>` | (none) | Certificate PEM (`mtls`) |
| `--device <id>` | first connected | FIDO2 device (`fido2`) |
| `--origin <url>` | `https://<rp-id>` | WebAuthn origin (`fido2`) |
| `--piv-backend <name>` | `file` | `file` / `pkcs11` / `tpm2` (`piv`) |
| `--piv-dir <path>` | `~/.quicbaas/piv` | Key/cert directory (`file` backend) |
| `--slot <id>` | `9a` | PIV slot (`piv`) |
| `--pin <s>` | (none) | PKCS#11 User PIN (`pkcs11` backend) |
| `--pkcs11-module <path>` | (none) | PKCS#11 library (`pkcs11` backend) |
| `--pkcs11-token <label>` | (none) | PKCS#11 token label (`pkcs11` backend) |

`factor list` takes `--user`, `factor remove` takes `--force`, and
`factor relabel` takes `--label`.

### Errors

| Type | When |
|------|------|
| `NoFactorsAvailableError` | `login` auto-discovery finds no client cert, FIDO2 device or local PIV certificate |
| `InvalidFactorTypeError` | `factor add` / `rotate` without `--type` or with an unknown type |
| `MissingLabelError` | `factor add` / `rotate` without `--label` |
| `MissingFactorIDError` | `factor remove` / `rotate` without a factor ID |
| `InvalidPIVBackendError` | `--piv-backend` is not `file` / `pkcs11` / `tpm2` |
| `PIVPINRequiredError` | `--method piv` on `pkcs11` without `--pin` or `QUICBAAS_USER_PIN` |

Removing the last factor without `--force` is refused by go-qrdb
(`LastFactorError`).

### See also

- [SDK `Security().Auth()`](../ssh/engineering.md#auth--authservice)
- [IAM architecture](architecture.md)
