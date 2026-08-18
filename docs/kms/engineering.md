# KMS — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/kms/`.

## Configuration File

```yaml
# quicbaas.yaml
kms:
  enabled: true

  # Backend selection - multiple backends can be enabled simultaneously
  # Each backend provides different capabilities (software, hardware, cloud)
  # The system will use the most appropriate backend for each operation
  backends:
    - software      # Always available, PKCS#8 key files
    - tpm2          # Hardware TPM 2.0 sealed keys
    - pkcs11        # HSM via PKCS#11
    - awskms        # AWS Key Management Service
    - azurekv       # Azure Key Vault
    - gcpkms        # GCP Cloud KMS

  # Master key defaults (asymmetric keys for signing/key-agreement)
  # Master keys protect DEKs and are used for signing operations
  defaults:
    algorithm: Ed25519    # Ed25519 | ECDSA-P256 | ECDSA-P384 | RSA-2048 | RSA-4096

  # DEK defaults (symmetric keys for bulk encryption)
  dek:
    algorithm: AES-256-GCM    # AES-256-GCM | AES-128-GCM | ChaCha20-Poly1305

  # AEAD safety configuration (server-level defaults for DEKs)
  # Reference: NIST SP 800-38D - Recommendation for Block Cipher Modes of Operation: GCM
  safety:
    # When true: apply these defaults to ALL keys/DEKs without explicit AEAD config
    # When false: keys/DEKs without explicit AEAD config have no limits
    enforce: true

    # Default limits per NIST SP 800-38D recommendations for AES-GCM
    max_operations: 4294967296    # 2^32 operations (NIST SP 800-38D Section 8.3)
    max_bytes: 375809638400       # 350 GB (NIST SP 800-38D - 2^39 bits = 64GB per key, ~350GB conservative)
    warning_percent: 80           # Warn at 80% of limit
    auto_rotate_percent: 95       # Auto-rotate at 95% of limit
```

## Backend selection (the `crypto:` block)

The **primary KMS crypto backend** — the go-xkms backend that holds the KMS key
universe and is registered as the go-xkms default — is selected by the
process-wide [`crypto:` block](../composition/engineering.md), resolved through the
central `BackendResolver` (`pkg/server/backend_resolver.go`). `startKMS`
(`pkg/server/lifecycle.go`) builds the KMS backend spec with this precedence
(highest first):

1. `crypto.subsystems.kms.backend` — per-subsystem override
2. `crypto.backend` — process-wide crypto default (applied when the KMS spec is unset/`auto`)
3. `kms.backend` — the legacy singular default-backend field, honored only when **no** `crypto.*` backend is configured at all
4. `auto` — hardware-prefer (TPM2 if present, else software)

An unset chain leaves the spec at `auto`, which resolves to software on a host
with no TPM — byte-for-byte the historical behavior. A configured-but-not-compiled
backend (e.g. `awskms` without `-tags awskms`) is a **typed `BackendNotCompiledError`
that fails daemon start** — it is never silently downgraded to software.

The `kms.backends` / `BackendRouter` configuration documented below is a
**separate, higher-level routing layer** (`kms.enabled_backends` + the registry)
that distributes operations across multiple registered backends by algorithm,
priority, and health. It sits on top of the primary backend chosen by the
`crypto:` block. For the full backend matrix, build tags, per-subsystem overrides
(CA / PIN / barrier AEAD), and the typed fail-closed errors, see
[Crypto Backend Selection](../composition/engineering.md).

## Multi-Backend Routing

When multiple backends are configured, the `BackendRouter` intelligently routes operations to the appropriate backend based on routing rules, algorithm requirements, and backend health.

### RoutingConfig Options

The `RoutingConfig` struct (`pkg/kms/router.go`) controls routing behavior:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `DefaultBackend` | `string` | `""` | Backend name to use when no routing rule applies. If empty, uses highest priority healthy backend. |
| `FailoverEnabled` | `bool` | `true` | Enable automatic failover to backup backends on failure. |
| `MaxFailoverAttempts` | `int` | `3` | Maximum number of backends to try before returning error. |
| `AlgorithmRouting` | `map[string]string` | `{}` | Maps algorithm names to preferred backend names. |
| `BackendTypeRouting` | `map[BackendType]string` | `{}` | Maps backend types to preferred backend names. |

### Routing Configuration

```yaml
kms:
  backends:
    - software
    - hsm
    - awskms

  # Routing configuration (optional)
  routing:
    # Default backend when no routing rule applies
    default_backend: software

    # Enable automatic failover to backup backends
    failover_enabled: true

    # Maximum failover attempts before returning error
    max_failover_attempts: 3

    # Algorithm-to-backend routing
    # Maps algorithm names to backend names
    algorithm_routing:
      CHACHA20-POLY1305: software    # Software backend for ChaCha20
      RSA-4096: hsm                   # HSM for RSA operations
      AES-256-GCM: awskms             # AWS KMS for AES-GCM
      ECDSA-P256: hsm                 # HSM for ECDSA signing

    # Backend type routing (optional)
    # Maps backend types to specific backend instances
    backend_type_routing:
      pkcs11: hsm-primary
      software: local-software
```

### Structured Key IDs

Use structured key IDs for explicit backend routing. Format:

```
backend:keytype:algorithm:keyname
```

| Component | Description | Examples |
|-----------|-------------|----------|
| `backend` | Target backend name | `hsm`, `software`, `vault`, `awskms` |
| `keytype` | Purpose of the key | `signing`, `encryption`, `master`, `dek` |
| `algorithm` | Cryptographic algorithm | `ecdsa-p256`, `aes-256-gcm`, `rsa-4096` |
| `keyname` | User-provided key name | `my-key`, `tenant-123`, `root` |

Examples:
```bash
# Create signing key on HSM backend
quicbaas kms key generate hsm:signing:ecdsa-p256:my-signing-key

# Create encryption key on software backend
quicbaas kms key generate software:encryption:aes-256-gcm:data-key

# Create master key in HashiCorp Vault
quicbaas kms key generate vault:master:rsa-4096:root-key

# Use simple key ID (uses default routing)
quicbaas kms key generate my-simple-key
```

### Routing Priority

The router selects backends in this order:

1. **Key ID prefix** - If key ID is structured (e.g., `hsm:signing:...`), use specified backend
2. **Algorithm routing** - Check `algorithm_routing` config for algorithm-to-backend mapping
3. **Registry algorithm lookup** - Query registry for backends supporting the algorithm
4. **Default backend** - Use `default_backend` if configured
5. **Priority-based** - Select highest priority healthy backend from registry
6. **Fallback** - Use fallback default backend if set via router option

### Backend Priority

Lower priority number = higher preference:

```yaml
kms:
  backends:
    - name: hsm
      priority: 1      # Primary - hardware security
    - name: awskms
      priority: 5      # Secondary - cloud backup
    - name: software
      priority: 100    # Fallback only
```

### Failover Configuration

Automatic failover retries operations on backup backends when the primary fails:

```yaml
kms:
  routing:
    # Enable/disable failover (default: true)
    failover_enabled: true

    # Maximum backends to try (default: 3)
    # Set to 0 to try all available backends
    max_failover_attempts: 3
```

Failover candidates include:
- All enabled backends not marked as `unhealthy`
- Backends supporting the requested algorithm (if specified)
- Default backend added as last resort

### Health Check Configuration

Backend health is tracked by the `BackendRegistry`:

```yaml
kms:
  registry:
    # How often to check backend health (default: 30s)
    health_check_interval: 30s

    # Timeout for each health check (default: 5s)
    health_check_timeout: 5s

    # Failures before marking unhealthy (default: 3)
    unhealthy_threshold: 3

    # Successes before marking healthy after recovery (default: 2)
    recovery_threshold: 2
```

## Backend Configuration

Multiple backends can be configured simultaneously. Each backend is configured independently, and the system can use any enabled backend based on operation requirements.

