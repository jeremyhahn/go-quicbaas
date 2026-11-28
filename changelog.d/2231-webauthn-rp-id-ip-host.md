### Changed

- quicbaasd refuses a configuration whose `server.host` is a routable IP
  address (for example a LAN address) while `iam.webauthn.rp_id` is unset
  (#2231). The error names `iam.webauthn.rp_id` and is reported at load,
  before any listener starts. Before this change, startup failed later, in
  IAM initialisation, or WebAuthn failed at login. A browser refuses an IP
  address as a WebAuthn RP ID, so set `rp_id` (and `rp_origins`) to the
  domain clients use, or set `server.host` to a hostname. Loopback and
  wildcard hosts still default to `localhost`.
