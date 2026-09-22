### Changed

- A job waiting in `reviewing` no longer holds one of its project's
  `max_concurrency` slots (go-quicbaas#978, owner ruling 2026-09-22). The
  quota now bounds only the states that bear a worker — initializing,
  executing, validating, re-delegating and completing. A runner declares its
  worker-free states with `JobStateSpec.OffProjectBudget`; the sdlc runner
  declares reviewing, cleanup and escalating. The envelope moves the job's
  lease off the budget on entering such a state and back on through the
  project's admission on leaving it. A review or CI round asked for while the
  project is full **waits** for a slot, and is queued ahead of new work
  (`JobLease.AwaitingSlot`), rather than failing. The lease, the one job per
  task, and the stall detector's bounds are unchanged. On the burn-in fleet two
  jobs parked on CI and codex review held both of go-quicbaas's slots, and
  the next ready task sat pending while no worker ran anywhere.
