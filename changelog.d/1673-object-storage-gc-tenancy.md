### Changed

- Object storage: the catalog of object metadata is stored in go-qrdb instead of a separate PebbleDB directory under the data directory. An existing catalog directory is not imported (pre-1.0).

### Fixed

- Object storage: reading garbage-collection progress no longer races the running collection.
- Object storage: compaction returns an Unimplemented error instead of reporting a compaction that never ran (#1880).
- Object storage: garbage collection reports, as a dry run, the backend objects no catalog entry references after a grace period, counting replica and archive copies as referenced, instead of reporting completion without scanning anything. A deleting run returns Unimplemented until the catalog is shared across nodes.
- Catalog: listing by prefix follows its cursor.
- Object storage: a tenant's put no longer fails with an invalid namespace; the catalog accepts the tenant-scoped namespace `tenant/{tenant}/{namespace}`.

### Security

- Object storage: every object key is scoped to the caller's tenant, so two tenants can use the same key, and another tenant's object is not found on read, list, copy or delete (#32). Garbage collection spans every tenant, so only the system scope can run it.
