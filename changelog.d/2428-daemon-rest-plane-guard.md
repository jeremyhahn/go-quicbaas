### Added

- `sdk/go/platformtoken` has a route guard that a downstream daemon's REST
  plane mounts, so the daemon keeps no auth model, route guard or token format
  of its own (go-trusted-platform#644, owner ruling 133). Use
  `Validator.Middleware(permission)`, or `Validator.AuthorizeRequest` for a
  router without net/http middleware. The guard accepts a go-quicbaas platform
  token as `Bearer` or `DPoP`. It verifies a DPoP-bound token's proof against
  the request, requires the client certificate an mTLS-bound token
  (`cnf` `x5t#S256`) names, and requires the permission among the granted
  scopes. It binds the token's tenant and subject onto the request's context
  (`platformtoken.FromContext`), never a tenant or user named in the request.
  A refusal is 401 with a `Bearer` challenge, 403 `insufficient_scope`, or 503
  when the guard cannot decide, and the validator's error text is never sent
  to the caller (#2428).
