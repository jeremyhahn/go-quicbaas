# MCP Tool Filtering by Agent Scopes

Agents only see MCP tools they are authorized to use. Unauthorized tools are invisible, not merely denied.

## How MCP Tool Filtering Works

1. Agent presents its OIDC token when connecting to the MCP server.
2. On `tools/list` request, the server extracts scopes from the token claims.
3. Each registered MCP tool maps to a `resource:action` pair.
4. The server iterates all tools, calling `CheckToolAuthorization()` for each.
5. Only tools where the agent's scopes satisfy the authorization check are included in the response.
6. Public tools (e.g., `ping`, `version`) are always visible regardless of scopes.

```
Agent Token (scopes: [kms:read, orchestrator:read])
     |
     v
MCP Server: tools/list
     |
     v
FilterToolsByScopes()
     |-- kms_get_key      -> requires kms:read     -> VISIBLE
     |-- kms_create_key   -> requires kms:write    -> HIDDEN
     |-- orchestrator_health -> requires orchestrator:read -> VISIBLE
     |-- secrets_get      -> requires secrets:read -> HIDDEN
     |-- ping             -> public                -> VISIBLE
     v
Response: [kms_get_key, orchestrator_health, ping]
```

## Tool to Resource:Action Mapping

Each MCP tool registers its required scope when added to the tool registry. Examples:

| MCP Tool | Required Scope | Description |
|----------|---------------|-------------|
| `kms_get_key` | `kms:read` | Read a KMS key |
| `kms_create_key` | `kms:write` | Create a KMS key |
| `kms_rotate_key` | `kms:write` | Rotate a KMS key |
| `secrets_get` | `secrets:read` | Read a secret |
| `secrets_put` | `secrets:write` | Write a secret |
| `orchestrator_health` | `orchestrator:read` | Check orchestrator health |
| `orchestrator_spawn` | `agent:spawn` | Spawn an agent session |
| `orchestrator_prompt` | `agent:prompt` | Prompt an agent |
| `orchestrator_close` | `agent:close` | Close an agent session |

## Security Benefits

**Smaller context window** -- Agents only see tools relevant to their role, reducing prompt size and improving response quality. A reviewer agent with `[orchestrator:read]` sees only read-only tools, not the full tool catalog.

**No prompt injection for unauthorized tools** -- Since unauthorized tools are invisible (not just denied), a malicious prompt cannot reference or attempt to use tools outside the agent's scope. The attack surface is reduced to only the tools the agent can see.

**Principle of least privilege** -- Each agent role receives exactly the permissions it needs. A coder agent does not see admin tools. A reviewer does not see write tools.

**Auditability** -- Every tool invocation is traced back to the agent's ServiceAccount with its specific scopes. The audit trail shows exactly which agent called which tool with what credentials.

## Example: Agent with `["kms:read"]` Scope

An agent provisioned with only `kms:read` scope:

**Sees:**
- `kms_get_key` (requires `kms:read`)
- `kms_list_keys` (requires `kms:read`)
- `ping` (public)
- `version` (public)

**Does not see:**
- `kms_create_key` (requires `kms:write`)
- `kms_rotate_key` (requires `kms:write`)
- `secrets_get` (requires `secrets:read`)
- `orchestrator_spawn` (requires `agent:spawn`)
- All other tools outside `kms:read` scope

The agent's MCP tools/list response contains only the visible tools. From the agent's perspective, the hidden tools do not exist.
