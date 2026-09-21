### Fixed

- **A duplicate triage job is refused at StartJob, not recorded as a failure.**
  A runner whose jobs are per-subject singletons (triage, review) acquires
  its lease inside the run, so starting a second job on a subject already in
  flight used to succeed, create the job, and fail it minutes later with
  "job lease: held by another job" — the triage backstop produced one such
  failed job every fifteen minutes for as long as a project's pass outran
  its interval. StartJob now applies the store's own admission rule to the
  subject's live lease and refuses synchronously, which is the answer the
  scheduler's "already running" branch was written for.
