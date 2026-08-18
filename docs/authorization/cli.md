# Authorization — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag, with runnable examples. Feature folder: `docs/authorization/`.

Role management commands for QuicBaaS RBAC.

## Overview

These commands allow you to create custom roles, assign roles to users, and manage role permissions. Role management requires admin privileges.

### Built-in Roles
- **admin**: Full administrative access to all resources
- **readwrite**: Read and write access to database operations
- **readonly**: Read-only access to database operations

## Commands

### create

Create a new custom role with specified permissions.

```bash
quicbaas role create <name>
```

#### Flags
- `--description string`: Role description
- `--permissions stringSlice`: Permissions in format resource:action (comma-separated) (required)

#### Examples

```bash
# Create a backup operator role
quicbaas role create backup-operator --description "Backup operations" \
  --permissions backups:read,backups:write,backups:delete

# Create a metrics reader role
quicbaas role create metrics-reader --description "Metrics read access" \
  --permissions metrics:read,health:read

# Create a cluster admin role
quicbaas role create cluster-admin --description "Cluster administration" \
  --permissions cluster:*

# Create a data analyst role (read-only access to data and indexes)
quicbaas role create data-analyst --description "Data analysis" \
  --permissions keys:read,indexes:read,metrics:read
```

### delete

Delete a custom role from the QuicBaaS authentication system.

```bash
quicbaas role delete <name>
```

#### Flags
- `-y, --yes`: Skip confirmation prompt

#### Examples

```bash
# Delete role with confirmation
quicbaas role delete backup-operator

# Delete role without confirmation
quicbaas role delete backup-operator --yes
```

### list

List all roles in the QuicBaaS authentication system.

```bash
quicbaas role list
```

#### Examples

```bash
# List all roles (table format)
quicbaas role list

# List all roles as JSON
quicbaas role list --output json
```

### show

Display detailed information about a specific role including all permissions.

```bash
quicbaas role show <name>
```

#### Examples

```bash
# Show role details
quicbaas role show admin

# Show role details as JSON
quicbaas role show admin --output json
```

### assign

Assign a role to a user, granting the associated permissions.

```bash
quicbaas role assign <role> <username>
```

#### Examples

```bash
# Assign role to user
quicbaas role assign readwrite john

# Assign admin role to user
quicbaas role assign admin jane

# Assign custom role to user
quicbaas role assign backup-operator backup-user
```

### unassign

Remove a role from a user, revoking the associated permissions.

```bash
quicbaas role unassign <role> <username>
```

#### Flags
- `-y, --yes`: Skip confirmation prompt

#### Examples

```bash
# Remove role from user
quicbaas role unassign readwrite john

# Remove role from user without confirmation
quicbaas role unassign admin jane --yes
```

### permissions grant

Grant a permission to a role.

```bash
quicbaas role permissions grant <role> <permission>
```

#### Examples

```bash
# Grant read access to keys
quicbaas role permissions grant backup-operator keys:read

# Grant all access to backups
quicbaas role permissions grant backup-operator backups:*

# Grant admin access to cluster
quicbaas role permissions grant cluster-admin cluster:admin
```

### permissions revoke

Revoke a permission from a role.

```bash
quicbaas role permissions revoke <role> <permission>
```

#### Examples

```bash
# Revoke write access to keys
quicbaas role permissions revoke backup-operator keys:write

# Revoke all access to backups
quicbaas role permissions revoke backup-operator backups:*
```

### permissions list

List all permissions for a specific role.

```bash
quicbaas role permissions list <role>
```

#### Examples

```bash
# List permissions for a role
quicbaas role permissions list admin

# List permissions as JSON
quicbaas role permissions list admin --output json
```

## Permission Format

Permissions are specified in the format: `resource:action`

### Resources
- `keys` - Database keys/entities
- `indexes` - Secondary indexes
- `cluster` - Cluster management
- `users` - User management
- `backups` - Backup operations
- `metrics` - Metrics and monitoring
- `health` - Health checks
- `transactions` - Transaction operations
- `watch` - Watch/subscribe operations
- `*` - All resources

### Actions
- `read` - Read access
- `write` - Write/create access
- `delete` - Delete access
- `admin` - Administrative access
- `*` - All actions

## Notes

- Built-in roles cannot be modified or deleted
- Users can have multiple roles with combined permissions
- Custom roles allow fine-grained access control
- Permission changes take effect immediately
- Deleting a role revokes permissions from all users with that role
