# Multi-Host Coordination

## Overview

QuicBaaS supports three deployment modes that scale from a single laptop to a multi-datacenter Raft cluster. Agents (Claude Code, Codex, Ollama) do not need to run on QuicBaaS servers -- they connect over the network.

## Mode 1: Standalone (Single Machine)

A single QuicBaaS instance with PebbleDB storage coordinates all agents on the same machine. No cluster or network required.

```
+-- Laptop / Workstation ------------------------------------------+
|                                                                   |
|  Claude Code (lead + workers)                                     |
|       |                                                           |
|       +-- Hooks -> HTTP -> QuicBaaS (standalone, PebbleDB)        |
|       +-- MCP Tools -> QuicBaaS (RAG, KV, secrets)                |
|       +-- Channel <- QuicBaaS pushes events                       |
|                                                                   |
|  Ollama (local GPU or CPU)                                        |
+-------------------------------------------------------------------+
```

Start with:

```bash
quicbaasd start
```

All communication is localhost. Hooks target `http://localhost:9090/api/v1/hooks/*`. The channel server runs in-process with no `--address` flag.

## Mode 2: Remote Agents

Agents run on machines without QuicBaaS installed. They connect to a remote QuicBaaS server over the network.

```
+-- Agent Machine (Mac) ---+     +-- Agent Machine (x86) --+
|                           |     |                          |
|  Claude Code              |     |  Claude Code             |
|    Hooks -> HTTP ---------|--+  |    Hooks -> HTTP --------|--+
|    MCP -> gRPC/QUIC ------|--+  |    MCP -> gRPC/QUIC -----|--+
|    Channel <- ------------|--+  |    Channel <- -----------|--+
|                           |  |  |                          |  |
|  (no QuicBaaS installed)  |  |  |  (no QuicBaaS installed) |  |
+---------------------------+  |  +--------------------------+  |
                               v                                v
         +--------------------------------------------------+
         |  QuicBaaS Server (standalone or cluster)          |
         |  KV, Messaging, RAG, Secrets, Task Store          |
         +--------------------------------------------------+
```

### Hook Configuration (Remote)

Point hook URLs to the remote QuicBaaS server:

```json
{
  "hooks": {
    "TaskCreated": [
      {
        "hooks": [
          {
            "type": "http",
            "url": "https://quicbaas-server.internal:9090/api/v1/hooks/task-created"
          }
        ]
      }
    ]
  }
}
```

### Channel Configuration (Remote Proxy)

The channel server runs locally on the agent machine as a stdio MCP process but connects to the remote QuicBaaS server for event sourcing:

```
Claude Code <--stdio--> ChannelServer <--gRPC/QUIC--> Remote QuicBaaS
```

```json
{
  "mcpServers": {
    "quicbaas-channel": {
      "command": "quicbaas",
      "args": [
        "channel", "serve",
        "--address", "quicbaas-server.internal:9090",
        "--enable-reply"
      ],
      "type": "stdio"
    }
  }
}
```

### MCP Tools (Remote)

The MCP tool server also supports remote mode, connecting to QuicBaaS over gRPC/QUIC:

```json
{
  "mcpServers": {
    "quicbaas": {
      "command": "quicbaas",
      "args": ["mcp", "serve", "--address", "quicbaas-server.internal:9090"],
      "type": "stdio"
    }
  }
}
```

## Mode 3: Multi-Node Raft Cluster

Full distributed mode with Raft consensus across multiple QuicBaaS nodes. Agents connect to any node.

```
  Agent Machine 1          Agent Machine 2          Agent Machine N
  +------------+           +------------+           +------------+
  |Claude Code |           |Claude Code |           |Claude Code |
  | Hooks->HTTP|           | Hooks->HTTP|           | Hooks->HTTP|
  | MCP->gRPC  |           | MCP->QUIC  |           | MCP->REST  |
  | Channel<-  |           | Channel<-  |           | Channel<-  |
  +-----+------+           +-----+------+           +-----+------+
        |                        |                        |
        v                        v                        v
  +----------+  Raft  +----------+  Raft  +----------+
  |QuicBaaS  |<------>|QuicBaaS  |<------>|QuicBaaS  |
  |Node 1    |        |Node 2    |        |Node 3    |
  +----------+        +----------+        +----------+
```

### Raft Guarantees

- **Task store** is replicated via Raft consensus. A task created on Node 1 is readable from Node 2 after commit.
- **Messaging** channels are cluster-wide. Events published on any node reach all subscribers.
- **RAG vectors** are stored in the replicated KV layer.
- **Session metadata** (active sessions, health state) is replicated for failover.

### Network Partition Handling

During a network partition:

- Nodes in the majority partition continue serving reads and writes normally.
- Nodes in the minority partition reject writes and serve stale reads (if configured for read-only degradation).
- Hooks targeting a minority-partition node receive 503 responses. Claude Code retries or the agent's hook configuration should include multiple QuicBaaS endpoints.
- When the partition heals, Raft reconciles state automatically.

## Cross-Host Data Flow

A task created on Host A and executed on Host B:

1. **Host A**: Claude Code fires `TaskCreated` hook to QuicBaaS Node 1.
2. **Node 1**: Stores task in Raft-replicated task store (status: `pending`). Publishes event to messaging channel `orchestrator:tasks:<team>`.
3. **Node 2**: Messaging subscriber receives the event. Channel server on Host B pushes the event into Claude Code's session.
4. **Host B**: Claude Code agent picks up the task, begins work.
5. **Host B**: Claude Code fires `TaskCompleted` hook to QuicBaaS Node 2.
6. **Node 2**: Marks task `completed` in the replicated store. Publishes completion event.
7. **Host A**: Lead agent receives completion notification through its channel server.

## Shared RAG Across Hosts

RAG vectors are stored in the replicated KV/vector store. Any agent on any host can:

- Index files via `quicbaas_rag_index_project` -- vectors replicate across nodes.
- Query context via RAG-enabled prompts -- reads from the local node's replica.

This means an agent on Host B benefits from codebase indexing performed by an agent on Host A without re-indexing.

## Deployment Recommendations

| Scale | Mode | Nodes | Notes |
|---|---|---|---|
| Solo developer | Standalone | 1 | No network, minimal config |
| Small team, mixed hardware | Remote agents | 1 | Single QuicBaaS server, agents on any machine |
| Production, HA | Raft cluster | 3+ | Odd number of nodes for quorum |
| Multi-datacenter | Raft cluster | 5+ | Cross-region replication, latency-aware routing |
