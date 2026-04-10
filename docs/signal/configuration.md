# Signal Configuration

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
