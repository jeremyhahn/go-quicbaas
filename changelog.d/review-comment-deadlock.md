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

- **The reviewer's own commit status decided whether the head was reviewable.**
  Both dispatch paths ask "is this head green enough to review" by evaluating
  every status on the head — including the one the reviewer itself publishes
  there. That is circular: it asks a review to be green before it has run.
  While the floor short-circuited on status presence the question never
  arose, so the fault was latent; making a stale pending status reviewable
  exposed it immediately, and mapping a comment to `failure` would have
  re-created the same deadlock in a different colour. The two dispatch sites
  now exclude the reviewer's context, which is what the sweep's own
  documentation already promised ("every *other* check"). The merge gate still
  counts it, because there a failed review is exactly what should hold a
  change back.
