### Fixed

- **Lint suppressions outside the API, server, IAM, security and service
  packages are replaced by fixes (go-quicbaas#559, slice A).** 82 of 94
  `//nolint` directives are gone. Behaviour changes that come with them:
  - The Go code chunker no longer fails on every declaration with a doc
    comment, and the comment appears once per chunk.
  - The RAG file watcher rejects a malformed exclude glob at construction.
  - `Stop` reports a failed close of the underlying notifier.
  - Pipeline retry jitter is drawn from `crypto/rand`.
  - SDK transports refuse values too large for the wire field instead of
    truncating them.
  - The scheduler refuses an execution timestamp before the epoch.
  - Triage records a stale disposition label it could not remove on the
    job audit trail.

  The 12 directives that remain are false positives that a fix would only
  make worse: gosec G101 on token-named constants that are not credentials,
  `bodyclose` on `websocket.Dial`, and G301 on a fixture directory other
  containers must read.
