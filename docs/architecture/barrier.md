# Barrier (encryption-at-rest) — QuicBaaS

QuicBaaS imports the barrier subsystem from go-qrdb rather than
duplicating it. The canonical reference for the barrier model lives at:

- [go-qrdb/docs/seal/sealed-window.md](../../../go-qrdb/docs/seal/sealed-window.md) — sealed-window endpoint allowlist, REST + gRPC contract, format-version sentinel, pre-host listener boot sequence.

This page documents only the QuicBaaS-specific deltas.

## QuicBaaS-side surface

| Concern | Where it lives | Notes |
|---------|----------------|-------|
| REST routes | `pkg/api/rest/handlers_{barrier,ceremony,bootstrap}.go` | Mirrors go-qrdb's path layout under `/api/v1/{barrier,init,bootstrap}/*`. The legacy `/api/v1/seal/*` paths are kept for one release with `Deprecation:` and `Sunset:` response headers. |
| Service facade | `pkg/service/{barrier,ceremony}_service.go` | Thin wrappers that delegate to go-qrdb's `BarrierClient` / `CeremonyClient`. |
| Strategy adapter | `pkg/barrier/manager.go` | The canonical SDK adapter. Constructed by `pkg/server/lifecycle.go:initBarrier` from the YAML `barrier.*` configuration. Replaces the deleted `defaultSealManager`. |
| CLI verbs | `cmd/quicbaas/{barrier,init,bootstrap}.go` | Mirror `xkmsctl` verb-for-verb. `quicbaas seal …` is retained for one release as a deprecation alias that warns to stderr and forwards to the new path. |
| Daemon CLI | `cmd/quicbaasd/init.go` | Mirrors `xkmsd init` — the `--barrier-strategy`, `--shares`, `--threshold`, `--so name:KIND:path`, `--trusted-ca` flag set is identical. |
| Frontend | `cmd/quicbaas/frontend/src/views/security/{Barrier,Ceremony,Bootstrap}View.svelte` | Strategy-aware UX (disabled / software / shamir / tpm2 / pkcs11 / *kms). Replaces `SealView.svelte`. |
| TS SDK | `sdk/typescript/src/{types,interfaces,rest,factory}.ts` | `Barrier*` / `Ceremony*` / `Bootstrap*` types and interfaces. `Seal*` types remain as deprecation aliases that `console.warn` on first use. |

## YAML configuration

```yaml
barrier:
  enabled: true
  strategy: shamir          # disabled|software|shamir|tpm2|pkcs11|awskms|gcpkms|azurekv|vault
  shamir:
    threshold: 2
    total_shares: 3
  credential_strategy: barrier
```

When `barrier.enabled: false`, the daemon runs without encryption-at-rest
and the sealed-window middleware is not installed.

When `barrier.enabled: true`, the daemon:

1. Builds the seal sub-store (`<data-dir>/seal/`).
2. Writes / validates the format-version sentinel
   (`__sys/barrier_format_version`). Pre-encfs data dirs are rejected
   with `seal.ErrLegacyDataDir`.
3. Constructs `*seal.StorageBarrier` from the configured strategy chain.
4. If sealed at boot, runs the pre-host listener serving only the
   allowlist (`/v1/health`, `/v1/barrier/*`, `/v1/init/*`,
   `/v1/bootstrap/*`).
5. After unseal, opens the data-plane Pebble through the encrypting
   `vfs.FS`, builds the full service graph, and starts the multi-protocol
   server.

## Migration

There is **no in-place upgrade** from a pre-encfs data directory.
Operators must export their data, deploy the new build to a fresh data
directory, initialize the barrier, and re-import. See
[../migrations/seal-to-barrier.md](../migrations/seal-to-barrier.md) for
the operator-facing procedure.

## Verification

The repo ships three CI watchdogs that catch drift between QuicBaaS,
go-qrdb, and go-xkms:

- `make verify-naming` — fails if Seal* identifiers ship outside the
  documented one-release alias surface.
- `make verify-api-shapes` — fails if a go-qrdb canonical
  Barrier/Ceremony/Bootstrap type is missing from the TypeScript SDK.
- `make verify-cli-flags` — diffs the `init` flag set on
  `xkmsctl init` and `quicbaas init`. Advisory by default; runs in
  blocking mode under `STRICT=1`.

`make verify` runs all three.
