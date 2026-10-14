### Security

- **The system tenant gets no lift on the account, agent catalog, cost
  settings and spend policy services (go-quicbaas#1150).** A request's
  `tenant_id` now resolves through `tenantctx.Target`: a caller names only
  its own tenant, and the system tenant naming another tenant is refused
  (`TenantTargetDeniedError`; REST 403, gRPC `PermissionDenied`), as is a
  caller with no tenant. Embedded SDK callers acting for a tenant name it
  with `transport.ContextWithTenant`.
- **gRPC IAM, federation, project and session servers confine
  `tenant_id` to the caller's own tenant (go-quicbaas#1150).** Each
  tenant-scoped method checks the requested tenant before the service is
  reached; another tenant, the system tenant and a tenantless caller get
  `PermissionDenied`. Federated login begin, invite activation and tenant
  provisioning are unchanged.
