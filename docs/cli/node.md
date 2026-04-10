# quicbaas node

Node management operations for cluster nodes.

## Overview

Commands for managing cluster nodes with automatic data evacuation and health checking.

## Commands

### add

Add a new node to the cluster.

```bash
quicbaas node add <node-id> <address>
```

#### Flags
- `--health-check-interval duration`: Health check interval (default: 30s)
- `--health-check-timeout duration`: Health check timeout (default: 5s)
- `--output string`: Output format (json, table) (default: "table")

#### Examples

```bash
# Add a new node
quicbaas node add node-4 10.0.0.4:50051

# Add node with custom health check settings
quicbaas node add node-4 10.0.0.4:50051 \
  --health-check-interval 30s \
  --health-check-timeout 10s
```

### remove

Remove a node from the cluster safely with data evacuation.

```bash
quicbaas node remove <node-id>
```

#### Flags
- `--evacuation-timeout duration`: Evacuation timeout (default: 30m)
- `--verify-data`: Verify data after evacuation (default: true)
- `--output string`: Output format (json, table) (default: "table")

#### Examples

```bash
# Remove a node
quicbaas node remove node-3

# Remove node without data verification
quicbaas node remove node-3 --verify-data=false

# Remove node with custom evacuation timeout
quicbaas node remove node-3 --evacuation-timeout 60m
```

### replace

Replace a failed or degraded node with a new node.

```bash
quicbaas node replace <old-id> <new-id> <address>
```

#### Flags
- `--evacuation-timeout duration`: Evacuation timeout (default: 30m)
- `--verify-data`: Verify data after evacuation (default: true)
- `--auto-replace`: Enable automatic replacement on future failures
- `--output string`: Output format (json, table) (default: "table")

#### Examples

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

### list

List all nodes in the cluster with their current status.

```bash
quicbaas node list
```

#### Flags
- `--output string`: Output format (json, table) (default: "table")

#### Examples

```bash
# List all nodes
quicbaas node list

# List nodes with JSON output
quicbaas node list --output json
```

## Node Replacement Process

1. **Add New Node**: New node joins the cluster
2. **Health Check**: Wait for new node to become healthy
3. **Data Evacuation**: Move data from old node (if accessible)
4. **Verification**: Verify data integrity (if enabled)
5. **Remove Old Node**: Remove old node from cluster
6. **Completion**: Generate replacement report

## Node States

- **Healthy**: Node is operational and responding to health checks
- **Degraded**: Node is experiencing issues but still functional
- **Failed**: Node is not responding to health checks
- **Evacuating**: Node is being drained of data
- **Removed**: Node has been removed from the cluster

## Notes

- Node removal is graceful and ensures no data loss
- Data evacuation moves all data to other healthy nodes
- Health checks verify node responsiveness
- Failed nodes can be replaced without data loss
- Node addresses should be in format: `host:port`
- Auto-replacement can automatically replace failed nodes
