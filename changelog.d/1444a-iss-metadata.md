### Added

- **`authorization_response_iss_parameter_supported` (go-quicbaas#1444).** The
  OpenID configuration and OAuth authorization server metadata documents (and
  `iam.OAuthASMetadata`) advertise RFC 9207 §3 support; authorization success
  and error redirects carry `iss` equal to the advertised `issuer`.
