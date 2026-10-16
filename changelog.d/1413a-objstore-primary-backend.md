### Fixed

- **Object storage put and copy without `target_backend` reach a backend
  (go-quicbaas#1413).** The server built its object store resolver with no
  primary backend, so these requests failed with `ErrBackendNameEmpty`. The new
  `object_storage.primary_backend` setting names the primary. When it is unset
  and exactly one backend is configured (including the automatic in-memory
  `default`), that backend is primary. Startup refuses an unknown name
  (`UnknownPrimaryBackendError`) and several backends without a primary
  (`PrimaryBackendUnresolvedError`, which names them).

### Changed

- **One object store resolver builder.** The reencrypt worker and the object
  storage service now share `Server.newObjectStoreResolver`. In both, the node
  `EnabledBackends` lists the configured storage backend names. The reencrypt
  worker's copy used to list KMS backend types, which nothing read.
