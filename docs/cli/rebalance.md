# quicbaas rebalance

Cluster rebalancing operations for QuicBaaS.

## Overview

Commands for managing cluster data rebalancing to maintain balanced data distribution across cluster nodes by moving data from overloaded shards to underutilized ones.

## Commands

### start

Start rebalancing data across cluster shards.

```bash
quicbaas rebalance start
```

#### Flags
- `--server string`: QuicBaaS server address (default: "localhost:50051")
- `--timeout duration`: Request timeout (default: 30s)
- `--target-imbalance float`: Target imbalance ratio (0.1 = 10%) (default: 0.1)
- `--max-concurrent int`: Max simultaneous transfers (default: 2)
- `--min-keys int64`: Minimum keys per shard (default: 1000)
- `--dry-run`: Plan without executing
- `--rate-limit int`: Rate limit in KB/s (default: 10240)
- `--output string`: Output format (json, table) (default: "table")
- `--follow`: Follow rebalance progress

#### Examples

```bash
# Start rebalancing with default settings
quicbaas rebalance start

# Start rebalancing with custom imbalance target
quicbaas rebalance start --target-imbalance 0.05

# Dry run to see rebalance plan without executing
quicbaas rebalance start --dry-run

# Rebalance with limited concurrency and bandwidth
quicbaas rebalance start \
  --max-concurrent 1 \
  --rate-limit 5120 \
  --follow
```

### status

Check the progress of an ongoing rebalance operation.

```bash
quicbaas rebalance status --operation <operation-id>
```

#### Flags
- `--server string`: QuicBaaS server address (default: "localhost:50051")
- `--timeout duration`: Request timeout (default: 10s)
- `--operation string`: Operation ID to check (required)
- `--output string`: Output format (json, table) (default: "table")

#### Examples

```bash
# Check rebalance status for a specific operation
quicbaas rebalance status --operation op-123

# Check rebalance status with JSON output
quicbaas rebalance status --operation op-123 --output json
```

### plan

Show the rebalance plan without executing it (dry-run).

```bash
quicbaas rebalance plan
```

#### Flags
- `--server string`: QuicBaaS server address (default: "localhost:50051")
- `--timeout duration`: Request timeout (default: 10s)
- `--target-imbalance float`: Target imbalance ratio (0.1 = 10%) (default: 0.1)
- `--output string`: Output format (json, table) (default: "table")

#### Examples

```bash
# Show rebalance plan
quicbaas rebalance plan

# Show plan with JSON output
quicbaas rebalance plan --output json

# Show plan with custom target imbalance
quicbaas rebalance plan --target-imbalance 0.05
```

## Rebalance Process

1. **Analysis**: Analyze current data distribution
2. **Planning**: Create rebalance plan to achieve target imbalance
3. **Execution**: Move data from overloaded to underutilized shards
4. **Verification**: Verify data integrity after moves
5. **Completion**: Generate rebalance report

## Configuration

### Target Imbalance
- **0.1 (10%)**: Allow up to 10% deviation from average (default)
- **0.05 (5%)**: Stricter balance, more rebalancing
- **0.15 (15%)**: More relaxed balance, less rebalancing

### Max Concurrent
- Number of simultaneous shard transfers
- Lower values reduce cluster load
- Higher values speed up rebalancing

### Min Keys Per Shard
- Minimum keys before triggering rebalance
- Prevents rebalancing small shards
- Default: 1000 keys

### Rate Limit
- Bandwidth limit in KB/s
- Controls network usage
- Default: 10 MB/s (10240 KB/s)

## Plan Output

The rebalance plan includes:
- Source and target shards for each move
- Number of keys and bytes to move
- Estimated time for each move
- Overall estimated completion time
- Current and projected imbalance ratios

## Notes

- Rebalancing is non-blocking and can run in the background
- Dry-run mode shows what would be done without executing
- Follow mode displays real-time progress updates
- Rebalancing improves query performance by distributing load evenly
- Imbalance is calculated as: max(shard_size) / avg(shard_size) - 1