### Software Backend (Default)

```yaml
kms:
  backends:
    - software
  software:
    # Key storage directory
    key_dir: /var/lib/quicbaas/keys
```

### TPM2 Backend

```yaml
kms:
  backends:
    - software
    - tpm2
  tpm2:
    # TPM device path
    device: /dev/tpmrm0
    # Persistent handle for sealing key
    handle: 0x81000002
    # PCR configuration
    pcr:
      banks:
        - sha256
      registers:
        - 0
        - 7
        - 9
```

### PKCS#11 Backend (HSM)

```yaml
kms:
  backends:
    - software
    - pkcs11
  pkcs11:
    # PKCS#11 library path
    library: /usr/lib/softhsm/libsofthsm2.so
    # Token label
    token_label: quicbaas
    # PIN (use environment variable)
    pin: ${PKCS11_PIN}
```

### AWS KMS Backend

```yaml
kms:
  backends:
    - software
    - awskms
  awskms:
    # AWS region
    region: us-west-2
    # KMS key for sealing
    key_id: arn:aws:kms:us-west-2:123456789:key/abcd1234
```

### Azure Key Vault Backend

```yaml
kms:
  backends:
    - software
    - azurekv
  azurekv:
    # Key Vault URL
    vault_url: https://myquicbaas.vault.azure.net
    # Key name for sealing
    key_name: quicbaas-seal
```

### GCP Cloud KMS Backend

```yaml
kms:
  backends:
    - software
    - gcpkms
  gcpkms:
    # Project ID
    project_id: my-project
    # Location
    location: us-west1
    # Key ring
    key_ring: quicbaas
    # Crypto key for sealing
    crypto_key: seal-key
```

## AEAD Safety Configuration

AEAD safety limits can be configured at three levels with the following inheritance:

```
┌─────────────────────────────────────────────────────────────────────┐
│                        AEAD Config Resolution                        │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  Key/DEK has explicit AEAD config?                                   │
│      ├── YES → Use key/DEK's AEAD config                            │
│      └── NO  → Check kms.safety.enforce                             │
│                   ├── true  → Use kms.safety defaults               │
│                   └── false → No AEAD limits applied                │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Server-Level Defaults

```yaml
kms:
  safety:
    enforce: true                   # Apply defaults to keys without explicit config
    max_operations: 4294967296      # 2^32 operations
    max_bytes: 375809638400         # 350 GB
    warning_percent: 80
    auto_rotate_percent: 95
```

### Per-Key Configuration

```bash
# Set per-key limits
quicbaas kms key set-safety my-key \
  --max-operations 1000000000 \
  --max-bytes 100GB \
  --warning-percent 75

# Clear per-key config (revert to server defaults)
quicbaas kms key set-safety my-key --clear
```

### Per-DEK Configuration

```bash
# Set per-DEK limits
quicbaas kms dek set-safety tenant-123/ \
  --max-operations 10000000000 \
  --max-bytes 1TB \
  --warning-percent 75

# Clear per-DEK config (revert to server defaults)
quicbaas kms dek set-safety tenant-123/ --clear
```

### Use Cases

| Scenario | Configuration |
|----------|---------------|
| Compliance (strict limits) | `enforce: true` with conservative limits |
| Flexible (limits optional) | `enforce: false`, set per-key as needed |
| High-volume key | Per-key config with higher limits |
| Multi-tenant | Per-tenant DEK config with tenant-specific limits |

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `QUICBAAS_KMS_ENABLED` | Enable KMS | `false` |
| `QUICBAAS_KMS_BACKENDS` | Comma-separated backend list | `software` |
| `QUICBAAS_KMS_ALGORITHM` | Default master key algorithm | `Ed25519` |
| `QUICBAAS_KMS_DEK_ALGORITHM` | Default DEK algorithm | `AES-256-GCM` |
| `PKCS11_PIN` | PKCS#11 token PIN | - |
| `AWS_KMS_KEY_ID` | AWS KMS key ID | - |
| `AZURE_KEYVAULT_URL` | Azure Key Vault URL | - |

## Cluster DEK Configuration

```yaml
kms:
  cluster_dek:
    # Algorithm for DEK (symmetric - used for bulk data encryption)
    algorithm: AES-256-GCM    # AES-256-GCM | AES-128-GCM | ChaCha20-Poly1305
    # Rotation schedule
    rotation_period: 30d
    # Re-encryption batch size
    reencrypt_batch: 1000
    # Concurrent re-encryption workers
    reencrypt_workers: 4
```

## Performance Tuning

### High-Throughput

```yaml
kms:
  # Cache decryption keys in memory
  cache:
    enabled: true
    ttl: 5m
    max_keys: 100

  # Batch usage updates
  batch_updates:
    enabled: true
    interval: 100ms
    max_batch: 100
```

### Low-Latency

```yaml
kms:
  # Use Ed25519 for master keys (fast signing)
  defaults:
    algorithm: Ed25519

  # Use AES-GCM with AES-NI for DEKs
  dek:
    algorithm: AES-128-GCM

  # Disable batching
  batch_updates:
    enabled: false
```

## Complete Example

```yaml
kms:
  enabled: true

  # Multiple backends enabled
  backends:
    - software    # Always available as fallback
    - tpm2        # Hardware security for sealing

  # Software backend config
  software:
    key_dir: /var/lib/quicbaas/keys

  # TPM2 backend config
  tpm2:
    device: /dev/tpmrm0
    handle: 0x81000002
    pcr:
      banks:
        - sha256
      registers:
        - 0
        - 7
        - 9

  # Master key defaults (asymmetric)
  defaults:
    algorithm: Ed25519

  # DEK defaults (symmetric for bulk encryption)
  dek:
    algorithm: AES-256-GCM

  # NIST SP 800-38D compliant safety defaults
  safety:
    enforce: true
    max_operations: 4294967296
    max_bytes: 375809638400
    warning_percent: 80
    auto_rotate_percent: 95

  cluster_dek:
    algorithm: AES-256-GCM
    rotation_period: 30d
    reencrypt_batch: 1000
    reencrypt_workers: 4

  cache:
    enabled: true
    ttl: 5m
    max_keys: 100

  batch_updates:
    enabled: true
    interval: 100ms
    max_batch: 100
