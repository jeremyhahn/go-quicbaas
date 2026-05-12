# Orchestrator → Inference

## Purpose

The Inference view submits stateless prompts to fleet-routed backends (e.g., Ollama) without creating a persistent session. Use this view to test backends, ask quick questions, or run one-off inference tasks. Responses stream in real-time if enabled, and a session history tracks the last 10 calls for reference.

## Layout

The view is structured as four stacked cards:

**Card 1 — Target**

```
Backend:        [Ollama ▼]  (populated from GET /api/v1/orchestrator/backends)
Host Affinity:  [Any ▼]     (filtered from GET /api/v1/orchestrator/agents)
Model:          [qwen3:1.7b]  (free-text input; defaults to backend default)
```

- **Backend** — dropdown of available backends. Session-only backends (Claude, Codex, OpenCode) are visually disabled with a tooltip: "This backend requires a session. Use Sessions view instead."
- **Host Affinity** — dropdown to pin the prompt to a specific agent. "Any" lets the orchestrator pick the best available.
- **Model** — free-text input; defaults to the backend's default model. Must be installed on the agent.

**Card 2 — Prompt**

```
System Prompt (optional): [_________________________________]
                          [Show / Hide]

Prompt (required):        [_________________________________]
                          [Send] (or press Enter)

⚙️ Stream  [ON | OFF]   (toggle; default ON)
          [Cancel]       (visible when request in-flight)
```

- **System Prompt** — collapsible section for optional system message.
- **Prompt** — required field. Press Enter or click Send.
- **Stream toggle** — enabled by default. When on, response streams as NDJSON chunks. When off, waits for full aggregated response.
- **Cancel button** — visible only during active request.

**Card 3 — Response**

```
Response:

Your response appears here. Stream chunks are appended in real-time.

─────────────────────────────────────────

Model: qwen3:1.7b | Stop Reason: end_turn | Elapsed: 2.34s

[Copy] [Clear]
```

- Live output area with `aria-live="polite"` for screen reader support.
- Footer shows model name, stop reason, and elapsed time.
- **Copy** button — copies response text.
- **Clear** button — resets the response (does not clear the prompt field).

**Card 4 — Session History**

```
Recent Inference Calls (last 10):

14:35:20  [Ollama | qwen3:1.7b | gpu-host]
          Q: Explain QUIC
          ⏱️ 2.34s
          [Expand to show response]
          
14:32:10  [Ollama | qwen3:1.7b | Any]
          Q: Write a haiku
          ⏱️ 1.89s
          [Expand to show response]
```

Each row shows timestamp, backend, model, host, prompt summary, and elapsed time. Click to expand and see the full response. History is preserved during the session but cleared on page reload.

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Send Prompt | Send | Submits the prompt to the backend | `POST /api/v1/orchestrator/inference` |
| Cancel Request | X | Aborts the in-flight request; partial output is preserved | — (client-side HTTP abort) |
| Copy Response | Clipboard | Copies the full response text to clipboard | — (client-side) |
| Clear Response | Trash | Clears the response area for a fresh prompt | — (client-side) |
| Toggle Stream | Toggle | Switches between streaming and aggregated response modes | — (client-side; changes request parameter `stream=true\|false`) |
| Expand History | ChevronDown | Expands a history row to show the full prompt and response | — (client-side) |

## Walkthrough

### Submit your first stateless inference request

1. Open the Inference view (`#/orchestration/inference`).
2. Verify that at least one agent is registered in the fleet. If not, see [orchestrator-fleet.md](orchestrator-fleet.md).
3. **Backend** dropdown shows "Ollama". If it does not, select it.
4. **Host Affinity** defaults to "Any" (good for load balancing).
5. **Model** defaults to `qwen3:1.7b` (or the backend's default). You can change this if another model is installed.
6. In the **Prompt** field, enter a prompt: "Explain QUIC in three sentences."
7. Ensure **Stream** is toggled ON (default).
8. Click **Send** or press Enter.

**Expected outcome:**

```
✓ Send button becomes disabled
✓ A spinner or loading state appears
✓ Response area begins to fill with text chunks
✓ Text appears progressively as the backend streams chunks
✓ After ~2–10 seconds, the final chunk arrives with "final": true
✓ Send button re-enables
✓ Footer shows: "Model: qwen3:1.7b | Stop Reason: end_turn | Elapsed: 3.45s"
```

9. To clear the response and ask another question, click **Clear**.
10. Scroll down to **Session History** and you should see your inference call:

```
14:35:20  [Ollama | qwen3:1.7b | Any]
          Q: Explain QUIC in three sentences
          ⏱️ 3.45s
```

11. Click the history row to expand and review your prompt and response.

### Try a session-only backend

12. Click the **Backend** dropdown.
13. Observe that "Claude", "Codex", and "OpenCode" are grayed out with tooltips:

```
This backend requires a session. Use Sessions view instead.
```

14. Attempting to send a prompt with these backends shows an error:

```
Error: 422 Unimplemented

This backend requires a persistent session. Use the Sessions view
to spawn a Claude session, then prompt it there.
```

15. To use a session-based backend, go to the [Sessions view](orchestrator-sessions.md) instead.

### Handle errors

16. Leave the **Model** field as is and click **Send**.
17. If the model is not installed on the agent, you see an error after a few seconds:

```
Error: Backend returned an error

stderr: error: model 'qwen3:1.7b' not found
```

18. Fix by selecting a model that is installed, or install it on the agent.
19. If you cancel a request mid-stream by clicking **Cancel**, the partial output is preserved:

```
Response:

QUIC is a modern transport protocol that...

─────────────────────────────────────────
(request cancelled; response incomplete)

[Copy] [Clear]
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Backend dropdown is empty | No backends registered in the fleet | Register an agent via Agent Fleet; see [orchestrator-fleet.md](orchestrator-fleet.md) |
| Host Affinity dropdown shows "No agents" | No agents registered for the chosen backend | Deploy an agent with the Ollama (or other) backend |
| Prompt sends but response is blank | Backend or agent timed out (>30s) | Check agent logs; consider a simpler prompt; restart the agent |
| Model field shows "not found" error | Model is not installed on the agent | Install the model: `ollama pull qwen3:1.7b` on the agent host |
| Stream toggle does not work | Browser does not support SSE or fetch streaming | Use Chrome/Firefox/Edge (Safari may have limited support) |
| Response text is gibberish or truncated | Encoding issue or network corruption | Refresh the page; try a shorter prompt; check network logs |
| Error: "Backend requires a session" | You selected Claude, Codex, or OpenCode | Use the Sessions view instead for these backends |
| Request hangs for >30 seconds | Agent is unreachable or overloaded | Check network; verify agent is running; try a different agent via Host Affinity |

## See also

- [agents/inference.md](../agents/inference.md) — complete API and architecture reference
- [CLI: quicbaas orchestrator inference](../cli/orchestrator.md#quicbaas-orchestrator-inference) — stateless inference via CLI
- [Orchestrator → Sessions](orchestrator-sessions.md) — persistent session-based prompts (for Claude, Codex, etc.)
- [Orchestrator → Agent Fleet](orchestrator-fleet.md) — register and manage agents
