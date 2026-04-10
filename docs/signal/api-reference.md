# Signal API Reference

## SignalService Interface

`SignalService` is the primary interface for the multi-tenant Signal messaging
service. A single instance serves all tenants. Methods that operate on tenant
data extract the tenant ID from `ctx` via `iam.TenantFromContext`.

```go
type SignalService interface {
    // Account operations
    RegisterAccount(ctx context.Context, req *RegisterRequest) (*RegisterResponse, error)
    GetAccount(ctx context.Context, uuid string) (*accounts.Account, error)
    DeleteAccount(ctx context.Context, uuid string) error

    // Messaging
    SendMessage(ctx context.Context, req *SendMessageRequest) error
    GetMessages(ctx context.Context, accountUUID string, deviceID uint64) ([]*messages.Envelope, error)

    // Keys
    StorePreKeys(ctx context.Context, accountUUID string, deviceID uint64, bundle *keys.SetKeysRequest) error
    GetPreKeys(ctx context.Context, accountUUID string, deviceID uint64) (*keys.PreKeyResponseItem, error)

    // Tenant management
    ProvisionTenant(ctx context.Context, tenantID string, cfg *SignalTenantConfig) error
    DeprovisionTenant(ctx context.Context, tenantID string) error
    GetTenantConfig(ctx context.Context, tenantID string) (*SignalTenantConfig, error)

    // Lifecycle
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
}
```

## Request/Response Types

### RegisterRequest

```go
type RegisterRequest struct {
    Number     string `json:"number"`     // Phone number (E.164 format)
    Password   string `json:"password"`   // Auth token for the account
    DeviceName string `json:"deviceName"` // Human-readable device name
}
```

### RegisterResponse

```go
type RegisterResponse struct {
    UUID     string `json:"uuid"`     // Assigned account UUID
    Number   string `json:"number"`   // Registered phone number
    DeviceID uint64 `json:"deviceId"` // Primary device ID
}
```

### SendMessageRequest

```go
type SendMessageRequest struct {
    DestinationUUID string `json:"destinationUuid"` // Recipient account UUID
    DeviceID        uint64 `json:"deviceId"`         // Target device ID
    Content         []byte `json:"content"`          // Encrypted message content
    Type            int    `json:"type"`             // Envelope type
    Timestamp       uint64 `json:"timestamp"`        // Unix timestamp (milliseconds)
}
```

## Account Operations

### RegisterAccount

Creates a new Signal account within the tenant namespace. Enforces the
tenant's `MaxAccounts` quota. Returns `AccountQuotaExceededError` if the limit
is reached.

**Errors:** `SignalNotEnabledError`, `TenantNotFoundError`,
`AccountQuotaExceededError`

### GetAccount

Retrieves a Signal account by UUID.

**Errors:** `SignalNotEnabledError`, `TenantNotFoundError`,
`KeyNotFoundError` (from store)

### DeleteAccount

Removes a Signal account and all associated data.

**Errors:** `SignalNotEnabledError`, `TenantNotFoundError`

## Messaging Operations

### SendMessage

Stores a message envelope for delivery to the specified device. The message
content should be encrypted client-side using the Signal Protocol before
sending.

**Errors:** `SignalNotEnabledError`, `TenantNotFoundError`

### GetMessages

Retrieves all pending message envelopes for a specific account and device.

**Errors:** `SignalNotEnabledError`, `TenantNotFoundError`

## Key Management Operations

### StorePreKeys

Persists pre-key material (one-time pre-keys, signed pre-key, identity key)
for a device. Clients upload pre-key bundles so that other clients can
initiate X3DH key agreement.

**Parameters:**
- `accountUUID` -- The account that owns the keys.
- `deviceID` -- The specific device.
- `bundle` -- A `keys.SetKeysRequest` containing the pre-key material.

**Errors:** `SignalNotEnabledError`, `TenantNotFoundError`

### GetPreKeys

Retrieves the pre-key bundle for a device. Used by other clients to establish
a Signal session via X3DH key agreement.

**Returns:** `*keys.PreKeyResponseItem` containing the signed pre-key,
one-time pre-key (if available), and identity key.

**Errors:** `SignalNotEnabledError`, `TenantNotFoundError`

## Tenant Management Operations

### ProvisionTenant

Initializes Signal for a tenant by persisting its `SignalTenantConfig` to
the KV store and caching it in memory. The tenant ID is passed explicitly
(not extracted from context).

**Errors:** `SignalNotEnabledError`, `InvalidConfigError`,
`TenantAlreadyProvisionedError`

### DeprovisionTenant

Removes the Signal configuration for a tenant. Does not delete tenant data
(accounts, messages, keys).

**Errors:** `InvalidConfigError`, `TenantNotFoundError`

### GetTenantConfig

Returns the `SignalTenantConfig` for a tenant from the in-memory cache.

**Errors:** `InvalidConfigError`, `TenantNotFoundError`

## Lifecycle

### Start

Loads persisted tenant configurations from the KV store into the in-memory
cache. If `SignalConfig.Enabled` is false, returns immediately without error.

### Stop

Gracefully shuts down the Signal service. Currently a no-op beyond logging.

## EventBridge

`EventBridge` publishes Signal events to QuicBaaS messaging channels. It is
a separate component from `SignalService`.

```go
type EventBridge struct { ... }

func NewEventBridge(ms messaging.Service, log *logger.Logger) *EventBridge
func (b *EventBridge) PublishMessageReceived(ctx context.Context, accountUUID string, envelope *messages.Envelope) error
func (b *EventBridge) PublishAccountEvent(ctx context.Context, eventType string, accountUUID string) error
```

### PublishMessageReceived

Publishes to channel `signal:messages:{accountUUID}` when a message is
delivered.

### PublishAccountEvent

Publishes to channel `signal:accounts:{eventType}`. Event types: `registered`,
`deleted`, `updated`.

## Constructor

```go
func NewSignalService(
    kvClient qrdbtransport.KVClient,
    log *logger.Logger,
    cfg *SignalConfig,
) SignalService
```

Creates a new `SignalService`. The service is not active until `Start` is
called.
