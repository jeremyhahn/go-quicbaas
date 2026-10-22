### Fixed

- Audit delete refuses `dry_run` and `event_types` on the server MCP tool
  `quicbaas_iam_audit_delete`, the SDK MCP transport and the REST route
  `DELETE /api/v1/iam/audit` (400 on `?dry_run=` or `?event_types=`), as embedded already
  did, instead of silently deleting every event before the cutoff. The typed
  errors `AuditDeleteDryRunUnsupportedError` and
  `AuditDeleteEventTypesUnsupportedError` now live in `sdk/go/client/iam`,
  shared by every transport (#1639).
- The MCP tool `quicbaas_iam_audit_delete` refuses a `dry_run` that is not a
  JSON boolean (a string such as `"true"`, a number, `null` or an object)
  with the invalid-input `BoolArgError` before any deletion, instead of
  reading it as false and deleting; its input schema now declares `dry_run`
  as a boolean (#1639).
- The REST route `DELETE /api/v1/iam/audit` refuses a malformed query string
  (such as `dry_run=true;` or `dry_run=%ZZ`) with 400 before any deletion,
  instead of dropping the malformed pair and deleting (#1639).
- The REST route `DELETE /api/v1/iam/audit` refuses a repeated `dry_run`,
  `tenant_id` or `before` (such as `dry_run=false&dry_run=true`) with 400 and
  the typed `AuditDeleteRepeatedParamError` before any deletion, instead of
  letting the first value win and deleting (#1639).
