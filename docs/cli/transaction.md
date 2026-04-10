# quicbaas txn

Transaction commands for atomic compare-and-swap operations.

## Overview

Transactions in QuicBaaS allow you to:
1. Specify compare conditions (predicates)
2. Define success operations (executed if all comparisons succeed)
3. Define failure operations (executed if any comparison fails)
4. Execute the transaction atomically

## Transaction Builder Pattern

Transactions are built incrementally using subcommands:
1. Add comparison conditions with `compare`
2. Add success operations with `success`
3. Add failure operations with `failure`
4. Execute with `execute`

## Commands

### compare

Add a comparison condition to the transaction.

```bash
quicbaas txn compare <entity-type> <id> <target> <operator> [value]
```

#### Targets
- **version**: Compare entity version number
- **value**: Compare entity value (requires JSON value argument)
- **exists**: Check if entity exists

#### Operators
- **equal**, **not_equal**, **greater**, **less**, **greater_equal**, **less_equal**

#### Examples

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

### success

Add an operation to execute if all comparisons succeed.

```bash
quicbaas txn success <operation> <entity-type> <id> [data]
```

#### Operations
- **put**: Create or update entity (requires JSON data argument)
- **delete**: Delete entity
- **get**: Retrieve entity

#### Examples

```bash
# Add a put operation
quicbaas txn success put user 123 '{"name":"John Updated"}'

# Add a delete operation
quicbaas txn success delete user 123

# Add a get operation
quicbaas txn success get user 123
```

### failure

Add an operation to execute if any comparison fails.

```bash
quicbaas txn failure <operation> <entity-type> <id> [data]
```

#### Operations
- **put**: Create or update entity (requires JSON data argument)
- **delete**: Delete entity
- **get**: Retrieve entity

#### Examples

```bash
# Add a put operation
quicbaas txn failure put user 123 '{"name":"John New"}'

# Add a delete operation
quicbaas txn failure delete user 123

# Add a get operation
quicbaas txn failure get user 123
```

### execute

Execute the transaction with all configured comparisons and operations.

```bash
quicbaas txn execute
```

#### Examples

```bash
# Execute the configured transaction
quicbaas txn execute

# Execute with JSON output
quicbaas txn execute --output json
```

### reset

Reset the transaction, clearing all comparison conditions and operations.

```bash
quicbaas txn reset
```

#### Examples

```bash
# Reset the transaction
quicbaas txn reset
```

### show

Display the current transaction state.

```bash
quicbaas txn show
```

#### Examples

```bash
# Show current transaction
quicbaas txn show
```

## Transaction Examples

### Conditional Update

Check if user exists, update if so, create if not:

```bash
quicbaas txn compare user 123 exists equal
quicbaas txn success put user 123 '{"name":"John Updated"}'
quicbaas txn failure put user 123 '{"name":"John New"}'
quicbaas txn execute
```

### Version-Based Update

Update only if version matches (optimistic locking):

```bash
quicbaas txn compare user 123 version equal 1
quicbaas txn success put user 123 '{"name":"Updated","version":2}'
quicbaas txn execute
```

### Read-Modify-Write

Read and update in a single transaction:

```bash
quicbaas txn success get user 123
quicbaas txn success put user 123 '{"name":"Modified","updated_at":"2025-10-12"}'
quicbaas txn execute
```

### Multi-Condition Transaction

Multiple conditions with multiple operations:

```bash
quicbaas txn compare user 123 exists equal
quicbaas txn compare user 456 exists not_equal
quicbaas txn success put user 123 '{"name":"Updated"}'
quicbaas txn success put user 456 '{"name":"New"}'
quicbaas txn failure delete user 123
quicbaas txn execute
```

## Transaction Execution

When executed:
1. All comparison conditions are evaluated atomically
2. If **all** comparisons succeed → success operations are executed
3. If **any** comparison fails → failure operations are executed
4. Results are returned for each operation
5. Transaction state is reset

## Notes

- Transactions are atomic (all-or-nothing per operation set)
- Comparisons are evaluated in order
- Success/failure operations are executed in order
- Transaction state is reset after execution
- Use `show` to review transaction before executing
- Use `reset` to start over without executing
- All operations within a transaction use strong consistency
