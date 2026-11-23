### Changed

- **The MCP tool permission table names registered permissions** (#1658).
  Tools guarded by a go-qrdb or quicbaas permission are declared with that
  permission's constant instead of a typed resource and action, so a
  misspelled permission no longer compiles; the permission each tool requires
  is unchanged.
