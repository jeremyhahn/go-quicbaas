### Fixed

- **The embedded transport routes Secrets GetVersion, ListVersions and
  Rollback** (#1560). The SDK secrets client sent all three, and the embedded
  transport answered each with `UnknownMethodError`. A version that is not
  positive is refused as invalid input before it reaches the service.
