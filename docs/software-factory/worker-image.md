# Software Factory — the worker image refresh

The worker image (`deployments/agents/software-factory-worker/Dockerfile`) is
where every sdlc worker, reviewer and triage session runs. It installs the
**Claude Code** and **Codex** CLIs, and a model release often needs a newer CLI
than the one baked in (`claude-opus-5-5` needs Claude Code 2.1.280; 2.1.278
rejects it). The daemon therefore keeps the image current by itself:
**a scheduled, gated refresh** — never an update at container start. It runs
once a day, and whenever an operator asks: from the CLI, the API, or the web
UI's **Check for updates now** (Software Factory → Fleet → Worker image).

Package: `pkg/sdlc/workerimage`. Issue: go-quicbaas#1021.

## One pass

| step | what happens |
|---|---|
| **Detect** | Read the latest `@anthropic-ai/claude-code` and `@openai/codex` from the npm registry — once per pass, and nothing else reads it — and the versions every bound image of a *managed* repository carries from its labels (`io.quicbaas.worker.claude-code.version`, `io.quicbaas.worker.codex.version`). An image is **stale** when either CLI is older than the registry's (semver; a registry that moves "latest" backwards never downgrades), or when its versions cannot be read (no labels, or not on this host). Nothing stale: the pass ends `current` — unless a tenant's bindings could not be read, in which case it ends `partial`, and `failed` when no tenant's could. A stale image that is **ahead** of the registry in the other CLI (say Claude Code newer than npm's latest while Codex trails) is never rebuilt: the build installs both CLIs at npm's latest, so it would downgrade one to bring the other forward, and the pass fails with the downgrade named instead. |
| **Build** | The *embedded* Dockerfile, with the repository's toolchain pins (`pkg/version/toolchain.mk`) and the newer CLIs, `--pull` so the base image's updates come too. A candidate already built under the same tag with the right labels is reused. The built image's labels are then checked against what was asked for. |
| **Gate** | (1) The image's **probes** — the same list `make test-worker-image` runs (`deployments/agents/software-factory-worker/probes.go`) — against exactly the built **image ID**, as the uid the spawn runs as, each in a named one-shot container (`quicbaas-oneshot-<id>`) that is force-removed if its run is cut short, so a hung probe does not outlive a failed pass: the toolchain and a signed commit, serena, a real Go lint, workflow linting (actionlint reaching shellcheck), full control of the container **under the production sandbox policy** (the capability set and pids bound a spawn uses, rendered by the same code), a fork past a lowered pids bound refused cleanly, and bind-mounted writes owned by the spawning uid. (2) A **smoke**: one real session per distinct agent (backend + model) the promotion would put in the image, **plus** the default Claude model (`sdlc.worker_image.smoke_model`) — spawned through the orchestrator's own path, sandboxed in the candidate, with the worker's own argv and the tenant's **pooled credential**, pinned to the fleet node registered from this node (this node's own embedded fleet node, `embedded-<hostname>` — never another node's, though the shared fleet registry lists them all — or the daemon whose registered host is this node's hostname; or `smoke_host` when set), and asked to answer `READY`. (3) The candidate tag must still name the gated build. |
| **Publish** | With `sdlc.worker_image.registry` set: tag, push (private login with the token from a tenant secret, see below) and record the manifest **digest**. The reference Agents get is `<registry>:<tag>@sha256:<digest>`. Without a registry (**local mode**) it is the local tag, and a local-mode pass refuses to promote at all in a tenant whose fleet has another node (see [Local mode and the fleet](#local-mode-and-the-fleet)). |
| **Promote** | Every [Agent](agents.md) that a binding slot (worker, reviewer or triage) references, and whose image is a stale managed image, is repointed at the reference through the agent catalog (`AgentCatalogService.ReplaceAgentImage`). Bindings name Agents and are not rewritten. Each Agent gets one audit entry. |

With a registry, a pass also **publishes what is already bound**: an Agent that
names a *current* image by a local tag of `repository` is pushed as it is —
the same image, not rebuilt, no gate — under its own tag, and repointed to
`<registry>:<tag>@sha256:<digest>`. Turning a registry on therefore moves every
Agent onto it at the next pass, with nobody repointing by hand. A push that
fails here fails the promotion of the Agents naming that image (the pass ends
`partial`), never the pass.

A failed pass changes **no Agent** and publishes nothing. There is no silent
failure: it is logged at error level, counted on
`quicbaas_sdlc_worker_image_refresh_total{outcome="failed"}`
(and `quicbaas_sdlc_worker_image_last_outcome_timestamp{outcome}`), recorded on
the audit trail (`worker_image:refresh`, outcome failure), and served as the
last report by `quicbaas sdlc worker-image status`.

A pass that promoted but could not update an Agent, or could not read a tenant
(a sealed barrier), ends `partial` and names each — including a pass that found
every readable tenant current: a tenant nobody could read was judged by nobody.
`partial` is surfaced exactly like `failed` (error log, metric, failure audit
entry). A pass that could read no tenant's bindings at all ends `failed`.

## Managed images and the tag scheme

Only Agents naming an image of **`sdlc.worker_image.repository`** (default
`quicbaas-software-factory-worker`) or of **`sdlc.worker_image.registry`** are
managed. Any other image is the operator's own and is never touched.

The **content tag** is `<quicbaas-version>-cc<claude-code>-cx<codex>`, e.g.
`0.11.0-alpha-cc2.1.280-cx0.156.0`. It names what is inside, so the same inputs
always produce the same name, a newer CLI always a new one, and **a previous
tag is never overwritten** — it stays on the host (and in the registry) for
rollback. `make build-worker-image` uses the same scheme.

## Local mode and the fleet

Local mode promotes a tag **only this host's engine holds**. That is right for
a single host and wrong for a fleet: every other node would be handed an image
it does not have and cannot pull. So before it builds, a local-mode pass asks
the fleet registry, per tenant whose Agents it would repoint, for the nodes
that are not this one — another node's embedded fleet node, or a daemon
registered from another host; disabled nodes count, since re-enabling one must
not find it bound to an image it cannot pull. If there are any, the pass fails
with `LocalPromotionRefusedError`, naming them: no build, no Agent changed, and
the usual error log, `failed` metric and audit entry. The fix is
`sdlc.worker_image.registry`.

## Rollback

Every promotion is audited with `from` → `to`. To roll back, write the
previous reference back onto the Agent with `quicbaas agents get <id> -o yaml`,
an edit, and `quicbaas agents update <id> -f`. The old image is still on the
host and in the registry. The next pass moves the Agent forward again only if
it is behind the registry. To hold an Agent on an older CLI, point it at an
image outside the managed repositories.

## Where it runs, and why

The build runs **on the daemon's host**, through the daemon's own container
engine — the same `orchestration.ExecContainerRuntime` the orchestrator spawns
sandboxes with — not behind the agent RPC:

- no agent RPC builds or pulls on the control plane's behalf today; the image
  was only ever built on the node (`make build-worker-image`);
- the gate's smoke is an orchestrator session on a tenant's pooled credential,
  which only the control plane can start;
- reaching the rest of the fleet is the **registry's** job. Agents started with
  `--auto-pull` (`auto_pull` for the embedded fleet node) now pull an image a spawn
  names before starting it, when their host does not hold it — so a promoted,
  digest-pinned reference reaches every node at its first job, instead of a
  3 GB `docker save | ssh docker load` per node per version.

Remote nodes pull with their engine's own credentials: none for a public
package (the usual case — the worker image bakes in no secrets), or a
`docker login <host>` on the node for a private one. A plain-HTTP registry is reached through
the engine's `insecure-registries` entry; nothing here weakens TLS.

## Configuration (`quicbaas.yaml`)

```yaml
sdlc:
  worker_image:
    enabled: true                       # default true (within sdlc.enabled)
    repository: quicbaas-software-factory-worker
    registry: ""                        # host[:port]/owner/name; empty = local mode
    registry_username: ""               # push credential: the registry account ...
    registry_token_secret: ""           # ... the tenant secret holding its token (e.g. sdlc/registry/worker-image/token)
    registry_token_tenant: ""           # ... and the tenant whose secrets hold it
    npm_registry_url: https://registry.npmjs.org
    refresh_interval: 24h               # the daily pass — the only time npm is read
    pass_timeout: 1h
    smoke_model: claude-opus-5-5        # the Claude model every pass must prove
    smoke_host: ""                      # default: the agent registered from this node
```

Every key is also `QUICBAAS_SDLC_WORKER_IMAGE_*`. The loop starts only on a node
with the SDLC module and a container engine; the boot log says which is missing.
It runs a pass every `refresh_interval` and whenever one is requested, one pass
at a time. Nothing runs at start: a daemon restarts far more often than a CLI
ships. There is deliberately no separate, more frequent registry poll — when a
release matters sooner than tomorrow, request a pass.

**`smoke_host`** is resolved per smoke when unset: the fleet node registered
from this node — this node's embedded fleet node (`embedded-<hostname>`; other
nodes' embedded agents are in the same registry and are never chosen), or a
daemon whose registered host is this node's hostname. If none matches (for example the local agent runs in a
container under another hostname), the smoke fails naming the agents that are
registered, and nothing is promoted; set `smoke_host` to the local agent's
name.

**The push credential** is a QuicBaaS secret, never a file. `registry_token_secret`
is a short, tenant-relative secret name — the same names a binding's
`forge.token_secret_ref` uses, never the `tenant/<id>/` storage path — read from
the secrets of `registry_token_tenant` (the tenant that owns the factory).
`registry_username`, `registry_token_secret` and `registry_token_tenant` go
together: all three or none, and a credential needs a `registry`. The reference
is validated when the loop starts; the secret itself is read **only when a pass
pushes**, so a pass with nothing to publish never touches it, and rotating the
token is a new version of the secret with no restart. The token is logged in to
a private client configuration created for that push and deleted after it
(`docker --config <tmp>` / `podman --authfile`), on stdin — never in an
argument, and the host's engine configuration never holds it. With none of the
three set the push uses the engine's own login. The removed
`registry_token_file` key is refused at load.

Storing the token, from a shell where it is typed and never echoed (the tenant
node principal's certificate, a tenant with `secrets:write`):

```
read -rs TOKEN && printf '%s' "$TOKEN" | base64 -w0 |
  python3 -c 'import json,sys; print(json.dumps({"data": sys.stdin.read()}))' |
  curl -s --cacert ca.crt --cert agent.crt --key agent.key -X PUT \
       -H 'Content-Type: application/json' --data-binary @- \
       "https://<node>/api/v1/secrets/sdlc/registry/worker-image/token?tenant_id=<tenant>"
unset TOKEN
```

## On demand

```
quicbaas sdlc worker-image status
quicbaas sdlc worker-image refresh                 # queues a pass, returns at once
quicbaas sdlc worker-image refresh --wait          # follows it; non-zero unless current/promoted
```

**Web UI:** Software Factory → Fleet shows a *Worker image* panel: each
bound image with the Claude Code and Codex versions it carries, the latest
versions npm published at the last check, when that check ran and how it
ended, and **Check for updates now**, which requests a pass and follows it to
its outcome. While mounted the panel re-reads the status on its own — every few
seconds while a pass runs or is queued, whoever started it, and once a minute
otherwise — so a pass already running when the page opened, or one started by
the schedule or another operator, is followed to its outcome too (desktop app through the Wails binding
`OrchestratorBinding.GetWorkerImageStatus` / `RefreshWorkerImage`, browser
through REST). TypeScript SDK: `client.orchestrator.getWorkerImageStatus()` /
`refreshWorkerImage()` (REST and Wails; the other browser transports refuse).

| transport | status | refresh |
|---|---|---|
| REST / QUIC | `GET /api/v1/sdlc/worker-image` | `POST /api/v1/sdlc/worker-image/refresh` (202) |
| gRPC / Unix | `SDLCService/GetWorkerImageStatus` | `SDLCService/RefreshWorkerImage` |
| MCP | `quicbaas_sdlc_worker_image` | `quicbaas_sdlc_worker_image_refresh` |
| Go SDK | `client.SDLC().WorkerImageStatus(ctx)` | `client.SDLC().RefreshWorkerImage(ctx)` |

These are **host verbs**: no tenant, and the cross-tenant permissions
`worker_images:read` (admin, operator) and `worker_images:refresh` (admin) —
held only by a system-tenant identity. A second request while a pass runs or is
queued is refused (409 / `Aborted`); a node without the loop answers 503 /
`Unavailable`.

## By hand

```
make build-worker-image                               # CLAUDE_CODE_VERSION / CODEX_VERSION pins in the Makefile
make build-worker-image CLAUDE_CODE_VERSION=2.1.281   # a newer CLI by hand
make test-worker-image                                # the gate's probes, via ./deployments/agents/software-factory-worker/probe
make push-worker-image WORKER_IMAGE_REGISTRY=<host>/<owner>/software-factory-worker   # scan, push, print the reference
```

`push-worker-image` runs `trivy image --severity CRITICAL,HIGH --ignore-unfixed
--exit-code 1 <image>` first, blocking (devops.md §8): a fixable CRITICAL or HIGH
— or no scanner at all — fails the target before anything is tagged or pushed.
`TRIVY` names the scanner (default: `trivy` on `PATH`). trivy reads the module
list compiled into every Go binary, so a tool whose newest release requires a
vulnerable module fails the gate too. gopls and gosec are therefore built from a
throwaway module that raises `GO_TOOL_DEPENDENCY_FLOORS`
(`pkg/version/toolchain.mk`), not `go install`ed. The fix for such a finding is a
floor there, never a trivy exception. The digest is taken from
the push's own output, since `RepoDigests` can be empty for a locally built
image, and the target ends on one machine-readable line:

```
WORKER_IMAGE_REF=<registry>:<tag>@sha256:<digest>
```

`WORKER_IMAGE_REGISTRY` has no default, in the Makefile or in
`pkg/config/defaults.go`: the deployment names its registry. To push under a
private login, point `DOCKER` at a private configuration:
`DOCKER="docker --config $cfg"`.
