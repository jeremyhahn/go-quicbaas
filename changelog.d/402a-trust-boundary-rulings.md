### Documentation

- **Trust-boundary rulings recorded (go-quicbaas#402, #416, #422, #329).**
  The plaintext TOTP secret between the KV read and `MFASecret.Secret` is an
  accepted known limitation until go-qrdb offers a protected-bytes read. A
  proven-hardware credential satisfies `mfa_verified` for the release
  candidate; second-factor evidence (WebAuthn UV, PIV PIN policy) is the
  follow-up. PIV slot attestation is accepted from the TLS handshake only, so
  attested PIV login does not work from a browser; new tests pin that an
  attestation sent outside the handshake is refused. The embedded transport
  is not an authorization boundary: linking the SDK is the grant, and gRPC
  stays a boundary.
