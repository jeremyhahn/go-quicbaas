### Removed

- The duplicate seal surface (#1559). The barrier is go-qrdb's, reached
  only through `Security().Barrier()`, the re-export of go-qrdb's
  `BarrierClient`, on all six transports. Removed:
  - the Go SDK seal client and its `sdk/go/service` seal type aliases
  - the `/api/v1/seal/*` REST routes
  - the `quicbaas_seal*` MCP tools
  - the go-quicbaas seal service
  - the CLI seal client plumbing (`SealClient` factory, SDK client, mock)
  - the REST and gRPC client seal methods
  - the JavaScript SDK seal client
