# QuicBaaS Remote Agent Daemon — Operator's Guide

This guide covers deployment, configuration, and troubleshooting of the QuicBaaS remote agent daemon (`quicbaas agent serve`).

## Overview

The QuicBaaS agent daemon is a long-lived service that runs on remote hosts (GPU machines, development workstations, edge devices) and exposes a gRPC `AgentRuntime` service. The QuicBaaS control plane calls this service to spawn AI agent sessions (Claude Code, Codex, Ollama) with full sandbox enforcement and tenant isolation.

**When to use the daemon (this guide):**
- You need to run agent sessions on remote hosts controlled by the control plane
- The remote host has Docker or Podman for sandboxed execution
- You want persistent daemon process management (systemd, Kubernetes, etc.)
- You need fine-grained sandbox policy enforcement (required/preferred/opt-in)

**When to use container scripts instead:**
- You want lightweight, ad-hoc agent deployment (see `deployments/agents/*/entrypoint.sh`)
- The host doesn't need to be registered with the control plane
- You're OK with self-registration via REST polling instead of bidirectional heartbeat

The daemon is **sandbox-first** by default: all spawn requests resolve to container-sandboxed execution unless explicitly opted out and the policy permits it.

## Identity & Registration

The daemon authenticates to the QuicBaaS server via **mTLS** on every
call. Identity material (certificate, private key, CA bundle) is
provisioned by `quicbaas agent deploy` — operators never handle a
pre-shared bootstrap token.

### Identity Files

The deploy flow installs three files into the daemon's data directory:

| File | Mode | Purpose |
| --- | --- | --- |
| `agent.crt` | 0644 | Leaf X.509 client cert issued by the QuicBaaS CA |
| `agent.key` | 0600 | Ed25519/ECDSA private key matching `agent.crt` |
| `server-ca.crt` | 0644 | CA bundle the daemon trusts when dialing the server |

The certificate carries `OU=agents`, `O=<tenant>`, `CN=agent-<hostname>`
and the `ClientAuth` EKU. The server's register handler rejects any
certificate missing these stamps.

### Flags

Point the daemon at the three files plus the server address:

```bash
quicbaas agent serve \
  --server https://quicbaas.example.com:8443 \
  --tenant acme \
  --agent-cert /var/lib/quicbaas-agent/agent.crt \
  --agent-key  /var/lib/quicbaas-agent/agent.key \
  --server-ca  /var/lib/quicbaas-agent/server-ca.crt \
  --allowed-mount-root /srv/agent-workspaces
```

