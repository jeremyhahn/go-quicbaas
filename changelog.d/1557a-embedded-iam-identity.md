### Fixed

- The embedded transport now routes the `quicbaas.IAM.Identity` verbs
  `LinkIdentity`, `UnlinkIdentity` and `ListUserIdentities` to the IAM
  federation manager that REST, gRPC and MCP already call. Before, each call
  ended at `UnknownMethodError`. The handlers apply the same caller-tenant
  check as the gRPC federation server (#1557).
