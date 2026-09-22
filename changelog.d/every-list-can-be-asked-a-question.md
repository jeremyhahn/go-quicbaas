### Every list endpoint can be filtered, faceted and drilled into

Paging (#968) made the listings answerable; it did not make them
**askable**. A page of twenty-five jobs newest-first is not how an operator
finds the two that agent-3 failed, and the Tasks view could not answer "what
is this daemon working on" at all — a task record names no agent.

Every orchestrator listing now takes the narrowings its view offers, and every
list response carries the counts a filter bar renders beside each choice:

- **tasks** — `statuses`, `project_id`, `agent_id`, `job_id` (plus the
  existing `auto_spawn`), and `sort=attempts` for "which tasks have burned the
  most attempts";
- **jobs** — `agent_id` beside the existing `task_id`, `runner`, `project_id`,
  `statuses` and `job_ids`, and `sort=agent_id`;
- **sessions** — `project_id`, `job_id`, `runner` and `agent_id` beside
  `backend` and `status`, and `sort=runner`;
- **agents** — `status` and `backend`, where `backend` is MEMBERSHIP: a daemon
  serving claude, codex and opencode from one record matches all three;
- **workflows** — `project_id`, `mode`; **projects** — `autonomous`;
  **accounts** — `provider`, `status`.

`agent_id` on tasks and `runner`/`agent_id` on sessions name something the
record does not carry: they live on the attempt's job, and the daemon resolves
them in ONE pass over the tenant's jobs rather than one read per row.

**The counts are standard faceted search**, and the order of operations is the
contract: search, THEN count, THEN narrow. Selecting `statuses=blocked` leaves
the counts beside `pending` and `completed` intact, so an operator can change
their mind without clearing the filter first — `total` is what was selected,
`facets` is what each other choice would yield. A listing the server did not
page carries no counts: an internal sweep renders no filter bar and must not
pay for one.

**An unknown filter value still matches nothing rather than failing** — an
agent id may legitimately name no record — with two deliberate exceptions that
are now `400` with a typed error: a `statuses` value outside the state enum,
and a `sort` field outside the allow-list. A state the engine does not have
cannot be a question about this tenant's records, and answering "there are
none" would report the caller's typo as a fact.

Wired across REST, MCP, the embedded transport, the Go SDK (client + REST
transport) and the TypeScript SDK, with the facets under `facets` in every
envelope. `Task.agent_id` is derived on read and zeroed on write, like
`Account.Default`: it is the job's fact, and a copy persisted on the task
would be stale for every attempt after it.
