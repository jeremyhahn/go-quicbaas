# quicbaas agent

Operate against the fleet agent layer: deploy daemons to remote
hosts, list registered daemons, inspect agent identity and
capabilities, and roll out updates.

The agent's local Ollama / ACP backends are **never** exposed to the
network — operators only ever talk to the central QuicBaaS server,
which routes prompts and inference calls over the agent's mTLS
gRPC channel.

## Subcommands

| Command | Description |
|---------|-------------|
| `deploy` | Provision a new agent on a remote host over SSH (full mTLS bootstrap) |
| `list` | List registered agent daemons (alias for `orchestrator agents list`) |
| `inspect <id>` | Show identity, certificate serial, last heartbeat, served backends |
| `events <id>` | Stream the agent's event log live |
| `update <id>` | Push a new static CLI binary and restart the daemon |
| `delete <id>` | Deregister and tear down a remote daemon |

## quicbaas agent deploy

Provisions a QuicBaaS agent daemon on a remote host. See the
[deploy reference](../agents/deploy.md) for the full step-by-step
breakdown of what this command does on the wire.

### Flags

| Flag | Description |
|------|-------------|
| `--ssh user@host[:port]` | SSH target (required) |
| `--ssh-key-name <name>` | xkms-stored SSH key (required) |
| `--ssh-key-scope user\|tenant\|service` | Defaults to `user` |
| `--ssh-key-owner <id>` | Defaults to `$USER` for `user` scope |
| `--ssh-insecure-host-key` | Skip remote host-key verification (operator opt-in) |
| `--server https://host:port` | URL the agent will dial (required) — must be LAN-routable from the target |
| `--tenant <id>` | Tenant the agent serves; defaults to `default` |
| `--backend <kind>` (repeatable) | Backends to enable (`ollama`, `acp`) |
| `--binary <path>` | Local path to the static CLI (default `bin/quicbaas-static`) |
| `--install-dir <dir>` | Remote install dir (default `~/.quicbaas/bin`) |
| `--data-dir <dir>` | Remote data dir (default `~/.quicbaas/agent`) |
| `--bind <addr:port>` | Local bind on the remote host (default `0.0.0.0:9091`) |
| `--systemd auto\|yes\|no` | Supervisor selection |
| `--wait-online <duration>` | Max time to wait for online status (default `2m`) |

### Example

```bash
# 1. Stage an SSH key in xkms (one-time per operator).
quicbaas ssh keys generate --name fleet-deploy --type ed25519 --scope user
quicbaas ssh keys export-public --name fleet-deploy --scope user --owner "$USER"
# → copy the printed public key to ~/.ssh/authorized_keys on the target.

# 2. Deploy.
quicbaas agent deploy \
  --ssh jhahn@gpu-host \
  --ssh-key-name fleet-deploy \
  --server https://198.51.100.52:18080 \
  --backend ollama \
  --binary bin/quicbaas-static \
  --wait-online 2m
```

The deploy ends with `Online At: <timestamp>` once the agent is
visible in the fleet registry.

## What the agent does on first start

1. **Loads identity** — reads its mTLS keypair from `~/.quicbaas/agent/agent.{crt,key}` and the server CA from `server-ca.crt`.
2. **Registers** — `POST /api/v1/agents/register` with the client cert. The server returns OAuth2 credentials AND the **gRPC event-ingest URL** (e.g., `https://server-host:19090`). Both are persisted to `credentials.json`.
3. **Opens the event-forwarding gRPC stream** — dials the gRPC ingest URL learned at registration, NOT the REST URL. This guarantees `h2` ALPN succeeds without dual-publishing gRPC behind the REST listener.
4. **Heartbeats** — periodic OAuth2 token refresh + status update.
5. **Serves prompts/inference** — exposes the `proto.AgentRuntime` gRPC service on its bind address (`0.0.0.0:9091` by default) for the central to dial.

The agent has **no** outbound listener for its embedded Ollama; that
process binds to `127.0.0.1:11434` only.

## Embedded agent vs. standalone daemon

`quicbaas agent serve` starts a **standalone** agent daemon — a separate process
that connects to a remote quicbaasd over the network using mTLS.

`quicbaasd --embedded-agent` starts an **embedded** agent inside the quicbaasd
process itself. It registers through the same fleet registry as standalone daemons
but communicates via an in-process bufconn transport (no network hop). Use this
when the orchestrator host itself has Ollama and you want zero-latency inference
without deploying a separate process.

See [embedded agent mode](../agents/daemon.md#embedded-agent-mode) for full details.

## See also

- [Fleet quick start](../agents/fleet-quickstart.md) — full E2E walkthrough
- [Daemon design](../agents/daemon.md) — agent runtime internals
- [Deploy reference](../agents/deploy.md) — every flag, every step
- [Inference reference](../agents/inference.md) — what `agent deploy --backend ollama` enables
