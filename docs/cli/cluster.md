# quicbaas cluster

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
