### Fixed

- **SDLC deletes remove only the record they checked (go-quicbaas#1273).**
  Deleting a binding, merge-train entry, reviewer override (one or all of a
  project's) or issue cluster read the record, checked it was the right one,
  then deleted the key without a guard, so a write landing between the read
  and the delete was erased although it had been acknowledged. Each delete
  now goes through `CompareAndDelete` over the bytes it read. A record
  changed after that read is kept and the delete returns
  `VersionConflictError`; a cluster copy re-filed into a newer cluster is left
  to it; a record another delete already removed counts as deleted. A tenant
  store without atomic deletes is refused with a `BackendError` carrying
  `ErrGuardedWriteUnsupported`.
