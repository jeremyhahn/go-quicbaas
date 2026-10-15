### Removed

- **`quicbaas_orch_job_budget_utilization` is gone (go-quicbaas#1058).**
  Nothing set the gauge, so it never emitted a value, and its `job_id` label
  gave it unbounded cardinality. The `Metrics.OrchJobBudgetUtilization` field
  and `Metrics.SetOrchJobBudgetUtilization` are removed, as is its row in the
  orchestration metrics table.
