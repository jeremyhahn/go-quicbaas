# Agent Fleet — Placement, Drift and Actuation (design note)

> **Status: DESIGN** · Last verified: 2026-10-15
> The plan for salvaging the archived wp13 placement and wp14 actuation work
> (go-quicbaas#761) onto today's fleet code. Nothing
> here is built yet. Where a question needs the owner or the CA it is listed
> under [Open questions](#open-questions), not decided.

Placement says **where a class of work may run**, reports where it actually
runs, and moves it back when the two disagree. Three layers, each usable without
the next: a declarative **spec**, a read-only **drift report**, and an
**actuator** that moves fungible work on its own and everything else only on a
person's word.

## Desired-state model

A `PlacementSpec` places a workload class onto **hosts**:

| Field | Meaning |
|---|---|
| `name` | Identifies the spec within its tenant |
| `workload_class` | What it places (see [classes](#actuation)) |
| `hosts` | The admissible hosts; empty means any host that passes the capability check |
| `required_capabilities` | All must be present on a host. A hard match, never a score |
| `actuate` | False: report drift only. True: the actuator may move work |
| `generation` | Orders edits. A put that read generation N writes N+1 by compare-and-swap, so two racing edits cannot lose one silently |

The archive called the target a **node** (`PlacementNode`). This note says
**host**, because "fleet node" already names a registry record and bare "nodes"
is the Raft cluster's word ([naming](architecture.md#naming)).

**How it differs from the fleet registry.** The registry
(`FleetRegistry`, `FleetNode`) is *observed* state, one record per enrolled
daemon: who registered, which backend it serves, its capabilities, health and
`MaxConcurrentJobs`. A spec is *desired* state written by an operator. The
registry answers "what exists", the spec answers "where should this work be".
A registry record is not a host: several records can name one `Host`, so
placement reads the registry through an inventory adapter that groups records
by `Host` before it compares anything (archive d40b48d1).

**Host affinity.** Today `WorkItem.HostAffinity` and `WithHostAffinity` /
`WithHostAffinityStrict` (`pkg/orchestration/host_affinity.go`,
`orchestrator.go`) are a *per-request* hint, filtered in
`ControlLoop.matchFleetNodeForTask`. `SessionRecord.HostAffinity` records where
a remote session landed. A spec is the *standing* rule that per-request hints do
not express. Actuation carries a move out as a re-spawn with strict affinity
to the destination host, so it adds no second routing path.

**Capacity admission.** `FleetAdmission.Admit` (#760) decides whether one host
has room *now*. A spec says which hosts are *permitted*. The two do not
substitute for each other. The drift report lists all the permitted
destinations, and the actuator asks admission about the destination
immediately before it orders a move. A refusal becomes a recorded outcome, never
an override.

## Drift report

`ReportDrift(tenant, spec)` compares three inputs:

1. the spec (its hosts and required capabilities);
2. the inventory: registry records grouped by host, with the **union** of their
   capabilities (union answers "can anything on this host do X", which is all a
   host-level constraint can ask; picking one agent stays with
   `matchFleetNodeForTask`);
3. the live work of the spec's class: on main, `SessionRecord` (tenant, session
   id, `HostAffinity`), which replaces the archive's `WorkLease`.

Findings come from one closed set of kinds: `node_not_admitted`,
`capability_missing`, `node_unregistered`, `node_unrecorded`,
`spec_node_absent`, `no_admissible_node`, `workload_class_unclassified`. Each
finding carries a remedy (`relocate` with its admissible destinations,
`no_candidate`, `operator_decision`) and the spec's disposition.

**Why exhaustive.** The report is what an operator trusts *instead of*
looking. A silent omission reads as "no drift". So exhaustiveness is checked
at construction, not by review. Every `PlacementSpec` field is classified as
constrained (it has a constraint function) or as deliberately not a
constraint. An unclassified, unknown or doubly-classified field fails
construction (`PlacementFieldUnclassifiedError`, `…UnknownError`,
`…AmbiguousError`). Adding a field to the spec without deciding what drift it
causes then fails the build's tests, rather than producing a report that
never mentions it.

**Refusal rules.** The report refuses to answer rather than answer wrongly:

- **A duplicate host description is refused** (`PlacementInventoryAmbiguousError`).
  If two inventory entries describe one host, keeping the last picks a
  capability set at random, union over-credits and intersection
  under-credits. Grouping records by `Host` happens in the adapter, before the
  reporter sees them, so an ordinary multi-daemon host never trips this. Only
  a broken adapter does.
- **Unnameable work is refused** (`PlacementLeaseAmbiguousError`, to be renamed
  for sessions). Two pieces of work the report cannot tell apart would produce
  one finding that an actuator could apply to the wrong one. In the archive
  this was a lease with no tenant. On main a `SessionRecord` carries its
  tenant, so the rule becomes: work with no tenant, no id or no recorded host
  is refused with the work named.
- A spec whose class is not in the classification table reports
  `workload_class_unclassified` and is never actuated.
- An edit written against a stale generation is refused
  (`PlacementSpecStaleError`), not merged.

## Actuation

The actuator consumes a drift report. It does not re-derive one. Each spec
resolves to one disposition: `forbidden` (`actuate` false), `automatic`,
`needs_consent` or `unclassified` (refuses). The disposition comes from a
single table of classes:

| Class | Disposition | Why |
|---|---|---|
| fungible: CI and review runs, sandboxed job sessions | `automatic` | Fully described by their inputs. Losing one costs a re-run |
| custody: persistent agent sessions | `needs_consent` | Holds a forge token, a signing key, a session and in-flight work. Moving it moves custody |

**Fungible moves are automatic.** The actuator checks the destination with
`FleetAdmission.Admit` and orders the move through the `Relocator` port.

**Every other move needs per-move consent.** The actuator raises a
`PlacementConsentRequest` and stops. A request is identified by a hash of the
move: tenant, spec name, **spec generation**, class, work id and source host,
each length-prefixed so distinct moves cannot collide. So two control-plane
replicas raise the same request without coordinating, and editing the spec
voids outstanding grants. The request is created once with a guarded create
(`PutIfAbsent`), and the answer is swapped onto it. A later sweep that loses
the create reads the existing answer instead of overwriting it. States are
`pending`, `granted`, `refused` and `incoherent`. An unreadable ledger fails the
actuation. It never defaults to permission.

**The mover/decider decision is recorded.** The actuator, which decides,
writes an `ActuationRecord` for every move it orders, fungible and consented
alike, failures included. The `Relocator`, which only moves things, is not
trusted to write that record. The record id includes the outcome, so a move
that failed and then succeeded leaves two records, while repeats of one result
collapse. Two artifacts, two facts: the consent request says a person agreed,
and the actuation record says the system acted.

## Mapping table

Archive tips: `db4af910` (wp13) and `9e3b13d2` (wp14). Only the placement
commits are mapped. The ancestors the two branches share are out of scope for #761.

| Commit | Subject | Verdict | Target and reason |
|---|---|---|---|
| e43129fe | declarative placement surface, lost-edit refusal | **adapt** | `PlacementSpec` and `PlacementDeclarer` become a tenant-scoped spec store in the orchestration service over the injected KV (`CompareAndSwap`), keyed per tenant like `orch/agent/{tenantID}/…`. `CoordinationStore` and `WorkloadClassProfile` do not exist on main |
| 0e10a6f1 | drift report, exhaustive by construction | **adapt** | `DriftReporter` and the field-coverage check are kept. Inputs move from `WorkLease`/`CoordinationStore` to `SessionRecord` and the fleet inventory adapter |
| da700901 | duplicate node description refused | **take as-is** | The rule and `PlacementInventoryAmbiguousError` carry over unchanged, keyed on host |
| db4af910 | unnameable work refused | **adapt** | The refusal stays. Its subject changes from a tenantless `WorkLease` to a `SessionRecord` missing a tenant, id or host |
| 089b6684 | wp13 copied verbatim into wp14 | **drop** | Duplicates e43129fe–db4af910 byte for byte (its own digests say so) |
| d40b48d1 | registry lists agents, placement places on hosts | **adapt** | `FleetInventory` over `FleetRegistry.ListFleetNodes` / `FleetNode` (was `AgentRegistration` / `ListAgents`), grouping by `FleetNode.Host` and taking the union of capabilities |
| 82a25280 | per-move consent | **adapt** | `ConsentLedger` and `ConsentMove.RequestID` are kept. They use the injected KV's `PutIfAbsent`/`CompareAndSwap` instead of `CoordinationTransactor`. `ConsentAnswer.By` comes from the authenticated subject, not the request body |
| 407f2d83 | fungible-move actuation | **adapt** | `Actuator` is kept, plus a destination check through `FleetAdmission.Admit`. The `Relocator` is implemented by the orchestration service as a re-spawn with strict host affinity |
| 0bdacdb9 | mover/decider split, actuation record | **take as-is** (rule) / **adapt** (store) | The decider writes `ActuationRecord` itself and the outcome is in the id. Ledger storage moves to the injected KV |
| d83c134a | lease names its tenant on the value (not named in the brief) | **drop** | Superseded: `SessionRecord` already carries `TenantID` |
| 3f7eb214 | RequiredCapabilities constrains every entry | **adapt** (re-apply) | **Landed (P0).** `matchFleetNodeForTask` now hard-matches every entry through `filterFleetNodesByCapabilities` before backend, host-affinity and default selection, and refuses with `NoMatchingFleetNodeError` (constraint `capability`); `persistent_sessions` stays backend-scoped |
| 9e3b13d2 | doc comment: the gate the union rests on | **adapt** | The comment moves with the inventory adapter and names `matchFleetNodeForTask` once that fix (above) is real |

## Interfaces

Service layer (`pkg/service`, over orchestration). Transports call only this:

```go
type PlacementService interface {
    DeclarePlacement(ctx context.Context, tenantID string, spec *PlacementSpec) (*PlacementSpec, error)
    GetPlacement(ctx context.Context, tenantID, name string) (*PlacementSpec, error)
    ListPlacements(ctx context.Context, tenantID string) ([]*PlacementSpec, error)
    WithdrawPlacement(ctx context.Context, tenantID, name string) error
    ReportDrift(ctx context.Context, tenantID, name string) (*DriftReport, error)
    ListConsentRequests(ctx context.Context, tenantID string, state ConsentState) ([]*PlacementConsentRequest, error)
    AnswerConsent(ctx context.Context, tenantID, requestID string, answer ConsentAnswer) (*PlacementConsentRequest, error)
    ListActuations(ctx context.Context, tenantID string, filter *ActuationFilter) ([]*ActuationRecord, error)
}
```

Ports the core depends on: `PlacementInventory` (implemented over
`FleetRegistry`), `Relocator` (orchestration re-spawn), and a destination
admission check (`FleetAdmission`).

**Authorization is go-qrdb's only.** Each verb maps to a permission
contributed through `pkg/permission` (`qrdbsdk.RegisterPermissions` and its
role and duty registrations). The PEP decides. The service adds no role
checks of its own. Proposed vocabulary: `placement:read` (get, list, drift,
actuations), `placement:write` (declare, withdraw), `placement:consent`
(answer). The tenant is the subject's OU. `tenant_id` must match it unless
the system-tenant rule applies, as for every fleet verb. `ConsentAnswer.By`
is the subject. A request body cannot name the decider.

**SDK.** New methods on the orchestrator client (`sdk/go/client/orchestrator`,
beside `ListFleetNodes`): `DeclarePlacement`, `GetPlacement`, `ListPlacements`,
`WithdrawPlacement`, `GetPlacementDrift`, `ListPlacementConsents`,
`AnswerPlacementConsent`, `ListPlacementActuations`.

**Transports.** Every verb reaches REST, gRPC, QUIC, MCP and Unix, plus the
embedded transport, and the CLI (`quicbaas fleet placement …`). A transport
returning `not yet supported by SDK` blocks the slice. The actuator sweep
itself has no verb: it is a server-side reconciler.

## Slices

Each slice is one lane (≤ ~150 turns), lands green and leaves main shippable.

| # | Slice | Tests and verification |
|---|---|---|
| P0 | **Landed.** Re-apply 3f7eb214: every `RequiredCapabilities` entry is a hard match in `matchFleetNodeForTask` (`persistent_sessions` stays backend-scoped) | Table test per capability. A regression test that a `docker` requirement no longer dispatches to an agent without it. `make test-orchestration` |
| P1 | `PlacementSpec`, validation, tenant-scoped store with CAS generation, `FleetInventory` grouping by host, the refusal error types | Lost-edit test (two racing declares, one `PlacementSpecStaleError`), using `pkg/testutil/qrdbnode` under the consensus-arbitration carve-out. A control test feeding the same records through a one-for-one inventory (refused) and through the adapter (reported). `make test-orchestration` |
| P2 | Read-only `DriftReporter` over specs, the inventory and `SessionRecord`, the field-coverage check, the unnameable-work refusal | One test per finding kind. Mutation checks that an unclassified field fails construction. Duplicate-host and tenantless-session refusals. `make test-orchestration` |
| P3a | `PlacementService` (declare/get/list/withdraw/drift), `placement:read`/`write` contributions, REST + gRPC + embedded + SDK client | Service tests with error paths. Authz table tests (denied without the permission, cross-tenant refused). `make test-service`, `test-api-rest`, `test-api-grpc` and the SDK client's package target |
| P3b | The same verbs on QUIC, MCP, Unix and the CLI. Docs in `api.md` / `cli.md` | Per-transport handler tests. Parity test across transports. A new `integration-test-fleet-placement` E2E over every transport (CI runs it, since it needs Docker) |
| P4 | `ConsentLedger` (raise/answer/get), `placement:consent`, list/answer verbs on all transports | Guarded-create race (one request, answer survives a re-raise). Generation change voids a grant. Unreadable ledger refuses. `By` taken from the subject |
| P5 | `Actuator`, `ActuationLedger`, `Relocator` over strict-affinity re-spawn, admission check, reconciler wiring. Fungible moves first; consented moves behind a granted request | Disposition partition tests (unknown refuses). Fail-then-succeed leaves two records. An admission refusal is recorded, not moved. A move without a grant never calls the `Relocator` |

## Open questions

For the owner or the CA. None is decided here.

1. **Home package and SPEC row.** Does placement live in `pkg/orchestration`
   (beside the registry and admission, as this note assumes) or under the
   scheduler, where SPEC's `declarative-placement` row and ROADMAP #28 point?
   Which row does #761 trace to?
2. **Workload classes on main.** The archive classified `ci`, `review` and
   `agent` on a `WorkLease`. Main has no workload class on `SessionRecord`.
   What are the classes, and where is a session's class recorded?
3. **Relocating a custody session.** Is "close, then re-spawn with strict
   affinity" an acceptable move for a persistent agent session, or does a
   consented move need workspace and session hand-off (like the account
   rotation's resume)? This may need its own design.
4. **Tenant scope of hosts.** Registry records are per tenant. Is a spec
   per tenant, with hosts shared across tenants, or system-scoped? This
   decides whether the inventory sweeps one tenant or all of them.
5. **Permissions.** Are `placement:read`/`write`/`consent` the right names?
   Which built-in roles hold them? Which duty does `placement:consent` belong
   to (governance, or undeclared)?
6. **Who hosts the actuator sweep**, at what interval, and is it
   leader-elected (scheduler leader) or run by every control-plane replica
   (which the derived request ids would tolerate)?
7. **Issue note correction.** The issue says 3f7eb214 is already reflected in
   `control_loop.go`. It is not (see the mapping). Confirm that P0 goes ahead
   as a prerequisite. **Answered:** P0 went ahead first and has landed.

## See also

[architecture.md](architecture.md) · [api.md](api.md) ·
[orchestration](../orchestration/architecture.md) ·
[scheduler](../scheduler/architecture.md) ·
[authorization](../authorization/api.md#contributing-permissions-to-the-qrdb-registry)
