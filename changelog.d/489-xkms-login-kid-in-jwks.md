### Fixed

- The JWKS now also publishes the OIDC signing key under the kid the xkms login module writes into its session tokens, so a login token from that module verifies at the node (#489).
