### Fixed

- The system admin's REST IAM requests now act in the tenant the URL names
  (#2212). The integration suite's data-plane enrollment no longer gets 403
  "no tenant is bound". The handlers now pass the resolved tenant context to
  every downstream call, so a role ID owned by another tenant is refused.
