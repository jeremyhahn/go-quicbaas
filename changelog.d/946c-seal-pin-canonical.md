### Security

- **The Go SDK's gRPC and Unix seal route verifies the SO PIN too.**
  `/qrdb.v1.QRDBService/BarrierSeal` is now guarded by
  `UnaryBarrierSealSOPINInterceptor`, which reads `x-qrdb-so-pin` (else
  `so_pin`) and refuses a missing or wrong PIN with `PermissionDenied` through
  the same verifier REST and MCP use. See `docs/barrier/api.md`.
- **No QUIC route seals the barrier without the SO PIN.** quicbaasd serves
  QUIC as REST over HTTP/3 only (the facade gates seal); the embedded node
  never starts go-qrdb's native QUIC server, and a test now pins that.
