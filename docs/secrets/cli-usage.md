# Secrets CLI Usage

Complete reference for the QuicBaaS secrets management CLI commands.

## Command Overview

```
quicbaas secrets <command> [flags]
```

| Command | Description |
|---------|-------------|
| `put` | Store a new secret |
| `get` | Retrieve a secret |
| `get-prefix` | Bulk retrieve secrets by path prefix |
| `list` | List secrets |
| `delete` | Soft-delete a secret |
| `destroy` | Permanently destroy a secret |
| `versions` | List secret versions |
| `rollback` | Rollback to previous version |
| `rotate` | Rotate secret to new value |
| `enable` | Enable a secret |
| `disable` | Disable a secret |
| `watch` | Watch for real-time secret changes |

## Global Options

All secrets commands support:

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--output` | string | `table` | Output format: `json`, `yaml`, `table` |

---

## secrets put

Store a secret with automatic encryption and versioning.

### Usage

```bash
quicbaas secrets put <path> --data KEY=VALUE [flags]
```

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `path` | Yes | Secret path (e.g., `database/prod`, `app/config`) |

### Flags

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--data` | string | Yes | - | Secret data as KEY=VALUE (repeatable) |
| `--ttl` | duration | No | 0 | Time-to-live (e.g., `1h`, `30m`, `7d`) |
| `--tenant-id` | string | No | - | Tenant ID for tenant-specific encryption |
| `--key-id` | string | No | - | Custom key identifier (DEKID or KeyID) |
| `--algorithm` | string | No | `AES-256-GCM` | Encryption algorithm |

### Supported Algorithms

- `AES-256-GCM` (default)
- `AES-128-GCM`
- `ChaCha20-Poly1305`

### Examples

```bash
# Store single key-value pair
quicbaas secrets put database/prod --data password=secret123

# Store multiple key-value pairs
quicbaas secrets put app/config \
  --data username=admin \
  --data password=secret \
  --data host=localhost

# Store with TTL (expires in 1 hour)
quicbaas secrets put temp/token --data token=abc123 --ttl 1h

# Store with tenant-specific encryption
quicbaas secrets put tenant/secret --data key=value --tenant-id tenant-123

# Store with custom algorithm
quicbaas secrets put app/secret --data key=value --algorithm ChaCha20-Poly1305

# Output as JSON
quicbaas secrets put database/prod --data password=secret --output json
```

### Output

**Table format:**
```
SECRET ID        VERSION  TIMESTAMP
database/prod    1        2024-01-15T10:30:00Z
```

**JSON format:**
```json
{
  "secret_id": "database/prod",
  "version": 1,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

---

## secrets get

Retrieve a secret's decrypted data.

### Usage

```bash
quicbaas secrets get <path> [flags]
```

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `path` | Yes | Secret path |

### Flags

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--version` | uint64 | No | 0 | Specific version (0 = latest) |

### Examples

```bash
# Get latest version
quicbaas secrets get database/prod

# Get specific version
quicbaas secrets get database/prod --version 2

# Output as JSON (includes encryption metadata)
quicbaas secrets get database/prod --output json
```

### Output

**Default format:**
```
password=secret123
username=admin
host=localhost
```

**Table format:**
```
SECRET ID      VERSION  CREATED
database/prod  2        2024-01-15T10:30:00Z

DATA:
  password = secret123
  username = admin
```

**JSON format:**
```json
{
  "secret_id": "database/prod",
  "version": 2,
  "data": {
    "password": "secret123",
    "username": "admin"
  },
  "metadata": {},
  "created_at": "2024-01-15T10:30:00Z",
  "timestamp": "2024-01-15T10:35:00Z",
  "algorithm": "AES-256-GCM",
  "key_identifier": "dek:global"
}
```

---

## secrets get-prefix

Bulk retrieve all secrets matching a path prefix.

### Usage

```bash
quicbaas secrets get-prefix <prefix> [flags]
```

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `prefix` | Yes | Path prefix to match (e.g., `database/`, `app/config/`) |

