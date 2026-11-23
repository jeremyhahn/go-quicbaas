### Fixed

- An administrator can add a passkey to their own account over MCP. The node serves the add-credential and finish-registration FIDO2 tools on user_authfactors:add, the permission their REST and gRPC routes take, in the caller's own tenant only. The other FIDO2 tools stay off MCP (#1700).
