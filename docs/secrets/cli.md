# Secrets — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag, with runnable examples. Feature folder: `docs/secrets/`.

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
quicbaas secrets put <path> (--data KEY=VALUE... | --from-stdin | --from-file PATH) [flags]
```

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `path` | Yes | Secret path (e.g., `database/prod`, `app/config`) |

### Flags

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--data` | string | One source | - | Secret data as KEY=VALUE (repeatable); stored as a JSON object |
| `--from-stdin` | bool | One source | false | Store the raw bytes of stdin verbatim; from an interactive terminal, one line read with echo disabled |
| `--from-file` | string | One source | - | Store the raw bytes of a file verbatim |
| `--ttl` | duration | No | 0 | Time-to-live (e.g., `1h`, `30m`, `7d`) |
| `--tenant-id` | string | No | - | Tenant ID for tenant-specific encryption |
| `--key-id` | string | No | - | Custom key identifier (DEKID or KeyID) |
| `--algorithm` | string | No | `AES-256-GCM` | Encryption algorithm |

### Supported Algorithms

- `AES-256-GCM` (default)
- `AES-128-GCM`
- `ChaCha20-Poly1305`

### Examples

Exactly one of `--data`, `--from-stdin`, `--from-file` is required. Use a raw
source for a secret that is ONE opaque value another resource names by
reference — a pooled agent account's `credential_secret_ref`, an SDLC binding's
forge token or webhook key — because those are read verbatim. The value is never
taken on the command line and never printed back.

`--from-stdin` prompts only when stdin is a real terminal (asked of the terminal
driver). Anything else — a pipe, a file, `/dev/null` — is read as a stream, so
an empty stream is refused as an empty value instead of waiting on a prompt.

```bash
# Store a provider token a pooled agent account references (hidden prompt, or pipe it in)
quicbaas secrets put agents/claude/claude1/oauth_token --from-stdin

# Store a credential file verbatim
quicbaas secrets put agents/codex/reviewer/auth_json --from-file ~/.codex/auth.json

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

- [API Reference](api.md) - REST, gRPC, and MCP API documentation
- Configuration - Server configuration options
- [Architecture](architecture.md) - Encryption and storage design


---

## quicbaas secrets

Secrets management commands for storing and managing sensitive data.

### Overview

These commands allow you to store, retrieve, and manage versioned secrets with automatic encryption and optional TTL.

### Commands

#### put

Store a secret at the specified path with key-value data.

```bash
quicbaas secrets put <path>
```

##### Flags
- `--data stringArray`: Secret data as KEY=VALUE pairs (can be specified multiple times) (required)
- `--ttl duration`: Time-to-live for the secret

##### Examples

```bash
# Store a single key-value pair
quicbaas secrets put database/prod --data password=secret123

# Store multiple key-value pairs
quicbaas secrets put app/config --data username=admin --data password=secret --data host=localhost

# Store with TTL
quicbaas secrets put temp/token --data token=abc123 --ttl 1h
```

#### get

Retrieve a secret from the specified path.

```bash
quicbaas secrets get <path>
```

##### Flags
- `--version uint64`: Secret version (0 for latest)
- `--output string`: Output format (json, yaml, table) (default: "table")

##### Examples

```bash
# Get latest version
quicbaas secrets get database/prod

# Get specific version
quicbaas secrets get database/prod --version 2

# Get as JSON
quicbaas secrets get database/prod --output json
```

#### list

List all secrets with optional prefix filtering.

```bash
quicbaas secrets list
```

##### Flags
- `--prefix string`: Filter by path prefix
- `--limit int`: Maximum number of secrets to return (default: 100)
- `--output string`: Output format (json, yaml, table) (default: "table")

##### Examples

```bash
# List all secrets
quicbaas secrets list

# List with prefix filter
quicbaas secrets list --prefix database/

