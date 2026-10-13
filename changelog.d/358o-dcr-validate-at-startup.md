### Changed

- **`quicbaasd` now refuses to start when
  `iam.dynamic_client_registration.anonymous_tenant_id` is `system` on an
  enabled section (go-quicbaas#358).** That setting would let an
  unauthenticated caller mint OAuth2 clients in the control plane. The
  section's validation existed but no startup path ran it; the boot-time
  config validator now does, and the daemon exits naming the offending key.
  `POST /oauth2/register` already refused a system landing tenant with 403 at
  request time, and still does. A disabled section is not checked.