```


---

## Crypto Backend Selection — Implementation Design

> **Status: IMPLEMENTED.** The central `BackendResolver`, the KMS
> silent-downgrade fix (§5.1), the CA signing-key store split (§5.2), the PIN
> TPM path (§5.4), and the seal-strategy auto-detect (§5.5) are all shipped in
> `pkg/server/backend_resolver.go` + `pkg/config/crypto.go`. This page is the
> design of record; the **operator reference** for the `crypto:` block lives at
> [configuration/crypto.md](../composition/engineering.md). Code `file:line`
> references below are indicative — the functions named are the stable anchors.

### 1. Executive Summary

Every cryptographic operation QuicBaaS performs must be backed by a
configurable xkms backend and key reference. The default posture is
**AUTO-DETECT and PREFER HARDWARE**: TPM2 when the device is present,
software otherwise. Cloud backends (AWS KMS, GCP KMS, Azure Key Vault,
HashiCorp Vault) are supported as explicit opt-in choices.

This document is the authoritative implementation design. It defines:
- A single central resolver (`pkg/server/backend_resolver.go`) that
  every subsystem calls.
- A hardware-detect helper and the precise probe logic.
- The minimal additive config surface that is backward compatible with
  all existing deployments.
- A per-subsystem checklist ordered by value/risk, with exact file:line
  integration points and a test to add for each.
- An explicit section of go-qrdb upstream work that is out of scope for
  the quicbaas phase and requires separate sign-off.
- The risks: build-tag safety, FIPS interaction, and the
  embedded-node-owns-barrier constraint.

**Original motivating bug (now FIXED)**: `startKMS` previously hardcoded
`xkmssdk.NewSoftwareBackend` unconditionally, so even with `kms.backend: tpm2`
set the KMS service silently operated on a software backend. `startKMS`
(`pkg/server/lifecycle.go`) now resolves its backend through the central
`BackendResolver` and constructs it with the persistent key store via
`resolver.ServiceBackendWithStore` — a configured-but-not-compiled backend now
fails start with a typed `BackendNotCompiledError` instead of downgrading. See
§5.1.

---

### 2. The Abstraction: BackendResolver

**File**: `pkg/server/backend_resolver.go` (new file, no build tag)

This is the ONLY place in quicbaas that maps a configured backend type
to an xkms constructor call. No subsystem may call
`xkmssdk.NewSoftwareBackend`, `xkmssdk.NewTPM2Backend`, or any cloud
backend constructor directly.

#### Core types

```go
// BackendSpec identifies a backend type and optional key reference for
// one crypto site (KMS service, CA signing key, PIN backend, etc.).
// Backend "" and "auto" are synonymous: the hardware-prefer policy applies.
type BackendSpec struct {
    // Backend selects the backend type. Valid values:
    //   ""/"auto"   hardware-prefer auto-detection
    //   "software"  go-xkms software backend
    //   "tpm2"      TPM 2.0 (always compiled in)
    //   "pkcs11"    PKCS#11 HSM (requires -tags pkcs11)
    //   "awskms"    AWS Key Management Service (requires -tags awskms)
    //   "gcpkms"    Google Cloud KMS (requires -tags gcpkms)
    //   "azurekv"   Azure Key Vault (requires -tags azurekv)
    //   "vault"     HashiCorp Vault Transit (requires -tags vault)
    //   "quantum"   Post-quantum (ML-DSA/ML-KEM, pure Go, no tag)
    Backend config.BackendType

    // KeyRef is a backend-specific key identifier:
    //   tpm2:    persistent handle label (inherits from cfg.Seal.TPM2 if "")
    //   pkcs11:  CKA_LABEL of the key on the token
    //   awskms:  key ARN, key ID, or alias
    //   gcpkms:  full resource name "projects/P/locations/L/keyRings/R/cryptoKeys/K"
    //   azurekv: key name in the vault
    //   vault:   Transit key name
    //   software: key label / directory prefix (optional)
    KeyRef string
}

// HardwareProbe is the cached result of probing the local platform.
type HardwareProbe struct {
    TPM2Present   bool
    PKCS11Present bool
    // TPM2Device is the device path that responded (for diagnostic logging).
    TPM2Device string
    // PKCS11Library is the library path that exists on disk.
    PKCS11Library string
}

// BackendResolver constructs xkms backends from BackendSpec values. It is
// the single location where BackendType + KeyRef maps to an xkms constructor.
// Construct once at daemon startup via NewBackendResolver; pass down to each
// subsystem wiring function.
type BackendResolver struct {
    cfg   *config.Config
    probe HardwareProbe
}

// BackendNotCompiledError is returned when a BackendType requires a build tag
// not present in the current binary.
type BackendNotCompiledError struct {
    Backend config.BackendType
    Tag     string
}

func (e *BackendNotCompiledError) Error() string {
    return "crypto backend " + string(e.Backend) +
        " not compiled into this binary; rebuild with -tags " + e.Tag
}
```

The resolver returns four more typed errors (all in
`pkg/server/backend_resolver.go`). None ever panics or downgrades to software:

```go
// UnknownBackendError: value is neither auto/"" nor a recognized backend type.
type UnknownBackendError struct{ Backend config.BackendType }

// BackendConstructError: the go-xkms constructor failed (wraps the cause via Unwrap).
type BackendConstructError struct {
    Backend config.BackendType
    Cause   error
}

// StoreTypeUnsupportedError: backend has no CA key-store constant
// (only software/tpm2/pkcs11 are valid CA signing-key stores).
type StoreTypeUnsupportedError struct{ Backend config.BackendType }

// AEADUnsupportedError: backend cannot back a barrier AEAD provider
// (only software/tpm2 are supported).
type AEADUnsupportedError struct{ Backend config.BackendType }
```

#### Constructor and primary methods

```go
// NewBackendResolver creates a resolver that has already probed the local
// platform. Hardware detection runs exactly once; results are cached in
// r.probe.
func NewBackendResolver(cfg *config.Config) *BackendResolver {
    return &BackendResolver{cfg: cfg, probe: DetectHardware(cfg)}
}

// Resolve maps a BackendSpec to a concrete BackendType, applying the
// hardware-prefer policy when spec.Backend is "" or "auto". Returns
// BackendNotCompiledError when the resolved type requires a build tag
// not compiled in. Cloud backends are NEVER auto-selected.
func (r *BackendResolver) Resolve(spec BackendSpec) (config.BackendType, error)

// ServiceBackend constructs an xkmssdk.ServiceBackend ready to be
// registered with the global xkms service and wrapped by BackendFactory.
// Used by startKMS (pkg/server/lifecycle.go:809).
func (r *BackendResolver) ServiceBackend(spec BackendSpec) (xkmssdk.ServiceBackend, error)

// StoreType maps a BackendSpec to an xkmssdk.StoreType constant used in
// xkmssdk.KeyConfig and xkmssdk.TrustedCAIdentity.
//   software -> xkmssdk.StoreSoftware
//   tpm2     -> xkmssdk.StoreTPM2
//   pkcs11   -> xkmssdk.StorePKCS11
// Used by initializeCA (pkg/server/lifecycle.go:171).
func (r *BackendResolver) StoreType(spec BackendSpec) (xkmssdk.StoreType, error)

// Probe returns the cached hardware detection result (diagnostic use).
func (r *BackendResolver) Probe() HardwareProbe
```

#### Build-tagged backend registration

The same init()-registration pattern used by `seal_factories.go` applies
here. The resolver keeps a package-level dispatch map populated by
build-tagged files:

```go
// in backend_resolver.go (no build tag)
// keyStore is the persistence seam: startKMS passes a KVStore/file-backed store
// so KMS keys survive restarts; in-memory probes pass NewStorageMemory(). Every
// factory accepts it uniformly so dispatch stays a single signature, even for
// backends whose key material lives in hardware/off-host.
type serviceBackendFactory func(cfg *config.Config, spec BackendSpec, keyStore xkmssdk.StorageBackend) (xkmssdk.ServiceBackend, error)
var backendFactories = map[config.BackendType]serviceBackendFactory{}

