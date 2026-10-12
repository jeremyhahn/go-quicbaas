# Pooled provider accounts

> **Status: NORMATIVE** · Last verified: 2026-09-22
> Supplement to the Software Factory feature folder: the account model, how a
> credential reaches a worker host, selection, rotation and cost attribution.
> Verbs are in [cli.md](cli.md#quicbaas-account) and
> [api.md](api.md#quicbaasaccount--sdk-package-sdkgoclientaccount).
> Conflicts with this document are defects.

A **pooled provider account** is one provider login — a single Anthropic/Claude
seat, an OpenAI/Codex API key, an OpenCode provider config — that an agent
session can be spawned against instead of the host's shared identity. A tenant
holds a pool of them; a spawn names one, or asks a strategy to pick one.

The pool lives on the **control plane**. The agent runs on a **worker host**.
This page is about the seam between those two facts: how the credential gets
from the pool to the process that needs it, and what stops it going anywhere
else.

## The two halves of an account

Every account is stored as two records that never meet:

| Half | Where | What it holds |
| --- | --- | --- |
| Metadata | The tenant's orchestration KV (`pkg/orchestration/account_store.go`) | ID, provider, project scope, status, expiry, usage timestamps. Listable, loggable, returnable. |
| Credential envelope | The platform `SecretsManager`, under a tenant-specific DEK (`pkg/orchestration/account_secret_store.go`) | The access token, the refresh grant, provider config file bytes. |

The split is what makes an operator's `list` safe. `Account` has no credential
field, and no API response type carries one — with exactly one deliberate
exception, below.

## Where the credential comes from: inline or a tenant secret

An account takes its credential from exactly ONE of two sources. Supplying both,
or neither, is refused at write time (`400`, field `credential` or
`credential_secret_ref`).

| Source | Set with | Stored | Rotated by |
| --- | --- | --- | --- |
| **Secret reference** (recommended) | `credential_secret_ref` (+ `credential_secret_file`) | Nowhere new: the account names a secret in its tenant's secrets store | Writing a new version of that secret |
| Inline | `credential` | The account's own envelope (the table above) | `rotate` on the account |

**A reference is a name, not a value.** `credential_secret_ref` is the SHORT,
tenant-relative secret name — `agents/claude/claude1/oauth_token` — the same
kind of name an SDLC binding's `forge.token_secret_ref` uses. It lives on the
metadata record and is returned by `get`/`list`; the value never is.

**It resolves in the ACCOUNT's tenant, every time.** Every resolution — an
in-process spawn, and the release verb that serves a worker host — reads the
secret through the tenant `SecretsService`, scoped to the account's own
`tenant_id`, whatever tenant the caller's context carried
(`service.NewAccountSecretResolver`). A system administrator who provisions
tenant T's account gets T's secret; a same-named secret in the system keyspace
or another tenant is never read. Nothing is cached on the account, so writing
the secret is the rotation — no account write.

**How the value becomes a credential:**

- Without `credential_secret_file`, the value (surrounding whitespace trimmed)
  is the access token, projected into the provider's access-token variable
  (`CLAUDE_CODE_OAUTH_TOKEN` for `claude`). A provider with no such variable
  (`codex`, `opencode`) must name a file instead; that is refused at write time.
- With `credential_secret_file`, the value is written VERBATIM as that file,
  relative to the per-session home — `.codex/auth.json` for the codex reviewer
  account. The path must be clean and relative.

**Rules checked at write time** (create, and update with a reference):

- a referenced account is `refresh_policy: none`. QuicBaaS never renews it:
  renewal would have to write a rotated refresh token back into a secret the
  operator owns;
- the reference is a short name — not `tenant/<id>/...` (the storage prefix the
  store adds itself), no empty, `.` or `..` segment, no whitespace;
- the secret must **resolve now**, in the account's tenant, to a non-empty
  value. A reference that does not resolve is the operator's request to fix,
  not the first spawn's surprise.

**When the secret goes missing later.** A spawn whose account's secret is
absent, empty, disabled or unreadable fails with
`AccountSecretRefUnresolvedError`, which unwraps to
`ErrAccountCredentialNotFound`: the work item parks uncharged
(`account_credential_missing`), exactly as for a missing inline envelope. The
account is put in `status: error` with a `last_error` naming the reference and
the failure class (never the underlying error text), so it drops out of
selection and appears in the attention inbox. The caller's own context ending
marks nothing.

The same holds for a spawn on a **remote fleet node daemon**. The control plane
refuses the daemon's release with `409` and `"fault_class":
"credential_not_found"` in the error body; the daemon's client rebuilds
`ErrAccountCredentialNotFound` from that class, so its spawn reports the same
fault and the work parks uncharged there too. The class, not the status, is
what carries it: a revoked delivery grant is also a `409` and is not a
configuration fault.

To return it to service, write the secret, then re-apply the reference:
`quicbaas account update <id> --credential-secret-ref <same name>`. The
update re-checks the secret and clears `error`; it does not touch `revoked` or
`limited`.

**Switching an existing inline account.** `update --credential-secret-ref`
moves an inline account onto a reference: the record switches first, then the
inline envelope is destroyed (a crash in between leaves an orphaned envelope,
never an account with no credential). If that destroy fails the update reports
it, and **retrying the same update finishes the job**: every update of a
referenced account idempotently destroys any inline envelope it still has, so
the retry does not depend on still looking like a switch. `rotate` is refused on a referenced
account — its rotation is a secret write. Deleting an account never deletes
the secret it names; the operator owns it.

**Workers never see a reference.** The fleet node daemon's account wire shape has
no reference field, and its token manager has no resolver: a worker host
receives the RESOLVED envelope through the release verb, or nothing. That
release is marked `single_use` (below, under Rotation), so a secret write
reaches a worker host's very next spawn too.

### Runbook: loading provider tokens into tenant secrets

The owner's long-lived tokens live in an env file on disk. Load each one into
the TENANT's secrets store without the value ever reaching argv, a log, or the
terminal, then point an account at it.

1. **Write the secret as a principal of the account's tenant** (a tenant
   administrator, or the tenant's agent certificate). Secrets writes are
   tenant-only: a system-administrator write lands in the SYSTEM keyspace,
   where tenant T's account will not find it — its create is refused with
   `credential_secret_ref: secret ... in tenant T: the secret does not exist in
   this tenant`.

   ```sh
   ENV_FILE=~/sources/automatethethings/infra/agents/tokens/ai-accounts.env
   # One line per FACTORY account. Adjust the variable names to the file's.
   # personal/work tokens are the owner's own and are never loaded.
   for n in 1 2 3; do
     sed -n "s/^CLAUDE${n}_OAUTH_TOKEN=//p" "$ENV_FILE" | tr -d "\"'" \
       | quicbaas secrets put "agents/claude/claude${n}/oauth_token" --from-stdin \
           <tenant principal's --tls/--cert/--key/--ca>
   done
   ```

   `--from-stdin` stores the piped bytes verbatim with no `KEY=VALUE` JSON
   envelope (a trailing newline is trimmed when the value is used as a token).
   From an interactive terminal it reads one line with echo disabled instead.

2. **Create the accounts** (account writes take `credentials:*`, which is
   system-only — go-quicbaas#737 — so this step runs as the system
   administrator, naming the tenant):

   ```sh
   for n in 1 2 3; do
     quicbaas --tenant automatethethings agent account create --provider claude \
       --display-name "claude${n}" \
       --credential-secret-ref "agents/claude/claude${n}/oauth_token"
   done
   ```

   An existing inline account migrates in place:
   `quicbaas --tenant automatethethings agent account update <id> --credential-secret-ref agents/claude/claude1/oauth_token`.

3. **The codex reviewer account** carries a credential FILE:

   ```sh
   quicbaas secrets put agents/codex/reviewer/auth_json --from-file ~/.codex/auth.json <tenant principal flags>
   quicbaas --tenant automatethethings agent account create --provider codex \
     --display-name reviewer \
     --credential-secret-ref agents/codex/reviewer/auth_json \
     --credential-secret-file .codex/auth.json
   ```

4. **Rotate** by writing a new version of the secret — nothing else:
   `quicbaas secrets put agents/claude/claude1/oauth_token --from-stdin` (paste the
   new token at the hidden prompt, or pipe it in).
   Every spawn picks it up on its very next resolution, on the control plane
   and on every worker host alike: a referenced account's release is
   single-use, so no host serves a cached copy of a secret it cannot see change
   (see [Rotation](#rotation)).

**Updates are conditional.** `UpdateAccount` reads the account, applies the
request, and writes with `AccountStore.UpdateIfUnchanged`, which lands only if
the stored record's `updated_at` is still the one it read. A write that lost a
race re-reads and re-applies the request, up to five times. So two concurrent
updates, such as a rename and a switch to a secret reference, both take effect,
and neither can save a stale copy over the other. Revoke uses the same loop.

**The backend decides, not a lock.** The service layer and the orchestration
runtime each build their own account store over one KV, and every daemon has
its own, so no in-process lock can order their writes. Every write of an
existing account record — `UpdateIfUnchanged`, `Update`, and `Touch` (usage
and refresh stamps, status, cooldown, expiry) — commits with the KV's
compare-and-swap against the exact bytes it read. A write that loses is never
applied: an update reports `AccountVersionConflictError` (REST `409`, gRPC
`Aborted`) and the service re-reads; a touch re-reads and re-applies its field
changes itself. A store without compare-and-swap is refused rather than written
to unconditionally.

**A rotation cannot resurrect a switched account's envelope.** An inline
`rotate` checks the account is inline, writes the new envelope, then stamps the
record with `RequireInlineCredential`, which the store decides on the record it
commits over. If a switch to a secret reference committed in between (and
destroyed the inline envelope, which the rotation has just re-written), the
stamp is refused with `AccountCredentialSourceChangedError` (`409` / `Aborted`),
nothing reaches the record, and the rotation deletes the envelope it wrote.
**Automatic OAuth renewal is held to the same rule**: its success and failure
stamps require the account to still be inline, and a renewal overtaken by a
switch removes the envelope it wrote back and returns the same error instead
of handing out the renewed token.

**Automatic stamps never undo an operator.** Every stamp the runtime makes on
its own — a renewal's outcome, a failed secret read — refuses to replace
`revoked`. A failed-read stamp is also conditional on the account still reading
the reference that read attempted, as last applied (`credential_secret_ref`,
`credential_secret_file` and `last_refreshed_at` all unchanged). An operator
who re-points the reference, or writes the secret and re-applies the same one,
while an earlier read is failing has already answered that failure, so the
stale `error` is not stamped over the working account.

**A failed secret read reports no backend detail.** The refusal's message names
the reference and a failure class ("the secret does not exist in this tenant",
"the secret could not be read") and nothing else. Its underlying secrets-service
error stays in process: it is not reachable through the error chain, so no
transport (REST, gRPC, MCP) can serialize that service's diagnostics in place of
the sanitized message.

## Selection

Selection is a pure function over the metadata half:

- **`account_id`** pins one account. No strategy runs; that account or nothing.
- **`account_auto`** runs a strategy. The default is `least_usage`, which picks
  the account serving the fewest live sessions, then the one with the least
  recorded usage (see [Balancing](#balancing-and-session-caps)). `static`
  (always the scope default) stays available for a caller that names it.
- **`account_allow` / `account_exclude`** narrow the candidate list for one
  resolution. Rotation uses `account_exclude` to guarantee forward progress.
- **An Agent's `account` policy** supplies the strategy and the allow-list for
  every spawn by that Agent, unless the spawn pins `account_id`. The precedence
  and the checks made on save are in [agents.md](agents.md#accounts).

An account is a candidate only if it is *selectable now*: `active`, or
`limited` with a hold that has lapsed. `revoked` and `error` need an operator;
a live limit hold clears itself at `available_at`. A static-token account whose
declared expiry is less than two minutes away is not handed to a **new**
session either (see [the expiry buffer](#the-expiry-buffer)).

All of that is `orchestration.AccountResolver`, and it runs **unchanged** on
both sides of the seam below.

## Where the credential actually goes

### In-process (control plane runs the agent)

The resolver reads metadata from the KV, the credential from the barrier-backed
secret store, merges the provider descriptor's defaults, and projects the result
into the subprocess: environment variables directly, file-based material into a
fresh per-session directory that is bind-mounted read-only into the container.
Nothing crosses a network.

### Remote (a fleet node daemon runs the agent)

This is the interesting case, and the one go-quicbaas#734 built.

The central dispatches a Spawn to the daemon carrying only the **selectors** —
`account_id` / `account_auto` / `account_strategy` / `account_allow` /
`account_exclude`. **The credential is never in a Spawn RPC.** The daemon
resolves it for itself:

```
daemon spawn
  └─ orchestration.AccountResolver          ← the SAME resolver the central runs
       ├─ orchestration.AccountStore        ← pkg/fleet: GET /api/v1/agent-accounts
       ├─ orchestration.AccountTokenManager ← pkg/fleet: read-through credential store
       │    └─ POST /api/v1/agent-accounts/{id}/materialize
       └─ orchestration.AccountStrategyRegistry
            └─ GET /api/v1/agent-accounts/{id}/usage
```

Only the two READS underneath the resolver change. Everything about *which*
account gets chosen is identical on both sides by construction, not by two
implementations agreeing.

The daemon's calls go over the channel it already has: the mTLS client
certificate the deploy flow issued it and the bearer its enrollment minted —
the same `*http.Client` and `TokenSource` the heartbeat and the event forwarder
share.

## The release verb

`POST /api/v1/agent-accounts/{id}/materialize` is **the only endpoint in this
stack whose response body is secret material.** It is correspondingly narrow.

**Authorization.** It requires `agent_accounts:materialize`, which is *not* the
read verb that lists the pool: an identity that may see the pool's metadata must
not thereby be able to extract every login in it. An enrolled fleet node daemon holds
it through the tenant-scoped `agent-runtime` role (`iam.FleetRuntimeGrant`).

**Tenancy is the caller's own.** The tenant comes from the authenticated
principal, never from the request. A `tenant_id` naming anything else is refused
403, and a principal with no tenant binding is refused outright.

**Refusals.**

| Condition | Answer | Why that answer |
| --- | --- | --- |
| Account is `revoked` or `error` | `409` | The caller is entitled to it and it exists — the account's STATE refuses. |
| Live limit hold | `409`, with `available_at` | Same, and it clears itself. |
| Account fenced to another project | `403` | No retry or reshaped request by this caller will release it. |
| No such account in the caller's tenant | `404` | Indistinguishable from another tenant's account, deliberately. |
| No token manager wired | `503` | The deployment's fault, not the caller's. |
| `credential_secret_ref` does not resolve | `409`, with `fault_class: credential_not_found` | The account exists and is the caller's; its credential does not. The class is what a daemon parks the work on. |

A refusal whose failure is a spawn configuration fault carries its class in the
error body's `fault_class` — the same vocabulary as the gRPC spawn evidence
(`orchestration.SpawnFaultClass`). A class the daemon's build does not know is
ignored, leaving the refusal an ordinary charged failure. The daemon decodes the
whole error body (bounded at 64 KiB) before it shortens the message it keeps to
512 bytes, so a long refusal never loses its class.

**The refresh grant does not travel.** The released envelope carries the access
token, the environment variable to project it into, static env and file
material, and the expiry. It carries **no refresh token, no token endpoint and
no client id**: renewal is the central's job and already happened inside the
call that produced the envelope, so a recipient that must not refresh does not
receive the means to.

**One audit event per attempt**, success or refusal, naming both principals of
the release — which agent took which account. An agent repeatedly asking for a
revoked account is exactly the pattern an operator wants to see.

**The release is the use.** The central stamps `LastUsedAt` / `LastValidatedAt`
when it releases. On an account served to a remote daemon, `LastUsedAt`
therefore means *"last released to a worker host"* — which is the only thing the
central can truthfully observe about a spawn on a host it does not run.

## Credential delivery leases

Every hand-over of a pooled credential takes a **delivery lease**: a grant
scoped to `(tenant, project, job, account)`, short-lived, audited, and revoked
when the session that asked for it ends. It composes
`pkg/lease.LeaseManager` — the same authority `Security().DynamicSecrets()`
issues through (see [../secrets/dynamic-secrets.md](../secrets/dynamic-secrets.md))
— so a delivery grant appears in the same `ListLeases` and is revocable with the
same `RevokeLease` as every other lease in the platform.

The provider credential cannot be removed from the container: the CLI dials the
provider itself and a request proxy was ruled out. What the lease removes is its
**durability**.

### What a lease governs, and what it does not

A subscription account is OAuth. `AccountCredential.ExpiresAt` is the access
token's real lifetime and `account_refresher_oauth2.go` is the authority that
extends it. **That relationship is untouched.** A lease says nothing about how
long the token works; it says how long one *delivery* stays authorised without
renewal. The two therefore cannot fight:

- a lease lapsing does **not** shorten a token the refresher would have kept
  alive — the copy already delivered keeps working until the provider rejects it;
- a token being refreshed does **not** extend a lease — the refresher writes a
  new envelope, and the next delivery of it takes its own grant;
- what a lapsed or revoked lease **does** stop is the next delivery.

Delivery grants are renewable with **no renewal cap**: the bound that matters is
the session's conclusion, which revokes outright. Capping renewals would cut a
long job off at an arbitrary multiple of the TTL — the same "a lease TTL silently
truncating a credential" failure in another shape. `DefaultAccountDeliveryTTL` is
one hour (`orchestration.account_delivery_ttl`), the backstop for a session whose
conclusion never ran because its node was destroyed rather than shut down.

### Both hand-over paths, one authority

| Path | Where the grant is issued | How the session learns it |
| --- | --- | --- |
| In-process spawn (credential into the subprocess env / materialization dir) | `accountResolver.Resolve` | Directly: `ResolvedAccountCredential.LeaseID` |
| Remote release to an enrolled daemon | `MaterializeAccountCredential` | The daemon reports it back on the spawn response (`pkg/fleet/account_grant.go`) |

A release that cannot obtain a grant **fails**: a credential nothing can revoke
is the state the mechanism exists to remove. A deployment with no lease manager
wired (the sealed window) releases unleased, exactly as it did before grants
existed, rather than losing the capability.

**The remote path needs a return journey, and it is not decoration.** The control
plane issues the grant when it releases the credential — but the release happens
*inside* the daemon's account resolution, so the control plane cannot tell which
session the grant it just issued belongs to. Only the daemon can. Its resolver
composes an `AccountDeliveryGrantor` that does not issue anything: it reports the
grant its release was served under, reading it from the credential cache the
release populated. The grant then travels
`ResolvedAccountCredential.LeaseID` → `Session.CredentialLeaseID` →
`SpawnSessionResponse.credential_lease_id` → `SessionRecord.CredentialLeaseIDs`,
which is the only channel by which a remote session's grant becomes revocable at
all. Without it the grant merely aged out on its TTL while claiming to be
governed.

`AccountDeliveryGrantor` is separate from the full `AccountCredentialLeaser` for
that reason: a daemon holds no lease store, so `Validate` and `Revoke` are not
verbs it could implement without a stub. A resolution needs exactly one verb, and
the interface is exactly that verb.

### The enforcement point

`MaterializeAccountCredential` is the only surface whose response body is a live
provider login, so it is the only place the control has to hold.

- **No lease presented** — a first delivery. A grant is issued. This case is
  governed by the checks that already existed: account status, project fence,
  spend cap.
- **A lease presented** — a *refresh* of a copy the caller already holds. The
  grant is validated, and revoked / expired / wrongly-scoped are all refusals.
  The caller's belief about its own copy is irrelevant; the lease store decides.

The daemon's read-through cache presents the grant it was released under on every
refresh (`pkg/fleet/account_secrets.go`). That is what makes the distinction
real: without it every refresh would look like a first delivery and revocation
would be unenforceable. `lease_id` travels on the REST body the daemon uses;
`credential_lease_id` comes back on the remote spawn response, and `job_id` goes
down on the remote spawn request.

**A daemon's grant is scoped `(tenant, project, account)`, not by job.** It
leaves `job_id` empty on the release, deliberately: its credential cache is keyed
by account, so one release serves every job running on that host and a grant
naming one of them would be false — and validating a later refresh against it
would then refuse the daemon its own copy. An in-process resolution knows its
job and names it. A caller that releases for a single job names it too and gets a
job-scoped grant.

**The honest limit.** Revoking one delivery stops the host that holds that copy
from replacing it; it does not disable the account. A brand-new delivery is a
fresh authorization decision, and disabling the login outright is the account
status control's job (`AccountStatusRevoked`).

A scope is part of the grant. A live lease for another account, tenant, project
or job is refused with `AccountLeaseScopeError` — accepting it would let one
authorised delivery unlock every other one. `AgentID` is deliberately *not* a
scope check: the transport binds it from the authenticated principal, so the
comparison would be against itself.

### The reaper is a security control

`SessionRecord.CredentialLeaseIDs` carries every grant a session was handed —
one per hand-over, so a session that has rotated carries the grant of every
account that ever served it. It is on the **durable** record because the route
that matters most has nothing left in memory: the node died, the job failed, the
daemon left the fleet. `session_reconcile.go` concludes such a record from the
store alone, so the grants have to be readable from the store alone.

`concludeBackend` withdraws them, and **its failure is returned**: a session
whose grant could not be revoked is not concluded, the record stays as it was,
and the next pass retries the whole teardown. Logging it and marking the session
closed is the one outcome nobody can repair, because a closed record is never
revisited. Revocation is idempotent, so the retry converges.

**A shared grant is withdrawn by its last holder.** An in-process spawn leases
its own hand-over, so its grant is its own. A daemon's grant is per account on
that host, so two concurrent sessions there can be served under one grant — and
withdrawing it when the first of them ends would refuse the other's next refresh
and break a job nobody ended. So the conclusion first asks the durable session
records who else is still being served under each grant
(`credentialLeasesHeldElsewhere`), and withdraws only the grants nothing live
still holds. A record in `closed` or `error` is not a holder: it has no process
to refresh with, and a resume re-resolves and takes a grant of its own. A store
that cannot answer the scan **fails the conclusion** rather than revoking on the
assumption that nobody else holds it — the conclusion is retryable, a broken
running session is not.

So "revoked on session close" reads, precisely: *revoked once no live session is
still being served under it.*

A rotation withdraws the grant it supersedes and records the replacement in the
same step, before the record is written — so a later conclusion still withdraws
every grant the session ever held.

A spawn whose account resolution succeeded and whose dispatch then failed returns
no session, so `ACPBackend` withdraws that grant itself. It is the only
revocation the backend owns.

### No secret in any lease or audit record

A lease's metadata is identifiers only — kind, tenant, account, provider,
project, job, agent. No field of an `AccountCredential` is ever written to a
lease, a log line or an audit event: the trail records **which grant** a login
was released under, never the login. `pkg/orchestration`'s
`TestAccountCredentialLeaser_NoSecretInLeaseOrAudit` and `pkg/service`'s
`TestMaterialize_AuditNamesTheGrantAndNeverTheCredential` pin that.

## The daemon's own store

`pkg/fleet` supplies an `interfaces.SecretsManager` whose reads are satisfied
from the release endpoint, and plugs it into the ordinary
`orchestration.AccountSecretStore`. Exactly one method does anything; every
write-ish method refuses with a typed error, because each one would otherwise
mean the daemon believes it owns credential material it does not.

**The credential lives in the daemon's process memory and nowhere else.** It is
never written to disk, never formatted into a log line, and never put in an
error. A worker host has no barrier, no KV and no encrypted-at-rest backend, so
the only durable option available would be a plaintext file — a live provider
login at rest, in the clear, on the least-trusted host in the deployment. The
cache's whole job is served in memory; persistence would buy only survival
across a daemon restart, which costs one mTLS round trip to recover.

### Cache lifetime

| Envelope | Served from cache until |
| --- | --- |
| Declares an expiry | Two refresh skews before it, capped at 15 minutes |
| Declares none (typical static token) | 15 minutes |
| Already inside its renewal margin | Not cached at all |
| Released `single_use` (a `credential_secret_ref` account) | Not cached at all: re-released for every spawn |

The two-skew margin exists so the token manager's *renewal* path is unreachable
rather than merely unlikely: the daemon holds no refresh token, so it
re-materializes first and lets the central renew on the side that owns the
grant.

### Rotation

An operator rotating a credential on the central reaches the worker host **on
the very next spawn**, with no polling and no push:

1. `POST /agent-accounts/{id}/rotate` replaces the envelope and stamps
   `last_refreshed_at` on the metadata record.
2. The daemon's resolver reads that record before it reads any credential — it
   always does, because selection comes first.
3. A `last_refreshed_at` newer than the one the cached envelope was released
   under evicts the entry, and the next read re-materializes.

A **referenced** account (`credential_secret_ref`) is rotated by writing its
secret, and that also reaches the **very next spawn** on every host — with no
account write. The secret write moves neither timestamp, so step 3 cannot see
it; instead the control plane marks such a release `single_use` (on the REST,
gRPC, MCP and embedded wires alike), and the daemon serves it to the spawn
that asked and does not keep it. The next spawn re-releases, and the control
plane reads the secret afresh.

Why not cache it and key the cache on the secret's version instead? Because
the daemon cannot learn that version without asking the control plane, and
that round trip costs what a release costs — so there is nothing cheaper to
check. The price is one release (and its audit event) per spawn of a
referenced account; inline accounts keep the cache.

`last_refreshed_at` is the signal, not `updated_at`. `updated_at` moves on any
write — including the usage stamp the release itself makes — so a cache keyed on
it would invalidate the entry it had just created and never return a hit.

## Permissions

| Verb | Permission | Scope |
| --- | --- | --- |
| List the pool | `agent_accounts:list` | Tenant (`ScopeSelf`) |
| Read one account / its usage | `agent_accounts:read` | Tenant (`ScopeSelf`) |
| Release a credential | `agent_accounts:materialize` | Tenant (`ScopeSelf`) |
| Create / update / delete / rotate / revoke / set-default | `credentials:*` | **System tenant only** (`ScopeCross`) |

The reads take QuicBaaS's own tenant-scoped family rather than go-qrdb's
`credentials:*`, and that is a correction rather than a split for its own sake.
`credentials:*` is registered `ScopeCross` because qrdb's named-credential store
has no tenant namespacing at all; a pooled account is the opposite — every
record is written into the requesting tenant's own keyspace. Under
`credentials:*` the RBAC cross-scope rule denied every tenant identity a surface
that is tenant data, including the enrolled daemon that has to select over it.
This is the same correction go-quicbaas#534 made for `/api/v1/secrets`.

The **write** verbs deliberately still take `credentials:*`, so minting or
rotating a pooled login remains a system-tenant operation. Widening that is a
policy change in its own right.

Built-in role placement: `Admin` holds all three `agent_accounts:*` verbs;
`Operator` holds the two reads. The Security Officer holds none — a pool listing
is operational data, not module governance, and releasing a credential is
neither governance nor a cryptographic primitive (FIPS 140-3 AS10.03).

## Transports

| Transport | Release verb |
| --- | --- |
| REST | `POST /api/v1/agent-accounts/{id}/materialize` |
| gRPC / Unix | `quicbaas.account.v1.AccountService/MaterializeAccountCredential` |
| MCP | `quicbaas_agent_account_materialize` |
| Embedded | `quicbaas.Account/Materialize` |
| SDK | `sdk.Account().Materialize(ctx, req)` |

QUIC carries no account verbs at all today — the whole family predates that
surface — so the release verb matches the family rather than extending it.

## Wiring

**Control plane** (`pkg/server/lifecycle_orchestration.go`): the account service
is handed the same `AccountTokenManager` the in-process resolver uses, so an
`oauth_refresh` account is renewed on the side that owns its refresh token
before the envelope is released. Without that wiring the verb fails closed with
`503`.

**Daemon** (`cmd/quicbaas/fleet_serve.go`): `agent.NewAccountPool` composes the
resolver over the daemon's existing control-plane client and token source, and
`Runtime.SetAccountResolver` attaches it to every ACP backend the daemon serves.
A daemon with no resolver refuses every pooled spawn with
`account: resolver not configured` — which is fail-closed, not degraded.

## The spend cap

Both release paths above — the in-process resolver and the release verb — ask
the SPEND AUTHORITY before they produce a credential. An account carrying an
`AccountSpendPolicy` whose sliding window is spent, or whose circuit breaker
has tripped on its own failure history, is refused with a typed error the
dispatcher and the forge comment both render; an account carrying no policy is
unmetered and behaves exactly as it did before. Both ask BEFORE the envelope is
resolved, so a refused release never renews an `oauth_refresh` credential on
behalf of work that is about to be refused.

For an automatic selection the check is first a **candidate filter**, run
before the strategy ranks (go-quicbaas#1058, ruling F6): a spent account is
skipped and the pool fails over to a sibling with headroom. A pool whose every
account is spent is `AccountsSpentError`, a deferral until the first one frees
up, never a park. A worker daemon reads each account's verdict from the
`spend` field of the usage call.

The policy — sliding windows in tokens and/or list-price dollars, and the
failure breaker — is settable on create and update over every transport and
the CLI (`--spend-window`, `--breaker-failures/--breaker-window/--breaker-cooldown`,
`--clear-spend`, `--clear-breaker`; go-quicbaas#781). A malformed window or
breaker is refused at the write as `AccountInvalidError` naming
`spend.quota.*` or `spend.breaker.*`.

See [cost-enforcement.md](cost-enforcement.md#spend-limits).

## Billing

`Account.Billing` says how the account is billed, which decides the BILLED
figure every turn on it carries beside its list-price equivalent
(go-quicbaas#1058; see
[cost-enforcement.md](cost-enforcement.md#billing)):

| `billing.mode` | A turn bills | Notes |
|---|---|---|
| `subscription` | `$0` — a flat fee does not move with use | `billing.monthly_fee_micros` is the fee, recorded and shown on the account; reports do not pro-rate it yet (deferred to ruling F7) |
| `api` | its list price | a fee is refused: the bill is the usage |
| (not configured) | unknown | never assumed to be a subscription |

It is settable on create and update over every transport: REST (`billing` on
`POST /api/v1/agent-accounts` and `PUT /api/v1/agent-accounts/{id}`), gRPC
(`AccountBilling billing` on `CreateAccountRequest` / `UpdateAccountRequest`),
QUIC and Unix (the REST and gRPC surfaces), MCP (`billing` on
`quicbaas_agent_account_create` / `_update`), the embedded and Go SDK
(`account.Billing`), the TypeScript SDK (`AccountBilling`), and the CLI:

```
quicbaas account update acct_abc123 --billing subscription --monthly-fee 200
quicbaas account update acct_abc123 --billing none      # back to not configured
```

On update an absent `billing` leaves it unchanged; a present empty one clears
it. A malformed billing is refused at the write with the field named
(`billing.mode`, `billing.monthly_fee_micros`).

## Model

Pooling is additive: a `Spawn` that sets neither `AccountID` nor `AccountAuto`
behaves exactly as before — the underlying CLI authenticates however it
already does today, against the daemon host user's global `~/.claude`,
`~/.codex` or `~/.config/opencode`.

| Concept | Type | Lives in |
|---|---|---|
| Account metadata (id, tenant, provider, display name, status, default flag, timestamps) | `orchestration.Account` | `orchestration.AccountStore` — a tenant-scoped KV record, same key-layout pattern as `ProjectStore`/`TaskStore` (`orch/account/{tenant}/{id}`) |
| Account secret (env vars, files, OAuth access/refresh tokens) | `orchestration.AccountCredential` | `orchestration.AccountSecretStore` — `interfaces.SecretsManager`, encrypted under a **tenant-specific DEK** (`StoreOptions.TenantID`), never touched by `AccountStore` |
| Selection policy | `orchestration.AccountSelectionStrategy` | `StaticAccountStrategy` (pin / tenant-provider default) and `LeastUsageAccountStrategy` (lowest cumulative tokens, via `CostTracker`), registered in `AccountStrategyRegistry` |
| Token freshness | `orchestration.AccountTokenManager` | renew-before-use with a skew window, single-flight per tenant+account (mirrors `pkg/cli.TokenManager`) |
| Renewal mechanics | `orchestration.TokenRefresher` | `OAuth2TokenRefresher` — a generic RFC 6749 §6 `refresh_token` grant client, provider-agnostic |
| Putting it together | `orchestration.AccountResolver` | resolves a request into env vars + files ready to hand to a spawn, records `LastUsedAt`, emits an audit event |
| API surface | `service.AccountService` | tenant-enforced CRUD; scopes every request's context to the request's tenant so an account the system admin creates for tenant T is written into T's namespace, where T's spawn path reads (go-quicbaas#535); never returns credential material |

Account metadata is **never** a secret and is safe to list/log. The
credential envelope is fetched only inside `AccountResolver.Resolve`, merged
into the spawn's environment/files, and never returned by any
`AccountService` method.

## Selection strategies

`AccountSelectionRequest{TenantID, ProjectID, Provider, PinnedAccountID}` goes
through one of:

- **`least_usage`** (the production default, on the control plane and on every
  worker daemon; `NewBalancedAccountStrategyRegistry`) ranks the candidates
  by, most significant first:
  1. **live sessions**: an account serving none beats one serving two;
  2. **recorded usage**: cumulative **tokens** from
     `CostTracker.GetAccountUsage`. Tokens, not spend or request count,
     because tokens are what `CostTracker` measures, and a request count would
     treat a one-line prompt and an hour-long agentic session as equal load;
  3. **the default account**, so `default: true` means *preferred among
     equals*, never *always*;
  4. **least recently used** (`last_used_at`);
  5. the account ID, so the answer is deterministic.

  An account at its `max_concurrent_sessions` cap is not a candidate. A pool
  whose every candidate is capped answers `AccountsSaturatedError`.
- **`static`** uses `PinnedAccountID` when set, otherwise the candidate with
  `Default == true`, otherwise a lone remaining candidate. Ambiguity (more
  than one undesignated candidate) is a `NoAccountAvailableError`, never a
  silent pick. It is registered but no longer the default: with static as the
  default, the burn-in pool of three Claude accounts ran every worker on the
  one marked default until the provider refused it (go-quicbaas#1046).

A project-scoped selection (`ProjectID` set) that finds no active candidates
falls back to the tenant-wide pool for the same provider before failing.

New strategies register into `AccountStrategyRegistry` by name — no change to
`AccountResolver` is required.

## Balancing and session caps

Three inputs decide which pooled account a new session gets. The
live-session count and the session cap are described here. The recorded usage
is described in [Cost attribution](#cost-attribution).

### Live sessions

The count is read from the durable **session records**
(`NewAccountSessionCounter` over the agent store). Every record in state
`active` or `idle` counts against its `account_id`, whichever host resolved
the account. The delivery leases are the wrong signal: a worker daemon caches
a released credential per account and serves every session it spawns on that
account under **one** grant, so a lease count undercounts by exactly the
concurrency being measured. The session record is already what the session
conclusion scans to decide whether a shared grant is still in use.

The live count also includes every **reservation** no record has satisfied yet
(see [Reservations](#reservations-the-count-while-spawns-are-in-flight)), so a slot claimed
by a spawn in flight is seen by everyone else at once.

One lookup has four readers:

| Reader | Uses it to |
|---|---|
| the resolver (`WithAccountSessionReserver`) | drop capped candidates **before** the tenant-wide fallback decision, and reserve a slot on the account it selects |
| `LeastUsageAccountStrategy` | rank the candidates on the counts the resolver already read |
| admission (`WithHeadroomSessions`) | treat a capped account as having no headroom |
| `GetAccountUsage` (`live_sessions`, and `spend`, the spend verdict) | report it, so a worker daemon balances and fails over on the control plane's figures and not only on its own sessions |

The cap is applied before the fallback decision on purpose. A project whose own
accounts are all at their cap falls back to the tenant-wide pool, just as a
project whose own accounts are all held does. Only when the fallback pool is
capped too is the resolution `AccountsSaturatedError`.

A count that cannot be read **fails closed** (`AccountSessionCountError`),
both at selection and at admission. A count that was never read is not zero.

### `max_concurrent_sessions`

`Account.MaxConcurrentSessions` is the operator's statement of how many
sessions the subscription serves at once. **0 is unmetered**, which is how
every account written before the field existed behaves. QuicBaaS cannot
discover this figure; it is a property of the provider plan. It is set on
create and update on every transport:

```
quicbaas account update claude1 --max-concurrent-sessions 2
quicbaas account update claude1 --max-concurrent-sessions 0   # unmetered
```

REST/QUIC/MCP/Unix use `max_concurrent_sessions`, gRPC uses
`max_concurrent_sessions` (an `optional int32` on update, so an absent field
leaves the cap unchanged), and the SDK uses `MaxConcurrentSessions *int` on
`UpdateAccountRequest`. A negative cap is rejected at the store.

At **admission**, an account at its cap has no headroom. A project whose
pool is entirely capped is **deferred** and its share goes to other projects,
as it would be for a spent usage window. When every ready account is capped,
the project may run at most the pool's free sessions
(`PoolHeadroom.SessionBounded` / `SessionSlots`). The accounts may be
shared by several projects, so two projects can still race for the last slot.
The reservation settles that race: the loser gets `AccountsSaturatedError`,
fault class `accounts_saturated`. That class is refunded and requeued, never
parked.

### Reservations: the count while spawns are in flight

Counting records alone lags the truth. A session record is written only after
its spawn succeeds, so a burst of resolutions all read the pre-spawn count. With
a cap of one, every resolution passes it. With no cap, every resolution picks
the same account, the one that was least busy a moment ago. The control
plane's session counter is therefore also a **reservation table**
(`AccountSessionReserver`), and every resolution claims a slot in it:

1. **Occupancy is counted for every account, and the cap is enforced only
   where one is set.** Every resolution reserves on the account it selects,
   whether pinned or auto and whether metered or not (`Reserve`). The
   reservation is counted live for every reader at once. The count and the
   claim are decided under one per-tenant lock, and the session records are
   read inside it. An account already at its `max_concurrent_sessions` cap is
   refused with `AccountsSaturatedError`.
2. **Selection and claim are one decision.** An automatic selection claims
   *conditionally* on the live count it selected by (`ExpectLive`). If another
   claim has moved that count, the claim is refused with
   `AccountSessionCountChangedError`, and the resolver selects again on the
   new counts (`maxClaimAttempts`, 8). The final attempt claims
   unconditionally, with the cap still enforced. A burst of N resolutions
   therefore converges within N attempts and spreads across the pool. A
   pinned account has nothing to re-select to and is claimed unconditionally.
3. **A reservation is satisfied only by the record that names it.** The
   reservation ID travels from `ResolvedAccountCredential.ReservationID` onto
   the `Session`, across the daemon's spawn response (`reservation_id`), and
   onto the durable `SessionRecord.ReservationIDs`. A resume or rotation adds
   its own reservation. A record naming the reservation, in any state,
   satisfies it, and the record then counts the session itself. Nothing is
   matched by account, job or time, so two spawns on one account with no job
   can never satisfy each other's reservations.
4. **Give-back and expiry.** If anything later in the resolution fails, the
   resolver gives the slot back; if the **spawn** fails, the ACP backend
   does (`AccountReservationReleaser`, implemented by the resolver). A
   reservation that is neither named nor released expires after
   `AccountSessionReservationTTL` (15 minutes). That is longer than any spawn
   takes to produce its record, and short enough that a slot orphaned by a
   crashed daemon frees within one subscription window.

A **worker daemon** selects its account itself, so it reserves against the
control plane's table rather than one of its own. A per-host table would let
every host crowd onto, or fill, the same account. The daemon account client
uses:

- reserve: `POST /api/v1/agent-accounts/{id}/reservations`
  `{expected_live?}` → `{reservation_id}`. It answers **409** for an account
  at its cap and **412** for a conditional claim whose count moved. The
  daemon rebuilds each as its typed error.
- release: `DELETE /api/v1/agent-accounts/{id}/reservations/{rid}`.

**The tenant is always the authenticated caller's.** On REST it comes from the
principal, and a `tenant_id` query naming another tenant is refused. On gRPC
(`ReserveAccountSession` / `ReleaseAccountSession`) the authenticated subject's
tenant is used: a request naming another tenant answers PERMISSION_DENIED, and
an unauthenticated call answers UNAUTHENTICATED. MCP
(`quicbaas_agent_account_reserve_session` / `_release_session`) refuses a
`tenant_id` argument naming another tenant. The embedded transport refuses one
when the context carries a tenant-scoped subject. This is the same binding the
credential release uses. Both verbs carry `agent_accounts:materialize`, because
they accompany that release. On gRPC a full account is RESOURCE_EXHAUSTED and a
moved count is ABORTED. The SDK exposes both verbs as `ReserveSession` /
`ReleaseSession` over every transport.

The table lives in the control-plane process. Every deployment this ships to
has one control plane per tenant pool. A second control-plane node would keep
its own table, and the counts would then hold per node, not across nodes.

### The expiry buffer

A **new** session is not started on a credential that expires within
`AccountCredentialExpiryBuffer` (2 minutes). This applies only to credentials
QuicBaaS cannot renew (`static_token` with a declared expiry). An
`oauth_refresh` account is renewed by the token manager at resolve, and an
account with no recorded expiry is unaffected.

Naming an account does not bypass the buffer: a pinned **new** session on an
account inside it is refused with `AccountCredentialExpiringError`. Only a
resume (a resolution carrying `ReplacesSessionID`, which reconstitutes a
session already running on that account) is exempt.

### When the whole pool is unavailable

The whole pool can be found unavailable in two places, and both report through
the same function (`reportAccountPoolExhausted`):

- **At spawn**: a resolution that finds every candidate on a limit hold
  (`AccountsCoolingDownError`) or at its cap (`AccountsSaturatedError`).
- **At admission**: a project whose pool has no headroom is **deferred**, and
  no spawn is attempted. This is the common case, because a pool that stays
  exhausted never reaches a spawn. Admission asks every sweep, so it reports
  on the **transition**: once when the pool goes to zero, again only if the
  reason changes, and again after the pool has recovered and emptied once
  more. The provider label is `any`, because admission asks about the pool
  before it knows which provider a task will name.

Each report goes to three channels:

- an audit event, `orchestration_account` / `pool_exhausted`, whose resource ID
  is `pool/<project|_none>/<provider>`;
- the counter `quicbaas_orch_account_pool_exhausted_total{tenant,provider,reason}`,
  where `reason` is `cooling_down` (held or expiring; at admission also a
  spent window), `saturated`, or `spent` (a resolution whose every candidate
  was refused by its own spend policy, go-quicbaas#1058);
- a warning on the daemon log.

An empty pool is a configuration fault and is not reported as exhausted. The
Needs-you inbox (go-quicbaas#1020) is the intended home for these reports. When
it lands, it should add an `account_pool_exhausted` reason built from the same
error. Until then, these three channels are the only place the condition shows.

### The default account

Exactly one account per **scope** may be the default, where a scope is
`(tenant, provider, project)` — `project` being the account's own `ProjectID`,
or the tenant-wide scope when it has none. That is the same partition
`AccountResolver` builds its candidate list from, which is what makes the two
agree: a project's default is what an unpinned spawn *in that project* uses, and
it never becomes what another project (or an unscoped spawn) resolves to.

The default is held as a durable pointer,
`orch/accountdefault/{tenant}/{provider}/{scope}`. That pointer is the **sole
authority**: `Account.Default` is derived from it on every read and is never
written into the account record — the same treatment `credential_health` gets,
and for the same reason. A list reads one pointer per distinct scope, not one per
account.

Deriving rather than mirroring is what makes "exactly one default per scope" true
by construction instead of by discipline. `StaticAccountStrategy` selects on this
field, so two records that both claimed it would silently redirect an unpinned
spawn — and its billing — to a different provider login. With one key answering
the question, there is no second copy to drift. It also removed a deadlock: the
pointer swap no longer has to reach into the previous holder's record to clear a
flag, so it never takes a second lock (see [Locking](#locking)).

| Event | Effect on the scope's default |
|---|---|
| **First account created in a scope** | It becomes the default. A pool of exactly one account that still refused to resolve was the wrong answer. |
| **Later accounts created** | Non-default, unless the request sets `default` explicitly (which displaces the incumbent). |
| **`SetDefault`** | Re-points the pointer and clears the previous holder's flag in one locked step. |
| **The default is deleted** | The scope is left **with no default**. No survivor is promoted. |
| **The default is revoked** | The pointer is unchanged; a revoked account is simply never a candidate. |
| **An account is moved between scopes** (`Update` changing `project_id`) | Treated like a create into the destination: it claims that scope if vacant, arrives non-default if not, and releases the scope it left. |

`Account.Default` is wire-visible and unchanged in meaning on all six transports:
gRPC (`is_default`), REST / QUIC / MCP / Unix (`default`), and embedded. The three
that reproject an `Account` forward the derived value; the three that serialize the
service response directly get it from `Account.MarshalJSON`. Guard tests cover each
so the value cannot drift per protocol.

The create-time claim, the move re-evaluation, and `SetDefault` all take the same
per-scope lock, so concurrent imports into an empty scope produce exactly one
default — never two, and never none.

### Locking

Two keyed mutexes, with one rule: **`defaultMu` is a leaf.** `recordMu` (per
account record) may be held while taking `defaultMu` (per scope pointer); nothing
may be taken while holding `defaultMu`.

That rule is what a derived `Default` buys. While the flag was mirrored onto each
record, re-pointing the scope had to clear the previous holder's flag, which meant
taking that account's `recordMu` while `defaultMu` was held —
`recordMu(A) -> defaultMu -> recordMu(B)`. Two `SetDefault` calls on two accounts
in one scope that were each other's incumbent inverted it and wedged forever
(`keyedMutex` has no timeout, so the request goroutines leaked until restart).
With no flag to clear, `SetDefault` is a single-key write and the inversion cannot
be expressed. `TestAccountStore_SetDefault_ConcurrentMutualDisplacement_DoesNotDeadlock`
is the regression guard.

**Why deleting the default promotes nobody.** Choosing among the survivors is the
exact ambiguity `StaticAccountStrategy` refuses to resolve by guessing, and
promoting by age would silently redirect every unpinned spawn — and its billing —
to a different provider login with no operator action. What happens instead is
the safe pair already built into selection: one survivor still resolves through
the lone-candidate rule, and several fail loudly with `NoAccountAvailableError`
until the operator runs `set-default` or adds the next account (which claims the
vacancy, exactly as the first one did).

**A resume re-claims its own slot.** After a restart, a persistent session keeps
its durable status and has no process. Its resume pins the same account and
names the session it reconstitutes (`SpawnRequest.ReplacesSessionID`). The tally
leaves that session's own record out, so a session on a cap-one account does
not refuse its own resume. Any other session still counts. The field stays on
the control plane (spawn wire contract deny-list).

## Materialization (isolation guarantees)

- **Env-based credentials** (a single opaque token/API key — the common case
  for Claude's `CLAUDE_CODE_OAUTH_TOKEN` and Codex's `CODEX_API_KEY`) are
  merged directly into `SpawnRequest.Env`. There is no disk footprint for a
  raw-exec spawn: the credential lives only in the spawned process's own
  environment and disappears when it exits. Two concurrent sessions never share
  anything.
  - **In a sandbox**, that environment has to be handed to `docker`/`podman`,
    and it is handed over as a private `--env-file` (mode `0600`, in a `0700`
    per-launch directory) — never as `-e KEY=VALUE` arguments, which would
    publish the credential in the world-readable `/proc/<pid>/cmdline` of the
    run process. Log lines print `[redacted]` in place of every environment
    value. The file is removed when the detached run process exits, or on
    session `Close` for a long-lived sandboxed ACP session; that release has
    its own cleanup slot, so an account rotation cannot delete a file the
    running container is still reading. See
    `pkg/orchestration/container_env_file.go`.
- **File-based credentials** (e.g. OpenCode's `auth.json`) are written into a
  **fresh, per-session temporary directory** created by
  `materializeAccountFiles`, never into the operator's real `~/.config`.
  - **Host mode**: the subprocess's isolated `HOME` (already created per
    session to avoid OpenCode lock-file/DB collisions) points at that
    directory instead of a bare empty one.
  - **Container/sandbox mode**: that directory is bind-mounted **read-only**
    at the container's expected config path, replacing — not
    supplementing — the pre-existing behavior of mounting the host's shared
    `~/.config/opencode` into every sandboxed session (a fallback retained
    only when no account is selected).
  - The directory's lifetime is owned by the resolver's cleanup callback,
    composed with the existing per-session MCP-cert cleanup
    (`combineCleanups`) and released on session `Close`.

Both modes guarantee: no credential from one account is ever visible to a
session that resolved a different account, and a session with no account
selected is byte-for-byte unaffected by this feature.

## Lifecycle

- **Create** — `AccountService.CreateAccount` writes the credential before
  the metadata record, so a crash between the two writes never leaves a
  listable account with no credential. It also claims the scope's default when
  the scope has none (see [The default account](#the-default-account)) and
  mirrors the credential's declared expiry onto `CredentialExpiresAt`.
- **Rotate** — `RotateAccountCredential` replaces the envelope only;
  metadata (display name, default flag) is untouched, and the account's
  error state (if any) is cleared.
- **Revoke** — `RevokeAccount` sets `AccountStatusRevoked`. A revoked account
  is excluded from every selection strategy on the **next** resolution. A
  session that already captured the credential before revocation is **not**
  killed retroactively — revocation stops new use, it does not tear down a
  running subprocess. Killing an in-flight session belongs to the
  session-lifecycle owner, not to the account subsystem.
- **Delete** — releases the scope's default pointer (if this account held it),
  then removes metadata and credential together (best-effort on the credential
  once metadata is gone, to avoid a TOCTOU window where the resolver could still
  hand out an orphaned credential). The pointer is released FIRST so a Delete
  that returns an error has left the account intact rather than orphaning its
  credential. No survivor is promoted to default.
- **Refresh failure** — `AccountStatusError` with `LastError` set; excluded
  from selection until the next successful refresh (auto-recovers) or an
  operator revokes it.
- **Provider limit** — `AccountStatusLimited` with `AvailableAt` set. Unlike
  every other non-active state this one heals itself; see
  [Limit holds and rotation](#limit-holds-and-rotation).

### Credential health

`Account.CredentialHealth()` derives an operator-facing verdict from
`CredentialExpiresAt` — `unknown` / `valid` / `expiring` / `expired`, where
`expiring` means "inside `AccountCredentialExpiryWarning` (24h)". It is
reported as `credential_health` on every transport and is never stored: the
account record marshals it on the way out (`Account.MarshalJSON`) and the
durable write bypasses that method, so a stale verdict can never be persisted
and read back as fact.

`unknown` is the honest answer for a static token whose expiry the operator
never declared — QuicBaaS does not contact a provider to discover one.

`CredentialExpiresAt` is written from exactly two places, and nowhere else:

- **`CreateAccount` / `RotateAccountCredential`** — from the operator-declared
  `AccountCredential.ExpiresAt`. A rotation to a credential with no declared
  expiry CLEARS the mirror rather than leaving the retired credential's
  timestamp describing one that no longer exists.
- **A successful `oauth_refresh` renewal** — from the token endpoint's
  `expires_in`, alongside `LastValidatedAt` (a renewal the provider honored is
  provider-confirmed proof of life; merely loading a static envelope is not).

### Credential generations (go-quicbaas#997)

A session materializes its pooled credential **once**, at spawn, and never
re-reads it. So the question that decides whether its next prompt can succeed
is: *is the copy this session holds still the copy the account holds?* The
answer is an identity comparison, not a race between two clocks.

**Every copy of an account's material carries a generation**
(`AccountCredential.Generation`), taken from where the material lives:

| Credential source | Generation | Moved by |
| --- | --- | --- |
| Inline | stamped by `AccountSecretStore.Replace` into the same encrypted body as the material: the write instant in Unix nanoseconds, strictly increasing per process | `CreateAccount`, `RotateAccountCredential`, an `oauth_refresh` renewal |
| Secret reference | the referenced secret's **version** number, which every write of the secret advances | writing the secret; no account field moves |

The generation cannot be published apart from the material: for an inline
account it is inside the same `Put`, for a referenced one it is the version
the value was read from. It is an integer naming a copy, not credential
material, so it travels freely.

**The session records the copy it took, before its container starts.**
`SessionRecord.AccountGeneration` (and the matching field on each
`AccountHistory` segment) is the generation the resolver read with the
material it handed the spawn, so a rotation landing between the read and the
container's handshake is visible rather than hidden by a spawn instant later
than the rotation. A remote worker takes part the same way: the released
credential carries `generation` to the fleet host (REST, gRPC, MCP and
embedded release shapes alike), and the host reports it back as
`SpawnSessionResponse.account_generation`. A resume records the generation it
re-materialized.

**The account's current generation is read from the authority.**
`AccountTokenManager.CredentialGeneration` reads the envelope, or the
referenced secret, and nothing else: no renewal, no bookkeeping. The daemon
wires it into the agent service (`SetAccountCredentialGenerations`) so that a
secret rewritten by its owner, which no account field records, is still seen.
`Account.CredentialGeneration` mirrors inline generations onto the metadata
record (monotonically; `accountResolver.Resolve` repairs it on every spawn),
and answers only when no authority is wired.

`Account.CredentialRotatedAt` remains the operator-facing *when* for an inline
account: it moves only where a new envelope was actually written, never on a
refresh the provider refused. It is also the **fallback** evidence for a
session that carries no generation — one spawned before stamping existed.

**What reads it.** `SessionAccountFreshness(ctx, sessionID)` — a narrowed
capability on the agent service, not a method on `AgentService` — reports
whether a live session has been left behind. Every case where the answer is
not knowable — no pooled account on the session, no account store, an account
or secret that cannot be read, neither a generation nor a datable pair of
timestamps — reports **not stale**: replacing a healthy session on missing
evidence is worse than the failure the caller already has. The SDLC runner is
the consumer, and the project decides whether the replacement resumes the
conversation or starts fresh: see
[sdlc.md](sdlc.md#a-rotated-account-resumes-the-session-or-starts-it-fresh).

**Generations are ordered within one source.** A referenced account's versions
and an inline account's write instants are different numbering schemes; an
account switched from inline to a reference has sessions whose recorded
generation is not comparable with the secret's, and they are not reported
stale by the switch alone.

## Limit holds and rotation

An account that hit a rate, quota, or usage limit is not broken: the
credential is fine, it is simply busy. QuicBaaS answers that by holding the
account out of the pool and moving the session to another one.

### The hold

`AccountStatusLimited` + `Account.AvailableAt` is a **self-healing** state.
There is no sweeper goroutine: a lapsed hold is normalized back to `active`
in memory on every decode (`accountStore.load`, `ListByTenant`), and the next
write that touches the record persists it. A limited account with a **zero**
`AvailableAt` is held indefinitely — nothing promised when it would recover,
so guessing "immediately" would put it straight back into the failure.

A **pinned** resolve against a held account fails loudly with
`AccountLimitedError` (carrying `AvailableAt`) rather than being silently
swapped. Only an explicit rotation may move a session off its account.

### Detecting a limit — structured signals first

When a subscription account is spent, Claude Code (2.1.2xx) ends the turn with
text like `You've hit your session limit · resets 9pm (America/New_York)` and,
alongside it, three **structured** signals. The backend that read the stream
checks them in this order, before it looks at any text:

| Source | Signal |
|---|---|
| `rate_limit_event` | a `rate_limit_event` message whose `rate_limit_info.status` is `rejected`; its `resetsAt` (epoch seconds) is the reset |
| `error_category` | an assistant message whose `error` is `rate_limit` or `billing_error` (`overloaded`, `server_error`, etc. are provider-wide and are not limits) |
| `http_status` | `api_error_status: 429` on the result |
| `result_text` | only when none of the above fired: the phrase rules below, over the result text |

The verdict (`TurnFailureEvidence.ProviderLimit`: source, reset, redacted
refusal text) is decided **where the process ran**. It crosses the daemon wire
in the `TurnFailureEvidence` status detail
(`provider_limit_source`, `provider_limit_resets_at_unix`,
`provider_limit_signal`). The control plane never sees the result text, so it
could not decide this itself. A source this build does not know is still
treated as a limit and is renamed `unclassified`, never echoed.

The reset is the `rate_limit_event`'s `resetsAt` when there is one. Otherwise
it is parsed from the text: a machine-readable hint (below) first, then
Claude Code's wall-clock form, `resets 9pm (America/New_York)` or
`resets Sep 23, 10am (UTC)`. A clock time is read as its **next occurrence**
in the named IANA zone, and the zone is required. A time with no zone could be
any of 24 instants and is treated as unparseable. The zone database is
embedded (`time/tzdata`), so a worker container with no zoneinfo still reads
it.

The text rules that follow are the fallback. QuicBaaS does not sit between the
agent CLI and the provider, so without the structured fields it sees only what
the subprocess prints. The classifier is therefore a **heuristic over free
text**. The authoritative fix is a credential proxy.

The rule set is deliberately narrow and fails in the cheap direction:

| Class | Examples | Effect |
|---|---|---|
| strong (shared) | `too many requests`, `rate limit exceeded/reached`, `quota exceeded`, `insufficient_quota`, `usage limit reached/exceeded` | limit |
| strong (claude) | `hit your session limit`, `hit your weekly limit`, `you've hit your limit`, `session limit reached`, `claude usage limit reached`, `5-hour limit`, `weekly limit reached` | limit |
| strong (codex) | `exceeded your current quota`, `check your plan and billing` | limit |
| guarded 429 | `429` **and** one of `status`/`http`/`code`/`rate`/`limit`/`quota`/`retry` | limit |
| veto | `overloaded_error`, `service unavailable`, `context_length_exceeded`, `context window`, `prompt is too long`, `rate limit configuration`, `rate limiting disabled` | **not** a limit |

The vetoes matter more than the matches. A provider-side overload or a
context-window overflow is not this account's allowance, and rotating on one
would walk the whole pool through the same failure. A bare `429` with no HTTP
context is a number, not a status.

Retry hints are parsed when present — `retry-after` (seconds, ms, or HTTP
date) and epoch `resets_at` / `rate_limit_reset` — clamped to
`(0, MaxAccountLimitBackoff]` (24h). With no hint,
`DefaultAccountLimitBackoff` (5h, the subscription session window) applies.
The matched evidence is stored on the account's `last_error` and echoed into
the transcript **after redaction**, so a provider that echoes the presented
token in its error body cannot turn a metadata record into a copy of the
credential.

### Every turn path holds the account

`agentServiceImpl.runPromptTurn` holds the account itself when a turn fails on
a provider limit (`holdOnAccountLimit`). The account is set to `limited` until
the stated reset, `last_error` records the refusal, and the
`quicbaas_orch_account_limit_holds_total` counter is incremented. The turn's
error comes back as `TurnAccountLimitedError`, which unwraps to both the
`AccountLimitedError` (account and return time) and the turn's own evidence.
The synchronous path (every job runner) and the asynchronous path (operator
sessions, which additionally rotate; see below) share this. Recovery is
automatic: once `available_at` passes, the account is selectable again, with
no sweeper involved.

### A rejected login errors the account

A provider can refuse the account's **login** instead of its allowance: a
`401`, `token is expired`, `refresh token already used`, `sign in again`, or
Claude Code's `Please run /login`. No reset time lifts that; a person has to
log the account in again. The backend that read the CLI's output decides this
where the process ran (`TurnFailureEvidence.ProviderAuth`), and the verdict
crosses the daemon wire (`provider_auth_rejected`, `provider_auth_signal`,
redacted). The rules are narrow, like the limit rules: only phrases that name
the login itself, and a bare `401` only next to `unauthorized`, `status`,
`http` or `auth`.

When the control plane sees the verdict (`errorOnAccountAuthRejection`), it:

- puts the account in `error`, which is never selectable, with a `last_error`
  that begins `re-login needed: `. The inbox's account alert then reads
  "Pooled account … needs a re-login".
- returns the turn's error as `TurnAccountAuthRejectedError`
  (`ErrAccountAuthRejected`).

Everything downstream treats that error as a refused account:

- **SDLC worker**: rolls its session onto another account, once per account,
  exactly as for a limit, audited `class=account_auth_rejected`.
- **Failed attempt**: classified `account_auth_rejected`, then refunded and
  requeued.
- **Spawn walking an agent reference**: fails over to the next Agent
  (`account_expired`).

A reviewer dispatch after that selects another account in the pool. If there is
none, it fails at resolution without starting a container, until someone writes
the new login: `agent-accounts/{id}/rotate` for an inline account, or a new
secret version for a referenced one.

### Worker failover (the SDLC runner)

When a worker turn is refused for a limit, the runner rolls the session onto
another account (`rollOffAccount`). This uses the context rollover's
mechanics: it pushes what the session committed, closes it, and spawns a fresh
session on the same branch in the same round. The fresh session's selection
cannot choose the held account. The attempt is not charged. What the new
session keeps is the project's `session_rotation`: on `resume` (the default)
it continues the refused session's conversation (prompt
`account_resumed_note`, audited as `sdlc:session_resumed class=account_limited
account=… available_at=… replayed_turns=…`); on `fresh` it is told once why it
is fresh (prompt `account_rolled_note`, audited as `sdlc:session_rolled
class=account_limited account=… available_at=…`). See
[sdlc.md](sdlc.md#a-refused-account-rolls-the-session-onto-another-account).

Each account is rolled off at most once per run. If a replacement lands on an
account that was already refused, the hold did not take, and the error is
returned rather than looping. The error is then classified `account_limited`:
refunded and requeued. When no account is free, the fresh spawn fails with
`AccountsCoolingDownError`, which requeues the task, uncharged, until the
first hold lifts.

### Rotation policy

Rotation is authorized per session, and a deliberate pin is honored:

| `SpawnSessionRequest` | Rotatable? |
|---|---|
| neither `account_id` nor `account_auto` | no — the session does not use the pool |
| `account_id` only | **no** — a deliberate pin |
| `account_auto` | yes — the whole tenant/project/provider pool |
| `account_allow: [...]` | yes — exactly those accounts |

`account_allow` is an optional, additive **filter**, never a source: an ID it
names that the tenant does not own is simply not a candidate. The policy is
persisted on `SessionRecord` (`AccountAuto`, `AccountAllow`) so it survives a
daemon restart, and `SessionRecord.CanRotateAccount()` is the single place the
decision is made. Remote sessions are excluded — the daemon that owns the
session rotates it.

### Rotation execution

`agentServiceImpl.RotateSessionAccount` runs under the same keyed
single-flight lock as resume (`resumeMu`), so a rotation and a reconstitution
can never leave two agents answering to one identifier. It:

1. holds the outgoing account (`limited` + `AvailableAt`),
2. selects a replacement from the session's own policy with the outgoing
   account **excluded outright**, so forward progress does not depend on the
   hold having landed,
3. puts the session back in service, preferring the cheapest mechanism.

| Spawn path | Mechanism | Cost |
|---|---|---|
| one-shot CLI (`CLIMode && !Persistent`) | `in_place` — `ACPBackend.RotateAccount` swaps the session's stored env map, which the next prompt rebuilds its process env from | free; no process dies, nothing is replayed |
| persistent stream-json CLI | `resume` — close, then reconstitute with a fresh selection | one replay, folded into the spawn handshake |
| sandboxed container JSON-RPC | `resume` — close, then build a NEW container around a freshly materialized credential directory | one replay, charged as a real prompt (the ACP handshake carries no user turn) |
| unsandboxed host JSON-RPC | none | the agent exposes no way to restore a conversation |

The one-shot swap is a **replacement**, not a merge: the environment
variables the outgoing account projected are recorded at resolve time
(`resolvedSpawnAccount.EnvKeys`) and retracted before the replacement's are
merged, so a credential the new account happens not to overwrite cannot
survive.

Sandboxing and rotation are **no longer mutually exclusive**. A sandboxed
session's credential is a bind-mounted per-spawn directory, released by the
`Close` that precedes the resume; `materializeAccountFiles` mints a fresh tree
for the replacement, so no two containers ever share one.

Automatic rotation is hooked on the turn-failure branch of `dispatchTurn` and
runs **after** the terminal `turn_failed` event: the transcript records that
the turn failed and then that the session moved, rather than hiding the
failure behind a retry nobody asked for. The next prompt runs on the
replacement. Every rotation appends a `KindAccountRotated`
(`account_rotated`) session event.

If the replacement cannot be selected after the old process is already gone,
the session is returned to the **resumable** state with the reason on
`SessionRecord.ResumeError` — not left silently dead.

Every mutating store call emits a `StoreAuditEvent` (`create` / `update` /
`delete` / `use`), following the same `StoreAuditor` seam as
`ProjectStore`/`TaskStore`.

## Cost attribution

`CostTracker.RecordTurnUsage(ctx, TurnUsage{TenantID, JobID, AgentID,
AccountID, ProjectID, Tokens})` is the single entry point for attributing one
turn's tokens across every dimension the caller has available — job/agent
(pre-existing `RecordTokenUsage`), account, and project — so a caller cannot
record one dimension and silently skip another through a partial call
sequence. `GetAccountUsage` / `GetProjectUsage` return the cumulative report;
`AccountTokens` satisfies `AccountUsageLookup` so a `CostTracker` plugs
straight into `LeastUsageAccountStrategy`.

`least_usage` is only as good as the ledger it reads, so the ledger is written
on the one path every turn goes through: `agentServiceImpl.runPromptTurn`
calls `recordTurnUsage` after a turn completes, and that is the single call
site of `CostTracker.RecordTurnUsage`. Both the synchronous
(`PromptSession`) and asynchronous (`PromptSessionAsync`) entry points funnel
through it, so a turn is recorded exactly once regardless of how it was
submitted.

Four dimensions are attributed:

| Dimension | Source |
|---|---|
| tenant | `StorageTenant(ctx)` — always present |
| account | the account **currently** serving the session, read from the in-memory session record; rotation stores the replacement there atomically, so turns are charged per segment |
| project | the prompt's `ProjectID`, falling back to the session's |
| job + agent | `TurnAttributionFromContext` — set by the control loop, which is the only caller that knows both. It travels on the context rather than on `PromptSessionRequest` so an operator-facing transport cannot charge its tokens to someone else's job budget. |

Every ledger key is namespaced by tenant ID (`cost:account:{tenant}:{id}`,
`cost:project:{tenant}:{id}`, `cost:{tenant}:job:...`) **and** the store it
binds to is the tenant-routed durable KV, so one tenant's usage can neither be
read by, nor influence account selection for, another.

### Attribution across a rotation

Once a session can rotate, `SessionRecord.AccountID` is the account in force
**now**, not the one "the conversation was billed to" — that answer is a
sequence. `SessionRecord.AccountHistory` records it as closed segments
(`AccountAssignment{AccountID, Strategy, From, To, Reason}`), opened and
closed together with the scalar by `SessionRecord.OpenAccountSegment` so the
two can never disagree. A session that has never rotated carries a single open
segment opened at spawn.

Turns are attributed per segment, not retroactively: the in-memory record's
account ID is an atomic (`sessionRecord.SetAccountID`) that rotation updates
at the moment the move commits, so no account is ever billed for another's
tokens.

### Which backends report token counts

Nothing is estimated. A turn is recorded only when the agent itself published
counts; `PromptResponse.Usage == nil` means "not measured" and leaves the
ledger untouched.

| Backend | Reports usage? | Where from |
|---|---|---|
| `claude` (ACP stream-json CLI mode) | yes | `usage` on the terminal `result` message (the CLI's own turn aggregate), falling back to the per-`assistant`-message `usage` when the turn ends without one |
| `claude` one-shot (`--print --output-format json`) | yes | `usage` on the result object |
| `opencode` (ACP JSON-RPC) | yes | `usage` on the ACP `session/prompt` result (`inputTokens`, `outputTokens`, `thoughtTokens`, `cachedReadTokens`, `cachedWriteTokens`). It is read even when the answer streamed as `session/update` notifications, which is every opencode turn; reasoning tokens are counted as output |
| `codex` exec transport (`codex exec --json`, the worker daemons) | yes | `usage` on the `turn.completed` event. codex's `input_tokens` INCLUDES `cached_input_tokens`, so the fresh prompt is recorded as `input − cached` and the cached part as a cache read |
| `codex` (`CodexExecBackend`, the control plane's text-mode `codex exec`) | **no** | it prints a human-readable `tokens used` section, not a structured field; it is deliberately not scraped |
| `openvino` | yes | the OpenAI-compatible `usage` of every tool round, summed |
| `ollama` (direct) | yes | `prompt_eval_count` / `eval_count`; a response with neither is "reported nothing" |
| API inference (`anthropic`, `openai`, …) | yes | the provider's `usage`; a response with none is "reported nothing" |
| remote daemons (`RemoteACPBackend`) | yes, on the control plane | the daemon sends the usage its CLI reported back on the Prompt response (`PromptSessionResponse.usage_json`); the control plane's `runPromptTurn` charges it. The daemon has no ledger, so there is exactly one record. Before go-quicbaas#1046 the response carried no usage, and every remote turn, which is every factory worker turn, was recorded as costing nothing. |

The ledger keeps every dimension apart: `input_tokens` is the fresh prompt,
and `cache_read_tokens` / `cache_write_tokens` are the prompt tokens served
from and written into the provider's cache (go-quicbaas#1058). They price
differently, so folding them into input — as the ledger did before — left a
cached turn impossible to price. The total, which every quota window sums, is
unchanged.

## Provider registry (token supply)

Every pooled account belongs to a **provider**. A provider descriptor supplies
the OAuth defaults an account did not state for itself, and decides whether
that provider's accounts may run the RFC 6749 §6 refresh grant at all.

| Provider | OAuth-capable | Ships a token endpoint | Ships a client id | Access-token env key |
|---|---|---|---|---|
| `claude` (Anthropic Claude Code) | yes | **no** — operator-supplied | **no** — operator-supplied | `CLAUDE_CODE_OAUTH_TOKEN` |
| `codex` (OpenAI Codex) | yes | **no** — operator-supplied | **no** — operator-supplied | **none** — operator-supplied |
| `opencode` | no (file-based `auth.json`) | n/a | n/a | none |

Neither Anthropic nor OpenAI documents the OAuth application its CLI
authenticates with as a supported API, so **QuicBaaS ships no endpoint or
client id for either** rather than baking in a guessed value. The codex CLI
reads its OAuth material from its own `auth.json` rather than from an
access-token environment variable, so it has no honest equivalent of
`CLAUDE_CODE_OAUTH_TOKEN`; an operator pooling API keys instead sets
`access_token_env_key` (e.g. `OPENAI_API_KEY`) or `env` on the account.

### Resolution precedence

1. The account's own `AccountCredential` field (`token_endpoint`, `client_id`,
   `scopes`, `access_token_env_key`) — **always wins**.
2. The provider descriptor, for each field the account left empty.

Descriptor values are projected onto a **copy** of the stored envelope for the
duration of one resolution and are never persisted into the account, so
changing a provider default takes effect on the next spawn instead of being
frozen into whatever was written at import time. No vendor endpoint is ever
hardcoded into the credential path itself: `OAuth2TokenRefresher` still reads
`AccountCredential.TokenEndpoint` and nothing else.

### Configuring providers

`orchestration.account_providers` defines additional providers and overrides
built-in defaults. Built-ins are merged first; a configured entry replaces only
the fields it sets.

```yaml
orchestration:
  account_providers:
    # Override a built-in: everything not listed (the env key, display name)
    # keeps its shipped value.
    - name: claude
      override: true                                   # required for a built-in
      token_endpoint: https://auth.example.com/v1/oauth/token
      client_id: 9d1c-...

    # Define a provider QuicBaaS does not ship.
    - name: acme-llm
      display_name: ACME LLM
      token_endpoint: https://id.acme.example/oauth2/token
      access_token_env_key: ACME_LLM_TOKEN
      scopes: [inference]
```

Validation (`config.Validator`) rejects: an empty name, a duplicate name, a
name that shadows a built-in without `override: true`, `override: true` on a
name that is not built in, a token endpoint that is relative, non-`https`, or
host-less (a refresh token must never traverse a cleartext connection), and a
custom provider that declares no defaults at all.

A provider becomes oauth-capable when it is a built-in that says so, or when
configuration gives it a token endpoint — which is how the otherwise
file-based `opencode` provider can be opted into the refresh grant.

### Refresher registration

`pkg/server/lifecycle_orchestration.go` builds the registry from configuration,
hands it to the `AccountTokenManager` (`WithAccountProviders`) and to the
account write path (`AccountService.SetProviderRegistry`), then calls
`AccountTokenManager.RegisterOAuthRefreshers` once with a single generic
`OAuth2TokenRefresher`. That installs a refresher for **every** oauth-capable
provider, built-in or configured. Without it, an `oauth2` account fails its
first renewal with `AccountRefresherNotConfiguredError` at spawn time.

### Write-time validation

An `oauth2` account is rejected by `CreateAccount` / `RotateAccountCredential`
when it carries no `refresh_token` (`AccountRefreshTokenRequiredError`) or when
neither the account nor its provider descriptor resolves to a token endpoint
(`AccountProviderEndpointUnresolvedError`). A `none` (static) account carrying
a `refresh_token` is rejected with `AccountStaticTokenRefreshableError`, and an
unrecognized refresh policy with `AccountCredentialModeInvalidError`. All four
surface as HTTP 400 / gRPC `InvalidArgument` and keep the domain sentinel
reachable through `errors.Is`.

### What works today, and what is blocked on the operator

**`static_token` (`refresh_policy: none`) is complete end to end.** Import a
long-lived token or API key and it flows all the way into the agent process:
`CreateAccount` -> encrypted envelope -> `AccountResolver` -> `SpawnRequest.Env`
-> the subprocess. Nothing is missing and nothing needs configuring. See
`TestAccountPool_Anthropic_StaticToken_EndToEnd` and
`TestAccountPool_OpenAI_StaticToken_EndToEnd`
(`pkg/orchestration/account_spawn_e2e_test.go`), which assert on the env slice
the spawn's transport factory actually received.

- **Anthropic (`claude`)** needs nothing extra: the built-in descriptor supplies
  `CLAUDE_CODE_OAUTH_TOKEN`, so an account may carry just `access_token`.
- **OpenAI (`codex`)** must name its own variable. The built-in descriptor ships
  no `AccessTokenEnvKey` — the codex CLI reads OAuth material from its own
  `auth.json`, so there is no honest equivalent to ship — so an operator pooling
  API keys sets `access_token_env_key` (e.g. `OPENAI_API_KEY`) or `env` on the
  account. Alternatively, configure it once provider-wide under
  `orchestration.account_providers` (see [Configuring providers](#configuring-providers)).

**`oauth_refresh` (`refresh_policy: oauth2`) is complete code blocked on two
values QuicBaaS deliberately does not ship.** The renewal path — RFC 6749 §6
grant, single-flight per account, rotated-token persistence, expiry mirroring,
loud failure on refusal — is implemented and exercised end to end against a live
token endpoint in `TestAccountPool_OAuthRefresh_EndToEnd`. The only missing
inputs are `token_endpoint` and `client_id`, which the built-in Anthropic and
OpenAI descriptors leave EMPTY on purpose: neither vendor documents the OAuth
application its CLI authenticates with as a supported API, so a guessed value
would bake an unverified vendor endpoint into every deployment (a tripwire test,
`TestBuiltinAccountProviders_*`, fails if anyone hardcodes one).

Supply those two values — per account, or once per provider under
`orchestration.account_providers` — and the mode works with no code change.
Supply neither and the account is rejected at create time with
`AccountProviderEndpointUnresolvedError`, never at 3am when a live session's
token expires.

### Obtaining the initial refresh token

QuicBaaS does not run an interactive OAuth enrollment flow. The operator
bootstraps each account out of band — e.g. `CLAUDE_CONFIG_DIR` isolation plus
`claude login`, or `claude setup-token` — and imports the result with
`CreateAccount` / `RotateAccountCredential`:

- **Refreshable**: import the access/refresh pair as `refresh_policy: oauth2`
  and let `AccountTokenManager` renew it before every spawn.
- **Static**: import the long-lived `claude setup-token` value as
  `refresh_policy: none`; renewal is a manual operator action, and the
  operator-declared expiry is mirrored onto the account's metadata so a lapse
  is visible before spawns start failing.

### The SDLC module requires one (go-quicbaas#235)

The account pool is opt-in for a caller's own spawns and **mandatory for the
SDLC worker**, on the backends the server pools (`claude`, `claude-code`,
`opencode`). The worker spawn sets `AccountAuto` and names the item's project,
so selection is project-scoped first and tenant-wide second.

It is mandatory because #218 made it so, not as a separate policy. A worker is
spawned into the *SDLC module's* `CLAUDE_CONFIG_DIR` with the operator's `CLAUDE_*`
variables scrubbed out of its environment — which is a directory with no
credential in it. Measured on the go-truststrap burn-in, 2026-08-30: the CLI
exited `api_error` before its first model turn, three attempts inside sixty
seconds, and the item reached the terminal `exhausted` state without a single
model turn ever running.

**What an operator must do:** register at least one pooled account for the
backend's provider, scoped to the tenant (or to the project). A dedicated
login for the SDLC module — never a copy of an operator's interactive OAuth
credential, whose refresh-token rotation would invalidate one side or the
other.

**What happens if none is registered:** the dispatch **starts no process**.
The account is resolved inside `provisionAndSpawn`, ahead of the exec, so the
failure is a configuration fault rather than a dead worker: the item parks in
`blocked` with `no provider account for <provider> in tenant <t>`, the attempt
is not charged against its budget (`WorkItem.EnvironmentFaults`), and the
reason is posted to the forge issue. Fix the pool and re-apply the trigger
label.

On a **remote daemon** the request crosses the wire and the daemon that runs
the agent resolves it against its own pool — the credential itself never
travels. That daemon needs the account registered in *its* tenant scope, and a
secrets manager wired, or its spawns fail closed the same way.

**A pool that is merely busy is not a misconfiguration.** If the tenant HAS
accounts for the provider and at least one is on a rate-limit hold **that names
its expiry**, the item is **not** parked: it goes back in the queue with its
attempt uncharged and the next sweep tries again. Parking there would put a
person in front of work that was about to become dispatchable on its own.

The expiry is what makes that safe. A hold with no stated expiry is held
indefinitely, so it is **not** treated as self-clearing — a pool of nothing but
those parks like any other pool that needs a human, because requeueing against
a condition with no end is a livelock. A cooldown always names when the pool
returns.

**The item waits for the moment the pool named.** `WorkItem.RetryNotBefore` is
set from the hold's expiry and the dispatcher passes over any Ready item whose
moment has not arrived — reported as `deferred`, taking no slot, writing
nothing. Without it the wait would be the sweep interval instead: a thirty-second
sweep against a ten-minute hold dispatches three times in the first minute, and
with a three-strike bound that parks the item nine minutes before the credential
comes back.

Every requeue still counts toward the three-strike bound, cooldowns included —
with the wait in place the bound counts *holds waited out*, hours of real time,
rather than sweeps. Exempting cooldowns would let a pool that keeps renewing its
hold requeue an item forever with nobody told. The deferral is cleared on every
dispatch and clamped to 24 hours on write: the moment comes from a provider
record rather than from the SDLC module, and a clock wrong in the wrong direction
must not be able to hide an item indefinitely.

**Most environment faults retry rather than park.** Only the resolver's own
faults — no pool, no resolver, a selected account whose secret is missing, a
provider with no refresher — park on their first occurrence, because each is a
fact about the *configuration* that the SDLC module read directly and no retry
changes. Everything else requeues, **bounded at three consecutive failures**
with no model turn in between; the third parks with the reason "3 consecutive
environmental failures — <class>" and *is* charged.

The bound is what makes retrying the safe default. `api_error` with zero usage
is a missing credential **or** a 5xx or a DNS blip, and nothing in the CLI's
result object separates them — so parking on the first one puts a person in
front of work that may run on the next sweep, while retrying forever hides an
item that costs almost nothing per iteration and never progresses. The count is
consecutive and resets on any conclusion that is not another requeued fault, so
an item does not creep toward the wall across weeks of successful work.

The same accounting covers a CLI that exits before its first turn — but only
for **environmental** reasons. The evidence is structural, from the CLI's own
result object: `is_error` with a *reported-and-zero* token usage means no
request reached the model, and the `terminal_reason` says whether a different
environment would have changed that.

| refunded (environmental) | charged (work-derived) |
|---|---|
| `api_error`, `turn_setup_failed` | `blocking_limit`, `prompt_too_long`, `rapid_refill_breaker` (the session's context is full — the runner rolls the session, see [sdlc.md](sdlc.md#a-full-context-rolls-the-session-not-the-attempt)), `image_error`, `model_error`, `max_turns`, `budget_exhausted`, `stop_hook_prevented`, `hook_stopped`, and every other declared reason |

One cause of `api_error` is now removed before it can be reached rather than
refunded after it: a session whose pooled account's credential was rotated
under it is replaced at its next prompt instead of being prompted with a
credential the provider has stopped honouring — see
[Credential generations](#credential-generations-go-quicbaas997). The refund
still exists for everything else `api_error` covers.

`prompt_too_long` also consumes no input tokens, and it is a deterministic
consequence of the prompt built from the work item — refunding it would let a
human bypass the attempt budget by re-applying the trigger label. A reason this
build does not recognise is **charged**, with its class surfaced, so a newer CLI
cannot quietly widen what stops being charged. A turn that ran and then failed
reports non-zero input tokens and is charged and retried exactly as before.

## Multi-protocol surface

The account CRUD surface and the `AccountID` / `AccountAuto` /
`AccountStrategy` spawn fields are reachable on every transport (the Phase 8
rule). There is exactly one canonical SDK entry point: `SDK().Account()`
(`sdk/go/client/account`).

### Verbs

| SDK method         | REST                                          | gRPC / Unix `quicbaas.account.v1.AccountService` | QUIC sub-op | MCP tool                            |
|--------------------|-----------------------------------------------|--------------------------------------------------|-------------|-------------------------------------|
| `Create`           | `POST   /api/v1/agent-accounts`               | `CreateAccount`                                  | `0x30`      | `quicbaas_agent_account_create`      |
| `Get`              | `GET    /api/v1/agent-accounts/{id}`          | `GetAccount`                                     | `0x31`      | `quicbaas_agent_account_get`         |
| `List`             | `GET    /api/v1/agent-accounts`               | `ListAccounts`                                   | `0x32`      | `quicbaas_agent_account_list`        |
| `Update`           | `PUT    /api/v1/agent-accounts/{id}`          | `UpdateAccount`                                  | `0x33`      | `quicbaas_agent_account_update`      |
| `Delete`           | `DELETE /api/v1/agent-accounts/{id}`          | `DeleteAccount`                                  | `0x34`      | `quicbaas_agent_account_delete`      |
| `RotateCredential` | `POST   /api/v1/agent-accounts/{id}/rotate`   | `RotateAccountCredential`                        | `0x35`      | `quicbaas_agent_account_rotate`      |
| `Revoke`           | `POST   /api/v1/agent-accounts/{id}/revoke`   | `RevokeAccount`                                  | `0x36`      | `quicbaas_agent_account_revoke`      |
| `SetDefault`       | `POST   /api/v1/agent-accounts/{id}/default`  | `SetDefaultAccount`                              | `0x37`      | `quicbaas_agent_account_set_default` |
| `Usage`            | `GET    /api/v1/agent-accounts/{id}/usage`    | `GetAccountUsage`                                | `0x38`      | `quicbaas_agent_account_usage`       |

The embedded (in-process) transport routes the same nine methods through
`sdk/go/transport/embedded/handlers_account.go`. Unix is gRPC on a domain
socket and is served by the same registered `AccountService`.

QUIC has no free top-level opcode (`0x60` is the only unassigned byte and is
reserved), so the account sub-ops ride the `OpOrchestrator` (`0x8F`) dispatch
byte in a disjoint `0x30-0x38` range — the same "shared opcode, disjoint
sub-op range" pattern scheduler/messaging/extended-vector already use on
`OpScheduler`. Authorization is sub-op aware so account operations do NOT
inherit the weaker surface-level `orchestrator:status` permission.

### Authorization

qrdb registers no `account:*` permission family, so accounts authorize with
the qrdb `credentials:*` family — the same vocabulary the `/api/v1/secrets`
and `/v1/credentials` surfaces use, and semantically correct because an
account custodies credential material. The mapping is identical on all
transports:

| Verb                       | Permission              |
|----------------------------|-------------------------|
| create / update / default  | `credentials:store`     |
| get / usage                | `credentials:retrieve`  |
| list                       | `credentials:list`      |
| delete / revoke            | `credentials:delete`    |
| rotate                     | `credentials:rotate`    |

Reads take `credentials:retrieve` (the strongest read verb) even though no
account response carries secret material: over-restricting a read is the safe
direction and keeps a single "who may see this account at all" grant. If qrdb
later registers an `account:*` family, these mappings should move to it —
they live in `pkg/api/rest/authz_pep_mapping.go`,
`pkg/api/grpc/authz_mapping.go`, `pkg/api/quic/handler_authz_qrdb.go`, and
`pkg/api/mcp/authz_qrdb.go`.

### Operator-initiated session rotation

Automatic rotation answers a classified provider limit. The operator-initiated
door to the same verb — "this seat is bad, move my session now" — is
`SDK().Orchestrator().RotateSessionAccount`, reachable on all six transports:

| Transport      | Wire shape |
|----------------|------------|
| embedded       | `quicbaas.Orchestrator/RotateSessionAccount` |
| REST           | `POST /api/v1/orchestrator/sessions/{sessionID}/rotate-account` |
| gRPC / Unix    | `quicbaas.orchestrator.v1.OrchestratorService/RotateSessionAccount` |
| QUIC           | `OpOrchestrator` (`0x8F`) sub-op `0x20` |
| MCP            | `quicbaas_orchestrator_rotate_account` |

Request: `session_id` (path segment on REST, which is authoritative over any
body field), optional `reason`, `hold_outgoing`, and `available_at`
(RFC 3339 on the JSON transports, `available_at_unix` seconds on gRPC). An
empty body is a plain rotation with no hold. Response: `session_id`,
`previous_account_id`, `account_id`, `strategy`, `mode`
(`in_place` | `resume`).

Errors map consistently: a deliberate pin is `409` / `FailedPrecondition`
(`SessionNotRotatableError`), an unknown session is `404` / `NotFound`, and a
deployment whose agent service predates the pooled account subsystem answers
`501` / `Unimplemented` (`SessionRotationUnsupportedError`) rather than
panicking on a type assertion. Every server transport narrows the capability
through `orchestration.RotationServiceFrom`, so there is one typed answer
rather than five ad-hoc ones.

The QUIC sub-op sits at `0x20`, **outside** the contiguous `0x01-0x0D`
orchestrator range, on purpose: that range authorizes at the surface level as
`orchestrator:status`, and rotation is a write that must resolve its own
permission. `0x20` is disjoint from accounts (`0x30-0x38`), projects
(`0x40-0x47`), SSH (`0x50-0x5D`), certificate extensions (`0x60-0x62`) and
attestation (`0x70-0x72`); the two protocol tables carry cross-reference
comments and `TestQUICOrchestratorRotateSubOpParity` asserts the literal byte
on both sides.

**Authorization: `orchestrator:spawn`, on every transport.** Rotation closes
the session's agent process and reconstitutes it against a newly selected
pooled credential — that is a spawn's authority, and it is exactly the
permission the session was created with. `orchestrator:close` would let a
caller who may only terminate sessions instead move one onto a different
account's credential, and the surface-level `orchestrator:status` would gate a
write behind a read. qrdb registers no `orchestrator:rotate` permission, so the
nearest registered write verb is used, following the same rule the
`quicbaas_fleet_disable`/`quicbaas_fleet_enable` routes already follow.

### Credential never leaves the process

`AccountCredential` is a **request-only** shape on every transport. It is a
field of the create and rotate requests and of no response type — the proto
`AccountCredential` message appears in exactly two request messages, the REST
`AccountCredentialRESTRequest` in exactly two request bodies, and the SDK
`account.Credential` in exactly two request structs. Structural guard tests
(reflection over every response type) plus behavioral guard tests (drive the
verb with a populated credential, assert no marker appears in the serialized
response) run per transport:

- `pkg/api/rest/handlers_account_test.go`
- `pkg/api/grpc/server_account_test.go`
- `pkg/api/quic/handler_account_test.go`
- `pkg/api/mcp/account_tools_test.go`
- `sdk/go/transport/embedded/handlers_account_test.go`
- `sdk/go/client/account/client_test.go`
- `cmd/quicbaas/account_test.go`

## CLI

`quicbaas account` covers the full verb set and routes through the SDK
client, so it behaves identically on every transport:

```
quicbaas secrets put agents/claude/claude1/oauth_token --from-stdin   # as a tenant principal
quicbaas account create --provider claude --display-name claude1 \
  --credential-secret-ref agents/claude/claude1/oauth_token
quicbaas account update acct_abc123 \
  --credential-secret-ref agents/claude/claude1/oauth_token          # switch an inline account
quicbaas account create --provider claude --display-name alice-work \
  --refresh-policy oauth2 --credential-file -      # inline; '-' reads stdin
quicbaas account list --provider claude --output json
quicbaas account get acct_abc123
quicbaas account update acct_abc123 --display-name alice-personal
quicbaas account update acct_abc123 --max-concurrent-sessions 2
quicbaas account set-default acct_abc123
quicbaas account rotate acct_abc123 --credential-file ./new-token.json
quicbaas account usage acct_abc123
quicbaas account revoke acct_abc123 --reason "token leaked"
quicbaas account delete acct_abc123
```

`--credential-secret-ref` (and `--credential-secret-file`) name a tenant secret
instead of supplying a document; see
[Where the credential comes from](#where-the-credential-comes-from-inline-or-a-tenant-secret)
and its runbook. `--credential-secret-ref` and `--credential-file` together are
refused before anything is read.

Inline credentials are read from a file or stdin, never from a flag value: a
command-line secret lands in shell history and the process table. The
credential document mirrors the credential envelope:

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

`create` takes `--default` to import an account AS the default, displacing the
current one. It is not needed for the first account of a provider: that one
becomes the default with or without the flag.

A complete Anthropic bring-up, from nothing to a spawn that needs no `--account`:

```
claude setup-token > /tmp/tok
jq -n --arg t "$(cat /tmp/tok)" '{access_token: $t}' \
  | quicbaas account create --provider claude \
      --display-name team-max-1 --refresh-policy none --credential-file -
# -> Default: true

quicbaas orchestrator spawn --backend claude --account-auto
```

And OpenAI, which must name its own environment variable:

```
jq -n --arg k "$OPENAI_API_KEY" \
  '{access_token: $k, access_token_env_key: "OPENAI_API_KEY"}' \
  | quicbaas account create --provider codex \
      --display-name openai-prod --refresh-policy none --credential-file -
```

Multi-protocol coverage is exercised by
`test/integration/cli/agent_account_test.go` via the `RunForAllTransports`
convention.

## Spawn integration

`SpawnSessionRequest` carries `account_id` / `account_auto` /
`account_strategy` / `account_allow` (`api/proto/orchestrator.proto` fields
16-19). All five network transports plus embedded map them onto
`orchestration.SpawnSessionRequest`:

- REST and QUIC decode `orchestration.SpawnSessionRequest` directly, so the
  fields flow through the JSON tags.
- gRPC maps them in `pkg/api/grpc/server_orchestrator.go`.
- MCP maps them in `pkg/api/mcp/orchestrator_tools.go` (tool arguments
  `account_id`, `account_auto`, `account_strategy`).
- The SDK maps them in `sdk/go/transport/grpc/orchestrator_handler.go` and
  `sdk/go/transport/embedded/handlers_orchestrator.go`; the REST, QUIC, and
  MCP SDK paths serialize the SDK request struct directly.

`AccountAuto` is what makes the dynamic (least-usage) strategy requestable at
all, and `AccountAllow` is the second of the two things that authorize
rotation, so both are asserted per transport rather than assumed. A spawn that
carries `account_id` alone is a deliberate pin and is never rotated; adding
`account_allow` turns the same spawn into "this seat to start with, these
seats to rotate through".

## Remaining wiring (not yet implemented)

- **No interactive OAuth enrollment, and no shipped vendor OAuth endpoints.**
  The `oauth_refresh` renewal path itself is complete and tested; what is absent
  is an authorization-code flow run on the operator's behalf and any built-in
  `token_endpoint` / `client_id`. See
  [What works today](#what-works-today-and-what-is-blocked-on-the-operator).
- **`GetAccountUsage` answers `AccountUsageTrackerNotConfiguredError` when
  orchestration is not brought up.** The service container constructs
  `AccountService` without a `CostTracker` (`SetUsageTracker` is a settable
  dependency); `pkg/server/lifecycle_orchestration.go` binds the ledger during
  orchestration bring-up. A deployment with orchestration disabled therefore
  answers HTTP 503 / gRPC `Unavailable` for the usage verb — by design, not a
  transport gap.
- **`CodexExecBackend` has no account resolver.** `resolveAccount` /
  `SetAccountResolver` are implemented on `ACPBackend` only.
- **Per-turn usage recording covers the stream-json backends only.** See
  "Cost attribution" above for which backends report token counts.
- **Limit detection falls back to a heuristic over CLI output** when the CLI
  emits none of its structured limit signals. See
  [Detecting a limit](#detecting-a-limit--structured-signals-first). A
  credential proxy would make it exact; `AccountLimitClassifier` is an
  interface so such a deployment can substitute one
  (`SetAccountLimitClassifier`) without touching the rotation path.

## Web UI

`cmd/quicbaas/frontend/src/views/orchestrator/AccountsView.svelte` (route
`#/accounts`, sidebar "Accounts" under ORCHESTRATION) provides full lifecycle
management: create (with a refresh-policy-aware credential form — static
token vs. OAuth refresh fields — that never sends `refresh_token` for
`static_token`, matching the server-side rejection rule), list with
status/default badges and `last_error` surfaced inline when `status ==
"error"`, set-default, rotate-credential (write-only — the current credential
is never displayed or requested from the server), revoke, delete, and a usage
report dialog.

`cmd/quicbaas/frontend/src/components/orchestrator/SpawnDialog.svelte` adds an
Account control alongside Backend/Project: "Unset" (sends neither field),
"Automatic (strategy)" (`account_auto: true` plus an optional
`account_strategy`), or a specific account (`account_id`). The specific-account
list is narrowed to the selected backend's provider only when that mapping is
unambiguous (backend `claude`/`codex`/`opencode` → the identically-named
provider); any other backend shows every account, since the server — not this
dropdown — is the authority on which accounts a backend may use (see bug #59:
a prior hardcoded backend allowlist in this same dialog).

`SpawnDialog.svelte` does **not** yet expose an `account_allow` control — the
TypeScript `SpawnSessionRequest` type carries the field, but the dialog offers
only Unset / Automatic / a specific account. Adding an allow-list picker (and a
"rotate account" action on the session detail view) is a follow-up.

The TypeScript SDK gained `sdk/typescript/src/accounts.ts`
(`AccountsClient`, mirroring the Go SDK's 9 verbs) and
`client.accounts`/`SpawnSessionRequest.account_id|account_auto|account_strategy`
on `client.orchestrator.spawnSession`.

Note: `Account.CredentialExpiresAt` and `Account.LastValidatedAt` (domain
model, `pkg/orchestration/account_types.go`) are not exposed by any transport
today — REST's `AccountRESTResponse`, the Go SDK's `AccountInfo`, and the
proto surface all omit them. The UI therefore cannot show a credential-expiry
countdown; it surfaces only the fields actually on the wire
(`last_used_at`, `last_refreshed_at`, `last_error`, `status`).

## Related

- [cost-enforcement.md](cost-enforcement.md) — the consumption ledger, the
  spend cap and the circuit breaker that guard every release above.
- [architecture.md](architecture.md) — where the pool sits in the whole
  Software Factory picture.
- [api.md](api.md#quicbaasaccount--sdk-package-sdkgoclientaccount) — the
  account surface on every transport.
- [projects.md](projects.md) — the project an account may be scoped to.
- go-quicbaas#734 — the daemon half of the design, and the burn-in failure that
  motivated it.