`--allowed-mount-root` is repeatable and required in practice: without it the
daemon refuses every sandboxed spawn that names a work directory. See
[Bind-Mount Allowlist Reference](#bind-mount-allowlist-reference).

### Registration Flow (first start)

1. The daemon opens a TLS 1.2+ handshake to the server presenting
   `agent.crt` + `agent.key`.
2. The server validates the chain against its CA, checks for
   `OU=agents` and `ClientAuth`, and extracts the tenant claim from
   the certificate Organization field.
3. The daemon POSTs its declared capabilities to
   `/api/v1/agents/register` over the mTLS connection.
4. The server mints an OAuth2 client (`client_id` / `client_secret`)
   scoped to `agent:runtime:spawn|prompt|status` and returns the
   credentials in the response body.
5. The daemon persists the credentials to
   `${work_dir}/credentials.json` with mode 0600.

### Subsequent starts

The daemon reuses the cached `credentials.json` and goes straight to
`/oauth2/token` with the `client_credentials` grant. The registration
POST is skipped entirely.

To force re-registration (after CA rotation, tenant change, or
compromise recovery), delete `credentials.json` and restart.

## Sandbox Policy Reference

The `--sandbox-policy` flag controls how the daemon resolves sandbox modes and which execution styles are permitted.

| Policy | `DEFAULT` resolves to | `REQUIRED` allowed? | `NONE` allowed? | Use case |
|--------|----------------------|---------------------|-----------------|----------|
| **required** | Container-sandboxed | Yes | No | Production, FedRAMP, HIPAA, PCI DSS |
| **preferred** | Container-sandboxed | Yes | Yes | Staging, mixed workloads |
| **opt-in** | Raw process exec | Yes | Yes | Development, legacy systems (discouraged) |

### When to Pick Each Policy

**`required` (default, recommended for production):**
- Enforcement: All spawn requests run in containers unless explicitly denied by the policy
- Best for: Compliance (FedRAMP, HIPAA, PCI DSS), untrusted code execution, production deployments
- Tenant example: Regulated financial services, healthcare platforms
- Raw exec rejection: Requests with `SANDBOX_NONE` are rejected with `PERMISSION_DENIED`

**`preferred` (balanced, good for staging):**
- Enforcement: Defaults to container (safer), but clients can explicitly request raw exec
- Best for: Staging environments, development teams that need flexibility, mixed workloads
- Tenant example: SaaS platforms with compliance option, internal tooling
- Raw exec permission: Requests with `SANDBOX_NONE` are allowed

**`opt-in` (legacy, not recommended):**
- Enforcement: Defaults to raw process exec (less safe), containers only on explicit request
- Best for: Development, legacy integrations, internal-only systems
- Tenant example: Private research teams, legacy Claude Code setups
- Security posture: Weaker; do not use in production

### Tenant-Level Policy Override

The control plane can force a stricter policy per tenant, regardless of daemon configuration. Example: a FedRAMP tenant always gets `required` policy even if the daemon is set to `preferred`.

This is configured by the QuicBaaS operator when creating/updating the tenant:
```bash
quicbaas iam tenant update acme --sandbox-policy=required
```

**Precedence:** Tenant override > Daemon policy > Request-specific mode

## Sandbox Network Reference

Sandboxing has two independent isolation dimensions. `--sandbox-policy` decides
whether the agent runs in a container at all; `--sandbox-network` decides
whether that container gets its own network namespace.

| `--sandbox-network` | Container namespace | Egress | Can reach the host's `127.0.0.1` services |
|---------------------|---------------------|--------|-------------------------------------------|
| `bridge` (default)  | Own                 | Yes, NAT through the runtime bridge | No |
| `none`              | Own, loopback only  | No     | No |
| `<network name>`    | Own, on a user-defined runtime network | Per network | No |
| `host`              | **The host's**      | Yes    | **Yes — every one of them** |

`bridge` is the default because an ACP agent needs outbound HTTPS to reach its
provider API (`api.anthropic.com`, a remote Ollama, an OIDC issuer) but has no
legitimate reason to reach services bound to the daemon host's loopback
interface — the daemon's own admin listeners, local metrics endpoints, a
`127.0.0.1` datastore. Host firewall rules for those services generally assume
no container can reach them.

`host` remains available for hosts that genuinely need it, but only as an
operator decision recorded in configuration:

```bash
quicbaas agent serve --sandbox-network host ...      # remote agent daemon
quicbaasd start --embedded-agent-sandbox-network host  # embedded agent
```

A spawn request cannot select the network: there is no network field on the
request, so no tenant, workflow, or `container_config` can move a session onto
the host namespace. Choosing `host` is logged once per backend at WARN.

**Choosing `host` for a loopback MCP endpoint.** When the QuicBaaS MCP endpoint
a sandboxed agent dials resolves to `127.0.0.1` — which is what the *embedded*
agent's resolver produces, because control plane and agent share a host — a
bridged container cannot reach it. Either give the agent a routable MCP endpoint
(`--mcp-endpoint https://<host>:<port>/mcp`, which is how a remote agent daemon
is always configured) or set `--embedded-agent-sandbox-network host` and accept
that the container shares the host namespace. Reaching a *provider* API needs no
special handling: `bridge` has egress.

## Bind-Mount Allowlist Reference

Sandboxing's third isolation dimension. `--sandbox-policy` decides whether the
agent runs in a container, `--sandbox-network` decides whether that container
gets its own network namespace, and `--allowed-mount-root` decides how much of
**this host's filesystem** the container can see.

```bash
quicbaas agent serve \
  --allowed-mount-root /srv/agent-workspaces \
  --allowed-mount-root /srv/models ...
```

```yaml
allowed_mount_roots:
  - /srv/agent-workspaces
  - /srv/models
```

| Setting | Effect on a sandboxed spawn naming a `work_dir` |
|---------|------------------------------------------------|
| omitted (default) | **Every** spawn naming a work directory is refused |
| one or more absolute roots | Only sources resolving under a listed root are accepted |

Deny-by-default is deliberate. The daemon's Spawn RPC takes `work_dir` straight
off the wire, and the ACP backend bind-mounts it into the container **read-write
as the daemon's own uid**. An unchecked `work_dir` of `/` is therefore this
host's root filesystem, writable, for the agent — reachable by any principal
holding `agent:runtime:spawn`. There is no setting that turns the check off.

`pkg/agent.Runtime.Spawn` enforces it through the same
`orchestration.MountPolicy` the control plane uses, so both enforcement points
behave identically:

- **Resolved, not lexical.** Each source is resolved with
  `filepath.EvalSymlinks` and re-tested, so an agent that plants a symlink
  inside its own workspace cannot have the link's target mounted on the next
  spawn. A source that does not exist is refused — the runtime would create it
  root-owned and its identity could not be checked.
- **Roots match on segment boundaries.** `/srv/models` does not grant
  `/srv/models-staging`.
- **Both mount-bearing fields are covered.** `work_dir` is authorized when the
  spawn is containerized, which is exactly when it becomes a bind mount;
  `extra_mounts` is authorized on every path, since it exists for no other
  purpose.

The roots are **not** tenant-scoped: a daemon manages no project workspaces of
its own, so the operator's list is shared by every tenant it serves. Do not list
a directory holding one tenant's data on a daemon serving several. Non-absolute
entries fail `Validate()` at startup rather than silently granting nothing.

### Why the daemon checks again

The control plane authorizes the same fields in `SpawnSession`, but the daemon's
check is the last line, not a duplicate:

1. A spawn whose **control-plane-resolved** sandbox mode is not `required` skips
   the control plane's `work_dir` authorization entirely — yet this daemon's own
   `--sandbox-policy` may still containerize it. Only a check against the mode
   resolved *here* covers that.
2. The daemon's filesystem layout is its own. A path the control plane allowed
   means something different on this host.

A refused mount is returned as gRPC `PERMISSION_DENIED` carrying the offending
field, tenant and path, which the control plane re-surfaces as HTTP 403.

## Container Environment and Credentials

A sandboxed session's environment carries its provider credentials: the pooled
account's API key or OAuth access token, plus the agent's MCP connection
material. Those values never appear on the container runtime's command line.

`/proc/<pid>/cmdline` is world readable on a default Linux host and is what
`ps` prints, so a `docker run -e KEY=VALUE` argument would publish the value to
every local user for as long as the run process lives. The daemon instead writes
the container's environment to a private file (mode `0600`, in a `0700`
per-launch directory) and passes `--env-file <path>`:

