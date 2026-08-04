# Crypto Backend Selection (`crypto:`)

The `crypto:` block is QuicBaaS's process-wide cryptographic backend policy.
It declares **which go-xkms backend stores the keys** for each crypto site in
the daemon (KMS service, CA signing key, PIN backend, data-plane barrier AEAD),
with a single default plus per-subsystem overrides.

The block is **purely additive and backward compatible**: the zero value (no
`crypto:` block at all) resolves to the software backend everywhere, byte-for-byte
the historical behavior. Existing deployments are unaffected.

> Implementation: `pkg/config/crypto.go` (config surface) and
> `pkg/server/backend_resolver.go` (the single central `BackendResolver` that
> maps a backend type + key reference to a concrete go-xkms constructor). No
> subsystem calls a go-xkms backend constructor directly.

## Backend types

| `backend` value | Backend | Build tag required |
|-----------------|---------|--------------------|
| `software` | go-xkms software backend (PKCS#8 key files) | none (always compiled) |
| `tpm2` | TPM 2.0 hardware | none (always compiled) |
| `quantum` | Post-quantum (ML-DSA / ML-KEM, pure Go) | none (always compiled) |
| `pkcs11` | PKCS#11 HSM | `-tags pkcs11` |
| `awskms` | AWS Key Management Service | `-tags awskms` |
| `gcpkms` | Google Cloud KMS | `-tags gcpkms` |
| `azurekv` | Azure Key Vault | `-tags azurekv` |
| `vault` | HashiCorp Vault Transit | `-tags vault` |
| `auto` / `""` | Hardware-prefer auto-detection (see below) | none |

`auto` and the empty string are synonymous.

## Keys

```yaml
crypto:
  backend: auto          # process-wide default backend (software | tpm2 | pkcs11 |
                         #   awskms | gcpkms | azurekv | vault | quantum | auto)
  key_ref: ""            # process-wide default key identifier (backend-specific)

  subsystems:            # per-subsystem overrides; a zero-value entry inherits
    kms:                 #   from crypto.backend / crypto.key_ref
      backend: ""
      key_ref: ""
    ca:
      backend: ""
      key_ref: ""
    pin:
      backend: ""
    barrier_aead:
      backend: ""
```

### `crypto.backend` / `crypto.key_ref`

`crypto.backend` is the process-wide default applied to any subsystem whose own
spec is absent or `auto`. `crypto.key_ref` is the default key identifier; a
non-empty subsystem `key_ref` overrides it.

`key_ref` is backend-specific: TPM2 = persistent-handle label; PKCS#11 =
`CKA_LABEL`; awskms = key ARN/ID/alias; gcpkms = full resource name; azurekv =
key name; vault = Transit key name; software = key label/directory prefix. Empty
means the backend's own default applies.

### `crypto.subsystems`

Each subsystem resolves its backend independently. A zero-value `BackendSpec`
(`backend: ""`) inherits the process default.

| Subsystem | Controls | Valid backends | Notes |
|-----------|----------|----------------|-------|
| `kms` | The KMS service key universe (key management, signing, symmetric encryption) | any | Persistent key store survives restarts. |
| `ca` | The **CA signing key** store only | `software`, `tpm2`, `pkcs11` | The TLS server **leaf** key always stays `software` (it must be exportable for transport). A non-software type puts only the CA signing key in hardware. |
| `pin` | The barrier credential (PIN) backend | `software`, `tpm2` | Never auto-selects hardware — see below. |
| `barrier_aead` | The data-plane barrier's per-epoch symmetric key wrapping | `software`, `tpm2` | Envelope model: stays software. See below. |

The CA signing-key store is the only subsystem with a go-xkms key-store
constant; cloud and quantum backends are not valid CA stores
(`StoreTypeUnsupportedError`). The TPM leaf-vs-signing split is wired through the
CA's per-key `BackendForStore` routing (`pkg/server/lifecycle.go`
`caBackendForStore`): `crypto.subsystems.ca.backend=tpm2` puts the CA signing key
in the TPM while the TLS leaf stays software.

## The `auto` / hardware-prefer policy

When a subsystem resolves to `auto` (or `""`), the `BackendResolver` applies the
hardware-prefer policy:

- **TPM 2.0** if a device responds to the one-time hardware probe, else
- **software**.

Cloud and HSM backends (`pkcs11`, `awskms`, `gcpkms`, `azurekv`, `vault`) are
**never auto-selected** — they must be named explicitly, because they require
credentials/PINs with no safe default. PKCS#11 presence is diagnostic only.

Two subsystems deliberately do **not** auto-promote to hardware even on a
TPM-equipped host:

- **CA** — the CA signing key is the trust root for every certificate the node
  issues. `auto`/`""` keeps it `software`; only an **explicit** non-software
  backend moves it into hardware.
- **PIN** — `auto`/`""` always resolves to `software` (the safe default that
  boots without a TPM). Only an explicit `tpm2` (named on `crypto.subsystems.pin`
  or inherited from an explicit `crypto.backend: tpm2`) opts in to the TPM path.
  An explicit `tpm2` PIN with no active TPM seal strategy fails closed with
  `PINRequiresTPMSealError` (it is never silently downgraded).

