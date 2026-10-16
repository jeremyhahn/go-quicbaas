### Removed

- **`iam.NewTokenService`.** It built a token service with no signing key
  manager, so its tokens carried no `kid` and failed an external relying
  party's JWKS lookup. `NewOIDCProvider` is now the only constructor: every
  token it issues carries a `kid` that `GetJWKS` publishes. See
  `docs/iam/engineering.md#jwt-signing`.
