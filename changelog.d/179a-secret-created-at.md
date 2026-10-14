### Fixed

- **The Secrets API reports when a secret was written, not when it was read
  (go-quicbaas#179).** `GET /api/v1/secrets/<path>` returns the stored write
  time of the returned version as `created_at`. `GET /api/v1/secrets`
  returns the first version's write time as `created_at`, the latest
  version's as `updated_at`, and the real `current_version` (it was always
  1). The fix is in the secrets service, which every transport passes
  through unchanged. The times come from the per-version records the store
  already keeps, so
  secrets written before this fix show their real write times. A read whose
  version history cannot be loaded now fails, and the list leaves such a
  secret out, instead of reporting the time of the read.
