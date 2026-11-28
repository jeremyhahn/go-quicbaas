### Fixed

- A synchronous IAM audit write that the node's audit sink does not record
  now fails closed: `NodeAuditLogger.Log` returns the sink's
  `*NodeAuditWriteError` (wrapped) instead of reporting the event as
  recorded. `LogAsync`, which cannot return an error, logs the failure
  instead of discarding it (#827).
