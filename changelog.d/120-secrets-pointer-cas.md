### Fixed

- **Secrets pointer updates are decided by the store (go-quicbaas#120).**
  The internal secrets backend made only a path's first publish and Destroy
  conditional; a Store, soft Delete or Rollback that updated an existing
  pointer wrote it unconditionally, so two writers in different processes
  could both be acknowledged and the last one silently won. Every pointer
  update now goes through `CompareAndSwap` against the pointer bytes the
  writer read. The loser gets `ErrWriteConflict` and its reserved version
  entry is removed. A store without guarded writes still takes an
  unconditional write and needs a single writer process.