## Per-subsystem precedence

Each subsystem composes its spec from the most specific source first, then falls
back. `auto`/`""` at any level means "inherit / hardware-prefer".

| Subsystem | Precedence (highest first) |
|-----------|----------------------------|
| **KMS** | `crypto.subsystems.kms.backend` → `crypto.backend` → `kms.backend` (legacy) → `auto` |
| **CA** | `crypto.subsystems.ca.backend` → `crypto.backend` → `ca.keystore_type` (legacy) → `auto` (= software) |
| **PIN** | `crypto.subsystems.pin.backend` → `crypto.backend` → `software` |
| **barrier_aead** | `crypto.subsystems.barrier_aead.backend` → (software default) |

The legacy `kms.backend` and `ca.keystore_type` fields are honored only when no
`crypto.*` backend is configured, so older configs keep working unchanged. See
[KMS Configuration](../kms/configuration.md) for the relationship to the legacy
`kms.backends` router.

## Fail-closed: never a silent software downgrade

A configured backend whose constructor was **not compiled into the binary**
yields a typed `BackendNotCompiledError` from the resolver — the daemon refuses
to start rather than silently falling back to software (which would defeat the
security intent of the configuration):

```
crypto backend pkcs11 not compiled into this binary; rebuild with -tags pkcs11
```

The resolver **never panics** and **never silently downgrades**. The typed errors
it returns:

| Error | Cause |
|-------|-------|
| `BackendNotCompiledError` | Backend configured but its build tag is absent (e.g. `pkcs11` without `-tags pkcs11`). Carries the required `-tags` value. |
| `UnknownBackendError` | Backend value is neither `auto`/`""` nor a recognized go-xkms type (typically a typo). Also caught at config-validation time as a typed `ValidationError`. |
| `BackendConstructError` | The go-xkms constructor failed (e.g. unreachable device/endpoint). Wraps the underlying cause. |
| `StoreTypeUnsupportedError` | A non-`software`/`tpm2`/`pkcs11` backend was selected for the CA signing-key store. |
| `AEADUnsupportedError` | A non-`software`/`tpm2` backend was selected for `barrier_aead`. |

This silent-downgrade fix was the original motivation for the central resolver:
before it, `startKMS` hardcoded the software backend and ignored a configured
`kms.backend: tpm2`. See
[Crypto Backend Selection — design](../security/crypto-backend-selection.md).

## Worked example

A node that prefers hardware everywhere, pins the CA signing key into the TPM,
keeps the KMS service on the software backend, and leaves the PIN/barrier
defaults:

```yaml
crypto:
  backend: auto            # TPM2 if present, else software (KMS inherits this)
  key_ref: ""

  subsystems:
    kms:
      backend: software    # override: keep the KMS key universe on software
    ca:
      backend: tpm2        # CA SIGNING key in the TPM; TLS leaf stays software
      key_ref: ""          # TPM persistent-handle label (backend default if "")
    pin:
      backend: ""          # inherits crypto.backend, but PIN never auto-promotes:
                           #   resolves to software unless crypto.backend is an
                           #   explicit tpm2
    barrier_aead:
      backend: ""          # envelope model — stays software (AES-256-GCM)
```

With this config on a host **without** a TPM, every `auto` resolves to software
and the result is identical to a deployment with no `crypto:` block. On a host
**with** a provisioned TPM, the CA signing key lands in the TPM while the TLS
leaf, KMS keys, PIN, and bulk barrier AEAD stay software.

## Relationship to the seal strategy

`crypto:` selects **where keys are stored**. The **root-key seal** — how the
barrier's root key is protected at rest — is a separate axis configured by
`seal.strategy` (`none` / `passphrase` / `shamir` / `tpm2` / `pkcs11` / cloud /
`auto`). The same `BackendResolver` hardware probe backs `seal.strategy: auto`.
The two are orthogonal: a TPM-sealed root key (`seal.strategy: tpm2`) can coexist
with software-stored KMS keys, and vice versa. See
[Seal/Unseal](../security/seal.md) and [Barrier](../architecture/barrier.md).

The data-plane `barrier_aead` deliberately stays software even under `tpm2`: the
hardware boundary is the **root-key seal**, while the per-epoch bulk AEAD remains
software AES-256-GCM for throughput (the envelope-encryption model).

## FIPS interaction

Under `GOFIPS140=1` (Go 1.24+ native FIPS mode) the `software` backend uses
FIPS-validated Go stdlib primitives. TPM/HSM operations are outside the Go FIPS
boundary and rely on the device's own validation. The `quantum` backend's
ML-DSA/ML-KEM (cloudflare/circl) are **not** FIPS-140 validated — prefer an
explicit backend choice under FIPS. See [FIPS 140-3](../security/fips.md).

## See also

- [Crypto Backend Selection — design](../security/crypto-backend-selection.md) — the implementation design of record.
- [KMS Configuration](../kms/configuration.md) — `crypto.subsystems.kms` vs the legacy `kms.backends` router.
- [Seal/Unseal](../security/seal.md) — root-key seal strategies, including `auto`.
- [Barrier](../architecture/barrier.md) — the two-barrier model and the envelope boundary.
- [Security Overview](../security/README.md).
