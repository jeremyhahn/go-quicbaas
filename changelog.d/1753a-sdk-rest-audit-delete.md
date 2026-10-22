### Fixed

- The SDK REST transport serves the audit `Delete` verb as
  `DELETE /api/v1/iam/audit?tenant_id=&before=` instead of returning
  `Unimplemented`. A dry-run or event-type delete is refused with the shared
  typed errors before any request is sent, as on MCP and embedded (#1753).
