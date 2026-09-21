### Fixed

- **A worker session whose context is full is replaced, not failed.** In
  print mode Claude Code cannot compact; after a long delivery turn the
  round's re-prompt was refused as `blocking_limit` — which the CLI itself
  files under `context_limit` — and the attempt was failed and refunded,
  then re-run from scratch into the same wall (go-quicbaas #51, #965). The
  runner now commits and pushes what the dead session held, records the
  head, spawns a fresh session on the same branch with a note saying why, and
  goes on with the same round and findings; once per round. `blocking_limit`
  and `rapid_refill_breaker` leave the refunded (environmental) set.
