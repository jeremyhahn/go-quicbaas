### Added

- **`iam.NewSigningKeyManagerFromSigner(signer, alg)`** wraps an in-process
  `crypto.Signer` in a `SigningKeyManager`, deriving the `kid` from the public
  key as for xkms-backed keys. It cannot rotate: `Rotate` returns
  `SigningKeyRotationUnsupportedError`.

### Removed

- **`OIDCConfig.SigningKey` and `OIDCConfig.SigningKeyFile`.**
  `SigningKeyManager` is the only OIDC signing source, and `NewOIDCProvider`
  returns `OIDCConfigRequiredError` without one. `JWKSGenerationError` went
  with the single-key JWKS path.

### Fixed

- **Every OIDC token carries a `kid`.** Tokens signed through the removed
  `SigningKey` path had no `kid` header, so a verifier that selects keys from
  the JWKS by `kid` rejected them. See `docs/iam/engineering.md#jwt-signing`.
