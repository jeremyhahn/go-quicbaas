### Fixed

- **The fleet enrollment E2E reads the fleet listing the handler answers
  with (go-quicbaas#1355).** The listing helpers decoded an `agents` key that
  `GET /api/v1/fleet` stopped emitting when the endpoint moved to the paged
  `nodes` envelope, so every listing read as empty. The enrollment flow
  failed on its first positive check, and the "not listed under `system`"
  check passed whatever the registry held. Both now read `nodes`.
- **The pooled-account E2E seeds, revokes and rotates accounts the way an
  operator does (go-quicbaas#1355).** It used to call the account service
  with no caller bound, which the service has refused since go-quicbaas#1150.
  It now sends each write as a tenant operator over the production routes,
  behind the same mTLS and RBAC middleware the daemon's reads go through.
  These tests still fail, with a 403: the write routes require go-qrdb's
  `credentials:*` grants, which only a system-tenant principal can hold. The
  service refuses a system caller acting for another tenant, so today no
  principal can write a tenant's pool.
