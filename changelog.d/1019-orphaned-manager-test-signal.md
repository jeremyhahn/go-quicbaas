### Added

- **`cache.CacheManager.Done()` and `kms.KeyCache.Done()` report when a
  manager's background cleanup goroutine has exited.** Each channel is closed
  only when that goroutine exits, and `Close` waits for it, so an open channel
  means the goroutine is still running. A cache manager with no L1 TTL starts
  no goroutine, and its `Done()` is closed from the start.
  `CacheManager.Close` now waits on that channel instead of a WaitGroup.

### Fixed

- **`TestServer_Shutdown_Orphaned{Cache,KMS}Manager_IsReleased` no longer
  flake (#1019).** The old tests counted goroutines across the whole process
  that were sitting in the cleanup frame. The count missed a goroutine that
  had been created but not yet scheduled. Parallel tests starting and stopping
  their own managers also changed it. Each test now watches the `Done()`
  channel of the one manager it orphans.
