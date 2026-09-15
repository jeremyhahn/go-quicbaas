### Fixed

- **A task no longer abandons the pull request it opened.** A delivery is keyed
  by task, so it outlives the attempt that created it — but every attempt ran
  the worker regardless. When an attempt ended for a reason that was not the
  work's fault *after* its pull request was opened, the next attempt was handed
  a workspace with that branch checked out; the worker read the tree, found the
  change already present and reported `satisfied`, exactly as its contract
  says; and the conclusion seam completed the task as `already_satisfied` on
  the premise that "the work was already present, so there is nothing to
  merge". That premise holds for a stale issue and is false here — the pull
  request was still open, and completing the task left it with nobody to merge
  it, ever. Three of the burn-in fleet's nine open pull requests were stranded
  that way, two carrying review verdicts nobody would act on and one approved
  and gate-cleared. An attempt whose task already has an open, unmerged pull
  request now resumes it — straight to the review state, worker not re-run —
  and the forge is asked rather than the delivery record, so a pull request
  merged or closed by a person since is not reviewed again.
