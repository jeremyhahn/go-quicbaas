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
- A tenant unsealed through the node's own REST, gRPC, QUIC, Unix or MCP
  endpoints, or on the embedded transport, now has its stored policies
  reloaded the same way an unseal through the broker does, and the unseal is
  refused for a caller that is not bound to that tenant (#2212).
- ABAC policy reloads now run one at a time, so an older, slower reload can
  no longer replace the set a newer one loaded (#2212).
