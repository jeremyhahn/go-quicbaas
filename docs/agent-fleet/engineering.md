# Agent Fleet — Engineering

> **Status: NORMATIVE** · Last verified: 2026-10-11
> The implementation contract for the fleet registry, enrollment, heartbeat,
> the node daemon and its sandbox. Rationale is in
> [architecture.md](architecture.md). Conflicts with this document are defects.

## Registry

`fleetRegistry` (`pkg/orchestration/fleet_registry.go`) is the one
implementation, over an injected `interfaces.KVStore`. The server wires the
qrdb-backed store (durable, replicated, encrypted); `NewInMemoryFleetRegistry`
binds the same code to a memory store for tests.

- Keys are `orch/agent/{tenantID}/{nodeID}`; list is a tenant-scoped prefix
  scan.
- Registration is a guarded create decided by the store, not by a mutex. A
  keyed mutex serialises read-modify-write on one record.
- `FleetNodeStatus`: `registered`, `running`, `idle`, `offline`, `error`,
  `disabled`.
- The canonical node ID is `iam.FleetNodeAppName(tenant, CN)` =
  `agent-<tenant>-<sanitised CN>`; the deploy computes the same value to poll
  for its own node.

### Heartbeat timeout and session reconciliation

`orchestration.heartbeat_timeout` (env
`QUICBAAS_ORCHESTRATION_HEARTBEAT_TIMEOUT`, default 90s) is wired through
`SetFleetHeartbeatTimeout`; `DefaultFleetNodeStaleAfter` equals it.

`pkg/orchestration/session_reconcile.go` (`sessionHostAbsent`) treats a
session's pinned host as gone only on positive evidence:

| Moment | Evidence that the host is gone |
|---|---|
| Startup | No registration for the pinned daemon. Heartbeat age proves nothing: every record predates the restart. |
| Maintenance sweep | Silence past the timeout. |
| Either | An unreadable registry means nothing is gone (fail closed). |

The resolver is asked about the pinned daemon strictly, not about the
backend.

### Disable and enable

`FleetAdminService.DisableFleetNode` marks the node `disabled` and revokes its
OAuth2 client and mTLS certificate pin through the IAM layer, returning
`FleetNodeAlreadyDisabledError` if it already is. `EnableFleetNode` returns
`FleetNodeNotDisabledError` if it is not. Surfaces: REST and MCP.

## Enrollment

`HandleFleetEnroll` (`pkg/api/rest/handlers_fleet_enroll.go`), a public-group
route authenticated by mTLS. In order:

1. Validate the certificate (OU `agents`, ClientAuth EKU); take the tenant
   from the Organization; compare with the body `tenant_id` and the
   tenant-scoped SPIFFE SAN.
2. `RegisterAgentApp`: idempotent per tenant + CN. The `client_id` is
   stable, the secret rotates, and the `self_signed_tls_client_auth` client is
   re-pinned.
3. `registerMTLSApp`, which yields `mtls_client_id`.
4. `enrollFleetNodeCertIdentity` — failure is fatal.
5. Publish to the registry through `iam.FleetRegistrar.RegisterFleetNode`
   with `NodeType "agent-runtime"` and `Kind "daemon"`. The host is rewritten
   with `dialableFleetNodeHost(bind, hostname, peer)`; metadata carries
   `oauth2_client_id` and `mtls_client_id`. A refusal about *who* is
   registering (`fleetRegistrarRefusal`) fails the request; an operational
   registrar fault does not, because the heartbeat recovery path republishes
   the node.

The response advertises `event_ingest_addr` (`dialableEventIngestAddr`), the
gRPC ingest address — never the REST port. Scopes are
`DefaultFleetRuntimeScopes` (`agent:runtime:spawn|prompt|status`) plus the
rest of `iam.FleetRuntimeGrant()`; the role is `orchestration.FleetRuntimeRole`.

The daemon persists `credentials.json` (0600, including `event_ingest_addr`)
under `--work-dir` and on later starts goes straight to `/oauth2/token`.
Delete the file to force re-enrollment (CA rotation, tenant change,
compromise recovery).

### Enrollment retry (`pkg/orchestration/fleet/enroll_retry.go`)

| Class | Examples | Behaviour |
|---|---|---|
| transient | connection refused, network/host unreachable, DNS, timeout, HTTP 502/503/504 | Retry forever; capped exponential backoff with jitter (`--enroll-retry-base` 1s, `--enroll-retry-max` 1m); one INFO line per wait |
| terminal | 4xx, `invalid_client`, untrusted server certificate, missing or unreadable key material, unusable credentials file, invalid configuration, anything unrecognised | Exit 1 at once, naming the class |

There is no attempt budget. The packaged unit sets `Restart=on-failure`,
`RestartSec=5s`, `RestartSteps=5`, `RestartMaxDelaySec=120s`, and in `[Unit]`
`StartLimitIntervalSec=300` with `StartLimitBurst=5`, so a terminal failure
ends in `failed`.

## Heartbeat and recovery (`pkg/orchestration/fleet/heartbeat.go`)

