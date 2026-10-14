### Changed

- **The CLI's server address is settable by config file and environment, not
  only by flag (go-quicbaas#185).** `--address` resolves from the
  `client.address` key and `QUICBAAS_CLIENT_ADDRESS`, and `txn --rest` from
  `client.transaction_rest` and `QUICBAAS_CLIENT_TRANSACTION_REST`, with the
  precedence flag > env > config file > default. Flag names and defaults are
  unchanged.
- A malformed address fails the command with `ErrMalformedServerAddress`
  instead of being passed on to the transport.
- `quicbaas txn` subcommands now load the CLI configuration, logger and
  tracing like every other command. Before, the txn pre-run hook replaced the
  root's and skipped them.
