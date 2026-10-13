### Fixed

- **gRPC streaming reads report service errors (go-quicbaas#1284).**
  `List`, `Query`, `ScanIndex` and `TimeSeriesQuery` used to log a failed
  service call and still end the stream with OK, often after sending no rows.
  A gRPC caller could not tell "no rows" from "the server failed". These
  calls now end with a non-OK status. Rows streamed before the failure are
  still delivered. Request errors (not found, invalid input, unauthorized,
  forbidden, conflict) keep their message. Every other error class keeps its
  status code, but its message is replaced by "the request could not be
  completed", the same rule REST applies, and the cause is logged on the
  server.
