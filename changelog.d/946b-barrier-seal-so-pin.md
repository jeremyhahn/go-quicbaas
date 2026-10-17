### Security

- **Barrier seal verifies the Security Officer PIN when a PIN backend is
  configured.** `BarrierService.BarrierSeal` takes a `BarrierSealRequest` and
  refuses a missing or wrong SO PIN with a forbidden `QBError` (`403` on REST,
  `PermissionDenied` on gRPC); the barrier is left unchanged. With no PIN
  backend, seal behaves as before. REST (`so_pin` body), gRPC/Unix
  (`BarrierSealRequest.so_pin`) and MCP (`so_pin` argument) carry it; the
  Go SDK MCP transport sends a `WithSOPIN`-bound PIN over TLS only.
  `quicbaas barrier seal` reads it from `--so-pin-stdin` or
  `QUICBAAS_BARRIER_SO_PIN` (`--so-pin` stays accepted), and the TypeScript
  `barrier.seal({ so_pin })` sends it on REST and MCP. See
  `docs/barrier/api.md`.
- **The TypeScript SDK refuses to send an SO PIN over a non-`https://`
  endpoint.** `barrier.seal({ so_pin })` on REST and MCP throws
  `SOPINRequiresTLSError` before any request, loopback included, as the Go
  SDK does; a seal without a PIN still works over `http://`.
