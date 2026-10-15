# Agent Fleet — User Guide

> **Status: NORMATIVE** · Last verified: 2026-10-13
> The runbook for a fleet node's whole life — declare it, preflight it,
> deploy it, check it, stop, start and remove it, and fix it when it will not
> come online. Conflicts with this document are defects.

A fleet node is a host running `quicbaas fleet serve`, registered in the
control plane's fleet registry, that the dispatcher places sessions on.
Adding one is a **declared, pre-flighted, reversible** operation:

| Step | Command | Changes the node? |
|---|---|---|
| Declare | write a node spec | no |
| Check | `quicbaas fleet deploy --spec <file> --preflight` | no |
| Deploy | `quicbaas fleet deploy --spec <file>` | yes (runs the check first) |
| Stop / start | `quicbaas fleet stop` / `fleet start --spec <file>` | stops / starts the node daemon |
| Remove | `quicbaas fleet undeploy --spec <file>` | removes the install |

Every command talks to the node over SSH with a stored key and a pinned
host key, and to the control plane with the CLI's own connection.

## Quick start

1. **Start the server.** TLS is on by default, so a fresh data directory
   brings up the CA and first-boot. Set `iam.spiffe_trust_domain` first (for
   example `quicbaas.example.com`): the CA stamps the tenant SPIFFE SAN only
   when a trust domain is configured, and the daemon fails closed without one
   ([bootstrap](../bootstrap/cli.md#prerequisite-iamspiffe_trust_domain-required)).

   ```sh
   quicbaasd start --http-port 18080 --http-address 0.0.0.0 \
     --grpc-port 19090 --data-dir ./data
   ```

2. **Stage an SSH key** (once per operator). The key lives in the server's
   xkms-backed store; only the public half leaves it.

   ```sh
   quicbaas ssh keys generate --name fleet-deploy --type ed25519 --scope user
   quicbaas ssh keys export-public --name fleet-deploy --scope user --owner "$USER"
   ```

   Append the printed key to `~/.ssh/authorized_keys` on the target.

3. **Deploy** (after pinning the host key; see
   [Adding a node](#adding-a-node-end-to-end)):

   ```sh
   quicbaas fleet deploy --ssh user@target-host --ssh-key-name fleet-deploy \
     --ssh-key-scope user --server https://<server-lan-ip>:18080 \
     --backend ollama --binary bin/quicbaas-agent --wait-online 2m
   ```

   The CA signs a client certificate for the node (`OU=agents`,
   `O=<tenant>`, ClientAuth), the CLI uploads the binary, identity and CA
   bundle over SFTP, and the node's unit starts and enrolls. The enrollment
   response advertises the **gRPC event-ingest URL**, which the node keeps in
   `credentials.json`; events never traverse the REST port. A `--user` unit
   needs lingering, or the node stops when the SSH session closes.

4. **Verify** with `quicbaas fleet list`: the node shows `running` with a
   recent `LAST SEEN`. To run inference through it, see
   [Fleet-Routed Inference](../orchestration/user.md#fleet-routed-stateless-inference--embeddings).

On the wire:

```
operator CLI / GUI
  └─ SDK transport (REST | gRPC | QUIC | MCP | Unix)
       └─ QuicBaaS server (orchestrator service)
            └─ mTLS gRPC to the node's FleetRuntime
                 └─ node daemon ─► backend (e.g. Ollama on 127.0.0.1:11434)
```

The node's backend ports are bound to loopback with no external listener;
the control plane is the only caller.

## The node spec

One YAML file per node. `deployments/fleet/example.yaml` is the annotated
template. This repository is published, so a real fleet's specs — host
names, addresses, users — belong in that fleet's private infrastructure
repository ([public-releasability](../release/public-releasability.md)).

| Section | What it holds |
|---|---|
| `host`, `ssh_user`, `ssh_key_name` (`ssh_key_scope`, `ssh_key_owner`) | How to reach the node. A spec never carries a password. |
| `server`, `tenant` | The control plane the node dials, and the tenant it serves. |
| `node_bind`, `backends`, `binary`, `install_dir`, `data_dir`, `systemd`, `wait_online` | What the deploy installs and how. |
| `projects_root` | Where job workspaces live on the node. |
| `worker_images` | The images the node's sandboxed sessions run. |
| `node:` | The daemon's configuration — exactly the `fleet serve --config` schema (`fleet.Config`). |

The `node:` section is the one schema for every daemon setting: capacity,
sandbox policy and network, container runtime, image map and registry,
auto-pull, backend URLs (`ollama_base_url`, `vllm_base_url`,
`openvino_base_url`, …), mount roots. The deploy renders it to
`<install_dir>/agent.yaml` (0600) and the unit runs
`quicbaas fleet serve --config <install_dir>/agent.yaml`.

The deploy fills in what it owns — `server_addr`, `tenant_id`,
`bind_addr`, `backends`, `work_dir`, `projects_root`, the identity file
paths and `mcp_endpoint` — from the spec's top-level fields. Writing any of
those inside `node:` is refused (`DeployOwnedFieldError`) rather than
silently overwritten.

**Backends are selected one by one.** `backends` (and `fleet serve
--backends`) names each backend the node serves: `claude`, `codex`,
`opencode`, `openvino`, `ollama-agent`, `vllm`, `ollama`. `acp` is the group
of every agent backend — the three CLIs, `ollama-agent`, and `openvino` /
`vllm` when their URL is set — and empty means `acp` plus `ollama`. The node
advertises exactly what it serves, and placement routes by that, so a GPU
node that names `[opencode, openvino, ollama]` is never handed claude or
codex work. An unknown name (`ErrConfigUnknownBackend`) and `openvino` or
`vllm` named with no server (`ErrConfigBackendEndpointRequired`) are refused
before anything is installed.

When the node serves `ollama`, its `opencode` backend lists that ollama's
models as `ollama/<name>` beside the curated hosted ones, read from ollama
when the control plane asks (the backend models endpoint). An opencode spawn
on an `ollama/` model needs no pooled provider account.

**Capacity is required.** A spec without `node.max_concurrent_jobs`
(or `--max-concurrent-jobs`) is refused with `CapacityUndeclaredError`: a
node that declares no capacity leaves the whole fleet's capacity
undeclared, and admission cannot bound the fleet at all.

The rendered config is validated with the daemon's own rules before
anything is installed, so a config the daemon would refuse at start-up
(`AgentConfigInvalidError`) never reaches the node. An unknown key in the
spec is an error, not a silently ignored setting.

**Precedence.** Flag defaults < the spec file < flags set on the command
line. `--spec node.yaml --max-concurrent-jobs 2` deploys the spec with a
capacity of 2; a flag left at its default never overrides the spec. The
same rule applies to `fleet serve --config`.

## The address the control plane dials

The control plane reaches a node at one address: the dispatcher dials it for
every spawn, prompt and model listing. By default that address is derived
from the node's `bind_addr`, with a wildcard host (`0.0.0.0`, `[::]`)
replaced by the certificate or host name. When that is not how the control
plane reaches the node (NAT, a published port, a name only the control
plane resolves), advertise the address explicitly:

| Source | Setting |
|---|---|
| flag | `quicbaas fleet serve --advertise <host:port>` |
| environment | `QUICBAAS_ADVERTISE_ADDR=<host:port>` |
| config file (`node:` in a spec) | `advertise_addr: <host:port>` |

Precedence is defaults < config file < environment < flags set on the
command line. The value must be a `host:port` with a port in 1–65535 and a
host that is not a wildcard; anything else is refused at start-up with
`ErrConfigInvalidAdvertiseAddr`.

**Registration refuses an address it cannot reach.** The node binds its
listener before it enrolls, and the control plane resolves and dials the
address being registered (advertised or derived) before recording it. An
address that does not resolve or does not accept a connection is refused
with `422 agent_address_unreachable` (`AgentAddressUnreachableError`, naming
the address and whether parsing, resolution or the dial failed), and nothing
is registered.

**Co-location in a cluster needs a routable advertise.** An agent on the same
host as quicbaasd can enroll with a loopback address (`127.0.0.1`, `::1`,
`localhost`) when the control plane is a single node: that node is the only
one that dials it. On a clustered control plane the other nodes cannot dial
another host's loopback, so registration refuses it before the probe with
`422 agent_address_loopback_in_cluster` (`AgentAddressLoopbackInClusterError`,
naming the address). This applies to a derived address too, such as one from
`--bind 127.0.0.1:9091`. Set `--advertise`, `QUICBAAS_ADVERTISE_ADDR` or
`advertise_addr` to a host every node can dial; that address then goes
through the reachability probe like any other.

```
fleet node address "127.0.0.1:9091" is loopback, which the other nodes of a
clustered control plane cannot dial: set the agent's --advertise
(advertise_addr, QUICBAAS_ADVERTISE_ADDR) to a host every node can dial
```

The fleet node record carries what registration learned:

| Field | Meaning |
|---|---|
| `advertised_address` | The address the node advertised, when it named one. |
| `reachability` | `reachable` when the control plane dialed it at registration; `unverified` when it was not probed (a re-registration from a heartbeat). |

**A model listing names the node it could not reach.** `GET
/api/v1/fleet/{nodeID}/backends/{backend}/models` answers `503` when the
node's daemon cannot be dialed or does not answer in time, and `502` when it
answers with a server-side failure. The error names the backend and the
daemon address. The web console's Inference page shows that message, and
opens on the first stateless backend whose models list.

## Preflight

`fleet deploy --preflight` runs every check and changes nothing. `fleet
deploy` runs the same checks first and stops, with every failure at once,
when any fails; `--skip-preflight` deploys without them. Each failure is a
typed error that carries its fix — the command or edit that resolves it.

| Check | Fails with | Fix it names |
|---|---|---|
| ssh connection | `SSHDialError` | reachability of host and port |
| pinned host key | `SSHHostKeyNotPinnedError`, `SSHHostKeyMismatchError`, `SSHHostKeyAlgorithmMismatchError` | the `quicbaas ssh known-hosts add` command; an algorithm mismatch (the node presented, say, its ECDSA key while only its ed25519 key is pinned — Go's SSH client negotiates ECDSA first) is named as such |
| deploy key authorized | `SSHKeyNotAuthorizedError` | the exact `authorized_keys` line to append |
| systemd --user | `SystemdUserUnavailableError` (only when the spec requires systemd; `auto` falls back to nohup) | run under a user with a user manager, or `--systemd no` |
| linger | `LingerNotEnabledError` (passes when `sudo -n` can enable it) | `sudo loginctl enable-linger <user>` |
| container runtime | `ContainerRuntimeUnavailableError` | install it, start it, or `sudo usermod -aG docker <user>` |
| disk for worker images / workspaces | `InsufficientDiskError` (under 20 GiB / 10 GiB free) | free space or move `projects_root` |
| worker image | `WorkerImageUnavailableError`, `WorkerImageUIDMismatchError` | pull or build it; rebuild with `--build-arg AGENT_UID=<uid> --build-arg AGENT_GID=<gid>` |
| control plane reachable from node | `ControlPlaneUnreachableError` | open the route, or use the name on the server's certificate |

Two checks are worth knowing in detail:

- **Worker image uid.** Sandboxed sessions run as the daemon's uid. An image
  built for another uid has no passwd entry for it, and every signed commit
  in the container fails ("No user exists for uid"). Preflight compares the
  image's `USER` with `id -u` on the node. An image that is only pullable is
  reported as such; its uid cannot be read without pulling it.
- **Control plane.** The TLS connection is opened *from the node*, through
  the SSH connection (a direct-tcpip channel), so the node's routing and DNS
  are what is tested. The server's certificate is verified against the CA
  the deploy installs as `server-ca.crt`. Verification is never skipped.

```
$ quicbaas fleet deploy --spec agent-01.yaml --preflight
Preflight:  agent-01.internal
  ok        ssh connection                     agent@agent-01.internal
  ok        pinned host key                    verified against its pin in the ssh known-hosts store
  ...
  FAIL      linger                             deploy: systemd --user lingering is not enabled for agent; ...
            fix:                               sudo loginctl enable-linger agent
Result:     NOT ready to deploy
```

## Deploy

```sh
quicbaas fleet deploy --spec agent-01.yaml
```

Preflight, then: mint the agent's mTLS identity, upload the binary, the
identity and the rendered `agent.yaml`, install and start the systemd
`--user` unit (enabling lingering) or the nohup launcher, close the SSH
session, and wait until the fleet registry shows the agent online *and* its
runtime port answers. The details are in [cli.md](cli.md#fleet-deploy).

## Lifecycle

| Command | What it does |
|---|---|
| `fleet stop --spec <file>` | Stops the agent (unit or nohup process), then removes it from the fleet registry. A systemd unit stays enabled, so the agent comes back (and re-registers) at the next boot; use `undeploy` to take the node out for good. |
| `fleet start --spec <file>` | Starts the installed agent (ensuring lingering for a unit), then waits for it online unless `--wait-online 0`. It re-registers on its own. |
| `fleet undeploy --spec <file>` | Stops and disables the agent, removes the unit, `agent.yaml`, binary, start script, identity and persisted credentials, then removes it from the fleet registry. `--purge-workspaces` also removes the projects root and work directory, and refuses (`PurgeRefusedError`) a projects root that is `/`, the home directory or an ancestor of it. |

**A stopped node never reads `running`.** The registry keeps the status of
an agent's last heartbeat, so a dead agent used to stay `running` until
someone removed it by hand. `stop` and `undeploy` deregister as part of the
command, and do it *after* the process has stopped — the other order would
let a final heartbeat re-register it. They refuse to run without a
connection to the control plane (`FleetRegistryUnavailableError`); if the
removal itself fails, `FleetNodeDeregisterError` names the
`quicbaas fleet remove` command to finish it.

The agent's registry ID is derived from the certificate the deploy
installed, read from the node — so the commands remove the registration
that is this node's, not one named by hand. `undeploy` on a node with no
certificate still removes what is there and says it had no registration to
remove.

How the agent was installed is read from the node too: a unit file means
systemd, a start script means nohup.

## Adding a node, end to end

1. Stage the deploy key once per operator:
   `quicbaas ssh keys generate --name fleet-deploy --type ed25519 --scope user`, and
   append its public key to the node user's `~/.ssh/authorized_keys`.
2. Pin the node's host key: copy its public host key **from the node**
   (`/etc/ssh/ssh_host_ed25519_key.pub`), then
   `quicbaas ssh known-hosts add --host <node> --file <node>.pub` (or
   `--key '<authorized_keys line>'`). See
   [cli.md](cli.md#ssh-keys-and-host-pins).
3. Copy `deployments/fleet/example.yaml` into the fleet's infrastructure
   repository as `<node>.yaml` and fill it in — capacity included.
4. `quicbaas fleet deploy --spec <node>.yaml --preflight`; apply each fix
   it prints until it reports `ready to deploy`.
5. `quicbaas fleet deploy --spec <node>.yaml`.
6. `quicbaas fleet list` shows the node online.

## Checking a node

`quicbaas fleet list` shows every node of the tenant (`--status`, `--host`,
`--type`, `--backend` filter it); `quicbaas fleet status <node-id>` shows one.
A node's status is one of `registered`, `running`, `idle`, `offline`, `error`
or `disabled`. A node heartbeats every 30 seconds; one silent for longer than
the heartbeat timeout (90 seconds by default,
`QUICBAAS_ORCHESTRATION_HEARTBEAT_TIMEOUT`) is treated as gone when the
control plane reconciles sessions.

A node that the control plane forgot (a restart, a lost record) heals itself:
its next heartbeat re-publishes it from its certificate, or, failing that,
the daemon enrolls again. You do not re-run the deploy for that. See
[architecture.md](architecture.md#liveness-heartbeat-and-recovery).

To keep a node registered but stop it receiving work, disable it:
`POST /api/v1/fleet/{nodeID}/disable?tenant_id=<tenant>` (and `/enable`).
Disabling also revokes the node's OAuth2 client and certificate pin. There is
no CLI verb for it; see [api.md](api.md#rest).

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `fleet deploy` fails preflight | Each failure prints its fix; apply them and re-run `--preflight`. |
| Deploy times out waiting online | The node's daemon did not enroll or its runtime port is not reachable from the CLI host. Read the daemon log (`journalctl --user -u quicbaas-agent`), and check `node_bind` is a fixed numeric port. |
| Daemon log: `waiting to enroll` | The control plane is unreachable (refused, DNS, timeout, 502/503/504). The daemon retries forever with capped backoff; fix the route or the `server` address. |
| Daemon exits 1 at start-up | A terminal enrollment failure: a rejected certificate (`invalid_client`), a `tenant_mismatch`, an untrusted server certificate, or bad config. The unit lands in `failed` after five starts in five minutes. |
| Daemon log: `tls: no application protocol` | The node is dialing the REST port for events. Use a current CLI; enrollment now returns the gRPC ingest URL. |
| `fleet list` is empty after deploy | The node enrolled but failed its first heartbeat. Check its clock against the server's; more than 5 minutes of skew breaks JWT validation. |
| Enrollment returns 403 `tenant_mismatch` | The spec `tenant` and the certificate's tenant differ. Re-deploy so the identity is minted for the right tenant. |
| Node does not come back after a reboot or logout | Lingering is off, so the `--user` unit stopped with the session. `sudo loginctl enable-linger <user>`, then `fleet start`. |
| Sandboxed spawn fails | The container runtime is missing, the worker image is absent, or the project directory is outside the allowed mount roots. See [engineering.md](engineering.md#bind-mount-allowlist). |
| Node stays `running` after it died | It is not removed by the heartbeat alone. Use `fleet stop` or `fleet undeploy`, or `fleet remove <node-id>`. |

## Device telemetry

Each heartbeat also carries the node's accelerator telemetry — NVIDIA GPU
power, utilisation and memory (nvidia-smi), Intel NPU busy fraction (intel_vpu
sysfs) and CPU package power (RAPL) — which the control plane files as
per-node time series and integrates to cost local turns from measured energy.
See [../accounts/architecture.md](../accounts/architecture.md#measured-energy-from-fleet-telemetry).

## Host metrics and fleet health

Each heartbeat also carries a **host sample** (`host_metrics`), taken by the
daemon just before it sends the heartbeat:

| Field | Source | Provenance |
|---|---|---|
| `collected_at` | the daemon's clock when it took the sample (required) | — |
| `cpu_utilization` | busy share of `/proc/stat` CPU time since the previous sample, in [0, 1] | derived; unread on the first sample |
| `cpu_count` | online CPUs | measured |
| `load1`, `load5`, `load15` | `/proc/loadavg` | measured |
| `mem_total_bytes`, `mem_available_bytes` | `/proc/meminfo` (`MemAvailable`, not `MemFree`) | measured |
| `disk_total_bytes`, `disk_available_bytes` | statfs on the daemon's work directory | measured |

Every value is a reading, `{"value": <number>, "provenance": "measured" | "derived"}`.
A value the daemon could not read is sent as `{"provenance": "unknown"}` and
decodes as **unread, never as zero**. A reading with no value, an unknown or
unrecognised provenance (`estimated` is not telemetry), or NaN is also unread.

**Presence.** The observation is the `backends` key plus everything beside
it. A heartbeat with no `backends` key reconciles nothing, and a bare
heartbeat (no body) only refreshes liveness. `devices`, `signing_agents` and
`images` keep the distinction between absent and empty: an absent list
leaves the stored value alone, while an empty list reports "none". gRPC
carries that distinction in `*_reported` flags on `FleetNodeObservation`,
because proto3 cannot tell an empty repeated field from an unset one. Every
transport — REST, gRPC, QUIC, MCP, Unix and embedded — delivers the same
observation. `sdk/go/transport/internal/fleetparity` holds the test that
pins this: each transport must deliver exactly what the REST body decodes to.

**Health is fail-safe.** `FleetHealthJudge` (`pkg/orchestration`) judges a
node from its last sample and gives one reason:

| Reason | When |
|---|---|
| `unmeasured` | no sample, or a reading a check needs is unread |
| `invalid_sample` | the sample fails validation (no `collected_at`, a reading out of range) |
| `stale` | the sample is older than the fleet heartbeat timeout |
| `memory_exhausted` | under 2 GiB available, or over 90 % used |
| `cpu_saturated` | over 90 % CPU utilisation |
| `disk_exhausted` | under 5 GiB available, or over 95 % used |
| `ok` | none of the above |

A node is healthy only when the reason is `ok`. A node that cannot be
measured is never presumed healthy. The sample's age is the **older** of
two ages: the time since the node's last heartbeat (the control plane's
clock) and the time since `collected_at` (the node's clock). A node whose
clock runs ahead therefore cannot keep a dead sample fresh, and a sample
that arrives late is not aged from its arrival.

Health is computed and exposed (`JudgeFleetHealth`).

### Admission

Placing a spawn admits the node through `FleetAdmission`
(`pkg/orchestration`), which applies three gates in order and refuses at
the first that fails:

| Gate | Refusal reason | Rule |
|---|---|---|
| health | `unhealthy` | the node's health verdict is not `ok` — unmeasured, stale and invalid samples included |
| declared width | `at_declared_width` | the node already runs `max_concurrent_jobs` jobs; an idle sample never lifts this bound (0 means unreported, not zero slots) |
| measured room | `no_measured_room` | the sample leaves no room for another job, after the spawns placed since that sample |

**Measured room** is how many more jobs fit under the health ceilings:
the lesser of the spare CPU below 90 % divided by a job's cores, and the
memory available above the health floor divided by a job's memory. Any
reading it needs that is unread gives no room. The sample cannot show the
spawns placed after it was taken, so placement counts those against the room
until the next sample arrives.

**The job profile** — what one job costs — starts at 1 core and 2 GiB. On
each placement it is recalibrated from the healthy, fully measured nodes
that run jobs: their CPU and memory in use divided by their live jobs. A
recalibration needs at least 4 measured jobs; with fewer, the profile is
kept. A host's baseline use is attributed to its jobs, which errs towards
admitting less.

**A pinned spawn** — one continuing a job on the node it started on — is
judged on health only: the job already holds its slot.

**The embedded node** is measured by the control plane's own host, sampled
where it works (`server.embedded_fleet_node.data_dir`, else the storage data
directory). Where the host cannot be read, the embedded node is unmeasured
and refused like any other.

**The refusal** is `FleetCapacityRefusedError`, which names each node and
why it was refused and unwraps to `ErrFleetCapacityRefused`. It is
classified with the exhausted agent pool, so REST answers 503 and gRPC
`ResourceExhausted`. A spawn the fleet refused never falls through to a
local backend.

## Names

The node daemon and its registry are called the **fleet** everywhere; a
registered host is a **fleet node**. "Agent" names only the AI agents that run
on fleet nodes, and "nodes" alone stays the Raft cluster's word. There are no
aliases for the retired names.

| Surface | Name |
|---|---|
| CLI | `quicbaas fleet register\|list\|status\|remove\|heartbeat\|serve\|deploy\|undeploy\|stop\|start`; provider accounts moved to `quicbaas account` |
| `fleet serve` identity flags | `--node-cert`, `--node-key` (env `QUICBAAS_NODE_CERT`, `QUICBAAS_NODE_KEY` in the packaged unit) |
| Daemon config (`fleet.Config`) | `node_cert_file`, `node_key_file` |
| Node spec | `node_bind`, and the `node:` section |
| REST | `/api/v1/fleet`, `/api/v1/fleet/{nodeID}[/heartbeat\|disable\|enable\|backends/{backend}/models]`; the mTLS enrollment is `POST /api/v1/fleet/enroll` |
| gRPC | `OrchestratorService/{Register,Get,List,Remove}FleetNode`, `FleetNodeHeartbeat`; the node's own service is `quicbaas.fleet.runtime.v1.FleetRuntime`; events ingest on `OrchestratorService/PushEvents` |
| MCP | `quicbaas_fleet_{register,list,status,remove,heartbeat,disable,enable}`, `project_grant_fleet_node`, `project_revoke_fleet_node` |
| quicbaasd | `server.embedded_fleet_node` and `--embedded-fleet-node*` |
| UI | **Fleet** at `#/fleet`, a node at `#/fleet/{id}` |

Two things deliberately keep their old spelling until stored data is
migrated: the permission names (`orchestrator:agent_*`, which are go-qrdb's
canonical vocabulary and are stamped on every enrolled node's OAuth2
application) and the persisted record fields `agent_id` / `agent_roles` /
`workspace_agent_id` on projects, jobs, tasks and sessions. Host-installed
names — the `quicbaas-agent.service` unit, `/var/lib/quicbaas-agent`,
`agent.crt`, `agent.key` and `agent.yaml` — are unchanged so an installed
node keeps its state across the upgrade.
