### Fixed

- **A GDPR export or immediate deletion whose final status write fails is
  marked failed (go-quicbaas#559).** The background worker used to drop the
  error from `GenerateExport` and `ProcessDeletion`, which left the request
  pending or processing with nothing to pick it up again. A request that
  already reached a final status, a cancelled deletion included, keeps it.

### Changed

- **Production `//nolint` directives drop from 39 to 13, each a proven
  false positive with its linter and reason named (go-quicbaas#559).**
  Stale directives are gone. Unchecked `sync.Map` type assertions in the
  policy cache and the sharded client are now checked, helpers that only
  tests called are deleted, and constants that tripped gosec's credential
  heuristic by name alone are renamed.
