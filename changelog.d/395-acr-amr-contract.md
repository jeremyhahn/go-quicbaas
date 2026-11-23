### Fixed

- OIDC discovery now lists `acr` in `claims_supported`, since the hardware-attested login's ID token carries it (#395).

### Documentation

- The docs state that `acr` and `amr` appear only on the hardware-attested login's ID token and never on access tokens (#395).
