### Fixed

- **A secret write reports the version the backend assigned
  (go-quicbaas#179).** The secrets service used to predict the next version
  from the size of the history and store that guess in the secret's
  metadata. Concurrent writers could report the same version, and after a
  version was removed the guess fell behind the backend. Store, rotate,
  rotation-policy and status writes now take the version from the backend's
  write and no longer put a version into the metadata. The API, wire shapes
  and tenant scoping are unchanged.
- A secrets manager that cannot report the version of a write, or a read
  whose metadata carries no version, is now an internal error
  (`SecretVersionNotReportedError`) instead of a silent fallback. The
  `pkg/secrets` backends implement `VersionedBackend`, and `Manager` gains
  `StoreVersion` and `StoreVersionWithOptions`.
