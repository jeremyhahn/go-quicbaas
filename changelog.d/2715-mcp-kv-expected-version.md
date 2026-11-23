### Added

- The MCP key-value put and delete tools take an optional `expectedVersion`.
  A stale version fails with a resource-conflict error and changes nothing;
  omitting it keeps the unconditional write (#2715).
