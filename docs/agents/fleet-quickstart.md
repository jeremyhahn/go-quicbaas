# Fleet Quick Start: Deploy an Agent and Run Inference

This guide walks through the full end-to-end flow: starting the
QuicBaaS server, staging an SSH key, deploying a fleet agent that
serves a local Ollama instance, and running stateless inference
across **all five transports** (REST, gRPC, QUIC, MCP, Unix socket).
Every step is exercised through the SDK's transport abstraction, so
the same `quicbaas orchestrator inference` command works identically
regardless of which transport you select.

## 1. Start the server

```bash
quicbaasd start \
  --http-port 18080 --http-address 0.0.0.0 \
  --grpc-port 19090 \
  --unix-socket /tmp/quicbaas.sock \
  --data-dir ./data \
  --log-level info
```

The server publishes:

| Transport | Address                                  | Notes                              |
|-----------|------------------------------------------|------------------------------------|
| REST      | `https://<host>:18080`                   | TLS, `http/1.1` ALPN               |
| gRPC      | `<host>:19090`                           | TLS, `h2` ALPN                     |
| MCP       | `wss://<host>:11443`                     | TLS, JSON-RPC over WebSocket       |
| QUIC      | `<host>:10443`                           | TLS 1.3, multiplexed streams       |
| Unix      | `/tmp/quicbaas.sock`                     | gRPC over UDS, no TLS              |

Each transport carries the **same SDK contract**. Fleet inference
works identically on every one of them — the test harness in
`test/integration/cli/orchestrator_inference_test.go` exercises all
five.

## 2. Stage an SSH key (one-time, per operator)

The server's xkms-backed key store keeps deploy SSH keys mlocked
in-process. Private material never touches the operator's filesystem.

```bash
# Generate a fresh ed25519 key inside xkms.
quicbaas --address https://localhost:18080 ssh keys generate \
  --name fleet-deploy --type ed25519 --scope user

# Print the public key.
quicbaas --address https://localhost:18080 ssh keys export-public \
  --name fleet-deploy --scope user --owner "$USER"
```

Add the printed public key to `~/.ssh/authorized_keys` on every host
you intend to deploy to. The server holds the private key; only the
public half is ever copied.

## 3. Deploy an agent

```bash
quicbaas --address https://localhost:18080 agent deploy \
  --ssh user@target-host \
  --ssh-key-name fleet-deploy \
  --ssh-key-scope user \
  --server https://<server-lan-ip>:18080 \
  --backend ollama \
  --binary bin/quicbaas-static \
  --wait-online 2m
```

What happens:

1. The server's CA mints an mTLS client certificate for the agent
   (`OU=agents, O=<tenant>, EKU=ClientAuth`, 365-day default validity).
2. The static CLI binary, the agent identity (`agent.crt` /
   `agent.key`), and the server CA bundle are uploaded over SFTP to
   `~/.quicbaas/agent` on the target.
3. A systemd `--user` unit (or a `nohup` fallback) starts the daemon,
   which immediately registers via `POST /api/v1/agents/register`.
4. The register response advertises the **gRPC event-ingest URL**
   (e.g., `https://<server-lan-ip>:19090`) — the agent persists this
   into `credentials.json` and uses it for the long-lived `PushEvents`
   bidi stream. **Events never traverse the REST port.**
5. The deploy command polls the fleet registry every 2 s until the
   agent reports `running`, `idle`, or `ready`, then prints
   `Online At: <timestamp>`.

Verify:

```bash
quicbaas --address https://localhost:18080 agent list
```

The agent should appear with `STATUS=running` and a timestamp under
`LAST SEEN`.

## 4. Run inference

The agent's local Ollama is **not** exposed to the network — every
prompt is routed through the QuicBaaS server over the agent's mTLS
gRPC channel. Operators only ever talk to the control plane.

### Default (REST)

