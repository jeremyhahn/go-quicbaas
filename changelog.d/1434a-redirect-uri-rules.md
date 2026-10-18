### Fixed

- **Omitted `redirect_uri` resolves to the client's only one (go-quicbaas#1439).**
  Per draft-ietf-oauth-v2-1-16 §4.1.1, a missing `redirect_uri` is now
  resolved by the OIDC provider, not refused by the REST handler: when the
  client registered exactly one redirect URI, `GET /oauth2/authorize` uses it
  and stores it with the code. A client with several (or none) still gets a
  direct `400 invalid_request` that is never redirected.
- **Loopback redirect URIs match any port (go-quicbaas#1438).** Per
  draft-ietf-oauth-v2-1-16 §2.3.1 and §8.4.2, a registered
  `http://127.0.0.1` or `http://[::1]` redirect URI matches a request that
  differs only in the port. `localhost` and every other host still match
  exactly.
- **Token endpoint compares `redirect_uri` only when sent (go-quicbaas#1434).**
  Per draft-ietf-oauth-v2-1-16 §4.1.3, an authorization code exchange without
  `redirect_uri` succeeds; one that sends it must equal the value the
  authorize step resolved, or it gets `invalid_grant`.
