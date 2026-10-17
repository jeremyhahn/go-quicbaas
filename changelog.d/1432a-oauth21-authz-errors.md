### Fixed

- **Authorization endpoint redirects errors to the client (go-quicbaas#1432).**
  Per draft-ietf-oauth-v2-1-16 §4.1.2.1, once `client_id` and `redirect_uri`
  are validated, `GET /oauth2/authorize` now reports errors as a `302` to the
  redirect URI with `error`, `error_description`, `state` and `iss`, keeping
  the registered URI's query, instead of a JSON `400`. An unknown client or a
  missing or unregistered redirect URI still gets a direct `400` and is never
  redirected; `pkg/iam` marks these with `*iam.AuthorizeDirectError`.
  Successful authorization responses now carry `iss` as well.
- **`WWW-Authenticate: Basic` on 401 `invalid_client` (go-quicbaas#1433).**
  Per draft-ietf-oauth-v2-1-16 §3.2.4, the token and introspection endpoints
  add a Basic challenge to their `401 invalid_client` when the client
  authenticated with HTTP Basic.
