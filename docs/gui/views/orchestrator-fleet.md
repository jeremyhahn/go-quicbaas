# Orchestrator → Agent Fleet

## Purpose

The Agent Fleet view displays registered agent daemons and their status. Use this view to enroll new agents, monitor their heartbeat, and decommission offline agents. Agents appear in this registry after either a manual registration (via REST POST) or automatic enrollment (via `quicbaas agent deploy`).

## Layout

The page displays a searchable, filterable table of agents:

**Header** — "Agent Fleet" title with "Registered agents and their status" subtitle. Primary CTA is **+ Register Agent** button.

**Filters and Search** — Above the table:

- **Status filter dropdown** — "All", "Online", "Offline", "Unknown". Defaults to "All".
- **Search input** — client-side filter on agent name, ID, and hostname.

**Table** — Columns:

- **ID** — unique agent identifier (e.g., `agt_abc123def456`), truncated with tooltip
- **Name** — human-readable agent name (e.g., `gpu-host`, `agent-01`)
- **Host** — IP address or hostname where the agent runs
- **Type** — backend type (e.g., `acp`, `ollama`, `opencode`)
- **Status** — badge: "online" (green), "offline" (red), "unknown" (gray)
- **Last Seen** — ISO 8601 timestamp of last heartbeat
- **Actions** — three-dot menu with "Remove" (delete agent from registry)

**Empty State**:

```
No agents registered.
Deploy your first agent using the CLI or API.

[View Deployment Guide button]
```

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Register Agent | Plus | Opens a form to manually register an agent via POST | `POST /api/v1/orchestrator/agents` |
| Remove Agent | Trash | Unregisters an agent from the fleet; shows a confirm modal | `DELETE /api/v1/orchestrator/agents/{agentID}` |
| View Details | ChevronRight | (Not yet implemented; greyed out) Shows agent capabilities and resource usage | `GET /api/v1/orchestrator/agents/{agentID}` |

## Walkthrough

### Deploy an agent using the CLI and verify it appears in the fleet

1. Open the Agent Fleet view (`#/orchestration/agents`).
2. Note the current agent count (likely 0 initially).
3. Open a terminal on a remote machine or use localhost:9999 (QuicBaaS development mode).
4. Deploy an agent using the CLI:

```bash
quicbaas agent deploy \
  --name my-agent-01 \
  --backend ollama \
  --host localhost \
  --join https://quicbaas.example.com:18080
```

**Expected output:**

```
Agent ID: agt_abc123def456
Status: registered
Name: my-agent-01
Host: localhost
Type: ollama
Last seen: 2026-04-25T17:12:59Z
```

5. Return to the Agent Fleet view in the GUI.
6. Refresh the page (Ctrl+R).
7. Your new agent should appear in the table with status "online" (green).

**Expected outcome:**

```
✓ Agent ID matches the CLI output
✓ Status is "online" (green) if heartbeat is recent
✓ "Last Seen" timestamp is within the last 60 seconds
```

8. To manually remove the agent, click its row's three-dot menu and select **Remove**.
9. A confirm modal appears:

```
Remove agent my-agent-01?

This agent will be unregistered from the fleet. Running jobs will be affected.

[Keep Agent] [Remove Agent]
```

10. Click **Remove Agent**.

**Expected outcome:**

```
✓ Confirm modal closes
✓ Success toast: "Agent my-agent-01 removed"
✓ Agent disappears from the table
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Agent shows "offline" (red) immediately after deployment | Agent is not reachable or mTLS cert verification failed | Check agent daemon logs: `tail -f agent-logs.txt`; verify CA cert is trusted |
| Agent does not appear in the fleet after 30 seconds | Heartbeat not sent or registration API call failed | Manually register via REST: `curl -X POST https://localhost:18080/api/v1/orchestrator/agents -d '{"name":"..."}' --cacert ca.crt` |
| Remove button is greyed out | Agent is currently running a job | Wait for the job to complete or cancel it from the Jobs view |
| Register Agent form shows validation error | Required fields are missing (name, backend, host) | Ensure all fields are filled before submitting |

## See also

- [CLI: quicbaas agent deploy](../agents/deploy.md) — full agent deployment walkthrough
- [CLI: quicbaas orchestrator agents](../cli/orchestrator.md#quicbaas-orchestrator-agents-list) — list agents via CLI
- [Orchestrator → Inference](orchestrator-inference.md) — submit prompts to registered agents
- [agents/deploy.md](../agents/deploy.md) — comprehensive agent bootstrapping guide
