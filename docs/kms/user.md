# KMS — User Guide

> **Status: NORMATIVE** · Last verified: 2026-10-15
> Task-oriented usage, in the order tasks are done. Feature folder: `docs/kms/`.

Keys, algorithms, backends, rotation and AEAD safety limits are go-xkms's and
are documented there:
[Key Management — User Guide](../../../go-xkms/docs/key-management/user.md),
[Symmetric Encryption & AEAD — User Guide](../../../go-xkms/docs/symmetric-aead/user.md),
[Backends](../../../go-xkms/docs/backends/README.md). This page covers only
switching the KMS on in QuicBaaS and reaching it through the `quicbaas` CLI.

> **Being removed.** `quicbaas kms` and `quicbaas crypto` drive QuicBaaS's own
> key store, a parallel implementation removed on the owner's word
> ([SPEC](../SPEC.md) row `kms`). The steps below are the code as it stands
> until the go-xkms re-export replaces it.

## 1. Enable the KMS

```yaml
kms:
  enabled: true
  backend: software          # the default backend for new keys
  enabled_backends:          # every backend the server may use
    - software
    - tpm2
  safety:
    usage_warning: 80        # percent of the AEAD limit that raises a warning
    auto_rotate: 95          # percent of the AEAD limit that rotates the key
```

Every `kms:` key is listed in [engineering.md](engineering.md). A backend must
also be compiled in; see [Build tags](architecture.md#build-tags).

## 2. Create and use a key

```bash
quicbaas kms key generate prod/api-key
quicbaas kms encrypt prod/api-key --data "secret value"
quicbaas kms decrypt prod/api-key --data "<ciphertext>"
quicbaas kms key rotate prod/api-key --grace-period 24h
```

Keys are stored under the caller's tenant; the path in a key name is only a
name. Every verb and flag is in [cli.md](cli.md).

## 3. Data encryption keys

DEKs are a separate feature: [`quicbaas dek`](../data-encryption-keys/user.md).
`quicbaas kms dek` is a deprecated alias.

## See also

- [Architecture](architecture.md) — what lives in this repository
- [API](api.md) — transports and permissions
- [Secrets](../secrets/user.md)
- [Security hardening](../security-hardening/user.md)
