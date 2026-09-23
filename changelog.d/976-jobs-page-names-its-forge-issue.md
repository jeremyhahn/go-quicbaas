### Fixed

- **The jobs listing now names the forge issue behind every row, on every
  page.** `JobStatus` gained `source` — the origin of the TASK the job is an
  attempt at, in the task's own `{kind, ref, url}` shape — and
  `ListJobsPage` fills it for the page it answers, memoised per task. The
  Jobs view's `#N` chip read this from `orchestratorStore.tasks`, which since
  the listings became server-paged holds ONE page of twenty-five tasks: the
  chip rendered for whichever jobs happened to have their task on that page
  and blanked for the rest, which on page 2 of jobs is nearly every row. No
  task filter could express "the tasks behind these jobs", so the view had no
  second request to fall back on either. The chip now reads the row it is
  rendering, and needs no tasks loaded at all.

- The projection runs **after the page is cut**, which is the one thing
  `ListJobsPage` does beyond its unpaged twin plus `ApplyPage`: a tenant with
  two thousand jobs names nearly as many tasks, and resolving them all to
  answer a page of twenty-five would make the page cost the collection. It is
  memoised by task, so a task's whole attempt history costs one task read. A
  task that cannot be read leaves the row with no source rather than failing
  the page — the same rule `runner` follows on the sessions listing.

- The field is carried by **every** transport: REST (and the QUIC and Unix
  transports that delegate to it), the embedded transport (on `JobSummary` and
  on `GetJobStatus`'s `JobStatus`), the MCP `quicbaas_orchestrator_list_jobs`
  and `quicbaas_orchestrator_get_job_status` tools (which omit the key
  entirely when the job's task records no origin, so "no issue" reads
  differently from "blank issue"), the Go SDK, the TypeScript SDK, and
  gRPC — `OrchestratorJobSummary`
  and `OrchestratorGetJobStatusResponse` gained a `source` of the same
  `TaskSource` message a task already carries. `GetJobStatus` fills it
  alongside the rest of the task provenance, so the detail read and the
  listing agree.

- The gRPC listing takes no page fields, so it now reads the PAGED verb with
  a nil page: that is the verb the projection lives in, and a nil page cuts
  nothing and orders by started_at descending — the order the unpaged listing
  already sorted itself into. Same jobs, same order, with the source on them.
  Answering the whole listing costs one task read per DISTINCT task in it, on
  a call that already materialises every job the tenant owns; the request's
  `task_id` narrows the jobs before the projection runs, so a consumer that
  wants to pay less asks for less.
