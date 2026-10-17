### Fixed

- **KMS verbs on gRPC and REST resolve keys in the caller's tenant (#1476).**
  gRPC `RevokeKey`, `DeleteKey`, `GetKeyMetadata`, `UpdateKeyMetadata` and
  `UpdateKeyStatus`, and REST `UpdateKeyStatus` and `ListKeyVersions`, called
  the key manager with the raw key name and skipped the KMS service's tenant
  scoping. They now go through the KMS service, the same as the other verbs;
  the manager stays only as the fallback when no service is wired. gRPC
  `RevokeKey` now reports the service's `COMPROMISED` status, where the
  manager path reported `DISABLED`.

### Added

- **Every KMS transport is tested for tenant scope (#1476).** On the
  embedded, gRPC and REST transports, a table covers each key and DEK verb:
  tenant B naming tenant A's key or DEK gets the same not-found a missing id
  gets and leaves tenant A's keys, policies and DEK unchanged, and tenant A
  still reaches its own. The tests run the real KMS service over
  `pkg/testutil/kmstenant`, an in-memory key manager shared by the transport
  tests.
