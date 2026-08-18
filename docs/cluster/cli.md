# Cluster — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag, with runnable examples. Feature folder: `docs/cluster/`.

Cluster management commands for monitoring QuicBaaS cluster nodes.

## Commands

### status

Show cluster status and node information.

```bash
quicbaas cluster status
```

#### Flags
- `--address string`: QuicBaaS server address (default: "localhost:50051")
- `--timeout duration`: Request timeout (default: 10s)
- `--output string`: Output format (json, table) (default: "table")

#### Examples

```bash
# Show cluster status
quicbaas cluster status

# Show cluster status for specific server
quicbaas cluster status --address localhost:50051

# Show status as JSON
quicbaas cluster status --output json

# Show status with custom timeout
quicbaas cluster status --timeout 30s
```

## Output

The cluster status command displays:
- **Cluster Health**: Overall cluster health status
- **Leader ID**: Current Raft leader node ID
- **Total Nodes**: Total number of nodes in the cluster
- **Ready Nodes**: Number of nodes ready to serve traffic
- **Node Details**: Individual node information including:
  - Node ID
  - Address
  - Role (Leader/Follower)
  - Health status
  - Last seen timestamp
- **Raft Groups**: Information about Raft groups if available

## Notes

- Cluster status is retrieved from the server's Raft implementation
- Leader election is handled automatically by the Raft protocol
- Nodes are considered healthy if they respond to heartbeat messages
- Use this command to monitor cluster health and troubleshoot issues


---

## quicbaas node

Node management operations for cluster nodes.

### Overview

Commands for managing cluster nodes with automatic data evacuation and health checking.

### Commands

#### add

Add a new node to the cluster.

```bash
quicbaas node add <node-id> <address>
```

##### Flags
- `--health-check-interval duration`: Health check interval (default: 30s)
- `--health-check-timeout duration`: Health check timeout (default: 5s)
- `--output string`: Output format (json, table) (default: "table")

##### Examples

```bash
# Add a new node
quicbaas node add node-4 10.0.0.4:50051

# Add node with custom health check settings
quicbaas node add node-4 10.0.0.4:50051 \
  --health-check-interval 30s \
  --health-check-timeout 10s
```

#### remove

Remove a node from the cluster safely with data evacuation.

```bash
quicbaas node remove <node-id>
```

##### Flags
- `--evacuation-timeout duration`: Evacuation timeout (default: 30m)
- `--verify-data`: Verify data after evacuation (default: true)
- `--output string`: Output format (json, table) (default: "table")

##### Examples

```bash
# Remove a node
quicbaas node remove node-3

# Remove node without data verification
quicbaas node remove node-3 --verify-data=false

# Remove node with custom evacuation timeout
quicbaas node remove node-3 --evacuation-timeout 60m
```

#### replace

Replace a failed or degraded node with a new node.

```bash
quicbaas node replace <old-id> <new-id> <address>
```

##### Flags
- `--evacuation-timeout duration`: Evacuation timeout (default: 30m)
- `--verify-data`: Verify data after evacuation (default: true)
- `--auto-replace`: Enable automatic replacement on future failures
- `--output string`: Output format (json, table) (default: "table")

##### Examples

```bash
# Replace a failed node
quicbaas node replace node-2 node-5 10.0.0.5:50051

# Replace node with custom settings
quicbaas node replace node-2 node-5 10.0.0.5:50051 \
  --verify-data \
  --evacuation-timeout 45m

# Replace node with auto-replacement enabled
quicbaas node replace node-2 node-5 10.0.0.5:50051 \
  --auto-replace
```

#### list

List all nodes in the cluster with their current status.

```bash
quicbaas node list
```

##### Flags
- `--output string`: Output format (json, table) (default: "table")

##### Examples

```bash
# List all nodes
quicbaas node list

# List nodes with JSON output
quicbaas node list --output json
```

### Node Replacement Process

1. **Add New Node**: New node joins the cluster
2. **Health Check**: Wait for new node to become healthy
3. **Data Evacuation**: Move data from old node (if accessible)
4. **Verification**: Verify data integrity (if enabled)
5. **Remove Old Node**: Remove old node from cluster
6. **Completion**: Generate replacement report

