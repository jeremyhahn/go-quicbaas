### Changed

- **The `quicbaas scheduler job` and `scheduler status` commands go through
  the SDK (go-quicbaas#185).** They call `Async().Scheduler()` on the SDK
  client the global `--transport` selects, so they behave the same on every
  transport. The CLI's own per-transport scheduler clients are deleted.
  Output changes: `job create` prints only the new job's `id`, `name` and
  `next_run`. The job view drops `target_tenant_id`, `created_by` and
  `execute_at`. `job list` drops `timestamp`. `job trigger` prints `job_id` and `status`. `scheduler status`
  shows `Uptime` instead of `Started At`, and its JSON keys are now
  `running`, `total_jobs`, `running_jobs`, `success_count`, `failure_count`,
  `uptime` and `last_tick`. `--output raw` is listed in the help.

### Removed

- **`scheduler job history --cursor`.** The SDK's job history has no cursor.
  When more executions exist, the command says so; raise `--limit` to see
  them.