- The detached launch path releases the file as soon as the runtime process exits.
- A sandboxed ACP session's file lives as long as the container and is removed
  when the session is closed, on its own cleanup slot — independent of the
  session's MCP certificate directory and its pooled-account credential
  directory, so an account rotation cannot delete a file the running container
  still needs.
- Log lines render the argument vector with every environment value replaced by
  `[redacted]`, and the vector itself is a DEBUG-level diagnostic.

There is no configuration flag that restores inline environment arguments. An
environment entry the line-oriented env-file format cannot carry — a value
containing a newline (which would inject an undeclared variable into the
container), a name containing `=` or whitespace, a name starting with `#` — is
rejected at spec validation and the launch fails.

## Required IAM Scopes

The daemon's client JWT must include these scopes to authorize each RPC type:

| RPC | Scope | Permission |
|-----|-------|-----------|
| `Spawn` | `agent:runtime:spawn` | Initiate a new agent session |
| `Prompt` | `agent:runtime:prompt` | Send prompts to existing sessions |
| `Status` | `agent:runtime:status` | Query session status |
| `Close` | `agent:runtime:status` | Terminate sessions (grouped with status for simplicity) |
| `Heartbeat` | implicit (registered agents) | Bidirectional heartbeat stream |

These scopes are issued by the enrollment token exchange. If a token is missing any scope, that RPC is denied with a 403 Unauthenticated error.

## Host Requirements

### OS and Distribution

