### Added

- **Every table in the web UI is now one paged, sortable, searchable table.**
  No list surface paged: each of twenty-odd views rendered its own `<table>`
  over the whole collection with whatever search and sort somebody wrote that
  day, so the burn-in tenant's ~2,000 jobs went into the DOM in full before the
  Jobs page painted, and an operator met different controls on every page. The
  new `DataTable` renders 25 rows at a time with a 25/50/100 selector, a
  first/previous/next/last pager reading "1–25 of 1,987", three-state sorting
  on the columns a view declares, and a search box debounced at 250 ms over
  the columns it declares searchable — all keyboard-reachable, with the
  current page, size, sort and search kept in the route hash so a reload or a
  shared link lands where it was left. A view hands it either a collection or
  a loader; a loader answering an envelope with `total` puts the table in
  server mode and it asks for each page, and an answer without one is paged
  locally, so the UI works on both sides of the orchestrator list endpoints
  growing `limit`/`offset`/`sort`/`q`.

### Fixed

- **A session now says which role it played.** Session records carry no role,
  and a worker's transcript discussing "the review's findings" reads exactly
  like a reviewer's — the owner read one and concluded the wrong model was
  reviewing code. The session list and session detail now show a Role badge
  (`worker`, `reviewer`, `triage`, or the runner's own name) taken from the
  runner of the session's job, with the backend and model beside it. A session
  that belongs to no job, or to a job nobody has read, shows no badge rather
  than a guess.
