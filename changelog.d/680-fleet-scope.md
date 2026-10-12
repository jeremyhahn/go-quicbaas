### Fixed

- **Listing another tenant's fleet nodes is refused, not answered empty
  (go-quicbaas#680).** The orchestration service checks the requested tenant
  against the caller's own before it scans the registry. Previously the store
  scanned the caller's keyspace, found nothing, and the listing returned an
  empty fleet with 200. The caller now gets 403 on REST and QUIC,
  `PermissionDenied` on gRPC and Unix, and the typed forbidden error on MCP and
  the embedded transport. The system tenant gets no cross-tenant lift. A store
  whose scan denies the prefix (go-qrdb#648) surfaces as the same forbidden
  error.
