# Barrier — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/barrier/`.

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

`pkg/seal/BarrierSealManager` is a thin SDK adapter that implements `interfaces.BarrierManager` by delegating operations to `transport.BarrierClient` from the go-qrdb SDK. It:

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
| `POST /v1/barrier/enable-encryption` (go-qrdb wire) | Body `tenant_id`; empty = system | Per selected scope: `seal:enable_encryption`, or `tenant_barrier:enable_encryption` against that tenant | What go-qrdb's SDK, the CLI verb and the TS SDK's `barrier.enableEncryption()` call |

The wire mount is registered identity-gated because its scope is in its body.
`HandleBarrierWireEnableEncryption` decides the scope itself, binding the target
tenant explicitly (the system tenant when `tenant_id` is empty). The decision is
made by the embedded qrdb node's own composed RBAC and ABAC authorizer. The
daemon records it at node construction through the SDK's
`WithAuthorizerWrapper` option and returns it unchanged. It is the same
authorizer that renders every registration-site permission. It therefore
honours both store-resolved role grants and a platform app token's
`scp`-derived `ScopePermissions`. A tenant principal reaches only its own
tenant's barrier, never the system barrier, even when it holds
`seal:enable_encryption`. With no authorizer wired, or before the qrdb node is
up, the route answers `503`.

### Request fields (both routes)

