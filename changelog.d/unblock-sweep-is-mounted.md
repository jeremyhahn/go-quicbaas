### Fixed

- **The unblock sweep was never run.** It was written, documented and tested —
  a pass that re-examines every blocked task, checks the condition it parked
  on, and returns the ones whose condition has cleared through the engine's
  readmission verb — and nothing anywhere constructed it. So a task that
  blocked on a checkable condition stayed blocked however completely that
  condition cleared, and the only thing that ever returned one to the queue was
  a person. A fleet parks tasks faster than a person clears them, which is how
  every project ends up with nothing runnable: eight blocked tasks stood across
  two projects, the oldest from 09-09.

  It had no tenant-wide entry point either, which is the mechanical reason it
  could not be mounted beside the other backstops. It now has one, shaped like
  the review floor's, and runs at boot and on a ten-minute ticker — slower than
  the review floor because the latency it bounds is cheaper: a finished change
  waiting to merge costs more than a task whose blocker cleared a few minutes
  ago.

  It is wired to the engine's **service**, not its store. Only the service
  announces a readmission, and that announcement is what takes
  `factory:blocked` back off the issue — a sweep wired to the store would
  return the task to the queue while the issue still said it was blocked.
  `NewUnblockSweeper` refuses the store by name for exactly this reason.
