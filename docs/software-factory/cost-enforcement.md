# Cost enforcement — the consumption ledger, the spend cap and the breaker

> **Status: NORMATIVE** · Last verified: 2026-09-23
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

### Two keyspaces over one record

Every entry is written twice, identically:

```
orch/costchain/{tenant}/{project}/{task}/{job}/{session}/{round}/{entryID}
orch/costwindow/{tenant}/{account}/{bucket}/{occurredAtNanos}/{entryID}
```

The **attribution plane** is the chain, so a roll-up at any level is a prefix
scan of that level's subtree. The **quota plane** is keyed by account and by
the moment consumption occurred, so a window read costs what the window is
worth rather than what the account's history is worth — the hourly `bucket`
segment is what bounds it.

The quota plane is written FIRST. A partial write therefore leaves spend
visible to the LIMIT and missing from the INVOICE, never the reverse:
over-refusing is recoverable and silent overspend is not.

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
- A turn that drew on **no pooled account** is not ledgered. The quota plane is
  keyed by account because an allowance belongs to an account; such turns stay
  visible in the counters.

## The policy lives on the account

```go
type AccountSpendPolicy struct {
    Quota   AccountQuotaPolicy // sliding token windows
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

### There is one axis, and it is tokens

wp12 metered tokens and integer micros independently. The micros half was
re-homed and then **removed**, because nothing in this tree prices a turn:
`recordConsumption` is the single production writer of ledger entries and it
sets no `CostMicros`, so every entry carries zero. A `MaxCostMicros` window
would therefore sum zero and admit every turn forever — a knob an operator
sets believing it is a cap, which is this layer's own founding failure
reproduced on the money axis. Shipping it before the price source is worse
than not shipping it, because it looks like protection.

`ConsumptionEntry.CostMicros` is kept as the slot a price lands in, and **zero
on it means UNMEASURED, not free**. Nothing may render it as a measured
figure — an unmeasured figure is a dash, never a zero. go-quicbaas#791 adds
the per-model price source, the cost axis and the reported column together, so
no one of the three can ship claiming the other two.

When it does return, money stays an integer: summed as `float64` it does not
associate, so two roll-ups over one set of entries could disagree.

## Where a refusal happens

Two paths release a credential, and both ask the same authority:

| Path | Site |
| --- | --- |
| In-process spawn | `accountResolver.Resolve` → `authorizeSpend` |
| Remote agent daemon | `AccountService.MaterializeAccountCredential` |

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
| `AccountQuotaExhaustedError` | `account_quota_exhausted` | Requeues, waiting until the window's oldest entry ages out. `Axis` is always `tokens` today. |
| `AccountBreakerOpenError` | `account_breaker_tripped` | Requeues, waiting until the cooldown elapses. |
| `AccountBreakerTrialHeldError` | `account_breaker_tripped` | Requeues with no stated wait — it depends on another caller's turn. |
| `AccountLimitedError` | (existing) | The provider's own hold, reported in the pool's existing vocabulary rather than a second one. |

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
| REST | `GET /api/v1/orchestrator/cost?tenant_id=&project_id=&task_id=&job_id=&session_id=&round=` |
| gRPC | `OrchestratorService.GetSpendReport` |
| QUIC | the REST route over HTTP/3 |
| Unix | the gRPC RPC over the domain socket |
| MCP | `quicbaas_orchestrator_cost` |
| Embedded | `quicbaas.Orchestrator/GetSpendReport` |
| Go SDK | `Orchestrator().GetSpendReport(ctx, req)` |
| TypeScript SDK | `orchestrator.getSpendReport(req)` |

Permission: `orchestrator:status`, the existing orchestrator read family. A
spend report names token counts and an account's id and label; it never
carries credential material.

A scope that skips a level is REFUSED rather than answered by a filtered scan
of the tenant's whole history, which is the same rule `Rollup` already applied.

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
| `cost_micros` | Accompanied by `cost_measured`. **Nothing prices a turn**, so the flag is false on every production report and a reader must render a dash. Deriving the flag from the data rather than hard-coding it is what lets go-quicbaas#791's price source land without every reader changing. |
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
- [autonomy-and-dispatch.md](autonomy-and-dispatch.md) — where a refusal lands
  in the dispatch loop.
- [sdlc.md](sdlc.md) — how the Software Factory's turns reach the ledger.
- go-quicbaas#758 — the salvage this layer came from.
- go-quicbaas#794 — the read surface and the UI that reads it.
- go-quicbaas#791 — the price source the cost axis waits on.
