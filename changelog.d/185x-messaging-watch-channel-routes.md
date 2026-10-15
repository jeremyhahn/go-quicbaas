### Added

- **`messaging.Client.WatchChannel` is served on every transport
  (go-quicbaas#185).** REST serves it at `POST /api/v1/messaging/watch` and MCP
  as `quicbaas_msg_watch_channel`, both with permission `messaging:watch`. The
  SDK REST, QUIC and MCP transports map `WatchChannel` onto them, so it no
  longer fails outside gRPC, Unix and embedded. gRPC, REST and MCP share one
  request mapping, so they validate and build subscribe options the same way. An
  empty key prefix is refused with `InvalidInput`.
