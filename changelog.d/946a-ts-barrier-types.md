### Added

- **TypeScript SDK `barrier.enableEncryption()` (go-quicbaas#946).** The REST
  client posts go-qrdb's `BarrierEnableEncryptionRequest` (`strategy`,
  `mechanism`, `availability`, base64 `passphrase`, `shamir_threshold`,
  `shamir_total`, and `tenant_id` for a tenant barrier) to
  `POST /v1/barrier/enable-encryption` and resolves to
  `BarrierEnableEncryptionResponse` (`status`, `scope`). A refusal rejects with
  `APIError`. `make verify-api-shapes` no longer exempts the two types.
