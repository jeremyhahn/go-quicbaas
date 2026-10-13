### Changed

- **`quicbaasd` now refuses to start on an invalid enabled
  `iam.self_service_registration` section (go-quicbaas#358).** The section's
  validation existed but no startup path ran it, so an unknown
  `default_role`, an unsupported isolation mode, a `default_isolation_mode`
  outside `allowed_isolation_modes`, or `barrier`/`volume` allowed with an
  explicit empty `tenant_barrier_volume_pool` booted and failed only at the
  first registration. The boot-time config validator now runs it, and the
  daemon exits naming the offending key. A disabled section is not checked,
  and an enabled section with no other keys set still starts.
