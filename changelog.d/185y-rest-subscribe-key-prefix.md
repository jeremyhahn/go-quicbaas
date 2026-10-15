### Fixed

- **SDK REST and QUIC `messaging.Client.Subscribe` keep the filter's key prefix
  (go-quicbaas#185).** The REST transport, which QUIC also uses, sent the
  filter's event types but dropped `Filter.KeyPrefix`. A REST or QUIC
  subscriber therefore received every key, while a gRPC or MCP subscriber
  received only the prefix it asked for. The prefix is now sent as the subscribe
  route's existing `key_prefix` field. The embedded transport now builds
  `WatchChannel` and `Subscribe` options through the same request mapping as
  gRPC, REST and MCP. An embedded `WatchChannel` with an empty key prefix is
  refused with `InvalidInput`.
