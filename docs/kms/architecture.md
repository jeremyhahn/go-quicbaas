# KMS — Architecture

> **Status: NORMATIVE** · Last verified: 2026-10-15
> How QuicBaaS reaches the go-xkms KMS. Feature folder: `docs/kms/`.

## Owner

Key management, the crypto operations on those keys and every backend a key
can live in (software, TPM 2.0, PKCS#11, AWS KMS, GCP KMS, Azure Key Vault,
HashiCorp Vault, post-quantum) belong to go-xkms. Their design is documented
there:

- [Key Management — Architecture](../../../go-xkms/docs/key-management/architecture.md):
  the composite keystore, key identifiers, backend registry and data flow
- [Crypto Backends — Architecture](../../../go-xkms/docs/crypto-backends/architecture.md):
  the `Backend` contract and each backend's declared capabilities
- [Symmetric Encryption & AEAD — Architecture](../../../go-xkms/docs/symmetric-aead/architecture.md):
  symmetric keys, cipher modes and AEAD nonce and byte limits
- [Signing — Architecture](../../../go-xkms/docs/signing/architecture.md)

QuicBaaS re-exports the KMS through `sdk/go` on every transport
([SPEC](../SPEC.md) row `kms`).

## What lives in this repository

| Surface | SDK accessor | Route family | CLI group |
|---|---|---|---|
| Keys | `Security().KMS()` | `/api/v1/kms` | `kms` |
| Crypto operations | `Security().Crypto()` | `/api/v1/crypto` | `crypto` |
| Data encryption keys | `Security().DEK()` | see [`../data-encryption-keys/`](../data-encryption-keys/architecture.md) | `dek` |

The transport mapping and permissions are in [api.md](api.md), the verbs in
[cli.md](cli.md), the configuration keys in [engineering.md](engineering.md).

**Being removed.** `Security().KMS()` and `Security().Crypto()` are served today
by QuicBaaS's own KMS: a key store and key cache (`pkg/kms`), a backend router
and registry with failover and health checks (`pkg/kms/router.go`,
`pkg/kms/registry.go`), key versions and rotation, per-key policies and AEAD
usage tracking (`pkg/service/kms_*.go`). That is a parallel implementation,
removed on the owner's word ([SPEC](../SPEC.md) row `kms`). The replacement
re-exports the go-xkms key and crypto services under go-xkms's own names and
types. Until then this folder records how the code that ships is reached; it
does not document that implementation's internals as the product.

## Component boundaries

- **go-xkms** — keys, algorithms, backends, AEAD safety limits, signing. Its
  backends are built by QuicBaaS's `BackendResolver` from the `crypto:` block
  ([`../composition/`](../composition/engineering.md)) and consumed through
  `go-xkms/sdk/go`.
- **This repository** — the `Security()` accessors, the route families and
  CLI groups above, the `kms:` configuration block, and the build tags that
  select which go-xkms backends are compiled in.
- **The barrier** — key material rests under it, so a sealed server cannot
  serve keys ([`../barrier/`](../barrier/architecture.md)).
- **Data encryption keys** — the DEK hierarchy and its CLI are a separate
  feature ([`../data-encryption-keys/`](../data-encryption-keys/architecture.md)).

## Trust boundaries

- **Permissions are per verb.** Reading key metadata (`keys:get`) is separate
  from creating, importing or rotating a key (`keys:create`) and from deleting
  or revoking one (`keys:delete`); the full table is in [api.md](api.md).
- **Tenant scope comes from the request context.** The service reads the
  tenant from the context the transport set and stores every key under that
  tenant's prefix; a path in a key name (`prod/api-key`) is only a name.
- **Per-key policies** (`SetKeyPolicy` / `GetKeyPolicy`) are a check inside the
  KMS service being removed, not a go-qrdb authorization decision. They go with
  it.

## Build tags

Which go-xkms backends are compiled in is a build decision; what each backend
does is go-xkms's ([Build system](../../../go-xkms/docs/configuration/build-system.md),
[Backends](../../../go-xkms/docs/backends/README.md)). QuicBaaS maps each tag
to a Makefile variable:

| Tag | Backend | Makefile variable |
|-----|---------|-------------------|
| (none) | software, TPM 2.0 | — |
| `awskms` | AWS KMS | `WITH_AWS=1` |
| `gcpkms` | GCP KMS | `WITH_GCP=1` |
| `azurekv` | Azure Key Vault | `WITH_AZURE=1` |
| `vault` | HashiCorp Vault | `WITH_VAULT=1` |
| `pkcs11` | PKCS#11 HSM (needs CGO) | `WITH_PKCS11=1` |
| `quantum` | post-quantum extras | `WITH_QUANTUM=1` |

```bash
make build WITH_AWS=1 WITH_VAULT=1
make build-fips WITH_AWS=1
```

A backend configured but not compiled in fails daemon start with a typed
`BackendNotCompiledError`; it is never downgraded to software
([Crypto backend selection](../composition/engineering.md)).

## Cryptoperiod Enforcement (PCI DSS 3.6)

**Being removed** with the KMS key store ([SPEC](../SPEC.md) row `kms`). The
`kms.rotation` system job (`handlers.KMSRotationJob`, see
[System jobs](../scheduler/architecture.md#system-jobs)) runs every
`ServiceConfig.KMSRotationScanInterval` (default five minutes) and rotates each key whose
rotation policy is enabled, set to auto-rotate and past its next-rotation time.
It runs once per firing on a cluster. Key rotation and versions are go-xkms's:
see [Key Management — User Guide](../../../go-xkms/docs/key-management/user.md).

## Removed: application-level envelope DEK

Agent session logs and agent session state are written as plaintext JSON into
qrdb; at-rest confidentiality comes from qrdb's `StorageBarrier`. The former
per-tenant `security.TenantDEKManager` envelope was removed in the Phase 3
security hardening. Tenant crypto-shred is a dedicated per-tenant qrdb barrier
sealed by a go-xkms strategy ([`../barrier/`](../barrier/architecture.md)).
