### Fixed

- **gRPC integer fields no longer wrap silently (go-quicbaas#559).** Every
  `int` to `int32` narrowing in `pkg/api/grpc` is now checked. Some
  examples: list limits, counts and totals, secret retention, channel
  sizes, lease renewals, backup code counts, DEK warning thresholds,
  rebalance shard counts and ceremony share indexes. A client request value
  outside the int32 range is rejected as invalid input before the RPC is
  sent. A server value that does not fit its wire field now returns
  `codes.Internal` with the field name, instead of a wrapped number. DEK
  re-encryption progress counts and the time estimate still saturate at
  the int32 maximum, and this is now documented.
- **gRPC trace spans record entity IDs correctly.** An entity ID is a
  uint64 and is now recorded as its decimal string. Before, IDs above
  MaxInt64 were recorded as negative numbers, and some spans recorded an
  empty string.

### Changed

- **The gRPC package has no linter suppression directives.** The dead legacy seal
  surface on the gRPC `Server` (`SetSealManager` and its unread field) is
  removed. The lifecycle seal shims are typed against
  `interfaces.BarrierManager` instead of the deprecated `SealManager`
  alias. The unused binary FastStateMachine encoders are removed; they
  wrote a truncated length prefix for oversized keys. Tests dial with
  `grpc.NewClient`.
