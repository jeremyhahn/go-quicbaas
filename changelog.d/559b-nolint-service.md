### Fixed

- **The service layer no longer suppresses linter findings; it fixes them
  (go-quicbaas#559).** Integer conversions in `pkg/service` are now checked or
  saturating instead of wrapping. As a result:
  - a KV read timeout too large for a duration is rejected as invalid input;
  - secret versions beyond the platform `int` are rejected as invalid input;
  - a negative key or secret version reported by a backend is an internal
    error rather than a huge unsigned number;
  - list, backup and time-series page sizes and limits beyond `int` mean "all"
    instead of wrapping negative, which used to panic or return nothing;
  - out-of-range time-series entity IDs read as absent.

  One justified `//nolint` remains in `pkg/service`: G101 on an environment
  variable name.
