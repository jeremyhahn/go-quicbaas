# Async → Scheduler

## Purpose

The Scheduler view manages cron-like jobs that run on a configurable schedule. Use this view to create recurring jobs, trigger them immediately for testing, pause and resume scheduling, edit job definitions, and delete jobs. The scheduler persists job state and executes them reliably across daemon restarts.

## Layout

The page displays a list of scheduled jobs with filter and search:

**Header** — "Scheduler" title with "Cron jobs and recurring tasks" subtitle. Primary CTA is **+ New Job** button.

**Filters and Search** — Above the table:

- **Status filter** — "All", "Enabled", "Disabled", "Error". Defaults to "All".
- **Search input** — client-side filter on job ID and description.

**Jobs Table** — Columns:

- **Job ID** — unique identifier (e.g., `job_daily-backup`)
- **Description** — human-readable name
- **Schedule** — cron expression (e.g., `0 2 * * *` for 2 AM daily)
- **Status** — badge: "enabled" (green), "disabled" (gray), "error" (red)
- **Last Run** — ISO 8601 timestamp or "Never"
- **Next Run** — ISO 8601 timestamp
- **Actions** — three-dot menu with: Run Now, Pause/Resume, Edit, Delete

**Empty State**:

```
No scheduled jobs.
Create your first job to get started.

[+ New Job button]
```

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Create Job | Plus | Opens a form to define a new scheduled job | `POST /api/v1/scheduler/jobs` |
| Run Now | Play | Triggers the job immediately (bypasses schedule) | `POST /api/v1/scheduler/jobs/{id}/trigger` |
| Pause Job | Pause | Disables the job; no future runs until resumed | `POST /api/v1/scheduler/jobs/{id}/pause` |
| Resume Job | Play | Re-enables a paused job | `POST /api/v1/scheduler/jobs/{id}/resume` |
| Edit Job | Edit | Opens the job definition for inline modification | `PUT /api/v1/scheduler/jobs/{id}` |
| Delete Job | Trash | Removes a job permanently; shows a confirm modal | `DELETE /api/v1/scheduler/jobs/{id}` |
| View History | History | Shows past execution logs (if not yet implemented, note as missing) | `GET /api/v1/scheduler/jobs/{id}/history` |

## Walkthrough

### Create your first scheduled job

1. Open the Scheduler view (`#/async/scheduler`).
2. Click **+ New Job** button.
3. A form modal appears:

```
Create Scheduled Job

Job ID:         [daily-backup_______________]  (auto-gen: job_<name>)
Description:    [Daily database backup_____]
Schedule:       [0 2 * * * ▼]  (cron expression; 2 AM daily)
                Preset: [Every hour] [Daily] [Weekly] [Monthly] [Custom...]

Type:           [Command ▼]  (or Webhook, Script, Orchestration Job)

Command Configuration:
  Command:      [/usr/local/bin/backup.sh]
  Args:         [--db prod --retain 30d]
  Timeout:      [1 hour ▼]

Advanced:
  Retry on Failure:    [Enabled ▼]  Max Retries: [3]
  On Success:          [Notify via Email]
  On Failure:          [Notify via Email + Slack]

                [Cancel] [Create]
```

- **Job ID** — unique identifier; auto-generated from name
- **Description** — human-readable purpose
- **Schedule** — cron expression (or use presets)
- **Type** — Command, Webhook, Script, or Orchestration Job
- **Configuration** — depends on type
- **Advanced** — retry policy and notifications

4. Fill in the form:
   - **Job ID**: `daily-backup`
   - **Description**: `Daily database backup`
   - **Schedule**: Keep the preset "Daily" (0 2 * * * = 2 AM daily)
   - **Type**: `Command`
   - **Command**: `/usr/local/bin/backup.sh` (or use a mock command for testing)
   - **Args**: `--db prod --retain 30d`
   - **Timeout**: `1 hour`
   - **Retry**: Enabled with 3 max retries

5. Click **Create**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Job created: daily-backup"
✓ New job appears in the table with status "enabled" (green)
✓ "Next Run" shows a future timestamp (2 AM tomorrow if it's after 2 AM today)
```

### Run a job immediately

6. Find your new job in the table.
7. Click the three-dot menu and select **Run Now**.
8. A confirm modal appears:

```
Run Now?

This will execute the job immediately, bypassing the schedule.

[Cancel] [Run Now]
```

9. Click **Run Now**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Job triggered: daily-backup"
✓ "Last Run" timestamp updates to "now"
✓ Job status may briefly show "running" (or change indicator)
✓ After execution, status returns to "enabled"
```

### Pause and resume a job

10. Click the three-dot menu and select **Pause Job**.
11. A confirm modal appears:

```
Pause Job?

daily-backup will not run on its schedule until you resume it.

[Cancel] [Pause]
```

12. Click **Pause**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Job paused: daily-backup"
✓ Job status badge changes to "disabled" (gray)
✓ "Next Run" becomes "—" (no scheduled run)
```

13. Click the three-dot menu and select **Resume Job**.

**Expected outcome:**

```
✓ Success toast: "Job resumed: daily-backup"
✓ Status badge returns to "enabled" (green)
✓ "Next Run" updates to the next scheduled occurrence
```

### Edit a job

14. Click the three-dot menu and select **Edit Job**.
15. A modal opens with the current job definition:

```
Edit Scheduled Job: daily-backup

Description: [Daily database backup__________]
Schedule:    [0 2 * * * ▼]  (cron expression)
Type:        [Command ▼]
Command:     [/usr/local/bin/backup.sh]
Args:        [--db prod --retain 30d________]
Timeout:     [1 hour ▼]

             [Cancel] [Save]
```

16. Modify the **Schedule** to run more frequently (e.g., `0 * * * *` for every hour instead of daily).
17. Click **Save**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Job updated: daily-backup"
✓ Table updates: Schedule now shows "0 * * * *"
✓ "Next Run" recalculates based on the new schedule
```

### Delete a job

18. Click the three-dot menu and select **Delete Job**.
19. A confirm modal appears:

```
Delete Job?

daily-backup will be permanently removed. This cannot be undone.

[Cancel] [Delete]
```

20. Click **Delete**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Job deleted: daily-backup"
✓ Job disappears from the table
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Create Job form shows cron validation error | Cron expression is invalid | Use a preset (Daily, Weekly, etc.) or check cron syntax (e.g., `0 2 * * *`) |
| Run Now button is disabled | Job is already running or paused | Resume the job first if paused; wait for current execution to complete |
| "Last Run" shows an error status | Job execution failed | Check daemon logs; verify the command exists and is executable |
| "Next Run" is not updating | Scheduler is paused or daemon is not running | Check daemon status: `quicbaas health live` |
| Pause button is disabled | Job is already paused or being executed | Wait for execution to complete before pausing |
| Edit does not save | Required field is missing or cron is invalid | Fill in all fields; verify cron syntax |
| Delete fails with "Job is running" | Job is currently executing | Wait for execution to complete or click "Run Now" to interrupt (if supported) |

## See also

- [CLI: quicbaas scheduler](../cli/scheduler.md) — manage scheduler jobs via CLI
- [handlers_scheduler.go](../../pkg/api/rest/handlers_scheduler.go) — scheduler implementation
- [scheduler/README.md](../scheduler/README.md) — scheduler architecture and configuration
- [scheduler/api-reference-rest.md](../scheduler/api-reference-rest.md) — scheduler REST API
- [Async → Messaging](async-messaging.md) — pub-sub messaging
