# Migration: `seal` → `barrier`

QuicBaaS, go-qrdb, and go-xkms have unified on the `barrier` vocabulary
for the encryption-at-rest subsystem. The legacy `seal` surface is
retained for **one release** with deprecation aliases; CI's
`make verify-naming` watches the alias surface for new violations.

This page is the operator-facing migration table.

## Operator paths

| Old | New | Status |
|-----|-----|--------|
| `quicbaas seal status` | `quicbaas barrier status` | alias, warns to stderr |
| `quicbaas seal unseal --share <S>` | `quicbaas barrier unseal --share <S>` | alias |
| `quicbaas seal seal` | `quicbaas barrier seal` | alias |
| `quicbaas seal init-shamir --threshold N --total M` | `quicbaas barrier init-shamir --threshold N --total M` | alias |
| `POST /api/v1/seal/status` | `GET /api/v1/barrier/status` | legacy retained, returns `Deprecation:` and `Sunset:` headers |
| `POST /api/v1/seal/unseal` | `POST /api/v1/barrier/unseal` | legacy retained |
| `POST /api/v1/seal/init` | `POST /api/v1/barrier/initialize` | legacy retained |
| `POST /api/v1/seal/rekey` | `POST /api/v1/barrier/rekey` | legacy retained |

The alias surface is exhaustively listed in
`scripts/verify-naming.sh`. Any new code path that introduces a
`Seal*` identifier outside that list fails CI.

## SDK migration

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

## Daemon configuration

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

## Data migration

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

## Sunset timeline

The deprecation alias surface (legacy `/api/v1/seal/*` paths,
`SealClient` interfaces, `quicbaas seal …` CLI verbs, `seal:` YAML key)
is retained for one release. The current target sunset date is on the
`Sunset:` response header of the deprecated REST paths
(`Sunset: Wed, 01 Jul 2026 00:00:00 GMT`).
