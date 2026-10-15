### Fixed

- **`messaging.Client.GetChannelStats` works over MCP (go-quicbaas#1352).**
  The SDK MCP transport refused it, so `quicbaas msg channel-info` and
  `quicbaas msg list-subscriptions --channel` always failed with
  `--transport mcp`. The server now registers `quicbaas_msg_channel_stats`,
  which requires `messaging:stats` as the REST route and the gRPC method do,
  and refuses an unknown channel with a NotFound code. The SDK request type is
  exported as `messaging.GetChannelStatsRequest`.