func registerBackendFactory(bt config.BackendType, f serviceBackendFactory) { ... }
```

The resolver exposes two construction entry points: `ServiceBackend(spec)` (fresh
in-memory key store — non-persistent probes only) and
`ServiceBackendWithStore(spec, keyStore)` (caller-supplied persistent store).
`startKMS` uses the **WithStore** variant so KMS keys survive a daemon restart.

Each build-tagged file registers one constructor:

| File | Build tag | Registers |
|---|---|---|
| `backend_resolver_tpm2.go` | none | `config.BackendTypeTPM2` |
| `backend_resolver_pkcs11.go` | `pkcs11` | `config.BackendTypePKCS11` |
| `backend_resolver_awskms.go` | `awskms` | `config.BackendTypeAWSKMS` |
| `backend_resolver_gcpkms.go` | `gcpkms` | `config.BackendTypeGCPKMS` |
| `backend_resolver_azurekv.go` | `azurekv` | `config.BackendTypeAzureKV` |
| `backend_resolver_vault.go` | `vault` | `config.BackendTypeVault` |
| `backend_resolver_software.go` | none | `config.BackendTypeSoftware` |
| `backend_resolver_quantum.go` | none | `config.BackendTypeQuantum` |

`ServiceBackend()` looks up the type in `backendFactories`; a missing
entry returns `BackendNotCompiledError`. This is a clean typed error, not
a panic, not a nil return.

---

### 3. Hardware Auto-Detection

**Function**: `DetectHardware(cfg *config.Config) HardwareProbe`
in `pkg/server/backend_resolver.go`

#### TPM2 probe

```go
func probeTPM2(cfg *config.Config) (present bool, device string) {
    dev := "/dev/tpmrm0"
    if cfg != nil && cfg.Seal.TPM2.Device != "" {
        dev = cfg.Seal.TPM2.Device
    }
    _, err := xkmssdk.NewTPM2Backend(&xkmssdk.TPM2BackendConfig{Device: dev})
    if err == nil || errors.Is(err, xkmssdk.ErrTPM2BackendNotInitialized) {
        // Device opened (or opened + needs provisioning) = present.
        return true, dev
    }
    // Any other error (no device, permission denied, etc.) = absent.
    return false, ""
}
```

`xkmssdk.ErrTPM2BackendNotInitialized` means the TPM is accessible but
has not been provisioned by `provisionTPM2Backend`. It is present. Any
other error means absent. The probe NEVER panics and NEVER fails boot.

The resulting backend from a successful probe is discarded; the probe
establishes presence only. The actual backend is constructed later by
`NewBackendResolver.ServiceBackend` when needed.

#### PKCS11 probe

PKCS11 presence is inferred from config, not from dlopen, to avoid the
cost and risk of loading a shared object at probe time:

```go
func probePKCS11(cfg *config.Config) (present bool, lib string) {
    lib = ""
    if cfg != nil {
        if cfg.Seal.PKCS11.Library != "" {
            lib = cfg.Seal.PKCS11.Library
        } else if cfg.KMS.PKCS11.Library != "" {
            lib = cfg.KMS.PKCS11.Library
        }
    }
    if lib == "" {
        return false, ""
    }
    if _, err := os.Stat(lib); err != nil {
        return false, ""
    }
    return true, lib
}
```

#### Policy: resolving "auto"

```go
func (r *BackendResolver) Resolve(spec BackendSpec) (config.BackendType, error) {
    bt := spec.Backend
    if bt == "" || bt == "auto" {
        if r.probe.TPM2Present {
            bt = config.BackendTypeTPM2
        } else {
            bt = config.BackendTypeSoftware
        }
    }
    // Cloud backends are NEVER auto-selected; they fall through to the
    // factory lookup. If they require a build tag and it is absent,
    // BackendNotCompiledError is returned.
    if _, ok := backendFactories[bt]; !ok {
        tag := tagForBackend(bt) // maps BackendType -> "-tags" value
        return "", &BackendNotCompiledError{Backend: bt, Tag: tag}
    }
    return bt, nil
}
```

**Key policies**:
- `""` / `"auto"` → TPM2 if `r.probe.TPM2Present`, else software.
- PKCS11 is never auto-selected. It requires explicit `backend: pkcs11`
  because PKCS11 requires a configured token PIN which has no safe default.
- Cloud backends (`awskms`, `gcpkms`, `azurekv`, `vault`) are always
  explicit. They are never auto-detected.

---

### 4. Config Surface

**New file**: `pkg/config/crypto.go`

This is purely additive. Zero values preserve all existing behavior.
No existing config keys are removed or renamed.

```go
// CryptoConfig is the process-wide cryptographic backend policy.
// It provides a single default and per-subsystem overrides for every
// crypto site in quicbaas. Zero value = software backend everywhere
// (backward compatible with all existing deployments).
type CryptoConfig struct {
    // Backend is the process-wide default BackendType when a subsystem's
    // own spec is absent or "auto". The hardware-prefer policy treats ""
    // and "auto" identically: TPM2 if present, else software.
    // Cloud backends require explicit naming; they are never auto-selected.
    Backend BackendType `mapstructure:"backend" yaml:"backend"`

    // KeyRef is the process-wide default key label/identifier.
    // Subsystem-specific KeyRef overrides this when non-empty.
    KeyRef string `mapstructure:"key_ref" yaml:"key_ref"`

    // Subsystems provides per-subsystem backend overrides.
    // A zero-value BackendSpec means "inherit from the process default".
    Subsystems CryptoSubsystemsConfig `mapstructure:"subsystems" yaml:"subsystems"`
}

// CryptoSubsystemsConfig holds per-subsystem backend overrides.
type CryptoSubsystemsConfig struct {
    // KMS overrides the backend for the quicbaas KMS service (key management,
    // signing, symmetric encryption operations). When Backend is "",
    // inherits from CryptoConfig.Backend.
    KMS BackendSpec `mapstructure:"kms" yaml:"kms"`

    // CA overrides the backend for CA signing-key storage. The TLS server
    // key MUST remain software (it must be exportable for transport); only
    // the CA signing key is eligible for hardware storage.
    CA BackendSpec `mapstructure:"ca" yaml:"ca"`

    // PIN overrides the backend for the PIN backend (barrier credential
    // store). Valid: "" (auto), "software", "tpm2".
    // PKCS11 and cloud PIN backends are not wired in this phase.
    PIN BackendSpec `mapstructure:"pin" yaml:"pin"`

    // BarrierAEAD overrides the AEAD provider for the data-plane barrier's
    // internal symmetric key wrapping. "auto" follows the hardware-prefer
    // policy. Only software and tpm2 are supported in this phase.
    BarrierAEAD BackendSpec `mapstructure:"barrier_aead" yaml:"barrier_aead"`
}
```

`BackendSpec` is also defined in this file (it is pure config, not an
xkms type):

```go
// BackendSpec is re-exported from pkg/server for use in config structs.
// Defined here to keep pkg/config free of pkg/server import cycles.
type BackendSpec struct {
    Backend BackendType `mapstructure:"backend" yaml:"backend"`
    KeyRef  string      `mapstructure:"key_ref" yaml:"key_ref"`
}
```

Add `Crypto CryptoConfig` to `Config` in `pkg/config/config.go` and
to `Config.Clone()`.

**CAConfig additive field** — `pkg/config/config.go` in `CAConfig`:

```go
// KeystoreType selects the key store for the CA signing key.
// Values: "" (inherit from crypto.subsystems.ca.backend), "software",
// "tpm2", "pkcs11". Note: the TLS server key always uses software.
// Default: "" (inherits; backward compatible = software).
KeystoreType string `mapstructure:"keystore_type" yaml:"keystore_type"`
```

No existing field is modified. The new field is read only in
`initializeCA` when building the root CA identity.

**Example YAML** (showing the additive shape; existing configs without
this section are unaffected):

```yaml
crypto:
  backend: auto          # "" or "auto" = TPM2 if present else software
  key_ref: ""

  subsystems:
    kms:
      backend: tpm2      # override KMS to always use TPM2
      key_ref: ""
    ca:
      backend: software  # CA signing key: keep software for now
    pin:
      backend: ""        # inherits from crypto.backend
    barrier_aead:
      backend: ""        # inherits from crypto.backend
