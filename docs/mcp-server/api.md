# MCP Server — API

> **Status: NORMATIVE** · Last verified: 2026-11-23
> The capability on every transport, with permissions. Feature folder: `docs/mcp-server/`.

Model Context Protocol (MCP) interface for AI agent integration.

## Overview

MCP lets AI assistants call QuicBaaS operations as tools, over JSON-RPC 2.0.

- **Streamable HTTP over TLS.** The tools are served by go-qrdb's unified MCP
  server, on the daemon's own MCP listener at `/mcp`. The listener starts only
  when `server.mcp.enabled` is `true`; it listens on `server.mcp.port`, and
  `server.mcp.bind_address` overrides `server.host` for this listener alone.
- **stdio only through the CLI bridge.** An MCP host that spawns a child
  process (Claude Desktop, Claude Code, opencode) runs
  `quicbaas mcp serve --stdio`, which relays each message to the HTTP
  listener. See [`cli.md`](cli.md).
- **Tools only.** The server registers tools. It registers no MCP resources
  and no prompts.

## Authorization

Every request carries an identity: a bearer token in
`Authorization: Bearer <token>`, or a verified mTLS client certificate.

Each `tools/call` is authorized against the tool's qrdb permission
(resource and action). The decision is default deny: a tool with no permission
mapping is refused, and so is a caller that lacks the mapped permission.

**Public tools.** Three tools are public by design and need no permission:

- `qrdb_health` — health check
- `quicbaas_liveness` — liveness probe
- `quicbaas_readiness` — readiness probe