# List with limit
quicbaas secrets list --limit 50
```

#### delete

Soft-delete a secret (can be recovered).

```bash
quicbaas secrets delete <path>
```

##### Flags
- `--permanent`: Permanently delete (cannot be recovered)
- `--output string`: Output format (json, yaml, table) (default: "table")

##### Examples

```bash
# Soft-delete a secret
quicbaas secrets delete database/prod

# Permanently delete
quicbaas secrets delete database/prod --permanent
```

#### destroy

Permanently destroy a secret and all its versions.

```bash
quicbaas secrets destroy <path>
```

##### Examples

```bash
# Permanently destroy a secret
quicbaas secrets destroy database/old
```

#### versions

List all versions of a secret.

```bash
quicbaas secrets versions <path>
```

##### Flags
- `--output string`: Output format (json, yaml, table) (default: "table")

##### Examples

```bash
# List all versions
quicbaas secrets versions database/prod

# List as JSON
quicbaas secrets versions database/prod --output json
```

#### rollback

Rollback a secret to a previous version.

```bash
quicbaas secrets rollback <path>
```

##### Flags
- `--version uint64`: Target version (required)

##### Examples

```bash
# Rollback to version 2
quicbaas secrets rollback database/prod --version 2
```

### Secret Versioning

- Every update creates a new version
- Old versions are retained for audit and rollback
- Versions are numbered sequentially (1, 2, 3, ...)
- Deleted versions can be recovered (soft delete)
- Destroyed secrets cannot be recovered

### Secret Lifecycle

1. **Create**: Store initial secret data (version 1)
2. **Update**: Modify secret data (creates new version)
3. **Read**: Retrieve secret (latest or specific version)
4. **Delete**: Soft delete secret (can be recovered)
5. **Destroy**: Permanently delete secret (cannot be recovered)
6. **Rollback**: Restore previous version (creates new version)

### Security Features

- Automatic encryption at rest
- Versioning for audit trail
- Optional TTL for automatic expiration
- Soft delete with recovery capability
- Access control via RBAC

### Notes

- Secrets are automatically encrypted before storage
- Each secret can have multiple key-value pairs
- TTL causes automatic deletion after expiration
- Soft-deleted secrets can be recovered by fetching previous version
- Permanent deletion (destroy) is irreversible
- Rollback creates a new version with the old data


---

## quicbaas dynamic-secrets

Dynamic secrets management against the active QuicBaaS daemon.
Alias: `quicbaas dynsec`.

### Overview

A **role** is a template that a credential generator uses to mint short-lived
credentials. Every generated credential carries a **lease** that can be renewed
or revoked.

The verb tree wires directly to `Security().DynamicSecrets()`
(`sdk/go/client/dynamicsecrets`). The verb set mirrors that SDK client
interface one-for-one — four role verbs, one generation verb, three lease
verbs.

Source: [`cmd/quicbaas/dynamic_secrets.go`](../../cmd/quicbaas/dynamic_secrets.go).

### Verb summary

| Verb | Purpose |
|------|---------|
| `role create <name>` | Create a credential-generation role |
| `role get <name>` | Show a role |
| `role list` | List roles |
| `role delete <name>` | Remove a role |
| `generate <role>` | Generate a credential from a role |
| `lease renew <lease-id>` | Extend a credential lease |
| `lease revoke <lease-id>` | Revoke a credential lease immediately |
| `lease list` | List active leases |

### Flags

`dynamic-secrets` accepts `--output <json|yaml|table>` (default `table`) as a
persistent flag. Global flags (`--tenant`, `--transport`, `--address`,
`--timeout`, TLS flags) are inherited from the root command.

### Transport support

All eight verbs work on every transport — REST, gRPC, QUIC, MCP, Unix and
embedded. Each SDK transport carries a dedicated `quicbaas.DynamicSecrets`
handler because the leaf SDK types are plain Go structs that the generic
dispatch paths cannot marshal:

| Transport | Handler | Wire |
|-----------|---------|------|
| REST | [`sdk/go/transport/rest/dynamicsecrets_handler.go`](../../sdk/go/transport/rest/dynamicsecrets_handler.go) | `/api/v1/secrets/dynamic/*`; substitutes the role-name / lease-ID path parameters |
| gRPC | [`sdk/go/transport/grpc/dynamicsecrets_handler.go`](../../sdk/go/transport/grpc/dynamicsecrets_handler.go) | `quicbaas.dynamicsecrets.v1.DynamicSecretsService` |
| QUIC | `sdk/go/transport/quic/dynamicsecrets_handler.go` | top-level opcodes `0x64`-`0x6B` |
| MCP | [`sdk/go/transport/mcp/dynamicsecrets_handler.go`](../../sdk/go/transport/mcp/dynamicsecrets_handler.go) | the eight `quicbaas_dynamic_secrets_*` tools |
| Unix | delegates to the gRPC handler verbatim | gRPC over a domain socket |
| embedded | [`sdk/go/transport/embedded/handlers_dynamicsecrets.go`](../../sdk/go/transport/embedded/handlers_dynamicsecrets.go) | in-process service call |

The QUIC opcodes are top-level rather than `OpOrchestrator` sub-ops, so the
surface consumes none of the shared sub-op space. Client and server values are
pinned against each other by `TestDynamicSecretsOpcodeParity` in both
`pkg/api/quic` and `sdk/go/transport/quic`.

#### Authorization

Every transport maps onto the same qrdb permission vocabulary, so a grant is
transport-independent:

| Verb | Permission |
|------|------------|
| `role create` | `dynamic_secrets:role_create` |
| `role get` | `dynamic_secrets:role_read` |
| `role list` | `dynamic_secrets:role_list` |
| `role delete` | `dynamic_secrets:role_delete` |
| `generate` | `dynamic_secrets:generate` |
| `lease renew` | `dynamic_secrets:lease_renew` |
| `lease revoke` | `dynamic_secrets:lease_revoke` |
| `lease list` | `dynamic_secrets:lease_list` |

Credential minting (`generate`) and the two lease-mutating verbs are
write-strength actions distinct from the read/list actions; grant them
deliberately.

### role create

```bash
quicbaas dynamic-secrets role create readonly-db \
  --generator-type postgres \
  --default-ttl 1h --max-ttl 24h \
  --config host=db.internal --config role=readonly
```

| Flag | Meaning |
|------|---------|
| `--generator-type` | Credential generator to use (required) |
| `--config` | Generator config as `key=value` (repeatable) |
| `--default-ttl` | Default credential lifetime (required, positive) |
| `--max-ttl` | Maximum credential lifetime (required, positive, ≥ `--default-ttl`) |

### role get / list / delete

```bash
quicbaas dynamic-secrets role get readonly-db --output json
quicbaas dynamic-secrets role list --prefix readonly-
quicbaas dynamic-secrets role delete readonly-db
```

Deleting a role does not revoke leases already minted from it; revoke those
separately with `lease revoke`.

### generate

```bash
quicbaas dynamic-secrets generate readonly-db
quicbaas dynamic-secrets generate readonly-db --ttl 30m --output json
quicbaas dynamic-secrets generate readonly-db --reveal
```

**Security.** `generate` returns live credential material. Table output
redacts every credential value unless `--reveal` is passed. `--output json`
and `--output yaml` always carry the full payload, because those formats exist
to be piped into a consumer — prefer them over leaving secrets in a terminal
scrollback.

### lease renew / revoke / list

```bash
quicbaas dynamic-secrets lease renew lease_abc123 --increment 1h
quicbaas dynamic-secrets lease revoke lease_abc123
quicbaas dynamic-secrets lease list --prefix database/
```

The server clamps a renewal to the role's max TTL.

### Errors

| Type | Condition |
|------|-----------|
| `DynamicSecretsArgRequiredError` | The positional name / role / lease-id argument is missing or blank |
| `DynamicSecretsFlagInvalidError` | A flag carries a missing, negative, or contradictory value |
| `DynamicSecretsConfigFormatError` | `--config` is not in `key=value` form |
