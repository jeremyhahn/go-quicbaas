# Orchestrator → Sessions

## Purpose

The Sessions view displays persistent ACP (Agent Conversation Protocol) sessions with stateful backends (Claude, Codex, OpenCode). Use this view to spawn new sessions, prompt active sessions, attach (replay history + tail live events), and close sessions when done. Sessions maintain conversation state across multiple prompts.

## Layout

The page displays a searchable, filterable list of sessions:

**Header** — "Sessions" title with "Active and historical sessions" subtitle. Primary CTA is **+ Spawn Session** button.

**Filters and Search** — Above the table:

- **Status filter** — "All", "Running", "Paused", "Closed". Defaults to "All".
- **Backend filter** — dropdowns to filter by backend name (e.g., "claude", "codex").
- **Tenant filter** — filter by tenant (if multi-tenant).
- **Search input** — client-side filter on session ID and description.

**Table** — Columns:

- **Session ID** — truncated UUID (e.g., `a1b2c3d4…`)
- **Description** — user-provided description or empty
- **Backend** — backend name (e.g., "claude")
- **Status** — badge: "running" (blue), "paused" (yellow), "closed" (gray)
- **Created** — ISO 8601 timestamp
- **Last Prompt** — timestamp of last prompt sent (or "—")
- **Actions** — three-dot menu with: Prompt, Attach, Close

**Empty State**:

```
No sessions.
Spawn a new ACP session to get started.

[+ Spawn Session button]
```

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Spawn Session | Plus | Opens a modal to create a new session with a chosen backend | `POST /api/v1/orchestrator/sessions` |
| Prompt Session | MessageSquare | Opens a prompt dialog; sends prompt to the active session; updates on submit | `POST /api/v1/orchestrator/sessions/{sessionID}/prompt` |
| Attach | Eye | Navigates to `SessionDetailView` (history + live tail via SSE) | `GET /api/v1/orchestrator/sessions/{sessionID}/events` |
| Close Session | X | Terminates the session; shows a confirm modal | `DELETE /api/v1/orchestrator/sessions/{sessionID}` |

## Walkthrough

### Spawn a session and send your first prompt

1. Open the Sessions view (`#/orchestration/sessions`).
2. Click **+ Spawn Session** button.
3. A modal form appears:

```
Spawn Session

Backend:     [Claude ▼]  (or Codex, OpenCode)
Host Affinity: [Any ▼]   (or select a specific agent)
Description: [_________________________________]
             (optional)

[Cancel] [Spawn]
```

4. Select **Claude** from the Backend dropdown (or another available backend).
5. Optionally select a **Host Affinity** to pin to a specific agent, or leave as "Any".
6. Optionally enter a **Description** (e.g., "Code review session").
7. Click **Spawn**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Session spawned"
✓ New session appears in the table with status "running" (blue)
✓ Session ID is displayed
```

8. Click your new session's row to select it.
9. In the row's three-dot menu, select **Prompt**.
10. A prompt dialog opens:

```
Send Prompt

Prompt: [_________________________________]
        [Clear] [Send Prompt]
```

11. Type a prompt (e.g., "explain QUIC in three sentences").
12. Click **Send Prompt**.

**Expected outcome:**

```
✓ Dialog closes
✓ Success toast: "Prompt sent"
✓ "Last Prompt" timestamp in the table updates to "now"
✓ (Optional) Attach to view the response in real-time
```

13. In the row's three-dot menu, select **Attach** to view the session history and live response.
14. The `SessionDetailView` opens showing:

```
Session History & Events

Session: a1b2c3d4…

[Your prompt]
You: explain QUIC in three sentences

[Live response streaming in]
Claude: QUIC is a modern transport protocol...
```

15. To close the session when done, go back to the Sessions list and click the row's three-dot menu → **Close Session**.
16. A confirm modal appears:

```
Close session a1b2c3d4…?

This session will be terminated. You cannot send more prompts to it.

[Keep Open] [Close Session]
```

17. Click **Close Session**.

**Expected outcome:**

```
✓ Confirm modal closes
✓ Success toast: "Session closed"
✓ Session status badge changes to "closed" (gray)
✓ Session is no longer in the "Running" filter
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Spawn Session form shows "No backends available" | No session-based backends registered (Claude, Codex, OpenCode not deployed) | Deploy an agent with a session backend; see [orchestrator-fleet.md](orchestrator-fleet.md) |
| Prompt returns error "Backend requires authentication" | ACP backend (Claude) needs API key or token | Set up ACP credentials in the daemon config |
| Session stays in "paused" state | Backend is slow or unreachable | Check agent daemon logs; restart the agent |
| Attach view shows no events | No events recorded yet (new session) | Send a prompt first, then attach |
| Close Session button is disabled | Session is already closed | Refresh the page; the session may have been closed from another client |

## See also

- [CLI: quicbaas orchestrator session](../cli/orchestrator.md) — manage sessions via CLI
- [agents/inference.md](../agents/inference.md) — session vs. sessionless inference distinction
- [Orchestrator → Inference](orchestrator-inference.md) — sessionless prompts (no state)
- [Orchestrator → Live Events](orchestrator-events.md) — view session events in real-time
