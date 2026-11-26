# KMS — Engineering

> **Status: NORMATIVE** · Last verified: 2026-10-15
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/kms/`.

Backends, their configuration, key identifiers, AEAD safety limits and
algorithms are go-xkms's and are documented there:
[Key Management — Engineering](../../../go-xkms/docs/key-management/engineering.md),
[Crypto Backends — Engineering](../../../go-xkms/docs/crypto-backends/engineering.md),
[Backends](../../../go-xkms/docs/backends/README.md),
[AEAD bytes tracking](../../../go-xkms/docs/configuration/aead-bytes-tracking.md),
[xkmsd configuration](../../../go-xkms/docs/xkmsd-configuration/user.md).
This page records only the QuicBaaS configuration keys and how the primary
backend is chosen.

> **Being removed.** The `kms:` block configures QuicBaaS's own key store and
> backend router (`pkg/kms`), a parallel implementation removed on the owner's
> word ([SPEC](../SPEC.md) row `kms`). Its multi-backend routing, failover,
> health checks and key cache go with it; they are not documented here.

## Configuration keys (`kms:`)

From `KMSConfig` (`pkg/config/kms.go`):

| Key | Meaning |
|---|---|
| `enabled` | Start the KMS service |
| `backend` | Default backend for new keys (see the precedence below) |
| `enabled_backends` | Every backend the service may register |
| `defaults.algorithm`, `defaults.rotation_period` | Defaults for new keys |
| `safety.usage_warning`, `safety.auto_rotate`, `safety.check_interval` | AEAD usage thresholds (percent) and scan interval |
| `cache.enabled`, `cache.ttl`, `cache.max_keys` | In-memory key cache |
| `raft.*` | Replication settings for the key store |
| `software`, `tpm2`, `pkcs11`, `aws_kms`, `azure_kv`, `gcp_kms` | Per-backend settings; each backend's fields are go-xkms's ([Backends](../../../go-xkms/docs/backends/README.md)) |

The DEK keys are in [`../data-encryption-keys/engineering.md`](../data-encryption-keys/engineering.md).

## Backend selection (the `crypto:` block)

The **primary KMS crypto backend** — the go-xkms backend that holds the KMS key
universe and is registered as the go-xkms default — is selected by the
process-wide [`crypto:` block](../composition/engineering.md#crypto-backend-selection-crypto), resolved through the
central `BackendResolver` (`pkg/server/backend_resolver.go`). `startKMS`
(`pkg/server/lifecycle.go`) builds the KMS backend spec with this precedence
(highest first):

1. `crypto.subsystems.kms.backend` — per-subsystem override
2. `crypto.backend` — process-wide crypto default (applied when the KMS spec is unset/`auto`)
3. `kms.backend` — the legacy singular default-backend field, honored only when **no** `crypto.*` backend is configured at all
4. `auto` — hardware-prefer (TPM2 if present, else software)

An unset chain leaves the spec at `auto`, which resolves to software on a host
with no TPM. A configured-but-not-compiled backend (e.g. `awskms` without
`-tags awskms`) is a **typed `BackendNotCompiledError` that fails daemon
start** — it is never silently downgraded to software. Build tags are in
[architecture.md](architecture.md#build-tags).

---

## Key Exportability

go-xkms decides whether a key can be exported. It records the `Exportable`
attribute in its own key record when the key is created, and refuses export of
a key recorded as not exportable with `ErrBackendKeyNotExportable`. The
`Exportable` flag in the quicbaas key metadata only mirrors that record.

Every path that creates a key therefore passes the flag to go-xkms:

| Operation | Source of the flag | Path to go-xkms |
|---|---|---|
| `GenerateKey` | `GenerateKeyRequest.Exportable` | copied into the `KeyAttributes` passed to `KMSManager.GenerateKey` |
| `ImportKey` | `ImportKeyRequest.KeyAttributes.Exportable` | `KMSManager.ImportKey(..., exportable)` → backend option `exportable` → `XKMSBackend.ImportKey` sets `KeyAttributes.Exportable` before the go-xkms import |
| `UnwrapKey` | `UnwrapKeyRequest.KeyAttributes.Exportable` (absent means not exportable) | the same import path as `ImportKey` |

The REST, gRPC and MCP import handlers that call `KMSManager.ImportKey`
directly pass the request's flag the same way. Imported and unwrapped
symmetric keys are stored as `KeyTypeSecret`, the type generated symmetric
keys use, so they encrypt and decrypt like generated keys.

---

## Crypto Backend Selection — BackendResolver

Every backend is a go-xkms backend and behaves as go-xkms documents it (for
example [TPM 2.0](../../../go-xkms/docs/tpm2/api.md)). Operators choose backends
in the `crypto:` block, documented in
[composition](../composition/engineering.md#crypto-backend-selection-crypto)
and defined in `pkg/config/crypto.go`. This section covers only the go-quicbaas
resolver that maps a `config.BackendSpec` onto a go-xkms backend.

### Surface (`pkg/server/backend_resolver.go`)

| Symbol | Contract |
|---|---|
| `NewBackendResolver(cfg)` | probes hardware once (`DetectHardware`) and keeps the result |
| `Probe()` | the `HardwareProbe`: `TPM2Present`, `TPM2Device`, `PKCS11Present`, `PKCS11Library` |
| `Resolve(spec)` | `""` or `auto` becomes `tpm2` if a TPM was found, else `software`; PKCS#11 and cloud backends are never auto-selected. Returns `*UnknownBackendError` for an invalid type and `*BackendNotCompiledError` (naming the build tag) when no factory is compiled in |
| `ServiceBackend(spec)` | `ServiceBackendWithStore` over a fresh in-memory key store |
| `ServiceBackendWithStore(spec, store)` | resolves, then calls the registered factory; a nil store means in-memory |
| `StoreType(spec)` | `software`, `tpm2` or `pkcs11` store type; any other backend is `*StoreTypeUnsupportedError` |
| `AEADProvider(spec)` | `nil, nil` for `software` and `tpm2` (go-qrdb's software AEAD); any other backend is `*AEADUnsupportedError` |

A factory failure is returned as `*BackendConstructError`, which unwraps to the
go-xkms error.

**Hardware probe.** TPM2 is present if go-xkms can open `seal.tpm2.device`
(default `/dev/tpmrm0`), including a TPM that is not yet provisioned. PKCS#11 is
present if `seal.pkcs11.library`, or else `kms.pkcs11.library`, names a file
that exists.

**Build tags.** `software`, `tpm2` and `quantum` are always compiled in.
`pkcs11`, `awskms`, `gcpkms`, `azurekv` and `vault` each register their factory
from `backend_resolver_<tag>.go` only when built with the tag of the same name.

### Callers

| Subsystem | Code | Spec |
|---|---|---|
| KMS | `startKMS` → `buildKMSServiceBackendFn` → `ServiceBackendWithStore` (`lifecycle.go`) | see [Backend selection](#backend-selection-the-crypto-block) |
| CA signing key | `resolveCASigningSpec`, `caBackendForStore` → `ServiceBackend`, `resolveCAStoreType` → `StoreType` (`lifecycle.go`) | `crypto.subsystems.ca`, else `ca.keystore_type`; the TLS server key stays software |
| Raft cluster TLS on hardware | `provisionRaftClusterTLSHardware` → `ServiceBackend` (`qrdb_node_cluster_hw.go`) | the enrolment's key backend |

Outside the resolver:

- **PIN backend:** `resolvePINBackendType` and `buildPINBackend`
  (`pin_options.go`) read `crypto.subsystems.pin`, else `crypto.backend`, and
  hand the choice to go-xkms. Here `auto` means `software`, not hardware-prefer.
  `tpm2` without an active TPM2 seal strategy is `*PINRequiresTPMSealError`.
- **Seal strategies:** `buildHardwareSealStrategies` (`seal_factories.go`)
  constructs go-xkms backends directly. `resolveEffectiveSealStrategy`
  (`seal_autodetect.go`) maps an empty or `auto` `seal.strategy` to
  `passphrase`; it does not probe hardware.
- **Barriers:** the embedded go-qrdb node owns its barrier; the data-plane
  barrier (`buildDataPlaneBarrierService`) leaves `AEADProvider` unset, so both
  use go-qrdb's software AEAD under a seal-protected root key.

### Known gaps

- `crypto.subsystems.barrier_aead` is validated but never read, and
  `AEADProvider` has no production caller.
- Direct go-xkms constructions bypass the resolver:
  `NewSoftwareBackend` in `lifecycle.go` and `pkg/crypto/memory_manager.go`,
  and every `seal_factory*.go` / `seal_factories.go` backend.
