## Merge conflict with ${base}

This task's pull request was approved at `${head}`, but it cannot merge.
`${base}` has moved on since your branch was cut, and the forge reports that
the two now conflict. This is not a review finding. Your change was accepted,
and the latest `${base}` now has to be merged into your branch.

1. Fetch the latest base: `git fetch origin ${base}`.
2. Merge it into your branch: `git merge origin/${base}`. Do not rewrite,
   reset or amend any commit already on your branch: it is published, and the
   factory never rewrites a published branch or forces a push over it.
3. Resolve every conflict by hand so that the intent of both sides is kept:
   what `${base}` changed and what this task changed. Never resolve a
   conflict by discarding a side — no `git merge -X ours`, no
   `git merge -X theirs`, no `git checkout --ours` or `--theirs` — and do not
   rewrite the task from scratch.
4. Commit the merge once every conflict is resolved.
5. Re-run the project's build and tests on the result. They must pass.
6. Leave the result committed on your branch and do not push it yourself.
   The factory publishes it with a plain push when your turn ends, and the
   new head is reviewed again before it merges.
