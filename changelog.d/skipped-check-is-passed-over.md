### Fixed

- **A skipped CI job no longer blocks review and merge forever.** Gitea
  Actions posts a job behind a false `if:` as its own `skipped` state; the
  forge adapter read every unrecognised state as "still running", so the
  gate reported the head as unfinished on every sweep. go-quicbaas's Docker
  job is skipped on every pull request: PR #970 sat fully green for an hour
  with no reviewer dispatched (2026-09-21). `skipped` is now a finished
  check with its own status, passed over by both gates — neither red nor
  green — and never triaged as a failure; a head whose every check was
  skipped is "no check covers this head", not clear.
