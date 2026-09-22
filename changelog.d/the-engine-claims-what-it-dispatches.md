### Fixed

- **A task the engine dispatches reads `in_progress` while its job runs, not
  `pending`.** `ClaimTask` is the edge an *agent* takes, so a job the engine
  spawned for a task claimed nothing and the task stayed queued for the whole
  life of its attempt: measured on the burn-in node, a task adopted at 02:13:49
  and merged at 02:28:06 read `pending` for those fourteen minutes and went
  straight to `completed` (go-truststrap#14, 2026-09-21). `Task.Status` is what
  the API, `orchestrator task get`, the task list and the label reconciler all
  read, so a busy hour showed every running delivery as queued, the reconciler
  reported `LabelReconcileUndecidable` three times per delivery on evidence that
  was wrong about the world, and a fourteen-minute-old *pending* task is also
  the signature of a stranded binding — a healthy run and a real stall were
  indistinguishable by the record. The spawn claim now takes the task
  `pending -> in_progress` with the job as the claimant, through the same
  transition table every other edge is checked against, in the same
  version-checked write that records the job; a claim given back returns the
  task to the queue, and a conclusion still moves it to
  `completed`/`failed`/`blocked` (go-quicbaas#966).
