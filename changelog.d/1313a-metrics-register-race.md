### Fixed

- **Building the same metric concurrently no longer panics
  (go-quicbaas#1313).** `NewCounter`, `NewGauge` and `NewHistogram` in
  `pkg/util/metrics` checked the metric cache and registered the collector
  under separate locks, so two callers that missed the cache together both
  registered and the second panicked with `duplicate metrics collector
  registration attempted`. All six constructors now share one helper that
  re-checks the cache and registers under a single write lock, so every
  caller gets the same collector. Metric names, the registry and the
  wrong-type cache eviction are unchanged.
