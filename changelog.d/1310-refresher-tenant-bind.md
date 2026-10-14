### Fixed

- **The worker-image refresher now binds each tenant it acts for
  (go-quicbaas#1310).** Every pass bound only the orchestration tenant key,
  so the tenant-scoped agent catalog refused each call with "no tenant is
  bound to the request" and the pass skipped every tenant. The refresher and
  its smoke now bind go-qrdb's tenant key as well when they read bindings and
  catalogs, check the fleet, repoint agents and spawn the smoke session. A
  tenant whose catalog cannot be read is still reported as skipped and the
  pass carries on to the next tenant. No system-tenant lift was added.
