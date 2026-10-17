### Fixed

- **The JS integration server binds the compose network.**
  `sdk/javascript/test/integration/quicbaas.yaml` sets `server.host: 0.0.0.0`
  so the test-runner container can reach `central:8080`. The default
  `127.0.0.1` accepted loopback only. The bind is for the isolated compose
  network only.

### Changed

- **The JavaScript scheduler client speaks the server's REST contract.**
  - `createJob` requires `handler`, validated client-side. A delayed job
    requires `delaySeconds`.
  - New request options: `handler`, `executeAt` (Date or RFC 3339 string),
    `delaySeconds`, `priority`, `timeoutSeconds`, `labels` (string map),
    `nodeAffinity`, `targetTenantId`. `listJobs` adds `cursor`, `handler`
    and `targetTenantId`. `getJobHistory` adds `cursor`.
  - `updateJob` sends `PUT` (the server has no `PATCH` route).
  - `createJob` returns the full job. `deleteJob` returns `{ jobId, deleted }`.
    Pause, resume and cancel return `{ jobId, previousStatus, newStatus }`.
    `triggerJob` returns `{ jobId, status, triggeredAt }`. `listJobs` and
    `getJobHistory` return `nextCursor` and `hasMore`. `getStatus` nests its
    counters under `stats`. Execution records carry `endedAt` and `output`.
    Job durations (`delayMs`, `timeoutMs`) are converted from nanoseconds.

### Removed

- Scheduler request options the server has no field for:
  - `createJob`: `runAt` (use `executeAt`), `delayMs` (use `delaySeconds`),
    `timeoutMs` (use `timeoutSeconds`), `tags` (use `labels`), `metadata`,
    `retryDelayMs`.
  - `updateJob`: `timeoutMs` (use `timeoutSeconds`), `tags` (use `labels`),
    `metadata`, `retryDelayMs`.
  - `listJobs`: `offset` (use `cursor`), `type`, `tags`.
  - `getJobHistory`: `offset` (use `cursor`), `status`, `startTime`,
    `endTime`.
- Response fields the server never sends: `success` and `message` on delete
  and on job actions, `executionId` on trigger, `totalCount` on history,
  and `completedAt`, `result` and `attemptNumber` on executions.
