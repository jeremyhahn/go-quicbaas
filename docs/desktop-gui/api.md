# Desktop GUI — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Feature folder: `docs/desktop-gui/`.

**No network API of its own.** The GUI is a client. It does not listen, it does
not serve, and there is no `quicbaas.GUI` service in `sdk/go/client/`.

The six-transport matrix used by other features is therefore inapplicable rather
than incomplete: this feature has no capability of its own to carry over a
transport. Everything it displays it fetched from a feature that does.

## The internal binding surface

What the GUI *does* have is a Wails binding layer — Go methods exposed to the
Svelte frontend through the Wails runtime, called in-process, never over a
socket. Seven binding objects are constructed at
`cmd/quicbaas/gui_desktop.go:36-42`:

| Binding | Fronts | Documented in |
|---|---|---|
| `ConnectionBinding` | server address, transport, TLS material | [`../multiprotocol-api/`](../multiprotocol-api/api.md) |
| `OrchestratorBinding` | orchestration operations | [`../software-factory/`](../software-factory/api.md) |
| `SessionBinding` | session event history and subscriptions | [`../software-factory/`](../software-factory/api.md) |
| `FleetBinding` | fleet registry queries | [`../agent-fleet/`](../agent-fleet/api.md) |
| `EmbeddedServerBinding` | in-process server control | [`../composition/`](../composition/api.md) |
| `FleetDeployBinding` | remote fleet node deployment | [`../agent-fleet/`](../agent-fleet/cli.md#fleet-deploy) |
| `SSHKeysBinding` | server-held SSH keys | [`../ssh/`](../ssh/api.md) |

These are **not a public API**. They are not versioned, not stable, and not
reachable from outside the process. A consumer wanting programmatic access
should use the Go SDK ([`../go-sdk/`](../go-sdk/api.md)) or one of the six
transports.

## Where the GUI's data actually comes from

Six of the seven bindings share one `ConnectionBinding`
(`cmd/quicbaas/gui_desktop.go:37-42`), so every pane in the window is talking to
the same server over the same transport with the same credentials. Requests
leave the process through the ordinary SDK client, meaning:

- the permissions that apply are the connected principal's, exactly as at the
  CLI;
- an operation the GUI cannot perform is an authorization result, not a GUI
  limitation;
- the audit trail records GUI-originated actions like any other.

`EmbeddedServerBinding` and `SSHKeysBinding` are the exceptions and do not take
the shared connection — see `engineering.md`.

## Web UI

The same Svelte frontend is served as an embedded web UI. It reaches the server
over REST rather than through Wails bindings, so in that mode the applicable
surface is [`../multiprotocol-api/api.md`](../multiprotocol-api/api.md).
