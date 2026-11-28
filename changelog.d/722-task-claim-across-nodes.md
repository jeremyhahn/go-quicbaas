### Fixed

- A pending task is claimed once across nodes. Every write of an existing
  task record now commits through the store's compare-and-swap against the
  bytes it read, so when two nodes claim one task the second write loses,
  re-reads the record and observes the first node's claim instead of
  overwriting it (#722).
