# Signal Multi-Tenancy

## Isolation Model

Every tenant operates in a fully isolated Signal namespace. Tenant A cannot
read, write, or enumerate tenant B's accounts, messages, keys, or profiles.
Isolation is enforced at the KV storage layer through key prefixing -- there
is no application-level filtering or row-level security.

## Context-Based Routing

Tenant identification flows through the standard QuicBaaS IAM middleware.
The `iam.TenantFromContext(ctx)` function extracts the tenant ID from the
request context. All `SignalService` methods that operate on tenant data use
this mechanism.

Request flow:

```
HTTP/gRPC request
    -> IAM middleware sets tenant ID in context
    -> SignalService.RegisterAccount(ctx, req)
        -> getTenantStores(ctx)
            -> iam.TenantFromContext(ctx) -> "tenant-abc"
            -> qrdbtransport.WithTenant(kvClient, "tenant-abc")
            -> NewKVAdapter(tenantKV)
            -> newSignalStores(adapter, maxDevices)
        -> stores.accounts.Create(ctx, account)
```

If the context does not carry a tenant ID, `TenantNotFoundError` is returned.

## TenantKVClient Key Prefixing

`qrdbtransport.WithTenant(kvClient, tenantID)` returns a `TenantKVClient` that
transparently prepends the tenant namespace to all keys. When the signal
adapter writes `signal/accounts/{uuid}`, the actual key stored is:

```
{tenant-namespace}/signal/accounts/{uuid}
```

This is invisible to go-signal managers -- they operate as if they own the
entire key space. The prefix is applied and stripped at the transport layer.

## Tenant Provisioning

Before a tenant can use Signal, it must be provisioned via `ProvisionTenant`.
This method:

1. Validates that Signal is globally enabled.
2. Validates the tenant ID is non-empty.
3. Applies default values to the tenant config.
4. Validates the tenant config.
5. Checks that the tenant is not already provisioned (returns
   `TenantAlreadyProvisionedError` if so).
6. Persists the config to `signal/tenants/{tenantID}/config` in the KV store.
7. Adds the config to the in-memory cache.

```go
err := svc.ProvisionTenant(ctx, "tenant-abc", &signal.SignalTenantConfig{
    Enabled:     true,
    MaxAccounts: 50000,
})
```

## Tenant Deprovisioning

`DeprovisionTenant` removes a tenant's Signal configuration:

1. Validates the tenant ID.
2. Checks that the tenant exists (returns `TenantNotFoundError` if not).
3. Deletes the config from the KV store.
4. Removes the config from the in-memory cache.

Note: Deprovisioning removes the configuration only. Tenant data (accounts,
messages, keys) remains in the KV store. A separate data cleanup process
should be used to purge tenant data if required.

```go
err := svc.DeprovisionTenant(ctx, "tenant-abc")
```

## Account Quotas

Each tenant has a configurable maximum number of accounts (`MaxAccounts`,
default 10000). During `RegisterAccount`, the service counts existing accounts
under the tenant namespace by listing keys with prefix `signal/accounts/`
(excluding index keys like `by-number/`). If the count meets or exceeds the
quota, `AccountQuotaExceededError` is returned.

Device limits are enforced per-account via `MaxDevices` (default 6), which
is passed to the go-signal `AccountsManager`.

## Error Types

| Error                          | Condition                                        |
|--------------------------------|--------------------------------------------------|
| `SignalNotEnabledError`        | Signal disabled globally or for the tenant       |
| `TenantNotFoundError`          | No configuration for the tenant ID               |
| `TenantAlreadyProvisionedError`| Tenant already has a Signal configuration        |
| `AccountQuotaExceededError`    | Tenant has reached its maximum account count     |
| `InvalidConfigError`           | Tenant configuration fails validation            |

## Configuration Caching

Tenant configs are cached in memory (`map[string]*SignalTenantConfig`) protected
by `sync.RWMutex`. The cache is populated on startup by scanning
`signal/tenants/` in the KV store, and updated on provision/deprovision
operations. This avoids a KV lookup on every request.
