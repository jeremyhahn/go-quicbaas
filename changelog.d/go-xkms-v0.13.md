### Changed

- **go-xkms bumped to v0.13.0-alpha**, which unblocks consumers waiting on the
  xKey unlock-convergence fix.

  It is not a mechanical bump. v0.13 deleted the SDK's re-export of the storage
  layer's database surface — key-value stores, engines, watches, leases,
  indexes, time series — on the grounds that go-xkms does not ship that layer
  and must not expose it. The types are now imported from go-qrdb's own SDK
  directly, which is what this project's SDK-boundary rule said to do anyway.

  `BackendTypeThreshold` is gone and deliberately not replaced by a local
  constant: upstream removed it because the threshold backend did not do
  threshold cryptography, and re-adding the name here would restore exactly
  that misrepresentation. A configuration naming it is now refused as an
  unknown backend. `BackendTypePhone` is added, matching upstream.
