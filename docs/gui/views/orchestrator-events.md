# Orchestrator → Live Events

## Purpose

The Live Events view streams job and session events in real-time using Server-Sent Events (SSE). Use this view to monitor the activity of running jobs, sessions, and orchestration operations as they happen. Events include job state changes, session prompts, task claims, and errors.

## Layout

The page displays a live event log with two sections:

**Header** — "Live Events" title with "Real-time orchestration activity stream" subtitle. Controls:

- **Pause/Resume** button — pause the event stream (live events stop appearing)
- **Clear** button — clear all events from the current view (does not affect the daemon's event log)
- **Auto-scroll** toggle — enabled by default; disables when user scrolls up to read old events

**Event Stream** — Vertically stacked cards or rows, newest at the top:

```
[Timestamp] [Event Type] [Status Badge] [Details]

14:32:01    job_started          ✓       Job a1b2c3d4 started on backend claude
14:31:55    session_spawned      ✓       Session xyz789 opened
14:31:50    task_claimed        ✓       Task tsk_123 claimed by agent-01
14:31:45    inference_completed  ✓       Inference finished in 2.34s
14:31:40    job_escalated        ⚠       Job needs human approval
```

Each event shows:

- **Timestamp** — ISO 8601 or relative time (e.g., "5 seconds ago")
- **Event Type** — a fixed set of event kinds (job_started, session_spawned, task_claimed, etc.)
- **Status badge** — checkmark (success), warning triangle, or X (error)
- **Details** — human-readable description of what happened

**Empty State** (on first load):

```
Waiting for events...

Events from jobs, sessions, and tasks will appear here as they occur.
```

**Error State** (if stream disconnects):

```
Event stream disconnected.

The connection was lost. Check your network and refresh the page.

[Refresh] [View Logs]
```

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Pause Stream | Pause | Pauses incoming events (UI does not refresh) | — (client-side only) |
| Resume Stream | Play | Resumes listening for events | — (client-side only) |
| Clear Log | Trash | Clears all events from the current view | — (client-side only) |
| Click Event | ChevronRight | Expands event details; may navigate to related resource | `GET /api/v1/orchestrator/{resource}/{id}` |

## Walkthrough

### Monitor a job from start to finish

1. Open the Live Events view (`#/orchestration/events`).
2. Ensure the **Auto-scroll** toggle is ON and **Pause** is not active.
3. Open a new browser tab or terminal and start a job:

```bash
quicbaas orchestrator jobs start \
  --backend claude \
  --description "Test job"
```

Or use the GUI: go to Jobs view, click **+ Start Job**, and create one.

4. Return to the Live Events view.
5. You should see events appear in real-time:

```
[14:35:20] job_created       ✓  Job abc123 created
[14:35:21] job_assigned      ✓  Job assigned to agent-01
[14:35:22] job_executing     ✓  Job started execution
[14:35:25] job_escalating    ⚠  Job requires approval (optional)
[14:35:30] job_completed     ✓  Job completed in 10s
```

**Expected outcome:**

```
✓ Events appear as the job progresses
✓ Timestamps increment
✓ Final event shows "job_completed" with green checkmark
```

6. Click one of the job events to expand its details:

```
Job abc123 Completed

State: completed
Duration: 10.23s
Output: {...json payload...}
Backend: claude
Agent: agent-01

[View Full Job] [View Related Events]
```

7. To pause the stream while reading old events, click **Pause**.

**Expected outcome:**

```
✓ New events no longer appear
✓ "Resume" button becomes active
✓ You can scroll up to read history without new events pushing you down
```

8. Click **Resume** to continue listening.

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Event stream disconnected message appears | Network connection lost or daemon is unreachable | Check network; wait for daemon to recover; refresh the page |
| No events appear even though jobs are running | Event stream is paused | Click **Resume** to restart listening |
| Events are delayed by 10+ seconds | Network latency or daemon is overloaded | This is expected under high load; check daemon logs |
| Event details show `[object Object]` instead of readable text | Serialization error in the frontend | Refresh the page; report issue if persists |
| Clicking an event does nothing | Navigation feature not yet implemented | Use the Jobs or Sessions view to see full details |

## See also

- [Orchestrator → Jobs](orchestrator-jobs.md) — monitor and manage jobs
- [Orchestrator → Sessions](orchestrator-sessions.md) — attach to a specific session's event stream
- [handlers_session_events.go](../../pkg/api/rest/handlers_session_events.go) — event stream implementation (REST endpoint: `GET /api/v1/orchestrator/sessions/{sessionID}/events`)
- [Orchestrator → Workflows](orchestrator-workflows.md) — workflows generate events