- **Linux** (recommended): AlmaLinux, Rocky Linux, Ubuntu 22.04+ LTS, Debian 12+
- **macOS**: Intel or Apple Silicon, 10.15+ (for local development)
- **Windows**: Windows Server 2022+, WSL2 for development

### Container Runtime

The daemon requires Docker or Podman for sandboxed execution:

**Docker:**
- Install: `curl https://get.docker.com | sh`
- Socket location: `/var/run/docker.sock`
- User permission: Add daemon user to `docker` group OR mount socket with appropriate permissions
- Systemd unit mounts the socket at `/var/run/docker.sock` (read-write)

**Podman:**
- Install: `sudo apt install podman` (or equivalent)
- Socket location: `/run/podman/podman.sock` (rootless) or `/run/podman/podman.sock` (rootful)
- Compatibility: Daemon auto-detects; use `--container-runtime=podman` to force
- Advantage: Rootless mode (no privilege escalation needed)

**Auto-detection (`--container-runtime=auto`, default):**
- Probes for Docker first
- Falls back to Podman if Docker unavailable
- Fails startup if neither is available (unless `--container-runtime=none` disables sandboxing)

### Network Ports

| Port | Direction | Purpose |
|------|-----------|---------|
| 9091 | Inbound | AgentRuntime gRPC service (default bind; configurable) |
| 8443 | Outbound | Central QuicBaaS orchestrator (gRPC/QUIC) |
| 53 | Outbound | DNS (for control plane hostname resolution) |

The daemon initiates outbound connections to the control plane; no inbound access to the control plane is needed.

### Disk Space

Estimate disk usage per session:

| Component | Size |
|-----------|------|
| Daemon binary | ~50 MB |
| Per-session workspace | 100 MB – 1 GB (depends on workload) |
| Persisted credentials | < 1 KB |
| Logs (per session) | 10 – 100 MB |

**Recommendation:** Allocate 20 – 100 GB for `/var/lib/quicbaas-agent` depending on concurrent session count and data size.

### Optional: GPU Support (Ollama)

If you plan to run Ollama inference with GPU acceleration:

**NVIDIA GPU (recommended):**
- Hardware: NVIDIA GPU (RTX 6000 Pro, H100, RTX 4090, etc.)
- Driver: NVIDIA GPU driver 535+ installed on host
- Container runtime: Docker with `nvidia-container-runtime` (installed via `apt install nvidia-container-runtime`)
- Entrypoint: Daemon automatically passes `--gpus all` to containerized Ollama
- Verification: `nvidia-smi` on host returns GPU info

**AMD GPU:**
- Hardware: AMD Radeon GPU (MI250, MI300, RDNA 2+)
- Driver: ROCm driver installed
- Podman preferred (better rocm-runtime support)
- Entrypoint: Daemon passes `--device /dev/kfd` to Ollama containers

**No GPU:**
- Ollama falls back to CPU (slow; not recommended for production)
- CPU performance: ~10–100 tokens/sec depending on model size and CPU

**Verification:**
```bash
docker run --rm --gpus all nvidia/cuda:12.0-runtime nvidia-smi
```

If this succeeds, GPU acceleration is available to the daemon.

## Troubleshooting

This section guides operators through common issues. Follow each step in order.

### Registration Fails

**Symptom:** Daemon exits with "server rejected registration"

**Step 1: Verify the certificate is still valid**
```bash
openssl x509 -in /var/lib/quicbaas-agent/agent.crt -noout -dates -subject
# Check notAfter is in the future; confirm OU=agents and O=<tenant>
```

**Step 2: Verify control plane reachability**
```bash
# On daemon host, test connectivity to the server
curl -k https://${QUICBAAS_SERVER}:8443/healthz
# Should return HTTP 200 (allow self-signed certs with -k)
```

**Step 3: Check clock skew**
```bash
# Verify daemon host clock is synchronized
date
timedatectl
# If drift > 5 minutes, resync:
sudo timedatectl set-ntp true
sudo systemctl restart systemd-timesyncd
```

**Step 4: Check tenant configuration**
```bash
# On the server, verify tenant exists and is active
quicbaas iam tenant list --filter acme
# Should show status: active
```

### Container Spawn Fails

**Symptom:** Spawn requests fail with "no container runtime" or "image pull failed"

