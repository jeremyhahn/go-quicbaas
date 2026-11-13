### Fixed

- `SDK().Batch().Execute` is served on every transport, not only the embedded one. The daemon serves `POST /api/v1/batch/execute` (REST and QUIC), the `BatchExecute` gRPC RPC (gRPC and Unix) and the `quicbaas_batch_execute` MCP tool, each under `batch:execute`. Each request applies its puts and deletes as one atomic batch, and a bad request is refused with InvalidInput (#1925).
