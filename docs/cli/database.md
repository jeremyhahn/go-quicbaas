# quicbaas database

Database management commands for QuicBaaS multi-tenant BaaS.

## Overview

The `quicbaas database` command group manages tenant databases through
the BaaS layer. All operations are scoped to the authenticated tenant
unless the caller has `db:admin` role for cross-tenant access.

## Global Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--tenant` | Tenant ID (extracted from auth context if not set) | - |
| `--output` | Output format (json, yaml, table) | `table` |
| `-y, --yes` | Skip confirmation prompts | `false` |

## Commands

### database create

Create a new database for the current tenant.

```bash
quicbaas database create <name>
```

#### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--engine` | `kv` | Engine type (kv, timeseries, vector) |
| `--backend` | `raft` | Backend type (memory, file, pebble, raft) |
| `--etcd` | `false` | Enable etcd protocol |
| `--etcd-listen-addr` | `:2379` | etcd gRPC listen address |
| `--etcd-advertise-urls` | | etcd advertise URLs |
| `--barrier` | `false` | Enable barrier encryption |
| `--barrier-key-id` | | Barrier encryption key ID |

#### Examples

```bash
# Basic KV database
quicbaas database create orders --tenant acme

# With etcd protocol
quicbaas database create k8s --tenant acme --engine kv --backend raft \
  --etcd --etcd-listen-addr :2379 --etcd-advertise-urls https://node1:2379

# With barrier encryption
quicbaas database create secrets --tenant acme --barrier
```

#### Output

```
Database created successfully.

NAME           acme_orders
ENGINE         kv
BACKEND        raft
SHARD          104
TENANT         acme
STATUS         open
```

### database list

List databases for the current tenant.

```bash
quicbaas database list
```

#### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--all` | `false` | List all databases (admin only) |

#### Examples

```bash
# List tenant databases
quicbaas database list --tenant acme

# List all databases (admin)
quicbaas database list --all

# JSON output
quicbaas database list --tenant acme --output json
```

#### Output (table)

```
NAME            ENGINE      BACKEND    SHARD    STATUS
acme_system     kv          raft       101      open
acme_orders     kv          raft       104      open
acme_logs       timeseries  raft       105      open
```

#### Output (json)

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

### database info

Get detailed information about a database.

```bash
quicbaas database info <name>
```

#### Examples

```bash
quicbaas database info orders --tenant acme
quicbaas database info orders --tenant acme --output json
```

#### Output

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

### database drop

Drop a database and permanently delete all its data.

```bash
quicbaas database drop <name>
```

#### Examples

```bash
quicbaas database drop orders --tenant acme
quicbaas database drop orders --tenant acme --yes  # skip confirmation
```

Dropping a database:

1. Closes the database
2. Deletes the database DEK (`DeleteDatabaseDEK`)
3. Removes the database record from the tenant's system database
4. Deletes all data files

### database update-etcd

Update etcd protocol configuration for a database.

```bash
quicbaas database update-etcd <name>
```

#### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--enabled` | | Enable etcd protocol |
| `--disabled` | | Disable etcd protocol |
| `--listen-addr` | `:2379` | etcd gRPC listen address |
| `--advertise-urls` | | Advertise URLs for etcd client discovery |

#### Examples

```bash
quicbaas database update-etcd k8s --tenant acme --enabled \
  --listen-addr :2379 --advertise-urls https://node1:2379

quicbaas database update-etcd k8s --tenant acme --disabled
```

### database update-protocol

Update SQL wire protocol configuration.

```bash
quicbaas database update-protocol <name>
```

#### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--mysql` | `false` | Enable MySQL protocol |
| `--mysql-port` | `3306` | MySQL listen port |
| `--postgresql` | `false` | Enable PostgreSQL protocol |
| `--postgresql-port` | `5432` | PostgreSQL listen port |

#### Examples

```bash
quicbaas database update-protocol analytics --tenant acme \
  --mysql --mysql-port 3306

quicbaas database update-protocol analytics --tenant acme \
  --postgresql --postgresql-port 5432
```

### database update-raft

Update Raft consensus configuration.

```bash
quicbaas database update-raft <name>
```

#### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--election-rtt` | `10` | Election RTT in number of heartbeat intervals |
| `--heartbeat-rtt` | `1` | Heartbeat RTT in milliseconds |

#### Examples

```bash
quicbaas database update-raft orders --tenant acme \
  --election-rtt 10 --heartbeat-rtt 1
```

### database update-barrier

Update barrier encryption configuration.

```bash
quicbaas database update-barrier <name>
```

#### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--enabled` | | Enable barrier encryption |
| `--disabled` | | Disable barrier encryption |
| `--key-id` | | DEK ID for barrier encryption |

#### Examples

```bash
quicbaas database update-barrier orders --tenant acme --enabled
quicbaas database update-barrier orders --tenant acme --disabled
```

### database update-connection

Update connection pool and timeout settings.

```bash
quicbaas database update-connection <name>
```

#### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--max-connections` | `100` | Maximum concurrent connections |
| `--idle-timeout` | `30s` | Idle connection timeout |

#### Examples

```bash
quicbaas database update-connection orders --tenant acme \
  --max-connections 200 --idle-timeout 60s
```

## Cross-References

- [Database API Reference](../api/database.md)
- [Multi-Tenant Architecture](../architecture/multi-tenant.md)
- [Per-Database Encryption](../security/per-database-encryption.md)