```

---

### 5. Per-Subsystem Implementation Checklist

Ordered by value/risk: highest-value first.

#### 5.1 KMS / Crypto / Secrets services — SILENT-DOWNGRADE FIX (P0 — IMPLEMENTED)

**Status**: implemented in `startKMS` (`pkg/server/lifecycle.go`). The backend is
resolved through `NewBackendResolver` and constructed with the persistent key
store via `resolver.ServiceBackendWithStore`. Precedence (highest first):
`crypto.subsystems.kms.backend` → `crypto.backend` → `kms.backend` (legacy) →
`auto`. A not-compiled/unknown backend returns the resolver's typed error and
fails start — never a software downgrade.

**Problem (historical)**: `startKMS` previously called `newSoftwareBackend`
unconditionally, ignoring `cfg.KMS.Backend` and `cfg.Crypto.Subsystems.KMS`. The
patch below is retained as the design narrative.

**Files to change**:
- `pkg/server/lifecycle.go` function `startKMS` (~lines 863-895)
- `pkg/server/backend_resolver.go` (new) — `ServiceBackend()` dispatch

**What to change**:

Replace the hardcoded software backend construction block at lines
863-876 with:

```go
resolver := NewBackendResolver(s.config)
kmsSpec := BackendSpec{
    Backend: s.config.Crypto.Subsystems.KMS.Backend,
    KeyRef:  s.config.Crypto.Subsystems.KMS.KeyRef,
}
// Fallback: if subsystem spec is empty, use the top-level KMS.Backend.
if kmsSpec.Backend == "" {
    kmsSpec.Backend = s.config.KMS.Backend
}
primaryBackend, err := resolver.ServiceBackend(kmsSpec)
if err != nil {
    return &KMSInitError{Stage: "resolve kms backend", Cause: err}
}
```

Then replace `softwareBackend` with `primaryBackend` as the argument to
`newXKMSKeystore`. The software backend is no longer unconditional; it is
the result of `ServiceBackend` when the resolved type is
`BackendTypeSoftware`.

`initFromXKMSFn` and the `BackendFactory.InitializeFromXKMS` path at
lines 908-919 remain unchanged; they consume whatever backends are now
registered in the global xkms service.

**xkms cloud path**: cloud backends (`awskms`, `gcpkms`, `azurekv`,
`vault`) use `xkmssdk.RegisterServiceBackend` rather than constructing a
`ServiceBackend` directly. The respective build-tagged file
`backend_resolver_awskms.go` calls the xkms factory registration API and
returns the registered backend as a `ServiceBackend`.

**Test to add**: `pkg/server/start_kms_errors_tt_test.go` (or a new
`lifecycle_kms_backend_test.go`):
```
TestStartKMS_HonorsTPM2Backend — when cfg.KMS.Backend = "tpm2" and a
mock TPM2 backend factory is registered, assert the mock factory is
called and the resulting backend is registered with the xkms service.

TestStartKMS_BackendNotCompiledError — when cfg.KMS.Backend = "pkcs11"
but the pkcs11 tag is absent, assert startKMS returns
BackendNotCompiledError wrapping "pkcs11" and does NOT fall back to software.

TestStartKMS_AutoSelectsSoftwareWhenNoTPM — when DetectHardware returns
{TPM2Present: false} and backend is "auto", assert software backend is used.
```

---

#### 5.2 CA / cert private keys (IMPLEMENTED)

**Status**: implemented in `pkg/server/lifecycle.go`. The CA signing-key spec is
resolved by `resolveCASigningSpec` (precedence: `crypto.subsystems.ca.backend` →
`crypto.backend` → `ca.keystore_type` legacy → `auto`, where `auto` stays
software — the trust root is never auto-promoted to hardware). `resolveCAStoreType`
maps it to a go-xkms `StoreType`, and `initializeCA` wires the CA's per-key
`Params.BackendForStore` closure (`caBackendForStore`) so a non-software store type
physically lands the **CA signing key** in the configured backend (e.g. `tpm2`)
while the software `StoreType` always maps back to the PKCS#8 software backend —
keeping the **TLS server leaf key** software and exportable for transport. A
configured-but-not-compiled backend surfaces `BackendNotCompiledError` instead of
a silent downgrade.

**Problem (historical)**: `initializeCA` previously hardcoded
`KeystoreType: xkmssdk.StoreSoftware` for the CA signing key. The narrative below
predates the `BackendForStore` per-key routing approach and is retained for
context; the TLS server key must still stay software (exportability is enforced
by the `Capabilities().Export` check).

**Files to change**:
- `pkg/server/lifecycle.go` function `initializeCA` (lines 257, 288)
- `pkg/config/config.go` `CAConfig` — add `KeystoreType string`

**What to change**:

At line 252 (before the `keyConfig` literal at 252-259), resolve the CA
key store type:

```go
caResolver := NewBackendResolver(s.config)
caSpec := BackendSpec{
    Backend: config.BackendType(s.config.CA.KeystoreType),
    KeyRef:  s.config.Crypto.Subsystems.CA.KeyRef,
}
if caSpec.Backend == "" {
    caSpec.Backend = s.config.Crypto.Subsystems.CA.Backend
}
caStoreType, err := caResolver.StoreType(caSpec)
if err != nil {
    return api.NewOperationFailedError("resolve CA keystore type", err)
}
```

Then at line 257 replace `xkmssdk.StoreSoftware` with `caStoreType`, and
at line 288 replace `xkmssdk.StoreSoftware` with `caStoreType`.

**Split keyStorage for hardware CA keys**: When `caStoreType !=
xkmssdk.StoreSoftware`, the CA signing key and the TLS server key must
use separate storage backends. The CA backend (hardware) is
non-exportable; the TLS backend (always software) is exportable. The
`keyStorage` variable at the top of `initializeCA` is currently shared.
Split it:

```go
// tlsKeyStorage is always software (exportable for TLS transport).
tlsKeyStorage := keyStorage  // existing software path, unchanged
// caKeyStorage is hardware when configured, software otherwise.
caKeyStorage := keyStorage
if caStoreType != xkmssdk.StoreSoftware {
    caKeyStorage, err = resolver.ServiceBackend(caSpec)
    // ... error handling
}
```

Use `caKeyStorage` for the `ServiceBackendConfig.Backend` in the CA
identity's keystore; use `tlsKeyStorage` for all server TLS cert
operations. The `Capabilities().Export` check at line ~508 continues to
apply only to `tlsKeyStorage`.

**Test to add**:
```
TestInitializeCA_SoftwareStoreSoftware — existing happy path, no regression.

TestInitializeCA_TPM2StoreType — cfg.CA.KeystoreType = "tpm2", mock
BackendResolver.StoreType to return StoreTPM2; assert keyConfig.StoreType
= xkmssdk.StoreTPM2 and caCAConfig.StoreType = xkmssdk.StoreTPM2.

TestInitializeCA_TLSKeyAlwaysSoftware — CA backend = "tpm2", assert the
TLS server key config (KeyTypeStringTLS) still uses StoreSoftware.
```

---

#### 5.3 Data-plane barrier AEAD provider

**Status (P2 — confirmed software by design)**: NOT wired, intentionally.
`BackendResolver.AEADProvider` returns `nil` for both `software` AND `tpm2`
(see `pkg/server/backend_resolver.go`), and `buildDataPlaneBarrierService`
leaves `BarrierConfig.AEADProvider` unset, so qrdb uses its software
AES-256-GCM epoch-DEK default. This is the envelope-encryption model: the
hardware boundary is the ROOT-KEY SEAL (the seal strategy, §5.5) and
optionally the DEK envelope (§6.2); the per-block/per-epoch bulk AEAD stays
software for throughput (§6.4). Injecting the provider here would be dead
code today (always `nil` for the only supported backends), so the seam is
left documented rather than wired (YAGNI). The text below is retained as the
design of record for when a hardware DEK envelope lands.


**Problem / Opportunity**: `buildDataPlaneBarrierService` at
`pkg/server/dataplane_barrier.go:113` constructs a `qrdbsdk.BarrierConfig`
at line 184 but leaves `AEADProvider` nil (defaults to software). The
`qrdbsdk.BarrierConfig.AEADProvider` field (declared at
`go-qrdb/pkg/seal/config.go:93`, re-exported at
`go-qrdb/sdk/go/types_seal.go:101`) IS the injection seam — it already
exists in qrdb and requires no upstream change.

**Note on scope**: This applies to the DATA-PLANE barrier that quicbaas
builds directly (for vector/timeseries at-rest encryption). It does NOT
apply to the embedded qrdb node's own internal barrier, which requires an
upstream qrdb change (see Section 6.1).

**Files to change**:
- `pkg/server/dataplane_barrier.go` function `buildDataPlaneBarrierService`
  (lines 184-197)
- `pkg/server/backend_resolver.go` — add `AEADProvider()` method

**What to change**:

Add to `BackendResolver`:

```go
// AEADProvider returns an xkms-backed AEADProvider for the given spec,
// or nil when the spec resolves to software (nil = qrdb uses the
// software default, which is already AES-256-GCM).
// Only tpm2 is supported in Phase 1; pkcs11/cloud return ErrNotSupported.
func (r *BackendResolver) AEADProvider(spec BackendSpec) (qrdbsdk.AEADProvider, error)
```

In `buildDataPlaneBarrierService`, after line 184:

```go
barrierCfg := qrdbsdk.BarrierConfig{
    RootKeyPath: "root-key",
}
// Inject hardware AEAD provider when configured.
if cfg.Crypto.Subsystems.BarrierAEAD.Backend != "" &&
    cfg.Crypto.Subsystems.BarrierAEAD.Backend != config.BackendTypeSoftware {
    resolver := NewBackendResolver(cfg)
    ap, apErr := resolver.AEADProvider(cfg.Crypto.Subsystems.BarrierAEAD)
    if apErr != nil {
        logger.Warn("data-plane barrier: hardware AEAD provider unavailable, falling back to software",
            slog.String("reason", apErr.Error()))
        // Non-fatal: fall back to software AEAD. Log and continue.
    } else if ap != nil {
        barrierCfg.AEADProvider = ap
    }
}
```

**Phase 1 implementation note**: A TPM2-backed `AEADProvider` requires a
`*xkmssdk.TPM2Backend` instance to perform the in-TPM HMAC-derive +
AES-GCM operations. That instance is currently not shared outside
`buildTPM2Strategy`. In Phase 1, the AEAD provider for `barrier_aead:
tpm2` can return a software provider (no-op for this field) while the
TPM seal strategy protects the root key — the security boundary is
maintained by the seal, not the per-key-epoch AEAD. A future phase
wires the TPM backend instance through to this provider once the
`buildTPM2Strategy` function is refactored to share its backend.

**Test to add**:
```
TestBuildDataPlaneBarrierService_SoftwareAEADByDefault — zero config,
assert BarrierConfig.AEADProvider is nil (uses qrdb default).

