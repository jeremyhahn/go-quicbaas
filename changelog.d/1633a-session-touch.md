### Fixed

- IAM session Touch updates the session through
  `SessionManager.UpdateActivity` on MCP and the embedded transport. MCP no
  longer re-creates the session (which every call refused as already
  existing), and an empty `session_id` is refused before any service call.
  The embedded transport now routes `quicbaas.IAM.Session/Touch`. The server
  stamps its own clock; the client's `Timestamp` is not carried (#1633).
