# MCP Preconfiguration for Spawned Agents

A spawned agent (ACP backend — claude / codex / opencode — or a remote
SSH-deployed daemon) can be handed the QuicBaaS MCP endpoint at spawn time so it
has native access to platform tools (KV, inference, …) over the Model Context
Protocol. This document describes how that configuration is delivered and how
the agent authenticates.

## Authentication: the agent's own mTLS certificate

Unlike the workflow-assigned service-account flow (see
[agent-identity.md](agent-identity.md)), which issues a short-lived OIDC token,
a preconfigured MCP agent authenticates with the **long-lived mTLS certificate
it already holds**:

- Embedded and SSH-deployed agents receive a 365-day client certificate from the
  platform CA with the subject `CN=agent-<name>, O=<tenantID>, OU=agents`
  (`pkg/service/agent_cert_issuer.go`).
- The MCP transport recognizes that certificate as an agent identity and admits
  it with a **least-privilege, per-tenant** scope set — no bearer token, and
  therefore nothing to refresh mid-session.

This deliberately avoids the token-renewal problem: a generic ACP agent has no
way to refresh a 15-minute platform token, so a token-based identity would
strand a long-lived agent the moment its refresh token lapsed. Certificate
renewal is a yearly cert-rotation concern handled by the issuing infrastructure,
invisible to the agent.

### What an agent identity may do

A certificate-authenticated agent is granted exactly
`orchestration.AgentMCPScopes()`:

| Scope | Tool examples | Purpose |
|-------|---------------|---------|
| `orchestrator:inference` | `quicbaas_orchestrator_inference` | sessionless model inference |
| `kv:get` / `kv:put` / `kv:list` | `qrdb_get` / `qrdb_put` / `qrdb_list` | tenant-scoped key-value workspace |

The set is intentionally minimal. An agent **cannot** reach secrets, KMS, IAM,
the barrier, the cluster control plane, or orchestrator control-plane verbs
(spawn / close / job control) over MCP — an agent must not be able to administer
the platform that runs it. Tenant isolation is enforced from the certificate's
`O=` field via row-level security; an agent only ever sees its own tenant's data.

The decision path is in `pkg/api/mcp/authz_agent_cert.go`. It is fail-closed: a
verified client certificate without the `agents` OU is **not** granted agent
scopes (it falls through to the normal bearer-token requirement), a certificate
without a tenant or common name is denied, and a presented bearer token always
takes precedence over the certificate.

## Delivery: the spawn environment

The selected MCP servers are delivered to the agent through its **process
environment**, not the ACP `session/new` `mcpServers` parameter (the
claude / codex / opencode agents do not share a single `mcpServers` JSON shape,
but they all inherit the spawned process environment).

At spawn, `ACPBackend.Spawn` calls `injectMCPServersEnv`
(`pkg/orchestration/acp_mcp_env.go`), which serializes the request's MCP servers
into a single self-describing environment variable:

```
QUICBAAS_MCP_SERVERS=[{"name":"quicbaas","url":"https://…/mcp","env":{…}}]
```

The value is a JSON array of `MCPServerConfig` objects. Each server's `env` map
carries connection material — for the QuicBaaS endpoint preset, the agent's mTLS
cert and key file paths. The variable is set once at the `Spawn` entry so it
reaches the initial subprocess **and** is persisted on the session for the
prompt-time relaunch. Sessions that do not opt in carry no extra environment,
and an operator-supplied `QUICBAAS_MCP_SERVERS` value is never silently
overwritten.

## Configuring it from the UI / SDK

The `SpawnSessionRequest` carries an optional `mcp_servers` field
(`MCPServerConfig[]`) across both SDKs (TypeScript and Go) and the REST surface:

```ts
await client.orchestrator.spawnSession({
  tenantID: 'system',
  backend: 'claude',
  mcp_servers: [{ name: 'quicbaas', url: '' }],
});
```

In the web UI, the **Spawn Session** dialog exposes an opt-in *"Attach QuicBaaS
MCP endpoint"* checkbox. When selected it sends `mcp_servers: [{ name:
'quicbaas', url: '' }]`. The empty `url` is a signal that the daemon resolves
its own MCP endpoint — the browser never needs to know the operator's MCP host
or port.

## `MCPServerConfig` shape

```
{
  "name":    "quicbaas",          // server identifier
  "url":     "https://…/mcp",     // HTTP MCP endpoint (empty = QuicBaaS preset)
  "command": "",                  // stdio server executable (alternative to url)
  "args":    [],                  // stdio server arguments
  "env":     { "KEY": "value" }   // connection material (e.g. cert/key paths)
}
```

`env` round-trips losslessly through the proto bridge (`map<string,string> env`)
and the SDK, so credential material configured at spawn reaches the agent intact.

## Scope of the current implementation

Wired and tested end-to-end:

- Certificate-identity MCP authorization with the least-privilege scope set.
- `mcp_servers` across the REST + SDK surfaces and the Spawn dialog.
- Environment delivery of the configured servers to the agent process.

Follow-ups (tracked separately):

- Daemon-side resolution of the empty-`url` `quicbaas` preset to the configured
  `Server.MCP` endpoint URL plus injection of the agent's actual cert/key file
  paths. (Embedded agents currently hold their key in memory only; persisting a
  disk certificate for the embedded case is an open decision.)
- MCP preconfiguration for the SSH-deployed **daemon** (as opposed to a single
  spawned session), which configures the deployed daemon's MCP client.
