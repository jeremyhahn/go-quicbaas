### Added — faceted filters, a visible sort affordance, and a pager at both ends of every list

- **Every list page can now be narrowed by the things it is made of.** The
  deployed Tasks and Jobs pages offered a handful of chips — All /
  Auto-spawned / Queued, Active / Completed / Failed — and no way at all to
  ask for one project's blocked tasks or one agent's failed jobs, which is
  the question an operator actually arrives with. `FilterBar`
  (`cmd/quicbaas/frontend/src/components/core/FilterBar.svelte`) renders one
  chip group per facet, each chip carrying the COUNT of records behind it, so
  "how many jobs failed" is answered before anything is clicked. Tasks narrow
  by status, dispatch, project and agent; jobs by state, runner, project and
  agent; sessions by status, backend, role, project and agent; workflows by
  mode and project; the fleet by status and backend; projects by dispatch;
  accounts by status and provider.
- Status on Tasks and state on Jobs are MULTI-select, because the endpoints
  take `statuses` and "pending or blocked" is one question rather than two
  lists to reconcile by eye. A bounded vocabulary renders as chips; an
  unbounded one — a project, an agent — renders as a picker, because a chip
  per project is a thousand chips.
- **The counts come from the answer and degrade rather than disappear.** A
  listing's `facets` member is counted over the searched set BEFORE its own
  filters, which is what makes a count useful: it says what picking a value
  would do. A daemon that sends no `facets` gets the declared vocabulary
  without counts, and a value the server counts that this build never
  declared is still offered — a state added server-side does not become
  unaskable until the UI is rebuilt.
- **The pager renders at the top of every table as well as the bottom**, from
  one piece of state, with the `1–25 of 1,987` label and the rows-per-page
  selector at the top. A pager only under twenty-five rows is below the fold
  on every laptop in the building, which is how a paged table gets reported
  as having no pagination at all.
- **A sortable header says so.** Every sortable column carries a sort icon —
  neutral when unsorted, an arrow when sorting — with a hover state and a
  "Sort by …" tooltip; an unsortable header carries none, which is how the
  two are told apart without clicking.
- **A narrowed list is a link.** Filters join page, size, sort and search in
  the route hash under the table's own key —
  `#/tasks?tasks_status=pending,blocked&tasks_project=<id>` — so a filtered
  view can be pasted into an issue. `routeTasks(projectId)` now builds
  `?tasks_project=`, the table's own facet, rather than the parallel
  `?project=` grammar it used to invent; the chip that shows the narrowing is
  the control that clears it.
- **Drill-down.** Ids are links: a task's project opens that project's tasks,
  its agent opens the agent, its session and job ids open the record. A
  detail page's Back returns to the list AS IT WAS LEFT — narrowed, sorted,
  on the operator's page — rather than to a bare route that throws away the
  filters they used to get there.

### Fixed — an agent's detail page lists what ran on THAT agent

- The Sessions and Jobs panels of `#/agents/{id}` sieved the store's
  collection by BACKEND: every session on `claude`, whichever host it ran on.
  Two agents sharing a backend — the normal case — each showed the other's
  work. Both panels now ask the server for `agent_id`, paged, which is a
  filter the listings have and this page was not using.

### Added — the TypeScript SDK spells every listing's own filters

- `TaskPageParams` gains `agent_id` and `job_id`, `JobPageParams` gains
  `agent_id`, `SessionPageParams` gains `project_id`, `job_id`, `runner` and
  `agent_id`, and `AccountPageParams` gains `status`. The workflows and
  projects listings get `WorkflowPageParams` (`project_id`, `mode`) and
  `ProjectPageParams` (`autonomous`) instead of sharing the bare `PageParams`
  and silently dropping anything a caller passed.
- `PageResult` gains `facets` — the per-field count of the matched set —
  ABSENT rather than empty when the endpoint counted nothing, because "nobody
  counted" and "every value has none" are different answers. `adoptFacets` is
  exported for a consumer normalising its own fetch.
