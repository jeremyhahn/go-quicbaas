### Changed

- **`messaging.Client.GetDedupStats` takes its channel (go-quicbaas#185).**
  The server keys deduplication statistics by channel, but the SDK method took
  no argument, so it failed with `InvalidInput` on every transport. It is now
  `GetDedupStats(ctx, channel)` and is carried on REST, QUIC, gRPC, Unix, MCP
  and embedded. A missing channel is refused with `InvalidInput`, an unknown
  one with `NotFound`.

### Fixed

- **Dedup figures are reported correctly.** The server's `total_messages`
  counts unique messages, but every SDK transport treated it as the total
  number of checks. `DedupStats` now reports `UniqueMessages` as the server's
  unique count and `TotalChecks` as unique plus duplicates.
- **The MCP dedup tool refuses an unknown channel.** `quicbaas_msg_dedup_stats`
  now checks the channel exists, as the gRPC and REST handlers do, and refuses
  an unknown one with a NotFound code. The SDK MCP transport no longer answers
  `GetDedupStats` with Unimplemented.
