### Changed

- **Self-service registration is documented in the example config
  (go-quicbaas#358).** `.quicbaas.example.yaml` now has an
  `iam.self_service_registration.enabled: false` block, and the IAM
  configuration reference (`docs/iam/engineering.md`) explains it. The
  default is still invite-only. Setting the flag to `true` lets anyone create
  a tenant and become its Admin.
