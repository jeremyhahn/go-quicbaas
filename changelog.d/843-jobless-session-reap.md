### Fixed

- **Sessions bound to no job are now concluded once they go quiet
  (go-quicbaas#843).** Reconciliation used to end a session only through its
  job, so a session with no job stayed open forever. The maintenance sweep now
  concludes a session with no job once it has been quiet past the longest job
  idle bound, with reason `session_idle` and evidence such as `session has
  been quiet for 59h55m0s, past the idle bound of 3h0m0s, and is bound to no
  job`. A session is kept if its record carries the new persisted `keep`
  marker, if it is the node's lead session, or if its record shows a turn in
  flight. The sweep reads the stored record again before it writes the
  ending, so a session whose record shows a prompt, a keep mark or a job
  binding written in the meantime is not concluded. A prompt still running
  is not yet on the record, so a sweep during that turn can still conclude
  the session. Older records that carry a project name in `project_id`
  reconcile the same way.

### Added

- **`keep` on the spawn request (go-quicbaas#843).** `SpawnSessionRequest`
  takes an optional `keep` (proto field 35, JSON `keep`). It is persisted on
  the session record and keeps a session bound to no job past the idle
  bound. It changes nothing for a session bound to a job. The field is
  carried by REST, gRPC, QUIC, Unix, the embedded transport and the MCP spawn
  tool, and is exposed by the Go and TypeScript SDKs and by
  `quicbaas orchestrator spawn --keep`. Sessions spawned from Signal chat are
  kept.
