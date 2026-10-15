# Agent Fleet — CLI Reference

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Every `quicbaas fleet` verb, checked against `cmd/quicbaas/fleet*.go`.
> Conflicts with this document are defects.

```
quicbaas fleet register|list|status|remove|heartbeat   registry operations
quicbaas fleet serve                                   run the node daemon
quicbaas fleet deploy|stop|start|undeploy              manage a node over SSH
```

There is no `inspect`, `events`, `update`, `delete`, `disable` or `enable`
verb. Disabling a node is REST/MCP only ([api.md](api.md#rest)). Provider
accounts live under `quicbaas account`.

The registry and deploy verbs use the global connection flags (`--address`,
`--transport`, `--tls*`) and `--tenant` (default: the `system` tenant). The registry verbs take no per-command
tenant flag. `fleet` has the persistent flag `--output json|yaml|table`.

## Registry verbs

| Verb | Flags | Notes |
|---|---|---|
| `fleet register` | `--name`, `--host`, `--type` (required), `--backend`, `--model`, `--work-dir` | Registers a node record by hand; an enrolled daemon registers itself. |
| `fleet list` | `--host`, `--status`, `--type`, `--backend`, paging flags | `--backend` matches nodes that serve the backend. |
| `fleet status <nodeID>` | | One node. |
| `fleet remove <nodeID>` | | Removes the registration. |
| `fleet heartbeat <nodeID>` | | Sends a bare heartbeat, mainly to test credentials. |

## fleet serve

Runs the node daemon: enrolls with the control plane, heartbeats, and serves
the `FleetRuntime` gRPC service on `--bind`.

```sh
quicbaas fleet serve \
  --server https://quicbaas.example.com:8443 --tenant acme \
  --node-cert agent.crt --node-key agent.key --server-ca server-ca.crt \
  --backends acp,ollama --max-concurrent-jobs 4
```

| Flag | Default | Meaning |
|---|---|---|
| `--server`, `--tenant`, `--node-cert`, `--node-key`, `--server-ca` | required | Control plane, tenant and mTLS identity. Required unless `--config` supplies them. |
| `--config` | | Daemon config file (the `node:` schema of the node spec). Precedence: flag defaults < file < flags set. |
| `--bind` | `127.0.0.1:9091` | Runtime gRPC listen address. |
| `--backends` | `acp,ollama` | Backends served; see [user.md](user.md#the-node-spec). |
| `--container-runtime` | `auto` | `auto`, `docker`, `podman` or `none`. |
| sandbox flags | | Policy, network and image options; reference in [engineering.md](engineering.md#sandbox). |
| backend URL and model flags | | Per-backend endpoints and default models. |
| `--image-map`, `--image-registry`, `--auto-pull` | | Worker image selection and pulling. |
| `--work-dir` | `/var/lib/quicbaas-agent` | Daemon state. |
| `--projects-root` | `<work-dir>/projects` | Job workspaces (`<root>/<tenant>/<project>/workspace/<key>`); must persist across restarts. |
| `--go-cache-dir` | `<work-dir>/cache/go` | Shared per-tenant Go module and build caches. |
| `--sockets-root` | | Relayed signing sockets. |
| `--allowed-mount-root` | | Extra bind-mount root; repeatable. |
| `--heartbeat-interval` | `30s` | Heartbeat period. |
| `--shutdown-grace` | `30s` | Time to drain on SIGTERM. |
| `--max-concurrent-jobs` | | Declared capacity; required by `fleet deploy`. |
| `--mcp-endpoint` | | MCP endpoint handed to spawned sessions. |
| `--enroll-retry-base`, `--enroll-retry-max` | `1s`, `1m` | Backoff bounds for transient enrollment failures. |

Enrollment retries forever on transient failures and exits 1 at once on
terminal ones ([architecture.md](architecture.md#design-decisions-and-rejected-alternatives)).
The cert and key may also come from `QUICBAAS_NODE_CERT` / `QUICBAAS_NODE_KEY`.
Credentials earned by enrollment persist as `credentials.json` under
`--work-dir`.

## fleet deploy

```sh
quicbaas fleet deploy --spec agent-01.yaml [--preflight]
```

Shared target flags (deploy, undeploy, stop, start): `--spec`, `--ssh`,
`--ssh-key-name`, `--ssh-key-scope`, `--ssh-key-owner`, `--ssh-password`,
`--ssh-insecure-host-key`, `--install-dir`, `--data-dir`, `--projects-root`.
`--install-dir` and `--data-dir` default to `~/.quicbaas/bin` and
`~/.quicbaas/agent` on the node. There is no `--tenant` flag; the tenant is
the global `--tenant` or the spec's `tenant`.

`deploy` adds:

| Flag | Default | Meaning |
|---|---|---|
| `--server` | | Control plane URL the node dials. |
| `--bind` | `0.0.0.0:9091` | Node runtime bind address. A fixed numeric port is required when `--wait-online` is non-zero. |
| `--backend` | | Backend to serve; repeatable. |
| `--binary` | `bin/quicbaas-agent` | Binary to upload. |
| `--systemd` | `auto` | `auto`, `yes` or `no` (nohup launcher). |
| `--max-concurrent-jobs` | | Capacity. |
| `--preflight` | | Run the checks only. |
| `--skip-preflight` | | Deploy without the checks. |
| `--wait-online` | `2m` | Wait for registry and port; `0` skips. |

Steps: preflight; generate an ECDSA P-256 key on the CLI host and have the
server CA sign its CSR (365-day default validity, so the CA sees only the
public key); upload binary, identity, `server-ca.crt` and rendered
`agent.yaml` (0600); install and start the `systemd --user` unit, enabling
lingering, or the nohup launcher; close SSH; wait until the registry shows
the node `running`, `idle` or `ready` **and** the runtime port accepts a
connection (polled every 2s). The preflight checks and their typed errors are
in [user.md](user.md#preflight).

`--binary` is the linux agent from `make build-agent` (cgo, statically
linked; `make build-agent-fips` for FIPS; see
[architecture.md](architecture.md#the-agent-binary)). The unit is
`~/.config/systemd/user/quicbaas-agent.service`; the nohup launcher is
`<install-dir>/start-quicbaas-agent.sh` and gets no linger check (it relies on
logind's default `KillUserProcesses=no`). The result prints the agent
certificate's serial, to match the deploy against the CA's store.

A deploy runs on the operator's side: from this CLI or the desktop app, which
calls the same `service.FleetDeployService`. The TypeScript SDK's
`orchestrator.deployFleetNode` throws `DeployNotSupportedError` on every
transport except the desktop (Wails) one.

### SSH keys and host pins

The deploy uses a stored key (`quicbaas ssh keys generate`) and a pinned
host key (`quicbaas ssh known-hosts add --host <node> --key '<authorized_keys
line>'`, or `--file` to read the line from a file). A pin stores the full host
key; its algorithm and SHA256 fingerprint are derived from it, and a presented
key is accepted only when both match. Pins are looked up under the same
scope and owner as the SSH key (`--ssh-key-scope`, `--ssh-key-owner`). A host
may have several pins, which is how its key is rotated; any one suffices.
An unpinned host stops the deploy before it dials; a key that does not match
its pin fails the handshake, and the cause should be found before re-pinning.
`--ssh-insecure-host-key` skips the pin and is for throwaway hosts only. A
spec never carries a password.

### Lifecycle

| Command | Extra flags | Effect |
|---|---|---|
| `fleet stop` | | Stop the daemon, then deregister. |
| `fleet start` | `--wait-online` | Start the installed daemon and wait for it online. |
| `fleet undeploy` | `--purge-workspaces` | Remove the install and the registration; the purge refuses `/`, the home directory or an ancestor of it (`PurgeRefusedError`). |

The node's registry ID is derived from the certificate read from the node.
Behaviour and failure types are in [user.md](user.md#lifecycle).

## Embedded node

`quicbaasd` can run the same daemon in-process; see
[engineering.md](engineering.md#embedded-fleet-node).

| | Embedded | `fleet serve` |
|---|---|---|
| Process | inside `quicbaasd` | separate host process |
| Transport | in-memory (bufconn) | gRPC over the network |
| Identity | none needed | mTLS certificate |
| Node ID | `embedded-<hostname>` | `agent-<tenant>-<certificate CN>` (CN defaults to `agent-<hostname>`) |
| Typical use | single-host install | remote GPU/CPU hosts |

## Worker image

`quicbaas sdlc worker-image status|refresh` shows and refreshes the image
sandboxed sessions run; see [worker-image.md](worker-image.md).

## See also

[user.md](user.md) · [api.md](api.md) · [engineering.md](engineering.md)
