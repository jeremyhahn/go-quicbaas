### Changed

- **`quicbaas msg` runs on the SDK messaging client (go-quicbaas#185).** Every
  `msg` command now goes through `Async().Messaging()`, so it works on every
  transport the SDK offers instead of only gRPC. The CLI's own messaging
  clients are gone.
- **`msg` flags and output follow the SDK.** Flags the SDK cannot carry are
  removed: `create-channel --description`, `list-channels --limit/--offset/
  --filter`, `subscribe --replay`, `list-subscriptions --limit/--offset`,
  `publish --ack`, `watch --filter/--output/--timestamps/--raw` and
  `dedup-stats --reset/--window`. JSON and YAML output print the SDK types.
- **`msg subscribe` and `msg watch` register and exit.** The SDK has no
  notification stream, so neither command streams messages. `watch` now takes
  a KV key prefix; the REST and QUIC servers do not serve it and the command
  returns the SDK's error there.
- **`msg list-subscriptions` lists subscriber counts per channel.**

### Fixed

- **An unknown `--output` no longer half-runs a `msg` command.** The format is
  checked before the command contacts the server.
