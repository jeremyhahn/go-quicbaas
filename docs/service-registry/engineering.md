# Service Registry — Engineering

> **Status: NORMATIVE** · Last verified: 2026-10-01
> Build, test and operate. Feature folder: `docs/service-registry/`.

## Tests

| Scope | Where | What it proves |
|---|---|---|
| Storage logic | `pkg/registry/*_test.go` | ownership marks, cross-tenant refusal, revisions, typed watch endings |
| Test store | `pkg/registry/registrytest` | lease deletion, history replay, compaction recognised by qrdb's classifier |
| Service | `pkg/service/registry_service_test.go` | TTL bounds, status codes, lease expiry seen by a watcher |
| gRPC server | `pkg/api/grpc/server_registry_test.go` | proto conversion, `OUT_OF_RANGE` on compaction |
| REST server | `pkg/api/rest/handlers_registry_test.go` | routes, 410, SSE frames, heartbeat, lease IDs as strings |
| MCP server | `pkg/api/mcp/registry_mcp_test.go` | arguments, loss-free poll cursor, compaction as a result |
| SDK client | `sdk/go/client/registry` | "from now" pinning, resume from next revision, retry budget |
| SDK transports | `sdk/go/transport/{grpc,rest,mcp,embedded,unix}` | what the server RECEIVED; gRPC over `bufconn`, MCP over a real in-process MCP server, embedded end to end |
| CLI | `cmd/quicbaas/registry_test.go` | flags, output formats, failures |
| Integration | `test/integration/cli/registry_test.go` | the CLI on every transport against a live server: lifecycle, replayed watch, lease expiry |

Run:

```bash
make test-registry               # unit tests across every layer
make coverage-registry
make integration-test-registry   # against the integration stack (devcontainer)
```

## Operating notes

- Choose a TTL comfortably above the renewal interval (renew at a third of the
  TTL) so one missed heartbeat does not drop a healthy instance.
- TTLs are bounded to 1–86400 seconds; a longer lease would keep a dead host's
  endpoint listed beyond any health check.
- Watch history is bounded by qrdb's `mvcc.compaction_retention`. A watcher idle
  for longer than the retention (MCP polls only advance on delivered changes)
  may get `WatchCompactedError` and must resolve again.
- Permissions: `service_registry:register` (Admin, Operator) and
  `service_registry:read` (Admin, Operator, Auditor), self-scoped.
