# Accounts — API

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Every wire surface for pooled accounts, spend limits, cost and account rotation. Feature folder: `docs/accounts/`. Conflicts with this document are defects.

The design is in [architecture.md](architecture.md), the errors and their
status codes in [engineering.md](engineering.md#errors), and the commands in
[cli.md](cli.md).

## Transports

Every verb on this page is served on every transport:

| Transport | What it is |
| --- | --- |
| REST | `/api/v1/...` (`pkg/api/rest/routes.go`) |
| gRPC | `quicbaas.account.v1.AccountService` (`api/proto/account.proto`) and `quicbaas.orchestrator.v1.OrchestratorService` (`api/proto/orchestrator.proto`) |
| Unix | The gRPC wire over a domain socket |
| QUIC | The REST surface over HTTP/3 |
| MCP | `quicbaas_agent_account_*` and `quicbaas_orchestrator_*` tools (`pkg/api/mcp`) |
| Embedded | In-process operations (`sdk/go/transport/embedded/transport.go:296-307`) |

The Go SDK reaches the account verbs through `SDK().Account()`
(`sdk/go/client/account/client.go`) and the orchestrator verbs through
`client.Orchestrator()`. The TypeScript SDK covers less; see
[engineering.md](engineering.md#known-gaps).

## The account surface

| Verb | REST | gRPC RPC | MCP tool | Embedded op | Permission |
| --- | --- | --- | --- | --- | --- |
| Create | `POST /api/v1/agent-accounts/` | `CreateAccount` | `quicbaas_agent_account_create` | `quicbaas.Account/Create` | `credentials:store` |
| List | `GET /api/v1/agent-accounts/` | `ListAccounts` | `quicbaas_agent_account_list` | `quicbaas.Account/List` | `agent_accounts:list` |
| Get | `GET /api/v1/agent-accounts/{id}` | `GetAccount` | `quicbaas_agent_account_get` | `quicbaas.Account/Get` | `agent_accounts:read` |
| Update | `PUT /api/v1/agent-accounts/{id}` | `UpdateAccount` | `quicbaas_agent_account_update` | `quicbaas.Account/Update` | `credentials:store` |
| Delete | `DELETE /api/v1/agent-accounts/{id}` | `DeleteAccount` | `quicbaas_agent_account_delete` | `quicbaas.Account/Delete` | `credentials:delete` |
| Set default | `POST /api/v1/agent-accounts/{id}/default` | `SetDefaultAccount` | `quicbaas_agent_account_set_default` | `quicbaas.Account/SetDefault` | `credentials:store` |
| Rotate credential | `POST /api/v1/agent-accounts/{id}/rotate` | `RotateAccountCredential` | `quicbaas_agent_account_rotate` | `quicbaas.Account/RotateCredential` | `credentials:rotate` |
| Revoke | `POST /api/v1/agent-accounts/{id}/revoke` | `RevokeAccount` | `quicbaas_agent_account_revoke` | `quicbaas.Account/Revoke` | `credentials:delete` |
| Usage | `GET /api/v1/agent-accounts/{id}/usage` | `GetAccountUsage` | `quicbaas_agent_account_usage` | `quicbaas.Account/Usage` | `agent_accounts:read` |
| Release | `POST /api/v1/agent-accounts/{id}/materialize` | `MaterializeAccountCredential` | `quicbaas_agent_account_materialize` | `quicbaas.Account/Materialize` | `agent_accounts:materialize` |
| Reserve | `POST /api/v1/agent-accounts/{id}/reservations` | `ReserveAccountSession` | `quicbaas_agent_account_reserve_session` | `quicbaas.Account/ReserveSession` | `agent_accounts:materialize` |
| Release reservation | `DELETE /api/v1/agent-accounts/{id}/reservations/{rid}` | `ReleaseAccountSession` | `quicbaas_agent_account_release_session` | `quicbaas.Account/ReleaseSession` | `agent_accounts:materialize` |

Routes: `pkg/api/rest/routes.go:290-301`. Permissions:
`pkg/api/rest/testdata/route_permissions.golden`, `account.proto:37-85` and
`pkg/api/mcp/authz_qrdb.go:404-421`. MCP tools:
`pkg/api/mcp/account_tools.go:70-246`.

### Authorization

The permissions split three ways:

- **Writes** use the `credentials:*` actions: `store` to create, update and set
  the default, `rotate` to replace a credential, `delete` to delete or revoke.
- **Reads** use `agent_accounts:list` and `agent_accounts:read`. They return
  metadata only, never credential material.
- **Release and reservations** use `agent_accounts:materialize`. Holding the
  read permission does not grant it. An enrolled daemon holds it through the
  tenant-scoped `agent-runtime` role.

The write and read verbs take the tenant from the request (`tenant_id`, or
`?tenant_id=` on a REST path verb, `handlers_account.go:669`). The release and
reservation verbs take it from the authenticated principal. A `tenant_id` that
names another tenant is refused `403`.

## REST bodies

### Create

`POST /api/v1/agent-accounts/` (`handlers_account.go:80`). Answers `201` with
`{account, timestamp}`.

```json
{
  "tenant_id": "acme",
  "project_id": "",
  "provider": "claude",
  "display_name": "claude1",
  "default": false,
  "refresh_policy": "none",
  "credential_secret_ref": "agents/claude/claude1/oauth_token",
  "credential_secret_file": "",
  "max_concurrent_sessions": 2,
  "billing": {"mode": "subscription", "monthly_fee_micros": 200000000},
  "spend": {
    "quota": {"windows": [{"name": "five-hour", "size": 18000000000000, "max_tokens": 2000000}]},
    "breaker": {"window": 3600000000000, "failure_threshold": 3, "cooldown": 1800000000000}
  }
}
```

`tenant_id` may come from `?tenant_id=` instead. An inline account sends
`credential` in place of `credential_secret_ref`:

```json
"credential": {
  "env": {"SOME_KEY": "value"},
  "files": {},
  "access_token": "...",
  "refresh_token": "...",
  "expires_at": "2026-01-01T00:00:00Z",
  "token_endpoint": "https://auth.example.com/v1/oauth/token",
  "client_id": "<client-id>",
  "scopes": ["inference"],
  "access_token_env_key": "CLAUDE_CODE_OAUTH_TOKEN"
}
```

Durations in `spend` are Go durations in nanoseconds.

### Update

`PUT /api/v1/agent-accounts/{id}?tenant_id=acme` (`handlers_account.go:161`).
Only the fields present change:

| Field | Effect |
| --- | --- |
| `display_name`, `refresh_policy` | Replace |
| `project_id` / `clear_project_id` | Move into a project scope, or back to tenant-wide |
| `credential_secret_ref`, `credential_secret_file` | Switch onto a tenant secret, or re-point it |
| `max_concurrent_sessions` | New session cap; `0` removes it |
| `billing` | Replace; an empty object clears it |
| `spend` | Replace; an empty object makes the account unmetered |

Answers `{account, timestamp}`.

### The account

Every response that carries an account uses `AccountRESTResponse`
(`handlers_account.go:196`). It never carries credential material.

```json
{
  "id": "acct_abc123",
  "tenant_id": "acme",
  "project_id": "",
  "provider": "claude",
  "display_name": "claude1",
  "default": true,
  "status": "active",
  "refresh_policy": "none",
  "billing": {"mode": "subscription", "monthly_fee_micros": 200000000},
  "credential_secret_ref": "agents/claude/claude1/oauth_token",
  "spend": {"quota": {"windows": [{"name": "five-hour", "size": 18000000000000, "max_tokens": 2000000}]}},
  "last_validated_at": "2026-01-01T00:00:00Z",
  "credential_health": "valid",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "last_used_at": "2026-01-01T00:00:00Z",
  "max_concurrent_sessions": 2
}
```

Empty fields are omitted: `billing` and `spend` when not configured,
`available_at` except on a `limited` account, `credential_expires_at` when the
expiry is unknown. `status` is `active`, `revoked`, `error` or `limited`. `credential_health` is
`unknown`, `valid`, `expiring` or `expired`.

### Other verbs

| Verb | Body | Response |
| --- | --- | --- |
| Get | — | `{account, timestamp}` |
| List | — | `{accounts, total, limit, offset, next_offset, facets, timestamp}` |
| Rotate | `{credential}` | `{account_id, timestamp}` |
| Revoke | `{reason}` | `{account, timestamp}` |
| Set default | — | `{account_id, timestamp}` |
| Delete | — | `{account_id, deleted, timestamp}` |
| Usage | — | `{usage, live_sessions, spend, timestamp}` |

List takes `tenant_id` (required), `provider`, `project_id`, `status` and the
shared paging parameters (see
[../software-factory/api.md](../orchestration/api.md#list-paging-sorting-and-search)).
`facets` counts the matches by provider and by status.

Usage answers:

```json
{
  "usage": {"total_tokens": 5200, "budget_limit": 0, "percent_used": 0,
            "by_agent": {"agent-01": 5200}, "last_updated": "2026-01-01T00:00:00Z"},
  "live_sessions": 1,
  "spend": {"admits": false, "refusal": "quota_exhausted", "window": "five-hour",
            "axis": "tokens", "used": 2000000, "limit": 2000000,
            "frees_at": "2026-01-01T05:00:00Z"}
}
```

`spend` is the account's spend headroom (`account_spend_headroom.go:42`): the
same verdict the selection filter applies. `refusal` is `quota_exhausted` or
`breaker_open`; `failures` is set for an open breaker. A worker daemon reads
it so that its own selection skips a spent account.

## Protocol buffers

`api/proto/account.proto`:

| Message | Fields |
| --- | --- |
| `Account` | `id` 1, `tenant_id`, `project_id`, `provider`, `display_name`, `is_default`, `status`, `refresh_policy`, `created_at_unix`, `updated_at_unix`, `last_used_at_unix`, `last_refreshed_at_unix`, `last_error`, `available_at_unix` 14, `credential_expires_at_unix` 15 (0 is unknown), `last_validated_at_unix` 16, `credential_health` 17, `credential_secret_ref` 18, `credential_secret_file` 19, `max_concurrent_sessions` 20, `billing` 21, `spend` 22 |
| `AccountBilling` | `mode`, `monthly_fee_micros` |
| `AccountCredential` (requests only) | `env`, `files`, `access_token`, `refresh_token`, `expires_at_unix`, `token_endpoint`, `client_id`, `scopes`, `access_token_env_key` |
| `UpdateAccountRequest` | `optional max_concurrent_sessions` 9; `billing` 10 and `spend` 11, where a present empty message clears billing or makes the account unmetered |
| `GetAccountUsageResponse` | `usage` (`AccountUsage`), `live_sessions` 3, `spend` 4 (`SpendHeadroom`) |
| `MaterializedAccountCredential` | `env`, `files`, `access_token`, `expires_at_unix`, `scopes`, `access_token_env_key`, `generation` 7 |

`api/proto/spend.proto` (`quicbaas.spend.v1`) holds the shape an account, a
project and a tenant share:

| Message | Fields |
| --- | --- |
| `SpendLimitWindow` | `name`, `size_nanos`, `max_tokens`, `max_equivalent_micros`. At least one maximum must be positive; zero leaves that axis unmetered. |
| `SpendLimits` | `windows` |
| `SpendBreakerPolicy` | `window_nanos`, `failure_threshold`, `cooldown_nanos` |
| `AccountSpendPolicy` | `quota`, `breaker` (unset: never tripped) |
| `SpendHeadroom` | `admits`, `refusal`, `window`, `axis`, `used`, `limit`, `failures`, `frees_at` |

### List paging

`ListAccountsRequest` takes `tenant_id`, `provider`, `project_id`, `limit` (0
means 25, at most 100), `offset`, `sort` (`field` or `field:desc`; an unknown
field is `InvalidArgument`) and `q`. The response carries `accounts`, `total`,
`timestamp`, `limit`, `offset` and `next_offset`.

## Reservations

A worker daemon reserves a session slot on an account before it releases the
credential, so the account's `max_concurrent_sessions` holds across hosts
(`pkg/api/rest/handlers_account_reservation.go`).

```
POST /api/v1/agent-accounts/{id}/reservations
{"expected_live": 1}
→ 200 {"reservation_id": "<reservation-id>", "timestamp": "..."}

DELETE /api/v1/agent-accounts/{id}/reservations/{rid}
→ 200 {"reservation_id": "<reservation-id>", "timestamp": "..."}
```

`expected_live` is optional. When it is present and the live count has moved,
the reservation is refused (`412`; gRPC `ABORTED`) and the daemon re-reads the
usage. A capped account is refused `409` (gRPC `RESOURCE_EXHAUSTED`). An
unreleased reservation expires after 15 minutes.

gRPC: `ReserveAccountSessionRequest{tenant_id, account_id, optional
expected_live}` answers `{reservation_id, timestamp}`;
`ReleaseAccountSessionRequest{tenant_id, account_id, reservation_id}`.

## The release verb

`POST /api/v1/agent-accounts/{id}/materialize` hands an account's credential to
an enrolled worker daemon (`handlers_account.go:866-1010`,
`pkg/service/account_materialize.go:301`). It is the only verb whose response
is secret material; see
[architecture.md](architecture.md#the-release-verb).

```json
{"project_id": "p1", "job_id": "", "lease_id": "lease_..."}
```

- `lease_id` is empty on a first delivery and names the held grant on a
  refresh.
- A daemon leaves `job_id` empty, because one release serves every job on the
  host.
- The response sets `Cache-Control: no-store`.

```json
{
  "account_id": "acct_abc123",
  "provider": "claude",
  "display_name": "claude1",
  "project_id": "p1",
  "refresh_policy": "none",
  "credential": {
    "access_token": "...",
    "expires_at": "2026-01-01T08:00:00Z",
    "access_token_env_key": "CLAUDE_CODE_OAUTH_TOKEN",
    "generation": 4
  },
  "expires_at": "2026-01-01T08:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "lease_id": "lease_...",
  "lease_expires_at": "2026-01-01T01:00:00Z",
  "single_use": true,
  "timestamp": "2026-01-01T00:00:00Z"
}
```

`credential` also carries `env`, `files` (base64 values) and `scopes` when the
account has them. It carries no refresh token, token endpoint or client id.
`single_use` is true for an account over a tenant secret: the daemon uses the
copy for one spawn and does not cache it.

gRPC: `MaterializeAccountCredentialRequest{tenant_id, account_id, project_id,
job_id, lease_id}`. The response carries the credential, `lease_id` 11,
`lease_expires_at_unix` 12 and `single_use` 13.

### Refusals

Checked in this order (`account_materialize.go:319-433`):

| Check | REST | gRPC |
| --- | --- | --- |
| No token manager wired | 503 | `Unavailable` |
| Unknown account | 404 | `NotFound` |
| Project fence: the account is scoped to another project | 403 | `PermissionDenied` |
| The account is revoked, errored or held (`available_at` in the body) | 409 | `FailedPrecondition` |
| The account's spend policy refuses | 500 (Known gap 15) | `Internal` |
| A revoked delivery grant is presented | 409 | `Internal` (Known gap 14) |
| The tenant secret cannot be read | 409, `fault_class: credential_not_found` | `FailedPrecondition` |
| No secret resolver wired | 503 | `Unavailable` |

A refusal caused by spawn configuration carries `fault_class` in the error
body, from the same vocabulary as gRPC spawn evidence
(`orchestration.SpawnFaultClass`). The daemon reads up to 64 KiB of the error
body for the class before it shortens the message to 512 bytes.

Every attempt, granted or refused, writes one audit event naming the account,
the agent and, when present, the lease and job. No credential field is ever in
it.

## Spend policy

The tenant's own spend limits (`handlers_orchestrator_spend_policy.go`).

| Verb | REST | gRPC (`OrchestratorService`) | MCP | Permission |
| --- | --- | --- | --- | --- |
| Get | `GET /api/v1/orchestrator/spend-policy` | `GetTenantSpendPolicy` | `quicbaas_orchestrator_spend_policy_get` | `orchestrator:status` |
| Set | `PUT /api/v1/orchestrator/spend-policy` | `SetTenantSpendPolicy` | `quicbaas_orchestrator_spend_policy_set` | `spend_policy:write` |

```json
PUT /api/v1/orchestrator/spend-policy?tenant_id=acme
{"limits": {"windows": [{"name": "monthly", "size": 2592000000000000, "max_equivalent_micros": 2000000000}]}}
```

`limits` with no windows make the tenant unmetered. Both verbs answer the
report:

```json
{
  "tenant_id": "acme",
  "limits": {"windows": [{"name": "monthly", "size": 2592000000000000, "max_equivalent_micros": 2000000000}]},
  "updated_at": "2026-01-01T00:00:00Z",
  "windows": [{
    "name": "monthly", "size": 2592000000000000,
    "max_tokens": 0, "used_tokens": 5200, "remaining_tokens": 0,
    "max_equivalent_micros": 2000000000, "used_equivalent_micros": 36500,
    "remaining_equivalent_micros": 1999963500,
    "exhausted": false, "frees_at": "2026-01-31T00:00:00Z"
  }]
}
```

`exhausted` is true once used equals the maximum, even though a spawn with a
zero estimate is still admitted. Errors: an invalid window `400`, another
tenant `403`, the store or the accessor unavailable `503`.

A project's limits are its `spend` field (a `SpendLimits`) on the project
verbs; see [../projects/architecture.md](../projects/architecture.md). How
admission enforces both is in
[architecture.md](architecture.md#projects-and-tenants-deferred-at-admission).

## Spend report

`GET /api/v1/orchestrator/cost` reads one scope of the consumption ledger.

| Transport | Name |
| --- | --- |
| gRPC | `OrchestratorService/GetSpendReport` |
| MCP | `quicbaas_orchestrator_cost` |
| Go SDK | `client.Orchestrator().GetSpendReport` |

Permission `orchestrator:status`.

```
GET /api/v1/orchestrator/cost
  ?tenant_id=   optional; must be the caller's own tenant (403 otherwise)
  &project_id=  one project
  &task_id=     one task       (requires project_id)
  &job_id=      one job        (requires task_id)
  &session_id=  one session    (requires job_id)
  &round=       one round      (requires session_id)
  &from=        turns at or after this RFC 3339 instant
  &to=          turns before this instant (requires from)
  &host=        turns that ran on this host ("-" for none)
  &agent=       turns of sessions that ran as this tenant Agent ("-" for
                raw-backend sessions); adds agent_policy_status and agent_windows
```

```json
{"level": "project", "tenant_id": "acme", "project_id": "p1",
 "project_name": "example-service", "currency": "USD",
 "entries": 4, "input_tokens": 1000, "output_tokens": 1200,
 "cache_read_tokens": 3000, "cache_write_tokens": 0, "total_tokens": 5200,
 "equivalent_micros": 36500, "priced_entries": 4, "unpriced_entries": 0,
 "billed_micros": 0, "billed_entries": 4, "unbilled_entries": 0,
 "duration_ms": 184000, "successes": 4, "failures": 0,
 "child_level": "task",
 "children": [{"id": "task-1", "level": "task", "total_tokens": 5200, "equivalent_micros": 36500, "priced_entries": 4}],
 "accounts": [{"account_id": "acct_abc123", "display_name": "claude1",
               "billing": {"mode": "subscription", "monthly_fee_micros": 200000000},
               "total_tokens": 5200, "equivalent_micros": 36500, "billed_micros": 0,
               "policy_status": "none"}],
 "models": [{"engine": "claude", "model": "claude-opus-4-5-20251101", "total_tokens": 5200, "equivalent_micros": 36500}],
 "hosts": [{"host": "gpu-host.internal:9091", "total_tokens": 5200}],
 "measured_energy_entries": 0, "electricity_entries": 0}
```

Rules a client must follow:

- **The scope is a path.** A level is accepted only with every level above it;
  a skipped level is `400`. Over MCP, `-` at a level opens the subtree of
  entries with no value there, such as a job that belongs to no task.
- **Zero entries means unmeasured.** Read `priced_entries` before
  `equivalent_micros` and `billed_entries` before `billed_micros`. Zero means
  unmeasured: show a dash, never zero. Non-zero `unpriced_entries` or
  `unbilled_entries` make the figure a floor.
- **Fees are shared only at the tenant level.** A tenant-level report
  pro-rates each subscription's monthly fee over the range
  (`accounts[].subscription_fee_micros`, summed as `subscription_fee_micros`).
- **`policy_status`** distinguishes an account with no cap (`none`) from one
  whose policy could not be read (`unknown`).
- A deployment that records no consumption answers `503`, not an empty report.

The MCP tool takes `tenant_id`, `project_id`, `task_id`, `job_id`, `session_id`
and `round`.

## Cost settings

How a tenant's local inference is costed
(`handlers_orchestrator_cost_settings.go`,
[architecture.md](architecture.md#local-inference-is-costed-from-electricity)).

| Verb | REST | gRPC (`OrchestratorService`) | MCP | Permission |
| --- | --- | --- | --- | --- |
| Get | `GET /api/v1/orchestrator/cost-settings` | `GetCostSettings` | `quicbaas_orchestrator_cost_settings_get` | `orchestrator:status` |
| Set | `PUT /api/v1/orchestrator/cost-settings` | `SetCostSettings` | `quicbaas_orchestrator_cost_settings_set` | `cost_settings:write` |

```json
PUT /api/v1/orchestrator/cost-settings?tenant_id=acme
{"electricity_price_per_kwh": 0.15, "engine_power_watts": {"openvino": 12}}
```

The body replaces the settings. An absent price inherits the configuration;
an engine set to `0` is not costed for electricity. Both verbs answer
(`cost_settings.go:272`):

```json
{
  "settings": {"tenant_id": "acme", "electricity_price_per_kwh": 0.15, "engine_power_watts": {"openvino": 12}},
  "electricity": {"price_per_kwh": 0.15, "source": "tenant_settings"},
  "engine_power_watts": {"openvino": 12, "ollama": 250}
}
```

`source` is `tenant_settings`, `tenant_config`, `deployment_config` or
`default` (`price_book.go:471-474`). Errors: an invalid value `400` (MCP
`CostSettingsArgumentError`), another tenant `403`, the store or the accessor
unavailable `503`.

## Rotating a session's account

Move a running session onto another pooled account
([architecture.md](architecture.md#rotating-a-session-onto-another-account)).

| Transport | Name |
| --- | --- |
| REST | `POST /api/v1/orchestrator/sessions/{sessionID}/rotate-account` |
| gRPC | `OrchestratorService/RotateSessionAccount` (`orchestrator.proto:43`) |
| MCP | `quicbaas_orchestrator_rotate_account` |
| Embedded | `quicbaas.Orchestrator/RotateSessionAccount` |

Permission `orchestrator:spawn`: the rotation rebuilds the agent process on a
new credential, which is a spawn's authority.

```json
POST /api/v1/orchestrator/sessions/sess_abc123/rotate-account?tenant_id=acme
{"reason": "seat retired", "hold_outgoing": true, "available_at": "2026-01-01T05:00:00Z"}
```

| Field | gRPC | Meaning |
| --- | --- | --- |
| `session_id` | 1 | The session. On REST the path wins. |
| `tenant_id` | 2 | The session's tenant |
| `reason` | 3 | Recorded on the closed account segment and in the transcript |
| `hold_outgoing` | 4 | Hold the outgoing account out of the pool |
| `available_at` | 5 (`available_at_unix`) | When the held account rejoins; empty or 0 applies the default backoff |

The body may be empty. The response
(`orchestrator.proto:899`; REST also carries `credential_lease_id` and
`reservation_id`):

```json
{"session_id": "sess_abc123", "previous_account_id": "acct_a",
 "account_id": "acct_b", "strategy": "least_usage", "mode": "resume"}
```

`mode` is `in_place` when the credential was swapped without a restart, or
`resume` when the process was rebuilt and the transcript replayed. A session
pinned with a bare `account_id` is refused `409`; an unknown session is `404`;
a deployment without rotation answers `501`
(`handlers_orchestrator.go:314-363`).

## Spawn fields

`SpawnSessionRequest` (`orchestrator.proto:620-667`) selects the account:

| Field | Number | Meaning |
| --- | --- | --- |
| `account_id` | 16 | Pin one account. A bare pin is never rotated. |
| `account_auto` | 17 | Select with a strategy; rotatable across the pool |
| `account_strategy` | 18 | `least_usage` (default) or `static` |
| `account_allow` | 19 | Restrict to these accounts; rotatable among them |
| `account_exclude` | 26 | Accounts to skip. Set by rotation. |

The response carries `credential_lease_id` 8 (`:775`), the delivery grant
the session was served under, and `reservation_id` 10 (`:784`). The rest of
spawn is in
[../orchestration/architecture.md](../orchestration/architecture.md).
