# Observability — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented guide to knowing what a running deployment is doing. Feature
> folder: `docs/observability/`.

## Prerequisites

- A running server you can reach.
- For metrics, a Prometheus-compatible scraper.
- Nothing for health: the probes are unauthenticated by design.

## First run — wire the probes correctly

This is the one decision that causes real outages, so do it before anything
else.

```bash
# Liveness: "is this process wedged?" -> restart on failure.
curl -fsS https://quicbaas.example.com:18080/api/v1/health/live

# Readiness: "should traffic come here?" -> remove from the pool on failure.
curl -fsS https://quicbaas.example.com:18080/api/v1/health/ready
```

**Point your supervisor at liveness and your load balancer at readiness.** A
server that is sealed, still bootstrapping, or waiting on a Raft quorum is alive
and not ready. Wire readiness into the restart policy and you get a restart loop
during exactly the conditions you needed to observe.

## Common tasks

### Find out why a node is not taking traffic

```bash
curl -fsS https://quicbaas.example.com:18080/api/v1/health/ready
quicbaas cluster status
quicbaas barrier status
```

Readiness tells you *that* it is not ready. The next two tell you *why*: no
quorum, or a sealed barrier.

### Turn up detail on one command

```bash
quicbaas --log-level debug iam user list
```

Prefer this to raising the server's level: it scopes the noise to the thing you
are investigating.

### Capture server logs to a file

```bash
quicbaasd start --log-file /var/log/quicbaas/quicbaasd.log
tail -f /var/log/quicbaas/quicbaasd.log
```

The file is appended to, not truncated, so a restart does not lose the run that
preceded it.

### Check a subsystem rather than the whole process

```bash
curl -fsS https://quicbaas.example.com:18080/api/v1/vector/health
curl -fsS https://quicbaas.example.com:18080/api/v1/ollama/health
curl -fsS https://quicbaas.example.com:18080/api/v1/orchestrator/health
curl -fsS https://quicbaas.example.com:18080/api/v1/xkms/health
```

Subsystems report separately on purpose. A degraded embedding provider should
not take the process out of the pool.

## Before you expose this

The health endpoints are **unauthenticated**, and they disclose whether the
server is sealed or bootstrapping. That is information about your security
posture. The per-protocol `bind_address` settings exist so the probe surface can
be bound somewhere narrower than the API — use them rather than accepting a
network-wide probe surface as the price of a health check.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Restart loop after a restart | Liveness and readiness are swapped in the supervisor config. |
| Health returns 200 while requests fail | You are probing liveness. Probe readiness. |
| No traces anywhere, and no error | The binary was built without the OpenTelemetry tag; `tracing_no_otel.go` compiles a no-op. This is a build-time choice, not a flag. |
| `--log-file` produces nothing | The path is not writable by the server's user. The flag does not fall back to stderr silently for the daemon. |
| Metrics cardinality explodes | A label is carrying an unbounded value — a tenant id, a key name. See `engineering.md` for which labels are bounded. |

## What to use instead of this feature

- **For a security record**, not diagnostics: `../audit-logging/`. Logs are not
  evidence; the audit trail is.
- **For retained time series**: `../timeseries/`. Nothing here stores history.
- **For alerting**: whatever scrapes the metrics. This feature has no rules
  engine and no notification path.
