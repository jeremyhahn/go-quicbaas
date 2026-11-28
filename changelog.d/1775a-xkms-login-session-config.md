### Changed

- The hosted xkms login module now takes its session options from quicbaas config: its tokens carry the issuer and the access and refresh token lifetimes the OIDC provider uses, and its per-client login rate is the new `iam.login_rate_per_min` (zero keeps the module default; a negative value is refused at boot) (#1775, #1901).
