### Added

- `sdk/go/platformtoken` has a route guard that a downstream daemon's REST
  plane mounts, so the daemon keeps no auth model, route guard or token format
  of its own (go-trusted-platform#644). Use `Validator.Middleware(permission)`,
  or `Validator.AuthorizeRequest` for a router without net/http middleware.
  The guard accepts a go-quicbaas platform token as `Bearer` or `DPoP`. It
  verifies a DPoP-bound token's proof against the request and the presented
  token (its ath claim), requires the client certificate an mTLS-bound token
  (`cnf` `x5t#S256`) names, requires the permission among the granted scopes,
  and asks the go-qrdb authorizer (`Options.Authorizer`, RBAC with its ABAC
  policies) for it. The authorizer sees the token's subject, roles and
  authentication claims (`attest_method`, `attest_level` and the like), so an
  ABAC policy that denies one of the caller's roles or authentication claims
  refuses the request, and a role- or attribute-selected allow never admits a
  request that the role-less decision refuses. It binds the token's tenant and subject onto the
  request's context (`platformtoken.FromContext`), never a tenant or user
  named in the request. A refusal is 401 with a `Bearer` challenge, 403
  `insufficient_scope` or `access_denied`, or 503 when the guard cannot
  decide, and the validator's error text is never sent to the caller (#2428).
