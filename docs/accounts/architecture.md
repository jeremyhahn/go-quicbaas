# Accounts — Architecture

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Design, component boundaries and data flow. Feature folder: `docs/accounts/`. Conflicts with this document are defects.

A **pooled provider account** is one provider login that an agent session can
be spawned against instead of the host's shared identity: one Anthropic/Claude
seat, one OpenAI/Codex API key, or one OpenCode provider config. A tenant holds
a pool of them. A spawn names one account, or asks a strategy to pick one.

This folder covers four related mechanisms:

| Mechanism | Question it answers |
| --- | --- |
| [The account pool](#the-account) | Which login does this session run on, and how does the credential reach the process that needs it? |
| [The spend cap](#spend-limits) | May this account, Agent, project or tenant spend more right now? |
| [Cost enforcement](#the-consumption-ledger) | What did each turn consume, and what did it cost? |
| [Job admission](#job-admission) | Which node owns a job, how is its outcome charged to its task, and who concludes a job whose node died? |

The pool lives on the **control plane** and the agent runs on a **worker
host**. Most of this design is about the seam between the two: how a
credential gets from the pool to the process that needs it, and what stops it
from going anywhere else.

Sessions, the job envelope and the dispatch loop are described in
[../orchestration/architecture.md](../orchestration/architecture.md). Worker
hosts and their daemons are in
[../agent-fleet/architecture.md](../agent-fleet/architecture.md). The SDLC
runner that consumes the pool is in [../sdlc/architecture.md](../sdlc/architecture.md),
and projects are in [../projects/architecture.md](../projects/architecture.md).

## Components

All code is in `pkg/orchestration` unless a path says otherwise.

| Component | File | Role |
| --- | --- | --- |
| `AccountStore` | `account_store.go` | Account metadata in the tenant's orchestration KV, at `orch/account/{tenant}/{id}`. Every write is a compare-and-swap. |
| `AccountSecretStore` | `account_secret_store.go` | The credential envelope, in the platform `SecretsManager` under a tenant-specific DEK. |
| `AccountResolver` | `account_resolver.go` | Turns a selection request into environment variables and files ready for a spawn. It is the same code on the control plane and on every worker daemon. |
| `AccountStrategyRegistry` | `account_strategy*.go` | The named selection strategies: `least_usage` (the default) and `static`. |
| `AccountSessionReserver` | `account_balancing.go` | Live-session counts and the reservation table that enforces `max_concurrent_sessions`. |
| `AccountTokenManager` | `account_token_manager.go` | Renews credentials before use, single-flight per tenant and account. |
| `OAuth2TokenRefresher` | `account_refresher_oauth2.go` | A provider-agnostic RFC 6749 §6 `refresh_token` grant client. |
| `AccountCredentialLeaser` | `account_lease.go` | Delivery leases over `pkg/lease.LeaseManager`. |
| `service.AccountService` | `pkg/service/account_service.go` | Tenant-enforced CRUD, the release verb, and reservations. It never returns credential material except from the release verb. |
| `ConsumptionLedger` | `account_ledger.go`, `account_ledger_store.go` | The append-only, cross-host record of what was consumed. It is the single authority for spend. |
| `QuotaAuthority` | `account_quota.go` | Answers from sliding windows over the ledger: may this spender serve more work now? |
| Circuit breaker | `account_breaker.go` | Derived from the ledger's failure entries. |
| `SpendAuthority` | `account_spend_authority.go` | The gate. It composes quota and breaker, and the credential-release paths ask it. |
| `PriceBook` | `price_book.go` | Stamps each entry's list-price equivalent from the configured price table. |
| Scope spend admission | `admission_spend.go` | Defers a project whose own or whose tenant's spend windows are spent. |
| `JobLeaseStore` | `job_lease.go` | The durable lease that makes one node the owner of a job. |
| Attempt accounting | `task_attempt.go` | Classifies a job's outcome and charges or refunds its task's attempt budget. |
| `JobReaper`, `JobStallDetector` | `job_reaper.go`, `job_stall.go` | Conclude jobs whose node died or whose agent vanished. |

## The account

### Two halves that never meet

Every account is stored as two records:

| Half | Where | What it holds |
| --- | --- | --- |
| Metadata | `AccountStore`, a tenant-scoped KV record at `orch/account/{tenant}/{id}` | ID, provider, project scope, status, expiry, usage timestamps, billing, spend policy, session cap. It is safe to list, log and return. |
| Credential envelope | `AccountSecretStore`, encrypted under a tenant-specific DEK (`StoreOptions.TenantID`) | The access token, the refresh grant, and provider config file bytes. |

That split is what makes an operator's `list` safe. `Account` has no
credential field, and no API response carries one, with exactly one deliberate
exception: [the release verb](#the-release-verb).

`service.AccountService` scopes every request's context to the request's
tenant. An account that a system administrator creates for tenant T is written
into T's namespace, where T's spawn path reads it.

### Where the credential comes from

An account takes its credential from exactly one of two sources. Supplying
both or neither is refused at write time with `400`, naming the field
`credential` or `credential_secret_ref`.

| Source | Set with | Stored | Rotated by |
| --- | --- | --- | --- |
| **Secret reference** (recommended) | `credential_secret_ref`, plus optionally `credential_secret_file` | Nowhere new. The account names a secret in its tenant's secrets store. | Writing a new version of that secret |
| Inline | `credential` | The account's own envelope | `rotate` on the account |

**A reference is a name, not a value.** `credential_secret_ref` is the short,
tenant-relative secret name, such as `agents/claude/claude1/oauth_token`. It is
the same kind of name an SDLC binding's `forge.token_secret_ref` uses. The name
lives on the metadata record and is returned by `get` and `list`. The value
never is. Secret references are described in
[../secrets/dynamic-secrets.md](../secrets/dynamic-secrets.md).

**It resolves in the account's tenant, every time.** Every resolution reads the
secret through the tenant `SecretsService`, scoped to the account's own
`tenant_id`, whatever tenant the caller's context carried
(`service.NewAccountSecretResolver`, `pkg/service/account_secret_ref.go:49`).
That covers both an in-process spawn and the release verb that serves a worker
host. A system administrator who provisions tenant T's account gets T's
secret. A same-named secret in the system keyspace or in another tenant is
never read. Nothing is cached on the account, so writing the secret is the
rotation, with no account write.

**How the value becomes a credential:**

- Without `credential_secret_file`, the value is the access token, with
  surrounding whitespace trimmed. It is projected into the provider's
  access-token variable, such as `CLAUDE_CODE_OAUTH_TOKEN` for `claude`. A
  provider with no such variable (`codex`, `opencode`) must name a file
  instead, and is refused at write time if it does not.
- With `credential_secret_file`, the value is written verbatim as that file,
  relative to the per-session home: `.codex/auth.json` for a codex account.
  The path must be clean and relative.

**Rules checked at write time** (create, and an update that sets a reference):

- A referenced account is `refresh_policy: none`. QuicBaaS never renews it,
  because renewal would have to write a rotated refresh token back into a
  secret the operator owns.
- The reference is a short name. It is not `tenant/<id>/...` (the storage
  prefix the store adds itself), and it has no empty, `.` or `..` segment and
  no whitespace.
- The secret must resolve now, in the account's tenant, to a non-empty value. A
  reference that does not resolve is the operator's request to fix, not the
  first spawn's surprise.

**When the secret goes missing later.** A spawn whose account's secret is
absent, empty, disabled or unreadable fails with
`AccountSecretRefUnresolvedError`, which unwraps to
`ErrAccountCredentialNotFound`. The work item parks uncharged with class
`account_credential_missing`, exactly as for a missing inline envelope. The
account is put in `status: error` with a `last_error` that names the reference
and the failure class, never the underlying error text. It drops out of
selection and appears in the attention inbox. The caller's own context ending
marks nothing.

A spawn on a **remote fleet node daemon** behaves the same way. The control
plane refuses the daemon's release with `409` and
`"fault_class": "credential_not_found"` in the error body. The daemon's client
rebuilds `ErrAccountCredentialNotFound` from that class, so its spawn reports
the same fault and the work parks uncharged there too. The class, not the
status, carries the meaning: a revoked delivery grant is also a `409` and is
not a configuration fault.

**Switching an inline account to a reference.** An update that sets
`credential_secret_ref` moves an inline account onto a reference. The record
switches first, then the inline envelope is destroyed. A crash in between
leaves an orphaned envelope, never an account with no credential. If the
destroy fails, the update reports it, and retrying the same update finishes
the job: every update of a referenced account idempotently destroys any inline
envelope it still has. `rotate` is refused on a referenced account, because
its rotation is a secret write. Deleting an account never deletes the secret it
names; the operator owns that secret.

**Workers never see a reference.** The fleet node daemon's account wire shape
has no reference field, and its token manager has no resolver. A worker host
receives the resolved envelope through the release verb, or nothing. That
release is marked `single_use` (see [Rotation reaches the next
spawn](#rotation-reaches-the-next-spawn)), so a secret write reaches a worker
host's very next spawn too.

### Conditional writes

**Updates are conditional.** `UpdateAccount` reads the account, applies the
request, and writes with `AccountStore.UpdateIfUnchanged`, which lands only if
the stored record's `updated_at` is still the one it read. A write that lost a
race re-reads and re-applies the request, up to five times. Two concurrent
updates, such as a rename and a switch to a secret reference, both take
effect, and neither can save a stale copy over the other. Revoke uses the same
loop.

**The backend decides, not a lock.** The service layer and the orchestration
runtime each build their own account store over one KV, and every daemon has
its own, so no in-process lock can order their writes. Every write of an
existing account record commits with the KV's compare-and-swap against the
exact bytes it read. That covers `UpdateIfUnchanged`, `Update` and `Touch`
(usage and refresh stamps, status, cooldown, expiry). A write that loses is
never applied. An update reports `AccountVersionConflictError` and the service
re-reads. A touch re-reads and re-applies its field changes itself. A store
without compare-and-swap is refused rather than written to unconditionally.

**A rotation cannot resurrect a switched account's envelope.** An inline
`rotate` checks that the account is inline, writes the new envelope, then
stamps the record with `RequireInlineCredential`, which the store decides on
the record it commits over. If a switch to a secret reference committed in
between, it destroyed the inline envelope the rotation has just re-written. The
stamp is then refused with `AccountCredentialSourceChangedError`, nothing
reaches the record, and the rotation deletes the envelope it wrote. Automatic
OAuth renewal is held to the same rule: its success and failure stamps require
the account to still be inline. A renewal overtaken by a switch removes the
envelope it wrote back and returns the same error instead of handing out the
renewed token.

**Automatic stamps never undo an operator.** Every stamp the runtime makes on
its own, such as a renewal's outcome or a failed secret read, refuses to
replace `revoked`. A failed-read stamp is also conditional on the account
still reading the same reference as last applied: `credential_secret_ref`,
`credential_secret_file` and `last_refreshed_at` all unchanged. An operator who
re-points the reference, or who writes the secret and re-applies the same one,
has already answered that failure, so the stale `error` is not stamped over a
working account.

**A failed secret read reports no backend detail.** The refusal's message
names the reference and a failure class ("the secret does not exist in this
tenant", "the secret could not be read") and nothing else. The underlying
secrets-service error stays in process. It is not reachable through the error
chain, so no transport can serialize that service's diagnostics in place of
the sanitized message.

### The default account

Exactly one account per **scope** may be the default. A scope is
`(tenant, provider, project)`, where `project` is the account's own
`ProjectID`, or the tenant-wide scope when it has none. That is the same
partition `AccountResolver` builds its candidate list from. A project's default
is what an unpinned spawn in that project uses, and it never becomes what
another project, or an unscoped spawn, resolves to.

The default is held as a durable pointer,
`orch/accountdefault/{tenant}/{provider}/{scope}`. That pointer is the **sole
authority**. `Account.Default` is derived from it on every read and is never
written into the account record. A list reads one pointer per distinct scope,
not one per account.

Deriving rather than mirroring makes "exactly one default per scope" true by
construction. `StaticAccountStrategy` selects on this field, so two records
that both claimed it would silently redirect an unpinned spawn, and its
billing, to a different provider login. With one key answering the question,
there is no second copy to drift.

| Event | Effect on the scope's default |
| --- | --- |
| First account created in a scope | It becomes the default. |
| Later accounts created | Non-default, unless the request sets `default`, which displaces the incumbent. |
| `SetDefault` | Re-points the pointer in one locked step. |
| The default is deleted | The scope is left with **no default**. No survivor is promoted. |
| The default is revoked | The pointer is unchanged. A revoked account is never a candidate. |
| An account moves between scopes (`Update` changing `project_id`) | Treated like a create into the destination: it claims that scope if vacant, arrives non-default if not, and releases the scope it left. |

The create-time claim, the move re-evaluation and `SetDefault` all take the
same per-scope lock, so concurrent imports into an empty scope produce exactly
one default.

**Locking.** There are two keyed mutexes and one rule: **`defaultMu` is a
leaf.** `recordMu` (per account record) may be held while taking `defaultMu`
(per scope pointer). Nothing may be taken while holding `defaultMu`. Because
`Default` is derived, `SetDefault` is a single-key write and never needs the
previous holder's record lock, so two `SetDefault` calls that displace each
other cannot deadlock.

**Why deleting the default promotes nobody.** Choosing among the survivors is
the ambiguity `StaticAccountStrategy` refuses to resolve by guessing.
Promoting by age would redirect every unpinned spawn, and its billing, to a
different login with no operator action. Instead, one survivor still resolves
through the lone-candidate rule, and several fail loudly with
`NoAccountAvailableError` until the operator runs `set-default` or adds the
next account, which claims the vacancy.

### Lifecycle

- **Create.** `AccountService.CreateAccount` writes the credential before the
  metadata record, so a crash between the two writes never leaves a listable
  account with no credential. It claims the scope's default when the scope has
  none, and mirrors the credential's declared expiry onto
  `CredentialExpiresAt`.
- **Rotate.** `RotateAccountCredential` replaces the envelope only. Display
  name and default are untouched, and an `error` state is cleared.
- **Revoke.** `RevokeAccount` sets `revoked`. A revoked account is excluded
  from selection on the next resolution. A session that already captured the
  credential is not killed: revocation stops new use and does not tear down a
  running subprocess.
- **Delete.** Releases the scope's default pointer first, then removes metadata
  and credential together. Releasing the pointer first means a delete that
  returns an error has left the account intact rather than orphaning its
  credential.
- **Refresh failure.** `error` with `last_error` set. The account is excluded
  from selection until the next successful refresh or an operator acts.
- **Provider limit.** `limited` with `available_at` set. Unlike every other
  non-active state, this one heals itself. See [Limit holds](#limit-holds).

**Credential health.** `Account.CredentialHealth()` derives `unknown`,
`valid`, `expiring` or `expired` from `CredentialExpiresAt`, where `expiring`
means within `AccountCredentialExpiryWarning` (24h,
`account_types.go:126`). It is reported as `credential_health` and never
stored: `Account.MarshalJSON` adds it on the way out, and the durable write
bypasses that method. `unknown` is the honest answer for a static token whose
expiry the operator never declared; QuicBaaS does not contact a provider to
discover one.

`CredentialExpiresAt` is written from exactly two places:

- `CreateAccount` and `RotateAccountCredential`, from the operator-declared
  `AccountCredential.ExpiresAt`. A rotation to a credential with no declared
  expiry clears it.
- A successful `oauth2` renewal, from the token endpoint's `expires_in`,
  together with `LastValidatedAt`.

### Credential generations

A session materializes its pooled credential once, at spawn, and never
re-reads it. Whether its next prompt can succeed depends on one question: is
the copy this session holds still the copy the account holds? That is an
identity comparison, not a comparison of two clocks.

Every copy of an account's material carries a generation
(`AccountCredential.Generation`):

| Credential source | Generation | Moved by |
| --- | --- | --- |
| Inline | Stamped by `AccountSecretStore.Replace` into the same encrypted body as the material: the write instant in Unix nanoseconds, strictly increasing per process | `CreateAccount`, `RotateAccountCredential`, an `oauth2` renewal |
| Secret reference | The referenced secret's version number | Writing the secret |

The generation cannot be published apart from the material. It is an integer
naming a copy, not credential material, so it travels freely.

- **The session records the copy it took** before its container starts:
  `SessionRecord.AccountGeneration`, and the matching field on each
  `AccountHistory` segment. A remote worker takes part the same way: the
  released credential carries `generation` to the fleet host, and the host
  reports it back as `SpawnSessionResponse.account_generation`.
- **The account's current generation is read from the authority.**
  `AccountTokenManager.CredentialGeneration` reads the envelope, or the
  referenced secret, and nothing else. `Account.CredentialGeneration` mirrors
  inline generations onto the metadata record and answers only when no
  authority is wired.
- **`SessionAccountFreshness(ctx, sessionID)`** reports whether a live session
  has been left behind. Every case where the answer is not knowable reports
  "not stale", because replacing a healthy session on missing evidence is
  worse than the failure the caller already has. The SDLC runner is the
  consumer; see
  [../sdlc/architecture.md](../sdlc/architecture.md#a-rotated-account-resumes-the-session-or-starts-it-fresh).
- **Generations are ordered within one source.** An account switched from
  inline to a reference has sessions whose recorded generation is not
  comparable with the secret's version, and they are not reported stale by the
  switch alone. `Account.CredentialRotatedAt` is the fallback evidence for a
  session that carries no generation.

## Selection

Selection is a pure function over the metadata half.

- **`account_id`** pins one account. No strategy runs: that account or
  nothing.
- **`account_auto`** runs a strategy. The default is `least_usage`.
- **`account_allow` / `account_exclude`** narrow the candidate list for one
  resolution. Rotation uses `account_exclude` to guarantee forward progress.
- **An Agent's `account` policy** supplies the strategy and allow-list for
  every spawn by that Agent, unless the spawn pins `account_id`. See
  [../software-factory/agents.md](../software-factory/agents.md#accounts).

An account is a candidate only if it is selectable now: `active`, or `limited`
with a hold that has lapsed. `revoked` and `error` need an operator. A static
token whose declared expiry is less than two minutes away is not handed to a
new session (see [The expiry buffer](#the-expiry-buffer)).

A spawn that sets neither `account_id` nor `account_auto` does not use the
pool. The CLI then authenticates however it already does, against the daemon
host user's own `~/.claude`, `~/.codex` or `~/.config/opencode`.

### Strategies

`AccountSelectionRequest{TenantID, ProjectID, Provider, PinnedAccountID}` goes
through one of two strategies:

- **`least_usage`** is the default on the control plane and on every worker
  daemon (`NewBalancedAccountStrategyRegistry`). It ranks candidates by, most
  significant first:
  1. live sessions: an account serving none beats one serving two;
  2. recorded usage: cumulative tokens from `CostTracker.GetAccountUsage`.
     Tokens, not request count, because a request count would treat a
     one-line prompt and an hour-long agentic session as equal load;
  3. the default account, so `default` means "preferred among equals", never
     "always";
  4. least recently used (`last_used_at`);
  5. the account ID, so the answer is deterministic.

  An account at its `max_concurrent_sessions` cap is not a candidate. A pool
  whose every candidate is capped answers `AccountsSaturatedError`.
- **`static`** uses the pinned account when set, otherwise the scope's default,
  otherwise a lone remaining candidate. More than one undesignated candidate is
  `NoAccountAvailableError`, never a silent pick.

A project-scoped selection that finds no candidates falls back to the
tenant-wide pool for the same provider before failing. New strategies register
into `AccountStrategyRegistry` by name with no change to `AccountResolver`.

### Live sessions and the session cap

The live-session count is read from durable **session records**
(`NewAccountSessionCounter` over the agent store). Every record in state
`active` or `idle` counts against its `account_id`, whichever host resolved the
account. Delivery leases are the wrong signal: a worker daemon caches a
released credential per account and serves every session on that account
under one grant, so a lease count undercounts by exactly the concurrency being
measured.

The count also includes every reservation no record has satisfied yet, so a
slot claimed by a spawn in flight is seen by everyone at once.

One lookup has four readers:

| Reader | Uses it to |
| --- | --- |
| The resolver (`WithAccountSessionReserver`) | Drop capped candidates before the tenant-wide fallback decision, and reserve a slot on the account it selects |
| `LeastUsageAccountStrategy` | Rank candidates on the counts the resolver already read |
| Admission (`WithHeadroomSessions`) | Treat a capped account as having no headroom |
| `GetAccountUsage` (`live_sessions`, and `spend`) | Report it, so a worker daemon balances on the control plane's figures |

The cap is applied before the fallback decision on purpose. A project whose
own accounts are all capped falls back to the tenant-wide pool, just as a
project whose own accounts are all held does. Only when the fallback pool is
capped too is the resolution `AccountsSaturatedError`. A count that cannot be
read fails closed with `AccountSessionCountError`, both at selection and at
admission.

`Account.MaxConcurrentSessions` is the operator's statement of how many
sessions the subscription serves at once. **0 is unmetered.** QuicBaaS cannot
discover this figure; it is a property of the provider plan.

At **admission**, an account at its cap has no headroom. A project whose pool
is entirely capped is deferred and its share goes to other projects. When every
ready account is capped, the project may run at most the pool's free sessions
(`PoolHeadroom.SessionBounded`, `SessionSlots`). Two projects that share
accounts can still race for the last slot. The reservation settles that race:
the loser gets `AccountsSaturatedError`, class `accounts_saturated`, which is
refunded and requeued, never parked.

### Reservations

Counting records alone lags the truth. A session record is written only after
its spawn succeeds, so a burst of resolutions all read the pre-spawn count. The
control plane's session counter is therefore also a **reservation table**
(`AccountSessionReserver`, `account_balancing.go:223`), and every resolution
claims a slot in it.

1. **Occupancy is counted for every account, and the cap is enforced only
   where one is set.** Every resolution reserves on the account it selects,
   pinned or automatic, metered or not. The count and the claim are decided
   under one per-tenant lock, with the session records read inside it. An
   account already at its cap is refused with `AccountsSaturatedError`.
2. **Selection and claim are one decision.** An automatic selection claims
   conditionally on the live count it selected by (`ExpectLive`). If another
   claim moved that count, the claim is refused with
   `AccountSessionCountChangedError`, and the resolver selects again on the new
   counts, up to `maxClaimAttempts` (8, `account_resolver.go:210`). The final
   attempt claims unconditionally, with the cap still enforced. A burst of N
   resolutions converges within N attempts and spreads across the pool. A
   pinned account has nothing to re-select to and is claimed unconditionally.
3. **A reservation is satisfied only by the record that names it.** The
   reservation ID travels from `ResolvedAccountCredential.ReservationID` onto
   the `Session`, across the daemon's spawn response (`reservation_id`), and
   onto `SessionRecord.ReservationIDs`. A resume or rotation adds its own
   reservation. Nothing is matched by account, job or time.
4. **Give-back and expiry.** If anything later in the resolution fails, the
   resolver gives the slot back. If the spawn fails, the ACP backend does
   (`AccountReservationReleaser`). A reservation that is neither named nor
   released expires after `AccountSessionReservationTTL` (15 minutes,
   `account_balancing.go:188`).

A **worker daemon** selects its account itself, so it reserves against the
control plane's table rather than one of its own; a per-host table would let
every host crowd onto the same account. The daemon uses the reserve and
release verbs described in [api.md](api.md#reservations).

The table lives in the control-plane process. A second control-plane node
would keep its own table, and the counts would then hold per node. See
[engineering.md](engineering.md#known-gaps).

**A resume re-claims its own slot.** After a restart, a persistent session
keeps its durable status and has no process. Its resume pins the same account
and names the session it reconstitutes (`SpawnRequest.ReplacesSessionID`). The
tally leaves that session's own record out, so a session on a cap-one account
does not refuse its own resume. The field stays on the control plane.

### The expiry buffer

A new session is not started on a credential that expires within
`AccountCredentialExpiryBuffer` (2 minutes, `account_balancing.go:59`). This
applies only to credentials QuicBaaS cannot renew: a static token with a
declared expiry. An `oauth2` account is renewed at resolve, and an account with
no recorded expiry is unaffected.

Naming an account does not bypass the buffer: a pinned new session on an
account inside it is refused with `AccountCredentialExpiringError`. Only a
resume, which carries `ReplacesSessionID`, is exempt.

### When the whole pool is unavailable

The whole pool can be found unavailable in two places, and both report through
`reportAccountPoolExhausted` (`account_pool_exhaustion.go:92`):

- **At spawn**: a resolution that finds every candidate on a limit hold
  (`AccountsCoolingDownError`), at its cap (`AccountsSaturatedError`), or spent
  (`AccountsSpentError`).
- **At admission**: a project whose pool has no headroom is deferred and no
  spawn is attempted. Admission asks every sweep, so it reports on the
  transition: once when the pool goes to zero, again only if the reason
  changes, and again after the pool has recovered and emptied once more. The
  provider label is `any`, because admission asks before it knows which
  provider a task will name.

Each report goes to three channels: an audit event
(`orchestration_account` / `pool_exhausted`, resource ID
`pool/<project|_none>/<provider>`), the counter
`quicbaas_orch_account_pool_exhausted_total{tenant,provider,reason}`, and a
warning on the daemon log. An empty pool is a configuration fault and is not
reported as exhausted.

## Delivery

### In-process: the control plane runs the agent

The resolver reads metadata from the KV and the credential from the
barrier-backed secret store, merges the provider descriptor's defaults, and
projects the result into the subprocess. Nothing crosses a network.

### Remote: a fleet node daemon runs the agent

The control plane dispatches a spawn to the daemon carrying only the
**selectors**: `account_id`, `account_auto`, `account_strategy`,
`account_allow` and `account_exclude`. **The credential is never in a spawn
RPC.** The daemon resolves it for itself:

```
daemon spawn
  └─ orchestration.AccountResolver          ← the same resolver the control plane runs
       ├─ orchestration.AccountStore        ← pkg/fleet: GET /api/v1/agent-accounts
       ├─ orchestration.AccountTokenManager ← pkg/fleet: read-through credential store
       │    └─ POST /api/v1/agent-accounts/{id}/materialize
       └─ orchestration.AccountStrategyRegistry
            └─ GET /api/v1/agent-accounts/{id}/usage
```

Only the two reads underneath the resolver change. Which account gets chosen
is identical on both sides by construction, not by two implementations
agreeing. The daemon's calls use the channel it already has: the mTLS client
certificate the deploy flow issued and the bearer its enrollment minted, the
same `*http.Client` and `TokenSource` its heartbeat uses. Daemon enrollment is
described in [../orchestration/architecture.md](../orchestration/architecture.md).

**Wiring.** On the control plane, `pkg/server/lifecycle_orchestration.go` hands
the account service the same `AccountTokenManager` the in-process resolver
uses, so an `oauth2` account is renewed on the side that owns its refresh token
before the envelope is released. Without that wiring the release verb fails
closed with `503`. On the daemon, `cmd/quicbaas/fleet_serve.go` composes the
resolver with `agent.NewAccountPool` over the daemon's control-plane client and
token source, and `Runtime.SetAccountResolver` attaches it to every ACP backend
the daemon serves. A daemon with no resolver refuses every pooled spawn with
`account: resolver not configured`.

### The release verb

`MaterializeAccountCredential` is **the only verb in the stack whose response
body is secret material.** It is correspondingly narrow.

- **Authorization.** It requires `agent_accounts:materialize`, which is not the
  read verb that lists the pool. An identity that may see the pool's metadata
  must not thereby be able to extract every login in it. An enrolled daemon
  holds it through the tenant-scoped `agent-runtime` role
  (`iam.FleetRuntimeGrant`).
- **Tenancy is the caller's own.** The tenant comes from the authenticated
  principal, never from the request. A `tenant_id` naming anything else is
  refused `403`, and a principal with no tenant binding is refused outright.
- **The refresh grant does not travel.** The released envelope carries the
  access token, the environment variable to project it into, static env and
  file material, the expiry and the generation. It carries no refresh token,
  no token endpoint and no client id. Renewal is the control plane's job and
  already happened inside the call that produced the envelope.
- **One audit event per attempt**, success or refusal, naming both principals:
  which agent took which account.
- **The release is the use.** The control plane stamps `LastUsedAt` and
  `LastValidatedAt` when it releases. For an account served to a remote daemon,
  `LastUsedAt` therefore means "last released to a worker host".

The refusals are tabled in [api.md](api.md#the-release-verb). A refusal whose
cause is a spawn configuration fault carries its class in the error body's
`fault_class`, using the same vocabulary as gRPC spawn evidence
(`orchestration.SpawnFaultClass`). The daemon decodes the whole error body,
bounded at 64 KiB, before it shortens the message to 512 bytes, so a long
refusal never loses its class. A class the daemon does not know is ignored,
leaving the refusal an ordinary charged failure.

### Delivery leases

Every hand-over of a pooled credential takes a **delivery lease**: a grant
that is short-lived, audited, and revoked when the session that asked for it
ends. It composes `pkg/lease.LeaseManager`, the same authority
`Security().DynamicSecrets()` issues through (see
[../secrets/dynamic-secrets.md](../secrets/dynamic-secrets.md)). A delivery
grant therefore appears in the same `ListLeases` and is revocable with the same
`RevokeLease` as every other lease.

The provider credential cannot be removed from the container: the CLI dials the
provider itself. What the lease removes is the credential's **durability**.

**What a lease governs.** `AccountCredential.ExpiresAt` is the access token's
real lifetime, and `account_refresher_oauth2.go` is the authority that extends
it. A lease says nothing about how long the token works. It says how long one
delivery stays authorized without renewal, so the two cannot fight:

- a lease lapsing does not shorten a token the refresher would have kept alive;
- a token being refreshed does not extend a lease, because the next delivery
  takes its own grant;
- a lapsed or revoked lease stops the next delivery.

Delivery grants are renewable with **no renewal cap**. The bound that matters
is the session's conclusion, which revokes outright. `DefaultAccountDeliveryTTL`
is one hour (`account_lease.go:88`, configured as
`orchestration.account_delivery_ttl`), the backstop for a session whose
conclusion never ran because its node was destroyed.

| Path | Where the grant is issued | How the session learns it |
| --- | --- | --- |
| In-process spawn | `accountResolver.Resolve` | Directly: `ResolvedAccountCredential.LeaseID` |
| Remote release to an enrolled daemon | `MaterializeAccountCredential` | The daemon reports it back on the spawn response (`pkg/fleet/account_grant.go`) |

A release that cannot obtain a grant fails, because a credential nothing can
revoke is the state the mechanism exists to remove. A deployment with no lease
manager wired (the sealed window) releases unleased rather than losing the
capability.

**The remote path needs a return journey.** The control plane issues the grant
when it releases the credential, but the release happens inside the daemon's
account resolution, so the control plane cannot tell which session the grant
belongs to. Only the daemon can. Its resolver composes an
`AccountDeliveryGrantor` that issues nothing; it reports the grant its release
was served under, read from the credential cache. The grant then travels
`ResolvedAccountCredential.LeaseID` → `Session.CredentialLeaseID` →
`SpawnSessionResponse.credential_lease_id` → `SessionRecord.CredentialLeaseIDs`.
`AccountDeliveryGrantor` is separate from the full `AccountCredentialLeaser`
because a daemon holds no lease store, so `Validate` and `Revoke` are not
verbs it could implement.

**The enforcement point** is `MaterializeAccountCredential`:

- **No lease presented** means a first delivery. A grant is issued, governed by
  the account status, project fence and spend cap.
- **A lease presented** means a refresh of a copy the caller already holds. The
  grant is validated, and revoked, expired or wrongly scoped grants are all
  refusals.

The daemon's read-through cache presents the grant it was released under on
every refresh (`pkg/fleet/account_secrets.go`). Without that, every refresh
would look like a first delivery and revocation would be unenforceable.

**A daemon's grant is scoped `(tenant, project, account)`, not by job.** Its
credential cache is keyed by account, so one release serves every job on that
host, and a grant naming one job would be false. An in-process resolution knows
its job and names it.

A grant's scope is checked. A live lease for another account, tenant, project
or job is refused with `AccountLeaseScopeError`. `AgentID` is deliberately not
checked, because the transport binds it from the authenticated principal.

Revoking one delivery stops the host holding that copy from replacing it. It
does not disable the account. Disabling the login is the account status
control's job (`revoked`).

**The reaper is a security control.** `SessionRecord.CredentialLeaseIDs`
carries every grant a session was handed, one per hand-over, so a session that
rotated carries the grant of every account that served it. It is on the
durable record because the route that matters most has nothing left in memory:
the node died. `session_reconcile.go` concludes such a record from the store
alone. `concludeBackend` withdraws the grants, and its failure is returned: a
session whose grant could not be revoked is not concluded, and the next pass
retries.

A shared grant is withdrawn by its last holder. A daemon's grant is per account
on that host, so two concurrent sessions can share it. The conclusion asks the
durable session records who else is still served under each grant
(`credentialLeasesHeldElsewhere`) and withdraws only the grants nothing live
still holds. A store that cannot answer that scan fails the conclusion rather
than revoking on assumption. A rotation withdraws the grant it supersedes in
the same step as recording the replacement. A spawn whose resolution succeeded
and whose dispatch then failed returns no session, so `ACPBackend` withdraws
that grant itself.

**No secret in any lease or audit record.** Lease metadata is identifiers only:
kind, tenant, account, provider, project, job and agent. No field of an
`AccountCredential` is ever written to a lease, a log line or an audit event.

### The daemon's own store

`pkg/fleet` supplies an `interfaces.SecretsManager` whose reads are served from
the release verb, and plugs it into the ordinary `AccountSecretStore`. Every
write-ish method refuses with a typed error, because each one would otherwise
mean the daemon believes it owns credential material it does not.

**The credential lives in the daemon's process memory and nowhere else.** A
worker host has no barrier, no KV and no encrypted-at-rest backend, so the only
durable option would be a plaintext file on the least-trusted host in the
deployment. Persistence would buy only survival across a daemon restart, which
costs one mTLS round trip to recover.

| Envelope | Served from cache until |
| --- | --- |
| Declares an expiry | Two refresh skews before it, capped at 15 minutes |
| Declares none (typical static token) | 15 minutes |
| Already inside its renewal margin | Not cached |
| Released `single_use` (a referenced account) | Not cached: re-released for every spawn |

The two-skew margin makes the token manager's renewal path unreachable on the
daemon, which holds no refresh token: it re-materializes first and lets the
control plane renew.

### Rotation reaches the next spawn

An operator rotating an inline credential reaches the worker host on the very
next spawn, with no polling and no push:

1. `rotate` replaces the envelope and stamps `last_refreshed_at` on the
   metadata record.
2. The daemon's resolver reads that record before any credential, because
   selection comes first.
3. A `last_refreshed_at` newer than the one the cached envelope was released
   under evicts the entry, and the next read re-materializes.

`last_refreshed_at` is the signal rather than `updated_at`, because
`updated_at` moves on any write, including the usage stamp the release itself
makes.

A **referenced** account is rotated by writing its secret, which moves neither
timestamp. The control plane therefore marks such a release `single_use`, and
the daemon serves it to the spawn that asked and does not keep it. The price is
one release, and its audit event, per spawn of a referenced account.

### Materialization

- **Environment credentials** (a single token or API key, such as
  `CLAUDE_CODE_OAUTH_TOKEN` or `CODEX_API_KEY`) are merged into
  `SpawnRequest.Env`. A raw-exec spawn has no disk footprint. In a sandbox,
  the environment is handed to the container runtime as a private
  `--env-file` (mode `0600` in a `0700` per-launch directory), never as
  `-e KEY=VALUE` arguments, which would publish the credential in
  `/proc/<pid>/cmdline`. Log lines print `[redacted]` in place of every value.
  The file is removed when the run process exits, or on `Close` for a
  long-lived sandboxed session (`pkg/orchestration/container_env_file.go`).
- **File credentials**, such as OpenCode's `auth.json` or a codex
  `.codex/auth.json`, are written into a fresh per-session temporary directory
  (`materializeAccountFiles`), never into the operator's real config. In host
  mode, the subprocess's isolated `HOME` points at that directory. In container
  mode, the directory is bind-mounted read-only at the expected config path,
  replacing the fallback mount of the host's shared config. The directory is
  released on session `Close`.

No credential from one account is ever visible to a session that resolved a
different account, and a session with no account selected is unaffected.

## Limit holds

An account that hit a rate, quota or usage limit is not broken: the credential
is fine, and the account is busy. QuicBaaS holds the account out of the pool
and moves the session to another one.

`limited` plus `Account.AvailableAt` is **self-healing**. There is no sweeper:
a lapsed hold is normalized back to `active` in memory on every decode, and the
next write persists it. A limited account with a zero `AvailableAt` is held
indefinitely, because nothing promised when it would recover. A **pinned**
resolve against a held account fails with `AccountLimitedError`, carrying
`AvailableAt`, rather than being silently swapped.

### Detecting a limit

The backend that read the CLI's stream decides, where the process ran, and
checks structured signals before any text:

| Source | Signal |
| --- | --- |
| `rate_limit_event` | A message whose `rate_limit_info.status` is `rejected`. Its `resetsAt` (epoch seconds) is the reset. |
| `error_category` | An assistant message whose `error` is `rate_limit` or `billing_error`. `overloaded` and `server_error` are provider-wide and are not limits. |
| `http_status` | `api_error_status: 429` on the result. |
| `result_text` | Only when none of the above fired: the text rules below. |

The verdict (`TurnFailureEvidence.ProviderLimit`: source, reset, redacted
refusal text) crosses the daemon wire in the `TurnFailureEvidence` status
detail (`provider_limit_source`, `provider_limit_resets_at_unix`,
`provider_limit_signal`). A source the receiving build does not know is still a
limit and is renamed `unclassified`.

The reset is `resetsAt` when present. Otherwise it is parsed from the text: a
machine-readable hint first, then the wall-clock form `resets 9pm
(America/New_York)` or `resets Sep 23, 10am (UTC)`. A clock time is read as
its next occurrence in the named IANA zone, and the zone is required. The zone
database is embedded (`time/tzdata`).

The text rules are a **heuristic** fallback, because QuicBaaS does not sit
between the CLI and the provider. They are deliberately narrow and fail in the
cheap direction:

| Class | Examples | Effect |
| --- | --- | --- |
| strong (shared) | `too many requests`, `rate limit exceeded/reached`, `quota exceeded`, `insufficient_quota`, `usage limit reached/exceeded` | limit |
| strong (claude) | `hit your session limit`, `hit your weekly limit`, `you've hit your limit`, `session limit reached`, `claude usage limit reached`, `5-hour limit`, `weekly limit reached` | limit |
| strong (codex) | `exceeded your current quota`, `check your plan and billing` | limit |
| guarded 429 | `429` and one of `status`, `http`, `code`, `rate`, `limit`, `quota`, `retry` | limit |
| veto | `overloaded_error`, `service unavailable`, `context_length_exceeded`, `context window`, `prompt is too long`, `rate limit configuration`, `rate limiting disabled` | not a limit |

The vetoes matter more than the matches: rotating on a provider overload or a
context overflow would walk the whole pool through the same failure. Retry
hints (`retry-after` in seconds, milliseconds or HTTP date, and epoch
`resets_at` / `rate_limit_reset`) are clamped to `(0, MaxAccountLimitBackoff]`
(24h). With no hint, `DefaultAccountLimitBackoff` (5h) applies
(`account_limit_classifier.go:73,78`). The matched evidence is stored on
`last_error` after redaction, so a provider that echoes the presented token
cannot turn a metadata record into a copy of the credential.

### Every turn path holds the account

`agentServiceImpl.runPromptTurn` holds the account itself when a turn fails on
a provider limit (`holdOnAccountLimit`). The account is set `limited` until the
stated reset, `last_error` records the refusal, and
`quicbaas_orch_account_limit_holds_total` is incremented. The turn's error is
`TurnAccountLimitedError`, which unwraps to both `AccountLimitedError` and the
turn's own evidence. The synchronous path (every job runner) and the
asynchronous path (operator sessions, which also rotate) share this.

### A rejected login errors the account

A provider can refuse the account's login rather than its allowance: a `401`,
`token is expired`, `refresh token already used`, `sign in again`, or `Please
run /login`. No reset lifts that; a person has to log the account in again. The
backend decides where the process ran (`TurnFailureEvidence.ProviderAuth`), and
the verdict crosses the daemon wire as `provider_auth_rejected` and
`provider_auth_signal`. A bare `401` counts only next to `unauthorized`,
`status`, `http` or `auth`.

When the control plane sees the verdict (`errorOnAccountAuthRejection`), it
puts the account in `error` with a `last_error` that begins `re-login needed:`,
and returns `TurnAccountAuthRejectedError` (`ErrAccountAuthRejected`,
`account_auth_rejection.go:175`). Downstream:

- the SDLC worker rolls its session onto another account, once per account,
  audited `class=account_auth_rejected`;
- a failed attempt is classified `account_auth_rejected`, refunded and
  requeued;
- a spawn walking an Agent reference fails over to the next Agent.

### Rotating a session onto another account

Rotation is authorized per session, and a deliberate pin is honored:

| Spawn request | Rotatable? |
| --- | --- |
| Neither `account_id` nor `account_auto` | No: the session does not use the pool |
| `account_id` only | **No**: a deliberate pin |
| `account_auto` | Yes: the whole tenant, project and provider pool |
| `account_allow: [...]` | Yes: exactly those accounts |

`account_allow` is an additive filter, never a source: an ID the tenant does
not own is not a candidate. The policy is persisted on `SessionRecord`
(`AccountAuto`, `AccountAllow`), and `SessionRecord.CanRotateAccount()`
(`session_store.go:378`) is the single place the decision is made. Remote
sessions are rotated by the daemon that owns them.

`agentServiceImpl.RotateSessionAccount`
(`orchestrator_account_rotation.go:333`) runs under the same keyed lock as
resume. It holds the outgoing account, selects a replacement with the outgoing
account excluded outright, and puts the session back in service by the
cheapest mechanism:

| Spawn path | Mechanism | Cost |
| --- | --- | --- |
| One-shot CLI | `in_place`: `ACPBackend.RotateAccount` swaps the session's stored env map | Free; no process dies |
| Persistent stream-json CLI | `resume`: close, then reconstitute with a fresh selection | One replay |
| Sandboxed container JSON-RPC | `resume`: close, then build a new container around a freshly materialized credential directory | One replay, charged as a real prompt |
| Unsandboxed host JSON-RPC | None | The agent cannot restore a conversation |

The in-place swap is a replacement, not a merge: the environment variables the
outgoing account projected are recorded at resolve time and retracted before
the replacement's are merged.

Automatic rotation runs after the terminal `turn_failed` event, so the
transcript records that the turn failed and then that the session moved. Every
rotation appends an `account_rotated` session event. If the replacement cannot
be selected after the old process is gone, the session is returned to the
resumable state with the reason on `SessionRecord.ResumeError`.

**The SDLC worker** rolls its session off a refused account
(`rollOffAccount`): it pushes what the session committed, closes it, and
spawns a fresh session on the same branch in the same round. The attempt is not
charged. Each account is rolled off at most once per run. When no account is
free, the spawn fails with `AccountsCoolingDownError`, which requeues the task
uncharged until the first hold lifts. The runner's side is in
[../sdlc/architecture.md](../sdlc/architecture.md#a-refused-account-rolls-the-session-onto-another-account).

### Attribution across a rotation

`SessionRecord.AccountID` is the account in force now. The full answer is
`SessionRecord.AccountHistory`, a sequence of segments
(`AccountAssignment{AccountID, Strategy, From, To, Reason}`) opened and closed
together with the scalar by `SessionRecord.OpenAccountSegment`
(`session_store.go:444`). Turns are attributed per segment: the in-memory
record's account ID is an atomic that rotation updates at the moment the move
commits, so no account is billed for another's tokens.

## Provider registry

Every account belongs to a **provider**. A provider descriptor supplies the
OAuth defaults an account did not state, and decides whether the provider's
accounts may run the refresh grant at all.

| Provider | OAuth-capable | Ships a token endpoint | Ships a client id | Access-token env key |
| --- | --- | --- | --- | --- |
| `claude` | yes | no, operator-supplied | no, operator-supplied | `CLAUDE_CODE_OAUTH_TOKEN` |
| `codex` | yes | no, operator-supplied | no, operator-supplied | none, operator-supplied |
| `opencode` | no (file-based `auth.json`) | n/a | n/a | none |

Neither vendor documents the OAuth application its CLI uses as a supported
API, so QuicBaaS ships no endpoint or client id for either rather than a
guessed value. The codex CLI reads its OAuth material from its own `auth.json`,
so it has no honest access-token variable; an operator pooling API keys sets
`access_token_env_key` (for example `OPENAI_API_KEY`) or `env` on the account.

**Precedence.** The account's own `AccountCredential` field (`token_endpoint`,
`client_id`, `scopes`, `access_token_env_key`) always wins. The descriptor
fills each field the account left empty. Descriptor values are projected onto
a copy of the envelope for one resolution and never persisted, so a changed
provider default takes effect on the next spawn. `OAuth2TokenRefresher` reads
`AccountCredential.TokenEndpoint` and nothing else.

A provider becomes OAuth-capable when it is a built-in that says so, or when
configuration gives it a token endpoint. Configuration is in
[engineering.md](engineering.md#configuration).

**Refresher registration.** `pkg/server/lifecycle_orchestration.go` builds the
registry from configuration, hands it to `AccountTokenManager`
(`WithAccountProviders`) and to the write path
(`AccountService.SetProviderRegistry`), then calls
`AccountTokenManager.RegisterOAuthRefreshers` once with a single
`OAuth2TokenRefresher`. That installs a refresher for every OAuth-capable
provider. Without it, an `oauth2` account fails its first renewal with
`AccountRefresherNotConfiguredError`.

**What works.** A static token (`refresh_policy: none`) works end to end with
nothing to configure. `oauth2` renewal is complete code that needs two values
QuicBaaS deliberately does not ship, `token_endpoint` and `client_id`. Supply
them per account or once per provider. Supply neither and the account is
rejected at create time with `AccountProviderEndpointUnresolvedError`.
QuicBaaS does not run an interactive OAuth enrollment; the operator bootstraps
each account out of band and imports the result.

**The SDLC worker requires an account.** The worker is spawned into the SDLC
module's own `CLAUDE_CONFIG_DIR` with the operator's `CLAUDE_*` variables
scrubbed, which is a directory with no credential in it. On the backends the
server pools (`claude`, `claude-code`, `opencode`) the worker spawn sets
`AccountAuto` and names the item's project. If no account is registered, the
dispatch starts no process and the item parks uncharged with
`no provider account for <provider> in tenant <t>`. How SDLC attempts are
charged is in [Attempt accounting](#attempt-accounting).

## The consumption ledger

A pooled account is a live paid credential. Usage counters alone report
usage; the cost layer turns that report into a refusal.

| Piece | File | What it is |
| --- | --- | --- |
| Consumption ledger | `account_ledger.go`, `account_ledger_store.go` | The append-only, cross-host record of what was consumed |
| Quota authority | `account_quota.go` | Sliding windows over the ledger |
| Circuit breaker | `account_breaker.go` | Derived from the ledger's failure entries; its only state is the half-open probe |
| Spend authority | `account_spend_authority.go` | The gate the credential-release paths ask |

### Why the ledger is not a counter

Counters are read-modify-write: two hosts recording turns for one account both
read the same total and both write their own sum, and one turn vanishes. That
is tolerable for an advisory figure and not for a limit decision.

A ledger entry is keyed by its own idempotency key and written with the store's
guarded create. Of N writers presenting one entry, exactly one stores it, and
the losers observe the stored bytes and agree. Totals are derived by summing
what is there. The counters remain: they are the observability surface, and
`LeastUsageAccountStrategy` balances on them.

### Five keyspaces over one record

Every entry is written to up to five planes:

```
orch/costwindow/{tenant}/{account}/{bucket}/{occurredAtNanos}/{entryID}   pooled entries only
orch/costagent/{tenant}/{agent}/{bucket}/{occurredAtNanos}/{entryID}      entries of a tenant Agent's session
orch/costproject/{tenant}/{project}/{bucket}/{occurredAtNanos}/{entryID}  entries with a project
orch/costtenant/{tenant}/{bucket}/{occurredAtNanos}/{entryID}             every entry
orch/costchain/{tenant}/{project}/{task}/{job}/{session}/{round}/{entryID}
```

The **attribution plane** is the chain, so a roll-up at any level is a prefix
scan of that level's subtree. The four **quota planes** are keyed by account,
Agent, project and tenant, then by the moment consumption occurred. The hourly
`bucket` segment bounds a window read to what the window is worth, not what the
history is worth.

The Agent plane is keyed by `resolved_agent_id`, the tenant Agent the turn's
session ran as. It is not the entry's `agent_id`, which is the control loop's
per-job identity. A raw-backend session's turns have no Agent plane.

The quota planes are written first: account, then Agent, then project, then
tenant. A partial write therefore leaves spend visible to the limit and missing
from the report, never the reverse.

**A level that does not apply is unattributed, not absent.** A job that belongs
to no task still has a task segment, filled with `-`. Collapsing the segment
would move the entry into the subtree of whatever sits at the next position.
`-` is refused as a caller-supplied value, so a caller can never spell the
sentinel.

### Where a turn becomes an entry

There is one site: `agentServiceImpl.recordTurnUsage`
(`turn_usage_recorder.go:124`), called from `runPromptTurn`. It writes the
counters and the ledger together, so a turn is in both or in neither.

- The **turn identity** is minted when the turn is planned (`planPromptTurn`),
  so a retried write is not a second charge.
- The chain comes from `TurnAttribution` on the context. `JobRun.Prompt`
  attaches it for every runner that prompts through the job envelope. It
  travels on the context rather than on `PromptSessionRequest` so an
  operator-facing transport cannot charge its tokens to someone else's job.
- A **failed** turn is recorded with outcome `failure`, even with no tokens.
  Without those entries the breaker could never trip.
- A turn whose backend reported **no token counts** is still ledgered, with no
  tokens. The counters skip it, but the breaker reads occurrences, and a
  successful unmeasured turn is what closes a tripped breaker.
- Each **token dimension keeps its own field**: `input_tokens` is the fresh
  prompt, and `cache_read_tokens` and `cache_write_tokens` are the prompt
  tokens served from and written to the provider's cache. The total every
  quota window sums includes all four.
- A turn that drew on **no pooled account**, such as local inference on
  OpenVINO or Ollama, is ledgered with no `account_id`. It is written to the
  project, tenant and attribution planes and never to the account quota plane.
- Every entry carries `kind` (the job's runner: `sdlc`, `review`, `triage`,
  `workflow`), `host`, `agent_id` and `duration_ms`. The host is never a metric
  label, because hosts are unbounded.

Every ledger key is namespaced by tenant and the store is the tenant-routed
durable KV, so one tenant's usage can neither be read by, nor influence
selection for, another.

**Which backends report token counts.** Nothing is estimated.
`PromptResponse.Usage == nil` means "not measured".

| Backend | Reports usage? | Where from |
| --- | --- | --- |
| `claude` (ACP stream-json) | yes | `usage` on the terminal `result` message, falling back to per-message `usage` |
| `claude` one-shot (`--print --output-format json`) | yes | `usage` on the result object |
| `opencode` (ACP JSON-RPC) | yes | `usage` on the `session/prompt` result; reasoning tokens count as output |
| `codex` (`codex exec --json`, on worker daemons and on the control plane's `CodexExecBackend`) | yes | `usage` on the `turn.completed` event. `input_tokens` includes `cached_input_tokens`, so the fresh prompt is recorded as `input − cached` and the cached part as a cache read. |
| `openvino` | yes | The OpenAI-compatible `usage` of every tool round, summed |
| `ollama` (direct) | yes | `prompt_eval_count` and `eval_count` |
| API inference (`anthropic`, `openai`, …) | yes | The provider's `usage` |
| Remote daemons (`RemoteACPBackend`) | yes, on the control plane | The daemon returns the usage on the Prompt response (`PromptSessionResponse.usage_json`), and the control plane's `runPromptTurn` charges it. The daemon has no ledger. |

**A turn's model** is, most authoritative first: the model the backend reports
it served, the model the turn was planned with, then the backend's configured
default. When nothing names one, the entry is filed under `unknown`
(`ConsumptionModelUnknown`), which no price table lists. For codex, a turn with
no named model is recorded under the model `config.toml` pins (`$CODEX_HOME`,
else `$HOME/.codex`), or `codex-cli-default` (`CodexCLIDefaultModel`) when it
pins none. A model name is recorded as the backend reported it, provider prefix
included.

## Spend limits

### The policy lives on the account

```go
type AccountSpendPolicy struct {
    Quota   SpendLimits    // sliding windows
    Breaker *BreakerPolicy // nil = never taken out of service for failing
}
```

An allowance belongs to the thing that is billed, and a provider meters a
login. Keeping it on the project would let two accounts on one plan each be
admitted up to the plan's whole allowance. The policy is validated at the write
(`AccountStore.Create` / `Update`). **An account with no policy is unmetered**:
inventing a default allowance would be inventing a fact about someone's plan.

**Windows are sliding**: `[now-Size, now)`, recomputed on every question. A
"5-hour limit" frees up gradually as old turns age out.

**Windows meter tokens, dollars or both.** A window (`QuotaWindow`) carries
`max_tokens`, `max_equivalent_micros` or both. Zero leaves that axis unmetered,
and a window that meters neither is refused at the write. When both are set,
the first spent one refuses and names its axis (`tokens` or
`equivalent_micros`). The dollar axis meters **priced turns only**, and it
refuses once the allowance is spent, because a turn's price is not known until
it has run.

### Four spenders, one comparison

One limits type, `SpendLimits`, is carried in four places, and each is judged
by the same comparison (`evaluateWindows`) over its own quota plane:

| Spender | Where the limits live | Measured over | Enforced |
| --- | --- | --- | --- |
| Account | `Account.Spend.Quota`, with the breaker | The account's pooled turns | A candidate filter in selection, and at credential release |
| Agent | `Agent.Spend` ([../software-factory/agents.md](../software-factory/agents.md#spend-limits)) | Every turn of every session that ran as the Agent | At spawn: a pool passes over it; named alone it is refused (`AgentSpendExhaustedError`) |
| Project | `Project.Spend` | Every turn charged to the project, pooled and local | Admission defers the project (`project_spend`) |
| Tenant | `TenantSpendPolicy`, at `orch/spendpolicy/{tenant}` | Every turn charged to the tenant | Admission defers every project of the tenant (`tenant_spend`) |

### Accounts: a spent account is filtered out before any strategy runs

The spend check is a **candidate filter** in `accountResolver.selectAccount`,
applied after the session cap and before the strategy ranks. Every strategy
therefore fails over past a spent account to a sibling with headroom.

- The filter asks `SpendAuthority.Check`, which answers without claiming a
  half-open breaker's trial. The one account selected is then asked
  `Authorize`.
- If that `Authorize` refuses an automatic selection, because a window was
  crossed or another host won the trial between the two reads, the account
  joins the spent side and selection runs again. A pinned account keeps its own
  refusal.
- When every candidate is spent, the resolver returns `AccountsSpentError`,
  "the pool is spent until `AvailableAt`". It is a **deferral, not a park**:
  class `accounts_spent`, refunded, requeued at `AvailableAt`, and never counted
  toward the park bound. A pool that is part spent and part cooling down
  reports whichever returns first.
- A check that cannot read the ledger fails the resolution.
- **A worker daemon** selects with the same resolver. Its spend authority is
  `AccountAPIClient.Check`, which reads the `spend` verdict the control plane
  puts on the usage call (`SpendHeadroom`). The trial is claimed only by the
  control plane's credential release.

Both release paths ask the spend authority **before** resolving the envelope,
so a refused release never renews an `oauth2` credential on behalf of work that
is about to be refused:

| Path | Site |
| --- | --- |
| In-process spawn | `accountResolver.Resolve` → `checkSelected` → `authorizeSpend` |
| Remote fleet node daemon | `AccountService.MaterializeAccountCredential` |

### Projects and tenants: deferred at admission

A project's and a tenant's limits cannot be enforced at release, because no
single account carries them. They are enforced at **admission**, which is the
fourth input the dispatcher asks every sweep (see
[../software-factory/admission-and-readiness.md](../software-factory/admission-and-readiness.md#the-four-inputs)).

`ScopeSpend` (`admission_spend.go:132`) asks the tenant first, because the
tenant's limits stop every project it owns and so explain the most. Only when
the tenant admits does it ask the project. Limits that meter nothing admit
without reading the ledger. A spent window **defers** the project with a typed
axis (`project_spend` or `tenant_spend`) and an operator-facing reason:

```
the project p1 spend window "5h" is spent: $41.20 of $40.00 at list price; it frees up at 2026-01-01T17:00:00Z
```

The deferral is reported when it starts or changes, never on every sweep: the
counter `quicbaas_factory_spend_deferrals_total{tenant,project,axis,reason}`
(`reason` is the quota refusal: `token_limit`, `cost_limit` or
`provider_hold`, and `other` for anything else), an audit event (project
resource, action `spend_deferred`, resource ID
`{project}/{axis}/{reason}/{window}`), and a warning log line. A window that
could not be summed fails closed with `AdmissionUndecidableError`. The reader
is wired in `pkg/server/lifecycle_orchestration.go:618`.

A tenant-level spend report carries the tenant's own windows, and a
project-level report the project's (`scope_policy_status`, `scope_windows`),
with live usage and `exhausted` on a spent one.

### The breaker

Open or closed is a pure function of the ledger, so two hosts asking the same
question get the same answer. "Tripped" means at least `FailureThreshold`
failures in the window **and** no success recorded since the most recent
failure.

The one non-derived decision is which host gets the half-open trial. That is a
guarded create on `orch/costprobe/{tenant}/{account}`, released by a guarded
delete. At the spawn gate, the trial's only release is its TTL
(`spendProbeTTL`, one minute, `account_spend_authority.go:69`), because a
spawned session's outcome reaches the ledger rather than the gate.

### Fail closed, never with a number

Every path that cannot establish an answer returns an error and no admission. A
gate that admitted whenever it could not read the ledger would open exactly
when the store is in trouble. A read failure is also never reported as a
refusal: an operator told "the cap is spent" would raise a limit that was never
reached.

A deployment with no ledger wired is unmetered and keeps working. The gate is
additive; one that failed closed when absent would take every account offline.

### What a refusal looks like

| Error | Fault class | Behaviour |
| --- | --- | --- |
| `AccountQuotaExhaustedError` | `account_quota_exhausted` | A pinned account: requeues until the window's oldest entry ages out. `Axis` is `tokens` or `equivalent_micros`. |
| `AccountBreakerOpenError` | `account_breaker_tripped` | Requeues until the cooldown elapses. |
| `AccountBreakerTrialHeldError` | `account_breaker_tripped` | Requeues with no stated wait. |
| `AccountLimitedError` | `account_limited` | The provider's own hold. |
| `AccountsSpentError` | `accounts_spent` | Every candidate of an automatic selection is spent: requeues at `AvailableAt`, never counting toward the park bound. |

None of these parks on first sight: a window frees up and a breaker retries.
The dispatcher writes the classified reason onto `Task.DispatchReason`, and the
forge comment renders a `**Cause:**` line from `Task.BlockFaultClass` and
`orchestration.FaultClassMeaning`.

## Pricing

Every entry carries two money figures, stamped once when the turn is recorded,
in integer millionths of a US dollar (summing `float64` money does not
associate):

| Figure | What it is | Where it comes from |
| --- | --- | --- |
| `equivalent_micros` | What the tokens would cost at **list price** on the provider's metered API. For local inference, the configured hardware cost. | `PriceBook.Stamp`, from the price table |
| `billed_micros` | What the turn added to the bill actually paid | `stampBilling` (`account_billing.go`), from how the paying account is billed |

**Lookup.** The tenant's table, then the deployment's; within each, the
engine-specific entry before the generic one. Names match **exactly**, plus
aliases. A tenant entry that re-prices a model also answers to the aliases the
deployment declares for it under the same engine scope.

**An unknown model is unpriced, never guessed.** Its entry keeps its tokens and
carries no `price_ref`, and every reader renders its money as a dash. **A price
is stamped, not looked up at read time**: each priced entry records `price_ref`
(`{version}:{scope}:{engine|*}/{model}`), so a price changed later never
re-prices earlier turns. A malformed table fails startup.

### Local inference is costed from electricity

A local model has no list price, so its cost is computed from the energy the
turn drew:

```
kWh     = power_watts × turn seconds / 3.6e6
dollars = kWh × electricity_price_per_kwh
```

| Input | Resolved from, first match wins |
| --- | --- |
| Electricity price | The tenant's runtime cost settings, then `tenants.<t>.electricity_price_per_kwh`, then `electricity_price_per_kwh`, then $0.12 |
| Device draw | The tenant's runtime `engine_power_watts[engine]`, then the tenant's table entry `power_watts`, then `tenants.<t>.engine_power_watts[engine]`, then the deployment's table entry `power_watts`, then `engine_power_watts[engine]` |

- An explicit per-token rate wins over `power_watts`.
- A per-engine draw of 0 switches electricity costing off for that engine at
  that scope. A model entry's `power_watts: 0` just means no draw is declared.
- A turn that would be costed from electricity but has no measured duration is
  recorded unpriced, never at $0.
- Every entry records `price_basis` (`token_rate` or `electricity`). An
  electricity entry also records `energy_mwh` (milliwatt-hours) and a
  `price_ref` naming the draw and rate and where each came from, for example
  `v1:electricity:default:openvino/qwen3-8b@12W:tenant_settings@0.15/kWh`.

**Runtime cost settings.** The rate and per-engine draws are also settable per
tenant while the node runs, stored at `orch/costsettings/{tenant}`
(`CostSettings`, `cost_settings.go:48`), and they beat the configuration. A
replace replaces everything; an absent rate or engine inherits the
configuration. The surfaces are in [api.md](api.md#cost-settings).

### Measured energy from fleet telemetry

The formula above is the **estimate**. Where the fleet node that ran a turn
reports device telemetry, the turn is costed from the energy its device
measurably drew.

Every heartbeat carries one sample per device the daemon can read
(`pkg/fleet/devicetelemetry.go`), each value with its provenance:

| Device | Source (no cgo) | Signals |
| --- | --- | --- |
| NVIDIA GPU | `nvidia-smi --query-gpu=uuid,name,power.draw,utilization.gpu,memory.used,memory.total`, 5 s timeout | power (measured), utilization, memory |
| Intel NPU | `/sys/class/accel/accelN/device/npu_busy_time_us`, `npu_memory_utilization` | busy fraction (derived), memory; no power sensor |
| CPU package | `/sys/class/powercap/intel-rapl:N/energy_uj` | power (derived from the counter, wrap-corrected) |

The control plane files each report in the qrdb node's time-series engine,
tenant-scoped, under `fleet.device.power_watts`, `.utilization`,
`.memory_used_bytes` and `.memory_total_bytes`, tagged `node`, `host`, `kind`,
`device` and `provenance` (`fleet_telemetry.go`). A storage failure is logged
and never marks a node dead. Every `fleet.device.*` series carries a retention
policy of `orchestration.pricing.telemetry_retention` (14 days by default,
`fleet_telemetry_series.go`). Expiring points is the time-series engine's job.

`orchestration.pricing.engine_devices` binds an engine to a device class; there
is no default binding. A turn's window is `[occurred_at − duration,
occurred_at]` on the entry's `host` (`turn_energy.go`). The window is cut at
each telemetry point and at each start and end of another turn on the same host
and device class. Each piece is charged the device's draw at its midpoint
divided by the turns in flight there. A point speaks for at most 90 s, and a
window that any piece leaves uncovered falls back to the estimate.

| `energy_provenance` | Meaning |
| --- | --- |
| `measured` | Integrated from a power sensor (GPU) |
| `derived` | Integrated from a counter-derived power (CPU package), or a measured busy fraction × the configured draw (NPU) |
| `estimated` | The configured draw × the turn's duration |

A measured entry's `price_ref` names it, for example
`v1:electricity:measured:gpu@agent-01:ollama/review-q8:deployment_config@0.12/kWh`.

### Late pricing

A turn whose model the table did not name yet, or whose tenant cost settings
could not be read, is recorded unpriced. The `ledger.pricing` scheduler system
job (hourly, `pkg/scheduler/handlers/ledger_pricing.go`, registered by
`pkg/server/ledger_pricing.go`) prices every such entry the table in force can
now price, in every tenant. This is the ledger's **one admitted amendment**
(`account_ledger_pricing.go`): it only prices, never re-prices; every plane is
compare-and-swapped from the stored bytes in `Record`'s order; and unmeasured
turns and turns under `unknown` stay unpriced.

### Billing

How an account is billed is a fact about the account (`Account.Billing`,
`account_billing.go:92`):

| Account billing | A turn's `billed_micros` |
| --- | --- |
| `subscription` | `0`, a known figure. `monthly_fee_micros` is the fee, pro-rated over a tenant-level report's range and never smeared across turns. |
| `api` | The list-price equivalent, when the model is priced. A fee is refused. |
| Not configured | **Unknown**, never zero |
| Unpooled local inference | Its electricity or configured hardware cost, stamped as billing `local` |
| Any other unpooled turn | Unknown, stamped as billing `unpooled` |

A roll-up sums each figure only over the entries that carry it and counts the
rest (`priced_entries` / `unpriced_entries`, `billed_entries` /
`unbilled_entries`). An **unmeasured** turn is recorded with zero tokens and
counted unpriced and unbilled, except a subscription turn, which bills zero
whatever it consumed. Some unpriced entries beside priced ones make a figure a
floor, which readers mark with `≥`.

## The spend report

`orchestration.ReadSpendReport` (`spend_report.go`) is a projection over
`ConsumptionLedger.Rollup` plus each account's own policy, so a dashboard and
a refusal can never disagree about one account.

**One verb for every level.** The chain is a path,
`tenant/project/task/job/session/round`, so a project's spend and a job's spend
are the same prefix scan at two depths. A scope that skips a level is refused.

**A time range.** `from` and `to` narrow any scope to `[from, to)`. A range
given only `from` closes at now; `to` alone is refused. Tenant and project
scopes read their quota planes, one scan per hourly bucket the range overlaps.

**Subscription fees.** A tenant-level report pro-rates every fee-carrying
subscription over the range: `AccountBilling.FeeShare`
(`account_billing.go:136`) is `monthly_fee_micros × range / 30.436875 days`,
rounded down. Each share starts no earlier than the account's registration, and
an idle subscription is listed beside its fee. An unranged report shares fees
from the first recorded turn to now. The shares are summed as
`subscription_fee_micros`, beside and not inside `billed_micros`. Below the
tenant level, and in a host-narrowed report, no fee is shared.

**By host and by Agent.** Every report breaks its totals down by host (`hosts`)
and by the tenant Agent each session ran as (`agents`). `host` and `agent`
narrow any scope as filters, and `-` selects turns that name none. Narrowed to
an Agent, the report carries that Agent's limits and live usage
(`agent_policy_status`, `agent_windows`).

**The unattributed subtree.** A scope may name the `-` sentinel to open it. The
write side refuses `-` as an entity id, which is what makes it unambiguous as a
scope value. It is exported as `orchestration.SpendUnattributed`,
`orchestrator.SpendUnattributed` (Go SDK) and `SPEND_UNATTRIBUTED`
(TypeScript SDK).

**What the report refuses to claim.** Every figure the system does not have is
absent and labelled, never zero:

| Field | Rule |
| --- | --- |
| `equivalent_micros` / `billed_micros` | No priced (billed) entry means a dash; unpriced (unbilled) entries beside priced ones make a floor. `currency` names the unit. |
| `accounts[].policy_status` | `metered`, `none` or `unknown`. `none` means read and meters nothing; `unknown` means nothing could read it. |
| `project_name` / `children[].name` | Absent for a project that no longer exists. A project store that cannot answer fails the report. |
| `children[].unattributed` | The no-value bucket, with `id` empty so no reader prints the sentinel. |
| `first_at`, `last_at`, `frees_at`, breaker instants | Absent rather than the zero instant. They are pointers in Go, because `omitempty` does not omit a zero `time.Time`. |

A deployment that records no consumption answers `503` / `Unavailable`, not an
empty report: "nothing is being recorded" and "nothing has been spent" are
opposite facts. An account store that cannot answer fails the report, while a
deleted account keeps its spend with an unknown allowance.

**`exhausted` is about the allowance, not a request.** It says `used_tokens`
has reached `max_tokens`. At exactly `used == max` the quota gate still admits
a spawn, which passes an estimate of zero; a turn estimating one token is
refused.

## Job admission

Job admission decides which node durably owns a job, how a job's outcome is
charged to its task, and how a job whose node died or whose agent vanished is
concluded. The job envelope, runners and dispatcher are described in
[../orchestration/architecture.md](../orchestration/architecture.md).

### The lease

Every job a service runs holds a **JobLease** (`job_lease.go:91`) before its
first session is spawned:

```
__sys:orch/joblease/{tenant}/{project}/{sha256(workflow)}/{sha256(ref)}
```

`ref` is the task's source reference, the task ID when the task has no source,
or the job ID for a job bound to no task. `workflow` is the workflow a workflow
job runs, or the name of the runner driving a job with no workflow (`sdlc`).
Both are hashed because they are not the engine's to shape. An unscoped job uses
`-` as its project segment.

```go
type JobLease struct {
    TenantID, ProjectID, Workflow, Ref, TaskID, JobID string
    Generation  int    // the attempt ordinal; 1 for a job with no task
    Node        string // the daemon running the loop
    AcquiredAt, HeartbeatAt int64
    Released    bool   // release is a field, never a delete
    ReleasedAt  int64
    // plus OffBudget, AwaitingSlot and Dormant (below)
}
```

**Admission.** A contender takes a held lease only when the holder released,
the holder's generation is lower, or the holder's heartbeat is older than the
TTL (`jobLeaseAdmits`, `job_lease.go:342`). A job re-acquiring its own lease at
its own generation is admitted; that is resumption after a restart. Anything
else is declined with `JobLeaseTakenError{HeldBy, HeldByJobID, Generation}`.

**Stores.** `NewTransactionJobLeaseStore` decides leases on go-qrdb's
transaction plane through `cas.Writer.Handover`, one proposal with no
read-then-write window, and serves every cluster-like mode.
`NewProcessJobLeaseStore` is the complete answer for a standalone boot, where
the only contention is between goroutines. The server picks by the same
predicate the task-claim arbiter uses.

**Capacity.** `NewProjectCapacityJobLeaseStore` wraps either store and refuses
an acquire that would put a project over `Project.Quotas.MaxConcurrency` with
`ProjectCapacityExceededError{Live, Limit, RetryAt}`. Live means unreleased and
heartbeating within the TTL. The fleet and backend capacity checks wrap it the
same way; see
[../software-factory/admission-and-readiness.md](../software-factory/admission-and-readiness.md).

**Off the budget.** A lease with `OffBudget` set is outside `MaxConcurrency` in
both directions: admitted while the budget is full, and not filling it. A runner
declares it through `JobLeasePolicy.OffProjectBudget`; the `review` runner does,
so a project at `max_concurrency: 1` can still review its own pull requests.
The flags are on the record, because the budget has three readers: the capacity
store, the task dispatcher and the task-source poller. All three answer through
`JobLease.CountsAgainstBudget(now, ttl)` (`job_lease.go:215`); the schedulers
use `CountLiveJobLeases` (`task_dispatcher.go:971`), which is a budget count, not
a job count.

**Off the budget, per state.** A runner whose jobs hold a worker slot in some
states and not others declares the difference with
`JobStateSpec.OffProjectBudget`. The sdlc runner declares `reviewing`,
`cleanup` and `escalating`. On every transition between the two kinds,
`JobRun.SetState` moves the lease with `JobLeaseStore.Rebudget`, rewriting
`OffBudget` and `AwaitingSlot` and heartbeating in the same write. A move off
the budget is never refused. A move back on is counted against
`JobLease.HoldsSlot` (`job_lease.go:231`). When the project is full, the job
**waits** in the state it is leaving and marks its lease `AwaitingSlot`, which
`CountsAgainstBudget` counts, so new work cannot take the slot first. The move
is retried every TTL/3. A budget write that answers an error leaves the run's
standing unknown, because a write can commit and still lose its answer. Before
the next worker state, the run re-reads its lease record; the store is the
arbiter.

**Dormant.** The same move marks the lease `Dormant`: a job in an off-budget
state of such a runner runs no worker, and the sdlc runner closes its container
on entry to `reviewing`. The fleet and backend checks count
`JobLease.ClaimsWorker` (`job_lease.go:255`), so a dormant job is off both.
Their `Rebudget` admits a dormant job waking, counted against `HoldsWorker`
(`job_lease.go:241`), and a refusal makes the round wait. A review or triage job
is off the budget for its whole life and is never dormant.

**Subject.** A lease is named after the work it holds: the task's source
reference for task-bound work, otherwise whatever `JobLeasePolicy.LeaseSubject`
answers. Without a policy, a job bound to no task is leased by its own id. See
[../orchestration/job-runners.md](../orchestration/job-runners.md).

**Heartbeat.** The envelope heartbeats on every state transition and on a
ticker at TTL/3, started when the lease is acquired and stopped by
`JobRun.execute` on every exit path, not by the runner. A heartbeat that finds
another job holding the lease aborts the run with `JobSupersededError{By,
Node}`. `JobStatus.Node` and `JobStatus.HeartbeatAt` carry the last proof of
life.

**Acquire before Prepare.** The lease is taken in `execute`, before the
runner's first state, so no side effect happens outside the lease.

**Release.** A job bound to no task releases on the run's exit. A job bound to
a task keeps its lease until `concludeAttempt` has written the task. Releasing
first would leave a window in which the task is open, the lease is released,
and the reaper, which acts only on unreleased leases, could never repair it.
`CancelJob` releases synchronously.

**A held subject is refused at StartJob.** A runner whose jobs are per-subject
singletons, such as triage and review, acquires its lease inside the run.
`StartJob` reads the subject's lease before any record exists and applies
`jobLeaseAdmits` to a contender of the request's generation. A contender the
store would refuse is refused synchronously as `ErrJobLeaseTaken` naming the
holder, rather than as a failed job minutes later.

### Attempt accounting

A job bound to a task concludes the task **synchronously**, on the goroutine
that ran the loop, immediately after `Run` returns
(`agentServiceImpl.concludeAttempt`, `task_attempt.go:1183`). The task is
written once through compare-and-swap on `Task.Version`; a conflict re-reads
and re-derives, so a concurrent block, readmit or artifact write survives.

The task accepts a conclusion only from the job it names in `SpawnedJobID`, and
only while its attempt is open (`pending`, `claimed`, `in_progress`). Every
conclusion moves `SpawnedJobID` into `Metadata["attempt_history"]`, so a second
report about the same job is refused with `AttemptConcludedError`.

`JobStatus.FailureClass` carries why a job failed, as a class:

| Class | Source | Accounting |
| --- | --- | --- |
| An environmental terminal reason (`api_error`, `turn_setup_failed`) | `PreFirstTurnFault` | Refunded, requeued (bounded) |
| `accounts_cooling_down` | `AccountsCoolingDownError` | Refunded, requeued at `AvailableAt` (clamped to 24h) |
| `accounts_saturated` | `AccountsSaturatedError` | Refunded, requeued |
| `accounts_spent` | `AccountsSpentError` | Refunded, requeued at `AvailableAt` (clamped to 24h); never counts toward the park bound |
| `account_limited`, `account_auth_rejected` | A pinned held account; a rejected login | Refunded, requeued |
| `project_capacity_exceeded` | `ProjectCapacityExceededError` | Refunded, requeued at `RetryAt` |
| `forge_unreachable` | An error unwrapping to `ErrForgeUnreachable` (`task_attempt.go:216`): a forge call unanswered after the adapter's retries and reconciliation ([../sdlc/architecture.md](../sdlc/architecture.md#forge-transport-deadlines-retries-and-unanswered-writes)) | Refunded, requeued (bounded) |
| `agent_interrupted` | An error unwrapping to `ErrAgentInterrupted` (`FaultAgentInterrupted`, `task_attempt.go:208`): a worker turn whose Prompt the fleet node stopped serving, or a container stream that closed with no exit of the worker's own | Refunded, requeued (bounded); never charged |
| `no_provider_account`, `no_account_resolver`, `account_credential_missing`, `account_refresher_missing` | Resolver errors | Refunded, **blocked** for a human |
| `superseded` | `JobSupersededError`, `JobLeaseTakenError` | Nothing written; the lease holder owns the task |
| `stalled` | Stall detector | Charged |
| `abandoned` | Reaper | Charged; requeued, or blocked (`escalated`) when the budget is spent |
| `""` | Any other failure | Charged; requeued, or `failed` with `Result.Metadata["reason"]="budget_exhausted"` |

The full set of resolver classes is `resolverFaultClasses`
(`task_attempt.go:359`).

**The budget.** `Task.ChargedAttempts()` (`task_attempt.go:943`) is `Attempts -
EnvironmentFaults`. `Task.BudgetExhausted()` (`task_attempt.go:962`) compares
it to `AttemptBudget`, which defaults to `DefaultAttemptBudget` (3).
`Task.Deferred(now)` reads `RetryNotBefore`. Three consecutive environmental
faults (`MaxConsecutiveEnvironmentFaults`, 3) block the task and charge the
attempt. The count is consecutive and resets on any conclusion that is not
another requeued fault.

**A pool that is merely busy is not a misconfiguration.** If the tenant has
accounts for the provider and at least one is on a hold that names its expiry,
the item goes back in the queue uncharged. `WorkItem.RetryNotBefore` is set from
the hold's expiry, clamped to 24 hours, and the dispatcher passes over any item
whose moment has not arrived. A hold with no stated expiry is not treated as
self-clearing, because requeueing against a condition with no end is a
livelock.

**Environmental or work-derived.** The evidence is structural, from the CLI's
own result object: `is_error` with a reported-and-zero token usage means no
request reached the model, and the `terminal_reason` says whether a different
environment would have changed that.

| Refunded (environmental) | Charged (work-derived) |
| --- | --- |
| `api_error`, `turn_setup_failed` | `blocking_limit`, `prompt_too_long`, `rapid_refill_breaker` (the session's context is full — the runner rolls the session, see [../sdlc/architecture.md](../sdlc/architecture.md#a-full-context-rolls-the-session-not-the-attempt)), `image_error`, `model_error`, `max_turns`, `budget_exhausted`, `stop_hook_prevented`, `hook_stopped`, and every other declared reason |

`prompt_too_long` is charged because it is a deterministic consequence of the
prompt built from the work item. A reason this build does not recognize is
charged, with its class surfaced. A session whose account credential was
rotated under it is replaced at its next prompt (see [Credential
generations](#credential-generations)), so that cause of `api_error` is removed
before it can be reached.

### Operator faults

The classes above are read off a typed error on the dispatch path. A second
family arrives by a different door: a judgement a runner makes while it holds
the job, carried on `BlockReason.FaultClass` through `JobRun.Park(kind, reason,
faultClass)` (`job_run.go:1308`). There are six classes
(`operatorFaultClasses`, `task_attempt.go:322`):

| Class | Proved by | Accounting |
| --- | --- | --- |
| `workspace_unfit` | The project names no workspace path; the daemon could not make the mount point; the checkout is absent, empty, holding another repository or missing a declared context repository; or a git command of the checkout failed | Refunded, blocked for an operator |
| `worker_environment` | The worker declared `SDLC-STOP: environment`, declared a stop code this binary does not know, or answered with no text on every turn | Refunded, blocked for an operator |
| `ci_host_defect` | The host that runs the work could not run it: the registry pull failed, the container runtime refused the container, or the module-resolution environment was not loaded | Refunded, blocked for an operator |
| `agent_unreachable` | The fleet node daemon did not answer the spawn (`FaultFleetNodeUnreachable`); no session, container or token was started | Refunded, blocked for an operator |
| `untrusted_signer` | The publisher refused a push because the branch carries a commit no key the project trusts signed, or the merge train ejected such a change | Refunded, escalated for an operator: record the key in the project's signing history ([../projects/user.md](../projects/user.md#rotate-the-signing-key)) or replace the commit, then readmit |
| `config_error` | The project's binding names a verification command the operator allowlist (`orchestration.verification_commands`) does not carry | Refunded, blocked: add the command or rebind the project, then readmit |

The park still stops the task until a person acts, and the class adds three
things: the attempt is refunded (`Task.EnvironmentFaults++`); the class is
written onto the block (`Task.BlockFaultClass`), so a re-label readmits the
task after the repair and the issue comment names the cause; and the daemon's
log attributes the fault to the factory rather than to the work.

**An unrecognized class is charged.** `OperatorFaultClass`
(`task_attempt.go:352`) answers false for anything outside the six, and
`JobRun.Park` drops such a class while keeping the park. Reading a genuine
failure as infrastructure would refund every attempt of a broken change.

**A question is refunded.** `ParkAwaitsAnAnswer` (`task_attempt.go:340`)
refunds a `needs_info` park, because the work is waiting on an answer, not
failing. Every other unclassified park, such as an owner's decision, a spent
review budget or a repeated prompt, is charged.

### Reading the attempt history back

`Task.AttemptHistory()` (`task_attempt.go:1535`) names every job that ran the
task.

**The narrowing is the server's.** `ListJobs(ctx, tenantID, *JobFilter)`
applies `JobFilter.TaskID`, and every transport passes the caller's `task_id`
into it. A job matches on `JobStatus.TaskID`, falling back to `Config.TaskID`.
A nil or empty filter lists every job the tenant owns.

**The record scope is untenanted by construction.** A job record is addressed by
its key, `job:{tenant}:{id}`, and a listing is bounded by `job:{tenant}:`. The
store underneath routes by context (`pkg/server/tenant_kvstore.go`), so all four
job-record paths are detached from the caller's tenant:

| Path | Scope | Why it cannot carry a tenant |
| --- | --- | --- |
| `persistJobStatus` | `jobRecordOp` | Runs on the job goroutine, after the starting request returned |
| `loadJobStatus` | `jobRecordOp` | Must answer for a record written by any of the others |
| `ListJobs`' merge scan | `jobRecordScope` | Must see the same records the other three do |
| `recoverJobs` | `jobRecordScope` | Enumerates every node's interrupted jobs at boot, before any request exists |

`RecordJobDeath` is why this matters: the reaper writes a death record for a
tenant whose barrier may by then be sealed.

The two scopes differ only in whether they impose a deadline. `jobRecordOp`
covers one record and is bounded by `jobRecordTimeout` (5 s,
`orchestrator.go:4472`). `jobRecordScope(ctx)` covers the two scans, drops the
context's values and adds no deadline of its own, so a deadline-less listing is
never truncated by a fallback ceiling. Cancellation is forwarded in both. A
record written with no tenant files under the system tenant.

### The reaper and the stall detector

Both run from the server on the heartbeat cadence (TTL/3) through
`agentServiceImpl.Sweep` (`job_admission.go:294`), and both are no-ops when
nothing is bound.

**Reaper** (`NewJobReaper`, `job_reaper.go:137`) scans every unreleased lease.
A lease whose heartbeat is older than the TTL and whose job is not running in
this process is a death: the persisted `JobStatus` is marked `failed` /
`abandoned`, the task is concluded through `concludeAttempt`, and the lease is
released. A late heartbeat on a job running here is `run_live` and untouched. A
dead lease with no task, or whose task has moved past the job, is `orphaned`
and released.

**Stall detector** (`NewJobStallDetector`, `job_stall.go:292`) examines this
node's running, task-bound loops against per-state bounds
(`orchestration.idle_thresholds`, defaults in `DefaultJobIdleThresholds`).
Whether a state is session-backed is the job's own runner's declaration when
the runner is registered. A job past its bound in a session-backed state is
asked whether any of its `SessionIDs` is still addressable through
`SessionStatus`. If none is, the loop is aborted with `JobStalledError` and
concluded as a charged failure. A job past its bound in any other state, or
with an addressable session, is reported once per threshold period and left
running. A session whose state cannot be read concludes nothing. The detector
validates operator thresholds against `KnownJobStates()` (`job_runner.go:347`)
through `JobIdleThresholdsFrom`, so a state any registered runner declares can
be configured.

The session is asked for under the job's own tenant: the sweep carries no
principal, so the detector binds `JobStatus.TenantID` onto the probe context
with `ContextWithTenant`. A run whose status names no tenant is left untenanted
and its probe is refused, which reads as `session_unknown`.

**Liveness of a containerized session is the container's.** A container session
has no supervising `*exec.Cmd`, so `ACPBackend.Status` asks the runtime
(`ContainerRuntime.IsRunning`). Not running, or `ContainerNotFoundError`
because `--rm` removed it, is `closed`. Any other runtime failure is an error,
so the detector reports `session_unknown` rather than ending live work. Across
the wire, a daemon answering `codes.NotFound` is translated back into
`SessionNotFoundError` by `RemoteACPBackend.Status`.

### Recovery

`recoverJobs` resumes only persisted jobs whose `Node` is this node's name
(`quicbaasd-node-<raft id>`, else `quicbaasd-<hostname>`). Another node's job
is that node's to resume, or the reaper's to conclude.

`resumeJob` reads the runner name off the persisted `JobStatus` and calls the
same `launchJob` a fresh start uses. `resumeStateFor(runner, interrupted)` asks
the runner's optional `JobResumer` and otherwise restarts at the runner's first
state.

| Runner | Interrupted in | Resumes at |
| --- | --- | --- |
| `workflow` | `initializing`, `decomposing` | `initializing` |
| `workflow` | `scheduling` … `reviewing`, `re-delegating` | `decomposing`; sessions are dead after a restart, work items and memory are preserved |
| `workflow` | `escalating` | `failed` |
| `workflow` | `completing`, `cleanup` | `completing` |
| `sdlc` | `initializing`, `executing`, `validating`, `re-delegating` | `initializing`; `Prepare` continues the branch the attempt already pushed |
| `sdlc` | `reviewing` | `reviewing` when the `Delivery` records a head, `initializing` when it does not |
| `sdlc` | `completing` | `completing`; `Complete` is head-bound and idempotent |
| `sdlc` | `cleanup` | `cleanup`; teardown needs no session |
| `sdlc` | `escalating` | `escalating`; the escalation finishes, then `failed` |

A state the runner does not declare restarts at `initializing`. The sdlc
`escalating` state resumes at `escalating` rather than `failed` because the job
still owes a person the reason it stopped. The park comment carries a marker
naming the task, the attempt and the block kind (`<!-- quicbaas-sdlc:blocked
task=… attempt=… kind=escalated -->`), and a resumed run reads the issue's
comments before it posts, so it posts exactly once. The park reason is restored
from the audit trail (`sdlc:escalated`).

### Shutdown drain

`Server.Shutdown` drains running control loops after the last thing that starts
a job and before the first thing that closes a store.
`agentServiceImpl.Shutdown(ctx)`:

1. marks the service draining, so a job launched from here on is refused with
   `ServiceShuttingDownError`;
2. waits for every launched job goroutine, then every asynchronous turn, within
   one window (`DefaultShutdownDrainTimeout`, 10 s, `shutdown.go:30`, or the
   caller's deadline);
3. returns `DrainTimeoutError` naming what it left running when the window
   closes first.

**The drain does not cancel the loops it waits for.** A cancelled loop would be
recorded as a failed job and charged. A loop that does not fit the window keeps
its non-terminal record and is resumed by `recoverJobs` on the next start. A
store that closes under such a loop is reported as `StoreClosedError` rather
than a panic.

## Rejected alternatives

| Alternative | Why it was rejected |
| --- | --- |
| A request proxy between the CLI and the provider | The CLI dials the provider itself. A proxy would make limit detection exact but is a separate service with its own trust boundary; `AccountLimitClassifier` is an interface so a deployment can substitute one. |
| Caching a referenced credential keyed on the secret's version | The daemon cannot learn the version without asking the control plane, and that round trip costs what a release costs. `single_use` is cheaper. |
| Persisting released credentials on the daemon | A worker host has no barrier or encrypted store, so persistence means a plaintext login at rest on the least-trusted host. |
| Counting delivery leases as live sessions | A daemon serves every session on an account under one grant, so a lease count undercounts by exactly the concurrency being measured. |
| A per-host reservation table | Every host would crowd onto, or fill, the same account. |
| Mirroring `Default` onto each account record | Two copies can drift, and clearing the previous holder's flag needed a second lock, which deadlocked. |
| Promoting a survivor when the default is deleted | It silently redirects every unpinned spawn, and its billing, to a different login. |
| `static` as the default strategy | Every worker runs on the one default account until the provider refuses it. |
| Capping delivery-lease renewals | Cuts a long job off at an arbitrary multiple of the TTL. The session's conclusion is the bound. |
| A counter as the spend authority | Read-modify-write loses concurrent turns. |
| Tumbling windows | Hand back a full allowance the instant a boundary passes, which is the burst the provider meters against. |
| The allowance on the project | Two accounts on one plan would each be admitted up to the plan's whole allowance. |
| A default allowance for unconfigured accounts | Invents a fact about someone's plan and refuses work the provider would serve. |
| Smearing a subscription fee across turns | The figure would depend on how many turns happened. Fees are pro-rated over a report's range instead. |
| Prefix, family or fallback price matching | Each is a price nobody wrote down. |
| Shipping vendor OAuth endpoints and client ids | Neither vendor documents them as a supported API. |
| Cancelling loops during the shutdown drain | Interrupted work would be charged as failed work. |

## Related

- [engineering.md](engineering.md): exported surface, invariants, errors,
  configuration, tests and known gaps.
- [api.md](api.md) and [cli.md](cli.md): every verb on every transport.
- [user.md](user.md): the tasks, in order.
- [../orchestration/architecture.md](../orchestration/architecture.md): sessions,
  the job envelope and the dispatcher.
- [../agent-fleet/architecture.md](../agent-fleet/architecture.md): worker
  hosts and their daemons.
- [../sdlc/architecture.md](../sdlc/architecture.md): the SDLC runner.
- [../projects/architecture.md](../projects/architecture.md): the project an
  account may be scoped to.
- [../secrets/dynamic-secrets.md](../secrets/dynamic-secrets.md): leases and
  secret references.
