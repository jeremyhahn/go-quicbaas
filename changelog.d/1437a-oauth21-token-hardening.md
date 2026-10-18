### Security

- **Token-endpoint parameters come only from the form body (#1437).** The
  token, revocation, introspection and mTLS token endpoints read every OAuth
  parameter from the POST body; a `client_secret`, `grant_type` or any other
  parameter in the URL query is ignored (draft-ietf-oauth-v2-1-16 §3.2).
- **A narrowed refresh keeps the refresh token's full scope (#1436).** A
  `refresh_token` grant that asks for a subset of the original grant issues
  that subset as the access token's scope, while the rotated refresh token
  keeps the presented token's full scope, so a later refresh can ask for the
  dropped scopes again (-16 §4.3.3).