TestBuildDataPlaneBarrierService_CustomAEADProvider — inject a mock
BackendResolver that returns a non-nil AEADProvider; assert the
BarrierConfig.AEADProvider is set to that value.

TestBuildDataPlaneBarrierService_AEADProviderFallback — inject a mock
BackendResolver that returns an error; assert construction succeeds
(non-fatal fallback) and BarrierConfig.AEADProvider is nil.
```

---

#### 5.4 PIN backend — TPM provider path (P3 — IMPLEMENTED)

**Status**: implemented in `pkg/server/pin_options.go` (selection + provider +
typed errors), `pkg/server/seal_factories.go` (shared-backend hook
`buildHardwareSealStrategiesShared` + `buildTPM2Backend`),
`pkg/server/seal_autodetect.go` (auto seam now returns the open backend), and
`pkg/server/qrdb_node.go` (`buildSealStrategyOnce` + threading the shared TPM
into `buildPINBackend`). The shared backend is cached on the `Server`
(`sealStrategy`, `sealTPM2Backend`).

**Selection + precedence** (`resolvePINBackendType`, `pin_options.go`):

1. `crypto.subsystems.pin.backend` — per-subsystem override
2. `crypto.backend` — process-wide default (applied when the PIN spec is
   unset/`auto`)
3. `""`/`auto` → **software**

Unlike KMS/CA, the PIN backend **never auto-selects hardware**: `""`/`auto`
always resolve to software (the safe default that boots without a TPM). Only an
EXPLICIT `tpm2` — named on the PIN subsystem OR inherited from an explicit
`crypto.backend: tpm2` — opts in to the TPM path. `software`/`auto`/`""` are
byte-for-byte the historical behavior (persistent software backend under
`<dataDir>/pin`).

**Single TPM open (the mechanism):** the seal strategy is built ONCE, up front,
by `Server.buildSealStrategyOnce` (before `buildXKMSCryptoOptions`).
`buildHardwareSealStrategiesShared` returns BOTH the seal strategy AND the open
`*xkmssdk.TPM2Backend` it wraps; that backend is cached on the Server and reused
for the PIN provider. The TPM is therefore opened EXACTLY ONCE for both seal and
PIN — `sealSharedTPMPINProvider.PlatformAuth()` builds its `PlatformKeyStore`
over `backend.TPM()` and NEVER calls `NewTPM2Backend` (a second open hangs a
single-session swtpm). Explicit `tpm2` is special-cased in the shared builder
(via `buildTPM2Backend`) precisely so the open is recoverable for sharing; the
per-tenant strategy registry still uses `buildTPM2Strategy` unchanged.

**tpm2-without-tpm-seal handling (fail-closed):** if the resolved PIN backend is
`tpm2` but no TPM2 seal strategy is active (no shared backend), `buildPINBackend`
returns the typed `*PINRequiresTPMSealError` — an explicit hardware demand is
never silently downgraded to software (mirrors the KMS P0 policy). The operator
must also set `seal.strategy: tpm2` (or `auto` with a provisioned TPM). A
resolved PIN type that the subsystem does not support (e.g. `pkcs11`) returns
`*PINBackendUnsupportedError`.

**No boot-time provisioning / clean software fallback:** when a TPM2 seal IS
active but no `PlatformSRK` is enrolled, the provider reports unavailable and
`pinselect` falls back to the persistent software backend (logged). quicbaasd
does NOT provision a `PlatformSRK` at boot — the qrdb `PINBackend` contract
verifies an existing auth value, it cannot bootstrap one, and boot-time TPM
provisioning is unreliable. A `PlatformSRK` is established out-of-band (e.g. the
xKey enrollment wizard against the same TPM + KeyDir). The PIN `PlatformSRK`
(handle `0x81000002`) is DISTINCT from the seal SSRK (`0x81000001`), so
setting/changing the PIN never disturbs the root-key seal.

**Tests:**
```
# hardware-free unit tests (run in `make test`):
TestResolvePINBackendType_Precedence                       (10-case precedence table)
TestBuildPINBackend_SoftwareDefault                        (empty/auto -> software, unchanged)
TestBuildPINBackend_SoftwareIgnoresProvider                (explicit software opts out of TPM)
TestBuildPINBackend_TPM2SelectsTPMProvider                 (tpm2 + usable provider -> tpm2)
TestBuildPINBackend_TPM2ProviderUnavailableFallsBackToSoftware  (no PlatformSRK -> software)
TestBuildPINBackend_TPM2WithoutSealReturnsTypedError       (PINRequiresTPMSealError, fail-closed)
TestBuildPINBackend_UnsupportedBackend                     (PINBackendUnsupportedError)
TestNewSealSharedTPMPINProvider_NilInputs / TestPINErrorTypes
# (existing) TestBuildPINBackend_PersistentSoftwareUnderDataDir / _NoDataDirIsInMemorySoftware

# swtpm hardware proof (build tag `swtpm,codec_json`; t.Skip when unreachable):
TestPINBackend_Swtpm_SharedSealTPM — seal.strategy: tpm2 -> single non-reopened
  TPM open -> shared *TPM2Backend -> PIN provider over the SAME open ->
  production buildPINBackend selects the TPM2 PIN backend. HARD-PROVES the
  single-open sharing + provider selection.
