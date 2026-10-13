### Added

- **Sysadmin tenant provisioning (go-quicbaas#358).** `POST
  /api/v1/tenants/{tenant_id}/provision` creates a tenant and its first admin
  in one call. It is the onboarding path when public registration is off.
  The admin enrolls by CSR (`cert`, the default) or by a one-time invite token
  (`invite`). The call is served over REST, QUIC, gRPC, Unix, MCP
  (`quicbaas_iam_tenant_provision`) and the embedded transport, and is
  reachable from the SDK as `iam.TenantClient.Provision`. It requires
  `tenants:create`. Released in the next tag.
