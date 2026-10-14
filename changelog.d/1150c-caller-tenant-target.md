### Security

- **A REST IAM path names only the caller's own tenant (go-quicbaas#1150).**
  The tenant-scoped IAM routes under `/api/v1/tenants/{tenant_id}/` used the
  path's tenant as given. These are apps, federation provider administration
  and account links, GDPR, MFA, policies, roles, sessions, users and WebAuthn
  credentials. They now resolve it through `tenantctx.Target`. A caller of
  another tenant, the system tenant and a tenantless request get `403`.
- **gRPC session event reads are confined the same way.**
  `SubscribeSessionEvents` and `GetSessionConversation` return
  `PermissionDenied` for a `tenant_id` that is not the caller's. An omitted
  `tenant_id` on `GetSessionConversation` now reads the caller's own
  conversation instead of the system tenant's.
