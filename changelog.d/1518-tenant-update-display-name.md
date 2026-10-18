### Fixed

- **A tenant update of DisplayName sets the display name, not the name.**
  Every transport sent `DisplayName` as the tenant's `Name` or dropped it,
  so the update renamed the tenant and a read back still showed the old
  display name. REST, QUIC, gRPC, Unix, MCP and the embedded transport now
  store it in `Settings.DisplayName`, starting from the stored settings so
  no other setting is lost, and the SDK reads `Tenant.DisplayName` back
  from `settings.display_name`. The REST server and the MCP
  `quicbaas_iam_tenant_update` tool accept `display_name`; the gRPC
  `TenantSettings` gains `display_name`, and its scalars are now optional,
  so `mfa_required=false` and `max_users=0` are applied rather than
  ignored.
