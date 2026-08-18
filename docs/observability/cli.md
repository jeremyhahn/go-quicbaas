# Observability — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Feature folder: `docs/observability/`.

**No dedicated command group.** There is no `quicbaas health`, no
`quicbaas metrics` and no `quicbaas logs` verb — the root command list
(`cmd/quicbaas/main.go:181-246`) has none of them.

Observability surfaces at the CLI in two other shapes.

## Root persistent flags that control logging

Declared at `cmd/quicbaas/main.go:161-162`, so they apply to every command:

| Flag | Type | Default | Description |
|---|---|---|---|
| `--log-level` | string | `info` | `trace`, `debug`, `info`, `warn`, `error`, `fatal`, `panic` |
| `--log-file` | string | none | Path to append to; default is stderr for `quicbaas`, stdout for `quicbaasd` |

`quicbaasd` carries the same two flags (`cmd/quicbaasd/main.go:196-197`), and
its `--log-file` doc note is worth repeating: it *appends*, so you can
`tail -f` it.

```bash
quicbaas --log-level debug iam user list
quicbaasd start --log-level debug --log-file /var/log/quicbaas/quicbaasd.log
```

Tracing is not a flag. It is selected at build time; a binary without the
OpenTelemetry build tag compiles `cmd/quicbaas/tracing_no_otel.go`, a no-op.
See `architecture.md`.

## Reading health from the command line

Use `curl` against the unauthenticated probes:

```bash
curl -fsS https://quicbaas.example.com:18080/api/v1/health
curl -fsS https://quicbaas.example.com:18080/api/v1/health/live
curl -fsS https://quicbaas.example.com:18080/api/v1/health/ready
```

`-f` makes `curl` exit non-zero on a non-2xx, which is what a supervisor script
wants. Do not use liveness where you mean readiness: a sealed or bootstrapping
server is **live and not ready**, and probing the wrong one restarts a process
that was about to come up.

Health is also reachable as public MCP tools (`qrdb_health`,
`quicbaas_liveness`, `quicbaas_readiness`) with no token, which is the route to
use from an MCP client:

```bash
quicbaas mcp call quicbaas_readiness
```

## Verbs in other groups that report state

These are not observability commands, but they are what an operator reaches for
next:

```bash
quicbaas cluster status         # membership and leadership
quicbaas scheduler status       # whether this node runs the timetable
quicbaas cache stats            # cache hit rate and evictions
quicbaas msg dedup-stats <ch>   # messaging deduplication counters
```

Each is documented in its own feature's `cli.md`.
