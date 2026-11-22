# Time Series — API

> **Status: NORMATIVE** · Last verified: 2026-11-14
> The capability on every transport, with permissions. Feature folder: `docs/timeseries/`.


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

### `Search().QRDBTimeSeries()` — go-qrdb's own client, re-exported

`Search().QRDBTimeSeries()` returns `QRDBTimeSeriesClient`, a type alias of
go-qrdb's `transport.TimeSeriesClient` (`sdk/go/qrdb.go`). It is go-qrdb's
client itself, built lazily over the transport the QuicBaaS client was built
with, and talks straight to the go-qrdb node: `TSWrite`, `TSWriteBatch`,
`TSWritePoint`, `TSQuery`, `TSRichQuery`, `TSAggregate`, `TSDeleteSeries`,
`TSGetRetention`, `TSSetRetention` and `TSListMetrics`. Over REST they map to
go-qrdb's `/v1/ts/*` routes (`/ts/write`, `/ts/query`,
`/ts/series/{metric}/retention`, ...). A go-qrdb client that cannot be built
(for example the embedded transport without go-qrdb servicers) is returned as
an error. Each transport takes an authenticated path: REST and MCP ride the
SDK's own transport, QUIC uses its REST adapter over HTTP/3 with the bearer,
gRPC and Unix add the bearer as metadata; a selection without an
authenticated adapter is refused rather than dialed unauthenticated. Tests:
`sdk/go/qrdb_timeseries_reexport_test.go`, `sdk/go/qrdb_quic_h3_test.go`.

### `quicbaas.TimeSeries` — SDK package `sdk/go/client/timeseries`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=yes; embedded routes registered: 11; MCP tools registered server-side: 7.

MCP tools: `qrdb_ts_delete_series`, `qrdb_ts_get_retention_policy`, `qrdb_ts_list_metrics`, `qrdb_ts_query`, `qrdb_ts_set_retention_policy`, `qrdb_ts_write_batch`, `qrdb_ts_write_point`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `Write` | `POST /api/v1/timeseries/write` | yes | `TSWritePoint` | `qrdb_ts_write_point` | `quicbaas.TimeSeries/Write` | `timeseries:write` |
| `Query` | `POST /api/v1/timeseries/query` | yes | `TSQuery` | `qrdb_ts_query` | `quicbaas.TimeSeries/Query` | `timeseries:write` |
| `SetRetention` | `PUT /api/v1/timeseries/retention/{metric}` | yes | `TSSetRetentionPolicy` | `qrdb_ts_set_retention_policy` | `quicbaas.TimeSeries/SetRetention` | `timeseries:write` |
| `GetRetention` | `GET /api/v1/timeseries/retention/{metric}` | yes | `TSGetRetentionPolicy` | `qrdb_ts_get_retention_policy` | `quicbaas.TimeSeries/GetRetention` | `timeseries:query` |

`SetRetention` and `GetRetention` carry a typed `RetentionPolicy` (`Duration`,
`Resolution`, `Aggregation`). On the REST, gRPC and MCP wires durations travel
as whole seconds and the aggregation as its lower-case name; `GetRetention`
returns a nil `Policy` for a metric without one; a zero policy (infinite
duration, no downsampling) is the same thing and also reads nil. `POST /api/v1/timeseries/query`
needs `timeseries:write`, as the generated permission table states; a query is
a read, so that row is a candidate for review.

Tests: `sdk/go/client/timeseries/client_retention_test.go`;
`sdk/go/transport/{rest,grpc,mcp}/timeseries_retention_test.go`;
`sdk/go/transport/embedded/handlers_timeseries_{retention,query}_test.go`.
