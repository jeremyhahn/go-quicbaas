### Changed

- **An empty CLI server address is refused before any command runs
  (go-quicbaas#185).** An empty `client.address` from `--address`, a set but
  empty `QUICBAAS_CLIENT_ADDRESS`, or the config file now fails with
  `ErrMalformedServerAddress`, as an empty `txn --rest` already did, instead
  of reaching the client as `ErrEmptyAddress`. The same holds for an empty
  `QUICBAAS_CLIENT_TRANSACTION_REST`. The error names the key and where the
  value came from (flag, environment variable, config file or default).

### Fixed

- The CLI docs' Global Flags table lists the root command's real persistent
  flags. It no longer lists `--output`, `--consistency`, `--max-retries` and
  `--retry-backoff`, which only some command groups have, or
  `--tls-skip-verify`, which does not exist; `--tenant` defaults to `system`,
  not the retired `default`, and `--transport` to `unix`.
