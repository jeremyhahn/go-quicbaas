# Cost enforcement — the consumption ledger, the spend cap and the breaker

> **Status: NORMATIVE** · Last verified: 2026-09-24
> Supplement to the Software Factory feature folder: the consumption
> ledger, the per-account spend cap and the failure circuit breaker that
> guard every credential release. Conflicts with this document are defects.

A pooled provider account is a live paid credential. Before this, nothing
stopped work from spending against one: the token counters in
`pkg/orchestration/cost_tracker.go` REPORTED usage (ok / warning at 80% /
exceeded) and no code path refused anything on the strength of that report. A
runaway or compromised worker could spend without limit, and the first evidence
was the invoice.

This document describes the layer that turns a report into a refusal.

## The four pieces

| Piece | File | What it is |
| --- | --- | --- |
| Consumption ledger | `account_ledger.go`, `account_ledger_store.go` | The append-only, cross-host record of what was spent. The single authority. |
| Quota authority | `account_quota.go` | "May this account serve more work right now", from sliding token windows over the ledger. |
| Circuit breaker | `account_breaker.go` | Derived from the ledger's failure entries. No state of its own except the half-open probe. |
| Spend authority | `account_spend_authority.go` | The gate. Composes the two and is what the credential-release paths ask. |

## Why the ledger is not a counter

The counters are read-modify-write. Two hosts recording turns for one account
both read the same total and both write their own sum, and one turn's tokens
vanish. That is tolerable for an advisory budget and is not tolerable for the
number a limit decision is made against.

A ledger entry is keyed by its own idempotency key and written with the store's
guarded create: of N writers presenting one entry exactly one stores it, the
losers observe the stored bytes and agree, and no arithmetic is ever performed
on a value another host is concurrently changing. Totals are derived by summing
what is there, so a total can never be wrong in a way a re-read would not
correct.

The counters were kept. They are the observability surface, they feed the same
recording site, and `LeastUsageAccountStrategy` still balances on them.

### Five keyspaces over one record

Every entry is written to up to five planes, identically:

```
orch/costwindow/{tenant}/{account}/{bucket}/{occurredAtNanos}/{entryID}   pooled entries only
orch/costagent/{tenant}/{agent}/{bucket}/{occurredAtNanos}/{entryID}      entries of a tenant Agent's session
orch/costproject/{tenant}/{project}/{bucket}/{occurredAtNanos}/{entryID}  entries with a project
orch/costtenant/{tenant}/{bucket}/{occurredAtNanos}/{entryID}             every entry
orch/costchain/{tenant}/{project}/{task}/{job}/{session}/{round}/{entryID}
```

