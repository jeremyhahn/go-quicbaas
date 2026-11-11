# MCP Server — Architecture

> **Status: NORMATIVE** · Last verified: 2026-09-23
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

The MCP endpoint is go-qrdb's `mark3labs/mcp-go` Streamable HTTP server; the
bespoke JSON-RPC transport this package once ran — and its OAuth 2.1 / PKCE /
Dynamic Client Registration flow — was retired (see the package doc in
`pkg/api/mcp/server.go`). Authorization today is composed, not bespoke:

- **Authentication is the connection's mTLS peer certificate**, resolved by
  go-qrdb's shared `pkg/auth/middleware.AuthMiddleware` exactly as it is for
  REST, gRPC, QUIC and Unix (`pkg/api/mcp/auth.go:authHTTPContext` in
  go-qrdb). The resolved identity is bound into every tool call's context.
- **Authorization is per tool and DEFAULT-DENY.** Each call is checked against
  the permission the tool registered (`pkg/api/mcp/authz.go`,
  `authz_qrdb.go`, surfaced through `pkg/apimodule`'s
  `MCPToolPermissions`); a tool with no registered permission is refused.
- **There is an HTTP-layer gate.** With authentication on, go-qrdb answers a
  request no authenticator accepts with `401` before mcp-go reads it, so not
  even `initialize` or `tools/list` reaches an anonymous caller.
- **An OAuth 2.1 bearer challenge is optional and not enabled.** go-qrdb can
  emit a `401` + `WWW-Authenticate` challenge (`mcp.WithBearerChallenge`, via
  `qrdbsdk.WithMCPServerOptions`); quicbaas does not configure it.
- **The endpoint is session-based.** Every request other than `initialize`
  must carry the `Mcp-Session-Id` the server issued, or it is answered `404`.
  This is protocol state, not authorization — see
  [`cli.md`](cli.md#troubleshooting-a-404).

Two properties worth stating here:

- **A small set of tools is public.** `publicTools` in `pkg/api/mcp/authz.go`
  names them: `qrdb_health`, `quicbaas_liveness`, `quicbaas_readiness`. Both
  login tools stay undeclared (refused): `quicbaas_login_begin` until the
  pinned go-qrdb SDK expires and caps abandoned login attempts, and
  `quicbaas_login_finish` until go-qrdb#1181 gives MCP handlers the verified
  mTLS leaf it binds to. The SDK client's `isPublicTool`
  (`sdk/go/transport/mcp/transport.go`) skips `ensureAuthenticated` for the
  public tools, and also for `quicbaas_login_begin`, `quicbaas_login_finish`
  and `quicbaas_login`, which its logins call but the server refuses or does
  not register. Health must be public or a probe needs a token during exactly
  the states it exists to report; login is done over REST or gRPC.
- **Tools are scope-filtered per principal.** An agent is an IAM principal, and
  its `tools/list` reflects what it may call rather than the full 459. That is
  what makes handing an assistant an MCP endpoint different from handing it a
  token.

Neither property holds yet (go-quicbaas#1127): the HTTP-layer gate refuses the
public tools to an anonymous caller, and go-qrdb has no per-principal
`tools/list` filter. Both need go-qrdb support: an anonymous pass-through limited
to the public set, and a listing filtered by the caller's permissions.

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
  │   mTLS identity + per-tool DEFAULT-DENY permission check
  ▼
handler ──► the same service layer REST and gRPC reach
```

The last line is the important one: MCP is a *third door into one service
layer*, not a parallel implementation. A capability that behaves differently
over MCP than over REST is a defect.

## Trust boundaries

- **The public tool set is an unauthenticated surface.** It is deliberately three
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
