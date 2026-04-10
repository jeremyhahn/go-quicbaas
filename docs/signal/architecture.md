# Signal Architecture

## Design Principles

The Signal integration follows QuicBaaS conventions: service-layer abstraction,
typed errors, lock-free preference, and context-based tenant routing. A single
`SignalService` instance serves all tenants without per-tenant goroutines or
long-lived state.

## Component Diagram

```
    Request (with tenant in ctx)
              |
    +---------v-----------+
    |   SignalService      |  Single instance, all tenants
    |   (service.go)       |
    +---------+-----------+
              |
              | getTenantStores(ctx)
              |
    +---------v-----------+
    | iam.TenantFromContext|  Extract tenant ID
    +---------+-----------+
              |
    +---------v-----------+
    | qrdbtransport       |
    | .WithTenant()       |  Wrap base KVClient with tenant prefix
    +---------+-----------+
              |
    +---------v-----------+
    |   KVAdapter          |  Bridge qrdb KVClient -> signal store.KVClient
    |   (kv_adapter.go)    |
    +---------+-----------+
              |
    +---------v-----------+
    | go-signal Managers   |  AccountsManager, MessagesManager,
    | (per-request scope)  |  KeysManager, ProfileStore
    +---------------------+
```

## Single Instance, Context-Based Routing

`signalService` holds a single go-qrdb `KVClient` and an in-memory cache of
tenant configurations (`map[string]*SignalTenantConfig`). There is no per-tenant
service instance.

On each request, `getTenantStores(ctx)`:

1. Extracts the tenant ID from `ctx` via `iam.TenantFromContext`.
2. Looks up the tenant configuration from the in-memory cache.
3. Wraps the base KV client with `qrdbtransport.WithTenant(kvClient, tenantID)`,
   which produces a `TenantKVClient` that prefixes all keys with the tenant
   namespace.
4. Wraps the tenant KV client in a `kvAdapter` to satisfy go-signal's
   `store.KVClient` interface.
5. Creates go-signal managers (`AccountsManager`, `MessagesManager`,
   `KeysManager`, profile store) backed by the adapted client.

These managers are request-scoped -- they are created per call, not cached.
This avoids cross-tenant state leaks and eliminates the need for per-tenant
connection pooling.

## KV Adapter Pattern

go-qrdb's `qrdbtransport.KVClient` and go-signal's `store.KVClient` have
similar but incompatible interfaces. The differences:

| Operation | go-qrdb                          | go-signal                              |
|-----------|----------------------------------|----------------------------------------|
| `Put`     | Variadic options                 | Plain `(key, value)` params            |
| `Get`     | Variadic options                 | Plain `(key)` params                   |
| `Scan`    | Returns `map[string][]byte`      | Callback-based `fn(key, value) error`  |

`kvAdapter` (in `kv_adapter.go`) bridges these by:

- Dropping variadic options on `Put`, `Get`, `Delete`, `List`, `Exists`.
- Converting `Scan` from map-return to callback-based iteration.

```go
adapter := signal.NewKVAdapter(tenantKV)
// adapter satisfies signalstore.KVClient
```

A compile-time interface check ensures the adapter stays in sync:

```go
var _ signalstore.KVClient = (*kvAdapter)(nil)
```

## Key Prefix Convention

Within a tenant namespace (already prefixed by `TenantKVClient`), Signal data
follows the go-signal key hierarchy:

```
signal/{domain}/{resource-type}/{identifier}
```

Concrete prefixes used by go-signal managers:

| Prefix                   | Owner              |
|--------------------------|--------------------|
| `signal/accounts/`       | Account records    |
| `signal/accounts/by-number/` | Phone number index |
| `signal/messages/`       | Message envelopes  |
| `signal/keys/`           | Pre-key bundles    |
| `signal/profiles/`       | User profiles      |
| `signal/fido2/`          | FIDO2 credentials  |

Tenant configuration is stored outside the tenant namespace at the global
level:

```
signal/tenants/{tenantID}/config
```

## Event Bridge

`EventBridge` publishes Signal protocol events to QuicBaaS messaging channels,
enabling real-time push to connected clients.

| Channel Pattern                      | Event                        |
|--------------------------------------|------------------------------|
| `signal:messages:{accountUUID}`      | New message received         |
| `signal:accounts:{eventType}`        | Account lifecycle (registered, deleted, updated) |

The bridge wraps the `messaging.Service` interface and publishes structured
JSON payloads. It is decoupled from `SignalService` and can be attached
independently.

## Tenant Configuration Lifecycle

On startup, `SignalService.Start` loads all persisted tenant configurations
from the KV store by listing keys under `signal/tenants/` and unmarshalling
each into a `SignalTenantConfig`. These are cached in a `sync.RWMutex`-protected
map for fast lookup during request processing.

Provisioning and deprovisioning update both the KV store (for persistence
across restarts) and the in-memory cache (for immediate effect).

## Concurrency Model

- The tenant config cache uses `sync.RWMutex` for read-heavy access patterns.
- go-signal managers are created per-request, eliminating shared mutable state
  across concurrent requests.
- The underlying KV client handles its own concurrency via Raft consensus.
