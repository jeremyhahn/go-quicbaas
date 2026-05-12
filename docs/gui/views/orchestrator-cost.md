# Orchestrator → Cost Tracking

## Purpose

The Cost Tracking view displays per-job iteration usage and estimated costs based on backend billing. Use this view to analyze how many API calls or iterations each job consumed, which backends incurred the most cost, and to forecast spending over time. This is useful for budget tracking and optimization.

## Layout

The page is structured as a filterable table with two sections:

**Header** — "Cost Tracking" title with "Per-job iteration usage and estimates" subtitle. Controls:

- **Date range selector** — pick start and end dates (defaults to last 7 days)
- **Backend filter** — dropdown to filter by backend (all, claude, codex, ollama, etc.)
- **Export** button — download data as CSV

**Summary Cards** (top of page):

```
Total Cost (7d)     Backend Breakdown    Avg Cost/Job    Highest Cost Job
$47.32              Claude: $42.00       $8.24           Job abc123: $15.20
                    Codex: $5.32
                    Ollama: $0
```

**Cost Table** — Columns:

- **Job ID** — truncated (e.g., `abc123…`)
- **Description** — job description
- **Backend** — backend name
- **Status** — badge (completed, failed, cancelled)
- **Start Time** — ISO 8601 timestamp
- **Duration** — wall-clock time (e.g., "2m 34s")
- **Iterations** — count of API calls or prompts
- **Est. Cost** — estimated dollar amount (based on backend pricing)
- **Cost per Iteration** — average cost per call (cost / iterations)

**Empty State**:

```
No cost data for the selected date range.
Try adjusting the date range or running some jobs.
```

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Select Date Range | Calendar | Filters costs by date; triggers table refresh | `GET /api/v1/orchestrator/jobs?start=X&end=Y` |
| Filter by Backend | Sliders | Shows only jobs that used the selected backend | — (client-side filter) |
| View Job Details | ChevronRight | Click a row to navigate to the job detail page | `GET /api/v1/orchestrator/jobs/{jobID}` |
| Export to CSV | Download | Downloads table as CSV file for external analysis | — (client-side export) |

## Walkthrough

### Analyze costs for jobs from the past week

1. Open the Cost Tracking view (`#/orchestration/cost`).
2. The page loads with a default date range of "last 7 days" and shows a summary:

```
Total Cost (7d): $47.32
Backend Breakdown:
  Claude: $42.00 (7 jobs)
  Codex: $5.32 (2 jobs)
```

3. The table below lists all jobs with their costs:

```
Job ID      Description           Backend  Status     Start       Duration  Iterations  Est. Cost
abc123…     Email campaign v1     Claude   completed  14:35:20    2m 34s    12          $8.20
xyz789…     Code review task      Claude   completed  14:25:10    1m 15s    5           $3.80
def456…     Ollama test           Ollama   completed  14:15:00    0m 42s    1           $0.00
...
```

4. To filter by a specific backend, click the **Backend** dropdown and select "Claude".

**Expected outcome:**

```
✓ Table re-filters to show only Claude jobs
✓ Summary cards update to show only Claude costs
✓ "Total Cost" shows only costs from Claude
```

5. To view a longer date range, click the **Date Range** selector and choose "Last 30 days".

**Expected outcome:**

```
✓ Table refreshes with more jobs
✓ Summary cards update with larger totals
✓ Est. Cost column may show higher numbers
```

6. Click a job row to navigate to the full job details page and see:

```
Job Details: abc123…

Backend: Claude
Duration: 2m 34s
Iterations: 12
Tokens Used: {input: 1024, output: 512}
Est. Cost: $8.20
Pricing Breakdown:
  Input tokens: $0.003 × 1024 = $3.07
  Output tokens: $0.015 × 512 = $7.68
  API call: $0.01 × 1 = $0.01
  Total: $8.20
```

7. To export all visible costs for external analysis, click the **Export** button.

**Expected outcome:**

```
✓ Browser downloads a file named "quicbaas-costs-2026-04-18-to-2026-04-25.csv"
✓ CSV contains all visible rows with headers
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| No cost data appears, but I can see jobs in the Jobs view | Cost tracking is not enabled in the daemon config | Enable cost tracking: `quicbaas config set orchestrator.cost-tracking true` |
| All costs show $0.00 | Backend pricing is not configured | Set up backend pricing models in the config file |
| Date range selector shows no jobs | Date range is too narrow or no jobs ran in that period | Expand the date range or run more jobs |
| Export button shows "No data" | No rows are visible due to filters | Remove filters or adjust date range |
| Cost estimates are inaccurate | Pricing model does not match the actual backend | Update pricing in the config to match the backend's current rates |

## See also

- [Orchestrator → Jobs](orchestrator-jobs.md) — monitor and manage jobs
- [configuration/orchestrator.md](../configuration/orchestrator.md) — configure cost tracking and pricing
- [CLI: quicbaas orchestrator jobs list](../cli/orchestrator.md#quicbaas-orchestrator-jobs-liststart) — jobs via CLI
