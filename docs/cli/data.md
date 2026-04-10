# quicbaas data

Data manipulation commands for interacting with QuicBaaS entities.

## Overview

These commands allow you to perform CRUD operations on entities stored in QuicBaaS. All commands connect to a running QuicBaaS server via gRPC.

## Global Flags

- `--address string`: QuicBaaS server address (default: Unix socket or "localhost:50051")
- `--transport string`: Transport protocol (grpc, rest) (default: "grpc")
- `--timeout duration`: Request timeout (default: 30s)
- `--output string`: Output format (json, yaml, table, raw) (default: "table")
- `--consistency string`: Consistency level (strong, linearizable, eventual, local) (default: "linearizable")
- `--tls`: Enable TLS
- `--tls-cert string`: TLS client certificate file
- `--tls-key string`: TLS client key file
- `--tls-ca string`: TLS CA certificate file
- `--max-retries int`: Maximum number of retry attempts (default: 3)
- `--retry-backoff duration`: Initial retry backoff duration (default: 100ms)

## Commands

### put

Store a key-value pair in QuicBaaS.

```bash
quicbaas data put <type> <key> <value>
```

#### Flags
- `--json`: Parse value as JSON

#### Examples

```bash
# Store a simple string value
quicbaas data put user john '{"name":"John Doe","email":"john@example.com"}'

# Store JSON data
quicbaas data put user john --json < user.json

# Store with strong consistency
quicbaas data put user john '{"name":"John"}' --consistency strong

# Store with TLS
quicbaas data put user john '{"name":"John"}' --tls --tls-cert client.crt --tls-key client.key --tls-ca ca.crt
```

### get

Retrieve a value by key from QuicBaaS.

```bash
quicbaas data get <type> <key>
```

#### Examples

```bash
# Get a value as table format (default)
quicbaas data get user john

# Get a value as JSON
quicbaas data get user john --output json

# Get raw value (useful for piping)
quicbaas data get user john --output raw > user.json

# Get with strong consistency
quicbaas data get user john --consistency strong

# Get with TLS
quicbaas data get user john --tls --tls-cert client.crt --tls-key client.key --tls-ca ca.crt
```

### delete

Delete a key-value pair from QuicBaaS.

```bash
quicbaas data delete <type> <key>
```

#### Flags
- `-y, --yes`: Skip confirmation prompt

#### Examples

```bash
# Delete with confirmation
quicbaas data delete user john

# Delete without confirmation
quicbaas data delete user john --yes

# Delete with strong consistency
quicbaas data delete user john --consistency strong --yes

# Delete with TLS
quicbaas data delete user john --yes --tls --tls-cert client.crt --tls-key client.key
```

### list

List entities of a given type with pagination support.

```bash
quicbaas data list <type>
```

#### Flags
- `--limit int`: Maximum number of items to return (default: 100)
- `--cursor string`: Pagination cursor from previous response

#### Examples

```bash
# List first 100 entities (default)
quicbaas data list user

# List with custom limit
quicbaas data list user --limit 50

# List next page using cursor
quicbaas data list user --cursor "cursor_token_from_previous_response"

# List as JSON
quicbaas data list user --output json

# List with TLS
quicbaas data list user --tls --tls-cert client.crt --tls-key client.key --tls-ca ca.crt
```

### query

Query entities using secondary indexes.

```bash
quicbaas data query <type>
```

#### Flags
- `--index string`: Index field to query (required)
- `--value string`: Value to search for (required)
- `--limit int`: Maximum number of results (default: 100)

#### Examples

```bash
# Query users by email index
quicbaas data query user --index email --value "john@example.com"

# Query with custom limit
quicbaas data query user --index email --value "john@example.com" --limit 50

# Query with JSON output
quicbaas data query user --index email --value "john@example.com" --output json

# Query with TLS
quicbaas data query user --index email --value "john@example.com" --tls --tls-cert client.crt --tls-key client.key
```

## Consistency Levels

- **strong**: Strongest consistency, reads are guaranteed to return the most recent write
- **linearizable**: Default, provides linearizable consistency
- **eventual**: Allows stale reads for better performance
- **local**: Same as eventual, reads from local replica

## Notes

- All data is automatically encrypted at rest
- Automatic indexing is enabled for all entity fields
- Entity types are created automatically on first use
- Values must be valid JSON for most operations
- Pagination cursors are opaque tokens returned by the server
