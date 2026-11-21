### Fixed

- gRPC: `QueryAuditEvents`, `GetAuditEvent` and `RecordAuditEvent` are authorized with the same permissions as REST and MCP (`audit:query`, `audit:read`, `audit_events:record`), so a caller holding them is no longer refused (#1751).
