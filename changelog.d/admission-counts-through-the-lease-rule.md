### Fixed

- **A triage or review job no longer consumes a worker slot.** The lease
  store's rule is that every reader counts a lease through
  `CountsAgainstBudget`, which honours the off-budget flag triage and review
  jobs carry — but the admission plan, the count the dispatcher actually
  uses, counted `Live` leases instead, project-wide and fleet-wide. A project
  whose backlog was being triaged ran at half its quota for the whole pass:
  measured on a freshly onboarded project (2026-09-21), "inflight=2 slots=0"
  against a quota of 2 with one worker running beside a 50-issue NPU triage
  pass. Both counts now go through the lease's own rule.
