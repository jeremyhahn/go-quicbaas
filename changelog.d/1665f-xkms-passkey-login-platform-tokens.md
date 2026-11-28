### Fixed

- A passkey (FIDO2) login through the hosted xkms routes `/api/v1/webauthn/login/finish` and `/api/v1/webauthn/sdk/authentication/finish` now returns platform tokens signed by the node's OIDC provider, so the token's key ID is in the published JWKS and the token verifies against it (#1665).
