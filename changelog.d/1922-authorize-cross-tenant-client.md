### Security

- The authorization endpoint no longer issues a code to an end user for a client registered in another tenant. Once the client and redirect URI are validated, the refusal is redirected to the client as `access_denied` with the `state` and no code (RFC 6749 §4.1.2.1); an end user with no tenant is refused too (#1922).
