# Seal/Unseal

Shamir's Secret Sharing for protecting the master encryption key.

## Status

Complete - Full implementation with CLI commands, gRPC/REST APIs, and service layer.

## Architecture

The seal subsystem is split across four repositories with clean ownership boundaries:

```
go-quicraft (consensus layer)
  └── pkg/seal/EpochBarrier             ← AEAD, epoch rotation, HKDF DEKs
        │
        ▼
go-qrdb (database layer)
  │
  ├── pkg/seal/ExtendedBarrier          ← pluggable SealingStrategy pattern
  ├── pkg/seal/BarrierRegistry          ← system + per-tenant barriers
  ├── pkg/seal/TenantBarrier            ← per-tenant namespaced encryption
  ├── pkg/seal/TenantKeyManager         ← per-tenant DEK envelope encryption
  ├── pkg/seal/ShareExchangeCoordinator ← cluster auto-unseal coordination
  ├── pkg/dek/ClusterDEKManager         ← cluster-wide DEK with envelope encryption
  └── pkg/dek/TenantDEKManager          ← per-tenant DEK isolation
        │
        ▼
go-xkms (KMS layer)
  └── pkg/seal/TPM2Strategy             ← hardware-backed sealing
  └── pkg/seal/PKCS11Strategy           ← HSM-backed sealing
  └── pkg/seal/CloudKMSStrategy         ← AWS/GCP/Azure KMS sealing
        │
        ▼
go-quicbaas (application layer — SDK adapter only)
  └── pkg/seal/BarrierSealManager       ← thin adapter over transport.BarrierClient
```

### Key Hierarchy

```
Root Key (per node, sealed to strategy)
    ↓
    ├── Barrier (ExtendedBarrier) — in memory only when unsealed
    │    └── DEK (derived via HKDF per epoch)
    │         └── Transparent encrypt/decrypt for all storage ops
    │
    ├── Cluster DEK (go-qrdb/pkg/dek/ClusterDEKManager)
    │    └── Tenant DEKs (go-qrdb/pkg/dek/TenantDEKManager)
    │
    ├── Shamir Shares (split for manual/auto unseal)
    │    └── ShareExchangeCoordinator for cluster auto-unseal
    │
    └── Recovery Keys (optional, split root key via Shamir)
```

### BarrierSealManager

`pkg/seal/BarrierSealManager` is a thin SDK adapter that implements `interfaces.SealManager` by delegating all operations 1:1 to `transport.BarrierClient` from the go-qrdb SDK. It:

- Delegates every seal lifecycle operation (init, unseal, seal, rekey) to the SDK client
- Does NOT import `go-qrdb/pkg/seal` directly
- Does NOT perform share accumulation — that is handled by go-qrdb's `ExtendedBarrier`
- Does NOT expose root keys through the transport layer (`GetRootKey` was removed for security)
- Generates root tokens during initialization via the SDK
- Tracks version numbers across rekey operations

All barrier logic (Shamir reconstruction, quorum tracking, encryption) lives in go-qrdb. QuicBaaS only adapts the SDK transport interface to its internal `SealManager` contract.

## Overview

When QuicBaaS starts, it's in a sealed state where encrypted data is inaccessible. The master key must be reconstructed from multiple key shares before the database can operate.

## How It Works

```
Initialization:
Master Key → Shamir Split → 5 Key Shares
  Share 1: Operator 1
  Share 2: Operator 2
  Share 3: Operator 3
  Share 4: Backup 1
  Share 5: Backup 2

Unseal Process:
Share 1 + Share 3 + Share 5 → Shamir Combine → Master Key → Unsealed
```

## CLI Usage

### Check Status

```bash
# Check seal status
quicbaas seal status
quicbaas seal status --output json
```

### Initialize

```bash
# Initialize with 5 shares, threshold of 3
quicbaas seal init --shares 5 --threshold 3

# Custom configuration
quicbaas seal init --shares 7 --threshold 4

# With recovery keys (generated separately after init)
quicbaas seal init --shares 5 --threshold 3
quicbaas seal recovery generate --shares 3 --threshold 2
```

Output stores key shares and root token securely.

### Unseal

