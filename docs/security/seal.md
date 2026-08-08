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

`pkg/seal/BarrierSealManager` is a thin SDK adapter that implements `interfaces.SealManager` by delegating operations to `transport.BarrierClient` from the go-qrdb SDK. It:

- Delegates seal lifecycle operations (init, unseal, seal, rekey) to the SDK client for non-EE paths
- Does NOT import `go-qrdb/pkg/seal` directly
- Does NOT perform share accumulation — that is handled by go-qrdb's `ExtendedBarrier`
- Does NOT expose root keys through the transport layer (`GetRootKey` was removed for security)
- Generates root tokens during initialization via the SDK
- Tracks version numbers across rekey operations

**Enable-encryption (EE) is embedded-node-driven:** The service layer (`pkg/service/barrier_service.go`) dispatches enable-encryption operations to the embedded qrdb node (`s.qrdbNode` wired as an `EncryptionMigrator`), not to `transport.BarrierClient`. This ensures EE has direct access to cluster state, DEK provisioning, and fail-safe coordination without round-tripping through a network transport.

All barrier logic (Shamir reconstruction, quorum tracking, encryption) lives in go-qrdb. QuicBaaS only adapts the SDK transport interface to its internal `SealManager` contract.

## Enable-Encryption (Barrier Migration)

