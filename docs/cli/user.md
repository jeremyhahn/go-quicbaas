# quicbaas user

User management commands for QuicBaaS authentication system.

## Overview

These commands allow you to create, delete, and list users in the QuicBaaS authentication system. User management requires admin privileges.

## Commands

### create

Create a new user in the QuicBaaS authentication system.

```bash
quicbaas user create <username>
```

#### Flags
- `--roles stringSlice`: Roles to assign to user (comma-separated) (default: ["readonly"])
- `--password string`: Password (not recommended, use interactive prompt instead)

#### Examples

```bash
# Create user with interactive password prompt
quicbaas user create john --roles readwrite

# Create admin user
quicbaas user create admin --roles admin

# Create user with multiple roles
quicbaas user create jane --roles readonly,readwrite

# Create user with password from environment
QUICBAAS_PASSWORD=secret quicbaas user create john --roles readwrite

# Create user on specific server with TLS
quicbaas user create john --roles readwrite --address quicbaas.example.com:50051 --tls
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

Update user information (password or roles).

```bash
quicbaas user update <username>
```

#### Flags
- `--password string`: New password
- `--roles stringSlice`: New roles (replaces existing roles)
- `--add-roles stringSlice`: Add roles to existing roles
- `--remove-roles stringSlice`: Remove roles from existing roles

#### Examples

```bash
# Update password interactively
quicbaas user update john

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

## Password Requirements

- Minimum length: 8 characters
- Interactive prompt recommended for security
- Avoid using `--password` flag in production
- Use environment variable `QUICBAAS_PASSWORD` for automation

## Security Best Practices

1. **Interactive Passwords**: Use interactive prompts instead of command-line flags
2. **Strong Passwords**: Use complex passwords with minimum 8 characters
3. **Least Privilege**: Assign minimal required roles
4. **Regular Rotation**: Rotate passwords periodically
5. **Audit Logging**: Monitor user creation and deletion events
6. **TLS**: Use TLS for all authentication operations

## Notes

- User management requires admin privileges
- Deleting a user revokes all their tokens immediately
- Users can be assigned multiple roles
- Role permissions are cumulative (union of all role permissions)
- Created timestamp is recorded for audit purposes
- User IDs are system-generated UUIDs
