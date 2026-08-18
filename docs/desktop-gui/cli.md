# Desktop GUI — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Feature folder: `docs/desktop-gui/`.

**There is no GUI subcommand.** The desktop application is what `quicbaas`
launches when it is invoked with **no subcommand at all** and the binary was
built with the `desktop` build tag.

```bash
# Launch the GUI (requires a desktop-tagged binary).
quicbaas

# Same binary, CLI mode: any subcommand.
quicbaas iam user list
```

## Flags that reach GUI mode

Only one:

| Flag | Effect |
|---|---|
| `--log-file <path>` | GUI logs are appended to this file |

It is read by scanning `os.Args` directly (`cmd/quicbaas/main.go:482`), because
cobra has not parsed flags at the point the GUI is launched. Every other
persistent flag — `--log-level`, `--address`, `--transport`, `--tls*` — is
**not** honoured in GUI mode: the log level is fixed to `info`/`json`, and
connection settings are chosen in the application's own connection pane.

```bash
quicbaas --log-file ~/.local/state/quicbaas/gui.log
tail -f ~/.local/state/quicbaas/gui.log
```

## Exit codes

| Code | Meaning |
|---|---|
| `0` | The GUI ran and closed normally. |
| `1` | The GUI started and failed. |
| `2` | **This binary has no GUI.** It was built without the `desktop` tag; `gui_stub.go`'s `launchGUI` returned `GUIUnavailableError` and the message on stderr says so. |

Exit 2 is worth handling explicitly in any wrapper: it means "rebuild", not
"retry".

## Building a GUI-capable binary

```bash
make build
```

`engineering.md` covers the build-tag mechanics and the embedded frontend
assets — in particular that a stale `frontend/dist` is embedded silently, so the
frontend must be built before the Go build.
