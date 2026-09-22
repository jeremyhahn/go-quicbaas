### Added

- **Every orchestrator list endpoint pages, sorts and searches.** Jobs,
  tasks, sessions, agents, workflows, projects and accounts each answered
  with their whole collection: the burn-in tenant's job list is ~2,000
  records and the UI loaded all of them on every visit, which the browser
  could not render. All seven now take the same `limit` (25 by default,
  capped at 100), `offset`, `sort=<field>[:desc]` over a documented
  allow-list, and `q` case-insensitive substring search, and answer with
  `total` / `limit` / `offset` / `next_offset` beside the records key they
  always used — so a consumer that ignores paging reads the response
  unchanged. A sort field an endpoint does not serve is a 400 naming the
  field and the allow-list rather than a silently unordered answer, and
  `next_offset` is absent on the last page. The jobs listing also gained the
  `statuses`, `runner` and `project_id` filters it had no way to express;
  every filter composes with the search and the page. One `Page` request,
  one `PageResult` envelope and one `ApplyPage` serve all seven from
  `pkg/orchestration`, reached over REST, QUIC, Unix, MCP and the embedded
  transport, with `page.Params` / `page.Envelope` on the Go SDK's list
  requests and responses. Contract:
  `docs/software-factory/api.md#list-paging-sorting-and-search`.
