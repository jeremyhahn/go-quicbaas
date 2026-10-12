### Fixed

- **A tenant's WebAuthn override is validated when it is written
  (go-quicbaas#368).** Creating or updating a tenant with a malformed
  `settings.webauthn` RP ID or origin is refused with an `ErrInvalidInput`
  error naming the field (REST 400, gRPC `InvalidArgument`), held to the same
  rules as the global `iam.webauthn` config. Those rules now live in
  `pkg/webauthnrp`, shared by `pkg/config` and `pkg/iam`.
- **A stored bad override fails closed.** The per-tenant WebAuthn factory
  validates the merged relying party before building it and returns
  `*iam.WebAuthnTenantRPError` instead of building a relying party that fails
  every ceremony; an override stored before this release is caught there.
