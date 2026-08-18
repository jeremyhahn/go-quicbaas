# Object Storage — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag, with runnable examples. Feature folder: `docs/object-storage/`.

## Command Overview

| Command | Description |
|---------|-------------|
| `quicbaas object put` | Store an object |
| `quicbaas object get` | Retrieve an object |
| `quicbaas object delete` | Delete an object |
| `quicbaas object head` | Get object metadata |
| `quicbaas object list` | List objects |
| `quicbaas object search` | Search objects |
| `quicbaas object replicate` | Add replicas |
| `quicbaas object migrate` | Change primary backend |
| `quicbaas object archive` | Move to archive |
| `quicbaas catalog get` | Get catalog entry |
| `quicbaas catalog locations` | Get object locations |
| `quicbaas backend list` | List backends |
| `quicbaas backend status` | Check backend health |

## Global Options

| Option | Description | Default |
|--------|-------------|---------|
| `--server`, `-s` | Server address | `localhost:8080` |
| `--token`, `-t` | Authentication token | - |
| `--format`, `-f` | Output format (table, json, yaml) | `table` |
| `--timeout` | Request timeout | `30s` |

## Object Commands

### quicbaas object put

Store an object in the object storage system.

**Usage**

```bash
quicbaas object put <key> <file> [flags]
```

**Arguments**

| Argument | Description |
|----------|-------------|
| `key` | Object key in `namespace:path` format |
| `file` | Local file path or `-` for stdin |

**Flags**

| Flag | Type | Description | Default |
|------|------|-------------|---------|
| `--content-type` | string | MIME type | auto-detect |
| `--backend` | string | Target backend | from config |
| `--tenant` | string | Tenant ID | from token |
| `--encryption-key` | string | Override encryption key | from config |
| `--tag` | stringArray | Tags (key=value, repeatable) | - |
| `--replicate` | stringArray | Immediate replication targets | - |
| `--no-encrypt` | bool | Disable encryption | false |

**Examples**

```bash
# Simple put
quicbaas object put backups:daily/2024-01-15.tar.gz ./backup.tar.gz

# Put with metadata
quicbaas object put tenant-acme:docs/report.pdf ./report.pdf \
  --content-type application/pdf \
  --tag department=finance \
  --tag classification=internal

# Put to specific backend
quicbaas object put blobs:images/logo.png ./logo.png --backend s3-primary

# Put with immediate replication
quicbaas object put critical:data/config.json ./config.json \
  --replicate gcs-west \
  --replicate s3-backup

# Put from stdin
cat data.json | quicbaas object put system:config/settings.json -

# Put with custom encryption key
quicbaas object put secrets:keys/api.key ./api.key \
  --encryption-key "dek:secrets/api-keys"
```

**Output**

```
Object stored successfully
  Key:          backups:daily/2024-01-15.tar.gz
  Object ID:    1234567890
  Size:         10.5 MB
  Checksum:     sha256:abc123...
  Backend:      s3-primary
  Encrypted:    Yes (AES-256-GCM)
```

---

### quicbaas object get

Retrieve an object from storage.

**Usage**

```bash
quicbaas object get <key> [flags]
```

**Flags**

| Flag | Type | Description | Default |
|------|------|-------------|---------|
| `-o`, `--output` | string | Output file path | stdout |
| `--backend` | string | Prefer specific backend | auto |

**Examples**

```bash
# Get to stdout
quicbaas object get backups:daily/2024-01-15.tar.gz > backup.tar.gz

# Get to file
quicbaas object get tenant-acme:docs/report.pdf -o ./report.pdf

# Get from specific backend
quicbaas object get blobs:images/logo.png -o logo.png --backend gcs-west
```

---

### quicbaas object delete

Delete an object.

**Usage**

```bash
quicbaas object delete <key> [flags]
```

**Flags**

| Flag | Type | Description | Default |
|------|------|-------------|---------|
| `--hard` | bool | Permanent delete (skip soft delete) | false |
| `--force` | bool | Skip confirmation | false |

**Examples**

```bash
# Soft delete (can be recovered)
quicbaas object delete backups:old/archive.tar.gz

# Hard delete (permanent)
quicbaas object delete backups:old/archive.tar.gz --hard --force
```

---

### quicbaas object head

Get object metadata without downloading content.

**Usage**

```bash
quicbaas object head <key>
```

**Examples**

```bash
quicbaas object head tenant-acme:docs/report.pdf
```

**Output**

```
Key:           tenant-acme:docs/report.pdf
Size:          1.2 MB
Content-Type:  application/pdf
Checksum:      sha256:abc123...
Backend:       s3-primary
Created:       2024-01-15 10:30:00
Modified:      2024-01-15 10:30:00
Last Access:   2024-01-15 12:00:00
Status:        active
Encrypted:     Yes
```

