### Fixed

- **`iam.self_service_registration.tenant_barrier_volume_pool` now defaults
  to `default` (go-quicbaas#358).** It was documented as the default but
  never applied, so allowing `barrier` or `volume` isolation without naming a
  pool failed validation. An explicit pool is still honoured, and an explicit
  empty pool with either mode allowed is still rejected.
- **`iam.self_service_registration.default_isolation_mode` now defaults to
  `keyspace` in the config itself (go-quicbaas#358).** The validator used to
  substitute `keyspace` for an empty value; it no longer does, so an explicit
  empty `default_isolation_mode` is now rejected like any other mode outside
  `allowed_isolation_modes`.
