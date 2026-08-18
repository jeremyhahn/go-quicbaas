# Data Services — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag, with runnable examples. Feature folder: `docs/data-services/`.

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


---

## quicbaas database

Database management commands for QuicBaaS multi-tenant BaaS.

### Overview

The `quicbaas database` command group manages tenant databases through
the BaaS layer. All operations are scoped to the authenticated tenant
unless the caller has `db:admin` role for cross-tenant access.

### Global Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--tenant` | Tenant ID (extracted from auth context if not set) | - |
| `--output` | Output format (json, yaml, table) | `table` |
| `-y, --yes` | Skip confirmation prompts | `false` |

### Commands

#### database create

Create a new database for the current tenant.

```bash
quicbaas database create <name>
```

##### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--engine` | `kv` | Engine type (kv, timeseries, vector) |
| `--backend` | `raft` | Backend type (memory, file, pebble, raft) |
| `--etcd` | `false` | Enable etcd protocol |
| `--etcd-listen-addr` | `:2379` | etcd gRPC listen address |
| `--etcd-advertise-urls` | | etcd advertise URLs |
| `--barrier` | `false` | Enable barrier encryption |
| `--barrier-key-id` | | Barrier encryption key ID |

##### Examples

```bash
# Basic KV database
quicbaas database create orders --tenant acme

# With etcd protocol
quicbaas database create k8s --tenant acme --engine kv --backend raft \
  --etcd --etcd-listen-addr :2379 --etcd-advertise-urls https://node1:2379

# With barrier encryption
quicbaas database create secrets --tenant acme --barrier
```

##### Output

```
Database created successfully.

NAME           acme_orders
ENGINE         kv
BACKEND        raft
SHARD          104
TENANT         acme
STATUS         open
```

#### database list

List databases for the current tenant.

```bash
quicbaas database list
```

##### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--all` | `false` | List all databases (admin only) |

##### Examples

```bash
# List tenant databases
quicbaas database list --tenant acme

# List all databases (admin)
quicbaas database list --all

# JSON output
quicbaas database list --tenant acme --output json
```

##### Output (table)

```
NAME            ENGINE      BACKEND    SHARD    STATUS
acme_system     kv          raft       101      open
acme_orders     kv          raft       104      open
acme_logs       timeseries  raft       105      open
```

##### Output (json)

```json
[
  {
    "name": "acme_orders",
    "tenant_id": "acme",
    "engine": "kv",
    "backend": "raft",
    "shard_id": 104,
    "purpose": "user",
    "created": "2026-03-10T14:30:00Z",
    "updated": "2026-03-10T14:30:00Z"
  }
]
```

#### database info

Get detailed information about a database.

```bash
quicbaas database info <name>
```

##### Examples

```bash
quicbaas database info orders --tenant acme
quicbaas database info orders --tenant acme --output json
```

##### Output

```json
{
  "name": "acme_orders",
  "tenant_id": "acme",
  "engine": "kv",
  "backend": "raft",
  "shard_id": 104,
  "purpose": "user",
  "status": "open",
  "created": "2026-03-10T14:30:00Z",
  "updated": "2026-03-10T14:30:00Z"
}
```

#### database drop

Drop a database and permanently delete all its data.

```bash
quicbaas database drop <name>
```

##### Examples

```bash
quicbaas database drop orders --tenant acme
quicbaas database drop orders --tenant acme --yes  # skip confirmation
```

Dropping a database:

1. Closes the database
2. Deletes the database DEK (`DeleteDatabaseDEK`)
3. Removes the database record from the tenant's system database
4. Deletes all data files

#### database update-etcd

Update etcd protocol configuration for a database.

```bash
quicbaas database update-etcd <name>
```

##### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--enabled` | | Enable etcd protocol |
| `--disabled` | | Disable etcd protocol |
| `--listen-addr` | `:2379` | etcd gRPC listen address |
| `--advertise-urls` | | Advertise URLs for etcd client discovery |

##### Examples

```bash
quicbaas database update-etcd k8s --tenant acme --enabled \
  --listen-addr :2379 --advertise-urls https://node1:2379

quicbaas database update-etcd k8s --tenant acme --disabled
```

#### database update-protocol

Update SQL wire protocol configuration.

```bash
quicbaas database update-protocol <name>
```

##### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--mysql` | `false` | Enable MySQL protocol |
| `--mysql-port` | `3306` | MySQL listen port |
| `--postgresql` | `false` | Enable PostgreSQL protocol |
| `--postgresql-port` | `5432` | PostgreSQL listen port |

##### Examples

```bash
quicbaas database update-protocol analytics --tenant acme \
  --mysql --mysql-port 3306

quicbaas database update-protocol analytics --tenant acme \
  --postgresql --postgresql-port 5432
```

#### database update-raft

Update Raft consensus configuration.

```bash
quicbaas database update-raft <name>
```

##### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--election-rtt` | `10` | Election RTT in number of heartbeat intervals |
| `--heartbeat-rtt` | `1` | Heartbeat RTT in milliseconds |

##### Examples

```bash
quicbaas database update-raft orders --tenant acme \
  --election-rtt 10 --heartbeat-rtt 1
```

#### database update-barrier

Update barrier encryption configuration.

```bash
quicbaas database update-barrier <name>
```

##### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--enabled` | | Enable barrier encryption |
| `--disabled` | | Disable barrier encryption |
| `--key-id` | | DEK ID for barrier encryption |

##### Examples

```bash
quicbaas database update-barrier orders --tenant acme --enabled
quicbaas database update-barrier orders --tenant acme --disabled
```

#### database update-connection

Update connection pool and timeout settings.

```bash
quicbaas database update-connection <name>
```

##### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--max-connections` | `100` | Maximum concurrent connections |
| `--idle-timeout` | `30s` | Idle connection timeout |

##### Examples

```bash
quicbaas database update-connection orders --tenant acme \
  --max-connections 200 --idle-timeout 60s
```

### Cross-References

- [Database API Reference](api.md)
- [Multi-Tenant Architecture](../multi-tenancy/architecture.md)
- Per-Database Encryption


---

## quicbaas txn

Transaction commands for atomic compare-and-swap operations.

### Overview

Transactions in QuicBaaS allow you to:
1. Specify compare conditions (predicates)
2. Define success operations (executed if all comparisons succeed)
3. Define failure operations (executed if any comparison fails)
4. Execute the transaction atomically

### Transaction Builder Pattern

Transactions are built incrementally using subcommands:
1. Add comparison conditions with `compare`
2. Add success operations with `success`
3. Add failure operations with `failure`
4. Execute with `execute`

### Commands

#### compare

Add a comparison condition to the transaction.

```bash
quicbaas txn compare <entity-type> <id> <target> <operator> [value]
```

##### Targets
- **version**: Compare entity version number
- **value**: Compare entity value (requires JSON value argument)
- **exists**: Check if entity exists

##### Operators
- **equal**, **not_equal**, **greater**, **less**, **greater_equal**, **less_equal**

##### Examples

```bash
# Check if user 123 exists
quicbaas txn compare user 123 exists equal

# Check if user 123 does not exist
quicbaas txn compare user 123 exists not_equal

# Check if user 123 version equals 1
quicbaas txn compare user 123 version equal 1

# Check if user 123 value matches JSON
quicbaas txn compare user 123 value equal '{"name":"John"}'
```

#### success

Add an operation to execute if all comparisons succeed.

```bash
quicbaas txn success <operation> <entity-type> <id> [data]
```

##### Operations
- **put**: Create or update entity (requires JSON data argument)
- **delete**: Delete entity
- **get**: Retrieve entity

##### Examples

```bash
# Add a put operation
quicbaas txn success put user 123 '{"name":"John Updated"}'

# Add a delete operation
quicbaas txn success delete user 123

# Add a get operation
quicbaas txn success get user 123
```

#### failure

Add an operation to execute if any comparison fails.

```bash
quicbaas txn failure <operation> <entity-type> <id> [data]
```

##### Operations
- **put**: Create or update entity (requires JSON data argument)
- **delete**: Delete entity
- **get**: Retrieve entity

##### Examples

```bash
# Add a put operation
quicbaas txn failure put user 123 '{"name":"John New"}'

# Add a delete operation
quicbaas txn failure delete user 123

# Add a get operation
quicbaas txn failure get user 123
```

#### execute

Execute the transaction with all configured comparisons and operations.

```bash
quicbaas txn execute
```

##### Examples

```bash
# Execute the configured transaction
quicbaas txn execute

# Execute with JSON output
quicbaas txn execute --output json
```

#### reset

Reset the transaction, clearing all comparison conditions and operations.

```bash
quicbaas txn reset
```

##### Examples

```bash
# Reset the transaction
quicbaas txn reset
```

#### show

Display the current transaction state.

```bash
quicbaas txn show
```

##### Examples

```bash
# Show current transaction
quicbaas txn show
```

### Transaction Examples

#### Conditional Update

Check if user exists, update if so, create if not:

```bash
quicbaas txn compare user 123 exists equal
quicbaas txn success put user 123 '{"name":"John Updated"}'
quicbaas txn failure put user 123 '{"name":"John New"}'
quicbaas txn execute
```

#### Version-Based Update

Update only if version matches (optimistic locking):

```bash
quicbaas txn compare user 123 version equal 1
quicbaas txn success put user 123 '{"name":"Updated","version":2}'
quicbaas txn execute
```

#### Read-Modify-Write

Read and update in a single transaction:

```bash
quicbaas txn success get user 123
quicbaas txn success put user 123 '{"name":"Modified","updated_at":"2025-10-12"}'
quicbaas txn execute
```

#### Multi-Condition Transaction

Multiple conditions with multiple operations:

```bash
quicbaas txn compare user 123 exists equal
quicbaas txn compare user 456 exists not_equal
quicbaas txn success put user 123 '{"name":"Updated"}'
quicbaas txn success put user 456 '{"name":"New"}'
quicbaas txn failure delete user 123
quicbaas txn execute
```

### Transaction Execution

When executed:
1. All comparison conditions are evaluated atomically
2. If **all** comparisons succeed → success operations are executed
3. If **any** comparison fails → failure operations are executed
4. Results are returned for each operation
5. Transaction state is reset

### Notes

- Transactions are atomic (all-or-nothing per operation set)
- Comparisons are evaluated in order
- Success/failure operations are executed in order
- Transaction state is reset after execution
- Use `show` to review transaction before executing
- Use `reset` to start over without executing
- All operations within a transaction use strong consistency