**Step 1: Verify container runtime is available**
```bash
# On daemon host, test docker/podman
docker ps
# OR
podman ps
# If both fail, install Docker or Podman
```

**Step 2: Check socket permissions**
```bash
# Verify daemon user can access Docker socket
ls -l /var/run/docker.sock
# Should be readable/writable by daemon user or docker group
sudo usermod -aG docker quicbaas-agent
# Restart daemon for group changes to take effect
```

**Step 3: Verify image availability (if `auto_pull=false`)**
```bash
# List local images
docker images | grep claude-code
# If missing, either:
# a) Enable auto_pull in config:
#    auto_pull: true
# b) Pre-pull manually:
docker pull quicbaas/claude-code:latest
```

**Step 4: Check image pull logs (if `auto_pull=true`)**
```bash
# Watch daemon logs during startup
sudo journalctl -u quicbaas-agent -f --lines=50
# Look for pull progress or "pull failed" errors
```

**Step 5: Verify image can run**
```bash
# Test image directly
docker run --rm quicbaas/claude-code:latest --version
# Should output version info; if it fails, image is broken
```

### Tenant Mismatch Errors

**Symptom:** Spawn requests rejected with "tenant isolation: request tenant=X but daemon serves tenant=Y"

**Root cause:** Central sent a spawn request for the wrong tenant.

**Step 1: Verify daemon configuration**
```bash
# Check daemon tenant from log
sudo journalctl -u quicbaas-agent | grep "tenant="
# Verify it matches your target tenant
```

**Step 2: Verify control plane's target tenant**
```bash
# On the server, check which tenant issued the spawn request
quicbaas orchestrator list-sessions --tenant <tenant>
# Confirm the spawn request was for the expected tenant
```

**Step 3: Verify control plane has correct daemon registration**
```bash
# On the server, check daemon is registered under correct tenant
quicbaas agent list --tenant acme
# Should show the daemon with agent_kind=daemon, status=running
```

### Stuck-Offline / Heartbeat Failures

**Symptom:** Daemon shows as "offline" on control plane; heartbeat errors in logs

**Step 1: Verify network path to the server**
```bash
# From daemon host, test connectivity
ping <server-hostname>
nc -zv <server-hostname> 8443
# Both should succeed
```

**Step 2: Check firewall rules**
```bash
# Verify outbound 8443 is allowed
sudo ufw status
sudo firewall-cmd --list-all
# Add rule if needed:
sudo ufw allow out 8443
```

**Step 3: Verify JWT token hasn't expired**
```bash
# Check persisted credentials
cat ${work_dir}/credentials.json | jq .expires_in
# Compare with current timestamp
date +%s
# If expired, delete credentials and restart to re-enroll:
rm ${work_dir}/credentials.json
sudo systemctl restart quicbaas-agent
```

**Step 4: Check control plane is healthy**
```bash
# From any client, verify control plane is responding
quicbaas agent list --server <addr>
# If control plane is down, restart it first
```

**Step 5: Increase heartbeat timeout on control plane**
```bash
# On the server, check agent heartbeat timeout config
quicbaas config show | grep agent.heartbeat
# Default is 3 missed heartbeats (90 seconds at 30s interval)
# If network is flaky, increase timeout
```

## Upgrade Procedure

Upgrading the daemon involves building/obtaining a new binary and a graceful restart.

### 1. Drain In-Flight Sessions

Allow active sessions to complete:

```bash
# Set daemon to read-only (no new spawns accepted)
# This is typically done by the control plane:
quicbaas agent update <daemon-id> --drain
# Or, wait for sessions to complete naturally (max ~5 minutes)
```

### 2. Stop the Daemon

```bash
# Graceful stop with 30-second drain timeout (configurable via --shutdown-grace)
sudo systemctl stop quicbaas-agent
# Monitor logs to verify clean shutdown
sudo journalctl -u quicbaas-agent --lines=10
```

### 3. Back Up Credentials

Credentials are persisted and will be reused on restart. Back them up for safety:

```bash
sudo cp /var/lib/quicbaas-agent/credentials.json /var/lib/quicbaas-agent/credentials.json.bak
```

### 4. Replace the Binary

