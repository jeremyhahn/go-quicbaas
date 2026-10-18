### Added

- **`iam.ErrRedirectURIRequired` (go-quicbaas#1439).** An authorization
  request that omits `redirect_uri` for a client with several (or no)
  registered redirect URIs is refused with this typed `invalid_request`
  error, reachable with `errors.Is` through the `AuthorizeDirectError`.
  The sole-URI default itself is described under go-quicbaas#1439 in
  `1434a-redirect-uri-rules.md`.
