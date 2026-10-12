# Accounts — CLI

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Every `quicbaas` command for pooled accounts, spend limits and cost. Feature folder: `docs/accounts/`. Conflicts with this document are defects.

Every command goes through the Go SDK, so it behaves the same on every
transport (`--transport`: `unix` by default, or `grpc`, `rest`, `quic`, `mcp`,
`embedded`). Tenant-aware commands take the global `--tenant`
flag; there is no per-command tenant flag. The examples use `acme`.

The design is in [architecture.md](architecture.md) and the wire surface in
[api.md](api.md).

## quicbaas account

`cmd/quicbaas/account.go`. The group's `--output` flag (`table`, `json`,
`yaml`; default `table`) applies to every verb (`account.go:252`).

| Command | Line | Does |
| --- | --- | --- |
| `create` | `:273` | Create an account over a tenant secret or an inline credential |
| `list` | `:390` | List the tenant's accounts |
| `get <accountID>` | `:459` | Show one account: identifiers only, never the credential |
| `update <accountID>` | `:496` | Change the name, project scope, refresh policy, credential source, session cap, billing or spend policy |
| `delete <accountID>` | `:598` | Delete the account and its inline envelope. A referenced secret is kept. |
| `rotate <accountID>` | `:639` | Replace an inline credential |
| `revoke <accountID>` | `:689` | Revoke the account |
| `set-default <accountID>` | `:734` | Make this the default of its scope |
| `usage <accountID>` | `:771` | Tokens, live sessions and spend headroom |

### create

