### Fixed

- **A fresh job whose pull request was already open gets a worker for its
  round.** Such a job never spawned one — `resumeReview` sent it straight to
  the gate — and when the gate asked for a round (a review's findings, a
  settled red pipeline) the turn was refused as "there is no worker session
  to prompt", the job failed, and the task was re-dispatched into the same
  refusal (go-quicbaas#53, 2026-09-21). A requested round now spawns the
  worker through `initializing`, as a resumed run always did.