```

**KNOWN xkms GAP (flagged):** the in-hardware PIN *set/verify* itself goes
through xkms `PlatformKeyStore.Initialize` -> `TPM2.VerifyAuth`, which —
reproduced STANDALONE with xkms's own `DefaultConfig` (zero quicbaas code) —
returns `auth verification failed` on swtpm even for the PIN the SRK was just
created with; the persistent SRK handle (`0x81000002`) also survives in swtpm NV
across runs while its key context does not. This is an xkms-side
`PlatformKeyStore`/`VerifyAuth` issue, NOT a quicbaas wiring defect. The swtpm
test flags it with a precise `t.Skip` after hard-proving the P3 wiring above.
Because quicbaasd never drives that path at boot (no enrollment flow), the
production posture is unaffected: a non-enrolled TPM degrades to software
cleanly. Resolving in-hardware PIN verification on the shared seal TPM requires
an xkms fix (and a quicbaas/xKey-style PlatformSRK enrollment flow) — tracked
separately, out of P3 scope.

#### 5.5 Seal strategy auto-detect + KEK wrap (P4 — IMPLEMENTED)

**Status**: implemented in `pkg/server/seal_autodetect.go`,
`pkg/server/seal_factories.go` (`buildHardwareSealStrategies`),
`pkg/server/init_marker.go` (`resolveBarrierStrategy`), and
`pkg/server/dataplane_barrier.go` (status label). The
`hardwareStrategyFactories` init-registration pattern is unchanged; TPM2 is
always compiled in, cloud/PKCS11 are build-tagged.

**Policy as built (conservative, OPT-IN — mirrors the P1 CA-path lesson):**

- Auto-detect is triggered ONLY by the explicit `seal.strategy: auto`
  sentinel. The empty string and the `passphrase` default are returned
  unchanged, so the default posture and every existing deployment are
  byte-for-byte unaffected and the unit-test path never touches a TPM.
  (The doc's earlier "auto from empty string" idea was dropped: it would
  make `make test` probe real hardware and would risk regressing TPM hosts
  on any partially-specified config.)
- Under `auto`, `buildHardwareSealStrategies` opens the TPM **exactly once**
  via `buildTPM2AutoSealStrategy`: a NON-PROVISIONING, time-bounded
  (`sealAutoProbeTimeout = 3s`) open. It selects `tpm2` only when the
  backend opens quickly AND reports `CanSeal()` (SRK hierarchy already
  present). The single open's seal-capable backend is RETAINED inside the
  returned `TPM2Strategy` and reused for the barrier's life — there is no
  probe-then-reopen.
- A fresh/unprovisioned TPM (`ErrTPM2BackendNotInitialized`), an
  inaccessible device, a non-seal-capable backend, or a timeout all fall
  back to the software passphrase strategy cleanly. Auto-detect NEVER
  provisions and NEVER stalls boot. An operator who wants boot-time
  provisioning must name `tpm2` explicitly (the explicit path is unchanged
  and still provisions via `buildTPM2Strategy`).

**Why single-open / no-provision (empirical, swtpm 0.7.3):**

- Boot-time provisioning via the production path is heavy and unreliable: a
  fresh swtpm's provisioning hangs >120s and fails with `TPM_RC_RETRY`.
  Auto must therefore never provision.
- A TPM command channel is a single session: the swtpm emulator HANGS on the
  SECOND open within a process (open #1 = ~11ms success; opens #2/#3 hang).
  The kernel `/dev/tpmrm0` resource manager multiplexes on real hardware,
  but opening once keeps the path emulator-safe and is strictly cheaper on
  real hardware too. This is why the probe was folded INTO the strategy
  build (one open) rather than a separate probe + rebuild.

**KEK wrap (root key seal):** the root key IS hardware-wrapped — the TPM2
strategy's `SealRootKey` seals it under the TPM SRK hierarchy
(`NewTPM2Strategy(backend, nil)`). Verified by the swtpm round-trip below.
The optional second arg (a hardware `types.SymmetricEncrypter` for the
per-epoch DEK) is intentionally `nil`: routing per-epoch/bulk symmetric
crypto through the TPM would defeat the envelope model's throughput goal
(§6.4) and would require provisioning a dedicated TPM-resident symmetric key
not in the seal recipe. The capability EXISTS in go-xkms
(`tpm2backend.Backend.SymmetricEncrypter`) — this is a deliberate
architecture choice, NOT an xkms gap. Bulk data stays software by design.

**Tests:**
```
# hardware-free unit tests (run in `make test`):
TestResolveEffectiveSealStrategy_StringMapping / _NilConfig
TestBuildHardwareSealStrategies_AutoSelectsTPM2WhenUsable
TestBuildHardwareSealStrategies_AutoFallsBackToSoftware
TestBuildHardwareSealStrategies_AutoNeverProvisionStub
TestResolveBarrierStrategy_AutoFallsBackToPassphrase
TestBuildTPM2AutoSealStrategy_UnreachableSimulatorIsNotUsable / _NilConfig

# swtpm hardware proof (build tag `swtpm,codec_json`; t.Skip when unreachable):
TestSealAutoDetect_Swtpm — seal.strategy: auto -> single non-provisioning
  open -> tpm2 strategy -> SealRootKey/UnsealRootKey round-trip recovers the
  root key byte-for-byte against swtpm. PASSES.
```

---

#### 5.6 Factor registry (already correctly wired)

**Current state**: `XKMSCryptoConfig.Factors` at
`pkg/server/crypto_options.go:66` and `XKMSCryptoOptions` at line 96
are the correct extension point. Hardware factors (FIDO2, PIV, PKCS11)
are registered by injecting `[]qrdbsdk.AuthFactorPlugin` into
`XKMSCryptoConfig.Factors` from the quicbaas lifecycle.

**No change required** for the registry mechanism itself. Adding a
hardware factor is done by constructing the xkms factor implementation
and appending it to `XKMSCryptoConfig.Factors` before calling
`XKMSCryptoOptions`. The test coverage in `crypto_options_test.go`
already validates registration.

---

#### 5.7 Per-tenant barrier

**Current state**: Per-tenant barriers go through the embedded qrdb
node's tenant barrier subsystem. The strategy registry for tenant
barriers is injectable via `qrdbsdk.WithTenantStrategyRegistry`
(available in `go-qrdb/sdk/go/types_tenant_compose.go`).

**What can be done now**: ensure
`XKMSCryptoConfig.StrategyFactories` (wired into `WithStrategyRegistry`)
also applies to tenant barriers by using the same factory map in
`WithTenantStrategyRegistry`. This ensures tenant barriers can use
hardware seal strategies when the operator configures them.

**What requires upstream work**: injecting a hardware `AEADProvider`
into per-tenant barriers requires the `WithBarrierConfig` server option
described in Section 6.1. Without it, per-tenant barriers always use the
software AEAD provider.

---

### 6. go-qrdb Upstream Work

These items require changes to go-qrdb and CANNOT be done in quicbaas
alone. They need separate sign-off and their own tracked phase.

#### 6.1 WithBarrierConfig ServerOption for the embedded qrdb node barrier

**Why needed**: The embedded qrdb node builds its own `StorageBarrier`
in `go-qrdb/pkg/server/server.go`. Quicbaas cannot currently inject a
custom `AEADProvider` into it. The data-plane barrier (Section 5.3
above) is built by quicbaas and is already injectable. The embedded
node's barrier is not.

**Proposed seam**:

```go
// in go-qrdb/pkg/server/server.go
func WithBarrierConfig(cfg *seal.BarrierConfig) ServerOption {
    return func(o *serverOptions) { o.barrierConfig = cfg }
}
```

The qrdb server constructor uses `o.barrierConfig.AEADProvider` (when
non-nil) when calling `seal.NewStorageBarrier`. Default: nil = existing
software AEAD behavior unchanged.

**Quicbaas call site** after this is available: in `buildQRDBNodeOptions`
(`pkg/server/qrdb_node.go`), add `qrdbsdk.WithBarrierConfig(&barrierCfg)`
where `barrierCfg.AEADProvider` is resolved via `BackendResolver`.

**Re-exported as**: `qrdbsdk.WithBarrierConfig` in
`go-qrdb/sdk/go/types_server.go`.

**Affects**: system barrier for qrdb internal data (credentials, Raft
state). Also allows per-tenant barrier AEAD customization through the
same mechanism.

---

#### 6.2 DEK Envelope Provider

**Current state**: `go-qrdb/pkg/dek/crypto.go:24` implements
`AESGCMEncrypt`/`AESGCMDecrypt` with a raw AES key. No provider seam
exists. DEK wrapping (the envelope that protects per-file encryption
keys) is hardcoded AES-256-GCM in software.

**Proposed seam**:

```go
// in go-qrdb/pkg/dek/ (new file: provider.go)
type EnvelopeProvider interface {
    // Wrap encrypts a plaintext DEK for storage.
    Wrap(plainDEK []byte) ([]byte, error)
    // Unwrap decrypts a stored DEK.
    Unwrap(wrappedDEK []byte) ([]byte, error)
}

