### Fixed

- **Query results and query errors are no longer lost to a stream race
  (go-quicbaas#1284).** The query service closes its result channel before it
  returns, and several callers treated that closed channel as success.
  - REST `GET /api/v1/query/...` could return an empty list in place of a
    buffered result. It could also answer HTTP 200 for a failed query.
  - REST `POST /api/v1/query/range` answered HTTP 200 for a failed scan.
  - The embedded transport, which also serves QUIC and Unix, returned success
    for a failed List, Query, ScanIndex or TimeSeriesQuery stream.
  - The MCP `quicbaas_query` and `quicbaas_scan_index` tools closed the
    service's channel a second time and panicked.

  Every caller now waits for the service's verdict once the channel closes.
  The gRPC List, Query and ScanIndex methods still log a service error and
  report success. That is left for a follow-up.
