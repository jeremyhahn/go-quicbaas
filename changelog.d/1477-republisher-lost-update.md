### Fixed

- **A job's workspace pin and surfaced park no longer go missing from its
  status (go-quicbaas#1477).** A JobRun status publish that raced a workspace
  pin being recorded, or a park being surfaced, could store a snapshot built
  before the change, overwriting the republish that carried it, so `Status()`
  lacked the pin or showed the park unannounced until the next publish. The
  publish now copies the pin and park under the lock the fan-out fix
  (#1474) introduced, and the pin and park writers hold that same lock from
  the change to the republish. The park is no longer read by a publish while
  it is being written. `JobRun.Parked()` now returns a copy taken under that
  lock, so the conclusion no longer reads the park while it is re-parked or
  surfaced.
