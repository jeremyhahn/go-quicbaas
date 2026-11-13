# Workspace Storage Tiers

> **Status: NORMATIVE** · Last verified: 2026-09-24
> Supplement to the Software Factory feature folder: where a sandboxed agent's
> checkout physically lives, what bounds it, and what makes its work survive the
> container. Conflicts with this document are defects.

A sandboxed agent writes continuously — a clone, a build cache, test binaries,
and every edit it makes. Bind-mounted from the host, all of it lands on the
operator's SSD, per attempt, forever. The **ephemeral** tier moves the checkout
into a sized tmpfs inside the container instead, so those writes never reach a
disk.

Implementation: `pkg/orchestration/workspace_storage.go`,
`pkg/orchestration/job_turn_push.go`, `pkg/orchestration/sdlc/publish.go`.

## The three tiers

| Tier | What it holds | Where it lives | Survives the container |
|---|---|---|---|
| **ephemeral** | the checkout | container-local tmpfs, sized, inside the memory cgroup | no — the branch survives by being pushed |
| **shared persistent** | caches worth keeping across attempts (module cache, npm cache) | host bind mount, authorized by `MountPolicy` | yes |
| **per-session persistent** | the transcript, and the *branch* | the daemon's session log; the forge | yes |

Only the first is new. The shared-persistent tier is
`SpawnSessionRequest.ExtraMounts` — already built, already authorized against
the operator's `MountPolicy` — and an ephemeral **checkout** deliberately leaves
those mounts alone: an ephemeral module cache would invert the goal, because a
cold download per attempt costs more than the writes the tmpfs saves. The
per-session tier's interesting half is the branch, and it persists by being
pushed rather than by a surviving working tree.

## Configuration

The tier is a property of the **project record**, never of a spawn request: it
decides where a tenant's work lives and whether it survives a restart, so a
caller able to ask for an ephemeral workspace could discard another tenant's
checkout by asking.

```go
// orchestration.Project
Storage ProjectStorage
```

| Field | Unit | Zero value |
|---|---|---|
| `workspace_tier` | `""` \| `persistent` \| `ephemeral` | `""` — host bind mount, unchanged |
| `workspace_tmpfs_mb` | MiB | unsized (grows into the whole memory cgroup) |
| `container_memory_mb` | MiB | the runtime's own default, usually unbounded |

**Every field is inert at its zero value.** An unconfigured project launches the
container it always launched, down to the mounts and to
`ContainerConfigDigest`. `""` and `persistent` behave identically; resolving the
zero value to `ephemeral` would silently move every existing project's work into
RAM and lose anything a turn had not pushed.

The three keys travel **flat** on `CreateProject` / `UpdateProject` and on the
project read shape over every transport (REST, gRPC, QUIC, MCP, Unix and the
embedded transport); gRPC carries them on `api/proto/project.proto`. On an
update, `workspace_tier` applies when non-empty and the two sizes are
presence-based: absent leaves the stored size alone, `0` clears it.

```bash
quicbaas project update <id> --workspace-tier ephemeral \
  --workspace-tmpfs-mb 4096 --container-memory-mb 8192
```

An unrecognized tier is **refused**, not rounded down: the zero value is the
weakest reading of an operator's intent, so a typo in `ephemeral` would quietly
leave the project writing to the disk it was configured off. The refusal is
the project service's (`ProjectStorage.Validate` on both the create builder and
the update applier), so every transport answers with the same
`InputValidationError` rather than each handler checking separately.

### Transitional risk

Memory and tmpfs limits preserve today's **unbounded** behaviour until an
operator sets them. An ephemeral tier with `workspace_tmpfs_mb: 0` and
`container_memory_mb: 0` is a tmpfs that may grow until the host runs out of
memory. Configure both together.

## How the tier reaches a container

There are **two** paths that build a sandbox container, and only one of them
runs a software-factory worker:

| Path | Used when | Builds the spec in |
|---|---|---|
| `agentServiceImpl.spawnSandboxedSession` | the backend does not containerize itself | `orchestrator.go` |
| `ACPBackend.sandboxSpec` | an ACP backend holds a container runtime | `acp_backend.go` |

`SpawnSession` sets `backendHandlesContainers` for the second case and **skips
the first entirely**, so the SDLC worker — an ACP backend with
`SetContainerRuntime` — never reaches `spawnSandboxedSession`. Both paths call
the same applier:

```go
orchestration.ApplyWorkspaceStorage(spec, containerCheckoutPath, plan)
```

The plan itself is resolved once, at the orchestrator's pre-side-effect
chokepoint beside the sandbox mode, the trust level and the network, and
travels to a backend on `SpawnRequest.Storage`.

The applier **replaces** the mount at the checkout path rather than adding one
beside it: a tmpfs added alongside the bind mount would be shadowed by whichever
the runtime applied last, and the checkout would still be on the operator's
disk.

### A remote spawn

