# Sandboxed Agent Execution

> **Status: NORMATIVE** · Last verified: 2026-09-13
> Supplement to the Software Factory feature folder: how a spawn becomes a
> container, what the container gets, and what happens when it cannot be one.
> Conflicts with this document are defects.

How a spawn becomes a container, what the container gets, and what happens when
it cannot be one.

- Policy semantics (`required` / `preferred` / `opt-in`, tenant overrides) —
  [Sandbox Policy Reference](engineering.md#sandbox-policy-reference)
- Network isolation — [Sandbox Network Reference](engineering.md#sandbox-network-reference)
- Bind-mount allowlist — [Bind-Mount Allowlist Reference](engineering.md#bind-mount-allowlist-reference)
- Image integrity (a separate axis) — [Container Trust](architecture.md#container-trust)
- Where the checkout physically lives, and what bounds it —
  [Workspace Storage Tiers](../software-factory/storage-tiers.md)

## The rule

**`required` fails closed.** A spawn whose effective sandbox mode is `required`
either runs in a container or returns a typed
`orchestration.SandboxModeViolationError`. It never degrades to a host process.

This is stated as a rule because it was once not true. Until 2026-09,
`ACPBackend.dispatchSpawn` asked whether the backend was in CLI mode *before* it
asked what isolation the request demanded, so a CLI-mode backend under
`--sandbox-policy required` ran the agent as a child of the daemon — the
daemon's uid, the daemon host's filesystem and network namespace, the tenant's
provider credential in its environment — and returned a session with no
container id. Nothing logged it. See go-quicbaas#741 / #742.

## How a spawn is routed

The sandbox mode is resolved first, and it selects the path:

| Effective mode | Backend | Path | Process |
|---|---|---|---|
| `required` | CLI mode (`claude`) | `spawnContainerCLI` | agent CLI as the container's command |
| `required` | ACP JSON-RPC (`opencode`, `codex`) | `spawnContainerJSONRPC` | ACP peer as the container's command |
| `required` | either, no container runtime | — | `SandboxModeViolationError` |
| `none` / `default` | CLI mode, persistent | `spawnCLI` | host subprocess |
| `none` / `default` | CLI mode, one-shot | `spawnOneShotCLI` | a fresh host process per prompt |
| `none` / `default` | ACP JSON-RPC | `spawnJSONRPC` | host subprocess |

`SandboxModeDefault` is resolved against the policy by
`Runtime.Spawn` (remote daemon) or `Orchestrator` (control plane) before the
backend is reached, so the backend only ever sees `required` or `none`. The
legacy `Sandboxed: true` flag maps to `required` via
`SpawnRequest.EffectiveSandboxMode()`.

The resume path (`resumeStarter`) applies the identical ordering: a sandboxed
session is reconstituted in a fresh container, never as a host process.

## The containerized CLI session

`spawnContainerCLI` speaks exactly the protocol `spawnCLI` speaks — Claude
Code's `--print --input-format stream-json --output-format stream-json` — over
the stdio of a container instead of the stdio of a subprocess. The argument
vector is built by the same `cliArgs`, and the opening exchange by the same
`openStreamJSON`, so a session cannot behave differently because it was
sandboxed.

What the container gets:

| | How |
|---|---|
| **Credential** | Container **environment**, written by the runtime to a private 0600 `--env-file`. Never a bind-mounted file, never a value in argv (`/proc/<pid>/cmdline` is world-readable). |
| **Workspace** | Bind-mounted at `/workspace` from a host path the mount policy authorized *before* the backend was called. |
| **Extra mounts** | Only those the mount policy authorized; appended verbatim. |
| **Network** | The daemon's configured `--sandbox-network`. |
| **Identity** | The daemon's own `uid:gid` — never root. Files written through a bind mount are therefore owned by the daemon, and Claude Code still accepts its tool grant (it refuses `--dangerously-skip-permissions` at uid 0). Privilege is reached by passwordless `sudo` inside the image instead. |
| **Tool grant** | `--dangerously-skip-permissions`, because the container **is** the boundary. See below. |
| **Lifetime** | `docker run -i --rm`, detached from the Spawn RPC context and torn down by `Close`. |

Both container paths build their spec through one function,
`ACPBackend.sandboxSpec`, so a change to the isolation cannot reach one protocol
and miss the other.

### One reader per session, and the replies it is owed

The agent's stdout is read by **exactly one goroutine** for the life of the
session (`streamJSONReader`), which feeds one channel every consumer takes
records from. A `bufio.Scanner` is not safe for concurrent use, and the
stream's order carries meaning: a `result` belongs to the prompt written
before it.

The open used to start a goroutine to wait for the init record, another to
drain the handshake reply, and every prompt started one more per line — each
returning to its caller on a timeout while the goroutine it had started was
still reading. Measured on the burn-in fleet on 2026-09-21 (go-quicbaas #53
and #62): a handshake whose API calls were being retried for four minutes
outlived the two-minute drain bound, the first prompt's reader then raced the
drain goroutine on the same scanner, and the prompt's own `result` went to a
channel nobody read. Both jobs held their slots for an hour, until the agent
was stopped.

Now the session counts the replies still owed (`pending`): one when the open
handed the session over with the handshake reply still in flight, and one for
every turn whose caller gave up on its context before the reply. A prompt
**settles what is owed before it writes** — reads up to and including each
owed `result` — so every result is attributed to the prompt that asked for it.
A caller that gives up leaves the records where they are; nothing is read
behind anyone's back, and nothing is lost.

### Full control inside the container

**The container is the boundary, and a second boundary inside it only costs
attempts.** A sandboxed session has its own root filesystem, its own network
policy, its own memory cgroup and no credential the engine did not put there.
The operator exercises authority by deciding that the session is containerized
at all — not by rationing tools inside it.

Two things follow, and both were measured rather than assumed
(go-quicbaas#927):

**The tool grant is `--dangerously-skip-permissions`, not
`--permission-mode acceptEdits`.** `acceptEdits` grants the file-editing tools
and withholds Bash, so a containerized worker could edit files and run nothing
— no build, no test, no `git diff` — and in a non-interactive `--print` session
there is nobody to approve the command it would have to ask for. Session
`570cff37-42e7-442d-b079-39d0f4623b50` spent 13m46s on one attempt and ended
`tool_uses=0 blocks=text:6`: six blocks of prose, not one tool call, one attempt
gone and the tree untouched.

`WorkerSpawnArgs(backend, sandboxed)` (`pkg/orchestration/worker_spawn.go`)
returns the wide grant only for a request whose own `SandboxMode` is
`SandboxModeRequired`. An unresolved mode takes the host grant, because the
narrower grant is the safe reading of an answer that is not yet known, and a
host session keeps `acceptEdits` — the operator's `ExecutionGuard` allowlist
remains the authority for what runs on the daemon.

**A role narrows itself by stating its own grant.** `ApplyWorkerSpawnEnvironment`
leaves an `ExtraArgs` that already names a permission mode alone, and that is
the opt-out triage uses (`pkg/sdlc/triage_runner.go`): triage reads a repository
at its default branch and writes nothing, so it keeps the narrower grant even
though it is containerized.

**Privilege is passwordless `sudo`, not `USER root`.** The image installs `sudo`
with `ALL ALL=(ALL:ALL) NOPASSWD: ALL`, so the agent installs a toolchain the
image does not carry with `sudo apk add` and runs anything else it needs. Root
was rejected for two concrete reasons:

- **File ownership on bind mounts.** Anything root writes through a bind mount
  is root-owned on the host, and a root-owned tree in the workspace root is one
  the daemon can neither clean up nor re-clone into. The ephemeral storage tier
  moves the checkout to a tmpfs and narrows this, but the tier is per project.
  The two remaining bind mounts — the workspace spec staging directory and the
  signing agent's socket — are mounted **read-only**, so no bind mount can
  receive an agent's write at all.
- **The tool grant itself.** Claude Code refuses
  `--dangerously-skip-permissions` at uid 0, so a root container would have
  taken back the grant that makes the agent useful.

**The uid must exist in the image's `/etc/passwd`, and that is load-bearing.**
`git` invokes `ssh-keygen` for every `gpg.format=ssh` signature, `ssh-keygen`
calls `getpwuid`, and without an entry it exits `"No user exists for uid"` — so
**every signed commit fails**, after the work is done and before it is pushed.
`sudo` resolves the invoking user the same way and refuses outright. The image
takes the uid as `AGENT_UID`, and the build asserts both properties as the uid
the spawn will run as: a real signed commit with a real read-back verification,
and `sudo -n apk add`. `make test-worker-image` asserts the same three
properties against the built image, plus that a bind-mounted write stays owned
by the spawning uid.

### The boundary around root

Passwordless `sudo` makes root inside the container reachable by design, so
**what root holds there is the width of the boundary**. Left to the runtime it
was Docker's whole default capability set and no process bound
(`HostConfig.CapDrop=[] PidsLimit=<nil>`). A live worker used that to
`apk add docker` and start `dockerd` inside itself (go-quicbaas#1040).

Every sandboxed container now runs under one privilege policy,
`orchestration.SandboxSecurity` (`pkg/orchestration/sandbox_security.go`):

```
--cap-drop ALL
--cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER --cap-add SETGID --cap-add SETUID
--pids-limit 4096
--ulimit memlock=4294967296:4294967296
```

It is applied in the places every sandboxed spawn builds its spec:
`ACPBackend.sandboxSpec`, shared by the CLI, JSON-RPC and exec transports;
the orchestrator's detached sandbox (`spawnSandboxedSession`); and the
OpenVINO/ollama-agent/vLLM tool container. So the embedded fleet node and a remote
`quicbaas fleet serve` get it through the same code. The policy **overrides**
the spec. A caller's `ContainerConfig` cannot ask for `--privileged`, a wider
capability set, a different process bound or its own ulimits.
`ContainerSpec.PidsLimit` and `ContainerSpec.Ulimits` are measured by
`ContainerConfigDigest`, like the capability lists.

**Each add-back is there because a command fails without it.** Measured
with the worker's `sudo -n apk add` under a bounding set missing one
capability at a time:

| Missing        | What fails                                                               |
|----------------|--------------------------------------------------------------------------|
| `SETUID`       | `sudo: setresuid() [0, 0, 0] -> [1000, -1, -1]: Operation not permitted` |
| `SETGID`       | `sudo: unable to change to root gid: Operation not permitted`            |
| `CHOWN`        | `apk: ERROR: Failed to set ownership on etc/.apk.…`                      |
| `FOWNER`       | `apk: ERROR: Failed to preserve modification time on etc/.apk.…`         |
| `DAC_OVERRIDE` | root writing a file whose mode bits exclude it, e.g. the agent's 0600 files |

`NET_RAW`, `MKNOD`, `SETFCAP`, `SYS_CHROOT`, `KILL`, `AUDIT_WRITE`,
`NET_BIND_SERVICE`, `FSETID` and `SETPCAP` are gone. `sudo mknod` is refused.
`ping` still works through the kernel's unprivileged ICMP sockets. Add a
capability back only for a command shown to fail without it.

**Two things are deliberately not done.** No `--security-opt` of any kind, so
the runtime's default seccomp and AppArmor profiles stay in force. And no
`no-new-privileges`: that flag stops a setuid binary from gaining privilege,
and `sudo` is a setuid binary.

**The pids limit counts threads as well as processes.** The default comes from
a measurement. A full `make test` of this repository, the heaviest workload a
worker runs here, peaked at 609 tasks on a 24-core host. That is the `pids.peak`
of the cgroup it ran in, so it is an upper bound. Go's thread count grows with
`GOMAXPROCS`, which is the host's core count, and the agent CLI, `gopls` and
`golangci-lint` run alongside the tests. So the default of 4096 leaves about
6.7 times that peak. A fork past the limit fails with `EAGAIN` inside the
container. It does not use up the host's pid space.

**Locked memory gets the CI runners' limit** (go-quicbaas#1038). Docker's
default `RLIMIT_MEMLOCK` is 8 MiB. A suite that holds its secrets in `mlock`ed
memory runs out after a few hundred keys, so it failed in the worker and passed
in CI. Root in the worker cannot raise the hard limit, because that takes
`CAP_SYS_RESOURCE`, which the policy drops. So every sandboxed container gets
`memlock=4294967296` soft and hard (4 GiB), what the CI runners pass. Locked
pages still count against the container's memory cgroup, so the limit grants
no memory the container did not already have.

**Configuration** (operator only; a spawn request cannot change it):

| Where                          | Capabilities                              | Pids limit                           | Ulimits                                  |
|--------------------------------|-------------------------------------------|--------------------------------------|------------------------------------------|
| Embedded fleet node (`server.embedded_fleet_node`) | `sandbox_cap_add: [...]`         | `sandbox_pids_limit: N`              | `sandbox_ulimits: ["memlock=N"]`         |
| Remote fleet node config            | `sandbox_cap_add`                         | `sandbox_pids_limit`                 | `sandbox_ulimits`                        |
| `quicbaas fleet serve`         | `--sandbox-cap-add CHOWN,...` (repeatable) | `--sandbox-pids-limit N`             | `--sandbox-ulimit name=soft[:hard]` (repeatable) |
| Fleet node daemon unit / image env  | `QUICBAAS_SANDBOX_CAP_ADD`                | `QUICBAAS_SANDBOX_PIDS_LIMIT`        | `QUICBAAS_SANDBOX_ULIMITS`               |

An empty capability list keeps the default set. A non-empty one **replaces**
it. Ulimits use the runtime's own `name=soft[:hard]` syntax and apply **by
name** over the default: an entry naming `memlock` replaces the 4 GiB default,
any other name is added. `ALL`, a malformed capability, a negative limit, an
unknown ulimit, a soft limit above its hard limit and a ulimit named twice are
refused at startup, not at the first spawn. There is no "unlimited".

`make test-worker-image` — and the daemon's worker image refresh gate, which
runs the same probes (`deployments/agents/software-factory-worker/probes.go`) —
proves the policy against the built image. The probe runner renders the flags
with the same two calls the spawn makes (`SandboxSecurity.Apply`,
`ContainerSpec.PrivilegeRunArgs`), so the gate cannot check a policy production
does not use. It asserts that `sudo apk add` works, that root's `CapEff` is exactly `0xcb` (no
`NET_RAW`, no `MKNOD`), that `mknod` is refused, and, using
`pids-limit-probe.py` under a lowered bound, that a fork past the limit gets
`EAGAIN` and the container recovers after its processes are reaped. It also
checks that `ulimit -l` reports the policy's memlock, soft and hard, and that
64 MiB can really be locked. The runner takes the expected value from the
policy it launches the probe under, so the number is not written down twice.

### The worker image

The image must carry the agent CLI on the `PATH` of the uid the daemon runs as.
The spawn clears the image's `ENTRYPOINT` and supplies its own command, so an
image entrypoint never runs — see
`deployments/agents/software-factory-worker/Dockerfile`, built by
`make build-worker-image`.

**It must also carry the toolchain the binding's verification commands need**,
because those commands run inside this container too
(`ContainerRuntime.Exec`), not on the daemon host. For a Go project that is
`git`, `ssh`, `make`, the Go toolchain, a C compiler for `go test -race` — and
**`golangci-lint`, `gosec` and `govulncheck`**, which are what `make lint`,
`make check` and `make security` are made of. An image missing them does not
fail visibly: `make lint` exits non-zero with *"golangci-lint is required but
not installed"* on a tree that is perfectly clean, so every review round fails
and every Go job escalates to a human (go-quicbaas#772). Where `make lint`
also lints workflows it needs **`actionlint`** and **`shellcheck`**: without
shellcheck actionlint silently skips every SC* rule, so the fleet's target
fails closed and verification failed on a clean tree (go-quicbaas#1039). The
image carries both at the Makefile's `ACTIONLINT_VERSION` /
`SHELLCHECK_VERSION`.

**And the build's own tools, GNU where the Makefile assumes GNU.** A project
that embeds a frontend copies it with `rsync`; its Makefile checks use
`grep --include`, `--exclude-dir` and `-E`, which busybox grep rejects — and a
rejected grep inside a `|| true` pipeline *passes* a security check that never
ran. The image carries GNU `grep` and `rsync`, and sets **no `NODE_ENV`**:
under `NODE_ENV=production` `npm ci` omits devDependencies, `tsc` never
installs, and `make build` fails on a missing module (go-quicbaas#966).
It also carries `pkgconf`, `gtk+3.0-dev` and `webkit2gtk-4.1-dev`, because
`make build` links the client with its embedded desktop GUI and `build-cli`
guards that with `pkg-config --exists webkit2gtk-4.1` — the same packages CI's
Build job installs, for the reason CI gives: provision the toolchain rather
than narrow what the gate covers. `make test-worker-image` probes all of
these. The cheapest way to find the next gap is to run the project's
`make build` inside the image on a clone of main:

```sh
docker run --rm -e HOME=/tmp -v "$PWD":/src:ro \
  quicbaas-software-factory-worker:<tag> \
  sh -lc 'git clone -q /src /tmp/w/p && cd /tmp/w/p && make build'
```

They are installed **at `$(go env GOPATH)/bin`, not merely on the `PATH`**. A Go
project's Makefile resolves the linter as `$(shell go env GOPATH)/bin/golangci-lint`
and `test -x`es it, so a binary reachable only through `PATH` still reads as
missing. The image therefore declares `GOPATH` rather than inheriting it from a
`$HOME` the spawn does not promise.

**The versions are the repository's, passed in at build time.** `make
build-worker-image` supplies `GOLANGCI_VERSION`, `GOSEC_VERSION` and
`GOVULNCHECK_VERSION` from `pkg/version/toolchain.mk`, which the Makefile
includes and the daemon's worker image refresh embeds — the same variables CI reads
(`make print-GOLANGCI_VERSION`) and `make install-tools` installs. There is one
copy of each pin in the repository, because a worker that lints with a
different version than CI produces a review verdict the merge then contradicts.
`make test-worker-image` proves it on the built image: the tools resolve where a
Makefile looks for them, `golangci-lint` reports the pinned version, and it
lints a vendored module clean **and** reports a finding on a bad one, with the
container on `--network none`.

Trivy — the fourth member of `make security` — is deliberately **not** in the
image: it downloads a vulnerability database on every run, so in a sandbox whose
network policy is the operator's to decide it would fail for the environment
rather than for the diff. A binding that needs it must name an image carrying it.

### No supervisor, by design

A containerized session has no `*exec.Cmd`: the `docker run` process belongs to
the container runtime, which returns pipes, a container id and a `Wait` that
reaps the client. The session calls that `Wait` once, when it is torn down, so
an exited client never stays a zombie of the daemon. There is still no
`procReaper` watching the process, and a containerized agent's death is
observed as EOF on its stdio rather than as an exit code. It is reported as
`ContainerStreamClosedError`, which names the container and the image and
deliberately carries **no** exit code — a fabricated `0` would read as success.
Host sessions keep the supervisor and `StreamJSONProcessExitedError`
(exit code + stderr tail) unchanged.

## The container id is load-bearing

`Session.ContainerID` is what lets the sdlc runner run the checkout, the
verification commands and the publish *inside* the sandbox
(`ContainerRuntime.Exec`). A worker session that reports no container id is
refused by the runner before it is prompted. If a required spawn comes back with
an empty `ContainerID`, the sandbox was not applied — treat it as a security
defect, not a cosmetic one.

## Agent-local workspaces

A spawn **names** the directory its session works in; it never gives a host
path. The host that executes the session — this daemon's in-process backend or
a fleet node (`quicbaas fleet serve`) — provisions it under **its own**
projects root and reports where:

```
<projects_root>/<tenant_id>/<project_id>/workspace/<key>          # the checkout, mounted at /workspace
<projects_root>/<tenant_id>/<project_id>/workspace/quicbaas-workspace-*   # the spec + forge token, read-only at /run/quicbaas/workspace
```

Until go-quicbaas#981 the sdlc runners provisioned that directory under the
**daemon's** projects root and bind-mounted the absolute path, so every sdlc and
review spawn routed to an agent on another machine was refused at the agent's
mount check (`mount source is outside the tenant's projects root and every
allowed mount root`). Git already stayed agent-side — the clone, the commit and
the push run inside the session's container — and now the directory does too.

| Piece | Where it lives |
|---|---|
| `SpawnSessionRequest.Workspace` (`tenant`, `project`, `key`) | the spawn; mutually exclusive with `WorkDir` |
| workspace spec + forge token (`WorkspaceSpecFiles`) | decided on the daemon, carried on the authenticated spawn RPC, **written by the executing host** beside the workspace; never serialized into a durable record |
| `WorkspaceManager.ProvisionSession` | the one provisioner, run by the daemon for in-process spawns and by every agent for its own |
| `SpawnSessionResponse.Workspace` (`agent_host`, `key`, `dir`) | reported back; `dir` is recorded as the session's `WorkDir` |
| `JobStatus.WorkspacePin` | the job's first placement, stored **strictly** the moment the spawn returns — before the checkout. A store that refuses it closes the session and returns `WorkspacePinPersistError` (parked), so no checkout is ever made that a recovery could not locate |

**Workspace keys.** Delivery `<task>-a<attempt>` (one checkout per attempt,
shared by its rounds and rolled-over sessions), review `review-<pr>-<head>`,
triage `triage-<project>`.

**A job stays where its checkout is.** `JobRun.Spawn` stamps every later spawn
of the job that names a workspace — the next review round, a session rolled over
on a full context, the respawn after a daemon restart — with the job's pin. The
orchestrator resolves a pinned spawn with **strict** host affinity to that agent
(or to the in-process backend when the daemon holds it) and checks the result
after resolution, because the dispatcher's soft fallback (an offline pinned host
falls through to the in-process backend) is exactly a fresh clone somewhere
else. What cannot be honoured is refused, typed, before any container starts:

| Refusal | When |
|---|---|
| `WorkspaceFleetNodeUnavailableError` | the pinned agent is deregistered, offline, or no longer serves the backend — or it resolved (heartbeat still fresh) and then never answered the spawn |
| `WorkspaceStaleError` | the pinned agent is there but the checkout is not where the pin says (removed, or its projects root moved) |
| `SessionWorkspaceUnsupportedError` | the executing host has no projects root, or is an agent build that predates named workspaces (the daemon closes that session again) |
| `SessionWorkspaceTenantError` | the workspace names a tenant other than the one the spawn runs under |
| `WorkspaceLinkError` | a level of the workspace is a symbolic link |

All four unwrap to `orchestration.ErrSessionWorkspace`, which crosses the agent
wire as spawn-fault evidence (`session_workspace`), and the sdlc runners report
them as `WorkspaceUnavailableError` → `ErrWorkspaceUnfit`: the attempt is parked
for an operator (`workspace_unfit`, `operator_repair`) with no tokens spent and
no attempt charged. Restart the agent, or readmit the task — a readmitted task
is a new attempt with a new key, which may land anywhere.

**Provisioning never leaves the tenant's subtree.** A tenant can mount its own
workspaces read-write, so anything under `<projects_root>/<tenant>` may be a
link it planted. Every create, chmod, write and removal the provisioner and the
sweep make goes through an `os.Root` opened at `<projects_root>/<tenant>`,
which refuses a path resolving outside it, race-free. Every level of a
workspace must be a directory — a link there is `WorkspaceLinkError` — and a
mode is set through a handle opened inside the root and proved to be the
directory at that name, never by name (`Root.Chmod` is documented as racy).

**The forge token never outlives its session.** The spec directory holds it in
plaintext, so:

- the executing host removes it when the session closes, and when a spawn fails
  after writing it;
- **at startup**, before it accepts a spawn, every host removes every spec
  directory under its projects root (`WorkspaceManager.SweepSessionSpecs`,
  run by `fleet serve`, the embedded fleet node and the daemon). No session
  survives a restart — the agent's container sweep ends them — so each one is
  an orphan no `Close` will ever release;
- the in-memory copy is wiped (bytes zeroed) the moment it is written on the
  executing host, and on the daemon once the spawn is over, sent or not.

What an agent persists about a session (`<work-dir>/sessions/*.json`) is the
workspace path and nothing more; the token is re-delivered by the control plane
on every spawn. The checkout stays for the job's next session.

**The mount grant.** An agent's projects root is admitted **tenant-scoped** by
its mount policy, exactly as the control plane admits its own, so it needs no
`--allowed-mount-root` entry. It defaults to `<--work-dir>/projects`; set
`--projects-root` to put checkouts on a larger or faster disk. It must persist
across restarts — a root on tmpfs makes every restart a `WorkspaceStaleError`.

**The signing agent is mounted where it is local, relayed where it is not.** A
container that signs commits — the delivery worker — or clones an **ssh**
remote is handed the KMS-backed ssh agent's socket, which is bound on the
**daemon** host. An agent that can reach it mounts it; one that cannot is
served the same agent **over its FleetRuntime link** (go-quicbaas#1013). The key
never leaves the daemon host either way.

*Local.* An agent proves it can reach the daemon's sockets root:

- The daemon writes a random identifier into its sockets root
  (`<sockets_root>/.quicbaas-signing-root`, `orchestration.EnsureSigningRootID`).
- An agent looks for that marker in, and one level below, each of its
  `--allowed-mount-root` directories (`DiscoverSigningRootIDs`) and advertises
  `signing-agent:<id>` for each one it finds — at registration and on every
  heartbeat (`signing_agents`), so the trait follows a daemon that re-created
  its root. An agent on another host finds none. A path would not do as the
  identifier: two daemons with the same data directory on two hosts derive the
  same sockets root.

*Relayed.* Every `quicbaas fleet serve` advertises `signing-relay` — at
registration and on every heartbeat (`signing_relay`). For a spawn placed on
such an agent that holds no `signing-agent:<id>` for this daemon, the control
plane's `RemoteACPBackend`:

1. drops the signing-socket mount (`SSH_AUTH_SOCK` stays), reads the public
   keys off the session's own daemon-local agent, and sends them on the spawn
   (`SpawnSessionRequest.signing_relay`);
2. the agent binds a socket of its own — `xkmsssh.StartNode` under its
   **sockets root** (`--sockets-root`, derived like the server's:
   `$XDG_RUNTIME_DIR/quicbaas/<instance>` or `<--work-dir>/s`) — whose signers
   hold only those public keys, and mounts it read-only at the same target;
3. after the spawn returns, the control plane opens
   `FleetRuntime.SigningRelay` and waits for `attached` **before** Spawn
   returns, so no checkout can ask for a signature the relay is not yet there
   to carry;
4. every signature the container asks for travels as a `sign` frame; the
   control plane answers it by dialing the session's daemon-local socket with
   a fresh connection, so a reader's `StopSigning` still closes the window;
5. every relayed signature is audited on the control plane — session, tenant,
   agent, key fingerprint and the SHA-256 of the data, never the data
   (`orchestration_signing_relay:sign`).

The refusals are typed and final. The control plane refuses a `sign` for
another tenant, session or key (`SigningRelayMismatchError`). The agent
authorizes the stream for the session's tenant under the spawn scope and
refuses an unknown session, another tenant's, one spawned with no relay, and a
second attach (`SigningRelayAttachError`). A sign before the attach, or after
the stream dropped, fails (`SigningRelayLinkError`): a dropped link is never
re-established and nothing falls back to another key. Closing the session
releases the relay socket; the agent sweeps the relay directories a killed
predecessor left at startup (`orchestration.EnsureSigningSocketRoot`, the same
rules as the daemon's own root) and refuses to start on a root it cannot vouch
for.

*Placement.* A spawn that mounts the signing socket
(`WorkspaceSigningAgentSocket`) is resolved with the requirement on its
context; the fleet resolver takes an embedded fleet node, one advertising **this**
daemon's `signing-agent:<id>`, or one advertising `signing-relay`, and
otherwise this daemon's own backend. With none of those it refuses with
`SigningAgentUnavailableError` — parked `workspace_unfit`, uncharged.

**Commands run where the container is.** The checkout, the verification and
the publish are commands the engine runs in a session's container
(`JobRun.Exec`). For a session on a fleet node the daemon's container runtime
does not know that container, so `ExecSession` routes the command to the agent
that holds it over the `FleetRuntime.ExecSession` RPC — authenticated as the
agent's tenant under the spawn scope, resolved from the SESSION (the caller
never names a container), refused for a session the agent did not spawn, of
another tenant (answered as not found), or with no container. The command is
validated exactly as on the local path, and the engine's command allowlist is
applied before either path; on the agent it runs under a deadline (30 minutes
when the caller set none) and each of stdout and stderr is bounded to 1 MiB,
keeping the end and cutting on a UTF-8 rune boundary. Output travels as
protobuf bytes and is returned byte for byte, exactly as the local exec path
returns it — output that is not UTF-8 is neither rejected nor rewritten. An agent that cannot be reached is `SessionHostUnavailableError`
— the daemon's runtime is never asked.

**A silent agent is not placed on.** A crashed daemon cannot mark itself
offline, so its record keeps its last status; when **placing a new session**
the resolver skips any daemon-kind registration whose last heartbeat is older
than `orchestration.heartbeat_timeout` (default 90s, `orchestration.FleetNodeAlive`,
the predicate the fleet capacity sum already used). **Routing an existing
session is different:** its prompt, status, exec and close go to the host it
was spawned on or fail with `SessionHostUnavailableError` — never to the local
backend, whose "not found" would conclude a session still running on the
agent. A missed heartbeat is not evidence the session is gone. Before this, a dead
agent's reviews were routed to a refused connection until someone
deregistered it.

**Work spreads across the fleet.** Placement no longer takes the first eligible
agent the registry lists — which put every spawn on one host while the rest of
the fleet sat registered, online and idle. Among **every** agent that serves the
backend and meets the spawn's constraints, the resolver takes the least loaded
(`remoteBackendPool.leastLoaded`):

1. the jobs live on the agent — counted from the control plane's own live
   session records by the host each was placed on
   (`orchestration.FleetLoadReporter`; a job's several sessions are one slot) —
   relative to its advertised `max_concurrent_jobs`;
2. then fewest placements in the last ten minutes, so a burst placed before any
   session exists goes round the fleet;
3. then name.

An agent already at its `max_concurrent_jobs` is skipped; a fleet full
everywhere is refused with the `capacity` constraint. An agent advertising no
capacity is ranked as one slot and never skipped. **Host affinity stays
strict:** a pinned workspace goes to its host whatever the load. The embedded
agent is ranked like any other.

## Container lifecycle and labels

**A session that ends removes its container.** `Close` removes the container by
name (`docker rm -f`), then cancels and reaps the `docker run -i --rm` client.
The sdlc runner issues it on every exit it takes: success, failure, a refusal
after the spawn, a checkout that would not run, a cancellation, a panic, a
rolled or superseded session, and the park (go-quicbaas#750). A spawn whose
handshake fails tears its container down the same way.

Killing the client alone is not enough, and that is how the burn-in node's disk
filled on 2026-09-24. A killed client closes the container's stdin, and `--rm`
fires only when the container's process exits. An agent that does not exit on
EOF keeps the container running, for example one mid-turn or one waiting on its
MCP servers. After that, nothing knows the container exists.

**Every sandbox container is labelled with who it runs for**, before it is
launched:

| Label              | Value                                                     |
|--------------------|-----------------------------------------------------------|
| `quicbaas.agent`   | the backend type (`claude`, `opencode`, ...)              |
| `quicbaas.session` | the session ID the session is registered under            |
| `quicbaas.job`     | the job that spawned it (absent when no job did)          |
| `quicbaas.tenant`  | the tenant it belongs to                                  |
| `quicbaas.owner`   | the daemon that launched it (see below)                   |

The session ID is decided before the launch, because a container cannot be
relabelled once it exists. The label used to be the placeholder `pending`, so
nothing could map a container back to its session. The agent CLI reports its own
session ID in its handshake. For an ACP agent that ID is kept as the session's
wire ID, as on a resume. For this reason a sandboxed session always resumes by
transcript replay, never with the CLI's `--resume`: the CLI's session store died
with the previous container anyway.

To map a container to its session and job:

```bash
docker ps --filter label=quicbaas.agent \
  --format '{{.Names}}\t{{.Label "quicbaas.session"}}\t{{.Label "quicbaas.job"}}\t{{.RunningFor}}'
```

## The orphan sweep and the reaper

What no teardown survives is the process not running any more. A daemon
**killed** between a spawn and its close leaves its worker container running,
with nothing left that knows which session it belonged to. A running daemon can
also lose track of a container, for example when a removal failed during
`Close`. Two passes cover these cases: the startup sweep and the running reaper.

**Startup sweeps.** It is the one moment a daemon knows which containers could
be its own and is holding none of them, so it removes the containers a previous
run of itself left and then starts its spawn server. A sweep that cannot be
completed — the runtime refusing to enumerate, or an identified orphan refusing
to be removed — is a startup failure, for the same reason the signing-socket
sweep's is (#676): an orphan that survives its sweep accumulates without bound.

**What makes the sweep exact is a label.** Every sandbox container carries

```
quicbaas.owner=<the daemon's work directory>
```

and the sweep asks the container runtime for exactly that label. A container
this daemon did not launch has no such label and is never enumerated, let alone
removed — an operator's own containers, and another daemon's live workers, are
untouched. The owner is the daemon's work directory for the same reason the
signing sockets root carries a per-daemon segment derived from
`storage.data_dir` (see [../sdlc/engineering.md](../sdlc/engineering.md#configuration)): it is the one setting two
daemons on one host cannot share, and it survives a restart, so a restarted
daemon recognises its predecessor's containers and nobody else's.

The label is part of the container **spec**, not something the runtime adds at
launch: `ContainerConfigDigest` measures `spec.Labels`, and a label injected
below the measurement would make an attested container disagree with the
configuration that was attested. A backend that was never told which daemon it
serves stamps no owner label at all, so no sweep can ever claim its containers
— an unswept container is an operator's problem, a container the wrong process
sweeps is a running worker killed under someone else.

Three kinds of daemon launch sandbox containers, and each claims its own:

| Launcher                                  | Owner label                     |
|-------------------------------------------|---------------------------------|
| `quicbaas fleet serve`                    | its `--work-dir`                |
| the embedded fleet node inside `quicbaasd`     | its data directory              |
| `quicbaasd`'s own orchestration backends  | `orchestrator:<data_dir>`       |

The orchestration backends in `quicbaasd` had no owner label until 2026-09-24,
so their containers were never swept. Their owner differs from the embedded
agent's because the two hold different sessions. A reaper that asked one of them
about the other's sessions would reap live workers.

**The running reaper** runs every 5 minutes (`DefaultContainerReapInterval`) in
all three daemons. It lists the containers carrying the daemon's owner label and
removes a container only when both of these are true:

- none of the daemon's backends holds a live session with the container's
  `quicbaas.session` ID, and
- the container is older than the 15-minute grace period
  (`DefaultContainerReapGrace`).

A container whose session is held is never touched. The grace period protects an
in-flight spawn: its container exists before the handshake that registers the
session. A container whose creation time cannot be read is kept. So is a
container that carries no session ID and is younger than the grace period; one
that is older is reaped. One removal that fails does not stop the pass.

Every removal is audited as a `WARN` log line that names the container, its
session, job and tenant, its age and the reason. The pass returns the same
record as `ContainerSweep.Removals`.

## The shared Go cache

A worker that runs `go build` or `go test` used to fill `GOMODCACHE` and
`GOCACHE` inside its own writable layer. On the burn-in node on 2026-09-24 that
was 9-12 GB of every 10-15 GB container: about 2 GB of modules and 7-10 GB of
build cache, rebuilt from nothing in every container. Go's module and build
caches are safe for concurrent use, so every container a daemon launches now
mounts one host-side copy:

```
<go_cache_dir>/<tenant>/mod    ->  /var/cache/quicbaas/go/mod    (GOMODCACHE)
<go_cache_dir>/<tenant>/build  ->  /var/cache/quicbaas/go/build  (GOCACHE)
```

| Daemon                 | Setting                              | Default                |
|------------------------|--------------------------------------|------------------------|
| `quicbaas fleet serve` | `--go-cache-dir` / `go_cache_dir`    | `<work_dir>/cache/go`  |
| `quicbaasd`            | `orchestration.sandbox.go_cache_dir` | `<data_dir>/cache/go`  |

**The cache is keyed by tenant.** A worker has full control of its container,
and the toolchain trusts both caches once they are written. An extracted module
is not re-checked against `go.sum` on each build, and a build-cache entry is
reused by its action ID alone. A cache shared across tenants would let one
tenant's worker plant code that another tenant's build then compiles and runs.
Workers within one tenant already share that tenant's forge identity and
credentials, so sharing a cache between them gives a worker nothing it could not
already do.

**The mount policy admits no request mount of the cache.** The daemon mounts the
cache itself after the request's mounts were authorized, as it does with the
credential directory. The daemon refuses to start if the cache root is inside,
or contains, the projects root or any allowed mount root. So no spawn can name a
cache directory, its own tenant's or another's, as an extra mount.

The build cache trims entries unused for five days on its own. The module cache
grows only with the distinct module versions a tenant's builds fetch. To reset
a tenant's cache, stop the daemon and remove its `<go_cache_dir>/<tenant>`.

## Verifying a deployment

```bash
# The daemon's own view: every session it spawned, and the container each runs in.
quicbaas agent sessions list

# The host's view: a required-mode daemon must have NO agent processes as children.
ps --ppid "$(pgrep -f 'quicbaas fleet serve')" -o pid,comm --no-headers

# And a container per live session.
docker ps --filter label=quicbaas.agent

# Nothing this daemon owns should outlive a finished or refused attempt.
docker ps -a --filter "label=quicbaas.owner=$WORK_DIR"

# Every container names a real session, never "pending".
docker ps --filter label=quicbaas.session=pending

# The writable layer holds no Go cache: a few hundred MB, not 10 GB.
docker ps --size --filter label=quicbaas.agent
```
