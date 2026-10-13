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

## Tenant databases

The CLI has no `database` command group. At startup the server opens every
tenant database already on disk through the embedded qrdb database manager
(`pkg/server/lifecycle_databases.go`). No CLI verb creates, lists or drops a
tenant database.

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
