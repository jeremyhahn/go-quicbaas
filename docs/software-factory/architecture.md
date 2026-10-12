# Software Factory — Architecture

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Component boundaries for the Software Factory: what the pieces are, where
> each is documented, and the scope fence. The engine, projects, pooled
> provider accounts and the SDLC module each have their own feature folder:
> [../orchestration/](../orchestration/architecture.md),
> [../projects/](../projects/architecture.md),
> [../accounts/](../accounts/architecture.md) and
> [../sdlc/](../sdlc/architecture.md). Feature folder: `docs/software-factory/`.
> Conflicts with this document are defects.

## What the Software Factory is, and what the pieces are called

The **Software Factory** is the user-facing name for the whole area:
a QuicBaaS node that runs AI coding agents against real repositories and
delivers merged code. In the source tree it is four things, and keeping them
apart is what keeps the engine generic:

| Piece | Package | Documented in |
|---|---|---|
| **The engine** — sessions, the job envelope and its runners, workflows, tasks and autonomy, fleet, inference backends, container trust and sandboxing | `pkg/orchestration` | [../orchestration/architecture.md](../orchestration/architecture.md), plus [job-runners.md](../orchestration/job-runners.md), [../accounts/architecture.md](../accounts/architecture.md#job-admission), [autonomy-and-dispatch.md](../orchestration/autonomy-and-dispatch.md), [sandbox.md](../orchestration/sandbox.md), [fleet.md](../orchestration/fleet.md) |
| **Projects** — the tenant-scoped record that binds a workspace, a repository, per-agent roles and quotas | `pkg/orchestration/project.go`, `pkg/service/project_service.go` | [projects/](../projects/architecture.md) |
| **Pooled provider accounts** — a tenant's pool of provider logins a spawn can be bound to instead of the host identity | `pkg/orchestration/account_*.go` | [../accounts/](../accounts/) |
| **The SDLC module** — git delivery against a forge: a labelled issue becomes a branch, a pull request, a review and a merge | `pkg/sdlc` | [../sdlc/architecture.md](../sdlc/architecture.md) |

"SDLC" is the company's own term and names what the module does. The engine
is the **Software Factory** on user-facing surfaces and `pkg/orchestration`
in code. The earlier working name for the area is retired and `make lint`
enforces its absence in any tracked file — see
[../sdlc/architecture.md](../sdlc/architecture.md#naming).

**Prompt sets** are the fifth piece. Every instruction sent to an agent is
markdown embedded in the binary — two sets, `workflow` owned by the engine and
`sdlc` owned by the module, declared and validated by
`pkg/orchestration/prompts`, with the shipped files being the system tenant's
defaults. The tenant- and project-scoped editable store layered over them is
not built. Both are [prompt-sets.md](prompt-sets.md).

### The scope fence

The Software Factory deliberately does **not**:

- **own identity, tenancy, secrets, keys or the barrier.** Those are go-qrdb's
  and go-xkms's, consumed through the SDK edge; the engine holds a tenant ID
  and asks.
- **run verification or git on the daemon.** A binding's verification
  commands and the delivery's git both run inside the worker's container
  through `ContainerRuntime.Exec`; a job with no containerized session is
  refused rather than run locally.
- **clone anything itself.** The daemon writes a workspace spec; the
  container clones the project and its context repositories.
- **model a delivery pipeline as a Workflow.** A Workflow is a lead agent
  decomposing work into a DAG; an issue is not a DAG. The distinction is the
  `JobRunner` seam ([job-runners.md](../orchestration/job-runners.md)).
- **carry a second forge abstraction.** Only an adapter this build compiles in
  can be bound, and `NewForgeBackend` is fail-closed.

## The engine

The engine's design (backends, sessions and the attach event log, the job
envelope, fleet, containment, the list contract) and the alternatives rejected
are in [../orchestration/architecture.md](../orchestration/architecture.md).
