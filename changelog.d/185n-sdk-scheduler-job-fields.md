### Added

- **The SDK scheduler job types carry what the scheduler service returns
  (go-quicbaas#185).** `JobInfo` gains `Handler`, `Priority` and
  `LastError`. `CreateJobRequest` gains `Priority`. `ListJobsOptions` gains
  a `Handler` filter and a `Cursor`, and `ListJobsResponse` gains
  `NextCursor`. `TriggerJobResponse` gains `Status`, and the pause, resume
  and cancel responses gain `OldStatus` and `NewStatus`. The new
  `DeleteJobWithOptions` sends `Force` and returns the job ID, `Deleted` and
  the deletion time. Every transport fills these fields: REST, gRPC, QUIC,
  MCP, Unix and embedded. A parity test checks each one against the real
  server stack.
