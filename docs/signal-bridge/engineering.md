# Signal Bridge — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/signal-bridge/`.

## Global Configuration

`SignalConfig` controls whether the Signal service is active at the platform
level. When disabled, all Signal operations return `SignalNotEnabledError`.

```go
type SignalConfig struct {
    Enabled bool `yaml:"enabled"`
}
```

### YAML Example

```yaml
signal:
  enabled: true
```

## Per-Tenant Configuration

`SignalTenantConfig` controls Signal behavior for an individual tenant. Each
tenant must be provisioned with its own configuration before accounts can be
registered.

```go
type SignalTenantConfig struct {
    Enabled          bool   `yaml:"enabled"`
    MaxAccounts      int    `yaml:"maxAccounts"`
    MaxDevices       int    `yaml:"maxDevices"`
    MessageRetention string `yaml:"messageRetention"`
    FIDO2Enabled     bool   `yaml:"fido2Enabled"`
    AllowFIDO2Only   bool   `yaml:"allowFido2Only"`
}
```

### Fields

| Field              | Type   | Default  | Description                                       |
|--------------------|--------|----------|---------------------------------------------------|
| `enabled`          | bool   | `false`  | Whether Signal is active for this tenant           |
| `maxAccounts`      | int    | `10000`  | Maximum number of accounts the tenant can create   |
| `maxDevices`       | int    | `6`      | Maximum number of devices per account              |
| `messageRetention` | string | `"30d"`  | How long messages are retained (e.g., `30d`, `720h`) |
| `fido2Enabled`     | bool   | `false`  | Enable FIDO2/WebAuthn credential management        |
| `allowFido2Only`   | bool   | `false`  | Require FIDO2 for all authentication               |

### Defaults

Calling `ApplyDefaults()` sets zero-value fields to their defaults:

- `MaxAccounts` = 10000
- `MaxDevices` = 6
- `MessageRetention` = "30d"

`ApplyDefaults` is called automatically during `ProvisionTenant`.

## Validation Rules

`Validate()` checks the following constraints and returns `InvalidConfigError`
on the first failure:

| Field              | Rule                                                    |
|--------------------|---------------------------------------------------------|
| `maxAccounts`      | Must not be negative                                    |
| `maxDevices`       | Must not be negative                                    |
| `messageRetention` | Must be a duration string ending in `d` (days) or `h` (hours) with a numeric prefix (e.g., `30d`, `720h`) |
| `allowFido2Only`   | Requires `fido2Enabled` to be `true`                    |

`Validate` is called automatically during `ProvisionTenant`.

## Provisioning Example

```yaml
# Tenant with defaults
signal_tenant:
  enabled: true

# Tenant with custom limits
signal_tenant:
  enabled: true
  maxAccounts: 50000
  maxDevices: 10
  messageRetention: "90d"
  fido2Enabled: true

# FIDO2-only tenant
signal_tenant:
  enabled: true
  fido2Enabled: true
  allowFido2Only: true
```

## Storage

Tenant configurations are persisted in the KV store at:

```
signal/tenants/{tenantID}/config
```

The value is JSON-marshalled `SignalTenantConfig`. On service startup, all
tenant configs under this prefix are loaded into an in-memory cache for fast
per-request lookup.


---

## Signal Integration

### Overview