---

### quicbaas object list

List objects matching criteria.

**Usage**

```bash
quicbaas object list [flags]
```

**Flags**

| Flag | Type | Description | Default |
|------|------|-------------|---------|
| `--prefix` | string | Key prefix filter | - |
| `--tenant` | string | Tenant filter | - |
| `--backend` | string | Backend filter | - |
| `--status` | string | Status filter (active, archived, deleted) | active |
| `--tag` | string | Tag filter (key=value) | - |
| `--limit` | int | Max results | 100 |

**Examples**

```bash
# List all objects in namespace
quicbaas object list --prefix backups:

# List tenant objects
quicbaas object list --tenant tenant-acme

# List by tag
quicbaas object list --tag department=finance

# List archived objects
quicbaas object list --status archived --limit 50
```

**Output**

```
KEY                                    SIZE      TYPE              STATUS    CREATED
backups:daily/2024-01-15.tar.gz       10.5 MB   application/gzip  active    2024-01-15
backups:daily/2024-01-14.tar.gz       10.2 MB   application/gzip  active    2024-01-14
backups:weekly/2024-w02.tar.gz        52.1 MB   application/gzip  active    2024-01-14

Showing 3 of 3 objects
```

---

### quicbaas object search

Advanced object search.

**Usage**

```bash
quicbaas object search [flags]
```

**Flags**

| Flag | Type | Description | Default |
|------|------|-------------|---------|
| `--prefix` | string | Key prefix | - |
| `--tag` | stringArray | Tag filters | - |
| `--size-min` | string | Minimum size | - |
| `--size-max` | string | Maximum size | - |
| `--created-after` | string | Created after date | - |
| `--created-before` | string | Created before date | - |
| `--sort` | string | Sort field (created_at, size, key) | created_at |
| `--order` | string | Sort order (asc, desc) | desc |

**Examples**

```bash
# Search large files
quicbaas object search --prefix tenant-acme: --size-min 100MB

# Search by date range
quicbaas object search --created-after 2024-01-01 --created-before 2024-01-31

# Complex search
quicbaas object search \
  --prefix tenant-acme: \
  --tag department=finance \
  --size-max 50MB \
  --sort size \
  --order desc
```

---

### quicbaas object replicate

Add replicas to an existing object.

**Usage**

```bash
quicbaas object replicate <key> [flags]
```

**Flags**

| Flag | Type | Description | Default |
|------|------|-------------|---------|
| `--target` | stringArray | Target backends (required) | - |
| `--mode` | string | Replication mode (sync, async) | async |
| `--wait` | bool | Wait for completion | false |

**Examples**

```bash
# Async replication
quicbaas object replicate tenant-acme:critical/data.json \
  --target gcs-west \
  --target s3-backup

# Sync replication (wait for completion)
quicbaas object replicate tenant-acme:critical/data.json \
  --target gcs-west \
  --mode sync

# Wait for async replication
quicbaas object replicate tenant-acme:critical/data.json \
  --target gcs-west \
  --wait
```

---

### quicbaas object migrate

Change the primary backend for an object.

**Usage**

```bash
quicbaas object migrate <key> --target <backend> [flags]
```

**Flags**

| Flag | Type | Description | Default |
|------|------|-------------|---------|
| `--target` | string | Target backend (required) | - |
| `--delete-source` | bool | Delete from old primary | false |

**Examples**

```bash
# Migrate primary
quicbaas object migrate tenant-acme:docs/report.pdf --target gcs-west

# Migrate and delete from old location
quicbaas object migrate tenant-acme:docs/report.pdf \
  --target gcs-west \
  --delete-source
```

---

### quicbaas object archive

Move object to archive storage.

**Usage**

```bash
quicbaas object archive <key> [flags]
```

**Flags**

| Flag | Type | Description | Default |
|------|------|-------------|---------|
| `--target` | string | Archive backend | from policy |
| `--delete-primary` | bool | Delete from primary after archive | false |
| `--compression` | int | Compression level (0-9) | 6 |

**Examples**

```bash
# Archive with defaults
quicbaas object archive tenant-acme:docs/old-report.pdf

# Archive and remove from primary
quicbaas object archive tenant-acme:docs/old-report.pdf \
  --delete-primary

# Archive with specific backend
quicbaas object archive tenant-acme:docs/old-report.pdf \
  --target s3-glacier \
  --compression 9
```

---

## Catalog Commands

### quicbaas catalog get

Get full catalog entry for an object.

**Usage**

```bash
quicbaas catalog get <key>
```

**Examples**

