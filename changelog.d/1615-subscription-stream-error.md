### Fixed

- sdk/go orchestrator: a session or job event subscription that fails mid-stream now delivers the error as the channel's last value (`Err`), instead of closing the channel as if the stream had finished. `quicbaas orchestrator attach` and `prompt --follow` exit non-zero with the error, `orchestrator run` falls back to polling and says why, and the signal bridge stops its forwarder (#1615).
