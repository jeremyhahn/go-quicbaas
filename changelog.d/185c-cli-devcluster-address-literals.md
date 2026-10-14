### Changed

- **No hardcoded addresses in the CLI, devcluster, WebAuthn origin or
  tracing (go-quicbaas#185).** A literal address now appears only as a
  defaults-layer value.
- devcluster has a new `devcluster.host` setting (env
  `QUICBAAS_DEVCLUSTER_HOST`, default `localhost`). Every node's Raft
  address, listen address and join roster is built from it, and IPv6 hosts
  are bracketed.
- The CLI tracing settings are now flags as well as environment variables and
  config keys, with the precedence flag > env > config file > default. The
  flags are `--tracing-enabled`, `--tracing-exporter`, `--otlp-endpoint` and
  `--tracing-service-name`; the keys sit under `tracing.*`.
  `localhost:4317` is the endpoint default only.
- `quicbaas upgrade start` returns `NotConfiguredError` when both `--server`
  and `--address` are empty. It no longer substitutes `localhost:50051`.
- The `quicbaas transaction` `--rest` default is now
  `https://localhost:<server.rest.port default>`, the same port the daemon
  uses (8443, over TLS). The WebAuthn issuer falls back to that port as well.
