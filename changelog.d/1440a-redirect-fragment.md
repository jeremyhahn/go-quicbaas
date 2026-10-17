### Fixed

- **Redirect URIs with a fragment are refused (go-quicbaas#1440).**
  draft-ietf-oauth-v2-1 §2.3 forbids a fragment component in a redirect URI.
  App registration now refuses one with `RedirectURIFragmentError`, and
  RFC 7591 dynamic client registration answers `invalid_redirect_uri`. A
  trailing bare `#` counts as a fragment on both paths.
- **The `crypto:` block link in `docs/kms/engineering.md` carries its anchor
  (go-quicbaas#1258).**
