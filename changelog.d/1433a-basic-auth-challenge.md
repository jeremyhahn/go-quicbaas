### Fixed

- **`WWW-Authenticate: Basic` on 401 `invalid_client` (go-quicbaas#1433).** Per draft-ietf-oauth-v2-1-16 §3.2.4, the token and introspection endpoints add a Basic challenge to their `401 invalid_client` when the client authenticated with HTTP Basic.