> **Known gap.** The gate in front of the tools is go-qrdb's MCP server, and
> the pinned go-qrdb does not yet behave as the public set requires. With
> authentication on, it answers any request that carries no credential with
> `401` before `initialize`, so the public tools are not reachable
> anonymously. It also has no per-principal `tools/list` filter: an
> authenticated principal is shown every registered tool, and each call is
> still authorized per tool. See
> [what an assistant can and cannot see](user.md#what-an-assistant-can-and-cannot-see).

`quicbaas_login_begin` and `quicbaas_login_finish` are refused on MCP (see
[`engineering.md`](engineering.md#public-tools)); log in over REST or gRPC.

## Architecture

```
AI assistant (MCP host)
       |
       | stdio (quicbaas mcp serve --stdio)   or   Streamable HTTP over TLS
       |
go-qrdb unified MCP server  (/mcp on server.mcp.port)
       |
       | authorization per tool, then dispatch
       |
QuicBaaS tool handlers (pkg/api/mcp) and go-qrdb's own tools
```

## Transports

### HTTP (Streamable HTTP over TLS)

```yaml
server:
  mcp:
    enabled: true
    port: 11443
    # bind_address: 10.0.0.5   # optional; defaults to server.host
```

A session starts with `initialize`; the reply carries an `Mcp-Session-Id`
header that every later request in the session sends back.

```bash
curl -sS -i --cacert server-ca.crt --cert agent.crt --key agent.key \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}' \
  https://quicbaas.example.com:11443/mcp
```

A `404 page not found` means MCP is not served at that address; see
[troubleshooting a 404](cli.md#troubleshooting-a-404).

### stdio (CLI bridge)

```bash
quicbaas mcp serve --stdio --address quicbaas.example.com:11443
```

The bridge forwards `initialize`, `tools/list` and `tools/call` verbatim and
uses the CLI's stored credentials and TLS material. See
[connecting an assistant](user.md#connecting-an-assistant).

## JSON-RPC Protocol

### Request Format

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "quicbaas_readiness",
    "arguments": {}
  }
}
```

`arguments` is a JSON object whose shape is the tool's `inputSchema`, as
`tools/list` returns it (`quicbaas mcp tools` prints it).

### Tool Errors

A tool that fails returns a `tools/call` result with `isError: true`, a
message in its text content, and a numeric code in its structured content:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "isError": true,
    "content": [{"type": "text", "text": "..."}],
    "structuredContent": {"code": -32003}
  }
}
```

## MCP Methods

| Method | Description |
|--------|-------------|
| `initialize` | Open an MCP session and exchange capabilities |
| `ping` | Liveness of the session |
| `tools/list` | List the registered tools |
| `tools/call` | Call one tool |

## Available Tools

The names below are the names a client calls. A name written `prefix_*` is a
family: every served tool whose name starts with that prefix. Each tool's
arguments are in its `inputSchema` from `tools/list`.

### Health

- `qrdb_health` — go-qrdb node health (public)
- `quicbaas_liveness` — liveness probe (public)
- `quicbaas_readiness` — readiness probe (public)

### Data

- `qrdb_put`, `qrdb_get`, `qrdb_delete`, `qrdb_list` — go-qrdb's entity
  store, served by go-qrdb itself
- `quicbaas_kv_*` — key-value get, put, delete and list
- `quicbaas_batch_*` — atomic multi-put, multi-get and multi-delete
- `quicbaas_transaction` — atomic compare-and-swap transaction
- `quicbaas_query` — query through secondary indexes
- `quicbaas_scan_index` — full index scan
- `quicbaas_aggregate` — aggregate over entities
- `quicbaas_cache_*` — cache statistics, configuration and clearing
- `qrdb_ts_*` — time series write, query and retention, served by go-qrdb
- `qrdb_vector_*` — vector namespaces, upsert and similarity search, served
  by go-qrdb
- `quicbaas_vector_*` — vector search, statistics, health and administration
- `quicbaas_object_*` — object storage, backends, policies, quotas and
  administration
- `quicbaas_parameter_*` — parameter store
- `quicbaas_msg_*` — messaging channels, publish and subscribe

### Cluster and operations

- `quicbaas_cluster_status` — cluster status
- `quicbaas_get_leader` — shard leader
- `quicbaas_add_node`, `quicbaas_remove_node`, `quicbaas_replace_node`,
  `quicbaas_upgrade_node` — shard membership and rolling upgrade
- `quicbaas_create_shard` — create a shard
- `quicbaas_rebalance_cluster`, `quicbaas_rebalance_status` — rebalance and
  its progress
- `quicbaas_migrate_data`, `quicbaas_migration_progress` — data migration and
  its progress
- `quicbaas_backup_*` — create, list, restore, verify and delete backups
- `quicbaas_fleet_*` — fleet nodes and placement declarations
- `quicbaas_scheduler_*` — scheduler nodes, jobs and placements
- `quicbaas_registry_*` — service registry: register, renew, resolve, watch

### Identity, sessions and access

- `quicbaas_logout` — end the caller's session
- `quicbaas_login_begin`, `quicbaas_login_finish` — registered, but refused on
  MCP (see [Authorization](#authorization))
- `quicbaas_introspecttoken` — token introspection
- `quicbaas_iam_*` — IAM: users, roles, policies, tenants, apps, sessions,
  federation, identities, impersonation, audit, GDPR, WebAuthn credentials
  and admin assignments
- `quicbaas_mfa_*` — MFA enrolment, challenges and backup codes
- `quicbaas_pep_*` — policy enforcement point checks
- `quicbaas_role_revoke` — revoke a role from a user
- `qrdb_role_*` — go-qrdb roles: create, delete, list, assign
- `qrdb_user_*` — go-qrdb user lookups and user authentication factors
- `qrdb_tenant_*` — tenant barrier, quotas and entitlements
- `qrdb_credential_*` — service credentials: submit, rotate, versions

### Barrier and ceremonies

- `qrdb_barrier_*` — barrier status, seal, unseal, Shamir shares, rekey and
  recovery, served by go-qrdb
- `qrdb_ceremony_*` — key ceremonies and officer enrolment
- `quicbaas_ceremony_list_officers` — list ceremony officers

### Keys, secrets and certificates

- `quicbaas_kms_*` — KMS keys, policies, rotation, safety and backends
- `quicbaas_dek_*` — data encryption keys and re-encryption
- `quicbaas_crypto_*` — encrypt, decrypt, sign, verify, hash, HMAC, random and
  data keys
- `quicbaas_secrets_*` — secrets store, versions, rotation and watch
- `quicbaas_dynamic_secrets_*` — dynamic secret roles and leases
- `quicbaas_ssh_*` — SSH keys, authorized keys and known hosts
- `quicbaas_certificate_*` — certificate issuance, signing, revocation and
  verification
- `quicbaas_trust_*` — trust store: AIA import, cross-signed roots, DANE
- `quicbaas_attestation_*` — attestation, endorsements and reference values
- `xkms_ca_*` — the embedded CA: bundle, certificate, sign CSR, issue,
  revoke, CRL and revocation check

The other xkms key, PIV, PIN and FIDO2 tools are go-xkms's own MCP surface,
served by go-xkms's module and documented there:
[Key Management — API](../../../go-xkms/docs/key-management/api.md).

### AI and orchestration

- `quicbaas_ollama_*` — Ollama health, models and embeddings
- `quicbaas_embedding_*` — embedding providers, generation and cache
- `quicbaas_rag_*` — RAG query, context building and indexing (see
  [RAG](../rag/api.md))
- `quicbaas_orchestrator_*` — orchestrator jobs, workflows, sessions, tasks,
  runners, backends and cost
- `quicbaas_task_*` — task artifacts
- `quicbaas_agents_*` — agent definitions
- `quicbaas_agent_account_*` — agent accounts and their sessions
- `quicbaas_agent_pools_*` — agent pools
- `prompt_get`, `prompt_put`, `prompt_list`, `prompt_history`, `prompt_diff`,
  `prompt_effective`, `prompt_reset`, `prompt_rollback` — the prompt store

### Projects and SDLC

- `project_create`, `project_get`, `project_get_by_name`, `project_list`,
  `project_update`, `project_delete`, `project_grant_fleet_node`,
  `project_revoke_fleet_node` — projects and their fleet nodes
- `sdlc_forges`, `sdlc_bind`, `sdlc_unbind`, `sdlc_binding`,
  `sdlc_onboard`, `sdlc_project_onboard` — forge bindings and onboarding
- `sdlc_issue_create`, `sdlc_adopt_issue`, `sdlc_open_work`,
  `sdlc_delivery`, `sdlc_conformance`, `sdlc_collaborators` — issues,
  work and delivery
- `sdlc_reviewer`, `sdlc_reviewer_set`, `sdlc_reviewer_clear` — the
  reviewer setting
- `quicbaas_sdlc_*` — the SDLC worker image

## Examples

### Initialize

Request:
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "2024-11-05",
    "capabilities": {},
    "clientInfo": {"name": "example", "version": "0"}
  }
}
```

The reply's `capabilities` advertises `tools`.

### Readiness

```bash
curl -sS --cacert server-ca.crt --cert agent.crt --key agent.key \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H "Mcp-Session-Id: $SESSION" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"quicbaas_readiness","arguments":{}}}' \
  https://quicbaas.example.com:11443/mcp
```

Or through the CLI, which opens the session itself:

```bash
quicbaas mcp call quicbaas_readiness
```

## Error Codes

### Standard JSON-RPC Errors

| Code | Name | Description |
|------|------|-------------|
| -32700 | Parse Error | Invalid JSON |
| -32600 | Invalid Request | Invalid JSON-RPC format |
| -32601 | Method Not Found | Unknown method |
| -32602 | Invalid Params | Invalid parameters |
| -32603 | Internal Error | Server error; also the tool error code for a failure with no closer class |

### Tool Error Codes

A failed tool call carries one of these in `structuredContent.code`:

| Code | Name | Meaning |
|------|------|---------|
| -32001 | AuthenticationRequired | The call has no authenticated caller |
| -32003 | PermissionDenied | Forbidden, cross-tenant or system-keyspace access |
| -32004 | ResourceNotFound | The target does not exist |
| -32005 | ResourceConflict | Conflicting state |
| -32006 | ServiceUnavailable | The backing service is not available (not wired, no leader, quorum lost, storage) |
| -32007 | RequestTimeout | Timeout or deadline exceeded |
| -32008 | ResourceExhausted | Quota or rate limit |
| -32009 | PreconditionFailed | A precondition does not hold |
| -32010 | OperationNotAllowed | The operation is not allowed in this state |
| -32011 | OperationCanceled | The call was canceled |
| -32012 | InvalidInput | Invalid or missing arguments, tenant or user context |
| -32013 | AlreadyExists | The target already exists |
| -32014 | Unimplemented | The operation is not implemented |

A request with no valid credential is refused with HTTP `401` before any
JSON-RPC processing (see [Authorization](#authorization)).

## Security

- Bearer tokens or mTLS client certificates identify the caller; see
  [Authorization](#authorization).
- Every tool call is authorized per tool against its qrdb permission, default
  deny.
- The listener serves TLS only; use `https://` URLs.

## See Also

- [CLI](cli.md) — `quicbaas mcp serve --stdio`, `tools`, `call`
- [User guide](user.md) — connecting an assistant and what it can see
- [Engineering](engineering.md) — registering tools, public tools, transport
- [Architecture](../multiprotocol-api/architecture.md) — system design
- [Model Context Protocol Specification](https://modelcontextprotocol.io/)