### Node States

- **Healthy**: Node is operational and responding to health checks
- **Degraded**: Node is experiencing issues but still functional
- **Failed**: Node is not responding to health checks
- **Evacuating**: Node is being drained of data
- **Removed**: Node has been removed from the cluster

### Notes

- Node removal is graceful and ensures no data loss
- Data evacuation moves all data to other healthy nodes
- Health checks verify node responsiveness
- Failed nodes can be replaced without data loss
- Node addresses should be in format: `host:port`
- Auto-replacement can automatically replace failed nodes


---

## quicbaas rebalance

Cluster rebalancing operations for QuicBaaS.

### Overview

Commands for managing cluster data rebalancing to maintain balanced data distribution across cluster nodes by moving data from overloaded shards to underutilized ones.

### Commands

#### start

Start rebalancing data across cluster shards.

```bash
quicbaas rebalance start
```

##### Flags
- `--server string`: QuicBaaS server address (default: "localhost:50051")
- `--timeout duration`: Request timeout (default: 30s)
- `--target-imbalance float`: Target imbalance ratio (0.1 = 10%) (default: 0.1)
- `--max-concurrent int`: Max simultaneous transfers (default: 2)
- `--min-keys int64`: Minimum keys per shard (default: 1000)
- `--dry-run`: Plan without executing
- `--rate-limit int`: Rate limit in KB/s (default: 10240)
- `--output string`: Output format (json, table) (default: "table")
- `--follow`: Follow rebalance progress

##### Examples

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

#### status

Check the progress of an ongoing rebalance operation.

```bash
quicbaas rebalance status --operation <operation-id>
```

##### Flags
- `--server string`: QuicBaaS server address (default: "localhost:50051")
- `--timeout duration`: Request timeout (default: 10s)
- `--operation string`: Operation ID to check (required)
- `--output string`: Output format (json, table) (default: "table")

##### Examples

```bash
# Check rebalance status for a specific operation
quicbaas rebalance status --operation op-123

# Check rebalance status with JSON output
quicbaas rebalance status --operation op-123 --output json
```

#### plan

Show the rebalance plan without executing it (dry-run).

```bash
quicbaas rebalance plan
```

##### Flags
- `--server string`: QuicBaaS server address (default: "localhost:50051")
- `--timeout duration`: Request timeout (default: 10s)
- `--target-imbalance float`: Target imbalance ratio (0.1 = 10%) (default: 0.1)
- `--output string`: Output format (json, table) (default: "table")

##### Examples

```bash
# Show rebalance plan
quicbaas rebalance plan

# Show plan with JSON output
quicbaas rebalance plan --output json

# Show plan with custom target imbalance
quicbaas rebalance plan --target-imbalance 0.05
```

### Rebalance Process

1. **Analysis**: Analyze current data distribution
2. **Planning**: Create rebalance plan to achieve target imbalance
3. **Execution**: Move data from overloaded to underutilized shards
4. **Verification**: Verify data integrity after moves
5. **Completion**: Generate rebalance report

### Configuration

#### Target Imbalance
- **0.1 (10%)**: Allow up to 10% deviation from average (default)
- **0.05 (5%)**: Stricter balance, more rebalancing
- **0.15 (15%)**: More relaxed balance, less rebalancing

#### Max Concurrent
- Number of simultaneous shard transfers
- Lower values reduce cluster load
- Higher values speed up rebalancing

#### Min Keys Per Shard
- Minimum keys before triggering rebalance
- Prevents rebalancing small shards
- Default: 1000 keys

#### Rate Limit
- Bandwidth limit in KB/s
- Controls network usage
- Default: 10 MB/s (10240 KB/s)

### Plan Output

The rebalance plan includes:
- Source and target shards for each move
- Number of keys and bytes to move
- Estimated time for each move
- Overall estimated completion time
- Current and projected imbalance ratios

### Notes

- Rebalancing is non-blocking and can run in the background
- Dry-run mode shows what would be done without executing
- Follow mode displays real-time progress updates
- Rebalancing improves query performance by distributing load evenly
- Imbalance is calculated as: max(shard_size) / avg(shard_size) - 1
