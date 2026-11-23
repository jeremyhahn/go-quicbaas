### Fixed

- OIDC discovery now lists `acr` in `claims_supported`, since the hardware-attested login's ID token carries it (#395).

### Documentation

- The docs state that `acr` appears only on the hardware-attested login's ID token, that `amr` appears on the ID token of every PIV/mTLS or passkey login, attested or not (an unattested PIV login reports `["mtls"]`), and that access tokens carry neither (#395).
