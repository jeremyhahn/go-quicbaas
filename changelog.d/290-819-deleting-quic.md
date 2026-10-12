### Fixed

- **WebAuthn registration no longer leaves a credential behind for an account
  being deleted (go-quicbaas#290).** The credential write-through refuses an
  account in the `deleting` lifecycle before writing, and re-checks after the
  write so a deletion that starts mid-ceremony rolls the credential and its
  login factor back. The ceremony fails with `WebAuthnRegistrantDeletingError`,
  or `WebAuthnRegistrantLookupError` when the account cannot be read.

### Changed

- **Module transport docs describe go-qrdb's `RegisterQUIC` hook
  (go-quicbaas#819).** They no longer say the hook does not exist. quicbaas's
  own API module still takes the no-op default: its `EmbeddingService`
  declares no `(qrdb.v1.permission)` options, and delegating to `RegisterGRPC`
  would make QUIC server construction panic.
