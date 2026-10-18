### Fixed

- The embedded transport now routes the `quicbaas.IAM.User` verbs `GetByEmail`, `FindByUsername`, `AssignRole`, `RevokeRole`, `Approve` and `Reject` to the identity store methods the REST, gRPC and MCP transports already call, instead of failing with an unknown-method error. `Suspend` and `Unsuspend` stay unrouted because no transport serves them (#1552).