A spawn dispatched to a remote fleet node daemon is sandboxed by *that* daemon, which
does not hold the project record — so the resolved plan travels on the wire, for
the same reason `trust_level` does:

```
SpawnSessionRequest.storage          api/proto/orchestrator.proto (field 27)
ContainerMount.tmpfs_size_bytes      api/proto/orchestrator.proto (field 5)
```

`pkg/orchestration/fleet/spawn_wire_contract_test.go` walks `SpawnRequest` by reflection and
fails on any field its sentinel leaves at zero, so a storage term that stopped
travelling is a test failure rather than a remote worker quietly writing to that
host's disk.

## The memory budget is fail-closed

A tmpfs is memory, not disk. Every byte written to it is a byte of the
container's memory cgroup, shared with the compiler, the linker and the test
binaries running beside it. A container whose limit equals its tmpfs size has
nowhere to work, and it does not fail at launch — it fails partway through an
attempt, as an OOM kill that reads like a flaky build.

```
sum(tmpfs sizes) + WorkspaceBuildHeadroomBytes  <=  MemoryBytes
```

`WorkspaceBuildHeadroomBytes` is **1 GiB and is a constant, not a knob**: an
operator who could dial it to zero to make a refusal go away would have
configured exactly the under-provisioned container the check exists to reject.

The check lives in `ContainerSpec.Validate()`, which every launch path already
calls (`ExecContainerRuntime.Run` and `RunInteractive` both do), so no caller can
skip it. The refusal is a typed `*orchestration.MemoryBudgetError` naming the
shortfall and every term that produced it:

```
container memory limit of 4294967296 bytes is short by 1073741824 bytes:
4294967296 bytes of tmpfs mounts plus 1073741824 bytes of build headroom
needs 5368709120 bytes
```

Two shapes are deliberately **not** checked, because both are correct
configurations today: a spec with no memory limit has no budget to exceed, and a
limited container with no sized tmpfs has nothing sharing its memory with a
build (a 64 MiB sidecar is not under-provisioned).

`MemoryBytes` and `mount.tmpfs_size` are folded into `ContainerConfigDigest`:
a configuration whose memory limit was lifted, or whose tmpfs was resized, must
not measure as the one an operator approved.

## The post-turn publication

On the ephemeral tier the tmpfs is the **only** copy of the work. A restart, an
eviction or an OOM kill takes every commit that was never pushed, and the
attempt resumes from a head missing work a person can see was done.

So the engine publishes after every turn that touched the tree:

```
pkg/orchestration/sdlc/runner.go   runTurn → workerTouchedTree → job.PushTurn
pkg/orchestration    JobRun.PushTurn → TurnPusher (installed by the runner)
pkg/orchestration/sdlc/publish.go  publisher.Publish, plan.Force → git push --force-with-lease
```

- **The engine owns the *when*, the runner owns the *how*.** `runTurn` is
  already the one place that knows a turn touched the checkout; the publication
  is the SDLC runner's existing `publisher`, which commits signed and pushes
  **inside** the worker's container. There is no second git implementation and
  none on the daemon.
- **It asks the agent for nothing.** An agent told to push may forget, may push
  the wrong ref, and costs tokens to be reminded. Deterministic work belongs in
  Go.
- **`--force-with-lease`, never `--force`.** A worker that amended or rebased
  between turns leaves a branch the remote cannot fast-forward to, and a plain
  push would fail the job over work that is fine. The lease refuses the push when
  the remote moved since this checkout last saw it, so the one thing a force must
  never do — discard a commit this container never observed — is refused by git.
- **Never the carve-out's target.** `plan.RemoteBranch` is cleared, so an
  unverified, unreviewed turn cannot land on a trunk.
- **A failure is fatal to the turn.** It is logged at error level with the job
  id, filed on the job's audit trail as `turn:push_failed`, and returned as a
  typed `*orchestration.TurnPushError`. Swallowing it would let an attempt spend
  its whole budget building on a head nobody can fetch.
- **Off the ephemeral tier it does nothing.** A persistent checkout is still on
  the host after the container goes, so there is no per-turn forge round-trip.
  Lead-driven jobs install no pusher at all.

## Verifying a deployment

```bash
# The tier a project is configured with (the record is the source of truth).
quicbaas project get <id> --output json | jq .storage

# What the container actually got: a tmpfs with no host source, and a limit.
docker inspect <container> --format '{{json .HostConfig.Mounts}}{{.HostConfig.Memory}}'

# The branch is being published per turn, not only at teardown.
quicbaas orchestrator job events <job-id> | grep -E 'agent_turn|turn:push_failed'
```

## See also

- [Sandboxed Agent Execution](../orchestration/sandbox.md) — how a spawn becomes a container
- [Container Trust](../orchestration/architecture.md#container-trust) — the measurement the
  resource limits are folded into
- [Projects](../projects/architecture.md) — the project record
