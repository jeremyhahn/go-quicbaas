# Task Tracking and Decomposition

The orchestrator decomposes high-level tasks into discrete work items using an AI agent backend. This enables multi-project planning where a complex objective is broken into trackable, dependency-aware units of work.

## Task Decomposition

`quicbaas_agent_decompose` sends a structured prompt to any registered agent backend, asking it to break a task into work items.

### Request

```json
{
  "backend": "claude",
  "task": "Add WebSocket support to the API gateway and update all client SDKs",
  "model": "claude-sonnet-4-20250514",
  "project_ids": ["proj-abc", "proj-def"],
  "use_rag": true
}
```

Required fields: `backend`, `task`. The optional `project_ids` array scopes the decomposition to specific registered projects, and `use_rag` enables RAG context enrichment.

### How It Works

1. `DecomposeTask` validates the request and looks up the backend
2. `buildDecompositionPrompt` constructs a structured prompt that includes the task description and target project IDs
3. The backend receives a system prompt constraining it to respond with valid JSON
4. The response is parsed via `parseWorkItems`, which extracts the first JSON array from the response text
5. If JSON parsing fails, a single "raw" work item is returned containing the unparsed response

## WorkItem Structure

```go
type WorkItem struct {
    ID           string   `json:"id"`
    ProjectID    string   `json:"project_id"`
    Title        string   `json:"title"`
    Description  string   `json:"description"`
    Priority     int      `json:"priority"`
    Dependencies []string `json:"dependencies,omitempty"`
}
```

| Field | Description |
|-------|-------------|
| `id` | Unique identifier for the work item |
| `project_id` | Target project this work item belongs to |
| `title` | Short summary of the work |
| `description` | Detailed description of what needs to be done |
| `priority` | Numeric priority (1 = highest) |
| `dependencies` | IDs of work items that must complete first |

## Example Response

```json
{
  "work_items": [
    {
      "id": "ws-1",
      "project_id": "proj-abc",
      "title": "Implement WebSocket handler",
      "description": "Add WebSocket upgrade support to the API gateway router",
      "priority": 1,
      "dependencies": []
    },
    {
      "id": "ws-2",
      "project_id": "proj-def",
      "title": "Update Go SDK with WebSocket client",
      "description": "Add WebSocket connection and message handling to the Go client SDK",
      "priority": 2,
      "dependencies": ["ws-1"]
    }
  ],
  "timestamp": "2026-03-16T10:00:00.000000000Z"
}
```

## Agent Cache

The agent cache tools allow sessions to store and retrieve expensive analysis results via the KV store, avoiding redundant computation across sessions.

### Tools

| Tool | Description |
|------|-------------|
| `quicbaas_agent_cache_put` | Store a value scoped by project ID and key |
| `quicbaas_agent_cache_get` | Retrieve a cached value by project ID and key |

### Storage

Cache entries are stored as KV entities with entity type `agent_cache`. The composite key is formed as `{project_id}:{key}` and hashed via FNV-1a to produce a deterministic uint64 ID. The KV store provides CAS (compare-and-swap) semantics through versioned writes, ensuring safe concurrent updates.

### Example Usage

```
# Store a dependency analysis result
quicbaas_agent_cache_put(
  project_id: "proj-abc",
  key: "dependency-analysis",
  value: "{\"direct\": 12, \"transitive\": 47, ...}"
)

# Retrieve it in another session
quicbaas_agent_cache_get(
  project_id: "proj-abc",
  key: "dependency-analysis"
)
```

## Build Tags

Task decomposition is always compiled. Agent cache MCP tools require the `mcp` build tag.
