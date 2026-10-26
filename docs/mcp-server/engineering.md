# MCP Server — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract for the MCP surface. Feature folder:
> `docs/mcp-server/`.

## Registering a tool

Server side, in `pkg/api/mcp/`:

```go
func (r *ToolRegistry) registerSchedulerTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_scheduler_create_job",
		Description: "Create a new scheduled job with scheduling parameters, handlers, and optional payload",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Human-readable job name",
				},
				// ...
			},
		},
	})
}
```

That is `pkg/api/mcp/scheduler_tools.go:13` verbatim in shape. Three fields, and
all three are load-bearing:

- **`Name`** must match what `ToolName` (`sdk/go/transport/mcp/transport.go:882`)
  will emit for the service/method pair the SDK dispatches. If it does not, the
  call arrives at a tool that is not registered.
- **`Description`** is read by a model and influences whether the tool is chosen
  correctly. It is not a code comment.
- **`InputSchema`** is JSON Schema. `enum` constraints are worth writing —
  `"owner_type": {"enum": ["system","tenant","delegated"]}` is what stops a
  model inventing a fourth.

## Tool naming — the rule and its exceptions

`ToolName` resolves in this order (`transport.go:882-980`):

1. If the method already starts with `quicbaas_` or `qrdb_`, it **is** the tool
   name; return it unchanged. This is the path the CLI uses when it passes a
   pre-constructed name.
2. **IAM** — explicit lookup via `mapIAMToolName`, falling back to
   `quicbaas_iam_<subsystem>_<snake_case_method>`.
3. **Orchestrator sessions** — `orchestratorMethodTools`.
4. **Pooled accounts** — `accountMethodTools`.
5. **SSH** — `sshToolName`.
6. **Dynamic secrets** — `dynamicSecretsToolName`.
7. **PIV** and **FIDO2** — `pivToolName` / `fido2ToolName`, which produce the
   dotted xkms compatibility names (`xkms.piv.*`, `xkms.fido2.*`) that the
   generic derivation could never generate.
8. **Projects** — `projectToolName`.
9. Otherwise `quicbaas_<service>_<snake_case_method>`, with a special case: core
   KV operations (`Put`, `Get`, `Delete`, `List`, `Scan`) become `qrdb_*`
   because they are qrdb-mirror tools, while quicbaas-specific KV methods keep
   the `quicbaas_` prefix.

**Adding a method to a subsystem with a lookup table means adding a row to that
table.** The fallback will produce a name nothing registered.

## IAM session tools are self-service

`quicbaas_iam_session_get`, `_list`, `_revoke` and `_revoke_all`
(`pkg/api/mcp/tools_iam_session.go`) act only on the authenticated caller's own
sessions, the same semantics as the embedded transport's `requireSessionOwner`
(ruling 207). An administrator ending another user's sessions uses an IAM users
verb, not a session tool.

- `user_id` and `tenant_id` are optional on `_list` and `_revoke_all`; they
  default to the caller. A value naming another user or tenant is refused as
  forbidden, as is a call with no authenticated caller.
- `_get` and `_revoke` refuse a session owned by another user or tenant.
- The four tools are tenant-scoped (`iamTenantScopedTools`) and require
  `user_authfactors:login`, the grant the embedded transport requires for the
  `quicbaas.Auth` session verbs.
- Results are `iam.SessionView`: no `token_hash` or `refresh_hash` ever leaves
  the server.

The SDK routes `Auth` `GetSession`, `ListSessions`, `RevokeSession` and
`RevokeAllSessions` to these tools through `authSessionTools`
(`sdk/go/transport/mcp/auth_handler.go`) and decodes the views with
`service.NewAuthSession`; the revoke verbs return nil.

## Public tools

`isPublicTool` (`transport.go:690-700`) is an exhaustive map:

```
quicbaas_login  quicbaas_login_begin  quicbaas_login_finish
qrdb_health     quicbaas_liveness     quicbaas_readiness
```

Everything else runs `ensureAuthenticated` before dispatch. Adding a name here
publishes an unauthenticated capability; it is a security change, not a
convenience.

**Known defect:** stale entries in the `publicTools` set with no guard test are
a registered boundary-violation item in this repository. A name listed here that
no longer corresponds to a registered tool is dead weight; a name that does is
an unauthenticated surface. Any change to this map should land with a test that
asserts the set matches intent.

## Transport mechanics

- **Streamable HTTP**, configured by `streamableHTTPOptions`
  (`transport.go:708`). The header set is read *dynamically* per request via
  `WithHTTPHeaderFunc(t.dynamicHeaders)`, which is what allows a token acquired
  mid-session by the login flow to take effect on in-flight and future requests
  on both the session client and the independent raw transport.
- **Retry** is at the `requestWithRetry` layer with `errors.IsRetryable`
  gating; non-retryable errors return immediately.
- **`ensureInitialized`** runs before every call: MCP is session-oriented and
  the session is established lazily.

## Sealed window

`pkg/api/mcp/sealed_window.go` carries the subset of tools answerable before the
barrier is unsealed. Everything else must fail closed while sealed — a tool that
answers from a sealed server is either serving stale state or reaching data it
should not be able to read.

## Test strategy

- **The build-time assertion that every emittable name is registered is the load
  bearing test.** `ToolName` is exported for exactly this purpose; keep the
  assertion.
- Schema tests: a tool whose `InputSchema` does not accept the arguments its
  handler requires fails only at call time, and only for a model that guessed
  right about everything else.
- Authorization tests must cover the *filtered* `tools/list`, not just
  allow/deny on invocation. A leaked tool listing is an information disclosure
  even if the call is refused.
- Parity: a capability reachable over REST and MCP must behave identically. The
  multiprotocol parity suite is where that is asserted.

## Observability

Tool invocations are auditable. Because an MCP caller is frequently an agent
principal, the audit trail is the only place the sequence of actions an
autonomous agent took is reconstructable — see
[`../audit-logging/`](../audit-logging/user.md).
