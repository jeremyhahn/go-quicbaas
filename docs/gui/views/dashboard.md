# Dashboard

## Purpose

The Dashboard is the landing page (route `#/`) and provides a system-wide health overview at a glance. It displays daemon uptime, service status (daemon, orchestrator, storage, KMS, scheduler), and high-level resource counts (sessions, agents, jobs). Use this page to quickly assess the health of the entire QuicBaaS deployment and identify any degraded subsystems.

## Layout

The Dashboard is structured as a full-width card layout with two main sections:

**Top Section — Service Health**

- **Daemon** — liveness check via `GET /api/v1/health/live`. Shows status badge (ok/degraded/down) and uptime in human-readable format (e.g., "2m 52s").
- **Orchestrator** — health check via `GET /api/v1/orchestrator/health`. Clickable: navigates to `#/orchestration` on click.
- **Storage (PebbleDB)** — data layer health. Clickable: navigates to KMS view if enabled.
- **KMS** — key management service status. Clickable: navigates to `#/security/kms`.
- **Scheduler** — background job scheduler health. Clickable: navigates to `#/scheduler`.

Each service shows a status badge (green checkmark for "ok", yellow triangle for "degraded", red X for "down").

**Bottom Section — Resource Counts**

Four stat cards displayed side-by-side (or stacked on small screens):

- **Sessions** — count of active sessions; clickable link to `#/orchestration/sessions`
- **Agents** — count of registered agent daemons; clickable link to `#/orchestration/agents`
- **Jobs** — count of queued/running orchestration jobs; clickable link to `#/orchestration/jobs`
- **Workflows** — count of defined workflows; clickable link to `#/orchestration/workflows`

Each stat card shows an icon (Session = MessageSquare, Agent = Bot, Job = Briefcase, Workflow = Workflow) and the numeric count.

## Lifecycle actions

The Dashboard is read-only. All actions navigate to the relevant view:

| Action | Navigate to | Endpoint |
|--------|-------------|----------|
| Click Daemon uptime | —  | `GET /api/v1/health/live` |
| Click Orchestrator | Orchestration | `GET /api/v1/orchestrator/health` |
| Click Sessions count | Sessions list | `GET /api/v1/orchestrator/sessions` |
| Click Agents count | Agent Fleet | `GET /api/v1/orchestrator/agents` |
| Click Jobs count | Jobs list | `GET /api/v1/orchestrator/jobs` |
| Click Workflows count | Workflows list | `GET /api/v1/orchestrator/workflows` |

## Walkthrough

### Create your first session and view it in the dashboard

1. Open the Dashboard (`#/`).
2. Note the current **Sessions** count (e.g., 0).
3. Click the **Sessions** count card to navigate to the Sessions view.
4. Click **+ Spawn Session** and follow the [Sessions walkthrough](orchestrator-sessions.md#walkthrough) to create a session.
5. After spawning, navigate back to the Dashboard (`#/`).
6. Refresh the page (Ctrl+R or Cmd+R).
7. The **Sessions** count should now show 1.

**Expected outcome:**

```
✓ Dashboard loads with system health displayed
✓ All service badges show "ok" (green)
✓ Sessions count updates after spawning
✓ Uptime increments as time passes
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Service badge shows "degraded" | Daemon or subsystem is under load or recovering | Check logs via CLI: `quicbaas health live` |
| Service badge shows "down" (red X) | Daemon is unreachable or crashed | Restart the daemon: `quicbaas daemon start` |
| Counts show 0 but I created resources | Counts cache is stale | Refresh the page |
| Health check times out | Network connectivity issue | Check network connectivity; verify daemon is running |
| Cannot click service badges | Service link is disabled for down services | Fix the underlying service first |

## See also

- [CLI: quicbaas health](../cli/health.md) — command-line health checks
- [Orchestrator → Sessions](orchestrator-sessions.md) — detailed sessions walkthrough
- [Orchestrator → Agent Fleet](orchestrator-fleet.md) — detailed agent registration walkthrough
