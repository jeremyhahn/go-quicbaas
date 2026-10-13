### Fixed

- **`iam.self_service_registration.default_role` now defaults to `User`
  (go-quicbaas#358).** It was documented as the default but never applied,
  so enabling self-registration without naming a role failed validation at
  startup. An explicit role is still honoured, and an invalid one is still
  rejected.
- **The environment now reaches every scalar
  `iam.self_service_registration.*` key (go-quicbaas#358).** Before, a
  `QUICBAAS_IAM_SELF_SERVICE_REGISTRATION_*` variable took effect only when
  the config file also named the key, so an environment-only deployment
  could not turn registration on. `allowed_isolation_modes` remains
  file-only.
