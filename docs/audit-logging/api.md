# Audit Logging — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/audit-logging/`.

## Transport matrix

Every row below is derived from source, not from prose. The columns mean:

- **SDK method** — the method name `sdk/go/client/<pkg>` passes to
  `transport.Request(ctx, serviceName, "<method>", req)`.
- **REST route (transport)** — where `sdk/go/transport/rest` sends it.
- **Served?** — whether that exact method+path pair is registered in the
  server's route table (`pkg/api/rest/*.go`). A **no** is a live SDK-to-server
  mismatch, not a documentation gap.
- **gRPC** — whether `sdk/go/transport/grpc` routes the service through a typed
  handler. **Unix inherits this column exactly**: the Unix transport is gRPC
  over `AF_UNIX` and delegates every `Request` to it
  (`sdk/go/transport/unix/transport.go:114`).
- **QUIC** is likewise not a separate column: `sdk/go/transport/quic` embeds
  `*rest.Transport` and swaps only the `http.Client` for an `http3.Transport`
  (`sdk/go/transport/quic/transport.go:86`), so every QUIC row is its REST row
  carried over HTTP/3. See the caveat in
  [../multiprotocol-api/architecture.md](../multiprotocol-api/architecture.md):
  **no component in this repository serves HTTP/3 today.**
- **MCP** — whether the SDK routes the service to an MCP tool; the registered
  server-side tool names are listed above the table.
- **Embedded** — the `service/method` key registered in
  `sdk/go/transport/embedded/transport.go`.
- **Permission** — `resource:action` from
  `pkg/api/rest/routes_permissions_generated.go`.

### `quicbaas.IAM.Audit` — the quicbaas IAM audit store

Dedicated transport handlers: rest=yes (`sdk/go/transport/rest/iam_handler.go`),
grpc=yes (`sdk/go/transport/grpc/iam_handler.go`), mcp=yes
(`sdk/go/transport/mcp/iam_handler.go`); embedded routes registered: 5
(`sdk/go/transport/embedded/handlers_iam_audit.go`).

MCP tools: `quicbaas_iam_audit_query`, `quicbaas_iam_audit_get`, `quicbaas_iam_audit_delete` (`pkg/api/mcp/tools_iam.go`)

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `Query` | `GET /api/v1/iam/audit` | yes | `QueryAuditEvents` | `quicbaas_iam_audit_query` | `quicbaas.IAM.Audit/Query` | `audit:query` |
| `Get` | `GET /api/v1/iam/audit/{id}` | yes | `GetAuditEvent` | `quicbaas_iam_audit_get` | `quicbaas.IAM.Audit/Get` | `audit:read` |
| `Record`, `RecordAsync` | `POST /api/v1/iam/audit` | yes | `RecordAuditEvent` | — | `quicbaas.IAM.Audit/Record`, `/RecordAsync` | `audit_events:record` |
| `Delete` | `DELETE /api/v1/iam/audit` | yes | — | `quicbaas_iam_audit_delete` | `quicbaas.IAM.Audit/Delete` | `audit_events:delete` |

`Delete` erases nothing: the chain is append-only and the call always answers
`deleted_count: 0` (see [architecture.md](architecture.md)). The gRPC handler
answers `Unimplemented` for `Delete`, and no MCP tool records an event.

### `quicbaas.Security().QRDBAudit()` — go-qrdb's audit service, re-exported

`sdk/go` re-exports go-qrdb's `transport.AuditClient` as
`quicbaas.QRDBAuditClient` (`sdk/go/qrdb.go`), with its types
`QRDBAuditEvent`, `QRDBAuditFilter` and `QRDBAuditPage`. A consumer reaches
go-qrdb's audit service without importing `go-qrdb/sdk/go`:

```go
ac, err := c.Security().QRDBAudit()
if err != nil {
    return err // the qrdb client failed to build, or its transport has no audit surface (Unavailable)
}
err = ac.EmitAudit(ctx, quicbaas.QRDBAuditEvent{Actor: "alice", Resource: "kv/key-1", Action: "kv.put", Outcome: "success"})
page, err := ac.QueryAudit(ctx, quicbaas.QRDBAuditFilter{Limit: 50})
```

| Method | What it does | Permission |
|---|---|---|
| `EmitAudit` | Records one compliance-grade event in the node's audit sink. A non-nil error means the sink refused the write. | `audit:emit` |
| `QueryAudit` | Returns a page of events matching the filter, always confined to the caller's own tenant; a filter naming another tenant is refused. | `audit:query` |

The client is the qrdb sub-client's `Audit()`:

- **Embedded**: backed by `QRDBEmbeddedServicers.Audit`, which the `quicbaas`
  CLI fills with the daemon's node audit sink (`(*Server).GetQRDBAuditSink`).
  Without a sink the accessor answers `Unavailable`.
- **Network transports**: reached with the client's platform bearer. Over
  REST and QUIC, `QueryAudit` is `GET /v1/audit/events` sent by the SDK's REST
  transport with its `Authorization` header; over MCP it is the
  `qrdb_audit_query` tool on the authenticated MCP session; over gRPC and Unix
  the bearer goes as `authorization` metadata. `EmitAudit` answers
  `NotImplemented` over REST, QUIC and MCP.
- A transport with no audit surface answers `Unavailable`.

