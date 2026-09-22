### Fixed — BREAKING: `GET /api/v1/barrier/recovery/status` renames its response field to `has_keys`

- **A barrier that HAS recovery keys no longer reports that it has none.**
  The route answered `{"has_recovery_keys": true}`, but the canonical client
  of that route — go-qrdb's REST transport — decodes the body into
  `transport.BarrierHasRecoveryKeysResponse`, whose field is tagged
  `has_keys`. The key never matched, so
  `Security().Barrier().BarrierHasRecoveryKeys(...)` returned `false` for a
  barrier holding recovery keys: a wrong answer on a disaster-recovery
  precondition, not an error, which is the failure mode that gets acted on.
  The handler now emits `has_keys`.
- The gRPC and MCP surfaces keep `has_recovery_keys`. The barrier is
  go-qrdb's, and go-qrdb spells this bit per transport — `has_keys` in its
  REST response type, `has_recovery_keys` in its MCP tool output and in the
  protobuf field — so each quicbaas transport serves the format its own
  upstream client reads, rather than one route emitting both keys.
- The TypeScript SDK's `BarrierHasRecoveryKeysResponse` field is renamed to
  `has_keys` to match the REST body it mirrors. Callers reading
  `status.has_recovery_keys` from `barrier.recoveryStatus()` read
  `status.has_keys` instead.