```bash
quicbaas --address https://localhost:18080 \
  orchestrator inference \
  --backend ollama \
  --model qwen3:1.7b \
  --prompt "explain QUIC in three sentences"
```

### Streaming (NDJSON over REST)

```bash
quicbaas --address https://localhost:18080 \
  orchestrator inference \
  --backend ollama --model qwen3:1.7b \
  --prompt "tell me a haiku about mTLS" \
  --stream
```

### gRPC

```bash
quicbaas --transport grpc --address localhost:19090 \
  orchestrator inference --backend ollama --model qwen3:1.7b \
  --prompt "ping"
```

### QUIC

```bash
quicbaas --transport quic --address localhost:10443 \
  orchestrator inference --backend ollama --model qwen3:1.7b \
  --prompt "ping"
```

### MCP

```bash
quicbaas --transport mcp --address localhost:11443 \
  orchestrator inference --backend ollama --model qwen3:1.7b \
  --prompt "ping"
```

### Unix socket

```bash
quicbaas --transport unix --address /tmp/quicbaas.sock \
  orchestrator inference --backend ollama --model qwen3:1.7b \
  --prompt "ping"
```

## 5. Use the GUI

Open the QuicBaaS web UI (or the Wails desktop build) and navigate to
**Orchestration → Inference**. The view loads the registered backends
via `GET /api/v1/orchestrator/backends`, lists the deployed agents
serving the chosen backend, and lets you submit prompts with
streaming on or off. Session-only backends (`claude`, `codex`,
`opencode`) appear in the dropdown but are disabled — for those, use
the **Sessions** view instead.

The GUI keeps a per-session history of the last 10 inference calls in
a collapsible card so you can re-read recent results without leaving
the view.

## 6. What's actually happening on the wire

```
operator CLI / GUI
  └─ SDK transport (REST | gRPC | QUIC | MCP | Unix)
       └─ QuicBaaS server (orchestrator service)
            └─ RemoteACPBackend → mTLS gRPC :19090
                 └─ agent runtime (target host)
                      └─ OllamaBackend → http://127.0.0.1:11434
```

- The agent's local Ollama port (`11434`) is bound to `127.0.0.1`
  only — it has **no** external listener.
- The agent's only outbound connection is the mTLS gRPC stream to
  the control plane's gRPC ingest URL.
- The control plane's REST listener never receives gRPC traffic, so the
  REST listener can keep advertising only `http/1.1` ALPN without
  breaking anything.

## Troubleshooting

| Symptom | Resolution |
|---------|------------|
| Deploy times out at "Wait online" | Verify `--server` is reachable from the target host (LAN IP, not `localhost`). Check `~/.quicbaas/agent/quicbaas-agent.log` on the target. |
| Agent log shows `tls: no application protocol` | The agent is dialing the REST port for events. Pull the latest CLI; the deploy now embeds the gRPC ingest URL in the register response. |
| `agent list` is empty after deploy | The agent registered but failed its first heartbeat. Check the agent's clock skew vs. the server (>5 min skew breaks JWT validation). |
| `inference` returns 422 | The chosen backend is session-only. Use `quicbaas orchestrator session spawn` then `prompt` instead. |
| `inference` returns "no online agents" | No agent serves the chosen backend in the chosen tenant. Run `agent list` and confirm at least one agent has `backend=ollama`. |
| Streaming hangs in the browser | Network proxy buffering NDJSON. Set `Cache-Control: no-cache` on the proxy or run the GUI directly against the daemon. |

## Related docs

- [Inference reference](inference.md) — full RPC, REST, SDK, GUI reference.
- [Agent deploy reference](deploy.md) — every flag, every step.
- [Daemon design](daemon.md) — agent runtime, registration, event forwarding.
- [GUI: Desktop & Web](../gui/desktop-and-web.md) — Wails desktop and browser UI.
- [API](../api/) — REST / gRPC / QUIC / MCP protocol details.
