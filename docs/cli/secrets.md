# quicbaas secrets

Secrets management commands for storing and managing sensitive data.

## Overview

These commands allow you to store, retrieve, and manage versioned secrets with automatic encryption and optional TTL.

## Commands

### put

Store a secret at the specified path with key-value data.

```bash
quicbaas secrets put <path>
```

#### Flags
- `--data stringArray`: Secret data as KEY=VALUE pairs (can be specified multiple times) (required)
- `--ttl duration`: Time-to-live for the secret

#### Examples

```bash
# Store a single key-value pair
quicbaas secrets put database/prod --data password=secret123

# Store multiple key-value pairs
quicbaas secrets put app/config --data username=admin --data password=secret --data host=localhost

# Store with TTL
quicbaas secrets put temp/token --data token=abc123 --ttl 1h
```

### get

Retrieve a secret from the specified path.

```bash
quicbaas secrets get <path>
```

#### Flags
- `--version uint64`: Secret version (0 for latest)
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# Get latest version
quicbaas secrets get database/prod

# Get specific version
quicbaas secrets get database/prod --version 2

# Get as JSON
quicbaas secrets get database/prod --output json
```

### list

List all secrets with optional prefix filtering.

```bash
quicbaas secrets list
```

#### Flags
- `--prefix string`: Filter by path prefix
- `--limit int`: Maximum number of secrets to return (default: 100)
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# List all secrets
quicbaas secrets list

# List with prefix filter
quicbaas secrets list --prefix database/

# List with limit
quicbaas secrets list --limit 50
```

### delete

Soft-delete a secret (can be recovered).

```bash
quicbaas secrets delete <path>
```

#### Flags
- `--permanent`: Permanently delete (cannot be recovered)
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# Soft-delete a secret
quicbaas secrets delete database/prod

# Permanently delete
quicbaas secrets delete database/prod --permanent
```

### destroy

Permanently destroy a secret and all its versions.

```bash
quicbaas secrets destroy <path>
```

#### Examples

```bash
# Permanently destroy a secret
quicbaas secrets destroy database/old
```

### versions

List all versions of a secret.

```bash
quicbaas secrets versions <path>
```

#### Flags
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# List all versions
quicbaas secrets versions database/prod

# List as JSON
quicbaas secrets versions database/prod --output json
```

### rollback

Rollback a secret to a previous version.

```bash
quicbaas secrets rollback <path>
```

#### Flags
- `--version uint64`: Target version (required)

#### Examples

```bash
# Rollback to version 2
quicbaas secrets rollback database/prod --version 2
```

## Secret Versioning

- Every update creates a new version
- Old versions are retained for audit and rollback
- Versions are numbered sequentially (1, 2, 3, ...)
- Deleted versions can be recovered (soft delete)
- Destroyed secrets cannot be recovered

## Secret Lifecycle

1. **Create**: Store initial secret data (version 1)
2. **Update**: Modify secret data (creates new version)
3. **Read**: Retrieve secret (latest or specific version)
4. **Delete**: Soft delete secret (can be recovered)
5. **Destroy**: Permanently delete secret (cannot be recovered)
6. **Rollback**: Restore previous version (creates new version)

## Security Features

- Automatic encryption at rest
- Versioning for audit trail
- Optional TTL for automatic expiration
- Soft delete with recovery capability
- Access control via RBAC

## Notes

- Secrets are automatically encrypted before storage
- Each secret can have multiple key-value pairs
- TTL causes automatic deletion after expiration
- Soft-deleted secrets can be recovered by fetching previous version
- Permanent deletion (destroy) is irreversible
- Rollback creates a new version with the old data