| Flag | Meaning |
| --- | --- |
| `--provider` | `claude`, `codex` or `opencode`. Required. |
| `--display-name` | Operator-chosen label |
| `--project` | Scope the account to one project. Empty means tenant-wide. |
| `--default` | Import as the scope's default, displacing the current one. The first account of a scope becomes the default without it. |
| `--refresh-policy` | `none` or `oauth2` |
| `--credential-secret-ref` | Short name of the tenant secret the credential is read from on every spawn |
| `--credential-secret-file` | With `--credential-secret-ref`: write the secret to this file, relative to the session home, instead of projecting it as the access token |
| `--credential-file` | Path to an inline credential document. `-` reads stdin. |
| `--max-concurrent-sessions` | How many live sessions the account may serve at once. 0 is unmetered. |
| `--billing`, `--monthly-fee` | See [Billing flags](#billing-flags) |
| `--spend-window`, `--clear-spend`, `--breaker-*`, `--clear-breaker` | See [Spend flags](#spend-flags) |

Exactly one of `--credential-secret-ref` and `--credential-file` is required.
Both together are refused before anything is read. Credentials are read from a
file or stdin, never from a flag value, because a command-line secret lands in
shell history and the process table.

The inline credential document mirrors the credential envelope:

```json
{
  "access_token": "...",
  "refresh_token": "...",
  "token_endpoint": "https://...",
  "client_id": "...",
  "access_token_env_key": "CLAUDE_CODE_OAUTH_TOKEN",
  "env": {"SOME_KEY": "value"}
}
```

```bash
# Over a tenant secret (recommended)
quicbaas --tenant acme account create --provider claude --display-name claude1 \
  --credential-secret-ref agents/claude/claude1/oauth_token
quicbaas --tenant acme account create --provider codex --display-name reviewer \
  --credential-secret-ref agents/codex/reviewer/auth_json \
  --credential-secret-file .codex/auth.json

# Inline, from stdin
claude setup-token | quicbaas --tenant acme account create --provider claude \
  --display-name alice-work --refresh-policy oauth2 --credential-file -
```

### list

| Flag | Meaning |
| --- | --- |
| `--provider` | Only this provider (`:407`) |
| `--project` | Only accounts scoped to this project (`:408`) |
| `--search`, `--sort`, `--limit`, `--all` | The shared paging flags (`:405`); see [../software-factory/cli.md](../orchestration/cli.md#paging-sorting-and-search) |

```bash
quicbaas --tenant acme account list --provider claude --output json
quicbaas --tenant acme account list --search work --sort display_name --limit 50
```

### update

Flags change only what they name (`:521-529`):

| Flag | Meaning |
| --- | --- |
| `--display-name` | New label |
| `--project` / `--clear-project` | Move the account into a project scope, or back to tenant-wide |
| `--refresh-policy` | `none` or `oauth2` |
| `--credential-secret-ref`, `--credential-secret-file` | Switch the account onto a tenant secret, or re-point it. Switching destroys the inline envelope. |
| `--max-concurrent-sessions` | New session cap; 0 removes it |
| `--billing`, `--monthly-fee` | See [Billing flags](#billing-flags); `--billing none` clears billing |
| Spend and breaker flags | See [Spend flags](#spend-flags) |

```bash
quicbaas --tenant acme account update acct_abc123 --display-name alice-personal
quicbaas --tenant acme account update acct_abc123 --max-concurrent-sessions 2
quicbaas --tenant acme account update acct_abc123 \
  --credential-secret-ref agents/claude/claude1/oauth_token
```

### rotate, revoke, set-default, usage, delete

| Command | Flags |
| --- | --- |
| `rotate` | `--credential-file` (required; `-` reads stdin). Refused on a referenced account: write the secret instead. |
| `revoke` | `--reason` |
| `set-default`, `usage`, `get`, `delete` | None |

```bash
quicbaas --tenant acme account rotate acct_abc123 --credential-file ./new-token.json
quicbaas --tenant acme account revoke acct_abc123 --reason "token leaked"
quicbaas --tenant acme account set-default acct_abc123
quicbaas --tenant acme account usage acct_abc123
quicbaas --tenant acme account delete acct_abc123
```

The release, reserve and release-reservation verbs are for worker daemons and
have no CLI command.

### Billing flags

`cmd/quicbaas/account_billing.go:70-71`, on `create` and `update`:

| Flag | Meaning |
| --- | --- |
| `--billing` | `subscription` or `api`. On `update`, `none` clears it. |
| `--monthly-fee` | A subscription's flat monthly fee in US dollars, such as `200`. Requires `--billing subscription`. |

### Spend flags

On `create` and `update`, an account's spend policy is set with the shared
window flags (`cmd/quicbaas/spend_flags.go:64,66`) and the breaker flags
(`cmd/quicbaas/account_spend.go:61-65`):

| Flag | Meaning |
| --- | --- |
| `--spend-window name=NAME,size=DURATION[,tokens=N][,usd=DOLLARS]` | A sliding window. Repeatable; the set replaces the windows. |
| `--clear-spend` | Remove every window |
| `--breaker-failures N`, `--breaker-window D`, `--breaker-cooldown D` | The failure breaker. All three together. |
| `--clear-breaker` | Remove the breaker |

On `update`, new windows keep the breaker and a new breaker keeps the windows.

```bash
quicbaas --tenant acme account update acct_abc123 \
  --spend-window name=five-hour,size=5h,tokens=2000000
quicbaas --tenant acme account update acct_abc123 \
  --breaker-failures 3 --breaker-window 1h --breaker-cooldown 30m
```

## quicbaas orchestrator spend-policy

`cmd/quicbaas/orchestrator_spend_policy.go`. Show or replace the tenant's own
spend limits: sliding windows over every turn charged to the tenant. When a
window is spent, admission defers every project of the tenant.

| Command | Line | Does |
| --- | --- | --- |
| `get` | `:59` | Show each window and its live usage |
| `set` | `:65` | Replace the limits with `--spend-window` (repeatable), or remove them with `--clear-spend`. Requires `spend_policy:write`. |

```bash
quicbaas --tenant acme orchestrator spend-policy get
quicbaas --tenant acme orchestrator spend-policy set \
  --spend-window name=monthly,size=720h,usd=2000
quicbaas --tenant acme orchestrator spend-policy set --clear-spend
```

The command's help text says tenant limits are not enforced at admission. That
text is stale; see [engineering.md](engineering.md#known-gaps).

## quicbaas orchestrator cost

`cmd/quicbaas/orchestrator_cost.go:63`. Show one scope of the consumption
ledger: tokens, list-price equivalent, billed cost, local energy (and how much
was measured), and breakdowns by host, model and the level below.

The scope is a path, so each flag needs every one above it (`:85-96`):

| Flag | Meaning |
| --- | --- |
| `--project` | Narrow to one project |
| `--task` | One task; requires `--project` |
| `--job` | One job; requires `--task` |
| `--session` | One session; requires `--job` |
| `--round` | One round; requires `--session` |
| `--from` | Only turns at or after this RFC 3339 instant |
| `--to` | Only turns before this instant; requires `--from` |
| `--host` | Only turns that ran on this host; `-` for turns that name none |
| `--agent` | Only turns of sessions that ran as this tenant Agent; `-` for raw-backend sessions. Also prints that Agent's spend windows. |

```bash
quicbaas --tenant acme orchestrator cost
quicbaas --tenant acme orchestrator cost --host gpu-host.internal
quicbaas --tenant acme orchestrator cost --agent claude-coder
quicbaas --tenant acme orchestrator cost --project p1 --task t1 \
  --from 2026-01-01T00:00:00Z --output json
```

## quicbaas orchestrator cost-settings

`cmd/quicbaas/orchestrator_cost_settings.go`. Show or replace how the tenant's
local inference is costed: the electricity price and each engine's device
draw. A local turn costs `watts × seconds / 3.6e6` kWh at the price.

| Command | Flags |
| --- | --- |
| `get` | None. Shows the settings, the values in force and where the price came from. |
| `set` | `--electricity-price` (US dollars per kWh), `--engine-watts ENGINE=WATTS` (repeatable), or `--clear` to go back to the configuration |

`set` replaces the settings, so give every value you want kept.

```bash
quicbaas --tenant acme orchestrator cost-settings get
quicbaas --tenant acme orchestrator cost-settings set \
  --electricity-price 0.15 --engine-watts openvino=12 --engine-watts ollama=250
quicbaas --tenant acme orchestrator cost-settings set --clear
```

## quicbaas orchestrator rotate-account

`cmd/quicbaas/orchestrator.go:452-479`. Move a running session onto a
different pooled account. The session keeps its identifier, transcript and
cost history; the replacement account opens a new attribution segment.

| Argument or flag | Meaning |
| --- | --- |
| `<sessionID>` | The session to move |
| `--reason` | Recorded on the closed account segment and in the transcript |
| `--hold-outgoing` | Hold the outgoing account out of the pool for the server's default backoff |

```bash
quicbaas --tenant acme orchestrator rotate-account sess_abc123
quicbaas --tenant acme orchestrator rotate-account sess_abc123 \
  --reason "seat retired" --hold-outgoing
```

Only a session spawned with `--account-auto` or `--account-allow` may rotate.
A bare `--account-id` pin is refused with a conflict error.

```
Session ID:        sess_abc123
Previous Account:  acct_a
Account:           acct_b
Strategy:          least_usage
Mode:              resume
```

`Mode` is `in_place` when the credential was swapped with no process restart,
or `resume` when the agent process was rebuilt and the transcript replayed.

## Spawn flags

`quicbaas orchestrator spawn` selects an account with four flags
(`cmd/quicbaas/orchestrator.go:215-222`). The rest of the spawn command is in
[../software-factory/cli.md](../orchestration/cli.md#orchestrator-spawn).

| Flag | Meaning |
| --- | --- |
| `--account-id <id>` | Pin one account. A bare pin is never rotated. |
| `--account-auto` | Select with a strategy, and allow rotation across the whole tenant, project and provider pool |
| `--account-strategy <name>` | `least_usage` or `static`. Empty selects the default (`least_usage`). Ignored without `--account-auto`. |
| `--account-allow <id>` | Restrict the session to these accounts and allow rotation between them. Repeatable or comma-separated. |

```bash
# Strategy-picked account, rotatable across the pool
quicbaas --tenant acme orchestrator spawn --backend claude --account-auto

# Start on a named account, rotatable only within the listed ones
quicbaas --tenant acme orchestrator spawn --backend claude \
  --account-id acct_a --account-allow acct_a --account-allow acct_b
```

`account_exclude` has no CLI flag; rotation sets it.
