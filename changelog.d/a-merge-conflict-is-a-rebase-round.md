### Fixed

- An approved pull request that conflicts with its base is now sent back to
  the worker as a rebase round on the same branch. Before this, the merge was
  retried until the attempts ran out and the attempt was charged as a failure
  of the work. Gitea answers "Please try again later" both while it checks a
  pull request and when the pull request conflicts, so the merge is still
  retried within its bound. When the bound is spent, a pull request that is
  still not mergeable on the approved head is reported as
  `MergeConflictError`. The runner then prompts the worker (`merge_conflict`)
  to rebase onto the latest base, keep both sides' intent and re-run
  verification. The new head is reviewed again, and the round counts against
  `max_review_rounds`, so a pull request that keeps conflicting still
  escalates. Measured on go-truststrap#64 (2026-09-22); go-quicbaas#1007.