```bash
quicbaas catalog get tenant-acme:docs/report.pdf
quicbaas catalog get tenant-acme:docs/report.pdf --format json
```

**Output**

```
CATALOG ENTRY: tenant-acme:docs/report.pdf
─────────────────────────────────────────────
Object ID:      1234567890
Checksum:       sha256:abc123...
Size:           1.2 MB (1258291 bytes)
Content-Type:   application/pdf
Status:         active
Version:        1

LOCATIONS:
  Primary:      s3-primary
    ObjectRef:  obj/1234567890/v1
    Status:     active
    Stored:     650 KB (compressed)
    Synced:     2024-01-15 10:30:00

  Replica:      gcs-west
    ObjectRef:  obj/1234567890/v1
    Status:     active
    Synced:     2024-01-15 10:35:00

ENCRYPTION:
  Enabled:      Yes
  Key ID:       dek:tenant/acme/objects
  Algorithm:    AES-256-GCM

POLICIES:
  Replication:  2 replicas, async mode
  Archive:      after 90 days to s3-archive
  Retention:    disabled

METADATA:
  Tenant:       tenant-acme
  Tags:         department=finance
  Created:      2024-01-15 10:30:00
  Modified:     2024-01-15 10:30:00
  Last Access:  2024-01-15 12:00:00
```

---

### quicbaas catalog locations

Get all storage locations for an object.

**Usage**

```bash
quicbaas catalog locations <key>
```

**Output**

```
STORE         STATUS    SIZE      COMPRESSED  SYNCED
s3-primary    active    650 KB    Yes         2024-01-15 10:30:00
gcs-west      active    650 KB    Yes         2024-01-15 10:35:00
```

---

## Backend Commands

### quicbaas backend list

List configured storage backends.

**Usage**

```bash
quicbaas backend list
```

**Output**

```
NAME          TYPE    HEALTHY   LATENCY   CONFIG
s3-primary    s3      Yes       45ms      bucket=quicbaas-objects, region=us-east-1
gcs-west      gcs     Yes       52ms      bucket=quicbaas-gcs-west
s3-archive    s3      Yes       120ms     bucket=quicbaas-archive, class=GLACIER
local-cache   local   Yes       2ms       path=/data/object-cache
```

---

### quicbaas backend status

Check health status for a backend.

**Usage**

```bash
quicbaas backend status <name>
```

**Output**

```
Backend:      s3-primary
Type:         s3
Healthy:      Yes
Latency:      45ms
Last Check:   2024-01-15 12:00:00
Message:      OK

Configuration:
  Bucket:     quicbaas-objects
  Region:     us-east-1

Statistics:
  Objects:    15,234
  Size:       42.5 GB
```

---

## Practical Examples

### Backup Workflow

```bash
# Create daily backup
tar -czf backup.tar.gz /data
quicbaas object put backups:daily/$(date +%Y-%m-%d).tar.gz backup.tar.gz \
  --tag type=daily \
  --replicate gcs-west

# List recent backups
quicbaas object list --prefix backups:daily/ --limit 7

# Archive old backups
quicbaas object archive backups:daily/2024-01-01.tar.gz --delete-primary
```

### Multi-Tenant Document Management

```bash
# Upload tenant document
quicbaas object put tenant-acme:contracts/2024/agreement.pdf ./agreement.pdf \
  --content-type application/pdf \
  --tag department=legal \
  --tag year=2024

# Search tenant documents
quicbaas object search --prefix tenant-acme: --tag department=legal

# Check storage usage
quicbaas tenant usage tenant-acme
```

### Disaster Recovery

```bash
# Verify all replicas
quicbaas object list --prefix critical: | while read key; do
  quicbaas catalog locations "$key"
done

# Force replication to DR site
quicbaas object replicate critical:config/cluster.json \
  --target dr-site \
  --mode sync
```

---

## Error Handling

| Error | Resolution |
|-------|------------|
| `object not found` | Check key spelling, verify object exists |
| `backend unavailable` | Check backend health with `backend status` |
| `quota exceeded` | Contact admin to increase tenant quota |
| `permission denied` | Verify you have required role/permissions |
| `encryption failed` | Check KMS connectivity and key permissions |


---

## quicbaas objectstorage

Object storage commands for QuicBaaS.

Aliases: `obj`, `os`

### Overview

Object storage provides a key-value interface for storing arbitrary binary data (blobs, files, documents) with support for chunking large files. It is useful for storing assets, files, and large objects that don't fit the vector or key-value patterns.

All commands connect to a running QuicBaaS server via gRPC.

### Commands

#### put

Store an object with the specified key.

Alias: `write`

```bash
quicbaas objectstorage put <key>
```

