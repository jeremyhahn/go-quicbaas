### Fixed

- **gRPC vector handlers no longer panic on out-of-range values
  (go-quicbaas#559).** A request value outside its range returns
  `codes.InvalidArgument`. A service value that does not fit its wire field
  returns `codes.Internal` with the field name.
- **gRPC handlers no longer swallow conversion errors.** An unknown
  algorithm, a YAML encoding failure or a role lookup failure is now
  returned to the caller. No default algorithm is substituted.
- **gRPC embedding, backup, timeseries, entity-type, dynamic-secret and
  worker-image responses are range-checked.** A count, size or port that
  does not fit its wire field returns `codes.Internal` with the field
  name, instead of a wrapped or clamped number. A negative `ListBackups`
  limit is rejected as invalid input.
- **gRPC watch streams reject unknown event types.** An event type the
  wire format does not define now fails the stream with `codes.Internal`,
  instead of being sent as a different type.

### Changed

- **gRPC client limits are refused, not clamped.** `TSQuery`,
  `TSListMetrics` and `ListBackupsFromRequest` reject a limit beyond the
  int32 range as invalid input before the RPC is sent. A negative limit
  is passed to the server unchanged.
- **The `interfaces.SealManager` alias is removed.** Its users take
  `interfaces.BarrierManager`, and the server seams are named for the
  barrier.
