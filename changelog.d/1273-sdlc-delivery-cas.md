### Fixed

- **SDLC version guards are decided by the store (go-quicbaas#1273).**
  `pkg/sdlc` no longer guards bindings, deliveries, train entries and train
  ledgers with the qrdb DAO's conditional save, which read the version and
  wrote in a separate step, so concurrent writers holding the same version
  all succeeded and every write but the last was lost. A create now lands
  only through `PutIfAbsent` and an edit only through `CompareAndSwap` over
  the bytes the writer read: exactly one contender wins and the rest get
  `VersionConflictError`. `PutDelivery` and `PutBinding` re-read and retry
  when they lose, so concurrent replacements each get their own version. A
  tenant store without atomic writes is refused with a `BackendError`
  carrying the new `ErrGuardedWriteUnsupported`. Records keep their key
  layout and JSON encoding, so existing data reads back unchanged.
