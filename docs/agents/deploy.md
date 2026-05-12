# Remote Agent Deploy

`quicbaas agent deploy` provisions a QuicBaaS agent daemon on a remote
host over SSH. It mints an mTLS client certificate for the daemon via
the server's embedded CA, uploads the static CLI and identity material
over SFTP, installs a systemd `--user` unit (or a nohup fallback), and
waits for the agent to appear online in the fleet registry.

The same workflow is available from the Wails desktop GUI
(`Orchestrator → Agents → Deploy Remote Agent`) and the Go SDK
(`AgentDeployService`). Every transport other than Wails rejects the
TypeScript SDK `orchestrator.deployAgent` call with
`DeployNotSupportedError` — deploys are operator-side and must be run
from the CLI or the desktop app.

## Prerequisites

1. **Static CLI binary** — produced by `make build-static-cli`. The
   default path the CLI expects is `bin/quicbaas-static`.
2. **SSH access** to the remote host. SSH private key stored
   in the xkms-backed key store (preferred).
3. **Run from the QuicBaaS server host** so the embedded CA can mint
   the agent identity. Operators do not stage any pre-shared secret.

## Identity bootstrap (mTLS)

The deploy flow issues a fresh X.509 client certificate for every
deployed daemon. The certificate:

- carries `OU=agents`, `O=<tenant>`, `CN=agent-<hostname>` by default;
- includes the `ClientAuth` extended key usage stamp;
- is signed by the QuicBaaS server's CA;
- ships with the server's CA bundle so the daemon can verify the
  server on every TLS handshake.

Both the certificate and private key are uploaded to the remote
host's data directory (`~/.quicbaas/agent/agent.crt`,
`~/.quicbaas/agent/agent.key`, 0600) along with the server CA bundle
(`~/.quicbaas/agent/server-ca.crt`, 0644). The deploy process wipes
the private-key buffer on the operator's host immediately after the
upload completes; there is no persisted copy of the key material on
the operator's machine.

## Run the deploy

```sh
quicbaas agent deploy \
  --ssh alice@node01 \
  --ssh-key-name alice-laptop \
  --server https://quicbaas.example.com:8443 \
  --tenant acme \
  --backend acp --backend ollama \
  --binary bin/quicbaas-static
```

The deploy executes the following steps in order:

1. **Issue agent certificate** — requests a fresh mTLS identity from
   the server's CA with `OU=agents, O=<tenant>, EKU=ClientAuth` and
   365-day default validity. Fails fast if the CA rejects the
   request — the remote host is never touched.
2. **SSH dial** — establishes a session to `alice@node01` using the
   stored key named `alice-laptop`.
3. **SFTP upload** — streams `bin/quicbaas-static` to
   `$HOME/.quicbaas/bin/quicbaas` and installs `agent.crt`,
   `agent.key`, `server-ca.crt` into `$HOME/.quicbaas/agent`.
4. **Supervisor install**
   - With `--systemd yes` the workflow writes
     `$HOME/.config/systemd/user/quicbaas-agent.service`, reloads, and
     starts the unit.
   - With `--systemd no` it uses a nohup-backed start script at
     `$HOME/.quicbaas/bin/start-quicbaas-agent.sh`.
   - With `--systemd auto` (default) it probes `systemctl --user` and
     falls back to nohup when the probe fails.
5. **Wait online** — polls the fleet registry every 2 seconds until
   the agent reports `running`, `idle`, or `ready`. Disable with
   `--wait-online 0`.

The command prints a result that includes the issued certificate
serial so operators can correlate the deploy against the CA's
certificate store.

## Agent-side authentication

Once installed, the daemon authenticates to the server over mTLS on
every call:

- **Registration** (first start): `POST /api/v1/agents/register`
  using the client certificate. The server returns OAuth2 client
  credentials the daemon caches at `$HOME/.quicbaas/agent/credentials.json`,
  along with the **gRPC event-ingest URL** the agent should dial for the
  long-lived `PushEvents` stream (e.g., `https://quicbaas.example.com:9443`).
  The agent persists this URL in `credentials.json` under
  `event_ingest_addr`.
- **Heartbeat + events** (every call): standard OAuth2 bearer token
  minted from the cached credentials via `POST /oauth2/token`. Events
  flow over the gRPC ingest URL learned at registration time — never
  over the REST port — so the gRPC `h2` ALPN handshake succeeds without
  having to dual-publish gRPC behind the REST listener.

The daemon reads its cert/key/CA from the files the deploy flow
installed. There is no shared-secret bootstrap token at any stage.

## Flag reference

| Flag | Description |
| --- | --- |
| `--ssh user@host[:port]` | SSH target (required) |
| `--ssh-key-name name` | Name of an xkms-stored SSH key (required) |
| `--server https://...` | QuicBaaS server the agent will dial (required) |
| `--tenant id` | Tenant the agent serves |
| `--backend kind` | Agent backend (repeatable) |
| `--binary path` | Local static CLI binary |
| `--install-dir dir` | Remote install dir (default `~/.quicbaas/bin`) |
| `--data-dir dir` | Remote data dir (default `~/.quicbaas/agent`) |
| `--systemd auto\|yes\|no` | Supervisor selection |
| `--wait-online dur` | Max time to wait for online status |