```bash
# Build new binary (on dev machine) and copy to daemon host:
make build
scp bin/quicbaas user@daemon-host:/tmp/quicbaas

# OR, pull from image registry and extract:
docker pull quicbaas/agent:v1.2.0
docker run --rm -v /tmp:/out quicbaas/agent:v1.2.0 cp /usr/local/bin/quicbaas /out/

# Copy to system location:
sudo cp /tmp/quicbaas /usr/local/bin/quicbaas
sudo chmod 755 /usr/local/bin/quicbaas
sudo chown root:root /usr/local/bin/quicbaas
```

### 5. Restart the Daemon

```bash
sudo systemctl start quicbaas-agent
sudo systemctl status quicbaas-agent
sudo journalctl -u quicbaas-agent -f --lines=20
```

### 6. Verify Registration

On the control plane, confirm the daemon is back online:

```bash
quicbaas agent list --tenant acme | grep daemon-id
# Should show status=running and recent heartbeat timestamp
```

**Credential survival:** Persisted credentials in `credentials.json` are automatically reused. The daemon will NOT re-enroll unless the file is deleted. This is safe across multiple restarts and version upgrades.

## Security Posture

### What the Daemon Protects

The daemon enforces:

- **Tenant isolation:** Requests for tenants other than the configured `--tenant` are rejected with `PERMISSION_DENIED`
- **Sandbox enforcement:** The daemon's `--sandbox-policy` controls whether raw exec is permitted (defaults to `required`, forbidding raw exec)
- **Bind-mount allowlist:** A sandboxed spawn may only bind-mount host paths resolving under `--allowed-mount-root`; with no roots configured every wire-supplied `work_dir` and extra mount is refused (see [Bind-Mount Allowlist Reference](#bind-mount-allowlist-reference))
- **JWT scope validation:** Only RPCs authorized by the daemon's OAuth2 scopes are allowed (e.g., `agent:runtime:spawn` for Spawn RPC)
- **Session state encryption:** Session state is encrypted at rest using the tenant's DEK (Data Encryption Key) from the control plane's KMS
- **Audit logging:** Every RPC is logged with tenant, backend, session ID, sandbox mode, and result

### What the Daemon Does NOT Protect

The operator is responsible for:

- **Host OS hardening:** SELinux, AppArmor, file permissions, user isolation
- **Network security:** Firewall rules, mTLS between control plane and daemon, VPN/private networking
- **Container runtime security:** Docker/Podman security options (capabilities, seccomp, AppArmor), image signing, registry authentication
- **Secrets management:** OAuth2 credentials file permissions (`0600`), key rotation, secret injection
- **Resource limits:** Memory/CPU quotas for containers, disk quota for work directory
- **Monitoring and alerting:** Daemon health checks, log aggregation, metrics collection

Example OS hardening:
```bash
# Restrict daemon user shell and home
sudo usermod -s /usr/sbin/nologin quicbaas-agent
sudo chmod 700 /var/lib/quicbaas-agent

# Enable SELinux (if available)
sudo semanage fcontext -a -t admin_home_t "/var/lib/quicbaas-agent(/.*)?"
sudo restorecon -Rv /var/lib/quicbaas-agent

# Enable read-only work directory except for specific subdirectories
sudo mount -o bind,ro /var/lib/quicbaas-agent /var/lib/quicbaas-agent
```

## References

- **Configuration file reference**: `deployments/agent-daemon/sample-config.yaml`
- **Systemd unit**: `deployments/agent-daemon/quicbaas-agent.service`
- **Docker image**: `deployments/agent-daemon/Dockerfile`
- **CLI flags**: `quicbaas agent serve --help`
- **Policy semantics**: `pkg/orchestration/sandbox_policy.go`
- **Bind-mount policy**: `pkg/orchestration/mount_policy.go` (shared with the control plane)
- **Network isolation**: `pkg/orchestration/sandbox_network.go`
- **Container environment transport**: `pkg/orchestration/container_env_file.go`
- **Daemon API types**: `pkg/agent/config.go`
- **Registration flow**: `pkg/agent/enrollment.go`

---

## Embedded Agent Mode

In addition to the standalone `quicbaas agent serve` daemon, quicbaasd can run an
**embedded agent** in-process. This is convenient when the orchestrator host itself
has Ollama installed and you want zero-latency inference without deploying a separate
daemon process.

### How it differs from the standalone daemon

| Aspect | Standalone (`quicbaas agent serve`) | Embedded (`quicbaasd --embedded-agent`) |
|---|---|---|
| Process | Separate | Same process as quicbaasd |
| Transport | TCP gRPC + mTLS | In-process bufconn (no network) |
| Identity | Operator-provided cert files | Issued from local CA at boot |
| Registration | POST /api/v1/agents/register | Direct fleet registry insertion |
| Heartbeat | HTTP heartbeat loop | Always alive while process runs |
| SSH/SFTP boot | Yes (deploy workflow) | No |

The embedded agent is a first-class fleet member. It appears in
`GET /api/v1/agents` with `kind: embedded` and can receive inference
requests via the same orchestrator dispatch path as remote daemons.

### Starting the embedded agent

```bash
# Start with embedded Ollama agent (default URL: http://127.0.0.1:11434)
quicbaasd start --embedded-agent

# Specify Ollama URL and advertise GPU capability
quicbaasd start \
  --embedded-agent \
  --embedded-agent-backend ollama \
  --embedded-agent-ollama-url http://127.0.0.1:11434 \
  --embedded-agent-gpu \
  --embedded-agent-host-affinity gpu-node-01
```

### Configuration keys

These map to `Server.EmbeddedAgent.*` in the YAML config:

```yaml
server:
  embedded_agent:
    enabled: true
    backends: [ollama]
    ollama_url: http://127.0.0.1:11434
    host_affinity: gpu-node-01   # self-tag for host_affinity routing
    gpu_available: true
    data_dir: /var/lib/quicbaas/embedded-agent
    sandbox_policy: opt-in     # opt-in | preferred | required
    sandbox_network: bridge    # bridge (default) | none | host | <network name>
```

The embedded agent has **no** bind-mount key of its own: it shares a host with
the orchestrator, so it enforces the orchestrator's policy —
`orchestration.projects_root` (tenant-scoped) plus
`orchestration.sandbox.allowed_mount_roots`. Without that, a spawn dispatched
over the bufconn would bypass the control plane's mount check.

### CLI flags

| Flag | Default | Description |
|---|---|---|
| `--embedded-agent` | false | Enable the embedded agent |
| `--embedded-agent-backend` | ollama | Backend kind (repeatable) |
| `--embedded-agent-ollama-url` | http://127.0.0.1:11434 | Ollama API endpoint |
| `--embedded-agent-host-affinity` | hostname | Affinity tag for routing |
| `--embedded-agent-gpu` | false | Advertise GPU capability |
| `--embedded-agent-data-dir` | `<data-dir>/embedded-agent` | Session state directory |
| `--embedded-agent-sandbox-policy` | opt-in | How `DEFAULT` sandbox requests resolve |
| `--embedded-agent-sandbox-network` | bridge | Container network for sandboxed sessions; `host` removes network isolation |

### Agent ID

The embedded agent registers with a synthetic ID of the form
`embedded-<hostname>-<8-char-uuid>`. This ensures multiple quicbaasd
instances on different hosts never collide in the shared fleet registry.

### Transport: bufconn loopback

Inference RPCs to the embedded agent use `google.golang.org/grpc/test/bufconn`
— gRPC's official in-memory transport. This preserves full gRPC semantics
(interceptors, error types, streaming) while eliminating marshal/unmarshal
overhead for in-process calls. Latency is approximately 1 µs per RPC vs.
~100 µs for a loopback TCP connection.

### mTLS identity

When TLS is enabled, the embedded agent issues its own X.509 client
certificate from the local CA using the same `XKMSAgentCertIssuer` path as
remote agent deployment. The cert is not needed for transport security
(bufconn is in-process only) but is recorded in registration metadata for
audit purposes.

### Shutdown

The embedded agent stops before quicbaasd's external-facing servers
(gRPC, REST, QUIC). The stop sequence:

1. Deregister from the fleet registry (in-flight dispatchers stop routing).
2. Graceful gRPC server stop (15-second timeout, then force stop).
3. Cancel background goroutines via context cancellation.
