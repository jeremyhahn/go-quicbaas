# MCP Server — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design and boundaries of the Model Context Protocol surface. Feature folder:
> `docs/mcp-server/`.

## The problem

An AI assistant reaching a backend needs a tool list it can enumerate, argument
schemas it can fill, and an authorization story that does not amount to handing
it an admin token. MCP is the protocol for that; this feature is its
implementation over the composed stack.

The failure it prevents is the obvious one: an assistant given a generic HTTP
client and a bearer token has the whole API and no schema, and every call it
makes is a guess.

## The shape chosen

**One tool per capability, registered in a server-side registry, named from an
explicit table.**

`pkg/api/mcp/` holds the registry. `ToolRegistry.addTool` takes a
`ToolDefinition` carrying a `Name`, a `Description` and a JSON-Schema
`InputSchema` — `pkg/api/mcp/scheduler_tools.go:13` is the shape to read first
because it is small. **459 tools are registered** across the subsystem files.

Tool names are resolved by the SDK through `ToolName`
(`sdk/go/transport/mcp/transport.go:882`), which is deliberately **not** a pure
derivation from `service` + `method`. It consults explicit lookup tables first,
for IAM, orchestrator sessions, pooled accounts, SSH, dynamic secrets, PIV,
FIDO2 and projects, because those subsystems' tool names are named after the
operation rather than the RPC — `quicbaas_orchestrator_prompt`, not
`..._prompt_session`; `ssh_key_generate`, not `..._generate_key`. Only after
those tables miss does it fall back to
`quicbaas_<service>_<snake_case_method>`.

The function's doc comment states why it is exported: it is "the single source
of truth for SDK-side tool naming", so the server package can assert at build
time that every name the SDK can emit is registered. That assertion is the
mechanism that keeps 459 registrations and one client in agreement.

### Alternatives rejected

- **Derive every tool name mechanically.** Rejected because the derivation
  produces names that read backwards for resource-shaped operations, and
  because it would have forced RPC names to be chosen for how they render as
  tools.
- **Expose one generic `call_api` tool.** Rejected: it gives the model no
  schema, which is the entire value of the protocol.
- **Proxy MCP to REST.** Rejected; the tool set is not the route set. Several
  tools compose more than one call, and several routes have no business being
  a tool.

## Authorization

MCP authorization is OAuth 2.1 with mandatory PKCE (S256), Resource Indicators
(RFC 8707) and Dynamic Client Registration (RFC 7591) — implemented per the MCP
authorization specification. The flow and its endpoints belong to
[`../oauth2-oidc/`](../oauth2-oidc/api.md); this folder covers the tool surface
that sits behind it.

Two properties worth stating here:

- **A small set of tools is public.** `sdk/go/transport/mcp/transport.go:691`
  names them: `quicbaas_login`, `quicbaas_login_begin`,
  `quicbaas_login_finish`, `qrdb_health`, `quicbaas_liveness`,
  `quicbaas_readiness`. Everything else calls `ensureAuthenticated` first.
  Login must be public or there is no way in; health must be public or a probe
  needs a token during exactly the states it exists to report.
- **Tools are scope-filtered per principal.** An agent is an IAM principal, and
  its `tools/list` reflects what it may call rather than the full 459. That is
  what makes handing an assistant an MCP endpoint different from handing it a
  token.

## Component boundaries

- **`pkg/api/mcp/`** — the server: registry, tool definitions, authorization
  (`authz.go`, `authz_qrdb.go`), the sealed-window subset (`sealed_window.go`).
- **`sdk/go/transport/mcp/`** — the client transport: `ToolName`, the
  per-subsystem request/response transformers, the JSON-RPC envelope.
- **`cmd/quicbaas/mcp.go`** — the CLI: `serve` (a stdio bridge), `tools`
  (discovery) and `call` (invocation).
- **Every other feature** — owns its own tools. `messaging_tools.go`,
  `scheduler_tools.go` and their siblings live in `pkg/api/mcp/` for
  registration, but the capability is the feature's. Each feature's `api.md`
  lists its registered tool names.

## Data flow

```
MCP client (assistant, or `quicbaas mcp call`)
  │  JSON-RPC over Streamable HTTP
  ▼
pkg/api/mcp  ── tools/list ──► scope-filtered registry view
  │
  ├─ public tool?  ──yes──► handler
  │        │no
  │        ▼
  │   OAuth 2.1 bearer validation, audience-checked
  ▼
handler ──► the same service layer REST and gRPC reach
```

The last line is the important one: MCP is a *third door into one service
layer*, not a parallel implementation. A capability that behaves differently
over MCP than over REST is a defect.

## Trust boundaries

- **The public tool set is an unauthenticated surface.** It is deliberately six
  tools and should stay that way; every addition is an access-control decision.
- **Tool descriptions and schemas are read by a model.** They are, in effect,
  instructions to an agent. Treat their wording as part of the security surface.
- **Scope filtering is the containment mechanism.** If `tools/list` is not
  filtered for a principal, that principal has been told about capabilities it
  should not know exist.

## What this feature deliberately does not do

- **It does not define capabilities.** Every tool fronts something another
  feature owns.
- **It is not a second authorization engine.** Decisions come from the same PDP
  as every other transport ([`../authorization/`](../authorization/architecture.md)).
- **It does not expose the whole REST surface.** 459 tools against 567
  permissioned routes is a deliberate difference, not a gap to close
  mechanically.
