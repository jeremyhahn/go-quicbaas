# Desktop GUI — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract for the Wails desktop application and its bindings.
> Feature folder: `docs/desktop-gui/`.

## Build-tag gating

The GUI is compiled only under the `desktop` build tag.

- `cmd/quicbaas/gui_desktop.go` (`//go:build desktop`) holds `launchGUI`, which
  calls `wails.Run`.
- `cmd/quicbaas/gui_stub.go` (`//go:build !desktop`) holds a `launchGUI` that
  returns `&GUIUnavailableError{}`.

`main` calls `launchGUI` at `cmd/quicbaas/main.go:492` and, on
`GUIUnavailableError`, prints:

```
quicbaas: this build was compiled without the desktop GUI.
Rebuild with `make build` to get the GUI-capable binary, or run a CLI subcommand (see `quicbaas --help`).
```

and exits **2**. That exit code is part of the contract: a wrapper script can
distinguish "no GUI in this build" from an ordinary failure.

## Assets are embedded

```go
//go:embed all:frontend/dist
var assets embed.FS
```

The Svelte frontend is built into `cmd/quicbaas/frontend/dist` and embedded in
the binary. **A stale `dist` is silently shipped**: `go build` will not tell you
the frontend was not rebuilt, so the frontend build must run before the Go
build, not alongside it.

## The seven bindings

`launchGUI` constructs seven binding objects and starts each in `OnStartup`
(`cmd/quicbaas/gui_desktop.go:44-60`):

| Binding | Constructor | Surface it fronts |
|---|---|---|
| Connection | `NewConnectionBinding(log)` | server address, transport, TLS material |
| Orchestrator | `NewOrchestratorBinding(log, conn)` | orchestration operations |
| Sessions | `NewSessionBinding(log, conn)` | session event history and subscriptions |
| Agents | `NewAgentBinding(log, conn)` | agent registry queries |
| Embedded server | `NewEmbeddedServerBinding(log)` | standalone-mode server control |
| Agent deploy | `NewAgentDeployBinding(log, conn)` | remote deployment |
| SSH keys | `NewSSHKeysBinding(log)` | server-held SSH keys |

Six of the seven take `conn`, which is the invariant worth stating: **the GUI
holds one connection and every binding shares it.** A binding that constructs
its own client would produce a window where half the panes talk to one server
and half to another.

`EmbeddedServerBinding` and `SSHKeysBinding` do not take `conn` because they are
local: one controls an in-process server, the other reaches the key store
through its own path.

## Window

`Title: "QuicBaaS"`, `Width: 1600`, `Height: 1000`. Those are the shipped
defaults; the layout in `frontend/src/views/` assumes roughly that aspect.

## Logging

GUI mode honours `--log-file`. Because cobra has not parsed flags at the point
`launchGUI` is reached, `extractLogFileFlag(os.Args[1:])` scans `os.Args`
directly (`cmd/quicbaas/main.go:482`). A change to how that flag is spelled must
be made in both places — this is the one flag parsed twice in the codebase.

Log level is fixed to `info` with `json` format in GUI mode
(`buildLoggerConfig("info", "json", guiLogFile)`); `--log-level` does not reach
it.

## Frontend layout

```
cmd/quicbaas/frontend/
  src/
    views/           dashboard, cluster/, data/, ai/, async/, security/, orchestrator/
    components/      core/, orchestrator/
    stores/          Svelte stores
    lib/
  test/e2e/          Playwright
  dist/              build output, embedded by go:embed
```

`user.md` walks the views; `wails.json` carries the Wails build configuration.

## Counters say what they count

A `StatCard`'s label and its value have to agree. A tile labelled "Active
Sessions" over `sessions.length` told an operator 54 when one session was live
and 54 were closed (go-quicbaas#850), and they read system state off it for days.

- A card claiming live state — active, open, pending, running — counts live
  records. `lib/sessions.ts` holds the one definition of a live session
  (terminal = `closed`, `error`).
- **The lifecycle field's name depends on the transport, not the store.** REST
  sends `state` (`orchestration.SessionInfo`, written out verbatim); the Wails
  binding renames it to `status`. Both stores fill from the same call, so a
  counter naming one field is dead on the other platform — two of them were, each
  reading 0 while sessions ran (go-quicbaas#864). `countLiveSessions` reads the
  value; do not reintroduce a way to name the field. go-quicbaas#856 normalises
  this at the SDK seam, and `sdk/typescript/test/listSessions.test.ts` pins what
  the transport returns today so that change announces itself.
- **A store double is not evidence about the wire.** Both counter defects shipped
  green because the tests handed the view a record shaped the way the code
  expected. Fixtures carry the shape the TRANSPORT produces, and the shape itself
  is asserted in the SDK's own suite.
- A card showing a total says total, and the other number goes in the
  qualifying `description` line: `value={live}` with
  `description="{all.length} total"`. That pattern is already on Active Jobs,
  Pending Tasks and Total Jobs; use it rather than a new one.
- The fetches stay unfiltered. The list views legitimately show closed
  sessions and finished jobs, so the filtering belongs in the count, not in the
  request — narrowing a shared store's fetch to fix a counter breaks its list.

## Test strategy

- **Playwright end-to-end tests** live in `frontend/test/e2e/`. They are the
  only tests that exercise a binding against a real server.
- Store tests in `frontend/test/stores/`.
- A GUI change that alters a binding signature breaks the Svelte side silently
  at runtime rather than at compile time. Regenerating the Wails bindings is
  part of the change, not a follow-up.

## Known unimplemented views

Four surfaces are present in the navigation and do nothing: Async Scheduler
"View History", Orchestrator Events click-through, Orchestrator Fleet "View
Details", and the Orchestrator Jobs detail view. They are recorded here because
a reader finding a dead control needs to know it is known, not undiscovered.
