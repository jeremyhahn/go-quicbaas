### Fixed

- Fleet node registration finds a tenant IAM created when the request is
  bound to that tenant. The orchestration tenant check now reads the tenant
  directory in the system scope, where tenant records are kept, instead of the
  caller's own tenant scope, which held no record and refused the registration
  with `tenant_id: tenant "<id>" does not exist` (#1605).