// SoftwareEnvelopeProvider is the current AES-256-GCM behavior.
type SoftwareEnvelopeProvider struct{ rootKey []byte }

// New server option:
func WithDEKProvider(p EnvelopeProvider) qrdbserver.ServerOption { ... }
```

When a TPM or HSM `EnvelopeProvider` is configured, every per-file DEK
is wrapped by the hardware device. This means hardware holds keys for all
bulk data — a significant security improvement.

**Implementation note**: the provider receives the already-derived DEK
and wraps it; it does NOT do bulk AES block encryption (that stays in
`encfs/block.go` as software — intentional for performance, see 6.4).

---

#### 6.3 HPKE Recipient Key Provider

**Current state**: `go-qrdb/pkg/keydelivery/hpke.go` hardcodes
DHKEM(P-256)/HKDF-SHA256/AES-256-GCM with the recipient's ECDSA private
key used directly in-process. No provider seam.

**Proposed seam**:

```go
// in go-qrdb/pkg/keydelivery/
type RecipientProvider interface {
    // DHKEMDecap performs the recipient-side DHKEM decapsulation.
    // Input: sender's ephemeral encapsulated key. Output: HPKE shared secret.
    DHKEMDecap(enc []byte) ([]byte, error)
}
```

A TPM/HSM `RecipientProvider` would perform the ECDH operation inside
the hardware, preventing the recipient private key from being exposed in
process memory.

**Priority**: Lower than 6.1 and 6.2. HPKE key delivery is used for
barrier share transport (Shamir unseal key delivery to cluster peers),
not for at-rest data. The risk is limited to the key delivery window.

---

#### 6.4 encfs/block.go — intentionally excluded

`go-qrdb/pkg/seal/encfs/block.go` performs AES-256-GCM inline block
encryption for every Pebble SST block. This is hardcoded in software and
**must remain so** for performance: a TPM cannot sustain the throughput
required for inline storage block crypto. The hardware boundary for
at-rest protection is the root key seal (controlled by the seal strategy)
and optionally the DEK envelope (6.2 above). Proposing a hardware seam
here would introduce unacceptable latency.

---

### 7. Risks

#### 7.1 Build-tag story

**Risk**: Calling a cloud or PKCS11 backend constructor when the binary
was not compiled with the corresponding tag.

**Mitigation**: The resolver's `backendFactories` map is populated only
by tagged `init()` files. A missing entry for a configured backend type
returns `BackendNotCompiledError` from `ServiceBackend()`. This error
propagates as the return value of `startKMS` (or `initializeCA`, etc.),
causing a clean, described daemon start failure:

```
crypto backend pkcs11 not compiled; rebuild with -tags pkcs11
```

**Design contract**: `BackendNotCompiledError` MUST be a first-class
typed error. The resolver MUST NOT panic. The resolver MUST NOT silently
fall back to software — silent fallback defeats the security intent of
the configuration.

**Testing**: each build-tagged resolver file must have a paired test file
(same build tag) that asserts the constructor is called correctly. The
base file must have a test asserting `BackendNotCompiledError` is returned
for backends whose tag is absent (testable by calling `ServiceBackend`
with a type that has no registered factory in the test binary).

---

#### 7.2 FIPS mode interaction

When `GOFIPS140=1` (Go 1.24+ native FIPS mode):

- **Software backend**: uses Go stdlib AES-GCM/SHA-256/ECDSA — all FIPS
  validated. No action needed.
- **TPM2 backend**: TPM hardware operations are outside the Go FIPS
  boundary. The TPM's own FIPS validation (FIPS 140-2 Level 2+ for most
  consumer TPMs) applies independently. No conflict.
- **PKCS11 backend**: the HSM's FIPS validation applies to on-token
  operations. The pkcs11 library is non-Go and is not subject to
  `GOFIPS140`. Operations that cross the PKCS11 boundary (key material
  import/export) must use FIPS-validated algorithms.
- **Quantum backend** (`BackendTypeQuantum`): ML-DSA and ML-KEM are FIPS
  204/203 algorithms, but cloudflare/circl's pure-Go implementations are
  NOT FIPS 140 validated. `DetectHardware` (or an early validation step
  in `BackendResolver.Resolve`) SHOULD emit a warning (not an error) when
  `BackendTypeQuantum` is configured with `GOFIPS140=1`.
- **Cloud backends**: cloud KMS endpoints are operated by the provider;
  their FIPS compliance depends on the endpoint variant (e.g., AWS KMS
  FIPS endpoints at `kms.us-east-1.fips.amazonaws.com`). The resolver
  does not enforce this; operators must configure the correct endpoint.

---

#### 7.3 Embedded-node-owns-barrier constraint

A running `quicbaasd` process contains TWO independent `StorageBarrier`
instances:

| Barrier | Owner | Built by | AEAD injectable now? |
|---|---|---|---|
| Embedded qrdb node barrier | go-qrdb internal | `qrdbnode.NewStandaloneNode` | No (needs 6.1) |
| Data-plane barrier | quicbaas | `buildDataPlaneBarrierService` | Yes (Section 5.3) |

Both barriers use `cfg.Seal.Strategy` for root key protection (already
correct — `WithStrategyRegistry` injects the hardware strategy into the
embedded node; `buildDataPlaneBarrierService` calls
`buildHardwareSealStrategies`). The gap is the AEAD provider for the
embedded node's barrier.

**Implication for Phase 1**: both barriers will have hardware-sealed root
keys when TPM2/HSM/cloud is configured. The per-key-epoch symmetric
wrapping within the embedded node's barrier remains software until 6.1
ships. This is acceptable: the root key is the primary secret; the AEAD
provider protects derived keys that are already protected by the root key.

**Key for implementations**: never assume the two barriers are the same
object. Do not attempt to share barrier instances or bypass this boundary.

---

### 8. Integration Point Summary

Anchors are function names (stable), not line numbers (which drift).

| Subsystem | Anchor | Status |
|---|---|---|
| KMS backend | `lifecycle.go` `startKMS` → `BackendResolver.ServiceBackendWithStore` | Implemented (§5.1) |
| CA signing key | `lifecycle.go` `resolveCASigningSpec` / `resolveCAStoreType` / `caBackendForStore` (`Params.BackendForStore`) | Implemented (§5.2) |
| TLS server key | `lifecycle.go` — stays `StoreSoftware` (must be exportable) | No change by design |
| Data-plane barrier AEAD | `dataplane_barrier.go` `buildDataPlaneBarrierService` + `BackendResolver.AEADProvider` | Software by design (§5.3) |
| PIN backend TPM path | `pin_options.go` `resolvePINBackendType` / `buildPINBackend` | Implemented (§5.4) |
| Seal strategy auto-detect | `seal_autodetect.go` + `seal_factories.go` `buildHardwareSealStrategies` | Implemented (§5.5) |
| Factor registry | `crypto_options.go` `XKMSCryptoConfig.Factors` | No change — already wired (§5.6) |
| Per-tenant barrier AEAD | `go-qrdb/pkg/server/server.go` | Upstream — `WithBarrierConfig` (§6.1) |
| DEK envelope | `go-qrdb/pkg/dek/crypto.go` | Upstream — `EnvelopeProvider` (§6.2) |
| HPKE recipient key | `go-qrdb/pkg/keydelivery/hpke.go` | Upstream — `RecipientProvider` (§6.3) |
