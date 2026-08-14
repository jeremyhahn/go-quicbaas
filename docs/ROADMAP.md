# QuicBaaS Architecture Roadmap

## Vision

One KMS implementation, one interface contract. Both xkmsd and quicbaasd use go-xkms `XKMSService` for all crypto business logic. Both expose `transport.Client` as the SDK interface. xkey works with either server without code changes.

## Architecture Overview

```
xkey / xkms clients                    QuicBaaS CLI / QuicBaaS clients
  |                                      |
  +-- xkms.Client (go-xkms SDK)         +-- quicbaas.Client (QuicBaaS SDK)
        |                                      |
        v                                      v
   +-----------------------------------------------------+
   |              quicbaasd server                        |
   |  +-----------------------------------------+        |
   |  | API layer (gRPC, REST, MCP, QUIC)       |        |
   |  | xkms-compatible endpoints + quicbaas    |        |
   |  +-------------------+---------------------+        |
   |                      |                               |
   |  +-------------------v---------------------+        |
   |  | Service layer (tenant context injection) |        |
   |  +-------------------+---------------------+        |
   |                      |                               |
   |  +-------------------v---------------------+        |
   |  | go-xkms XKMSService (THE implementation)|        |
   |  | + QuicBaaS KV, Vector, TimeSeries, etc. |        |
   |  +-------------------+---------------------+        |
   |                      |                               |
   |  +-------------------v---------------------+        |
   |  | Raft + PebbleDB (distributed storage)   |        |
   |  +-----------------------------------------+        |
   +-----------------------------------------------------+
```

## Key Design Decisions

1. **go-xkms XKMSService is THE business logic** with optional multi-tenancy.
2. **QuicBaaS's server exposes xkms-compatible wire-protocol endpoints.**
3. **xkey uses xkms.Client pointed at quicbaasd** -- no xkey changes needed.
4. **QuicBaaS keeps its own SDK** for QuicBaaS-specific features (KV, Vector, TimeSeries, Batch, etc.).
5. **No SDK merging** -- two SDKs, one server, compatible protocols.
6. **Tenant context injected at service layer** from auth context.
7. **Empty TenantID or the literal system tenant (`qrdbsdk.SystemTenantID`) = platform/single-tenant scope** (xkmsd, and quicbaasd control-plane callers); **any other TenantID = tenant-isolated mode** (quicbaasd). The empty string and `"system"` are the same scope per `qrdbsdk.IsSystemTenant` — see `docs/security/multi-tenancy.md`.

## SDK Sub-Interface Design

Both SDKs use a composed sub-interface pattern:

- `KeyService`, `CryptoService`, `CertService`, `SealService`, `BarrierService`
- `PIVService`, `FIDO2Service`, `CAService`, `PINService`, `UserService`
- `Client` = composition of all sub-interfaces (backward compatible)

**Benefits:** Testability (mock just one sub-interface), cleaner transport implementations, type safety.

## Multi-Tenancy Compliance Model

### Key Hierarchy

```
Barrier Master Key (go-xkms seal/unseal, 7+ strategies)
    |
    +-- System KEK (cluster-wide operations)
    |
    +-- Tenant KEK (per-tenant, wraps tenant's DEKs and key material)
    |     +-- Tenant DEK-1 (data encryption)
    |     +-- Tenant DEK-2 (data encryption)
    |     +-- Tenant Keys (signing, encryption, etc.)
    |
    +-- Tenant KEK (another tenant, completely isolated)
```

### Compliance Requirements

- **Per-tenant key isolation:** Required by FedRAMP, HIPAA, PCI DSS 4.0, SOC 2, FIPS 140-2/3, ISO 27001/27017, NIST SP 800-57.
- **Cryptographically enforced isolation:** Per-tenant KEKs wrapping per-tenant DEKs.
- **Physical separation not required:** Logical isolation with crypto enforcement is sufficient.
- **Per-tenant audit trail:** All key operations logged with tenant identity.

## Multi-Tenancy Status

Multi-tenancy has been addressed across Phases 1-2. The go-xkms SDK includes TenantID in `KeyAttributes`, storage paths, and transport types. The QuicBaaS service layer extracts tenant from auth context and applies tenant-prefixed storage paths with per-tenant KEK management.

## Phased Implementation

### Phase 0: Release Prep (0.1.0-alpha) -- COMPLETED

- Renamed `pkg/keychain` to `pkg/xkms`.
- Fixed formatting, CI, Makefile, VERSION.
- Tagged 0.1.0-alpha.

### Phase 1: go-xkms SDK Sub-Interface Refactor + Multi-Tenancy -- COMPLETED

- Split monolithic `transport.Client` (100+ methods) into composed sub-interfaces.
- Added TenantID to `KeyAttributes`, storage paths, transport types.
- Tenant enforcement in `XKMSService` (empty = single-tenant, backward compatible).

### Phase 2: QuicBaaS KMS Tenant Integration -- COMPLETED

- Tenant extraction from auth context in `pkg/service/kms.go`.
- Tenant-prefixed storage paths in `pkg/kms/storage_adapter.go`.
- Per-tenant KEK management in `pkg/security/tenant_dek_manager.go`.

### Phase 3: QuicBaaS SDK Sub-Interface Refactor -- COMPLETED

- Replaced `interface{}`-based generic transport with type-safe sub-interfaces.
- Defined typed sub-interfaces: `CoreDataService`, `ClusterManagementService`, `SecurityService`, `IdentityService`, `SearchService`, `StorageService`, `AsyncService`, `AIService`.
- Imported go-xkms sub-interfaces for KMS operations.

### Phase 4: Server-side xkms API Compatibility -- COMPLETED

- quicbaasd exposes same wire-protocol endpoints as xkmsd.
- REST, MCP, and gRPC handlers for all xkms operations including backends, keys, crypto, certs, and seal.
- All operations delegate to `XKMSService` with tenant injection.

### Phase 5: Service Expansion -- COMPLETED

- PIV, FIDO2, CA, PIN service interfaces with full handler coverage.
- REST handlers, MCP tools, and gRPC handlers for all expanded services.
- Barrier integration deferred to a separate effort (see What's Next).

### Phase 6: Documentation and Final Verification -- IN PROGRESS

- Documenting completed phases and updated architecture.
- Verifying all integration tests pass across protocols.
- Updating SDK examples and usage guides.

## What's Next

- **xkey integration testing:** Point `xkms.Client` at quicbaasd and validate end-to-end xkey workflows (SSH agent, CA, PIV, FIDO2) with zero code changes.
- **Barrier/seal consolidation:** Replace the native seal manager with go-xkms Barrier plus Raft coordination for a unified seal/unseal lifecycle.
- **Production hardening and performance optimization:** Load testing, latency profiling, connection pooling tuning, and operational runbook development.

## Critical Files

| File | Role |
|------|------|
| `pkg/xkms/xkms.go` | Integration layer: all crypto delegated to go-xkms |
| `pkg/kms/xkms_backend.go` | Adapts go-xkms backends to QuicBaaS's KMS Backend interface |
| `pkg/kms/storage_adapter.go` | KVStoreStorageAdapter: bridges QuicBaaS KVStore to go-xkms storage (tenant-prefixed) |
| `pkg/service/kms.go` | KMS service wrapper with tenant extraction from auth context |
| `pkg/security/tenant_dek_manager.go` | Per-tenant DEK and KEK management |
| `pkg/seal/manager.go` | Seal manager (to be refactored to go-xkms Barrier) |
