### Fixed

- Object storage: reading garbage-collection progress no longer races the running collection.
- Object storage: compaction returns an Unimplemented error instead of reporting a compaction that never ran (#1880).
- Object storage: garbage collection deletes backend objects that no catalog entry references, after a grace period, instead of reporting completion without collecting anything.

### Security

- Object storage: every object key is scoped to the caller's tenant, so two tenants can use the same key, and another tenant's object is not found on read, list, copy or delete (#32). Garbage-collection progress is visible only to the tenant that started it and to the system scope.
