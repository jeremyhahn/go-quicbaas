# MCP Server — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented guide to connecting an AI assistant to QuicBaaS over the Model
> Context Protocol. Feature folder: `docs/mcp-server/`.

MCP is how an AI assistant reaches this server with a tool list it can
enumerate, argument schemas it can fill, and an OAuth token scoped to what the
calling principal may do. **459 tools are registered**; a given principal sees
only the subset it is permitted to call.

## Prerequisites

- A server with the MCP listener enabled:

  ```yaml
  server:
    mcp:
      enabled: true
      port: 11443
  ```

  These two accept environment overrides — `QUICBAAS_SERVER_MCP_ENABLED` and
  `QUICBAAS_SERVER_MCP_PORT` are in the explicit list `pkg/config/loader.go`
  reads.
- TLS. The MCP listener serves HTTPS.
- Credentials: either a stored platform token (the CLI's usual login) or an
  OAuth 2.1 client for an external assistant. See
  [`../oauth2-oidc/user.md`](../oauth2-oidc/user.md).

## First run — see what is there

```bash
# List the tools this principal may call.
quicbaas mcp tools --address quicbaas.example.com:11443

# Invoke one.
quicbaas mcp call quicbaas_readiness
quicbaas mcp call quicbaas_scheduler_list_jobs --args '{}'
quicbaas mcp call quicbaas_iam_user_get --args '{"id":"user-4f2a"}'
```

`--args` takes a JSON object. The shape it must have is the tool's
`InputSchema`, which is what `mcp tools` prints — read it there rather than
guessing.

## Connecting an assistant

`quicbaas mcp serve` bridges an MCP host's stdio pipe onto the server:

```bash
quicbaas mcp serve --stdio --address quicbaas.example.com:11443
quicbaas mcp serve --stdio --address localhost:11443 --tls-ca ./data/certs/ca-chain.crt
```

Four things about the bridge, from its own documentation
(`cmd/quicbaas/mcp.go:174-196`):

- **It is transparent.** `initialize`, `tools/list`, `tools/call` and everything
  else reach the server verbatim, so the host negotiates directly with the
  server rather than with the bridge.
- **It uses the same credentials as every other CLI command** — the stored
  platform token and the same TLS material, honouring `--address`, `--tls-ca`,
  `--tls-cert`, `--tls-key` and `--timeout`.
- **Notifications are not forwarded.** JSON-RPC messages with no `id` are
  acknowledged locally, because each bridged call is an independent HTTP
  request and there is no client session for a notification to update.
- **It blocks until stdin reaches EOF** or the process is interrupted. That is
  the correct shape for an MCP host that spawns it as a child process.

## What an assistant can and cannot see

`tools/list` is **filtered per principal**. An agent is an IAM principal like
any other, so the tool list it receives reflects its permissions rather than the
full registry. Giving an assistant an MCP endpoint is therefore not equivalent
to giving it a token against the REST API — the containment is in the listing as
well as the invocation.

Five tools are public and need no token: `quicbaas_login_begin`,
`quicbaas_login_finish`, `qrdb_health`, `quicbaas_liveness`,
`quicbaas_readiness`. Login has to be reachable or there is
no way in; health has to be reachable or a probe needs a token during exactly
the states it exists to report.

> **Known gap (go-quicbaas#1127).** The endpoint does not yet behave as this
> section requires, because the gate is go-qrdb's MCP server. With
> authentication on, it answers any request that carries no credential with
> `401` before `initialize` or `tools/list`, so the public tools are not
> reachable anonymously. It also has no per-principal `tools/list` filter, so an
> authenticated principal is shown every registered tool and is refused only
> when it calls one. Fixing either needs go-qrdb support.

## Before you point an assistant at production

- **Scope the principal first.** Create a dedicated identity with the narrowest
  role that covers the task, and confirm with `mcp tools` that its listing is
  what you expect. Verify the *listing*, not just that a call is refused — a
  tool the model can see is a tool it will try.
- **Read the tool descriptions.** They are, in effect, instructions to a model.
  A vague description gets a tool chosen wrongly.
- **Turn on audit.** For an autonomous agent, the audit trail is the only
  reconstruction of what it did. See
  [`../audit-logging/user.md`](../audit-logging/user.md).

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `tools/list` is much shorter than expected | Correct behaviour: it is filtered to the principal's permissions. |
| A tool call fails with an argument error | The `--args` object does not match the tool's `InputSchema`. Print it with `mcp tools`. |
| Authentication fails and a token is being sent | Tokens go in the `Authorization: Bearer` header. Body parameters (`_token`, `accessToken`) were removed in v1.1.0. |
| Token valid, call still rejected | Audience mismatch. Resource Indicators bind a token to a resource. |
| A tool answers on a sealed server | Only the sealed-window subset should. Report it — everything else must fail closed. |
| Notifications from the host appear to be ignored | They are, deliberately, by the stdio bridge. |
| The bridge exits immediately | stdin reached EOF. It is meant to be spawned by an MCP host that holds the pipe open. |

## What this feature does not do

- **It does not add capabilities.** Every tool fronts something another feature
  owns; the per-feature `api.md` files list their tools.
- **It does not mirror the REST surface.** 459 tools against 567 permissioned
  routes is a deliberate difference.
- **It is not a second authorization engine.** Decisions come from the same
  policy decision point as every other transport.
