### Added

- **Token introspection reports attestation (go-quicbaas#393).** For an active
  access token that carries attestation, `POST /oauth2/introspect` now returns
  `attest_level` and `attest_aaguid`, the same values the JWT carries. An
  introspecting resource server can now enforce a hardware-only policy, as a
  JWT-parsing one already could. Both fields are omitted for an unattested
  token, and an inactive token still returns only `{"active":false}`. The Go
  SDK's `service.TokenIntrospection` has the matching `AttestLevel` and
  `AttestAAGUID` fields. Userinfo does not report attestation.
