# quicbaas user

User management commands for QuicBaaS authentication system (passwordless).

## Overview

These commands allow you to create, delete, and list users in the QuicBaaS authentication system. User management requires admin privileges. QuicBaaS uses **passwordless authentication only**—users enroll hardware authenticators (passkeys, security keys, smartcards) instead of passwords.

## Commands

### create

Create a new user in the QuicBaaS authentication system with `pending` status. The user must then activate by enrolling a hardware authenticator (WebAuthn passkey/security key or PIV smartcard).

```bash
quicbaas user create <username>
```

#### Flags
- `--roles stringSlice`: Roles to assign to user (comma-separated) (default: ["readonly"])
- `--email string`: User's email address (recommended for invite flow)

#### Examples

```bash
# Create user who will enroll a passkey
quicbaas user create alice@example.com --email alice@example.com --roles readwrite

# Create admin user
quicbaas user create admin-user --roles admin

# Create user with multiple roles
quicbaas user create jane@example.com --email jane@example.com --roles readonly,readwrite

# Create user on specific server with TLS
quicbaas user create alice@example.com --roles readwrite --address quicbaas.example.com:50051 --tls
```

### delete

Delete a user from the QuicBaaS authentication system.

```bash
quicbaas user delete <username>
```

#### Flags
- `-y, --yes`: Skip confirmation prompt

#### Examples

```bash
# Delete user with confirmation
quicbaas user delete john

# Delete user without confirmation
quicbaas user delete john --yes

# Delete user on specific server
quicbaas user delete john --yes --address quicbaas.example.com:50051
```

### list

List all users in the QuicBaaS authentication system.

```bash
quicbaas user list
```

#### Flags
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# List all users (table format)
quicbaas user list

# List all users as JSON
quicbaas user list --output json

# List users on specific server
quicbaas user list --address quicbaas.example.com:50051
```

### show

Display detailed information about a specific user.

```bash
quicbaas user show <username>
```

#### Flags
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# Show user details
quicbaas user show john

# Show user details as JSON
quicbaas user show john --output json
```

### update

Update user information (roles only). Authenticators are managed via the WebAuthn or PIV APIs.

```bash
quicbaas user update <username>
```

#### Flags
- `--roles stringSlice`: New roles (replaces existing roles)
- `--add-roles stringSlice`: Add roles to existing roles
- `--remove-roles stringSlice`: Remove roles from existing roles

#### Examples

```bash
# Update roles
quicbaas user update john --roles admin,readwrite

# Add roles
quicbaas user update john --add-roles admin

# Remove roles
quicbaas user update john --remove-roles readonly
```

## Built-in Roles

- **admin**: Full administrative access to all resources
- **readwrite**: Read and write access to database operations
- **readonly**: Read-only access to database operations

For custom roles, see the `quicbaas role` command.

## Authenticator Management

Users enroll and manage hardware authenticators via the Web UI or REST API:

- **WebAuthn (Passkey)**: Platform authenticators (Touch ID, Windows Hello) or roaming keys (YubiKey, Titan)
- **PIV Smartcard**: If PIV support is enabled
- **mTLS Certificate**: For service accounts (see [Passwordless Authentication](../auth/passwordless.md))

Users can register **multiple authenticators** as backups. See the [WebAuthn API](../iam/webauthn.md) or GUI for enrolling and managing credentials.

## Security Best Practices

1. **Passkey or Security Key**: Recommend users enroll a platform passkey + a backup hardware key
2. **Least Privilege**: Assign minimal required roles
3. **Regular Audit**: Monitor user creation and deletion events; review inactive users
4. **TLS**: Use TLS for all API calls
5. **Backup Authenticators**: Encourage users to register multiple authenticators to prevent lockout
6. **See Also**: [Passwordless Authentication](../auth/passwordless.md) for the authentication model

## Notes

- User management requires admin privileges
- Deleting a user revokes all their tokens immediately
- Users can be assigned multiple roles
- Role permissions are cumulative (union of all role permissions)
- Created timestamp is recorded for audit purposes
- User IDs are system-generated UUIDs
