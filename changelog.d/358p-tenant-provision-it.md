### Changed

- **Tenant provisioning is verified end to end against a running daemon
  (go-quicbaas#358).** New IAM integration tests provision a tenant in cert
  mode (the returned certificate logs in over mTLS and reads the tenant's
  users) and in invite mode (the admin stays pending until the invite is
  redeemed over PIV, and the token is refused a second time), assert the
  system principal is refused a read of the provisioned tenant's users, and
  assert invite provisioning of the system tenant is a 400, not a 500. The
  integration helper's PIV invite activation now calls the per-tenant route
  `POST /api/v1/iam/{tenant_id}/invites/{token}/activate/piv`; it called a
  path without the tenant segment that no route serves.
