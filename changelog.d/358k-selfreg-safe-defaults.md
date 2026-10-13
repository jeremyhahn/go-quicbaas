### Fixed

- **`iam.self_service_registration.require_admin_approval` now defaults to
  `true` (go-quicbaas#358).** It was documented as the safe default but was
  never applied, so the server ran with `false`: with self-registration on,
  new accounts were active without admin approval. An explicit
  `require_admin_approval: false` is still honoured.

### Added

- **Startup warning when self-service registration is on
  (go-quicbaas#358).** With `iam.self_service_registration.enabled: true`,
  the server logs one warning that names the flag, reports
  `require_admin_approval`, and states that anyone can create a tenant and
  become its Admin.