QuicBaaS integrates [go-signal](https://github.com/jeremyhahn/go-signal) as a
multi-tenant Signal messaging service. A single `SignalService` instance serves
all tenants, routing each request to the correct tenant namespace by extracting
the tenant ID from the request context via `iam.TenantFromContext`.

The integration lives in `pkg/signal/` and is conditionally compiled using
Go build tags. All Signal data is persisted through go-qrdb's KV client,
giving tenants the same Raft-replicated, strongly-consistent storage that
backs the rest of QuicBaaS.

### Build Tags

The Signal integration requires the `signal` build tag (IAM is always compiled):

```bash
go build -tags "signal" ./...
```

All files in `pkg/signal/` carry the `//go:build signal` directive. Without
this tag, the Signal package is excluded from the binary entirely.

### Quick Start

#### 1. Enable Signal in configuration

```yaml
signal:
  enabled: true
```

#### 2. Build with the signal tag

```bash
make build-signal
# or
go build -tags "signal" ./cmd/...
```

#### 3. Start the service

The `SignalService` is created and started as part of the QuicBaaS server
lifecycle:

```go
svc := signal.NewSignalService(kvClient, log, &signal.SignalConfig{
    Enabled: true,
})
if err := svc.Start(ctx); err != nil {
    return err
}
defer svc.Stop(ctx)
```

#### 4. Provision a tenant

Before a tenant can use Signal messaging, it must be provisioned:

```go
err := svc.ProvisionTenant(ctx, "tenant-abc", &signal.SignalTenantConfig{
    Enabled:          true,
    MaxAccounts:      10000,
    MaxDevices:       6,
    MessageRetention: "30d",
})
```

#### 5. Register accounts and send messages

All account and messaging operations require a context carrying the tenant ID
(set by IAM middleware):

```go
resp, err := svc.RegisterAccount(ctx, &signal.RegisterRequest{
    Number:     "+15551234567",
    Password:   "auth-token",
    DeviceName: "primary",
})

err = svc.SendMessage(ctx, &signal.SendMessageRequest{
    DestinationUUID: resp.UUID,
    DeviceID:        resp.DeviceID,
    Content:         ciphertext,
    Type:            1,
    Timestamp:       uint64(time.Now().UnixMilli()),
})
```

### Dependencies

| Dependency        | Purpose                                    |
|-------------------|--------------------------------------------|
| `go-signal`       | Signal protocol managers (accounts, keys, messages, profiles) |
| `go-qrdb`         | Distributed KV storage via `qrdbtransport.KVClient` |
| `go-quicbaas/iam` | Tenant ID extraction from request context  |

### Package Layout

```
pkg/signal/
    config.go        Global and per-tenant configuration
    errors.go        Typed error definitions
    event_bridge.go  Signal events to messaging channels
    kv_adapter.go    go-qrdb KVClient to go-signal store.KVClient adapter
    service.go       SignalService interface and implementation
```


---

## Signal Multi-Tenancy

### Isolation Model

Every tenant operates in a fully isolated Signal namespace. Tenant A cannot
read, write, or enumerate tenant B's accounts, messages, keys, or profiles.
Isolation is enforced at the KV storage layer through key prefixing -- there
is no application-level filtering or row-level security.

### Context-Based Routing

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

### TenantKVClient Key Prefixing

`qrdbtransport.WithTenant(kvClient, tenantID)` returns a `TenantKVClient` that
transparently prepends the tenant namespace to all keys. When the signal
adapter writes `signal/accounts/{uuid}`, the actual key stored is:

```
{tenant-namespace}/signal/accounts/{uuid}
```

This is invisible to go-signal managers -- they operate as if they own the
entire key space. The prefix is applied and stripped at the transport layer.

### Tenant Provisioning

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

### Tenant Deprovisioning

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

### Account Quotas

Each tenant has a configurable maximum number of accounts (`MaxAccounts`,
default 10000). During `RegisterAccount`, the service counts existing accounts
under the tenant namespace by listing keys with prefix `signal/accounts/`
(excluding index keys like `by-number/`). If the count meets or exceeds the
quota, `AccountQuotaExceededError` is returned.

Device limits are enforced per-account via `MaxDevices` (default 6), which
is passed to the go-signal `AccountsManager`.

### Error Types

| Error                          | Condition                                        |
|--------------------------------|--------------------------------------------------|
| `SignalNotEnabledError`        | Signal disabled globally or for the tenant       |
| `TenantNotFoundError`          | No configuration for the tenant ID               |
| `TenantAlreadyProvisionedError`| Tenant already has a Signal configuration        |
| `AccountQuotaExceededError`    | Tenant has reached its maximum account count     |
| `InvalidConfigError`           | Tenant configuration fails validation            |

### Configuration Caching

Tenant configs are cached in memory (`map[string]*SignalTenantConfig`) protected
by `sync.RWMutex`. The cache is populated on startup by scanning
`signal/tenants/` in the KV store, and updated on provision/deprovision
operations. This avoids a KV lookup on every request.


---

## Signal Messenger Bridge

Run a Signal bot backed by your QuicBaaS orchestrator. Operators chat with a normal Signal phone number; the bridge routes prompts into agent sessions and streams responses back. Encryption is end-to-end over the Signal Protocol; we use `go-libsignal` (PQXDH, Kyber, double ratchet) as a client, and an official-flavor `go-signal` server handles transport.

### Design Constraints

- **We do not modify `go-signal`.** The bridge registers as a normal client account against a running `go-signal` server, identical to any other Signal client. Upstream parity is deliberate so we track official Signal-Server behavior.
- The bridge is a separate process from the quicbaas daemon — one bridge per Signal account, scaling horizontally by running more bridges on different numbers.

### Components

- `pkg/signalbridge/http_transport.go` — REST + WebSocket client for the `go-signal` server. Uses `go-signal/pkg/api/rest` types and `go-signal/pkg/websocket.UnmarshalFrame` so wire shape tracks upstream exactly.
- `pkg/signalbridge/bot.go` — mapping between Signal inbound envelopes and orchestrator session events. Coalesces `assistant_chunk` streams into live-editable Signal messages, promotes large results to attachments, and enforces per-sender rate limits with exponential backoff.
- `pkg/signalbridge/registrar.go` — one-time account provisioning (verification code → account registration → pre-key upload).

### Deployment

1. Run a `go-signal` server (or use an existing one). The bridge needs a reachable base URL.
2. Build the CLI with the bridge included. The `signalbridge` verb exists only
   in builds tagged `quicbaas_signal`:
   ```bash
   go build -tags quicbaas_signal -o quicbaas ./cmd/quicbaas
   ```
3. Register a Signal account for the bot:
   ```bash
   quicbaas signalbridge register \
     --go-signal-url https://signal.example.com \
     --bot-identity +15551234567 \
     --config /etc/quicbaas/signalbridge.yaml
   ```
   `--state-path` overrides the state directory derived from the config.
4. Start the bridge. The daemon address, tenant and Signal server come from the
   bot config file:
   ```bash
   quicbaas signalbridge serve --config /etc/quicbaas/signalbridge.yaml
   quicbaas signalbridge status --config /etc/quicbaas/signalbridge.yaml
   ```

### User Experience

- **Prompt** — any direct message sent to the bot becomes a `prompt_submitted` event on a session keyed by (sender, conversation).
- **Streaming reply** — the bot sends an initial Signal message on first `assistant_chunk`, then edits it as more chunks arrive. Typing indicators are emitted throughout.
- **Tool calls** — shown as short interstitial messages ("Reading `/etc/hosts`…"), collapsed into the final reply on completion.
- **Large outputs** — results larger than 2 MB are uploaded as attachments; the reply links to them with a caption.
- **Rate limiting** — per-sender token bucket with exponential backoff on abuse. Returns a polite rate-limit notice rather than dropping silently.

### Exactly-Once Delivery

The bridge tracks `(session_id, sequence)` watermarks in local state, so reconnects after network hiccups replay from the last acknowledged event without duplicate Signal messages.

### Security

- All Signal traffic is E2E encrypted by `go-libsignal` — the bridge sees plaintext only in-memory between decryption and orchestrator dispatch.
- Bridge ↔ QuicBaaS uses OAuth2 client credentials with TLS; tokens are cached and refreshed on 401.
- Incoming senders are mapped to orchestrator operator identities through a configurable allowlist; unknown senders receive a fixed "unauthorized" reply.
