# Orchestrator → Jobs

## Purpose

The Jobs view displays a filterable, searchable list of orchestration jobs queued, running, or completed on the system. Use this view to monitor long-running operations, check job status, and cancel running jobs. Each job shows its ID, description, state, start time, and end time (if completed).

## Layout

The page is structured as a full-width table layout:

**Header** — "Jobs" title with "Orchestration job queue and history" subtitle. Primary CTA is **+ Start Job** button (green).

**Filters and Search** — Above the table:

- **State filter dropdown** — "All", "Active", "Completed", "Failed". Defaults to "All".
- **Search input** — real-time client-side filter on job ID and description.

**Table** — Columns:

- **Job ID** — truncated to 8 chars + ellipsis if longer (e.g., `a1b2c3d4…`)
- **Description** — user-provided job description
- **State** — badge with color (executing = blue, completed = green, failed = red, escalating = yellow)
- **Started** — timestamp in local time
- **Ended** — timestamp or "—" if still running
- **Actions** — three-dot menu with "Cancel" option (if job is cancellable)

**Empty State** — If no jobs match filters:

```
No jobs found.
Try adjusting your filters or start a new job.
[+ Start Job button]
```

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Start Job | Plus | Opens a modal to configure and queue a new job | `POST /api/v1/orchestrator/jobs` |
| Cancel Job | Trash | Terminates a running job; shows a confirm modal | `DELETE /api/v1/orchestrator/jobs/{jobID}` |
| View Details | ChevronRight | Clicks the row to navigate to `JobDetailView` with full job logs and task breakdown | `GET /api/v1/orchestrator/jobs/{jobID}` |

## Walkthrough

### Start and monitor your first job

1. Open the Jobs view (`#/orchestration/jobs`).
2. Note the current job count (likely 0 initially).
3. Click **+ Start Job** button.
4. A modal opens with a form:
   - **Description** — enter a descriptive name (e.g., "Email batch process")
   - **Backend** — select a registered backend (e.g., "claude" if available)
   - **Other options** — check for host affinity or priority toggles
5. Click **Start**.

**Expected outcome:**

```
✓ Modal closes
✓ Job ID appears in the table
✓ State badge shows "executing" (blue)
✓ "Started" timestamp is current
✓ "Ended" shows "—"
```

6. Leave the page open and watch the state update (jobs poll every 2–5 seconds).
7. Once the job completes, the state badge changes to "completed" (green) and "Ended" timestamp is filled.
8. Try cancelling an active job: click the row's three-dot menu and select **Cancel**.
9. A confirm modal appears:

```
Cancel Job?

Job a1b2c3d4… will be terminated. This cannot be undone.

[Keep Running] [Cancel Job]
```

10. Click **Cancel Job**.

**Expected outcome:**

```
✓ Confirm modal closes
✓ Success toast: "Job a1b2c3d4 cancelled"
✓ Job state changes to "cleanup" or "failed"
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Cannot start a job; modal shows "No backends available" | No backends registered in this tenant | Go to Agent Fleet and register an agent; see [orchestrator-fleet.md](orchestrator-fleet.md) |
| Job stays in "executing" state indefinitely | Job is stuck or agent is unreachable | Cancel the job and check daemon logs: `quicbaas logs` |
| Cancel button is disabled/grayed out | Job is already completed or failed (not cancellable) | Only running or escalating jobs can be cancelled |
| Job ID is not clickable (no detail view) | The job detail view is not yet implemented | Use the CLI: `quicbaas orchestrator jobs status <job-id>` |

## See also

- [CLI: quicbaas orchestrator jobs](../cli/orchestrator.md#quicbaas-orchestrator-jobs-liststart) — start and list jobs via command line
- [Orchestrator → Workflows](orchestrator-workflows.md) — parameterized multi-step job execution
- [Orchestrator → Live Events](orchestrator-events.md) — real-time job event streaming
