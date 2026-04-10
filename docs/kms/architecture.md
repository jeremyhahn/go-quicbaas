# KMS Architecture

## Overview

QuicBaaS KMS provides distributed key management through Raft consensus. All key operations are replicated across the cluster, ensuring consistency and high availability.

## Components

```
┌─────────────────────────────────────────────────────────────┐
│                     KMS Service                             │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │  REST API   │  │  gRPC API   │  │      MCP API        │  │
│  └──────┬──────┘  └──────┬──────┘  └──────────┬──────────┘  │
│         │                │                     │            │
│         └────────────────┼─────────────────────┘            │
│                          ▼                                  │
│  ┌──────────────────────────────────────────────────────┐   │
│  │               KMS State Machine                       │   │
│  │         (Raft Replicated Operations)                  │   │
│  └──────────────────────────┬───────────────────────────┘   │
│                             ▼                               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │               go-xkms Facade                      │   │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌─────────┐  │   │
│  │  │ Software │ │  TPM2    │ │ PKCS#11  │ │Cloud KMS│  │   │
│  │  └──────────┘ └──────────┘ └──────────┘ └─────────┘  │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

## Key Hierarchy

```
┌─────────────────────────────────────┐
│      Root Key (HSM/TPM/Cloud)       │
│   Never leaves hardware/cloud       │
└──────────────────┬──────────────────┘
                   │ seals
                   ▼
┌─────────────────────────────────────┐
│     Master Key (Per Backend)        │
│   Seals/unseals DEKs                │
└──────────────────┬──────────────────┘
                   │ seals
                   ▼
┌─────────────────────────────────────┐
│    Data Encryption Key (DEK)        │
│   Encrypts application data         │
│   Replicated via Raft               │
└──────────────────┬──────────────────┘
                   │ encrypts
                   ▼
