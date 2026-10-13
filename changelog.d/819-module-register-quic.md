### Added

- **quicbaas's API module is reachable over go-qrdb's QUIC transport
  (go-quicbaas#819).** `Module.RegisterQUIC` registers the `RegisterGRPC`
  services, so every unary method that declares a `(qrdb.v1.permission)`
  projects onto QUIC under the permission gRPC already enforces, with the same
  late-bound Unavailable answer before the facade is composed. Public login and
  health methods (QUIC has no unauthenticated path) and streaming methods stay
  off QUIC. The per-service table is in `docs/composition/architecture.md`.

### Fixed

- **gRPC and QUIC authorize `EmbeddingService` (go-quicbaas#819).** gRPC
  denied every embedding RPC because none was mapped to a permission. Each RPC
  now requires the verb REST and MCP already apply to the same operation:
  generation and cache clearing require `embedding:embed`; provider listing and
  inspection, default-provider selection and cache statistics require
  `embedding:list_models`.
