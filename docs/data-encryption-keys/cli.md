# Data Encryption Keys — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag, with runnable examples. Feature folder: `docs/data-encryption-keys/`.

Data Encryption Key (DEK) management against the active QuicBaaS daemon.

## Overview

DEKs provide envelope encryption with hierarchical, path-based namespacing and
multi-tenant isolation: data is encrypted with a DEK, and the DEK itself is
wrapped by a KMS master key.

The `dek` verb tree wires directly to `Security().DEK()` over the active SDK
transport (REST / gRPC / QUIC / MCP / Unix / embedded). The verb set mirrors
the SDK client interface (`sdk/go/client/dek`) one-for-one — eleven methods,
no invented verbs.

Source: [`cmd/quicbaas/dek.go`](../../cmd/quicbaas/dek.go).

> **Relationship to `quicbaas kms dek`.** `kms dek` used to be a separate
> eight-verb subtree backed by a gRPC-pinned client. That fork is gone:
> `kms dek` is now a deprecated alias that mounts this exact tree, so it
> inherits all eleven verbs and the SDK transport selection. The one renamed
> verb, `generate`, survives as an alias of `create`. Use `quicbaas dek` for
> new work; the alias will be removed in a future release.

## Verb summary

| Verb | Purpose |
|------|---------|
| `create <path>` | Create a DEK at a path |
| `get <path>` | Show DEK metadata |
| `list` | List DEKs under a path prefix |
| `rotate <path>` | Create a new version of a DEK |
| `delete <path>` | Remove a DEK |
| `status <path>` | Show detailed status including AEAD usage |
| `set-default <path>` | Mark a DEK as the default for its path level |
| `set-safety <path>` | Configure AEAD safety limits |
| `clear-safety <path>` | Remove custom AEAD safety limits |
| `reencrypt <path>` | Start background re-encryption to a new DEK version |
| `reencrypt-progress <path>` | Show re-encryption progress |

## Flags

`dek` accepts `--output <json|yaml|table>` (default `table`) as a persistent
flag. The global flags (`--tenant`, `--transport`, `--address`, `--timeout`,
TLS flags) are inherited from the root command.

## create

```bash
quicbaas dek create /tenants/acme --algorithm AES-256-GCM
quicbaas dek create /tenants/acme/db \
  --description "customer database" \
  --max-bytes 1099511627776 \
  --warning-percent 80
```

| Flag | Meaning |
|------|---------|
| `--algorithm` | Encryption algorithm (e.g. `AES-256-GCM`) |
| `--description` | Human-readable description |
| `--max-operations` | Maximum encrypt operations before rotation is required |
| `--max-bytes` | Maximum bytes encrypted before rotation is required |
| `--warning-percent` | Percent of the safety limit that triggers a warning (0–100) |

## get / status

```bash
quicbaas dek get /tenants/acme
quicbaas dek status /tenants/acme --output yaml
```

`status` adds the AEAD usage counters and, when one is running, the current
re-encryption progress.

## list

```bash
quicbaas dek list
quicbaas dek list --prefix /tenants/ --recursive
quicbaas dek list --limit 25 --output json
```

## rotate

```bash
quicbaas dek rotate /tenants/acme
quicbaas dek rotate /tenants/acme --background
```

Existing data stays readable under the previous version until it is
re-encrypted; use `dek reencrypt` to migrate it.

## delete

```bash
quicbaas dek delete /tenants/acme
quicbaas dek delete /tenants/acme --migrate-to /tenants/acme-v2
quicbaas dek delete /tenants/acme --force
```

The server refuses deletion while data is still encrypted under the DEK unless
`--force` is supplied or `--migrate-to` names a replacement.

## set-default

```bash
quicbaas dek set-default /tenants/acme
```

## set-safety / clear-safety

AEAD safety limits force a DEK to rotate before nonce reuse becomes
statistically possible. At least one of `--max-operations` / `--max-bytes`
must be supplied to `set-safety`.

```bash
quicbaas dek set-safety /tenants/acme --max-operations 4294967296
quicbaas dek set-safety /tenants/acme --max-bytes 1099511627776 --warning-percent 80
quicbaas dek clear-safety /tenants/acme
```

## reencrypt / reencrypt-progress

```bash
quicbaas dek reencrypt /tenants/acme --from-version 1 --to-version 2
quicbaas dek reencrypt /tenants/acme --from-version 1 --to-version 2 --batch-size 500 --force
quicbaas dek reencrypt-progress /tenants/acme --operation-id op_abc123
```

`--from-version` and `--to-version` are required, must be positive, and must
differ.

## Errors

All validation failures are typed:

| Type | Condition |
|------|-----------|
| `DEKArgRequiredError` | The positional `<path>` argument is missing or blank |
| `DEKFlagInvalidError` | A flag carries an out-of-range or contradictory value |
