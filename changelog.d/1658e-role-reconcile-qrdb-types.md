### Changed

- **Built-in role reconciliation reports go-qrdb's own result types** (#1658).
  `pkg/iam` no longer keeps a copy of go-qrdb's reconcile result and report;
  the REST reconcile response now always carries `created` for each changed
  role.
