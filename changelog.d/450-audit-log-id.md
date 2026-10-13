### Fixed

- **Audit log IDs are unique by construction (#450).** An ID is still
  `audit_` followed by 32 hex digits, now an 8-byte timestamp that never
  repeats within a process (one past the last value when the clock has not
  advanced or has stepped back) plus 8 random bytes from `xkms.Rand` that
  separate processes. Uniqueness within a process no longer depends on the
  random bytes or the clock. A CSPRNG failure is still refused with
  `AuditLogIDGenerationError` (#449), so no audited action proceeds without
  an ID. `MemoryAuditStorage.SaveEvent` now rejects an ID it already holds
  with `ErrAuditEventExists` instead of overwriting the record. An audit log
  ID addresses records; it is not a capability, and reading one still needs
  `audit:read` and the record's tenant scope.
