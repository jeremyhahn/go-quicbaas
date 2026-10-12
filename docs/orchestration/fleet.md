# Fleet nodes — adding, checking and removing a node

> **Status: NORMATIVE** · Last verified: 2026-09-25
> Supplement to the Software Factory feature folder: the runbook for a
> fleet node's whole life — declare it, preflight it, deploy it, stop,
> start and remove it. Conflicts with this document are defects.

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

`fleet deploy --preflight` runs every check and changes nothing. `agent
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
runtime port answers. The details are in [cli.md](cli.md#remote-fleet-node-deploy).

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
2. Pin the node's host key: read the fingerprint **on the node**
   (`ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub`), then
   `quicbaas ssh known-hosts add --host <node> --algorithm ssh-ed25519 --fingerprint <SHA256:...>`.
3. Copy `deployments/fleet/example.yaml` into the fleet's infrastructure
   repository as `<node>.yaml` and fill it in — capacity included.
4. `quicbaas fleet deploy --spec <node>.yaml --preflight`; apply each fix
   it prints until it reports `ready to deploy`.
5. `quicbaas fleet deploy --spec <node>.yaml`.
6. `quicbaas fleet list` shows the node online.

## Device telemetry

Each heartbeat also carries the node's accelerator telemetry — NVIDIA GPU
power, utilisation and memory (nvidia-smi), Intel NPU busy fraction (intel_vpu
sysfs) and CPU package power (RAPL) — which the control plane files as
per-node time series and integrates to cost local turns from measured energy.
See [../accounts/architecture.md](../accounts/architecture.md#measured-energy-from-fleet-telemetry).

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
| gRPC | `OrchestratorService/{Register,Get,List,Remove}FleetNode`, `FleetNodeHeartbeat`; the node's own service is `quicbaas.fleet.runtime.v1.FleetRuntime` |
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