QuicBaaS surfaces go-qrdb's barrier enable-encryption capability across all transports
(REST, gRPC, QUIC, MCP, Unix, embedded). Enable-encryption allows operators to transition
a system or tenant from cleartext (`none` barrier) to encrypted (`passphrase`, `shamir`,
TPM2, PKCS#11, or cloud KMS).

### System vs. Tenant Enable-Encryption

| Route | Scope | Permission | Use case |
|-------|-------|-----------|----------|
| `POST /api/v1/barrier/enable-encryption` | System barrier | `seal:enable_encryption` | Encrypt the shared cluster barrier (all tenants) |
| `POST /api/v1/tenants/{tenant_id}/barrier/enable-encryption` | Per-tenant barrier | `tenant_barrier:enable_encryption` | Encrypt a specific tenant's isolation barrier |

### Request fields (both routes)

- `strategy` — `passphrase`, `shamir`, `tpm2`, `pkcs11`, `awskms`, `gcpkms`, `azurekv`, `vault` (required)
- `mechanism` — Provider-specific mechanism (e.g., `transit` for Vault) (optional, provider-dependent)
- `availability` — Provider-specific availability mode (e.g., `aws-kms` for AWS) (optional, provider-dependent)
- `passphrase` — Wrapping passphrase for `passphrase`/`shamir` strategies (required for those; rejected for `none`)
- `shamir_threshold` — M in M-of-N Shamir (required for `shamir`; ignored otherwise)
- `shamir_total` — N in M-of-N Shamir (required for `shamir`; ignored otherwise)

### Implementation

Enable-encryption is driven by the embedded qrdb node (`s.qrdbNode` wired as `EncryptionMigrator`).
The embedded node provides direct access to:

- Cluster consensus and Raft coordination
- Data Encryption Key (DEK) provisioning and envelope encryption
- Per-shard migration orchestration (IsolationRaft for per-tenant barriers)
- Idempotent checkpoint-and-recovery semantics for crash resilience

### Error Handling

Entitlement violations and migration errors are mapped to HTTP status codes
(see [Barrier Architecture: Entitlement-Denied Error Mapping](../architecture/barrier.md#entitlement-denied-error-mapping)):

- **403 Forbidden** — Barrier is shared or entitlements deny the requested mode
- **409 Conflict** — Migration already in progress or barrier not in `none` state
- **400 Bad Request** — Invalid strategy, mechanism, or Shamir parameters
- **501 Not Implemented** — Storage barrier provider unavailable

## Overview

When QuicBaaS starts, it's in a sealed state where encrypted data is inaccessible. The master key must be reconstructed from multiple key shares before the database can operate.

## Sealed-Window Boot

A seal-enabled `quicbaasd` with no auto-unseal material (no injected
passphrase, no hardware/cloud auto-unseal strategy) boots into the
**sealed window** instead of exiting: all five transports (REST, gRPC,
Unix, QUIC, MCP) come up serving ONLY the canonical sealed-window
surface — barrier status/initialize/unseal, the Shamir/recovery unseal
flow, the init ceremony, and health. Everything else is rejected
(`503 {"error":"barrier sealed","code":"SEALED"}` on REST,
`Unavailable` on gRPC/QUIC, a sealed JSON-RPC error on MCP) until an
operator drives the barrier unsealed through any transport, at which
point the daemon completes the full boot automatically.

The allowlist mirrors go-qrdb's canonical sealed-window verb table
verbatim and is derived ONCE for all transports in
`pkg/api/sealedwindow` (single table → REST paths, gRPC methods,
QUIC opcodes + qrdb-quic bridge methods, MCP tool names), so the
transports cannot drift. Deliberate exclusions match qrdb: `seal`,
`enable-encryption`, share management, custodian enroll/revoke, the
per-tenant barrier surface, and credential rotation are never
reachable while sealed.

The gRPC allowlist is **service-qualified**. `GRPCAllowlist()` yields
the bare method tails that diff mechanically against qrdb's table, but
the list the interceptors and the authentication bypass actually gate on
is `GRPCFullMethodAllowlist()` — fully-qualified `/{service}/{method}`
paths matched against `FullMethod` in full. quicbaasd co-registers many
services and several declare a same-named method, so tail matching would
admit them: `/quicbaas.tenant.v1.TenantBarrier/Status` (a deliberate
exclusion) and the data-plane `Health` RPCs on the orchestrator, Ollama
and vector services all collide by tail. The full-method derivation
composes qrdb's own `qrdb.v1.QRDBService` list verbatim with the
quicbaas mirror verbs qualified onto the services that really declare
them (`quicbaas.barrier.v1.Barrier`, `quicbaas.ceremony.v1.Ceremony`,
and `quicbaas.v1.QuicBaaS/Health`).

The same probe-driven gates stay installed after boot: a post-boot
`barrier seal` immediately re-narrows every transport to the sealed
surface without a restart. While sealed there is no authorization gate
(no OIDC provider exists yet) — the allowlist is the control, and the
allowlisted handlers enforce their own credentials (officer cert +
nonce checks), exactly as on a qrdb node. Post-unseal, authorization
gate construction is fail-closed: under `security.require_authz` a
transport whose platform-token validator cannot be built aborts daemon
startup with a typed error rather than serving ungated.

Embedded (library) servers cannot serve a sealed window — a sealed
boot is refused with a typed `EmbeddedSealedBootError`; supply
`server.WithSealPassphrase` or an auto-unseal strategy instead.

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

### Seal strategy (`seal.strategy`)

`seal.strategy` selects how the barrier's **root key** is protected at rest. It is
distinct from the [`crypto:` block](../configuration/crypto.md), which selects
where each subsystem's keys are *stored* — the seal protects the root key, the
`crypto:` block stores the working keys.

| `seal.strategy` | Meaning |
|-----------------|---------|
| `passphrase` (default) | Software strategy: root key sealed under a KDF-derived passphrase (Argon2id, PBKDF2 under FIPS). |
| `none` | No seal (plaintext root key on disk). Ephemeral/test only. |
| `shamir` | Shamir M-of-N split of the root key. |
| `tpm2` | Root key sealed under the TPM SRK hierarchy (provisions at boot when named explicitly). |
| `pkcs11` / `awskms` / `gcpkms` / `azurekv` / `vault` | HSM- / cloud-KMS-backed seal. |
| `auto` | **Sentinel**: hardware-prefer. Uses a TPM2 seal **only** when a provisioned, seal-capable TPM is reachable; otherwise falls back to the software `passphrase` strategy. Never provisions, never stalls boot. |

```yaml
seal:
  strategy: auto   # passphrase | none | shamir | tpm2 | pkcs11 | awskms | gcpkms | azurekv | vault | auto
```

The `auto` decision is made by the same `BackendResolver` /
`buildHardwareSealStrategies` hardware probe that backs the `crypto:` block
(`pkg/server/seal_autodetect.go`): the TPM is opened **exactly once**, non-
provisioning and time-bounded (3s); a fresh, inaccessible, or non-seal-capable
TPM falls back to software cleanly. The empty string and `passphrase` are returned
unchanged — only the explicit `auto` sentinel triggers detection, so the default
posture and `make test` never touch hardware. Operators who want boot-time TPM
provisioning must name `tpm2` explicitly.

> **Envelope boundary.** Even when the root key is hardware-sealed, the data-plane
> barrier's per-epoch bulk AEAD stays **software AES-256-GCM** for throughput
> (the `crypto.subsystems.barrier_aead` resolver returns nil — qrdb's software
> default — for both `software` and `tpm2`). The hardware boundary is the
> **root-key seal**, not the per-block crypto. See [Barrier](../architecture/barrier.md).

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
