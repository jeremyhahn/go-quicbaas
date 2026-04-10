# Agent Identity and IAM Integration

Orchestration agents are first-class IAM security principals. Each agent receives a dedicated service account with scoped permissions, ensuring fine-grained access control and auditability.

## How Agents Get IAM Service Accounts

When the orchestrator assigns work to an agent, the identity provisioning flow is:

1. **Provision** -- The orchestrator calls `provisionAgentIdentity()` which creates an IAM ServiceAccount via `OrchestratorAuthService.CreateServiceAccount()`.
2. **Scope assignment** -- The service account receives scopes defined in the workflow YAML `mcp_scopes` field for the agent's role.
3. **Token issuance** -- A short-lived OIDC token is issued via the `client_credentials` grant for the agent's service account.
4. **Session binding** -- The token is injected into the agent's session context, used for all MCP tool calls.
5. **Cleanup** -- On job completion, cancellation, or failure, `revokeAgentIdentity()` revokes the token and deletes the service account.

## OIDC Token Lifecycle

| Phase | Action |
|-------|--------|
| Initialization | ServiceAccount created with workflow-defined scopes |
| Token issuance | Short-lived OIDC token issued via client_credentials grant |
| Active use | Token presented on every MCP tool call for authorization |
| Refresh | Tokens are short-lived; re-issued as needed during long jobs |
| Cleanup | Token revoked, ServiceAccount deleted on job termination |

Tokens contain standard OIDC claims plus custom scope claims that the MCP authorization layer uses for tool filtering.

## Scope-Based MCP Permissions

Each agent role specifies `mcp_scopes` in the workflow YAML. These scopes follow the `resource:action` format:

```yaml
mcp_scopes: [orchestrator:read, kms:read, secrets:read]
```

The MCP server filters the tools/list response based on the agent's scopes. An agent only sees tools it is authorized to use. See [MCP Permissions](mcp-permissions.md) for details.

## Permission Matrix by Role

| Role | Typical Scopes | Purpose |
|------|---------------|---------|
| lead | `orchestrator:read`, `orchestrator:write`, `agent:spawn`, `agent:prompt`, `agent:close` | Full orchestration control, manages other agents |
| coder | `kms:read`, `secrets:read`, `orchestrator:read` | Read secrets and keys for development, read job status |
| reviewer | `orchestrator:read` | Read-only access to review task status and artifacts |

## Defining Custom Scopes in Workflow YAML

Scopes are defined per agent role in the workflow definition:

```yaml
agents:
  - role: lead
    backend: claude
    mcp_scopes:
      - orchestrator:read
      - orchestrator:write
      - agent:spawn
      - agent:prompt
      - agent:close
  - role: coder
    backend: claude
    mcp_scopes:
      - kms:read
      - secrets:read
      - orchestrator:read
  - role: auditor
    backend: ollama
    mcp_scopes:
      - orchestrator:read
      - audit:read
```

The `resource:action` format accepts any valid resource and action combination. The IAM system validates scopes against the registered resource types during service account creation.
