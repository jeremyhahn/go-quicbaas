### Fixed

- A session whose durable record is CLOSED but whose event log never ended
  (#201) now gets its ending written: at daemon boot, and on the next
  `CloseSession` for it. Attachers replaying such a session now see
  `session_closed`, and the log's gate is shut. `CloseSession` on a closed
  session now returns `unavailable` while that log cannot be read, instead of
  reporting success over an unended log.
