# Observability — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design and boundaries of health, metrics, logging and tracing. Feature
> folder: `docs/observability/`.

## The problem

Three different readers need three different things from a running server, and
conflating them produces a surface that serves none of them:

- an **orchestrator** needs a fast, unauthenticated yes/no on whether to route
  traffic here or restart the process;
- an **operator** needs metrics and logs to explain behaviour over time;
- a **developer** needs traces to explain one request.

## The shape chosen

Three separate surfaces, deliberately not unified.

### Health — `pkg/api/rest` health routes

| Route | Handler | Question it answers |
|---|---|---|
| `GET /api/v1/health` | `HandleHealth` | Is the process alive and its subsystems reporting? |
| `GET /api/v1/health/live` | `HandleLiveness` | Should the supervisor restart this process? |
| `GET /api/v1/health/ready` | `HandleReadiness` | Should a load balancer send it traffic? |

Liveness and readiness are separate because the answers diverge exactly when it
matters: a server that is sealed, or still bootstrapping, or waiting on a Raft
quorum is **alive and not ready**. Collapsing them into one endpoint produces a
restart loop during precisely the conditions the operator needs to observe.

The SDK reaches these through `Client.ClusterMgmt().Health()` with methods
`Ping`, `Liveness` and `Readiness`. Health is one of the few **public** MCP
tools — `qrdb_health`, `quicbaas_liveness`, `quicbaas_readiness` appear in the
public-tool set at `sdk/go/transport/mcp/transport.go:691-698`, so a probe does
not need a token.

Several subsystems expose their own health route rather than folding into the
top-level one — `/api/v1/ollama/health`, `/api/v1/orchestrator/health`,
`/api/v1/vector/health`, `/api/v1/xkms/health`,
`/api/v1/object-backends/{name}/health`. That is deliberate: a degraded
embedding provider should not make the whole process look unready.

### Metrics — `pkg/metrics`

Prometheus-style counters and gauges, exposed on the metrics route. Metric
names are part of the contract; `engineering.md` enumerates them.

### Logging and tracing — `pkg/logger`, `pkg/tracing`

Structured logging with `--log-level` and `--log-file` as root persistent flags
(`cmd/quicbaas/main.go:161-162`). Tracing is OpenTelemetry, and it is
**build-tag-gated**: `cmd/quicbaas/tracing_no_otel.go` compiles a no-op when the
tag is absent. A build without it does not merely emit no spans — it does not
carry the dependency.

### Alternatives rejected

- **One `/health` endpoint.** Rejected for the restart-loop reason above.
- **Authenticating the health probes.** Rejected: a probe that needs a token
  fails during exactly the sealed or half-booted states it exists to report.
  The trade-off is accepted and stated in the trust boundary below.
- **Always compiling in OpenTelemetry.** Rejected; the no-op build tag keeps the
  dependency out of builds that will never export a span.

## Trust boundaries

- **Health endpoints are unauthenticated.** They disclose liveness, readiness,
  and by implication whether the server is sealed or bootstrapping. Treat that
  as information disclosure and bind them accordingly — every listener
  binds on `server.host`, so keep the probe surface from being network-wide at
  the network boundary.
- **Metrics can leak shape.** Counter cardinality reveals tenant and key names
  if labels are chosen carelessly. `engineering.md` states which labels are
  bounded.
- **Logs are where secrets escape.** Nothing in this feature redacts a value a
  caller put into a log field.

## What this feature deliberately does not do

- **It does not alert.** No rules, no thresholds, no notification path.
- **It does not store history.** Metrics are scraped; nothing here retains a
  time series. Persisting them is `../timeseries/`'s job if you choose to.
- **It is not the audit trail.** Audit is a security record with integrity
  requirements (`../audit-logging/`); logs are diagnostics. Do not substitute
  one for the other.
