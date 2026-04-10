# Cost Controls

Token budget tracking for orchestration jobs, with per-job and per-agent limits.

Source: `pkg/orchestration/cost_tracker.go`, `pkg/orchestration/cost_tracker_store.go`

## Overview

The CostTracker monitors token consumption across all orchestrated AI agents in a job. It provides budget status checks, per-agent breakdowns, and configurable actions when budgets are approached or exceeded.

## CostTracker Interface

```go
type CostTracker interface {
    RecordTokenUsage(ctx, tenantID, jobID, agentID string, tokens int64) error
    GetJobUsage(ctx, tenantID, jobID string) (*UsageReport, error)
    GetAgentUsage(ctx, tenantID, jobID, agentID string) (*UsageReport, error)
    CheckBudget(ctx, tenantID, jobID string) (BudgetStatus, error)
}
```

## Budget Status

| Status | Constant | Condition |
|--------|----------|-----------|
| `ok` | `BudgetOK` | Usage < 80% of budget |
| `warning` | `BudgetWarning` | Usage >= 80% of budget |
| `exceeded` | `BudgetExceeded` | Usage >= 100% of budget |

The warning threshold is fixed at 80% (`budgetWarningThreshold = 0.80`).

## Budget Actions

Configured via `budget_action` in workflow settings:

| Action | Behavior |
|--------|----------|
| `warn` | Log a warning. Job continues. |
| `pause` | Pause all agents. Requires manual resume. |
| `shutdown` | Gracefully shut down all agents in the job. |

## Usage Report

```go
type UsageReport struct {
    TotalTokens int64            // Total tokens consumed
    BudgetLimit int64            // Configured budget limit
    PercentUsed float64          // (TotalTokens / BudgetLimit) * 100
    ByAgent     map[string]int64 // Per-agent token breakdown
    LastUpdated int64            // Unix nanos of last update
}
```

## KV Storage Layout

Token counters are stored as big-endian int64 values:

| Key Pattern | Description |
|-------------|-------------|
| `cost:{tenant}:job:{job}:total` | Aggregate job token count |
| `cost:{tenant}:job:{job}:agent:{agent}` | Per-agent token count |
| `cost:{tenant}:job:{job}:budget` | Budget limit for the job |

Budget limits are cached in memory (`sync.Map`) to reduce KV read pressure.

## Configuration

```yaml
settings:
  cost_controls:
    max_tokens_per_job: 1000000     # Total token budget for the entire job
    max_tokens_per_agent: 200000    # Per-agent token limit
    budget_action: warn             # warn | pause | shutdown
```

## Example: Checking Budget in a Control Loop

```go
status, err := costTracker.CheckBudget(ctx, tenantID, jobID)
if err != nil {
    // BudgetNotSetError if no budget configured
    return err
}

switch status {
case orchestration.BudgetWarning:
    logger.Warn("job approaching budget limit")
case orchestration.BudgetExceeded:
    logger.Error("job exceeded budget")
    // Apply configured budget_action
}
```

## Error Types

| Error | Sentinel | Condition |
|-------|----------|-----------|
| `BudgetExceededError` | `ErrBudgetExceeded` | Job total >= budget limit |
| `BudgetNotSetError` | `ErrBudgetNotSet` | No budget configured for job |
| `UsageRecordError` | `ErrUsageRecord` | Failed to write token count |
| `UsageRetrievalError` | `ErrUsageRetrieval` | Failed to read token count |
| `CostInvalidInputError` | `ErrCostInvalidInput` | Missing/invalid input field |
