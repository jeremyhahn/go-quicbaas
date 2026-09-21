### Fixed

- **A failed verification command is no longer mistaken for a vanished
  container.** The exec path read any stderr containing "not found" as the
  runtime saying the container was gone; a `make build` whose compiler
  diagnostics said "not found" was therefore reported as a missing
  container, the job — which had just fixed a review finding on go-quicbaas
  PR #970 — was failed and charged, and the round was lost (2026-09-21).
  Only the runtime's own phrasing ("No such container", "no such object",
  "Error response from daemon: not found") names a missing container now;
  a command's exit status and output are the command's answer.
