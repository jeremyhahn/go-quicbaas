# Signal Integration

## Overview

QuicBaaS integrates [go-signal](https://github.com/jeremyhahn/go-signal) as a
multi-tenant Signal messaging service. A single `SignalService` instance serves
all tenants, routing each request to the correct tenant namespace by extracting
the tenant ID from the request context via `iam.TenantFromContext`.

The integration lives in `pkg/signal/` and is conditionally compiled using
Go build tags. All Signal data is persisted through go-qrdb's KV client,
giving tenants the same Raft-replicated, strongly-consistent storage that
backs the rest of QuicBaaS.

## Build Tags

The Signal integration requires the `signal` build tag (IAM is always compiled):

```bash
go build -tags "signal" ./...
```

All files in `pkg/signal/` carry the `//go:build signal` directive. Without
this tag, the Signal package is excluded from the binary entirely.

## Quick Start

### 1. Enable Signal in configuration

```yaml
signal:
  enabled: true
```

### 2. Build with the signal tag

```bash
make build-signal
# or
go build -tags "signal" ./cmd/...
```

### 3. Start the service

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

### 4. Provision a tenant

Before a tenant can use Signal messaging, it must be provisioned:

```go
err := svc.ProvisionTenant(ctx, "tenant-abc", &signal.SignalTenantConfig{
    Enabled:          true,
    MaxAccounts:      10000,
    MaxDevices:       6,
    MessageRetention: "30d",
})
```

### 5. Register accounts and send messages

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

## Dependencies

| Dependency        | Purpose                                    |
|-------------------|--------------------------------------------|
| `go-signal`       | Signal protocol managers (accounts, keys, messages, profiles) |
| `go-qrdb`         | Distributed KV storage via `qrdbtransport.KVClient` |
| `go-quicbaas/iam` | Tenant ID extraction from request context  |

## Package Layout

```
pkg/signal/
    config.go        Global and per-tenant configuration
    errors.go        Typed error definitions
    event_bridge.go  Signal events to messaging channels
    kv_adapter.go    go-qrdb KVClient to go-signal store.KVClient adapter
    service.go       SignalService interface and implementation
```