##### Flags
- `--file string`: Read object data from file (use '-' for stdin)
- `--data string`: Object data as string (mutually exclusive with --file)
- `--content-type string`: MIME type of the object (default: auto-detected)
- `--metadata string`: JSON object of metadata key-value pairs

##### Examples

```bash
# Store data from command line
quicbaas objectstorage put my-key --data "Hello, World!"

# Store file contents
quicbaas objectstorage put images/logo --file ./logo.png --content-type image/png

# Store with metadata
quicbaas objectstorage put documents/report.pdf --file ./report.pdf \
  --metadata '{"author":"John","version":"1.0"}'

# Store from stdin
cat data.json | quicbaas objectstorage put config/app --file -

# Store with auto-detected content type
quicbaas objectstorage put assets/image.jpg --file ./image.jpg
```

#### get

Retrieve an object by key.

Alias: `read`

```bash
quicbaas objectstorage get <key>
```

##### Flags
- `--output string`: Write object data to file (use '-' for stdout)
- `--show-metadata`: Include metadata in output

##### Examples

```bash
# Get object and display to stdout
quicbaas objectstorage get my-key

# Get object and save to file
quicbaas objectstorage get images/logo --output ./downloaded-logo.png

# Get object with metadata (JSON output)
quicbaas objectstorage get my-key --show-metadata --output json

# Stream to stdout
quicbaas objectstorage get documents/report.pdf --output - > report.pdf
```

#### head

Get object metadata without downloading the content.

Alias: `info`

```bash
quicbaas objectstorage head <key>
```

##### Examples

```bash
# Get metadata for an object
quicbaas objectstorage head my-key

# Output as JSON
quicbaas objectstorage head my-key --output json
```

#### delete

Delete an object by key.

Alias: `rm`

```bash
quicbaas objectstorage delete <key>
```

##### Flags
- `-y, --yes`: Skip confirmation prompt

##### Examples

```bash
# Delete with confirmation
quicbaas objectstorage delete my-key

# Delete without confirmation
quicbaas objectstorage delete my-key --yes
```

#### list

List objects with an optional prefix.

Alias: `ls`

```bash
quicbaas objectstorage list [prefix]
```

##### Flags
- `--limit int`: Maximum number of results (default: 100)
- `--cursor string`: Pagination cursor for next page
- `--recursive`: List objects recursively (default: true)
- `--delimiter string`: Delimiter for pseudo-directory grouping

##### Examples

```bash
# List all objects
quicbaas objectstorage list

# List objects with prefix
quicbaas objectstorage list images/

# List with pagination
quicbaas objectstorage list --limit 50

# List as JSON
quicbaas objectstorage list --output json
```

#### copy

Copy an object to a new key.

Alias: `cp`

```bash
quicbaas objectstorage copy <source-key> <dest-key>
```

##### Flags
- `--overwrite`: Overwrite destination if it exists

##### Examples

```bash
# Copy object
quicbaas objectstorage copy images/logo.png images/logo-backup.png

# Copy with overwrite
quicbaas objectstorage copy images/new-logo.png images/logo.png --overwrite
```

#### exists

Check if an object exists.

```bash
quicbaas objectstorage exists <key>
```

##### Examples

```bash
# Check if object exists
quicbaas objectstorage exists my-key

# Use in scripts (exits 0 if exists, 1 if not)
if quicbaas objectstorage exists my-key; then
  echo "Object exists"
fi
```

#### stat

Get detailed statistics about an object.

```bash
quicbaas objectstorage stat <key>
```

##### Examples

```bash
# Get object statistics
quicbaas objectstorage stat my-key

# Output as JSON
quicbaas objectstorage stat my-key --output json
```

### Global Flags

All objectstorage commands support these flags:

| Flag | Description | Default |
|------|-------------|---------|
| `--timeout` | Request timeout | `30s` |
| `--output` | Output format (json, yaml, table) | `table` |
| `--tls` | Enable TLS | `false` |
| `--tls-cert` | TLS client certificate file | - |
| `--tls-key` | TLS client key file | - |
| `--tls-ca` | TLS CA certificate file | - |

### Key Naming Conventions

Keys can include forward slashes (`/`) to create a pseudo-directory structure:

- `images/logos/company.png`
- `documents/2024/reports/q1.pdf`
- `config/app/settings.json`

### Content Type Detection

When `--content-type` is not specified, the content type is auto-detected based on:
1. File extension (when using `--file`)
2. File content magic bytes
3. Default to `application/octet-stream`

### Notes

- Large objects are automatically chunked for efficient storage and transfer
- Metadata keys must be strings; values are stored as strings
- Use the `--show-metadata` flag to view custom metadata
- Object keys are case-sensitive
- Maximum object size depends on server configuration