```bash
# Interactive (recommended)
quicbaas seal unseal

# Via flag (less secure)
quicbaas seal unseal --share "base64:dGVzdC1zaGFyZS0x"

# Check progress
quicbaas seal status
# Output: Progress: 1/3 shares provided
```

### Seal

```bash
# Seal the system
quicbaas seal seal
```

### Rekey

```bash
# Generate new shares with same master key
quicbaas seal rekey --shares 7 --threshold 4
```

## Configuration

```yaml
seal:
  enabled: true
  mode: manual  # manual or auto
  threshold: 3
  total_shares: 5

  # Recovery keys
  recovery:
    enabled: false
    shares: 3
    threshold: 2

  # Auto-unseal via cloud KMS or ShareExchangeCoordinator
  auto_unseal:
    enabled: false
    type: awskms  # awskms, gcpkms, azurekeyvault

    awskms:
      region: us-east-1
      kms_key_id: alias/quicbaas-unseal

    gcpkms:
      project: my-project
      location: global
      key_ring: quicbaas
      crypto_key: unseal-key

    azurekeyvault:
      vault_name: myvault
      key_name: quicbaas-unseal
```

## Security Best Practices

1. **Distribute Shares**: Store shares in separate secure locations
2. **Different Custodians**: Assign each share to different person
3. **Secure Transport**: Use encrypted channels for share transmission
4. **Never Store Together**: Never store threshold shares together
5. **Regular Rotation**: Rekey periodically to rotate shares
6. **Backup Shares**: Maintain secure backups
7. **Access Logging**: Monitor all seal/unseal operations
8. **Recovery Keys**: Consider recovery keys for disasters

## Disaster Recovery

### Lost Shares (Below Threshold)
- Use remaining shares to unseal
- Rekey to generate new shares
- Distribute new shares

### Lost Shares (At or Above Threshold)
- Use recovery keys if configured
- Restore from backup if available
- Re-initialize from scratch (last resort, data loss)

### Auto-Unseal Failure
- Fall back to recovery keys
- Check auto-unseal backend status
- Manual unseal as temporary measure

## Hardware / Cloud Seal Strategy Injection

QuicBaaS does not implement any seal crypto. Hardware/cloud sealing strategies
are owned by **go-xkms** and injected into the embedded go-qrdb engine through a
single seam: `XKMSCryptoOptions` (pkg/server/crypto_options.go) builds the
`StrategyRegistry`/`PINBackend`/`FactorRegistry` server options, and the node
builder applies the active strategy via `qrdbnode.WithSealStrategy(...)` inside
`buildNodeSealSubsystem`. The thin per-backend injectors live in
`pkg/server/seal_factory_*.go` (awskms, gcpkms, azurekv, vault, **pkcs11**,
**tpm2**) — each constructs the go-xkms backend from operator config and wraps it
with the canonical `xkmssdk.New<Backend>Strategy(...)`. No HSM/TPM crypto is
reimplemented here, and the go-xkms **xkey** sealed-blob services (a separate
desktop-agent domain) are never used by the barrier.

## Strategy Integration Test Coverage

All hardware/cloud seal strategies are exercised by real barrier
init→seal→unseal round-trips against emulators/HSM (no fakes, no skips):

| Strategy | Emulator/HSM | Test | Make target |
|---|---|---|---|
| `awskms` | LocalStack KMS | `TestAWSKMSSealRoundTrip` | `make integration-test-seal` |
| `vault` | Vault dev (Transit) | `TestVaultSealRoundTrip` | `make integration-test-seal` |
| `tpm2` | swtpm (TCP) | `TestTPM2SealRoundTrip` | `make integration-test-seal-hardware` |
| `pkcs11` | SoftHSM2 | `TestPKCS11SealRoundTrip` | `make integration-test-seal-hardware` |

The software (`passphrase`), `shamir`, and `none` strategies are covered by the
CLI barrier suite. Per-tenant barriers may select any of these strategies via
`TenantBarrierInitWithStrategy` (see [multi-tenancy.md](multi-tenancy.md)).

## See Also

- [Cluster DEK](cluster-dek.md) - Cluster encryption key management
- [KMS](kms.md) - Key management
- [Security Overview](README.md)
