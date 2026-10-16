# KMS — CLI

> **Status: NORMATIVE** · Last verified: 2026-10-15
> Every verb and flag. Feature folder: `docs/kms/`.

Key management and crypto operations are go-xkms's, and their CLI is
documented there: [Key Management — CLI](../../../go-xkms/docs/key-management/cli.md),
[Symmetric Encryption & AEAD — CLI](../../../go-xkms/docs/symmetric-aead/cli.md),
[Signing — CLI](../../../go-xkms/docs/signing/cli.md). This page lists only the
QuicBaaS verbs, their flags and the SDK calls behind them.

> **Being removed.** `quicbaas kms` and `quicbaas crypto` drive QuicBaaS's own
> key store and crypto service, a parallel implementation removed on the
> owner's word ([SPEC](../SPEC.md) row `kms`). The verbs below are the code as
> it stands until the go-xkms re-export replaces it.

Both groups take the persistent flag `--output` (`json`, `yaml`, `table`;
default `table`). Each verb calls `Security().KMS()` or `Security().Crypto()`
over the configured transport; the route and permission for each call are in
[api.md](api.md).

## `quicbaas kms`

| Verb | Arguments | Flags |
|---|---|---|
| `key generate` | `<key-name>` | `--algorithm` (default `AES-256-GCM`), `--description`, `--exportable` |
| `key get` | `<key-name>` | `--version` (0 = current) |
| `key list` | `[prefix]` | `--limit` (default 100) |
| `key rotate` | `<key-name>` | `--grace-period` (e.g. `24h`), after which the old version is disabled |
| `key delete` | `<key-name>` | `--force` (bypass deletion protection), `--purge` |
| `key import` | `<key-name>` | `--algorithm`, `--material` (base64), `--description` |
| `key enable` | `<key-name>` | `--version` (0 = all versions) |
| `key disable` | `<key-name>` | `--version` (0 = all versions) |
| `key versions` | `<key-name>` | `--limit` (0 = all) |
| `key export` | `<key-name>` | `--wrapping-key`, `--wrapping-algorithm`, `--format` |
| `key unwrap` | `<key-name>` | `--wrapping-key`, `--material` (base64), `--algorithm` |
| `key set-safety` | `<key-name>` | `--max-operations`, `--max-bytes`, `--warning-percent` (default 80) |
| `encrypt` | `<key-name>` | `--data`, `--aad`, `--base64` |
| `decrypt` | `<key-name>` | `--data` (hex, or base64 with `--base64`), `--aad`, `--base64` |
| `backend status` | — | — |
| `backend test` | — | — |
| `safety check` | — | `--threshold` (percent, default 80) |
| `safety report` | — | — |

`quicbaas kms dek` is a deprecated alias of `quicbaas dek`; use
[`quicbaas dek`](../data-encryption-keys/cli.md).

AEAD usage limits (`key set-safety`, `safety check`, `safety report`) are
go-xkms's: [AEAD bytes tracking](../../../go-xkms/docs/configuration/aead-bytes-tracking.md).

```bash
quicbaas kms key generate prod/api-key --algorithm AES-256-GCM
quicbaas kms encrypt prod/api-key --data "hello"
quicbaas kms key rotate prod/api-key --grace-period 24h
```

## `quicbaas crypto`

| Verb | Arguments | Flags |
|---|---|---|
| `encrypt` | `<key-name>` | `--data`, `--context`, `--base64` |
| `decrypt` | `<key-name>` | `--data` (hex, or base64 with `--base64`), `--context`, `--base64` |
| `rewrap` | `<key-name>` | `--data` (hex ciphertext) |
| `sign` | `<key-name>` | `--data`, `--algorithm` (`SHA256`, `SHA384`, `SHA512`; default `SHA256`) |
| `verify` | `<key-name>` | `--data`, `--signature` (base64), `--algorithm` (default `SHA256`) |
| `hash` | — | `--data`, `--algorithm` (default `SHA256`) |
| `random` | — | `--bytes` (default 32), `--format` (`base64`, `hex`; default `base64`) |
| `datakey` | `<key-name>` | `--bits` (128, 192, 256; default 256) |

```bash
quicbaas crypto sign prod/signing-key --data "payload"
quicbaas crypto random --bytes 32 --format hex
```
