### Fixed

- The system admin's REST IAM requests now act in the tenant the URL names
  (#2212). The integration suite's data-plane enrollment no longer gets 403
  "no tenant is bound". The handlers now pass the resolved tenant context to
  every downstream call, so a role ID owned by another tenant is refused.
- A tenant whose barrier is being unsealed stays closed to requests until its
  unseal checks pass, and a failed check keeps it closed instead of resealing.
- A tenant unseal on the embedded transport now goes through the same hold
  and unseal checks as one over REST, gRPC or QUIC, and is refused for a
  caller that is not bound to that tenant.
- Requests to a tenant that is still running its unseal checks now answer
  503 (gRPC `UNAVAILABLE`) on the native REST and gRPC transports instead of
  401 (`UNAUTHENTICATED`).
- A tenant held because several of its stored policies cannot load can now be
  repaired one policy at a time: the system admin's update or delete of each
  policy is kept, an update that does not compile is refused, and the tenant
  stays closed until its unseal checks pass.
- A policy repair that arrives after a held tenant's unseal checks have loaded
  its policies, but before the tenant opens, is rejected when its reload
  fails, like any other policy write.
- A tenant that is held at unseal still answers the node health check, so an
  SDK or CLI client can connect and retry the unseal.
