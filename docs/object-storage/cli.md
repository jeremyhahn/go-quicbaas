# Object Storage — CLI

> **Status: NORMATIVE** · Last verified: 2026-10-12
> Every verb and flag, with runnable examples. Feature folder: `docs/object-storage/`.

`quicbaas object` (aliases `obj`, `os`) routes through the active `--transport`
and acts on the tenant named by the global `--tenant`. Object arguments are a
single key, written `<bucket>/<key>` by convention; the CLI sends the whole
string as the key.

## Command Overview

| Command | Description |
|---------|-------------|
| `object put <bucket>/<key>` | Store an object |
| `object get <bucket>/<key>` | Retrieve an object |
| `object delete <bucket>/<key>` | Delete an object |
| `object head <bucket>/<key>` | Get object metadata without data |
| `object list <bucket>` | List objects in a bucket |
| `object list-by-tag` | List objects by tag |
| `object list-by-tenant` | List the tenant's objects |
| `object copy <source> <dest>` | Copy an object |
| `object archive <bucket>/<key>` | Archive an object to cold storage |
| `object restore <bucket>/<key>` | Restore an archived object |
| `object verify <bucket>/<key>` | Verify object integrity across replicas |
| `object replicate <bucket>/<key>` | Replicate an object to other backends |
| `object migrate <bucket>/<key>` | Move an object to another backend |
| `object metadata get <bucket>/<key>` | Get object policies and metadata |
| `object metadata update <bucket>/<key>` | Update object policies |
| `object lifecycle set <bucket>` | Set a bucket's lifecycle policy |
| `object lifecycle get <bucket>` | Get a bucket's lifecycle policy |
| `object stats` | List storage backends with their health |

There is no search, catalog, backend or per-tenant usage verb. Use
`object list --prefix` or `object list-by-tag` to find objects, `object head`
or `object metadata get` for an object's entry, and `object stats` for backends.

## Options

Persistent on `object` and every subcommand:

| Option | Description | Default |
|--------|-------------|---------|
| `--output` | `json`, `yaml`, `table` or `raw` | `table` |
| `--tls` | Enable TLS | `false` |
| `--tls-cert`, `--tls-key`, `--tls-ca` | Client certificate, key and CA files | — |
| `--max-retries` | Maximum retry attempts | `3` |
| `--retry-backoff` | Initial retry backoff | `100ms` |

Global options that apply: `--transport` (default `unix`), `--address`,
`--tenant`, `--timeout` (default `30s`).

## Object Commands

### object put

| Flag | Description | Default |
|------|-------------|---------|
| `-f`, `--file` | Read data from this file | stdin |
| `--content-type` | Content type | `application/octet-stream` |
| `--tag key=value` | Tag (repeatable) | — |
| `--metadata` | JSON object of string pairs, stored as tags; ignored when `--tag` is given | — |

Refused before any call: no `--file` and nothing piped to stdin; an unreadable
file; `--metadata` that is not a JSON object of strings.

```bash
quicbaas object put docs/report.pdf --file report.pdf --content-type application/pdf
quicbaas object put docs/report.pdf --file report.pdf --tag dept=finance --tag year=2026
echo "hello world" | quicbaas object put docs/hello.txt
```

### object get

| Flag | Description | Default |
|------|-------------|---------|
| `-o`, `--output-file` | Write data to this file | stdout |
| `--include-metadata` | Include full metadata | `false` |

```bash
quicbaas object get docs/report.pdf -o report.pdf
```

### object delete

| Flag | Description | Default |
|------|-------------|---------|
| `--permanent` | Bypass soft delete | `false` |

```bash
quicbaas object delete docs/report.pdf
quicbaas object delete docs/report.pdf --permanent
```

### object head

No flags.

```bash
quicbaas object head docs/report.pdf --output json
```

### object list

| Flag | Description | Default |
|------|-------------|---------|
| `--prefix` | Filter by key prefix | — |
| `-n`, `--limit` | Maximum objects returned | `100` |
| `--cursor` | Cursor from the previous page | — |
| `--include-metadata` | Include full metadata | `false` |

```bash
quicbaas object list docs --prefix 2026/ --limit 50
```

### object list-by-tag

| Flag | Description | Default |
|------|-------------|---------|
| `--tag key=value` | Tag filter (required) | — |
| `-n`, `--limit` | Maximum objects returned | `100` |
| `--cursor` | Cursor from the previous page | — |

A `--tag` without `=` is refused before any call.

```bash
quicbaas object list-by-tag --tag dept=finance
```

### object list-by-tenant

Lists the objects of the tenant given by the global `--tenant`.

| Flag | Description | Default |
|------|-------------|---------|
| `-n`, `--limit` | Maximum objects returned | `100` |
| `--cursor` | Cursor from the previous page | — |
| `--include-metadata` | Include full metadata | `false` |

```bash
quicbaas object list-by-tenant --tenant tenant-acme
```

### object copy

| Flag | Description | Default |
|------|-------------|---------|
| `--copy-tags` | Copy tags to the destination | `true` |

```bash
quicbaas object copy docs/report.pdf backup/report.pdf
```

### object archive

| Flag | Description | Default |
|------|-------------|---------|
| `--target-store` | Archive backend | policy default |
| `--delete-from-primary` | Delete from primary after archival | `false` |
| `--compression-level` | 0–9; 0 uses the default | `0` |

```bash
quicbaas object archive backup/2025-12.tar --delete-from-primary
```

### object restore

| Flag | Description | Default |
|------|-------------|---------|
| `--target-backend` | Backend to restore to | — |
| `--restore-days` | Days the restored copy stays accessible | `7` |
| `--tier` | `hot` or `warm` | — |

```bash
quicbaas object restore backup/2025-12.tar --tier warm --restore-days 3
```

### object verify

| Flag | Description | Default |
|------|-------------|---------|
| `--check-all` | Verify every replica location | `true` |
| `--repair` | Repair inconsistent replicas | `false` |

```bash
quicbaas object verify docs/report.pdf --repair
```

### object replicate

| Flag | Description | Default |
|------|-------------|---------|
| `--dest` | Destination backend(s), comma-separated (required) | — |
| `--sync` | Wait for replication to complete | `false` |

```bash
quicbaas object replicate docs/report.pdf --dest s3-dr,azure-backup --sync
```

### object migrate

| Flag | Description | Default |
|------|-------------|---------|
| `--dest` | Destination backend (required) | — |
| `--delete-source` | Delete from the source after migration | `false` |

```bash
quicbaas object migrate docs/report.pdf --dest gcs-primary --delete-source
```

### object metadata get / update

`metadata get` takes no flags. `metadata update` takes `--metadata` (required): a
JSON object with any of `replication`, `archive` and `retention` policies.
Invalid JSON is refused before any call.

```bash
quicbaas object metadata get docs/report.pdf --output json
quicbaas object metadata update docs/report.pdf --metadata '{"retention":{"enabled":true,"retention_days":365}}'
```

### object lifecycle set / get

`lifecycle set <bucket>` takes `--policy` (required, JSON). Invalid JSON is
refused before any call. `lifecycle get <bucket>` takes no flags.

```bash
quicbaas object lifecycle get backup
```

### object stats

Lists the configured storage backends with their health. No flags.

```bash
quicbaas object stats --output json
```

## Error Handling

| Error | Resolution |
|-------|------------|
| `object not found` | Check the key; `object list <bucket> --prefix` |
| `no data provided` | Pass `--file` or pipe data to stdin |
| `permission denied` | Verify the caller's role and `--tenant` |
| backend errors | Check backend health with `object stats` |
