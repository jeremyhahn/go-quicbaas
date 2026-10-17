# IAM — OAuth 2.1 Conformance

> **Status: INFORMATIVE** · Last verified: 2026-10-16
> The authorization server in `pkg/iam` and `pkg/api/rest`, compared with the pinned OAuth 2.1 draft.

This page records how the authorization server compares with
[draft-ietf-oauth-v2-1-16](../reference/draft-ietf-oauth-v2-1-16.txt) (2026-09-03), the revision
pinned in [docs/reference](../reference/README.md). It was evaluated on commit `0b79c00a`.
Line numbers refer to that commit. Follow-ups for the **departs** rows are tracked from #1428.

| Requirement | -16 section | Verdict | Code | Evidence |
|---|---|---|---|---|
| Implicit grant removed | §10, §10.1 | conforms | `pkg/iam/oidc.go:986`, `pkg/iam/oidc.go:144` | Any `response_type` other than `code` returns `unsupported_response_type`, and discovery advertises only `code`. |
| Password grant removed | §10 | conforms | `pkg/iam/oidc.go:330`, `pkg/iam/oidc_app.go:536` | The token endpoint serves only `authorization_code`, `client_credentials` and `refresh_token`, and app registration does not accept `password`. |
| PKCE required on the authorization code grant | §4.1.2.1, §4.1.3 | conforms | `pkg/iam/oidc.go:295`, `pkg/iam/oidc_types.go:603` | `Authorize` refuses a request without `code_challenge`, and it is the only path that issues a code. `ValidatePKCE` itself passes a code that has no challenge, so it would not reject one. |
| S256 only, `plain` refused and not advertised | §4.1.1, §7.5.2 | conforms | `pkg/iam/oidc.go:303`, `pkg/iam/oidc_types.go:613`, `pkg/iam/oidc.go:164` | Any method other than S256 is refused at both endpoints, and discovery lists only `S256`. |
| `code_challenge_method` is REQUIRED | §4.1.1 | conforms | `pkg/iam/oidc.go:313` | An authorization request without `code_challenge_method` is refused with `ErrPKCEMethodRequired` (`invalid_request`). |
| Exact string matching of redirect URIs | §2.3.1 | conforms | `pkg/iam/oidc.go:283`, `pkg/iam/oidc.go:1455` | The requested `redirect_uri` must equal a registered URI exactly, with no prefix or wildcard match. |
| Any port allowed for loopback redirect URIs | §2.3.1, §8.4.2 | conforms | `pkg/iam/oidc_redirect_uri.go:11` | `redirectURIMatches` ignores the port for `http://127.0.0.1` and `http://[::1]` URIs only, and matches every other URI exactly. |
| `redirect_uri` optional when one URI is registered | §4.1.1 | conforms | `pkg/iam/oidc.go:1001` | `validateAuthorizeClient` uses the single registered URI when `redirect_uri` is omitted, and refuses the request directly when the client registered more than one. |
| Redirect URI has no fragment | §2.3 | conforms | `pkg/iam/oidc_app.go:544`, `pkg/iam/oidc_app.go:315`, `pkg/api/rest/iam_dynamic_registration.go:458` | Registration and app update both run every redirect URI through `validateRedirectURI`, which refuses any `#`, and dynamic registration refuses one too. |
| Token request without `redirect_uri` | §4.1.3, §10.2 | conforms | `pkg/iam/oidc.go:1040` | The code exchange compares `redirect_uri` only when the client sends it, against the URI the authorize step resolved. |
| Authorization code used once | §4.1.3 | conforms | `pkg/iam/oidc_tokens.go:437` | `ValidateAuthCode` deletes the code before tokens are issued. Earlier tokens are not revoked on reuse, which is a SHOULD. |
| Refresh tokens sender-constrained or rotated | §4.3.1 | conforms | `pkg/iam/oidc.go:1202` | Every refresh revokes the presented token and issues a new one. Every client also authenticates with a secret (`pkg/iam/storage_backend.go:1062`). |
| Refresh scope limited to the original grant | §4.3.1 | conforms | `pkg/iam/oidc.go:1220` | A requested `scope` outside the original grant is refused with `invalid_scope`, and scopes are compared as exact strings. |
| Client credentials not in the request URI | §2.4.1 | departs | `pkg/api/rest/iam_oidc.go:209` | `r.FormValue` also reads the URL query, so `client_secret` in the query string is accepted. |
| Bearer tokens never accepted in the query string | §5.1 | conforms | `pkg/api/rest/auth_support.go:140`, `pkg/api/rest/iam_middleware.go:46` | Bearer tokens are read only from the `Authorization` header, and no handler reads `access_token` from the query. |
| Token endpoint error codes | §3.2.4 | conforms | `pkg/iam/errors.go:1193`, `pkg/iam/oidc.go:330` | The handlers return `invalid_request`, `invalid_client`, `invalid_grant`, `unauthorized_client`, `unsupported_grant_type` and `invalid_scope` from the -16 list. |
| 401 with `WWW-Authenticate` after a failed Basic authentication | §3.2.4 | conforms | `pkg/api/rest/iam_errors.go:185`, `pkg/api/rest/iam_oidc.go:312`, `pkg/api/rest/iam_oidc_introspect.go:73` | `challengeBasicClient` sets `WWW-Authenticate: Basic` on the 401 `invalid_client` response when the client authenticated with Basic. |
| Authorization endpoint errors redirected to the client | §4.1.2.1 | conforms | `pkg/api/rest/iam_oidc.go:162`, `pkg/api/rest/iam_oidc.go:193` | Once the client and redirect URI are valid, errors are redirected to the client with `error`, `error_description`, `state` and `iss`; only client and redirect URI errors stay a direct 400. |
