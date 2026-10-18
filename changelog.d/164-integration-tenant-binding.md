### Fixed

- **The integration suites bind a tenant like a production caller; the soak
  runs again (#164).** The system admin provisions each fixture tenant through
  `POST /api/v1/tenants/{id}/provision` (cert mode) and the tenant's own admin,
  logged in with the signed certificate, creates the data-plane role and user
  and registers the auth suite's OAuth app. The fixtures no longer act for a
  tenant with the system admin's bearer or with no credential, which the
  tenant check refuses. `TestRegisterAppRequiresBoundTenant` keeps both
  refusals covered.
- **A soak keeps its tenant-admin access and leaves no fixture tenants behind
  (#164).** The tenant admin's bearer lives about 15 minutes; the helpers'
  `TenantAdminSession` and the CLI runner now log the admin in again with its
  certificate when the bearer nears expiry. Every tenant a fixture provisions
  is deleted by the system admin when its test ends, and the CLI runner
  provisions only once a command actually runs, so a transport that skips
  the test creates no tenant.
