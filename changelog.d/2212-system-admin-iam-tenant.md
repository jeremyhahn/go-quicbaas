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
- A tenant unseal through the node's own endpoints keeps working after the
  node rebuilds its services, for example after a system encryption
  migration, and is answered as unavailable once shutdown has begun (#2212).
- On the embedded transport, `quicbaas iam tenant barrier unseal` and
  `unseal-share` act for the tenant `--tenant` names; an unseal for any
  other tenant is refused (#2212).
- WebAuthn credential administration inside a tenant uses that tenant's
  relying-party override, not the global relying party (#2212).
- After the node rebuilds its services, for example after a system encryption
  migration, IAM and the other tenant stores read the node's current storage,
  so a tenant unseal reloads that tenant's stored policies, denies included
  (#2212).
- A tenant unseal through the node's own endpoints is answered as
  unavailable (HTTP 503, gRPC `Unavailable`) while the node is stopping,
  and as permission denied (HTTP 403) for a caller that is not bound to
  that tenant, not as an internal error (#2212).
- Fleet node registration no longer reports an existing tenant as absent
  when the caller acts inside that tenant: the tenant check reads the
  catalog in the system namespace (#2212).
- Shutdown now refuses new per-tenant barrier calls from the moment it
  begins: a tenant unseal that arrives on a native transport while jobs
  drain or services stop is answered as unavailable instead of opening
  the barrier (#2212).
