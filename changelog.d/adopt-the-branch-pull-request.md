### Fixed

- **A re-adopted issue no longer races a rival branch against its own open pull
  request.** A delivery is keyed by task and a task is terminal, so an issue
  whose first task ended is adopted again as a new task with a fresh delivery
  that knows nothing of the pull request the old one opened. The branch name is
  planned from the issue, so it is the same name; the remote branch still
  carries the old commits; and the new attempt cut its branch from the base
  instead. Every push was then refused — `! [rejected] (non-fast-forward)` —
  and each refusal was charged to the *work*, so the task spent its whole
  attempt budget failing at something the work had nothing to do with. That
  made re-labelling an orphaned issue not merely useless but harmful. A
  delivery holding no pull request now adopts one already open on its branch,
  takes that pull request's head as its own, and goes to review instead of
  working again.
