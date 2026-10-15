# Agent Fleet — Architecture

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Design rationale for the fleet: the node daemon, the registry, enrollment,
> heartbeat and recovery, remote deploy, and the worker image. Conflicts with
> this document are defects.

The **fleet** is the set of hosts that run AI agent sessions on behalf of the
control plane. A **fleet node** is one such host: a `quicbaas fleet serve`
daemon (or the in-process embedded node) registered in the control plane's
fleet registry. The dispatcher places sessions on nodes; it never reaches a
backend that a node does not advertise. The session engine that does the
placing lives in [orchestration](../orchestration/architecture.md); this
folder owns the node side and the registry.

## Components

| Component | Where | Owns |
|---|---|---|
| Fleet registry | `pkg/orchestration/fleet_registry*.go` | Node records, status, last-seen, backends served |
| Enrollment endpoint | `pkg/api/rest/handlers_fleet_enroll.go` | mTLS identity check, OAuth2 client minting, registry publish |
| Node daemon | `pkg/fleet`, `cmd/quicbaas/fleet_serve.go` | Enrollment, heartbeat, the `FleetRuntime` gRPC service, sandboxed spawn |
| Remote deploy | `pkg/fleet/deploy`, `pkg/service/fleet_deploy*.go`, `cmd/quicbaas/fleet_deploy*.go` | Node spec, preflight, SSH install, lifecycle |
| Embedded node | `pkg/server/embedded_fleet_node.go` | The same daemon, in the `quicbaasd` process |
| Worker image refresh | `pkg/sdlc/workerimage` | The image sandboxed sessions run; see [worker-image.md](worker-image.md) |

### The agent binary

A remote node runs `bin/quicbaas-agent` (`make build-agent`), the default
`fleet deploy --binary`. It is the `quicbaas` CLI built for linux with
`CGO_ENABLED=1`, because the node runs runc `libcontainer` in-process and
libcontainer's `nsenter` init is C. It links statically (`-linkmode external
-extldflags -static`, tags `netgo osusergo` so name and user lookup stay in Go
and never load glibc NSS), so it runs on a host without matching shared
libraries. `make build-agent-fips` is the `GOFIPS140=latest` twin
(`bin/quicbaas-agent-fips`); cgo does not touch the Go FIPS module.
`make check-agent-build` builds both and checks their build info.

`quicbaasd` is never cgo: `build-daemon` and every daemon variant stay
`CGO_ENABLED=0`, and the check fails if that changes.

The registry stores records through the injected KV store, at
`orch/agent/{tenantID}/{nodeID}`. Durability is the store's property: the
server's qrdb-backed store makes the registry durable, replicated and
encrypted at rest, and a memory store gives tests and the embedded case the
identical code. The tenant is the first variable key segment, so a point read
is confined to the caller's tenant and a list is a prefix scan of that
tenant's subtree — no key shape lets one tenant address another's node.

## Data flow

```
operator ── fleet deploy ──► SSH/SFTP ──► node host
                │                          │ installs agent.crt/.key, server-ca.crt, agent.yaml
                │                          ▼
                │                   quicbaas fleet serve
                │                          │ 1. POST /api/v1/fleet/enroll   (mTLS client cert)
                │                          │ 2. POST /oauth2/token          (client_credentials)
                │                          │ 3. POST /api/v1/fleet/{id}/heartbeat   (every 30s)
                │                          │ 4. gRPC OrchestratorService.PushEvents ──► event_ingest_addr
                ▼                          ▲
        control plane ── dispatcher ── gRPC FleetRuntime (Spawn/Prompt/Status/Close/Infer…)
