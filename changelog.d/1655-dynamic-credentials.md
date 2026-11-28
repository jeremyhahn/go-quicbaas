### Added

- Dynamic secrets can now be generated: a role with `generator_type: credential` mints a random secret into go-qrdb's barrier-backed credential store in the caller's tenant, returns it once, and makes it resolvable as `credential://dynamic-<credential_id>`. Revoking or expiring the lease deletes it. The secret is never logged and never stored in the dynamic-secrets records (#1655).
