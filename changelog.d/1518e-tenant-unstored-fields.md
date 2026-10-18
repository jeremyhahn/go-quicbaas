### Fixed

- **A tenant update that sets Description, Metadata or Status is refused,
  not reported as stored.** None of the three is stored yet: Metadata and
  Description wait on go-qrdb's tenant seam row, and Status on the owner
  queue. `TenantClient.Update` now returns `TenantFieldNotStoredError`
  (matching `ErrTenantFieldNotStored`), naming the fields in the order
  description, metadata, status, before any transport call. The refusal is
  whole: a DisplayName in the same update is not applied either. The
  servers refuse the same fields: REST answers 400, the MCP
  `quicbaas_iam_tenant_update` tool answers invalid input, and the embedded
  handler returns the typed error. The gRPC `UpdateTenantRequest` cannot
  carry the fields, so the SDK gRPC handler, which Unix shares, refuses
  them before any call. A tenant read still returns no Metadata or Status.
