### Fixed

- **Role sentinels can be told apart (go-quicbaas#449).**
  `errors.Is` compares only the error code, so it treated a duplicate role
  name and a duplicate role id as the same error. It did the same for
  `ErrSystemRoleImmutable` and `ErrReservedRoleName` against any other
  forbidden or invalid-input error. Each sentinel now carries its own typed
  condition. `IsRoleAlreadyExists`, `IsRoleIDAlreadyExists`,
  `IsSystemRoleImmutable` and `IsReservedRoleName` each match only their own
  error. The code a client receives is unchanged.
- **Audit event IDs no longer fall back to a timestamp (go-quicbaas#450).**
  The audit store saves events under this ID, so two events with the same
  timestamp ID could overwrite each other. A CSPRNG failure now returns
  `AuditLogIDGenerationError` before any change is made. Admin assignment
  create/update/delete and impersonation start/end/record operations fail
  rather than run without an audit record.
  `BuildAuditEventForImpersonatedOperation` now returns an error as well.
- **Tenant barrier help shows the right command (go-quicbaas#228).** The
  examples said `quicbaas tenant barrier ...`, but the command is
  `quicbaas iam tenant barrier ...`. A test now checks that every example
  resolves through the real CLI root.