`HTTPHeartbeatSender` posts to `/api/v1/fleet/{agent_id}/heartbeat?tenant_id=`
every `--heartbeat-interval` (30s).

| Response | Daemon action |
|---|---|
| 2xx | done |
| 5xx or network error | back off (up to 30s) and retry |
| 401 | invalidate the token source, retry once with a fresh JWT |
| 404 or 403 | `Reregisterer`: clear credentials, enroll at `/api/v1/fleet/enroll`, persist, invalidate the token, retry once |
| other 4xx | terminal |

Three consecutive `Reregisterer` failures (`maxConsecutiveReregisterFailures`)
surface `ReregisterFailedError` (`ErrNodeReRegisterFailed`) and the process
exits so systemd restarts the unit.

Server side, `HandleFleetNodeHeartbeat` calls `tryRecoverFleetHeartbeat`
(`handlers_fleet_heartbeat_recover.go`) when the node is not found: a valid
peer certificate from a trusted CA is re-published into the registry and the
heartbeat accepted.

The body is optional and capped at 64 KiB. Telemetry comes from
`devicetelemetry.go` and `gpu.go`: NVIDIA GPU (nvidia-smi), Intel NPU
(intel_vpu sysfs) and RAPL CPU package power, used for measured energy and
cost ([accounts](../accounts/architecture.md#measured-energy-from-fleet-telemetry)).

## Sandbox

### Policy

`--sandbox-policy` decides how the daemon resolves sandbox modes. How a
resolved mode becomes a container is in
[sandbox](../orchestration/sandbox.md).

| Policy | `DEFAULT` resolves to | `REQUIRED` allowed | `NONE` allowed | Use |
|---|---|---|---|---|
| `required` | container | yes | no (`PERMISSION_DENIED`) | production, regulated tenants |
| `preferred` | container | yes | yes | staging, mixed workloads |
| `opt-in` | raw process | yes | yes | development; discouraged |

A tenant override may force a stricter policy than the daemon's. Precedence:
tenant > daemon > request. Semantics live in
`pkg/orchestration/sandbox_policy.go`.

### Network

`--sandbox-policy` decides whether the agent runs in a container;
`--sandbox-network` decides whether that container gets its own network
namespace.

| `--sandbox-network` | Namespace | Egress | Reaches host `127.0.0.1` services |
|---|---|---|---|
| `bridge` (default) | own | yes, NAT | no |
| `none` | own, loopback only | no | no |
| `<network name>` | own, user-defined | per network | no |
| `host` | the host's | yes | yes, all of them |

A spawn request has no network field. Choosing `host` is an operator decision
and is logged once per backend at WARN. A sandboxed agent whose MCP endpoint
resolves to `127.0.0.1` cannot reach it over `bridge`; give it a routable
`--mcp-endpoint`, as a remote node always has.

### Bind-mount allowlist

`--allowed-mount-root` (repeatable; config `allowed_mount_roots`) decides how
much of the host's filesystem a container may see.

| Setting | Effect on a sandboxed spawn naming a `work_dir` |
|---|---|
| omitted | Only workspaces the daemon provisioned under its `--projects-root`, for the requesting tenant |
| absolute roots | Those, plus sources resolving under a listed root |

The daemon's relayed signing sockets (`--sockets-root`) are admitted too, one
directory below the root, read-only. The shared per-tenant Go cache
(`--go-cache-dir`, default `<work-dir>/cache/go`) is mounted by the daemon
itself and no entry may reach it: the daemon refuses to start when an allowed
root or the projects root contains the cache root or lies inside it.

Enforcement is `orchestration.MountPolicy`, shared with the control plane:

- Sources are resolved with `filepath.EvalSymlinks` and re-tested; a source
  that does not exist is refused.
- Roots match on path-segment boundaries (`/srv/models` does not grant
  `/srv/models-staging`).
- `work_dir` is authorized when the spawn is containerized; `extra_mounts` on
  every path.
- Listed roots are not tenant-scoped; the projects root is.
- Non-absolute entries fail `Validate()` at start-up.

A refusal is gRPC `PERMISSION_DENIED` naming field, tenant and path, which
the control plane re-surfaces as HTTP 403. There is no setting that turns the
check off.

### Container environment

Provider credentials and MCP connection material reach the container through
a `--env-file`: mode 0600 in a 0700 per-launch directory. They are never
`-e KEY=VALUE` arguments, which `/proc/<pid>/cmdline` would publish to local
users. The file is released when the runtime process exits (detached launch)
or when the session closes (ACP session, on its own cleanup slot). Logs show
the argument vector with every value `[redacted]`. An entry the env-file
format cannot carry (a newline in a value, `=` or whitespace in a name, a
leading `#`) is rejected at spec validation. Code:
`pkg/orchestration/container_env_file.go`; capabilities and pids limit:
`pkg/orchestration/sandbox_security.go`.

### Container runtime selector

| `--container-runtime` | Behaviour |
|---|---|
| `auto` (default) | Probe Docker, then Podman; no runtime if neither |
| `docker`, `podman` | Bind that engine; no runtime if unusable |
| `none` | No containerized sessions; nothing probed |

The outcome is logged at INFO at start-up. Under `--sandbox-policy required`
a daemon with no runtime refuses to start; under `preferred` or `opt-in` it
starts and serves unsandboxed sessions.

### IAM scopes

| RPC | Scope |
|---|---|
| `Spawn` | `agent:runtime:spawn` |
| `Prompt` | `agent:runtime:prompt` |
| `Status`, `Close` | `agent:runtime:status` |

Enrollment issues them. The daemon also rejects a request for a tenant other
than its own with `PERMISSION_DENIED`, encrypts session state at rest, and
audit-logs every RPC.

## Host requirements

- Linux (Ubuntu 22.04+, Debian 12+, Alma/Rocky); `systemd --user` with
  lingering for the unit install, or the nohup launcher.
- Docker or Podman for sandboxed sessions; the daemon user must be able to
  reach the socket.
- Ports: inbound `--bind` (default 9091), outbound to the control plane.
  The daemon dials out; the control plane dials the runtime port.
- Disk: 20 GiB free for worker images and 10 GiB for workspaces are the
  preflight thresholds.
- GPU: an NVIDIA driver plus container toolkit makes Docker pass the GPU to
  containerized Ollama; `nvidia-smi` on the host feeds telemetry.

## Upgrade

Replace the binary and restart; `credentials.json` survives and the daemon
does not re-enroll.

1. `quicbaas fleet stop --spec <file>` (stops the daemon, then deregisters
   it), or `systemctl --user stop quicbaas-agent`. On SIGTERM the daemon
   drains for `--shutdown-grace` (30s).
2. `quicbaas fleet deploy --spec <file>` uploads the new binary and starts
   the node; or copy the binary yourself and `quicbaas fleet start --spec <file>`.
3. `quicbaas fleet list` shows the node `running` with a recent heartbeat.

## Security posture

The daemon enforces tenant isolation, the sandbox policy, the mount
allowlist, scope validation, encrypted session state and audit logging. The
operator owns host hardening, firewall and network security, container
runtime options and image provenance, credential file permissions, resource
quotas, and monitoring.

## Embedded fleet node

`quicbaasd` can run the same daemon in-process (`pkg/server/embedded_fleet_node.go`).

| Flag | Default |
|---|---|
| `--embedded-fleet-node` | off |
| `--embedded-fleet-node-backend` (repeatable) | `ollama` |
| `--embedded-fleet-node-ollama-url` | `http://localhost:11434` (`config.DefaultOllamaBaseURL`) |
| `--embedded-fleet-node-host-affinity` | hostname |
| `--embedded-fleet-node-gpu` | off |
| `--embedded-fleet-node-data-dir` | `<data-dir>/embedded-agent` |
| `--embedded-fleet-node-sandbox-image` | |
| `--embedded-fleet-node-sandbox-policy` | `opt-in` |
| `--embedded-fleet-node-sandbox-network` | `bridge` |
| `--embedded-fleet-node-auto-pull` | |

Config keys under `server.embedded_fleet_node`: `enabled`, `backends`,
`ollama_url`, `host_affinity`, `gpu_available`, `data_dir`, `sandbox_policy`,
`sandbox_network`, `sandbox_cap_add`, `sandbox_pids_limit`, `sandbox_ulimits`,
`sandbox_image`, `image_registry` (config and env only, no flag) and
`auto_pull`.

`ollama_url` resolves flag, then environment
(`QUICBAAS_SERVER_EMBEDDED_FLEET_NODE_OLLAMA_URL`), then file, then the
defaults layer. If it resolves empty while the `ollama` backend is enabled, the
fleet node does not start. Its startup error wraps
`*config.UpstreamEndpointMissingError`, which names
`server.embedded_fleet_node.ollama_url`.

- The node ID is `embedded-<hostname>`.
- Transport is gRPC's in-memory bufconn.
- It has no mount keys of its own: it enforces the orchestrator's
  `orchestration.projects_root` and `orchestration.sandbox.allowed_mount_roots`,
  because otherwise a spawn over bufconn would bypass the control plane's
  check.
- Stop order: deregister, graceful gRPC stop (15s, then forced), cancel
  background goroutines — before the external servers stop.

## Code map

| Concern | Where |
|---|---|
| Daemon config and flags | `pkg/orchestration/fleet/config.go`, `cmd/quicbaas/fleet_serve.go` |
| Enrollment, retry, heartbeat | `pkg/orchestration/fleet/enrollment.go`, `enroll_retry.go`, `heartbeat.go` |
| Packaged unit and env sample | `deployments/fleet-node/{Dockerfile,quicbaas-agent.service,agent.env.sample}` |
| Sandbox | `pkg/orchestration/{sandbox_policy,mount_policy,sandbox_network,sandbox_security,container_env_file}.go` |
| Deploy | `pkg/service/fleet_deploy*.go`, `pkg/orchestration/fleet/deploy`, `cmd/quicbaas/fleet_deploy*.go` |

## See also

[architecture.md](architecture.md) · [cli.md](cli.md) · [api.md](api.md) ·
[user.md](user.md)
