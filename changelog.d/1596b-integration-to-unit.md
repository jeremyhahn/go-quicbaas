### Fixed

- The integration harness no longer sleeps: `test/integration/helpers` gains
  `Poll`, a bounded ticker-based wait that returns a typed error at its
  deadline, and every helper sleep became a `Poll` or a timer select (#1596).
- `TestManagerConcurrentPutGet` in `pkg/catalog` reports each phase's errors
  before the next phase starts, so a failing Put phase fails the test instead
  of filling the error channel and hanging `wg.Wait` (#1596).
- `TestWorkerManager_CloseJoinsInFlightWork` in `pkg/catalog` keeps the
  worker's work gated until Close has cancelled it and reads the worker's
  exit state when Close returns, so it fails if Close stops joining the
  work (#1596).
- `waitForContainerStopped` in `test/integration/helpers` no longer takes a
  failed or deadline-cancelled `docker inspect` for a stopped container: only
  inspect's own "no such container" counts, and other failures reach the
  caller as a typed `ContainerInspectError` (#1596).

### Changed

- Integration suites that needed no container moved to the unit tier and their
  `test/integration` copies are gone: catalog, storage, embedding, factors,
  fleet_admin, kvstore, security and service. Lifecycle, project and
  object storage are split: their in-memory cases run in
  `pkg/server`, `pkg/service` and `sdk/go/transport/embedded`, while the cases that need real storage
  stay in `test/integration`: releasing recovered persistent tenant
  databases and joining the KMS key-cache goroutine after a real Start
  (`lifecycle`), and tenant backend selection and catalog
  locations over a PebbleDB catalog and several backends (`storage/object`),
  and the refusal of a dangling host affinity by a server whose Start wired
  the project service to its fleet registry (`project`). Every standalone
  case starts a server over PebbleDB in a real directory, so all of them
  (KV round trips, restart recovery, secrets, scheduler, passphrase-sealed
  start and the no-plaintext-on-disk scan) stay in `test/integration/standalone`.
  The moved cases run in `pkg/catalog`, `pkg/embedding`, `pkg/kvstore`,
  `pkg/server`, `pkg/service`, `sdk/go` and `sdk/go/transport/embedded`; new embedded tests also cover IAM user, role
  and tenant CRUD and the vector list, batch-delete, metadata-update,
  search-by-ID, hybrid-search and compaction-progress handlers. The
  `integration-test-embedding` target is removed (#1596).
