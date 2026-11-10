### Fixed

- The system admin's REST IAM requests now act in the tenant the URL names
  (#2212). The integration suite's data-plane enrollment no longer gets 403
  "no tenant is bound". The handlers now pass the resolved tenant context to
  every downstream call, so a role ID owned by another tenant is refused.
- A system admin's REST IAM request that names another tenant is authorized
  in that tenant before it acts there, so that tenant's deny policies apply.
- A tenant whose barrier is sealed no longer stops the ABAC policy reload for
  every other tenant: its unreadable policies are replaced by a deny for
  everything in that tenant except its barrier, and the set is reloaded when
  the tenant is unsealed (#2212).
