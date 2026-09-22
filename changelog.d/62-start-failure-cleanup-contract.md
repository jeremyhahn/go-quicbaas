### Fixed

- **A failed `Server.Start()` no longer strands the resources it had already
  built.** `Shutdown` now reaches the two whose owner may never exist after a
  partial start — the cache manager, created in `startService` before the
  service facade that closes it, and the qrdb audit sink plus seal strategy,
  created before the node they are injected into. Previously a `Shutdown`
  following a failed `Start` skipped all three, because each was guarded on
  the owner that the failure had prevented from being built, leaving a
  cleanup goroutine, an open audit file and a TPM session behind. The
  facade-owned path is untouched, so a successful boot tears down exactly as
  before.

- **Recovered tenant databases are now closed on shutdown.** `startDatabases`
  builds a qrdb `Manager`, and `Manager.OpenAll` opens every tenant database
  found on disk — but the manager was a local variable handed to a
  `DatabaseService` and on to the facade, and the facade's `Close` never
  reached it. Nothing did. So a `Start` that failed at any step after
  `startDatabases`, *and equally a `Start` that fully succeeded*, left a
  directory of open stores that no teardown path could close. The `Server`
  now takes ownership of the manager the moment it is built — before
  `OpenAll` opens the first database, so the window in which a handle exists
  without an owner is empty — and `Shutdown` closes it after the facade,
  whose `DatabaseService` routes into it.

- **A failed messaging start no longer orphans its notification hub.**
  `PushService.Start` launches the hub goroutine before its readiness wait,
  and both ways that wait can fail — a cancelled context, and a hub that
  never came up — returned with the goroutine still running and its context
  uncancelled. It was unreachable afterwards by construction: `Stop` returns
  immediately while `running` is false, and `running` is only set on success,
  while `Server.startMessaging` assigns `s.messagingService` only after
  `Start` returns, so `Shutdown` saw a nil field. `Start` now unwinds itself
  on both paths, cancelling the hub's context and *joining* the goroutine, so
  the hub is provably gone by the time the caller has the error and no
  teardown call is needed at all.

- **The job scheduler can no longer outlive the shutdown that stopped it.**
  The service facade started its `SchedulerEngine` from a goroutine, so a
  teardown that ran before that goroutine was scheduled — exactly what a
  `Shutdown` after a failed `Start` does — called `Stop` on an engine that
  was not running yet, was told "not running", and then watched the queued
  `Start` bring a poll loop up against storage the same teardown had already
  closed. The builder now starts the engine synchronously (`Start` launches
  its own loop and returns, so nothing is deferred by doing so), and
  `SchedulerEngine.Stop` is terminal even when it arrives before the matching
  `Start`, so a late `Start` refuses instead of resurrecting the loop. The
  error `Stop` returns for a not-running engine is unchanged; only the
  recorded state is.

- **The data-plane barrier's sealing strategies now have an owner.**
  `buildDataPlaneBarrierService` returned the pebble seal sub-store as its
  only `io.Closer`, so the strategies it built alongside were released by
  nobody: not the caller, which stores that closer and nothing else; not the
  barrier, which does not own its strategies; and not `stopQRDBNode`, whose
  `Server.sealStrategy` is a *different* instance from a separate build call.
  With a hardware seal strategy configured and node sealing disabled, that
  orphan holds an open TPM backend — and the `SealingStrategy` contract is
  that closing the strategy is what releases it — so a `Start` that failed
  after this point, and equally one that fully succeeded, left the TPM open
  for the life of the process. The builder now returns a single
  `dataPlaneBarrierResources` closer owning both the strategies and the
  store, every construction-error path releases what it had already opened,
  and `Shutdown` nils the field so a second call cannot re-close it.

- **The KMS manager is now closed on shutdown, so its key-material cache
  stops leaking a goroutine.** With `kms.cache.enabled` (the default),
  `startKMS` builds a `KMSService` whose `KeyCache` runs a cleanup goroutine
  for as long as the manager exists — and nothing downstream ever closed it:
  the service facade's `Close` does not, `MemoryCryptoManager.Close` is a
  no-op, and catalog's `KMSDEKProvider` only borrows the reference. The
  goroutine therefore survived a *clean* `Start`/`Shutdown` cycle as readily
  as a failed one. `Server.Shutdown` now closes the manager last, after the
  facade and the catalog workers that use it have stopped. Teardown is
  otherwise unchanged: `XKMSBackend.Close` is a documented no-op because xkms
  lifecycle is global, so this cannot pull the CA's shared key storage out
  from under anything.

### Added

- **The `Start()`-failure cleanup contract is stated where embedded callers
  read it** — `Server.Start`, `Server.Shutdown`, the Go SDK (`sdk/go/doc.go`,
  `sdk/go/README.md`), `docs/go-sdk/api.md` and `docs/composition/user.md`:
  `Start` does not unwind, so a non-nil error from it MUST be followed by
  `Shutdown`, and the deferred `Shutdown` therefore belongs immediately after
  `NewServer` rather than after a successful `Start`. Every embedded-server
  example is updated to that ordering, and the two in-tree embedded callers
  (`cmd/quicbaas`) now follow it.

### Testing

- **The cleanup contract is verified in both tiers, split by what each tier
  can actually run.** The unit half,
  `pkg/server/start_partial_failure_cleanup_test.go`, owns the RELEASE
  property: for every resource whose intended owner may never be built,
  `Shutdown` closes it and drops it. Each test hands the `Server` the orphan
  directly and calls `Shutdown`; **none of them calls `Start`**, and every
  server comes from a helper that clears both the pebble backend and the data
  directory, so the file runs entirely in memory as the Unit Test Contract
  requires. The data-plane barrier's own assembly is asserted there too,
  through a `newSealStoreFn` seam that substitutes an in-memory seal store —
  the same package-level-function-variable pattern `newCacheManagerFn` and
  `newSecretsMgr` already use.

  That split is forced rather than chosen: **a full `Server.Start()` cannot
  run in memory.** The embedded qrdb node refuses an empty Raft `DataDir`
  while the vector subsystem is compiled in, and `buildDataPlaneBarrierService`
  opens a pebble seal sub-store for any non-empty `Storage.DataDir`, ignoring
  `Storage.Backend` entirely. So every test that drives a real `Start` lives
  in `test/integration/lifecycle/`, which asserts the two things only a real
  boot over a real store can show: that a mid-`Start()` failure really lands
  in the window where the orphan exists, and that `Shutdown` afterwards joins
  the background goroutines and returns the OS handles — proved by goroutine
  frames, which cross the package boundary that unexported fields do not, and
  by re-recovering the seeded pebble database. It drives its failure from
  configuration rather than a seam, which is also the truer test: an embedded
  caller's `Start` fails that way, not by a swapped function pointer.

  Every assertion in both tiers was confirmed to fail with its fix reverted.
