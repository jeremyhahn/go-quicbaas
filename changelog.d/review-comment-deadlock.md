### Fixed

- **A review that declined to judge deadlocked its pull request permanently.**
  A `comment` verdict published a `pending` commit status, on the sound
  reasoning that it decides nothing and so must not go green. But pending is
  the shape of a review still *running*, and the review floor deliberately
  skips any head already carrying a status under the reviewer's context so that
  an in-flight review is never given a duplicate reviewer. A finished review
  that left pending behind therefore blocked the merge gate **and** the
  backstop that exists to recover the gate: nothing could re-review that head,
  ever. Measured on go-luks2 #202 — the round-2 commit answered the round-1
  finding, CI went green, and it then waited indefinitely on a review no code
  path could dispatch.

  Two changes close it. A `comment` now publishes `failure`, which is what
  `error` already published and what both mean to any reader: the reviewer
  finished and produced nothing that satisfies the gate. And the floor now
  treats a `pending` reviewer status older than the job stall bound as
  abandoned rather than in flight, so a reviewer killed mid-run no longer
  strands its head — the bound is shared with the engine's own "the agent is
  presumed gone" answer rather than being a second, independently drifting
  number. An undated pending status is never stale, so a forge that reports no
  timestamp does not cause a re-dispatch on every pass.
