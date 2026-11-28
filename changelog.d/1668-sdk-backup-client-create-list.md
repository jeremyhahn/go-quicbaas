### Fixed

- The Go SDK backup client's `Create` and `List` send and read the backup service's request and response types, so they work over the embedded and REST transports. `Create` takes an empty `BackupRequest` and refuses `Destination`, `Compress` and `Encrypt` with `InvalidInput`, because the service writes to its configured backup storage with its configured settings. `BackupResponse` gains `Status` (#1668).
