## Ejected from merge train #${train}

Review approved this task's pull request, and it was queued to land on
`${target}` in merge train #${train} together with other approved changes. The
train's pipeline ran on `${target}` with the queued changes merged on top in
order: it passed without this change and failed with this change added. So the
change was taken out of the train, and the others landed without it.

What failed is your change combined with the latest `${target}` and the
changes queued ahead of it, which your own pipeline never saw:

${findings}

1. Fetch and merge the latest base first: `git fetch origin ${target}`, then
   `git merge origin/${target}`. The failure may depend on changes that have
   landed there since your branch was cut. Do not rewrite, reset or amend any
   commit already on your branch, and never force a push.
2. Fix the failure on your branch so the build and tests pass on the result.
3. Leave the result committed. The factory publishes it when your turn ends;
   the new head is reviewed again and rejoins the queue once it is approved.
