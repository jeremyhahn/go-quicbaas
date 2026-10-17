### Added

- **An x/oauth2 + go-jose interop leg tests the OIDC/OAuth2 provider.**
  `pkg/api/rest/oauth2_interop_test.go` drives discovery, the authorization
  code grant with PKCE and refresh, the client credentials grant,
  introspection (RFC 7662) and revocation (RFC 7009) through
  `golang.org/x/oauth2` and `go-jose`. A revocation authenticated with HTTP
  Basic answers 200 and the token then introspects as inactive.
