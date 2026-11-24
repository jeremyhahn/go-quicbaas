### Added

- Tenant Agent foundation: each tenant can appoint one Tenant Agent, bound to a lead-capable catalog agent or pool, with its own tenant-scoped certificate principal, a read-only role and a tenant signing key; it never reads or writes another tenant (#1695).
- Tenant plans: a tenant's appointed agent sets its ranked project priorities and milestones as one revisioned plan, signed with the agent's tenant key and verified on every read; it names only the tenant's own projects (#1695).
