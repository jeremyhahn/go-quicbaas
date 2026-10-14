### Fixed

- **A secret read over the KV backend reports the version it returned
  (go-quicbaas#179).** The KV secrets backend now names the version it read
  in the returned metadata, as the memory and encrypted backends already
  did. `GET /api/v1/secrets/<path>` with no version therefore reports the
  version and `created_at` of the data it returns, even when a write lands
  during the read; before, it could report the newer version's number and
  time beside the older data. The stored entries are unchanged.
