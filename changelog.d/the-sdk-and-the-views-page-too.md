### Fixed

- **The web UI no longer presents the first 25 records as the whole
  collection.** The orchestrator list endpoints started paging and defaulting
  to 25 records, and the TypeScript SDK's list methods took no paging
  parameters and threw the envelope away — so every table in the UI would have
  shown one page, labelled it "25 of 25", and offered no way to the rest. The
  SDK grew a paged verb per listing (`listJobsPage`, `listTasksPage`,
  `listSessionsPage`, `listAgentsPage`, `listWorkflowsPage`,
  `listProjectsPage`, `listAccountsPage`) returning the server's envelope —
  items, `total`, `limit`, `offset`, `next_offset` — behind one params type and
  one result type shared by every resource. The Jobs, Tasks, Sessions and
  Workflows tables now ask the daemon for each page, and their filters, sorts
  and searches go with the request instead of narrowing the rows in hand: a
  filter applied to a page of twenty-five reports a total it cannot know and
  hides the matches sitting on page two. Sorting offers only the fields the
  endpoint orders by; a column it cannot sort no longer pretends to. A 200 that
  is not a listing is a typed error rather than an empty table, and a failed
  read no longer invites the operator to "create their first one".

- **A session says which role it played, on every page.** The Role badge
  resolved a session's role by finding its job in the job list the store
  happened to hold; the sessions listing carried no `job_id` at all, so the
  badge was blank for every row, and once the jobs endpoint paged there would
  have been no list to find the job in either. The listing now carries the job
  AND the runner that drove it — resolved server-side for the page being
  answered, memoised per job — and the badge reads that one field. A session
  belonging to no job, or to one no longer on record, shows no badge rather
  than a guess. The model beside it was dropped by the REST client's field
  mapping and is carried now too.

- **Opening a session from page two of the Sessions table finds it.** The
  session detail route looked the routed id up in the list the store held, and
  a paged list holds twenty-five — so a row opened from a later page answered
  "Session not found" for a session that plainly exists. The route now reads
  the cache first and asks the server once when it misses.

### Added

- **Two list filters a paged consumer cannot work without.** `job_ids` on
  `GET /api/v1/orchestrator/jobs` narrows the listing to the jobs a caller
  names, so a page of tasks resolves the jobs its rows point at in ONE request
  rather than by listing the tenant or asking once per row; `auto_spawn` on
  `GET /api/v1/orchestrator/tasks` is the dispatch-mode filter the Tasks view
  offers, tri-state so that "queued only" and "no preference" stay different
  questions. Both are honoured on REST, the embedded transport and the Go SDK;
  `job_ids` is also an argument of the `quicbaas_orchestrator_list_jobs` MCP
  tool. A non-boolean `auto_spawn` is a 400 naming the parameter.
