### Fixed

- **KV List returns one page and honours `PageToken` on every transport**
  (#1534). `List` sorts entities by ID and returns at most `PageSize` of them
  (default 100) with `NextPageToken` and `TotalCount`; the next call resumes
  after the last ID of the previous page. A token that does not parse, or
  that points past the last entity, fails with `service.InvalidPageTokenError`
  (invalid input) instead of restarting at page 1. The service no longer
  streams every page in one call, and the embedded transport no longer joins
  pages; the MCP `list` tool now returns `nextPageToken` and `totalCount`.
