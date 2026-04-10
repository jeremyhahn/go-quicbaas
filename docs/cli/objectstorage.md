# quicbaas objectstorage

Object storage commands for QuicBaaS.

Aliases: `obj`, `os`

## Overview

Object storage provides a key-value interface for storing arbitrary binary data (blobs, files, documents) with support for chunking large files. It is useful for storing assets, files, and large objects that don't fit the vector or key-value patterns.

All commands connect to a running QuicBaaS server via gRPC.

## Commands

### put

Store an object with the specified key.

Alias: `write`

```bash
quicbaas objectstorage put <key>
```

#### Flags
- `--file string`: Read object data from file (use '-' for stdin)
- `--data string`: Object data as string (mutually exclusive with --file)
- `--content-type string`: MIME type of the object (default: auto-detected)
- `--metadata string`: JSON object of metadata key-value pairs

#### Examples

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

### get

Retrieve an object by key.

Alias: `read`

```bash
quicbaas objectstorage get <key>
```

#### Flags
- `--output string`: Write object data to file (use '-' for stdout)
- `--show-metadata`: Include metadata in output

#### Examples

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

### head

Get object metadata without downloading the content.

Alias: `info`

```bash
quicbaas objectstorage head <key>
```

#### Examples

```bash
# Get metadata for an object
quicbaas objectstorage head my-key

# Output as JSON
quicbaas objectstorage head my-key --output json
```

### delete

Delete an object by key.

Alias: `rm`

```bash
quicbaas objectstorage delete <key>
```

#### Flags
- `-y, --yes`: Skip confirmation prompt

#### Examples

```bash
# Delete with confirmation
quicbaas objectstorage delete my-key

# Delete without confirmation
quicbaas objectstorage delete my-key --yes
```

### list

List objects with an optional prefix.

Alias: `ls`

```bash
quicbaas objectstorage list [prefix]
```

#### Flags
- `--limit int`: Maximum number of results (default: 100)
- `--cursor string`: Pagination cursor for next page
- `--recursive`: List objects recursively (default: true)
- `--delimiter string`: Delimiter for pseudo-directory grouping

#### Examples

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

### copy

Copy an object to a new key.

Alias: `cp`

```bash
quicbaas objectstorage copy <source-key> <dest-key>
```

#### Flags
- `--overwrite`: Overwrite destination if it exists

#### Examples

```bash
# Copy object
quicbaas objectstorage copy images/logo.png images/logo-backup.png

# Copy with overwrite
quicbaas objectstorage copy images/new-logo.png images/logo.png --overwrite
```

### exists

Check if an object exists.

```bash
quicbaas objectstorage exists <key>
```

#### Examples

```bash
# Check if object exists
quicbaas objectstorage exists my-key

# Use in scripts (exits 0 if exists, 1 if not)
if quicbaas objectstorage exists my-key; then
  echo "Object exists"
fi
```

### stat

Get detailed statistics about an object.

```bash
quicbaas objectstorage stat <key>
```

#### Examples

```bash
# Get object statistics
quicbaas objectstorage stat my-key

# Output as JSON
quicbaas objectstorage stat my-key --output json
```

## Global Flags

All objectstorage commands support these flags:

| Flag | Description | Default |
|------|-------------|---------|
| `--timeout` | Request timeout | `30s` |
| `--output` | Output format (json, yaml, table) | `table` |
| `--tls` | Enable TLS | `false` |
| `--tls-cert` | TLS client certificate file | - |
| `--tls-key` | TLS client key file | - |
| `--tls-ca` | TLS CA certificate file | - |

## Key Naming Conventions

Keys can include forward slashes (`/`) to create a pseudo-directory structure:

- `images/logos/company.png`
- `documents/2024/reports/q1.pdf`
- `config/app/settings.json`

## Content Type Detection

When `--content-type` is not specified, the content type is auto-detected based on:
1. File extension (when using `--file`)
2. File content magic bytes
3. Default to `application/octet-stream`

## Notes

- Large objects are automatically chunked for efficient storage and transfer
- Metadata keys must be strings; values are stored as strings
- Use the `--show-metadata` flag to view custom metadata
- Object keys are case-sensitive
- Maximum object size depends on server configuration
