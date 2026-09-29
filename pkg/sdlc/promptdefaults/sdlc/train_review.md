## This pull request is merge train #${train}

It is not one change. It is merge train #${train}: the changes below, each
merged in order onto `${target}` at `${base}`, so that they land together
through this one pull request.

${members}

The issues they close between them: ${issues}.

Each of those changes was already reviewed on its own pull request and
approved by this project's reviewer at exactly the head named. Those approvals
stand. Do not review each change again in isolation, and do not refuse the
train for something one of them already carried when it was approved.

Review what only the combination can show: how the changes interact with each
other and with the current `${target}`.

- Two changes that are each correct alone but conflict in behaviour, in shared
  state, or in an assumption one makes about the other.
- One change altering a function, type, configuration key or file that another
  still uses the old way.
- Duplicated or contradicting implementations of the same thing, arriving from
  different changes.
- A merge that dropped, doubled or misplaced code. Compare the combined result
  with what each change intended.
- Tests from one change that no longer cover what they did once another landed
  beside them.

If a finding traces to one change in particular, name that change by its pull
request number, so the reader knows where the fix belongs.
