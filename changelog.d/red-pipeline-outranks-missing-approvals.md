### Fixed

- **A pull request with a red pipeline and no review no longer waits for a
  review that cannot come.** The gate reported "0 of 1 approvals" before it
  looked at the checks, and the review sweep dispatches no reviewer until
  the checks are green — so a settled red sat in `reviewing` until the stall
  detector ended the job (go-quicbaas PR #971, 2026-09-21). A settled red is
  now reported ahead of the approval count, which is what sends it to the
  worker as a CI-red round; checks that have not finished still wait behind
  the approvals. The delivery runner's `executing`, `validating` and
  `reviewing` states also carry idle bounds sized to the work (2 h / 2 h /
  4 h) instead of the engine's default.
