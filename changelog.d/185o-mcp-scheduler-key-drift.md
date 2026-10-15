### Fixed

- **The SDK MCP transport sends and reads the scheduler keys the server
  tools use (go-quicbaas#185).** Labels go out as an object, status and owner
  type as strings, timeouts as duration strings, and the history limit, the
  cancel reason, the delete `force` flag and `node_affinity` now reach the
  tools. Results are decoded through the scheduler service types, so tags,
  run and failure counts, timeouts, job history and scheduler status are no
  longer lost. The scheduler parity test now runs over MCP as well. The MCP
  reference in `docs/scheduler/api.md` lists `node_affinity`, the server's
  real required fields, and RFC3339 timestamps.
