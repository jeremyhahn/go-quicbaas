### Fixed

- **The embedded qrdb node pins the configured SPIFFE trust domain (#125).**
  `iam.spiffe_trust_domain`, set in the config file or by an SDK consumer on
  `sdk.ServerFullConfig`, only reached quicbaas's own first-boot and
  transports; the embedded qrdb node never received it, so its mTLS
  authenticator did not pin the domain the admin certificate was issued under
  and its built-in CA minted certificates without a SPIFFE SAN. The value is
  now passed to the node as its `auth.spiffe_trust_domain`. Left unset, the
  node keeps qrdb's default (no SPIFFE pin).
