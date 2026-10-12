### Added

- **`sdk/go/xkms/acme/http01` re-exports the go-xkms client-side ACME
  http-01 challenge responder (go-quicbaas#560).** An SDK-only consumer can
  now answer an http-01 challenge: `NewResponder`, `Responder`,
  `ResponderConfig`, and the typed errors `ResponderConfigError`,
  `ResponderBindError` and `ResponderShutdownError`.
- **`sdk/go/xkms/ssh/knownhosts` re-exports the OpenSSH fingerprint validator
  (go-quicbaas#382).** `ValidateFingerprint` checks an operator-supplied
  `SHA256:<base64>` host-key fingerprint and returns
  `*InvalidFingerprintError` when it is malformed.
