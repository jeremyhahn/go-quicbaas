### Fixed

- **The root gRPC client reads `rate_limited` back (go-quicbaas#158).** The
  `pkg/api/grpc` client and `pkg/service.FromGRPCError` returned a throttled
  call as `resource_exhausted`. They now share one gRPC status map, which reads
  `rate_limited` from the `RESOURCE_EXHAUSTED` status message, as the Go SDK
  does.
- **The Go SDK's gRPC transport keeps `OUT_OF_RANGE` and `DATA_LOSS`
  (go-quicbaas#158).** They came back as `unknown`; they are now
  `invalid_input` and `internal`.
