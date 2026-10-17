### Security

- **Token revocation acts only on a verified JWT (#1448).**
  `TokenService.RevokeToken` used to read a JWT's `jti` and `tenant_id`
  without checking its signature, so an unsigned or foreign-signed token could
  revoke any token by `jti` in any tenant. It now revokes by `jti`, and takes
  the tenant from `tenant_id`, only after the provider's verified parse
  (`parseVerifiedAccessTokenClaims`, the one `Introspect` uses) accepts the
  token. A token that fails verification, including an expired one, is not
  revoked by `jti`; a stored refresh token is still revoked by its value.
- **Logout with an expired or forged bearer now fails.**
  `RevokeSessionToken` returns an error for a token that does not verify, so
  `Logout` reports it as unauthorized instead of succeeding.
