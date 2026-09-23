## Merge conflict with ${base}

This task's pull request was approved at `${head}`, but it cannot merge.
`${base}` has moved on since your branch was cut, and the forge reports that
the two now conflict. This is not a review finding. Your change was accepted,
and it now has to be put on top of the latest `${base}`.

1. Fetch the latest base: `git fetch origin ${base}`.
2. Rebase your branch onto it: `git rebase origin/${base}`. If the rebase
   cannot be finished cleanly, abort it and merge the base instead:
   `git merge origin/${base}`.
3. Resolve every conflict so that the intent of both sides is kept: what
   `${base}` changed and what this task changed. Do not drop either side to
   make a conflict go away, and do not rewrite the task from scratch.
4. Re-run the project's build and tests on the result. They must pass.
5. Leave the result committed on your branch and do not push it yourself.
   The factory publishes it when your turn ends, and the new head is reviewed
   again before it merges.
