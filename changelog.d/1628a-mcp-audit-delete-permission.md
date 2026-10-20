### Security

- The MCP `quicbaas_iam_audit_delete` tool now requires `audit:delete` on the target tenant before it prunes audit history, the same check REST `DeleteOldAuditEvents` and the embedded transport make; a principal granted only `audit:query` is refused with a permission-denied error, and a call with no authenticated subject fails closed (#1628).
