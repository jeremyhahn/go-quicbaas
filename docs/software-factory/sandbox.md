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
  [Workspace Storage Tiers](storage-tiers.md)

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
and every Go job escalates to a human (go-quicbaas#772).

They are installed **at `$(go env GOPATH)/bin`, not merely on the `PATH`**. A Go
project's Makefile resolves the linter as `$(shell go env GOPATH)/bin/golangci-lint`
and `test -x`es it, so a binary reachable only through `PATH` still reads as
missing. The image therefore declares `GOPATH` rather than inheriting it from a
`$HOME` the spawn does not promise.

**The versions are the repository's, passed in at build time.** `make
build-worker-image` supplies `GOLANGCI_VERSION`, `GOSEC_VERSION` and
`GOVULNCHECK_VERSION` from the Makefile — the same variables CI reads
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
the container runtime, which returns pipes and a container id. So there is
nothing for `procReaper` to `Wait` on, and a containerized agent's death is
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

## The orphan sweep

A session that ends releases its container: `Close` fires the process cancel
that ends `docker run --rm`, and it is issued on every exit the sdlc runner
takes — including a refusal after the spawn, a checkout that would not run, a
cancellation and a panic (go-quicbaas#750). What no teardown survives is the
process not running any more, so a daemon **killed** between a spawn and its
close leaves its worker container running with nothing left that knows the
session it belonged to.

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
`storage.data_dir` (see [sdlc.md](sdlc.md#configuration)): it is the one setting two
daemons on one host cannot share, and it survives a restart, so a restarted
daemon recognises its predecessor's containers and nobody else's.

The label is part of the container **spec**, not something the runtime adds at
launch: `ContainerConfigDigest` measures `spec.Labels`, and a label injected
below the measurement would make an attested container disagree with the
configuration that was attested. A backend that was never told which daemon it
serves stamps no owner label at all, so no sweep can ever claim its containers
— an unswept container is an operator's problem, a container the wrong process
sweeps is a running worker killed under someone else.

## Verifying a deployment

```bash
# The daemon's own view: every session it spawned, and the container each runs in.
quicbaas agent sessions list

# The host's view: a required-mode daemon must have NO agent processes as children.
ps --ppid "$(pgrep -f 'quicbaas agent serve')" -o pid,comm --no-headers

# And a container per live session.
docker ps --filter label=quicbaas.agent

# Nothing this daemon owns should outlive a finished or refused attempt.
docker ps -a --filter "label=quicbaas.owner=$WORK_DIR"
```
