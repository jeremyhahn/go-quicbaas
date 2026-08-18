# Data Encryption Keys — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design, component boundaries and data flow. Feature folder: `docs/data-encryption-keys/`.

Cluster-wide encryption key management for data at rest.

## Status

Production Ready - Full implementation with Raft-based multi-node DEK distribution (requires `raft` build tag).

## Architecture

DEK management has moved to `go-qrdb/pkg/dek/`. QuicBaaS accesses DEK operations exclusively through the SDK's `transport.DEKClient` interface, available via `client.DEK()`.

```
go-qrdb/pkg/dek/
  ├── ClusterDEKManager   ← cluster-wide DEK with envelope encryption
  └── TenantDEKManager    ← per-tenant DEK isolation

go-qrdb/pkg/seal/
  └── EncryptionBarrier   ← key wrapping interface (replaces BarrierEncryptor)

go-quicbaas (SDK consumer only)
  └── Uses transport.DEKClient from go-qrdb SDK
```

QuicBaaS no longer directly manages DEK lifecycle. All DEK creation, rotation, re-encryption, and tenant isolation are handled by go-qrdb. The quicbaas layer delegates through the SDK transport.

## Overview

The Cluster DEK provides:
- Cluster-wide encryption key management
- Key rotation with version tracking
- Background re-encryption after rotation
- Node synchronization capabilities

## Key Hierarchy

```
Master Key (per node)
    |-- From seal/unseal, in memory only
        |
        +-- seal.EncryptionBarrier wraps Cluster DEK
        |
        +-- Cluster DEK (cluster-wide, in go-qrdb/pkg/dek/)
                |-- Stored encrypted via EncryptionBarrier
                |-- Wraps tenant DEKs via stdlib crypto/aes + crypto/cipher
                    |
                    +-- Tenant DEKs (per tenant, in go-qrdb/pkg/dek/)
                            |-- Envelope encryption, no xkms dependency
```

## SDK Usage

```go
import (
    sdk "github.com/jeremyhahn/go-qrdb/sdk/go"
    "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

client, err := sdk.NewClient(myTransport)
// ... connect ...

dekClient := client.DEK()

// Initialize cluster DEK
err := dekClient.InitializeClusterDEK(ctx, &transport.InitClusterDEKRequest{
    Algorithm: "AES-256-GCM",
    KeyBytes:  32,
})

// Get cluster DEK status
status, err := dekClient.GetClusterDEKStatus(ctx)

// Rotate cluster DEK
err = dekClient.RotateClusterDEK(ctx, &transport.RotateClusterDEKRequest{
    Reason:         "scheduled rotation",
    StartReencrypt: true,
})

// Tenant DEK operations
tenantDEK, err := dekClient.GetOrCreateTenantDEK(ctx, "tenant-123")
err = dekClient.RotateTenantDEK(ctx, "tenant-123")
status, err := dekClient.GetTenantDEKStatus(ctx, "tenant-123")
err = dekClient.DeleteTenantDEK(ctx, "tenant-123")
```

## Lifecycle

```
Created -> Active -> Deprecated -> Archived
```

1. **Created**: Initial DEK generated
2. **Active**: Current DEK for encryption
3. **Deprecated**: Old version, valid for decryption only
4. **Archived**: No data encrypted with this version

## Configuration

```go
type ClusterDEKConfig struct {
    Algorithm string  // "AES-256-GCM", "ChaCha20-Poly1305"
    KeyBytes  int     // 32 for AES-256

    RotationPolicy struct {
        Enabled       bool
        IntervalHours int  // 720 = 30 days
        AutoReencrypt bool
    }

    Reencryption struct {
        BatchSize   int  // Keys per batch
        Concurrency int  // Parallel workers
        ThrottleMS  int  // Delay between batches
    }
}
```

## Security Considerations

1. **Key Isolation**: DEK wrapped by each node's master key via `seal.EncryptionBarrier`
2. **In-Memory Only**: Unwrapped DEK never persisted
3. **Version Tracking**: All versions tracked for audit
4. **Atomic Rotation**: Rotation is atomic and safe
5. **No xkms Dependency**: Tenant DEK wrapping uses stdlib `crypto/aes` + `crypto/cipher`
6. **Recovery Options**: Multiple recovery mechanisms available

## Multi-Node Features

When built with the `raft` build tag, the ClusterDEKManager provides full multi-node support:

### Raft Integration
- **Dedicated Raft Shard**: KMS/DEK operations run on the platform shard (`PlatformShardID = 1`)
- **Consensus-based Operations**: All DEK operations go through Raft
- **Cluster-wide Rotation**: Coordinated rotation across all nodes

### Node Synchronization
- **Last-sync Tracking**: Per-node synchronization state
- **Join/Leave Handling**: Automatic DEK distribution to new nodes
- **Re-encryption Progress**: Cluster-wide progress tracking

### Supported Operations (via Raft)
- `SetDEK` - Initial DEK setup with cluster distribution
- `RotateDEK` - Coordinated rotation with version tracking
- Background re-encryption with batch processing (configurable batch size and worker count)

### Single-Node Mode
Without the `raft` build tag, operates in single-node mode:
- DEK stored locally in wrapped form
- No distributed synchronization required

## See Also

- [go-qrdb DEK package](https://github.com/jeremyhahn/go-qrdb/tree/main/pkg/dek) - DEK manager implementations
- Per-Database Encryption - Per-tenant, per-database DEK management
- Seal/Unseal - Master key management
- KMS - Key management infrastructure
- Security Overview - Security architecture
