# Desktop & Web GUI

QuicBaaS ships a single Svelte 5 + Tailwind 4 + daisyUI bundle that runs as both a Wails desktop app (native binary, offline-capable) and an embedded web UI served by the quicbaas process at `/ui/`.

Same code, same UX, two shells:

| Shell | Transport | Use case |
|-------|-----------|----------|
| Wails desktop | Embedded Go bindings (no RPC) | Offline ops, single-tenant, laptop install |
| Embedded web (`/ui/`) | REST + SSE (optionally gRPC-Web) | Remote operators, shared deployments |

The frontend auto-detects its shell via `@quicbaas/sdk` and selects the matching transport at runtime. Application code never imports `fetch` or gRPC clients directly — it always calls the SDK.

## Building

### Desktop (Wails)

```bash
# From the project root.
make build-cli WITH_GUI=1
```

This installs `npm` dependencies under `cmd/quicbaas/frontend/`, builds the Svelte bundle, copies it to `pkg/api/rest/ui/dist`, and links Wails into the `quicbaas` binary. CGO is required.

### Web-only

```bash
# Build the frontend bundle and embed it in the daemon.
make _build-frontend
make build
```

The daemon exposes the UI at `GET /ui/` once REST is enabled.

## Transport Selection

`@quicbaas/sdk` exports `createClient({ transport: 'auto' | ... })`. On Wails, `'auto'` returns the embedded Wails transport. In a browser, it returns REST (+ SSE for live streams). Both return the same `Client` interface — screens never know which one they hold.

For gRPC-Web, QUIC, Unix-socket gRPC, and MCP flavors, pass the transport name explicitly. All six transports share the same request/response types.

## Complete GUI Walkthroughs

Comprehensive step-by-step walkthroughs for all 22+ views are available in [GUI Walkthroughs](README.md). Each walkthrough includes:

- **Purpose** — what the view is for and when to use it
- **Layout** — detailed description of the page structure
- **Lifecycle actions** — table of all row/card actions with backend endpoints
- **Walkthrough** — numbered end-to-end example flows
- **Troubleshooting** — common errors and fixes
- **See also** — links to CLI equivalents and related views

## Key Screens Overview

- **Dashboard** — system health, uptime, service status, and resource counts (sessions, agents, jobs, workflows)
- **Sessions list** — live view of running/paused/closed sessions, filterable by tenant, backend, and status
- **Session attach** — history replay + live SSE tail. Renders each event kind with its own component (prompts, tool calls, file diffs, etc.). Scrollback virtualizes with IntersectionObserver sentinels.
- **Agent Fleet** — enrolled agent daemons, their heartbeat status, and the jobs they own
- **Orchestration → Inference** — stateless prompt submission to a fleet-resident backend with optional streaming, host affinity, and a per-session history of the last 10 calls. See [agents/inference.md](../agents/inference.md#using-the-gui) and [Inference walkthrough](README.md#orchestration) for the full layout and error matrix.

## Loading the UI

Once `quicbaasd` is running with REST enabled, browse to:

```
https://<host>:18080/ui/
```

The browser must trust the daemon's CA. Either:

1. **Recommended** — install the daemon's CA into the system trust store via
   [`xkey`](https://github.com/jeremyhahn/go-xkms) +
   [`go-truststrap`](https://github.com/jeremyhahn/go-truststrap):
   ```bash
   xkey ca install --address https://<host>:18080
   ```
2. **Development only** — accept the warning ("Advanced → Proceed to <host>")
   in Chrome / Firefox. The UI works but sub-resource loads will be flagged as
   not secure.

The Wails desktop build skips this step entirely — it talks to the embedded
Go bindings, never over TLS.

## Authentication

By default the embedded UI uses the daemon's IAM service for login. Initial
operator credentials are printed once when the data dir is bootstrapped:

```
quicbaasd start --data-dir ./data ...
# →  initial admin: admin / <generated password>
```

Subsequent logins use the same credentials (or any user provisioned via the
IAM API / GUI). Sessions persist as JWTs in `localStorage`; logging out
clears the token and reloads the login view.

## Development

```bash
# Frontend dev server with hot reload, proxying to a running daemon.
cd cmd/quicbaas/frontend
npm run dev
```

Tests run under vitest:

```bash
cd cmd/quicbaas/frontend && npm test
```