### Flags

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--include-data` | bool | No | true | Include secret data in response |
| `--limit` | int | No | 100 | Maximum secrets to return (1-1000) |

### Examples

```bash
# Get all secrets under database/
quicbaas secrets get-prefix database/

# Get secrets without data (metadata only)
quicbaas secrets get-prefix database/ --include-data=false

# Limit results
quicbaas secrets get-prefix app/ --limit 50

# Output as JSON
quicbaas secrets get-prefix database/ --output json
```

### Output

**Table format:**
```
PREFIX: database/
TOTAL: 3 secrets

PATH               VERSION  KEYS
database/prod      3        password, username, host
database/staging   1        password, username
database/dev       2        password

Timestamp: 2024-01-15T10:30:00Z
```

**JSON format:**
```json
{
  "secrets": {
    "database/prod": {
      "data": {
        "password": "c2VjcmV0MTIz",
        "username": "YWRtaW4="
      },
      "metadata": {
        "owner": "devops"
      },
      "version": 3
    },
    "database/staging": {
      "data": {
        "password": "c3RhZ2luZzEyMw=="
      },
      "metadata": {},
      "version": 1
    }
  },
  "total_count": 3,
  "has_more": false,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

---

## secrets list

List secrets with optional prefix filtering.

### Usage

```bash
quicbaas secrets list [flags]
```

### Flags

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--prefix` | string | No | - | Filter by path prefix |
| `--limit` | int | No | 100 | Maximum results (1-1000) |

### Examples

```bash
# List all secrets
quicbaas secrets list

# List with prefix filter
quicbaas secrets list --prefix database/

# List with limit
quicbaas secrets list --limit 50

# Output as JSON
quicbaas secrets list --output json
```

### Output

**Table format:**
```
SECRET ID          VERSION  CREATED               UPDATED
database/prod      3        2024-01-10T10:00:00Z  2024-01-15T10:30:00Z
database/staging   1        2024-01-12T14:00:00Z  2024-01-12T14:00:00Z
app/config         2        2024-01-15T10:30:00Z  2024-01-15T10:35:00Z

Total: 3 secrets
```

**JSON format:**
```json
{
  "secrets": [
    {
      "secret_id": "database/prod",
      "name": "database/prod",
      "current_version": 3,
      "created_at": "2024-01-10T10:00:00Z",
      "updated_at": "2024-01-15T10:30:00Z"
    }
  ],
  "total_count": 3
}
```

---

## secrets delete

Soft-delete a secret (recoverable).

### Usage

```bash
quicbaas secrets delete <path> [flags]
```

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `path` | Yes | Secret path |

### Flags

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--permanent` | bool | No | false | Permanently delete (cannot recover) |

### Examples

```bash
# Soft-delete (can be recovered)
quicbaas secrets delete database/prod

# Permanent delete
quicbaas secrets delete database/prod --permanent
```

### Output

```
SECRET ID        DELETED  TIMESTAMP
database/prod    true     2024-01-15T10:45:00Z
```

---

## secrets destroy

Permanently destroy a secret and all versions. **This operation is irreversible.**

### Usage

```bash
quicbaas secrets destroy <path> [flags]
```

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `path` | Yes | Secret path |

### Examples

```bash
# Permanently destroy
quicbaas secrets destroy database/old
```

### Output

```
SECRET ID        DELETED  TIMESTAMP
database/old     true     2024-01-15T10:50:00Z
```

---

## secrets versions

List all versions of a secret.

### Usage

```bash
quicbaas secrets versions <path> [flags]
```

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `path` | Yes | Secret path |

### Examples

```bash
# List versions
quicbaas secrets versions database/prod

# Output as JSON
quicbaas secrets versions database/prod --output json
```

### Output

**Table format:**
```
VERSION  CREATED               CREATED BY       DELETED
1        2024-01-10T10:00:00Z  user@example.com false
2        2024-01-12T14:00:00Z  rotation-svc     false
3        2024-01-15T10:30:00Z  user@example.com false

Total: 3 versions
```

**JSON format:**
```json
{
  "versions": [
    {
      "version": 3,
      "created_at": "2024-01-15T10:30:00Z",
      "created_by": "user@example.com",
      "deleted_at": null
    }
  ]
}
```

---

## secrets rollback

Rollback to a previous version. Creates a new version with the content from the target version.

### Usage

```bash
quicbaas secrets rollback <path> --version <version> [flags]
```

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `path` | Yes | Secret path |

### Flags

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--version` | uint64 | Yes | - | Target version to rollback to |

### Examples

```bash
# Rollback to version 2
quicbaas secrets rollback database/prod --version 2
```

### Output

```
SECRET ID        NEW VERSION  TIMESTAMP
database/prod    4            2024-01-15T11:00:00Z
```

---

## secrets rotate

Rotate a secret to a new value. Creates a new version with updated data.

### Usage

```bash
quicbaas secrets rotate <path> [value] [flags]
```

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `path` | Yes | Secret path |
| `value` | No | Simple string value (creates `{"value": VALUE}`) |

### Flags

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--data` | string | No | - | Complex data as KEY=VALUE (repeatable) |
| `--tenant-id` | string | No | - | Tenant ID (inherits if not set) |
| `--key-id` | string | No | - | Custom key ID (inherits if not set) |
| `--algorithm` | string | No | - | Algorithm (inherits if not set) |

### Examples

```bash
# Rotate with simple value
quicbaas secrets rotate database/prod "new-secret-password"

# Rotate with --data flag
quicbaas secrets rotate database/prod --data password=newsecret123

# Rotate with multiple fields
quicbaas secrets rotate database/prod \
  --data password=newsecret \
  --data username=newadmin

# Rotate with different algorithm
quicbaas secrets rotate database/prod \
  --data password=newsecret \
  --algorithm ChaCha20-Poly1305
```

### Output

```
SECRET ID        OLD VERSION  NEW VERSION  TIMESTAMP
database/prod    2            3            2024-01-15T11:05:00Z
```

---

## secrets enable

Enable a previously disabled secret or version.

### Usage

```bash
quicbaas secrets enable <path> [flags]
```

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `path` | Yes | Secret path |

### Flags

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--version` | uint64 | No | 0 | Specific version (0 = current) |
| `--all` | bool | No | false | Enable all versions |

### Examples

```bash
# Enable current version
quicbaas secrets enable database/prod

# Enable specific version
quicbaas secrets enable database/prod --version 2

# Enable all versions
quicbaas secrets enable database/prod --all
```

### Output

```
SECRET ID        STATUS   VERSIONS UPDATED  TIMESTAMP
database/prod    enabled  1                 2024-01-15T11:10:00Z
```

---

## secrets disable

Disable a secret or version. Disabled secrets cannot be retrieved.

### Usage

```bash
quicbaas secrets disable <path> [flags]
```

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `path` | Yes | Secret path |

### Flags

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--version` | uint64 | No | 0 | Specific version (0 = current) |
| `--all` | bool | No | false | Disable all versions |

### Examples

```bash
# Disable current version
quicbaas secrets disable database/prod

# Disable specific version
quicbaas secrets disable database/prod --version 2

# Disable all versions
quicbaas secrets disable database/prod --all
```

### Output

```
SECRET ID        STATUS    VERSIONS UPDATED  TIMESTAMP
database/prod    disabled  1                 2024-01-15T11:15:00Z
```

---

## secrets watch

Watch for real-time secret changes via streaming. Monitors a path prefix and outputs changes as they occur.

### Usage

```bash
quicbaas secrets watch <prefix> [flags]
```

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `prefix` | Yes | Path prefix to watch (e.g., `database/`, `app/`) |

### Flags

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--timeout` | duration | No | 0 | Watch duration (e.g., `60s`, `5m`). 0 = indefinite |

### Examples

```bash
# Watch indefinitely (Ctrl+C to stop)
quicbaas secrets watch database/

# Watch with timeout
quicbaas secrets watch database/ --timeout 60s

# Watch with JSON output (for scripting)
quicbaas secrets watch app/ --output json

# Watch all secrets
quicbaas secrets watch /
```

### Output

**Table format (streaming):**
```
Watching prefix: database/
Press Ctrl+C to stop...

TIME                  TYPE     PATH               VERSION
2024-01-15T11:20:00Z  CREATED  database/new       1
2024-01-15T11:20:15Z  UPDATED  database/prod      4
2024-01-15T11:20:30Z  DELETED  database/old       3
```

**JSON format (streaming, one object per line):**
```json
{"path":"database/new","change_type":"CREATED","version":1,"timestamp":"2024-01-15T11:20:00Z"}
{"path":"database/prod","change_type":"UPDATED","version":4,"timestamp":"2024-01-15T11:20:15Z"}
{"path":"database/old","change_type":"DELETED","version":3,"timestamp":"2024-01-15T11:20:30Z"}
```

### Change Types

| Type | Description |
|------|-------------|
| `CREATED` | New secret was stored |
| `UPDATED` | Existing secret was modified (new version) |
| `DELETED` | Secret was deleted |

### Use Cases

**Configuration Hot-Reload:**
```bash
# Watch for config changes and reload application
quicbaas secrets watch app/config/ --output json | while read change; do
  echo "Config changed: $change"
  systemctl reload myapp
done
```

**Audit Logging:**
```bash
# Log all secret changes
quicbaas secrets watch / --output json >> /var/log/secret-changes.log
```

**Health Check with Timeout:**
```bash
# Verify watch API is working (timeout after 5s)
quicbaas secrets watch test/ --timeout 5s
```

---

## Practical Examples

### Application Configuration

```bash
# Store database credentials
quicbaas secrets put prod/db/postgres \
  --data host=db.example.com \
  --data user=app \
  --data password=secret123

# Retrieve password for application
export DB_PASSWORD=$(quicbaas secrets get prod/db/postgres --output json | jq -r '.data.password')
```

### CI/CD Integration

```bash
# Store CI secrets
quicbaas secrets put ci/github-actions \
  --data DOCKER_TOKEN=dckr_pat_xxx \
  --data NPM_TOKEN=npm_xxx \
  --algorithm AES-256-GCM

# Export as environment variables
eval $(quicbaas secrets get ci/github-actions --output json | jq -r '.data | to_entries | .[] | "export \(.key)=\(.value)"')
```

### Multi-Tenant Secrets

```bash
# Store tenant-specific secret
quicbaas secrets put app/api-key \
  --data key=sk-tenant-abc123 \
  --tenant-id tenant-abc

# Store for different tenant
quicbaas secrets put app/api-key \
  --data key=sk-tenant-xyz789 \
  --tenant-id tenant-xyz
```

### Key Rotation Script

```bash
#!/bin/bash
# Monthly key rotation
NEW_PASSWORD=$(openssl rand -base64 32)

quicbaas secrets rotate database/prod \
  --data password="$NEW_PASSWORD"

echo "Rotated database/prod to new version"
```

### Disaster Recovery

```bash
# List all versions
quicbaas secrets versions database/prod

# Rollback to known good version
quicbaas secrets rollback database/prod --version 5

# Verify rollback
quicbaas secrets get database/prod
```

---

## Error Handling

| Error | Cause | Resolution |
|-------|-------|------------|
| `secret not found` | Path does not exist | Verify path spelling |
| `version not found` | Version doesn't exist | Use `versions` to list valid versions |
| `invalid algorithm` | Unsupported algorithm | Use AES-256-GCM, AES-128-GCM, or ChaCha20-Poly1305 |
| `permission denied` | Insufficient access | Check RBAC policies |
| `connection refused` | Server unavailable | Verify server address |

---

## See Also

- [API Reference](api-reference.md) - REST, gRPC, and MCP API documentation
- [Configuration](configuration.md) - Server configuration options
- [Architecture](architecture.md) - Encryption and storage design
