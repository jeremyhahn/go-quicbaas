### Fixed

- **The embedded transport serves vector (go-quicbaas#1404).** Twenty
  `quicbaas.Vector` RPCs (namespaces, vectors, search, index and compaction
  admin, stats) now go to the embedded node's `VectorService` and stop
  returning `UnknownMethodError`. Vector ids have to be decimal uint64, and
  metadata filters accept only `eq`. Anything else gets a typed
  `INVALID_INPUT` error. Export and Import are still not served on embedded.