┌─────────────────────────────────────┐
│         Application Data            │
│   Secrets, user data, etc.          │
└─────────────────────────────────────┘
```

## KMS State Machine

The KMS uses a dedicated Raft state machine for key replication.

### State Machine Operations

| Op Code | Operation | Description |
|---------|-----------|-------------|
| 0x01 | CreateKey | Create new key entry |
| 0x02 | DeleteKey | Remove key (soft delete) |
| 0x03 | RotateKey | Create new key version |
| 0x04 | SetDEK | Set cluster DEK |
| 0x05 | RotateDEK | Rotate cluster DEK |
| 0x06 | ExportKey | Export key material |
| 0x07 | ImportKey | Import key material |
| 0x08 | UpdateUsage | Update AEAD usage counters |

### Key Entry Structure

```go
type KMSKeyEntry struct {
    ID           string            // Unique key identifier
    Type         string            // "symmetric", "sealing"
    Algorithm    string            // "AES-256-GCM", etc.
    SealedData   []byte            // Key material (sealed)
    Version      uint64            // Key version
    UsageCount   uint64            // Operations performed
    BytesUsed    uint64            // Bytes encrypted
    CreatedAt    int64
    RotatedAt    int64
    Metadata     map[string]string
}
```

### Binary Command Format

```
CREATE:     [0x01][IDLen:2B][ID][Algorithm:1B][Metadata...]
DELETE:     [0x02][IDLen:2B][ID]
ROTATE:     [0x03][IDLen:2B][ID]
SET_DEK:    [0x04][SealedLen:4B][SealedData]
UPDATE:     [0x08][IDLen:2B][ID][UsageCount:8B][BytesUsed:8B]
```

## AEAD Safety Tracking

go-xkms tracks cryptographic limits per key:

| Algorithm | Nonce Limit | Bytes Limit |
|-----------|-------------|-------------|
| AES-128-GCM | 2^32 operations | 64 GB |
| AES-256-GCM | 2^32 operations | 64 GB |
| ChaCha20-Poly1305 | 2^64 operations | 256 GB |
| XChaCha20-Poly1305 | 2^96 operations | 256 GB |

### Safety Flow

```
1. Encrypt request arrives
2. Check usage counters in state machine
3. If approaching limits, trigger rotation
4. Perform encryption via go-xkms
5. Update usage counters (Raft replicated)
```

## Cluster DEK Management

### Initialization

```
1. Leader generates DEK via go-xkms
2. DEK sealed with backend-specific key
3. Sealed DEK replicated via Raft
4. Each node unseals locally for use
```

### Rotation

```
1. Leader generates new DEK
2. Old DEK marked for phase-out
3. New sealed DEK replicated
4. Background re-encryption of data
5. Old DEK deleted after migration
```

## Multi-Backend Routing

The KMS Manager supports intelligent routing to multiple backends through the `BackendRouter` component (`pkg/kms/router.go`).

### Routing Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                       KMS Manager                                │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                   BackendRouter                          │    │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐  │    │
│  │  │ Key ID      │  │ Algorithm   │  │ Health-Based    │  │    │
│  │  │ Prefix      │  │ Routing     │  │ Failover        │  │    │
│  │  │ Routing     │  │             │  │                 │  │    │
│  │  └──────┬──────┘  └──────┬──────┘  └────────┬────────┘  │    │
│  │         └────────────────┼──────────────────┘           │    │
│  └──────────────────────────┼──────────────────────────────┘    │
│                             ▼                                    │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                   BackendRegistry                         │   │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────────┐  │   │
│  │  │software │  │  hsm    │  │  aws    │  │   vault     │  │   │
│  │  │priority:1│  │priority:10│ │priority:5│  │ priority:5 │  │   │
│  │  └─────────┘  └─────────┘  └─────────┘  └─────────────┘  │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

### Routing Flow

The `BackendRouter.Route()` method selects backends in this priority order:

```
┌──────────────────────────────────────────────────────────────┐
│                    Route(keyID, algorithm)                    │
├──────────────────────────────────────────────────────────────┤
│  1. Parse Key ID                                              │
│     ├── Structured? (backend:keytype:algorithm:keyname)       │
│     │   └── YES → Try backend from key ID prefix              │
│     │             ├── Available? → RETURN backend             │
│     │             └── Not available → Continue to step 2      │
│     └── NO → Continue to step 2                               │
│                                                               │
│  2. Algorithm Routing                                         │
│     ├── Check AlgorithmRouting config map                     │
│     │   └── Match? → Try configured backend                   │
│     └── Query registry.GetByAlgorithm()                       │
│         └── Found? → RETURN backend                           │
│                                                               │
│  3. Default Backend                                           │
│     └── DefaultBackend configured?                            │
│         └── YES → Try default backend                         │
│             └── Available? → RETURN backend                   │
│                                                               │
│  4. Priority-Based Selection                                  │
│     └── registry.GetHealthy()                                 │
│         └── Returns highest priority healthy backend          │
│                                                               │
│  5. Fallback Default                                          │
│     └── Use fallback defaultBackend if set via option         │
│                                                               │
│  6. Error: No available backend                               │
└──────────────────────────────────────────────────────────────┘
```

### Structured Key ID Format

Keys can use a structured ID format for explicit backend routing:

```
backend:keytype:algorithm:keyname
```

The `ParseKeyID()` function parses this format into a `ParsedKeyID` struct:

| Field | Description | Example |
|-------|-------------|---------|
| Backend | Target backend name | `hsm`, `software`, `vault` |
| KeyType | Purpose of the key | `signing`, `encryption`, `master` |
| Algorithm | Cryptographic algorithm | `ecdsa-p256`, `aes-256-gcm` |
| KeyName | User-provided key name | `my-signing-key` |

Examples:
- `hsm:signing:ecdsa-p256:my-signing-key` - Routes to HSM backend
- `software:encryption:aes-256-gcm:data-key` - Routes to Software backend
- `vault:master:rsa-4096:root-key` - Routes to HashiCorp Vault backend
- `my-simple-key` - Unstructured, uses default routing

### Failover Behavior

The `RouteWithFailover()` method provides automatic failover:

```go
err := router.RouteWithFailover(ctx, keyID, algorithm, func(backend Backend) error {
    return backend.Encrypt(ctx, keyID, plaintext)
})
```

Failover behavior is controlled by `RoutingConfig`:

| Option | Default | Description |
|--------|---------|-------------|
| `FailoverEnabled` | `true` | Enable automatic failover on failure |
| `MaxFailoverAttempts` | `3` | Maximum backends to try before returning error |

Failover candidates are selected from:
1. All enabled backends with `Health.Status != Unhealthy`
2. Backends supporting the requested algorithm (if specified)
3. Sorted by priority (lower number = higher preference)
4. Default backend added as last resort if not already included

### Backend Registry

The `BackendRegistry` (`pkg/kms/registry.go`) manages backend lifecycle:

| Feature | Description |
|---------|-------------|
| Health Tracking | Automatic health checks with configurable intervals (default: 30s) |
| Priority Selection | Lower priority value = higher preference |
| Algorithm Support | Route by supported cryptographic algorithms |
| Health Status | `healthy`, `degraded`, `unhealthy`, `unknown` |
| Auto-Recovery | Backends transition through `degraded` state during recovery |

Registry health check configuration:

| Option | Default | Description |
|--------|---------|-------------|
| `HealthCheckInterval` | 30s | Time between health checks |
| `HealthCheckTimeout` | 5s | Timeout for each health check |
| `UnhealthyThreshold` | 3 | Consecutive failures before marking unhealthy |
| `RecoveryThreshold` | 2 | Consecutive successes before marking healthy |

### Supported Backend Types

Using go-xkms's `BackendType` constants:

| Type | Description |
|------|-------------|
| `BackendTypeSoftware` | In-memory software implementation |
| `BackendTypePKCS11` | Hardware Security Module via PKCS#11 |
| `BackendTypeSmartCardHSM` | SmartCard-HSM devices |
| `BackendTypeTPM2` | Trusted Platform Module 2.0 |
| `BackendTypeAWSKMS` | AWS Key Management Service |
| `BackendTypeGCPKMS` | Google Cloud KMS |
| `BackendTypeAzureKV` | Azure Key Vault |
| `BackendTypeVault` | HashiCorp Vault |
| `BackendTypeQuantum` | Post-quantum algorithms |
| `BackendTypeThreshold` | Threshold signatures |
| `BackendTypeSymmetric` | Symmetric key operations |

## Multi-Backend Support

Each node can use a different backend:

```
Node A (AWS)           Node B (GCP)           Node C (Azure)
┌──────────────┐      ┌──────────────┐      ┌──────────────┐
│ KMS State    │      │ KMS State    │      │ KMS State    │
│ Machine      │ ←──→ │ Machine      │ ←──→ │ Machine      │
└──────┬───────┘      └──────┬───────┘      └──────┬───────┘
       │                     │                     │
       ▼                     ▼                     ▼
┌──────────────┐      ┌──────────────┐      ┌──────────────┐
│   AWS KMS    │      │   GCP KMS    │      │  Azure KV    │
└──────────────┘      └──────────────┘      └──────────────┘
```

- Sealed data format is backend-agnostic
- Each node unseals using its local backend
- Provides multi-cloud redundancy

## Security Considerations

### Key Protection

- Keys never stored in plaintext on disk
- Sealed via go-xkms backend
- Hardware backends (TPM/HSM) bind to device

### Access Control

- RBAC policies for key operations
- Audit logging for all operations
- Principle of least privilege

### Recovery

- Quorum required for key operations
- Snapshot includes sealed key material
- Node recovery re-seals with local backend
