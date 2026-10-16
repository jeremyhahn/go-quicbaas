### Changed

- **The service registry refuses a standalone qrdb node with a typed error.**
  `registry.NewFromClient` now takes a context and checks the node's
  capabilities first: a node without the raft Lease, MVCC and Watch planes
  fails with `RegistryRequiresRaftError` naming its mode, reported as a failed
  precondition on every transport, instead of the transport's raw
  not-configured error from the first lease call. A standalone boot logs one
  startup line saying so. See `docs/service-registry/architecture.md#run-mode-raft-only`.
