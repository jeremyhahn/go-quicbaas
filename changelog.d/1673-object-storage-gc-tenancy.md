### Security

- Object storage keys are now scoped to the caller's tenant. Two tenants can store the same key without overwriting each other, and one tenant can no longer read, list or delete another tenant's object by naming its key. Keys in responses come back unscoped (#1673).
- A garbage collection operation records the tenant that started it. Another tenant asking for its progress gets not found (#1673).

### Fixed

- Object storage garbage collection now deletes orphaned backend objects that no catalog entry references and that are older than the grace window: one hour, or five minutes in aggressive mode. A dry run reports what it would delete. Previously a run deleted nothing (#1673).
- `CompactBackend` now returns an unimplemented error, instead of reporting a compaction that never ran (#1673).
- Reading garbage collection progress while a run is in progress no longer races with the run (#1673).
