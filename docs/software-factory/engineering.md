# Software Factory — Engineering

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Implementation contract for the Software Factory modules: exported surface,
> invariants, errors, configuration, persistence, tests and observability —
> the SDLC module, projects, pooled accounts and prompt sets. Feature folder: `docs/software-factory/`.
> Conflicts with this document are defects.

The orchestration engine's contract (backends, sessions, the attach event log,
jobs and amendments, fleet, workflows, inference) is in
[../orchestration/engineering.md](../orchestration/engineering.md). This
document covers the modules built on it.

## The SDLC module

The git-delivery module's exported surface, invariants, typed errors
and configuration are in
[../sdlc/engineering.md](../sdlc/engineering.md).

## Projects and pooled accounts

The Project record, its quota and permission matrix, and its service
invariants are [projects/engineering.md](../projects/engineering.md). The pooled-provider-account model,
its two-record split, selection strategies, limit holds, rotation and cost
attribution are [../accounts/architecture.md](../accounts/architecture.md).

## Prompt sets

Every instruction the Software Factory sends an agent is markdown embedded in
the binary, not a Go constant. `pkg/orchestration/prompts` is the generic
package — `SetKind`, `PromptSpec`, `SetSpec`, a registry swapped as a whole
under `atomic.Pointer`, a validator, a `${name}` renderer and a content hash.
Two sets register from `init()`: `workflow`, owned by `pkg/orchestration` from
`promptdefaults/workflow/*.md`, and `sdlc`, owned by `pkg/sdlc` from
`promptdefaults/sdlc/*.md`. **The embedded files are the system tenant's
defaults.**

The invariants:

- **A body carries `${name}` holes and nothing else.** Every hole is declared;
  a required hole must stay; an optional one with no value renders as nothing.
  No conditionals, no loops — `text/template` in human-edited markdown cannot
  be validated. Loops, plurals, fenced logs, redaction and JSON assembly stay
  in Go as thin assemblers.
- **One validator, two call sites.** `Validate` refuses a blank body, an
  undeclared `${x}`, a missing required variable and a missing marker, each
  with a typed error that names the thing. It runs at registration — where a
  failure panics in `init()` like an unregisterable job runner — at
  `Store.Save`, and when a stored body is resolved for a job.
- **Markers come from the parser's constants.** The charter's markers are
  built from `TurnStopMarker` and every key of `stopCodeOutcomes`, so a stop
  code the runner dispatches on is a code the charter must teach. This is the
  enforced form of the review control the old constant asserted.
- **`Set.MustRender` panics**, and only assemblers with no error to return use
  it: the content is embedded and was validated against its own spec, so a
  failure is a defect the golden tests catch. Tenant-supplied content does not
  reach it unvalidated: the store refuses it at save and the resolver at
  launch.
- **Resolved once per job.** `launchJob` resolves a `PromptSetProvider`
  runner's set project → tenant → system → embedded, skipping records with
  `follow_parent=true`; the job renders from that `ResolvedSet` for its whole
  life and a resume resolves afresh. A failure refuses the launch with
  `prompts.ResolveError`. `JobStatus.Prompts` records a `VersionRef` per
  prompt and the `prompts:resolved` job audit event names every non-embedded
  answer. The SDLC issue source resolves once per poll and refuses the poll
  on failure.

**Persistence.** One `prompts.Record` per `(tenant, project | "", set, name)`
in the owning tenant's store via `qrdbsdk.TenantStoreResolver`; the system set
is the tenant-wide record under `qrdbsdk.SystemTenantID`. History is inline
and bounded by `MaxRevisions` (20). Every write is a compare-and-swap on
`Version`; a stale one is `VersionConflictError`. Writes are audited as
resource `prompt` (`create`, `update`, `rollback`, `reset`, `delete`) on the
orchestration store trail, identifiers only. Tenant creation seeds a copy of
each prompt with `follow_parent=true` and deletes its writes on rollback; the
boot reconcile (`pkg/server/prompt_reconcile.go`) moves the system set onto
the build's defaults first, then seeds what each tenant lacks, never touching
an edited record. Without an embedded qrdb node there is no store and every
job renders the embedded defaults.

**Permissions.** `prompts:read` and `prompts:write` are `admin` and
`operator`; `prompts:history` adds `auditor`. All are `ScopeSelf` and outside
the Security Officer's duties (`pkg/permission`). The system set is written
only by the boot reconcile.

Byte equality is the acceptance test: `testdata/prompts/*.golden` in both
packages were written by the previous builders and every assembler still
reproduces them exactly. The transports and SDK (go-quicbaas#805), the GUI
(#806) and the per-role charters (#807) are not built. Full description —
scopes, `follow_parent`, seeding, reconcile, and the catalogue of
instructions that ought to become code: [prompt-sets.md](prompt-sets.md).