These routes answer `204`, and take their scope from the path: the system
route rejects a body `tenant_id` with `400`. The go-qrdb wire mount
`POST /v1/barrier/enable-encryption` — the one go-qrdb's SDK and the
`quicbaas barrier enable-encryption` verb use — takes the same fields with the
target named `strategy`, plus a body `tenant_id` that selects the tenant
barrier (empty = system), and answers `200 {status, scope}` (go-quicbaas#990).

- `target_strategy` — `passphrase`, `shamir`, `tpm2`, `pkcs11`, `awskms`, `gcpkms`, `azurekv`, `vault` (required)
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
(see [Barrier Architecture: Entitlement-Denied Error Mapping](architecture.md#entitlement-denied-error-mapping)):

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

The sealed-phase network listeners (REST, gRPC TCP, QUIC, MCP) bind a
dedicated host — `server.sealed_bind_host`
(`QUICBAAS_SERVER_SEALED_BIND_HOST`), default `127.0.0.1` — never
`server.host` or the per-protocol `bind_address` overrides, so a
deployment that binds the full API on `0.0.0.0` does not expose the
pre-unseal surface network-wide. Deployments whose Shamir officers
drive the ceremony/unseal flow remotely opt in by setting the sealed
bind host to a routable address (for example `0.0.0.0`). The
post-unseal full-boot listeners are unaffected and keep resolving
`server.host` / `bind_address` as before.

The allowlist mirrors go-qrdb's canonical sealed-window verb table
verbatim and is derived ONCE for all transports in
`pkg/api/sealedwindow` (single table → REST paths, gRPC methods,
QUIC opcodes + qrdb-quic bridge methods, MCP tool names), so the
transports cannot drift. Deliberate exclusions match qrdb: `seal`,
`enable-encryption`, share management, custodian enroll/revoke, the
per-tenant barrier surface, and credential rotation are never
reachable while sealed. Since go-qrdb v0.25 neither are rekey, the
recovery-key generate / delete / status verbs or the barrier officer
list: they are not justified as anonymous. Use `barrier officer
claim-status` during first boot and `barrier officer list` once the node
is unsealed. Officer and recovery-officer registration are
method-qualified REST entries (`POST /v1/barrier/officers`), matched with
go-qrdb's `sealedwindow.RESTEntryMatches`, so a `GET` of the same path is
not admitted.

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

### Strict ceremony model: two narrowing axes

quicbaas deliberately diverges from qrdb's permissive "Vault model" for the
ceremony surface. `pkg/api/sealedwindow` narrows it on two independent axes,
both properties of the single canonical table, so every transport inherits
them from one derivation:

1. **`StrictCeremonyExclusions`** removes the ceremony verbs that MINT or
   DISTRIBUTE trust — `ceremony-initialize`, `ceremony-sign-csr` and
   `ceremony-enroll-external-officer` — from every transport's sealed window
   entirely. They are neither served while sealed nor authentication-exempt;
   they run in-process during `quicbaasd init`, before any listener exists.
   The exclusion is applied to qrdb's own `qrdb.v1.QRDBService` allowlist too,
   so the verbs cannot be reached on the co-registered qrdb service either.

2. **Exemption scope.** The remaining self-authorizing ceremony routes are
   still SERVED while sealed, but their AUTHENTICATION EXEMPTION is scoped:

   - `ScopeAlways` — barrier status/initialize/unseal, the Shamir and recovery
     unseal flow, root-token, officer claim-status and share claims, the
     ceremony officer list (quicbaas's own), submit-unseal-share and health. Exempt in EVERY ceremony state. A daemon restarted after a
     completed ceremony is operational AND sealed, and these verbs are the
     only way back to a usable data plane, so they must never depend on
     ceremony state.
   - `ScopePreOperational` — the ceremony claim handshakes
     (`claim-cert/begin|complete`, `officer-share/begin|complete`,
     `claim-share/begin|complete`) and the read-only `ceremony/status` probe.
     These authenticate the caller against material submitted at enrollment (a
     nonce signed by the key in the officer's CSR, or the mTLS thumbprint
     recorded for that officer) rather than against an account that does not
     exist yet — which is why they may skip ACCOUNT authentication at all, but
     only while the ceremony they belong to is unfinished. Once the deployment
     is operational the exemption expires and the routes require a token like
     any other; they remain on the deny-gate allowlist, so the response is
     `401`/`Unauthenticated` rather than the sealed envelope.

The ceremony state is supplied by `Server.OperationalProbe()`, installed on
every transport gate next to the sealed probe. It **fails closed**: an absent
probe, a status read that errors, and an unrecognized state all report
OPERATIONAL, so an unavailable probe narrows the unauthenticated surface to
`ScopeAlways` and can never widen it.

The probe combines two signals, because neither is sufficient:

- the live in-process ceremony state — `enrolling` means Initialize ran in
  THIS process, so the officer share enclaves that make the claim handshakes
  meaningful are in memory and the ceremony is genuinely unfinished;
- the barrier's persisted initialization timestamp — qrdb's ceremony state is
  in-memory and resets to `awaiting_init` on every process start, so on its
  own it would re-open the pre-operational window on EVERY restart of an
  already-initialized daemon. `awaiting_init` therefore counts as
  pre-operational only while the barrier has never been initialized (a genuine
  first boot, with no trust root to steal).

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
quicbaas barrier status
quicbaas barrier status --output json
```

### Initialize

```bash
# Initialize with 5 shares, threshold of 3
quicbaas barrier init-shamir --total 5 --threshold 3

# With recovery keys (generated separately after init)
quicbaas barrier recovery generate --total 3 --threshold 2
```

Output stores key shares and root token securely.

### Unseal

```bash
# Shamir: one --share per key holder, or --shares-file
quicbaas barrier unseal-shares --share "base64:dGVzdC1zaGFyZS0x"

# Passphrase barrier
quicbaas barrier unseal

# Check progress
quicbaas barrier status
```

### Seal

```bash
quicbaas barrier seal
```

### Rekey

```bash
# Generate new shares with same master key
quicbaas barrier rekey --total 7 --threshold 4
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
distinct from the [`crypto:` block](../composition/engineering.md), which selects
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
> **root-key seal**, not the per-block crypto. See [Barrier](architecture.md).

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
| `awskms` | Floci KMS | `TestAWSKMSSealRoundTrip` | `make integration-test-seal` |
| `vault` | Vault dev (Transit) | `TestVaultSealRoundTrip` | `make integration-test-seal` |
| `tpm2` | swtpm (TCP) | `TestTPM2SealRoundTrip` | `make integration-test-seal-hardware` |
| `pkcs11` | SoftHSM2 | `TestPKCS11SealRoundTrip` | `make integration-test-seal-hardware` |

The software (`passphrase`), `shamir`, and `none` strategies are covered by the
CLI barrier suite. Per-tenant barriers may select any of these strategies via
`TenantBarrierInitWithStrategy` (see multi-tenancy.md).

## See Also

- Cluster DEK - Cluster encryption key management
- KMS - Key management
- Security Overview


---

## Migration: `seal` → `barrier`

QuicBaaS, go-qrdb, and go-xkms have unified on the `barrier` vocabulary
for the encryption-at-rest subsystem. The legacy `seal` CLI verbs are
removed and have no aliases; CI's `make verify-naming` watches for new
violations.

This page is the operator-facing migration table.

### Operator paths

| Old | New | Status |
|-----|-----|--------|
| `quicbaas seal status` | `quicbaas barrier status` | removed |
| `quicbaas seal unseal --share <S>` | `quicbaas barrier unseal-shares --share <S>` | removed |
| `quicbaas seal seal` | `quicbaas barrier seal` | removed |
| `quicbaas seal init-shamir --threshold N --total M` | `quicbaas barrier init-shamir --threshold N --total M` | removed |
| `POST /api/v1/seal/status` | `GET /api/v1/barrier/status` | legacy retained, returns `Deprecation:` and `Sunset:` headers |
| `POST /api/v1/seal/unseal` | `POST /api/v1/barrier/unseal` | legacy retained |
| `POST /api/v1/seal/init` | `POST /api/v1/barrier/initialize` | legacy retained |
| `POST /api/v1/seal/rekey` | `POST /api/v1/barrier/rekey` | legacy retained |

The alias surface is exhaustively listed in
`scripts/verifynaming`. Any new code path that introduces a
`Seal*` identifier outside that list fails CI.

That script carries a second, unrelated rule: the retired product-area word
must appear in no tracked file — see
[../sdlc/architecture.md](../sdlc/architecture.md#naming). Both rules run under
`make lint`.

### SDK migration

Go SDK:

```go
// Old
sealClient := sdk.Seal()
status, _ := sealClient.Status(ctx)

// New
barrierClient := sdk.Barrier()
status, _ := barrierClient.Status(ctx)
```

TypeScript SDK:

```ts
// Old (still works for one release with console.warn)
import { SealClient } from '@quicbaas/sdk';
const seal: SealClient = client.seal;

// New
import { BarrierClient } from '@quicbaas/sdk';
const barrier: BarrierClient = client.barrier;
```

### Daemon configuration

```yaml
# Old
seal:
  enabled: true
  strategy: shamir
  shamir_threshold: 2
  shamir_total: 3

# New (same shape; rename the top-level key)
barrier:
  enabled: true
  strategy: shamir
  shamir:
    threshold: 2
    total_shares: 3
```

The `seal:` YAML key is still parsed for one release and forwarded to
the barrier subsystem unchanged. A deprecation log line is emitted at
startup when `seal:` is used.

### Data migration

There is **no in-place upgrade** from a pre-encfs data directory. The
encrypting `vfs.FS` (Pebble layer) introduced in this release writes
incompatible on-disk format. Operators must:

1. Snapshot or export data from the old daemon.
2. Stop the old daemon.
3. Move the old data directory aside (`mv data data.legacy`).
4. Start the new daemon against an empty data dir.
5. Run `quicbaas barrier init-shamir --threshold 2 --total 3` (or the
   equivalent for software/hardware strategies).
6. Run `quicbaas barrier unseal-shares --shares-file shares.txt`.
7. Re-import data from the snapshot.

The format-version sentinel
(`<data-dir>/seal/__sys/barrier_format_version`) is what makes the
new daemon refuse to start on a legacy directory. Operators see:

```
ERROR: data dir was created with a pre-barrier-refactor version;
in-place upgrade is not supported
```

This is intentional — falling through to "best effort" decryption of a
plaintext data dir would silently mask a corrupt-data scenario.

### Sunset timeline

The deprecation alias surface (legacy `/api/v1/seal/*` paths,
`SealClient` interfaces, `quicbaas seal …` CLI verbs, `seal:` YAML key)
is retained for one release. The current target sunset date is on the
`Sunset:` response header of the deprecated REST paths
(`Sunset: Wed, 01 Jul 2026 00:00:00 GMT`).
