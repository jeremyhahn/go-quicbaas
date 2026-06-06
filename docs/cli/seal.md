# quicbaas seal (deprecated)

> **Status:** Deprecated alias. New work should use
> [`quicbaas barrier`](barrier.md). Every `seal` verb prints a
> deprecation warning to stderr and forwards to the equivalent
> `barrier` verb.

The legacy `seal` tree is retained for operator-runbook compatibility
through at least the next major release. The verb and flag surface
matches the original; behaviour matches the canonical `barrier`
surface bit-for-bit because both share the same SDK
`Security().Barrier()` backend.

Source:
[`cmd/quicbaas/seal.go`](../../cmd/quicbaas/seal.go) (the forwarding
shim) and
[`cmd/quicbaas/barrier.go`](../../cmd/quicbaas/barrier.go) (the
canonical implementation).

## Migration table

| Deprecated `seal` verb | Canonical `barrier` verb |
|------------------------|--------------------------|
| `quicbaas seal status` | `quicbaas barrier status` |
| `quicbaas seal init --shares N --threshold M` | `quicbaas barrier init` (single passphrase) or `quicbaas barrier init-shamir --secret <s> --total N --threshold M` |
| `quicbaas seal unseal --share <s>` | `quicbaas barrier unseal --share <s>` (or `--secret` for password-based) |
| `quicbaas seal seal` | `quicbaas barrier seal` |
| `quicbaas seal rekey --shares N --threshold M` | `quicbaas barrier rekey --total N --threshold M` |

The flag rename `--shares` → `--total` keeps `barrier` aligned with
`xkmsctl barrier` so operators have a single consistent surface
across the four canonical binaries (qrdb, xkmsctl, quicbaas,
platformctl).

## Why the rename

The original surface used "seal" for both "the encryption mechanism"
(the seal) and "the operation that locks the mechanism" (sealing).
The unification spec §6 renames the mechanism to "barrier" so the
nouns and verbs are unambiguous:

| Concept | Old name | New name |
|---------|----------|----------|
| The encryption mechanism | "seal" | "barrier" |
| The action of locking the barrier | "seal" | "seal" (verb, on `barrier`) |
| The action of unlocking the barrier | "unseal" | "unseal" |

So the command tree reads `quicbaas barrier seal` / `quicbaas barrier
unseal` — verb on the noun. The old `quicbaas seal seal` /
`quicbaas seal unseal` redundancy was the immediate motivation for
the rename.

## Behavioural notes

- **Recovery keys** (`--recovery-shares` / `--recovery-threshold` on
  the legacy `seal init`): the canonical `barrier` surface does not
  ship a separate recovery-key concept. Disaster recovery uses the
  same Shamir share material; operators retain shares per their
  organisational policy. The legacy flags are still parsed by the
  forwarding shim but emit a deprecation warning indicating the new
  mechanism.
- **Confirmation prompt** on `seal`: forwarded verbatim. Pass
  `--assume-yes` to skip it.
- **JSON output** on `status` / `init-shamir` / `rekey`: identical
  schemas between `seal` and `barrier`.

## Removal timeline

The deprecation warning ships in the current release. The
forwarding shim will be retained through at least one major version
to give operator runbooks time to migrate. There is no scheduled
removal date.

## See also

- [`docs/cli/barrier.md`](barrier.md) — canonical barrier verbs.
- [`docs/architecture/barrier.md`](../architecture/barrier.md) —
  StorageBarrier design.
- [`docs/sdk/security.md`](../sdk/security.md) —
  `Security().Barrier()` SDK accessor.