```

Three connections, deliberately separate:

- **Enrollment and heartbeat** are REST, authenticated by the node's
  certificate (enrollment) and the OAuth2 token it earns (heartbeat).
- **Events** go over gRPC `PushEvents` to the `event_ingest_addr` the
  enrollment response advertises. The REST listener advertises only `http/1.1`
  in ALPN, so a daemon that streamed events to the REST host failed its
  handshake with "no application protocol". The ingest address is carried in
  the response, not guessed.
- **Work** flows the other way: the control plane dials the node's
  `FleetRuntime` service on its bind address. The node's local Ollama or ACP
  backends are never exposed; operators talk only to the control plane.

## Identity

A node's identity is its certificate, and nothing else bootstraps it. The
deploy generates a key pair on the operator's host, has the server CA sign
the CSR (the CA never sees the private key), and so obtains an X.509 client
certificate with
`OU=agents`, `O=<tenant>`, `CN=agent-<hostname>` and the `ClientAuth` EKU
(365-day default validity). The enrollment handler:

1. requires the `OU` and EKU stamps (401 `invalid_client` otherwise);
2. takes the tenant from the certificate Organization and refuses a body
   `tenant_id` that disagrees (403 `tenant_mismatch`), and cross-checks the
   tenant-scoped SPIFFE SAN;
3. registers an agent application for tenant + CN with the fleet runtime
   grant, plus a `self_signed_tls_client_auth` client pinned to the
   certificate, and grants the same authority to the certificate's identity;
4. publishes the node into the registry so the dispatcher can use it at once.

Step 3 grants twice because a node holds two credentials for one identity.
go-qrdb's authenticator chain is first-success-wins with mTLS first, so a
request carrying the client certificate is authenticated as the certificate's
subject and the bearer token is never consulted; the application grant alone
left every control-plane call refused 403 (go-quicbaas#663). The certificate
identity (`iam.IdentityStore.EnrollFleetNodeCertIdentity`) is a machine user —
CN as username, `AuthMethods: ["mtls"]`, no email login — holding a
tenant-scoped `agent-runtime` role and bound to the certificate's SPKI
thumbprint. The token's scopes are the grant on the application record
(`iam.FleetRuntimeGrant`); the daemon sends no `scope` parameter, so a grant
change reaches it on its next token refresh. A registration that cannot confer
the grant fails (500), and re-registration never re-enables an identity an
administrator disabled. The registry record is written under the record's own
tenant (`orchestration.fleetTenantContext`), because the public enroll route's
context names none.

A spawned session is not the node. It is enrolled as its own principal
(`EnrollAgentSessionIdentity`) with the narrower `agent-session` role
(`iam.AgentSessionGrant`), revoked when the session closes; see
[MCP preconfiguration](../orchestration/engineering.md#mcp-preconfiguration-for-spawned-agents).

Registration is **idempotent** on tenant + CN: a daemon that enrolls again
gets the same `client_id` and `mtls_client_id` with a rotated secret, so a
restart or a retry never leaves an orphan OAuth2 client.

## Liveness, heartbeat and recovery

A node heartbeats every 30 seconds (`--heartbeat-interval`). The heartbeat
body also carries what the node serves and its device telemetry. The registry
records the last-seen time; the control plane treats a node silent for longer
than `orchestration.heartbeat_timeout` (default 90s) as gone for session
reconciliation.

Recovery is layered so that each failure heals at the lowest layer that can:

1. **Server-side recovery.** A heartbeat for an unknown node carrying a valid
   mTLS peer certificate from a CA the control plane trusts is itself an
   authentication. The certificate's tenant and CN map deterministically to
   the node's canonical ID, so the server re-publishes the node into the
   registry from the certificate and accepts the heartbeat. The node never
   sees a 404, and it avoids a re-enrollment herd that would invalidate
   in-flight tokens.
2. **Client-side re-enrollment.** When the server cannot recover and answers
   404, or answers 403 (a missing grant), the daemon clears its cached
   credentials, enrolls again, and retries the heartbeat once. After three
   consecutive re-enrollment failures it exits with `ReregisterFailedError`
   so the supervisor restarts it.
3. **Enrollment retry.** A daemon whose control plane is not answering waits,
   with capped exponential backoff, rather than exiting.

## Container runtime

`pkg/containerruntime` is the stack's one container runtime: the
`PodmanManager` lifecycle model, attestor hooks, exec, attach and pty, and
per-container metrics, with their tests. It was moved from go-trusted-platform
`pkg/compute/podman` with its history (go-quicbaas#1349; design:
[trusted service plane §1](../design/trusted-service-plane.md)).

- **It runs only in the fleet node agent**, the `quicbaas fleet serve`
  daemon, never in `quicbaasd`. `quicbaasd` stays `CGO_ENABLED=0`, and
  `TestDaemonNeverLinksContainerRuntime` fails if the daemon links the
  package.
- **It is not wired yet.** Nothing imports it; the node daemon and the
  embedded node still launch containers through `ExecContainerRuntime` in
  `pkg/orchestration`, which is removed once the agent uses this package.
- **The exec core is replaced later.** The moved core still calls `crun`,
  `podman` and `skopeo`. A later change replaces it with runc `libcontainer`
  running in-process in the agent, with `containers/image` and
  `containers/storage`: no external runtime binaries and no sockets.

## Design decisions and rejected alternatives

**Certificate enrollment, not a bootstrap token.** A pre-shared token must be
generated, transported, stored on the node and revoked; each step is a
secret in a place it should not be. The deploy already holds the CA, so it
has the CA sign a CSR for a key generated on the CLI host and ships the identity over the SSH session that installs
the node. There is no shared-secret bootstrap at any stage.

**No attempt budget on enrollment.** A daemon that gave up after N attempts
would be restarted by systemd straight back into the same wait with the
backoff reset. Instead, failures are triaged: transient ones (connection
refused, unreachable, DNS, timeout, 502/503/504) retry forever; everything
else — 4xx, `invalid_client`, an untrusted server certificate, unusable key
material, bad configuration, and anything unrecognised — exits at once. A
silent retry on a misconfiguration would hide it. The packaged unit then
escalates its restart delay and lands in `failed` after five starts in five
minutes, which monitoring sees.

**The deploy confirms online by dialing the node, not by trusting the
registry.** A registration keeps the status of its last heartbeat after the
process behind it is gone, so a node that died when the deploy's SSH session
closed still read `running`. The deploy therefore waits for the registry
*and* for the node's runtime port to accept a connection, after the SSH
session is closed. That is why a non-zero `--wait-online` needs a fixed
numeric bind port the deploy can dial back.

**`stop`, `start` and `undeploy` take the node out of the registry.** A node
that is not running must never read `running`, so each lifecycle verb removes
the registration as part of the operation. `stop` and `undeploy` do it
*after* the process has stopped, because the other order would let a final
heartbeat re-register the node, and they refuse to run without a connection
to the control plane rather than leave a ghost record.

**Heartbeat age is not evidence at startup.** When the control plane
restarts, every registration it can read predates the restart, so every live
peer looks silent. Concluding "gone" from that would close the sessions of
workers that are still running. Absence of a registration is evidence;
staleness is not, and the maintenance sweep asks the stronger question once
the node has been up long enough for a live daemon to have spoken. An
unreadable registry means nothing is gone.

**A durable, tenant-keyed registry, not an in-memory map.** Records survive
a control-plane restart, so recovery has something to recover.

**Sandbox network defaults to `bridge`, never `host`.** An agent needs
outbound HTTPS to its provider but has no reason to reach services on the
daemon host's loopback. A spawn request has no network field, so no tenant
or workflow can move a session into the host namespace; `host` is an
operator decision recorded in configuration and logged at WARN.

**Mounts are deny-by-default, and the daemon checks again.** The `Spawn` RPC
takes `work_dir` off the wire and bind-mounts it read-write as the daemon's
uid; an unchecked `/` would be the host's root filesystem. The control plane
authorizes the same fields, but a spawn whose control-plane-resolved mode is
not `required` skips that check while the daemon's own policy may still
containerize it, and the daemon's filesystem layout is its own.

**Spec is declarative and refuses silently-wrong input.** The node spec's
`node:` section is the daemon's one config schema; the deploy fills the fields
it owns and refuses the spec if `node:` sets any of them, if capacity is
undeclared, or if the rendered config would not pass the daemon's own
validation. Nothing is installed from a config the daemon would refuse at
start-up.

**Embedded node is the same daemon over bufconn.** A host that runs both the
control plane and Ollama gets zero-hop dispatch without a second process; it
registers through the same registry rather than a parallel path.

## Naming

The node daemon and its registry are the **fleet**; a registered host is a
**fleet node**. "Agent" names only the AI agents that run on nodes, and
"nodes" alone stays the Raft cluster's word. Permission names keep the
`orchestrator:agent_*` spelling (go-qrdb's vocabulary, stamped on every
enrolled node's OAuth2 application), as do the persisted `agent_id`,
`agent_roles` and `workspace_agent_id` fields. Host-installed names
(`quicbaas-agent.service`, `/var/lib/quicbaas-agent`, `agent.crt`,
`agent.key`, `agent.yaml`) are unchanged so an installed node keeps its state
across an upgrade.

## See also

[engineering.md](engineering.md) · [cli.md](cli.md) · [api.md](api.md) ·
[user.md](user.md) · [worker-image.md](worker-image.md) ·
[sandbox](../orchestration/sandbox.md)
