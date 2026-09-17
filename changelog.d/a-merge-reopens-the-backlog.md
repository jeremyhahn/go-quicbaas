### Fixed

- **A parked issue was parked for ever, so every project ran out of work.** A
  triage verdict is an answer about a *tree* — "blocked on another change",
  "the prerequisite has not landed", "not actionable yet" — and the only thing
  that could reopen one was a person commenting on the issue. Nothing about the
  repository changing reopened anything, so a backlog the factory parked stayed
  parked however much the code moved underneath it. Measured: 23 of 23 open
  issues across two bound projects sat `factory:escalated` / `needs-info` /
  `blocked`, every verdict predating the day's fixes, while triage woke every
  fifteen minutes, found nothing, and slept.

  Every verdict now records the commit it was formed against, and a parked
  issue is re-judged when the project's default branch has moved since — which
  is exactly the event that makes "blocked on another change" wrong. A verdict
  carrying no commit is re-examinable, so a backlog parked by an older build
  gets one fresh reading.

- **A triage job recorded neither its backend nor its model**, so the queue
  showed rows that could not tell an operator whether a sweep was about to
  spend a local GPU turn or a frontier-model one. Both are now set when the
  job is started.

- **A merge no longer re-asks a question a person owes.** `needs-info` names a
  specific thing only a person can supply, so movement in the repository says
  nothing about it; reopening those on every merge spent a model turn per
  parked issue to reach the same verdict again, which on a parked backlog is
  most of the fleet's triage cost. Only `not-actionable` and escalated
  verdicts — the ones that are answers about a tree — are reopened by a merge.

- **A verdict now replaces the previous disposition instead of stacking on
  it.** Each disposition only ever added its label, so an issue judged
  needs-info after being escalated carried both: two contradictory answers,
  with no way for an operator to tell which was current. Six of eleven open
  go-luks2 issues were in that state after a single re-triage pass.
