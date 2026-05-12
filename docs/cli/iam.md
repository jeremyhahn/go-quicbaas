# quicbaas iam

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
- `--groups stringSlice`: Groups to add user to (comma-separated)

#### Examples

```bash
quicbaas iam user create alice@acme.com --tenant acme-corp --name "Alice Smith"
quicbaas iam user create alice@acme.com --tenant acme-corp --name "Alice" --roles admin,developer
```

**No Password Field:** QuicBaaS is passwordless. Users enroll hardware authenticators instead. See [Passwordless Authentication](../auth/passwordless.md).

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
- `--group string`: Filter by group

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

## Group Management

### group create

Create a new group.

```bash
quicbaas iam group create <id> --tenant <tenant-id>
```

#### Flags
- `--name string`: Group display name (required)
- `--description string`: Group description

### group get

Get group details.

```bash
quicbaas iam group get <id> --tenant <tenant-id>
```

### group update

Update group information.

```bash
quicbaas iam group update <id> --tenant <tenant-id>
```

#### Flags
- `--name string`: New group name
- `--description string`: New group description

### group delete

Delete a group.

```bash
quicbaas iam group delete <id> --tenant <tenant-id>
```

### group list

List groups in a tenant.

```bash
quicbaas iam group list --tenant <tenant-id>
```

### group add-member

Add a user to a group.

```bash
quicbaas iam group add-member <group-id> <user-id> --tenant <tenant-id>
# or using flags
quicbaas iam group add-member --tenant <tenant-id> --group <group-id> --user <user-id>
```

### group remove-member

Remove a user from a group.

```bash
quicbaas iam group remove-member <group-id> <user-id> --tenant <tenant-id>
```

### group members

List group members.

```bash
quicbaas iam group members <group-id> --tenant <tenant-id>
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
