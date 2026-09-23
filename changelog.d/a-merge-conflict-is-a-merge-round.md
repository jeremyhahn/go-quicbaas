### Changed

- The factory never force-pushes. An approved pull request that conflicts
  with its base is now a merge round, not a rebase round: the
  `merge_conflict` prompt tells the worker to `git fetch origin <base>`,
  `git merge origin/<base>`, resolve each conflict keeping both sides'
  intent, commit the merge and re-run verification. The worker charter
  forbids amending, rebasing or resetting a published commit. Every
  publication, including the mid-attempt push on an ephemeral workspace, is
  a plain push; `--force-with-lease` is gone. A push git refuses as
  non-fast-forward now surfaces as `PublishCommandError`. Owner ruling
  2026-09-04 (handbook version-control §5b); go-quicbaas#1051.