The **attribution plane** is the chain, so a roll-up at any level is a prefix
scan of that level's subtree. The four **quota planes** are keyed by account,
by tenant Agent, by project and by tenant, and then by the moment consumption
occurred, so a
window read costs what the window is worth rather than what the history is
worth — the hourly `bucket` segment is what bounds it. The project and tenant
planes (go-quicbaas#1058, ruling F2) are what a project's or a tenant's window
and every time-ranged insight read (`ProjectWindowUsage`, `TenantWindowUsage`).

The Agent plane is keyed by `resolved_agent_id`: the tenant Agent the turn's
session ran as, stamped on the session when a spawn by Agent or pool resolved
and kept on its durable record, so a restored or resumed session still
attributes to it. It is not the entry's `agent_id`, which is the control loop's
per-job identity. A raw-backend session's turns have no Agent plane.
`AgentWindowUsage` reads it.

The quota planes are written FIRST, account then Agent then project then tenant. A partial
write therefore leaves spend visible to the LIMIT and missing from the INVOICE,
never the reverse: over-refusing is recoverable and silent overspend is not.

### A level that does not apply is unattributed, not absent

A job that belongs to no task still has a task segment, filled with `-`.
Collapsing the segment would shorten the key and move the entry into the
subtree of whatever sits at the next position, so a project roll-up would start
counting session ids as tasks. `-` is refused as a caller-supplied value, so a
sentinel is never something a caller can also spell.

## Where a turn becomes a ledger entry

One site: `agentServiceImpl.recordTurnUsage` (`turn_usage_recorder.go`), called
from `runPromptTurn`. It writes the counters and the ledger together, so a turn
is either in both or in neither and the dashboard can never contradict the cap.

- The **turn identity** is minted when the turn is PLANNED
  (`planPromptTurn`), not when it is recorded. An id minted at the moment of
  recording would be new on every attempt, so a retried write would be a second
  charge. The same id is what an asynchronous turn's ack returns.
- The chain comes from `TurnAttribution` on the context. `JobRun.Prompt`
  attaches it for every runner that prompts through the job envelope, so a
  runner cannot forget; the control loop attaches a richer one (it knows the
  agent) and that is not overwritten.
- A **failed** turn is recorded too, with outcome `failure`, even when the
  backend reported no tokens. Without those entries the breaker can never
  trip: a ledger of successes says every account is healthy however hard it is
  failing.
- A turn whose backend reported **no token counts** is still ledgered, with
  no tokens on it. The counters skip it (a zero there would make its account
  look perpetually least-used to `LeastUsageAccountStrategy`), but the breaker
  reads occurrences rather than quantities — and for the backends that publish
  no counts at all, every success is unmeasured, so dropping them would leave
  an account that can trip and can never be untripped by the turn that proves
  it recovered. Which backends publish counts, and from where, is tabled in
  [accounts.md](accounts.md#which-backends-report-token-counts).
- Each **token dimension keeps its own field**: `input_tokens` is the fresh
  prompt, `cache_read_tokens` and `cache_write_tokens` the prompt tokens served
  from and written into the provider's cache. They price differently, and
  before go-quicbaas#1058 both were folded into input. The total every quota
  window sums is the sum of all four, so the split moved no cap.
- A turn that drew on **no pooled account** — local inference such as the
  OpenVINO triage model or a review model on Ollama — **is ledgered**
  (go-quicbaas#1058, ruling F1). The owner's 2026-09-23 request is that every
  model the factory runs is tracked. Such an entry has no `account_id`, is
  written to the project, tenant and attribution planes, and is **never**
  written to the account quota plane: an allowance belongs to an account, and
  a turn that drew on none drew on no allowance. It is not filed under a
  placeholder account, which would make local inference look like the
  tenant's largest pooled spender.
- Every entry carries **what the work was and where it ran**: `kind` (the
  job's runner — `sdlc` for a worker, `review`, `triage`, `workflow`), `host`
  (the worker host, or this node for a local session), `agent_id`, and
  `duration_ms`. The host is on the entry and in the API only; it is never a
  metric label, because hosts are unbounded.

## The policy lives on the account

```go
type AccountSpendPolicy struct {
    Quota   SpendLimits        // sliding windows
    Breaker *BreakerPolicy     // nil = never taken out of service for failing
}
```

An allowance belongs to the thing that is billed, and a provider meters a
login — which is what an `Account` records. Keeping it on the project instead
would let two accounts on one plan each be admitted up to the plan's whole
allowance.

The policy is validated at the WRITE (`AccountStore.Create` / `Update`). By the
time the gate reads a malformed policy the choice is between refusing an
account the operator believes is capped and admitting one they believe is
metered, and both are wrong.

**An account with no policy is unmetered.** The zero value meters nothing and
admits, because that is the only honest answer for an account nobody has
configured a limit for — inventing a default token allowance would be inventing
a fact about someone's plan, and a wrong one refuses work the provider would
have served. What a deployment's shipped default should be is an operator
decision, not this layer's.

### Windows are sliding

`[now-Size, now)`, recomputed on every question. That is how the plans this
models actually meter: a "5-hour limit" frees up gradually as old turns age
out. A tumbling window would hand back a full allowance the instant a boundary
passed, which is precisely the burst the provider is metering against.

### Windows meter tokens, dollars or both

A window (`QuotaWindow`) carries `max_tokens`, `max_equivalent_micros` or
both; zero leaves that axis unmetered and a window that meters neither is
refused at the write. When both are set, both are limits and the first spent
one refuses, naming its axis (`QuotaDecision.Axis`,
`AccountQuotaExhaustedError.Axis`: `tokens` or `equivalent_micros`).

**The dollar axis meters PRICED turns only.** It sums the list-price
equivalent the price book stamped (see [Pricing](#pricing-what-a-turn-costs-twice));
a turn on a model the table does not price moves no dollar window. That is
the honest reading of a dollar cap: an operator who wants every turn counted
prices every model, or caps tokens as well. And it refuses once the allowance
is SPENT rather than when an estimate would cross it, because a turn's price
is not known until it has run.

## Spend limits

One limits type (`SpendLimits`, go-quicbaas#1058 ruling F5) is carried in
four places, each judged by the same comparison (`evaluateWindows`) over its
own quota plane:

| Spender | Where the limits live | Measured over | Enforced |
| --- | --- | --- | --- |
| Account | `Account.Spend.Quota` (with the breaker) | the account's pooled turns | a candidate filter in selection, and at credential release (below) |
| Agent | `Agent.Spend` ([agents.md](agents.md#spend-limits)) | every turn of every session that ran as the Agent, pooled and local, whichever project it served | at spawn: a pool passes over it (`over_budget`); named alone it is refused (`AgentSpendExhaustedError`) |
| Project | `Project.Spend` | every turn charged to the project, pooled and local | admission defers the project (`project_spend`) |
| Tenant | `TenantSpendPolicy` at `orch/spendpolicy/{tenant}` | every turn charged to the tenant | admission defers every project of the tenant (`tenant_spend`) |

`QuotaAuthority.AdmitScope` answers for a project or a tenant from the project
and tenant quota planes, and admission asks it every sweep
(`admission_spend.go`, ruling F6); see
[admission-and-readiness.md](admission-and-readiness.md#the-four-inputs). A
tenant-level spend report carries the tenant's own windows, and a project-level
report the project's (`scope_policy_status`, `scope_windows`), with live usage
against each and `exhausted` on a spent one. A spender with no limits is
unmetered. `Project.Spend` replaced the lifetime `ProjectQuotas.TokenBudget`,
which nothing ever enforced.

### A spent account is filtered out before any strategy runs

The spend check is a **candidate filter** in `accountResolver.selectAccount`
(ruling F6), applied after the session cap and before the strategy ranks. Every
strategy — `static`, `least_usage`, and the daemon's pool — therefore fails
over past a spent account to a sibling with headroom. Before, the check was
asked of the one account the strategy picked, so a least-used account over its
window was selected, refused, and the spawn requeued onto the same refusal.

- The filter asks `SpendAuthority.Check`, which answers `Authorize`'s question
  **without claiming a half-open breaker's trial**; claiming it for every
  candidate would hold the probe on accounts the strategy passed over. The one
  account selected is still asked `Authorize`.
- If that `Authorize` refuses an automatic selection — the window was crossed,
  or another host won the trial, between the two reads — the account joins the
  spent side and selection runs again over the rest. A pinned account keeps
  its own refusal.
- When every candidate is spent the resolver returns `AccountsSpentError`,
  "the pool is spent until `AvailableAt`" (the earliest a window's oldest turn
  ages out, a breaker's cooldown ends, or a held trial's probe expires). It
  unwraps to `ErrNoAccountAvailable` and `ErrAccountsSpent`. It is a
  **deferral, not a park**: class `accounts_spent`, refunded, requeued at
  `AvailableAt`, and it never counts toward the three-fault park bound. A pool
  that is part spent and part cooling down reports whichever returns first.
- A check that cannot read the ledger fails the resolution. It is never read
  as headroom or as a spent pool.
- **The worker daemon** selects with the same resolver. Its spend authority is
  `AccountAPIClient.Check`, which reads the verdict the control plane puts on
  the usage call it already makes for live sessions:
  `GET /api/v1/agent-accounts/{id}/usage` → `spend`
  (`SpendHeadroom`: `admits`, `refusal` = `quota_exhausted`|`breaker_open`,
  `window`, `axis`, `used`, `limit`, `failures`, `frees_at`). The verdict is
  on every transport's usage verb and in `quicbaas account usage`. It is
  absent when the control plane wires no gate. The trial is claimed only by
  the control plane's credential release.

Every one is settable on every transport and the CLI, and validated at the
write with the operator's field named:

| | Account (go-quicbaas#781) | Project | Tenant |
| --- | --- | --- | --- |
| REST | `spend` on `POST`/`PUT /api/v1/agent-accounts` | `spend` on `POST`/`PUT /api/v1/projects` | `GET`/`PUT /api/v1/orchestrator/spend-policy` |
| gRPC (and Unix) | `AccountSpendPolicy spend` | `SpendLimits spend` | `OrchestratorService.Get/SetTenantSpendPolicy` |
| MCP | `spend` on `quicbaas_agent_account_create`/`_update` | `spend` on the project tools | `quicbaas_orchestrator_spend_policy_get`/`_set` |
| QUIC | the REST surface over HTTP/3 | ← | ← |
| Embedded, Go SDK | `account.CreateAccountRequest.Spend` (`spend.AccountPolicy`) | `project.*Request.Spend` (`spend.Limits`) | `Orchestrator().Get/SetTenantSpendPolicy` |
| TS SDK | `spend` (`AccountSpendPolicy`) | `spend` (`SpendLimits`) | `orchestrator.get/setTenantSpendPolicy` |
| CLI | `account create/update --spend-window … --breaker-…` | `project create/update --spend-window …` | `orchestrator spend-policy get/set` |

The wire shape is `api/proto/spend.proto`. On an update an absent value leaves
the limits unchanged and an empty one clears them. Reading a tenant's policy
is `orchestrator:status`, like the spend report; setting it is
`spend_policy:write`, Admin only among the built-in roles, in its own
permission family because go-qrdb owns `orchestrator:*`.

## Pricing: what a turn costs, twice

Every entry carries two money figures, stamped once when the turn is recorded
(go-quicbaas#791, #1058 rulings F3 and F4), in integer millionths of a US
dollar — summed as `float64` money does not associate, so two roll-ups over one
set of entries could disagree.

| Figure | What it is | Where it comes from |
| --- | --- | --- |
| `equivalent_micros` | What the tokens would cost at **list price** on the provider's metered API. The business figure for a factory that runs on subscriptions. For local inference it is the operator's configured hardware rate. | `PriceBook.Stamp` (`price_book.go`), from the price table |
| `billed_micros` | What the turn added to the bill **actually paid**. | `stampBilling` (`account_billing.go`), from how the paying account is billed |

### The price table

`orchestration.pricing` in `quicbaas.yaml` (`config.PricingConfig`): per model,
the four rates in US dollars per million tokens, because a fresh prompt token,
an output token, a cache read and a cache write all price differently.

```yaml
orchestration:
  pricing:
    version: "2026-09-23"          # required once any model is priced
    models:
      - model: claude-opus-4-5-20251101
        aliases: [claude-opus-4-5] # further exact names the CLI reports
        provider: anthropic        # the vendor spend is broken down by
        input_per_mtok: 5.00
        output_per_mtok: 25.00
        cache_read_per_mtok: 0.50
        cache_write_per_mtok: 6.25
      - model: qwen3-8b
        engine: openvino           # engine-specific beats the generic entry
        provider: local
        power_watts: 12            # local inference: costed from electricity
    electricity_price_per_kwh: 0.12 # the default when omitted
    engine_power_watts:
      ollama: 250                  # every ollama model the table does not list
    tenants:
      acme:                        # replaces the same (engine, model) entry for one tenant
        electricity_price_per_kwh: 0.30
        models:
          - {model: claude-opus-4-5-20251101, provider: anthropic, input_per_mtok: 4, output_per_mtok: 20}
```

Lookup is the tenant's table, then the deployment's, and within each the
engine-specific entry before the generic one. Names match **exactly** (plus
aliases). There is no prefix, family or fallback match, because each of those
is a price nobody wrote down. A tenant entry that re-prices a model also
answers to the aliases the deployment's table declares for it under the same
engine scope, so `opus` and `claude-opus-5` resolve to the same tenant price
and `price_ref`, and an alias that names another model on another engine is
never claimed. A name the
tenant's own table defines keeps the tenant's entry.

**An unknown model is unpriced, never guessed.** Its entry keeps its tokens,
carries no `price_ref`, and every reader renders its money as a dash. Local
inference is unpriced until the operator names its models. **A price is
stamped, not looked up at read time**: each priced entry records `price_ref`
(`{version}:{scope}:{engine|*}/{model}`), so a price changed next week never
re-prices this week's turns, and every figure can be traced to the sheet it
came from. A malformed table fails startup rather than stamping figures from a
table nobody meant.

### Local inference is costed from electricity

A local model has no list price, so its cost is not invented as a per-token
rate. It is costed from the energy the turn drew:

```
kWh     = power_watts × turn seconds / 3.6e6
dollars = kWh × electricity_price_per_kwh
```

The turn's seconds are its wall-clock duration, which the ledger already
records. **`electricity_price_per_kwh` defaults to $0.12.** It is a
configuration value because this is an open-source project and no site's
tariff belongs in the code.

| Input | Resolved from, first match wins |
| --- | --- |
| electricity price | the tenant's runtime cost settings, then `tenants.<t>.electricity_price_per_kwh`, then `electricity_price_per_kwh`, then $0.12 |
| device draw | the tenant's runtime `engine_power_watts[engine]`, then the tenant's table entry `power_watts`, then `tenants.<t>.engine_power_watts[engine]`, then the deployment's table entry `power_watts`, then `engine_power_watts[engine]` |

- **An explicit per-token rate wins.** A table entry with any `*_per_mtok`
  rate is priced by its rates, `power_watts` or not: it is the operator's
  precise figure.
- **A per-engine draw of 0 is a decision.** It switches electricity costing off
  for that engine at that scope, so a tenant can turn off a draw the deployment
  declares. A model entry's `power_watts: 0` just means no draw is declared.
- **Unknown duration is unknown cost.** A turn that would be costed from
  electricity but has no measured duration is recorded unpriced, never at $0.
  It keeps `price_basis: electricity` and counts as missing coverage.
- Every ledger entry records its `price_basis` (`token_rate` or `electricity`).
  An electricity entry also records its energy (`energy_mwh`, milliwatt-hours)
  and a `price_ref` naming the draw, the rate and where each came from, e.g.
  `2026-09-23:electricity:default:openvino/qwen3-8b@12W:tenant_settings@0.15/kWh`.
  Roll-ups sum `energy_mwh` and count `electricity_entries` on every scope and
  every project, model and account row. The Cost page shows the energy beside
  the dollars.

**Runtime cost settings.** The rate and per-engine draws are also settable
while the node runs, per tenant, in `orch/costsettings/{tenant}`
(`CostSettings`, `cost_settings.go`), and they beat the configuration. Reading
them is `orchestrator:status`; replacing them is the new `cost_settings:write`
permission (Admin). A caller may act only on its own tenant, unless it is of
the system tenant. The check is made against the caller's authenticated
identity before the request is scoped to the target
(`service.targetTenantContext`); any other caller is refused 403 /
PermissionDenied. The read returns the tenant's settings plus the rate and
draws in force, and where the rate came from.

| Surface | Read | Replace |
| --- | --- | --- |
| REST | `GET /api/v1/orchestrator/cost-settings[?tenant_id=]` | `PUT` same, `{"electricity_price_per_kwh": 0.15, "engine_power_watts": {"openvino": 12}}` |
| gRPC (and Unix) | `OrchestratorService.GetCostSettings` | `SetCostSettings` |
| MCP | `quicbaas_orchestrator_cost_settings_get` | `quicbaas_orchestrator_cost_settings_set` |
| Go SDK | `Orchestrator().GetCostSettings` | `SetCostSettings` |
| TypeScript SDK | `orchestrator.getCostSettings` | `setCostSettings` |
| CLI | `quicbaas orchestrator cost-settings get` | `cost-settings set --electricity-price 0.15 --engine-watts openvino=12` (or `--clear`) |
| UI | Cost page, "Local inference cost" | same panel |

A replace replaces everything: an absent rate or engine inherits the
configuration. A set that names nothing (`--clear`) restores the
configuration.

### Measured energy: fleet device telemetry

The formula above is the **estimate**. Where the fleet node that ran a turn
reports device telemetry, the turn is costed from the energy its device
measurably drew instead, and the estimate is used only when nothing measured
the window.

**What each node reports.** Every heartbeat carries one sample per device the
daemon can read (`pkg/fleet/devicetelemetry.go`), each value an
`orchestration.Reading` with its provenance, so an unread signal is never a
zero:

| Device | Source (no cgo) | Signals |
| --- | --- | --- |
| NVIDIA GPU | `nvidia-smi --query-gpu=uuid,name,power.draw,utilization.gpu,memory.used,memory.total`, 5 s timeout | power (measured), utilisation, memory |
| Intel NPU | `/sys/class/accel/accelN/device/npu_busy_time_us` (intel_vpu), `npu_memory_utilization` | busy fraction (derived from the counter), memory; the driver has no power sensor |
| CPU package | `/sys/class/powercap/intel-rapl:N/energy_uj` | power (derived from the counter, wrap-corrected) |

NVML has no pure-Go binding, and the daemon builds `CGO_ENABLED=0`, so the CLI
is the GPU path. RAPL counters are root-only on most distributions: an
unreadable counter reports the package with its power unread and is logged
once. The first sample of every counter-derived signal is unread, because
there is nothing to difference it against.

**Where it is filed.** The control plane files each report per node in the
qrdb node's time-series engine, tenant-scoped (`service.TenantScopedMetric`),
under `fleet.device.power_watts`, `.utilization`, `.memory_used_bytes` and
`.memory_total_bytes`, tagged `node`, `host`, `kind`, `device` and
`provenance` (`fleet_telemetry.go`). Telemetry is best-effort: a heartbeat is a
liveness signal first, and a storage failure is logged, never a dead node.

**How long it is kept.** Every `fleet.device.*` series carries a time-series
retention policy (qrdb `TSSetRetention`) keeping raw points for
`orchestration.pricing.telemetry_retention`, 14 days by default
(`fleet_telemetry_series.go`). The policy is set on every stored series at
startup and on a tenant's series at its first report after startup; setting it
twice is harmless. A refused policy is logged and retried on the next report.
Expiring points past the policy is the time-series engine's job, not
QuicBaaS's: as of go-qrdb v0.20.2 the engine stores the policy but does not
yet enforce it.

```yaml
orchestration:
  pricing:
    telemetry_retention: 336h   # 14 days; zero is the default
```

**Which device a turn used.** `orchestration.pricing.engine_devices` binds an
engine to a device class; there is no default binding.

```yaml
orchestration:
  pricing:
    engine_devices:
      ollama: gpu
      vllm: gpu
      openvino: npu
```

**How a turn's energy is worked out** (`turn_energy.go`). The window is the
turn's `[occurred_at − duration, occurred_at]` on the host that ran it (the
entry's `host`). For every device of the engine's class on that host, the
window is cut at each telemetry point and at each start and end of another
turn on the same host and device class; each piece is charged the device's
draw at its midpoint divided by the turns in flight there, so two concurrent
turns on one GPU are not each charged the whole GPU. A device's draw is its
power series; an NPU's is the configured draw × its measured busy fraction. A
point speaks for at most 90 s (three default heartbeats); a window any piece of
which no point covers is not measured, and falls back to the estimate.

Every electricity entry says which it was, in `energy_provenance`:

| Value | Meaning |
| --- | --- |
| `measured` | integrated from a power sensor (GPU) |
| `derived` | integrated from a counter-derived power (CPU package), or a measured busy fraction × the configured draw (NPU) |
| `estimated` | the configured draw × the turn's duration; nothing on the device was read |

A measured entry's `price_ref` names it, e.g.
`2026-09-23:electricity:measured:gpu@gpu-host:9091:ollama/review-q8:deployment_config@0.12/kWh`.
Totals carry `measured_energy_entries` beside `electricity_entries`, and the
Cost page says "3 of 5 measured".

What is measured today: a fleet node on a host with an NVIDIA GPU measures its
GPU turns; a host with an Intel NPU derives its NPU turns; RAPL needs read
access to `energy_uj`. A VM with no accelerator (vm-host) reports no
devices, so its local turns stay estimated.

### Late pricing: a system job prices what was recorded unpriced

A turn is priced when it is recorded. A turn whose model the table did not name
yet, or whose tenant cost settings could not be read, is recorded unpriced. The
`ledger.pricing` scheduler system job (`SystemJobIDFloor+2`, hourly,
`pkg/scheduler/handlers/ledger_pricing.go`, registered by
`pkg/server/ledger_pricing.go`) prices every such entry the table in force can
now price, in every tenant.

This is the ledger's **one admitted amendment** (`account_ledger_pricing.go`):

- It only prices. The priced record must equal the stored one in every field
  but the price, energy and billing figures, and a priced entry is never
  re-priced — a price changed next week still does not re-price this week's
  turns.
- It is decided by the store. Every plane is compare-and-swapped from the exact
  stored bytes, the quota planes before the attribution plane as `Record`
  orders them, so a partial amendment is finished by the next sweep, and two
  sweepers converge rather than conflict.
- Unmeasured turns and turns filed under model `unknown` stay unpriced. A
  tenant that cannot be read is logged and skipped.

### A turn's model

The model an entry is filed under is, most authoritative first: the model the
backend reports it served, the model the turn was planned with (the request's,
the project's default, the session's), and the backend's configured default.
When nothing names one, the entry is filed under `unknown`
(`ConsumptionModelUnknown`) and the gap is logged — the ledger refuses an entry
with no model, and dropping it would lose the turn's spend and its breaker
evidence. `unknown` is in no price table, so such a turn reads as unpriced,
never as free.

### Billing

How an account is billed is a fact about the account, not the model:
`Account.Billing` (`account_billing.go`), settable at create and update on
every transport and the CLI (`--billing subscription|api`, `--monthly-fee`).

| Account billing | A turn's `billed_micros` |
| --- | --- |
| `subscription` | `0`, a known figure: a flat fee does not move with use. `monthly_fee_micros` is the fee, recorded on the account and **pro-rated over a tenant-level report's range** (below); it is never smeared across turns, where it would depend on how many turns happened. |
| `api` | the list-price equivalent, when the model is priced |
| not configured | **unknown**, never zero. Inventing "subscription" would claim a fact about someone's plan. |
| (unpooled local inference) | its electricity or configured hardware cost, stamped as billing `local`. A turn is local when it is costed from electricity or priced by an entry whose provider is `local` |
| (any other unpooled turn) | **unknown**, stamped as billing `unpooled`: a cloud CLI on the host's own login has a plan nobody configured |

A roll-up sums each figure only over the entries that carry it, and counts the
rest: `priced_entries` / `unpriced_entries` and `billed_entries` /
`unbilled_entries`. An **unmeasured** turn (`unmeasured: true`: the backend
reported no token counts) is recorded with zero tokens and counted unpriced
and unbilled, because what it cost is unknown, not zero. The one exception is
a subscription turn, which bills zero whatever it consumed. No priced entry is
a dash; some unpriced beside priced makes
the figure a floor, and the UI says so: the scope's figures carry a note ("at
least — N turns unpriced"), and every project, account and model row marks a
floor with `≥` and explains it in a tooltip.

The control plane's codex backend runs `codex exec --json` and reads its
usage from `turn.completed`, like the worker daemons. When no model is named,
the turn is recorded under the model codex's `config.toml` pins
(`$CODEX_HOME`, else `$HOME/.codex`). If the config pins none, it is recorded
as `codex-cli-default`, because the CLI doesn't report its built-in default.
The price table can list that name like any other model. A turn succeeds
only when codex reports it completed with an answer. Anything else is a failed
turn, which is ledgered under the same model with whatever usage codex
reported.

A model name is recorded as the backend reported it, provider prefix included
(`ollama/review-q8:latest` from opencode). It is payload, not a ledger key
segment; only the engine is refused a `/`, which is what keeps the report's
`engine/model` split exact.

## Where a refusal happens

Two paths release a credential, and both ask the same authority:

| Path | Site |
| --- | --- |
| In-process spawn | `accountResolver.Resolve` → `checkSelected` → `authorizeSpend` (after the candidate filter above) |
| Remote fleet node daemon | `AccountService.MaterializeAccountCredential` |

Both ask BEFORE resolving the credential envelope, so a refused release never
renews an `oauth_refresh` account — a provider round trip and a rotation of
live secret material — on behalf of work that is about to be refused anyway.

### Fail closed, and never with a number

Every path that cannot establish the answer returns an error and NO admission.
A gate that admitted whenever it could not read the ledger would open exactly
when the store is in trouble, which from outside is indistinguishable from a
healthy quiet account. A read failure is also never reported as a refusal: an
operator told "the cap is spent" would raise a limit that was never reached.

### The breaker's one non-derived decision

Open/closed is a pure function of the ledger, so two hosts asking the same
question get the same answer because they read the same facts. "Tripped" means
exactly: at least `FailureThreshold` failures in the window AND no success
recorded since the most recent failure.

The one thing that cannot be derived is which host gets the half-open trial.
That is a guarded create on `orch/costprobe/{tenant}/{account}`, released by a
guarded delete. At the spawn gate the trial's only release is its TTL
(`spendProbeTTL`, one minute): a spawn admitted on a half-open breaker has no
single moment at which the trial is over, because its session may run for
minutes and its outcome reaches the ledger rather than the gate.

## What a refusal looks like to a person

| Error | Fault class | Behaviour |
| --- | --- | --- |
| `AccountQuotaExhaustedError` | `account_quota_exhausted` | A pinned account: requeues, waiting until the window's oldest entry ages out. `Axis` is `tokens` or `equivalent_micros`. |
| `AccountBreakerOpenError` | `account_breaker_tripped` | Requeues, waiting until the cooldown elapses. |
| `AccountBreakerTrialHeldError` | `account_breaker_tripped` | Requeues with no stated wait — it depends on another caller's turn. |
| `AccountLimitedError` | (existing) | The provider's own hold, reported in the pool's existing vocabulary rather than a second one. |
| `AccountsSpentError` | `accounts_spent` | An automatic selection whose every candidate is spent: requeues at `AvailableAt`, never counting toward the park bound. |

Neither new class parks on first sight: a window frees up and a breaker
retries, so putting a human in front of either would be putting them in front
of a condition that resolves itself. Three consecutive environment faults still
park the task, which is where a saturated cap reaches a person.

- The **dispatcher** writes the classified reason onto `Task.DispatchReason`,
  so "why is my task still pending?" is a field on the task.
- The **forge comment** (`pkg/sdlc/blocked.go`) renders a `**Cause:**` line
  from `Task.BlockFaultClass` plus `orchestration.FaultClassMeaning`, so a
  reader sees "the account's configured spend cap for the window is spent"
  rather than the generic "the task waits on an external condition".

## Metrics

Every turn the ledger records is also counted on two Prometheus counters,
after the ledger holds it, so the metrics and the spend report sum the same
turns (go-quicbaas#1058, ruling F7). They replace
`quicbaas_orch_agent_tokens_total`, whose `job_id` label grew a series for
every job and which nothing recorded.

| Metric | Labels |
| --- | --- |
| `quicbaas_factory_tokens_total` | `tenant`, `project`, `kind`, `provider`, `backend`, `model`, `direction` (`input`, `output`, `cache_read`, `cache_write`) |
| `quicbaas_factory_cost_micros_total` | the same, with `basis` = `equivalent` (priced turns) or `billed` (turns whose bill is known) |
| `quicbaas_factory_spend_deferrals_total` | `tenant`, `project`, `axis` (`project_spend`, `tenant_spend`), counted when a deferral starts or changes |

**Every label is bounded.** `model` is a name the price table lists (aliases
included, `PriceBook.Lists`) or `other`. `provider` is a vendor the price table
names, `local`, or `unknown`. `backend` is a configured engine. `project` and
`kind` are `none` for a turn with neither. The host, agent, job, task and
session are never labels. They are on the ledger entry and in the spend report,
where a question about one of them is a lookup and not a new time series. A
spent account pool is counted on the existing
`quicbaas_orch_account_pool_exhausted_total{reason="spent"}`.

## Wiring

`pkg/server/lifecycle_orchestration.go` builds the ledger over the same
tenant-routed orchestration KV as tasks/projects/accounts, so entries live
inside the owning tenant's barrier. The spend authority names the node as the
holder of a breaker's half-open trial. It is then attached to both release
paths: `WithSpendAuthority` on the resolver, `SetSpendAuthority` on the account
service.

A deployment with no ledger wired is unmetered and keeps working — the gate is
additive, and one that failed closed when absent would take every account
offline on upgrade.

## Reading the ledger: the spend report

The enforcement half above decides. The READ half — added in go-quicbaas#794 —
reports, and it is the same record: `orchestration.ReadSpendReport`
(`spend_report.go`) is a projection over `ConsumptionLedger.Rollup` plus the
account's own `AccountSpendPolicy`, so a dashboard and a refusal can never
disagree about one account.

### One verb for every level

The chain is a PATH — `tenant/project/task/job/session/round` — so a project's
spend and a job's spend are the same prefix scan at two depths. There is one
verb, scoped by the levels the caller supplies contiguously:

| Transport | Surface |
| --- | --- |
| REST | `GET /api/v1/orchestrator/cost?tenant_id=&project_id=&task_id=&job_id=&session_id=&round=&from=&to=&host=&agent=` |
| gRPC | `OrchestratorService.GetSpendReport` |
| QUIC | the REST route over HTTP/3 |
| Unix | the gRPC RPC over the domain socket |
| MCP | `quicbaas_orchestrator_cost` |
| Embedded | `quicbaas.Orchestrator/GetSpendReport` |
| Go SDK | `Orchestrator().GetSpendReport(ctx, req)` |
| TypeScript SDK | `orchestrator.getSpendReport(req)` |
| CLI | `quicbaas orchestrator cost [--project --task --job --session --round --from --to --host --agent]` |

Permission: `orchestrator:status`, the existing orchestrator read family. A
spend report names token counts and an account's id and label; it never
carries credential material.

A scope that skips a level is REFUSED rather than answered by a filtered scan
of the tenant's whole history, which is the same rule `Rollup` already applied.

### A time range, and the subscription fees in it

`from`/`to` (`ConsumptionScope.From`/`To`) narrow any scope to the turns that
occurred in `[from, to)`. Both or neither reach the ledger; the report closes a
range given only `from` at now, and refuses `to` alone. A tenant scope reads
the tenant quota plane and a project scope the project quota plane (ruling F2),
one scan per hourly bucket the range overlaps, with the drill-down row read off
each entry. Deeper scopes are small subtrees and are scanned and filtered.

A **tenant-level** report pro-rates every fee-carrying subscription of the
tenant over the range (ruling F4): `AccountBilling.FeeShare` is
`monthly_fee_micros × range / mean Gregorian month` (30.436875 days), integer
micro-dollars rounded down. Each share starts no earlier than the account's
registration. An idle subscription is listed with no consumption beside its
fee, because an unused plan still costs its fee. An unranged report shares
fees from the first recorded turn to now. The shares are summed as
`subscription_fee_micros` beside the per-turn `billed_micros`. They are not
folded into it, because a turn's bill and a flat fee are different facts.
Below the tenant level no fee is shared: one subscription serves every
project, and charging its whole fee to the project being read would count it
once per project.

### By host

Every report breaks its totals down by the host each turn ran on (`hosts`,
sorted, the unattributed row last). `host` narrows any scope to one host's
turns: like the range it is a filter over the scope's own subtree, not a chain
level, so it costs nothing extra and composes with every level. `-` selects
the turns that name no host. A host-narrowed tenant report carries no
subscription-fee share, because a fee belongs to an account, not to a host.

### By Agent

Every report also breaks its totals down by the tenant Agent each turn's
session ran as (`agents`, with each Agent's name, the raw-backend row last).
`agent` narrows any scope to one Agent's turns, a filter exactly like `host`;
`-` selects raw-backend sessions. Narrowed to an Agent, the report carries that
Agent's own limits with live usage against each window (`agent_policy_status`,
`agent_windows`), read from the Agent quota plane a spawn is checked against.
The status is `unknown` when no agent catalog is wired or the Agent no longer
exists. The Cost page shows the Agent rows, narrows on a click and edits the
narrowed-to Agent's limits in place.

### Addressing the unattributed subtree

A job that belongs to no task is filed under the ledger's no-value sentinel
(`-`). A scope may NAME that sentinel to open the subtree, and this is the one
place the write and read rules deliberately differ: the write side refuses it
as an entity id, which is exactly what makes it unambiguous as a scope value.
Without it the subtree would be reportable and not addressable — a roll-up
showing a row nothing can drill into. Exported as
`orchestration.SpendUnattributed`, `orchestrator.SpendUnattributed` (Go SDK)
and `SPEND_UNATTRIBUTED` (TypeScript SDK).

### What the report refuses to claim

Every figure the system does not have is absent and labelled, never a zero:

| Field | Rule |
| --- | --- |
| `equivalent_micros` / `billed_micros` | Accompanied by `priced_entries` / `unpriced_entries` and `billed_entries` / `unbilled_entries`, on the report and on every account, model and drill-down row. **No priced (billed) entry means the figure is a dash, never a zero**; unpriced (unbilled) entries beside priced ones make it a floor. `currency` names the unit. |
| `accounts[].policy_status` | `metered` / `none` / `unknown`. `none` means the account was READ and meters nothing; `unknown` means nothing could read it. Collapsing the two would let an unwired deployment report every account as uncapped. |
| `project_name` / `children[].name` | A project's name, from the project store. Absent for a project that no longer exists — the ledger outlives what it names, so its spend stays under its id — and a project store that cannot answer FAILS the report, the same rule as the account store. |
| `children[].unattributed` | The no-value bucket, with `id` deliberately EMPTY so no reader can print the sentinel by accident. |
| `first_at` / `last_at` / `frees_at`, and the breaker's instants | Absent rather than the zero instant, which a client would render as the year 1. They are POINTERS in Go, because `omitempty` does NOT omit a zero `time.Time` — for a struct field `encoding/json` treats nothing as empty, so a non-pointer would always have serialised, and an absent moment would have crossed as `0001-01-01T00:00:00Z` on REST, MCP and the desktop binding (which re-marshals the SDK type). Guarded by tests that read the JSON rather than the struct, since the struct cannot tell the two encodings apart. |

A deployment that records no consumption answers `503` / `Unavailable`, not an
empty report: "nothing is being recorded" and "nothing has been spent" are
opposite facts, and only one of them is reassuring. For the same reason an
account store that cannot ANSWER fails the report, while an account that is
merely GONE does not: the ledger outlives the accounts it names, so a deleted
account keeps its spend with an unknown allowance, but a store fault rendered
as "nobody configured a cap" would be a fault presented as a fact.

**`exhausted` is about the allowance, not about a request.** It says
`used_tokens` has reached `max_tokens`, so nothing is left. At exactly
`used == max` the quota gate still admits a SPAWN, because a spawn passes an
estimate of zero and the refusal is `used + estimated > max`; a turn
estimating even one token is refused. The two answer different questions from
one record, and the report deliberately does not take the gate's estimate into
account — it has none.

### What the UI shows

`CostView.svelte` is the tenant's roll-up with a drill-down along the chain,
and `JobDetailView.svelte`'s Cost tab is that job's entries. Both read through
`cmd/quicbaas/frontend/src/lib/cost.ts`, which owns the rendering rules so they
are testable rather than re-derived in a template.

**Where the money went, by host.** A By host card shows each host's tokens,
money and energy, with how much of the energy was measured. A host row narrows
the whole page to that host; the filter survives drilling, and its chip clears
it.

**Limits are edited where they are read.** At the tenant level the scope's
spend limits carry an Edit action (`TenantSpendLimitsEditor.svelte`,
get/set tenant spend policy). A pooled account's display name, billing (a
subscription's monthly fee, or metered API), max concurrent sessions and spend
windows are edited from the Accounts page (`AccountEditDialog.svelte`,
`lib/accountEdit.ts`); the account's breaker rule is carried over unchanged.

**A row reads as its name.** A project row renders the project's `name` and
keeps the id as its tooltip and its drill value (`childLabel`,
`childIsNamed`); the breadcrumb names the project it opened (`scopeLabel`).
Before go-quicbaas#1058 the By Project card listed bare ids.

**The iteration gauge renders only for a runner that iterates.** Before #794
the Cost page showed "0.0% Iterations" and a per-job breakdown of `0/10`:
those are the `workflow` runner's fields, they are structurally zero for every
other runner, and they were never a cost. The job page keeps them, for the
jobs that have them; a job driven by any other runner says so in place. Which
runner iterates is `isWorkflowJob` in `lib/jobDetail.ts` — the job page's own
rule from #796, not a second copy.

**Spend, unlike the gauge, is EVERY runner's.** go-quicbaas#796 hid the job
page's Cost tab for the sdlc runner, correctly at the time: the tab then held
nothing but that gauge and a job-memory token panel no production writer
populates. Once the tab reads the ledger, the reason evaporates — every runner
burns provider tokens — and keeping it hidden would leave an sdlc job's real
spend unreachable on its own page. The tab is offered to both runners again
and only its gauge is runner-gated. The DAG tab remains the workflow
runner's.

## Related

- [accounts.md](accounts.md) — the pool itself and the release verb.
- [autonomy-and-dispatch.md](../orchestration/autonomy-and-dispatch.md) — where a refusal lands
  in the dispatch loop.
- [sdlc.md](sdlc.md) — how the Software Factory's turns reach the ledger.
- go-quicbaas#758 — the salvage this layer came from.
- go-quicbaas#794 — the read surface and the UI that reads it.
- go-quicbaas#791 — the price source the cost axis waits on.
