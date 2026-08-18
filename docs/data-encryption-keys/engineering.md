# Data Encryption Keys — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract for the DEK surface. Feature folder:
> `docs/data-encryption-keys/`.

## Exported surface

The SDK service is `quicbaas.DEK` (`sdk/go/client/dek`), reached through the
composed client. Eleven methods:

| Method | Purpose |
|---|---|
| `CreateDEK` | Create a data encryption key at a path |
| `GetDEK` | Read a DEK's metadata |
| `ListDEKs` | Enumerate DEKs |
| `DeleteDEK` | Remove a DEK |
| `RotateDEK` | Generate a new version of a DEK |
| `GetDEKStatus` | Report a DEK's state |
| `SetDefaultDEK` | Nominate the DEK used when none is named |
| `SetDEKSafety` | Engage the safety interlock |
| `ClearDEKSafety` | Release the safety interlock |
| `StartReencryption` | Begin re-encrypting data under a new DEK version |
| `GetReencryptionProgress` | Poll that operation |

A DEK is addressed by **path**, not by id. Every CLI verb but `list` takes a
`<path>` positional (`cmd/quicbaas/dek.go:180-717`), and the REST routes are
mounted as a wildcard subtree under `/api/v1/kms/deks/*` for the same reason.

## The safety interlock

`SetDEKSafety` / `ClearDEKSafety` exist because the destructive operations in
this feature are irreversible in a way that ordinary confirmation prompts do not
adequately guard: deleting or rotating away a DEK makes data encrypted under it
unreadable, and there is no undo.

The interlock is state on the server, not a CLI flag. That is deliberate — a
flag protects against a typo in one shell, while server state protects against
every path to the operation, including the API. Treat clearing the safety as its
own audited action, not a step in a longer procedure.

## Re-encryption is asynchronous

`StartReencryption` returns as soon as the operation is accepted;
`GetReencryptionProgress` is how you learn whether it finished. Two consequences
for anything automating this:

- **Do not delete the old DEK version on the return of `StartReencryption`.**
  It has not re-encrypted anything yet.
- **A progress poll that stops early leaves data under mixed versions.** That is
  a legal state — the reader resolves the version per record — but it means the
  old version cannot be retired.

## Persistence and key layout

DEKs live under the barrier. A sealed barrier means DEKs are unreadable, which
means data encrypted under them is unreadable: that chain is the point. See
[`../barrier/architecture.md`](../barrier/architecture.md).

The wrapping key is the KMS's; see [`../kms/`](../kms/architecture.md). A DEK is
never stored in the clear.

## Error semantics and fail-closed paths

- A DEK operation against a **sealed** barrier fails; it does not queue and does
  not degrade to an unencrypted path.
- A destructive operation with the safety interlock **engaged** is refused. The
  refusal is a distinct error from a permission denial — do not treat them
  alike, because the remedy is different.
- Re-encryption failure leaves records under their existing version. It does not
  half-write a record.

## Configuration

DEK behaviour is configured under the KMS block; there is no separate `dek:`
section. See [`../kms/engineering.md`](../kms/engineering.md). Like most blocks
in this repository, it is **config-file only** — `pkg/config/loader.go` reads
one explicit list of environment variables and no DEK key appears in it.

## Test strategy

- Unit tests cover the safety interlock's refusal paths and the path-addressing
  rules.
- The re-encryption progress path needs an integration test with a real barrier;
  a unit test cannot exercise the asynchronous handoff meaningfully.
- Any test that deletes a DEK must assert that data encrypted under it becomes
  unreadable. A test that only asserts the delete returned success proves
  nothing about the property the feature exists for.

## Observability

DEK operations are auditable events — creation, rotation, deletion and safety
changes in particular. See [`../audit-logging/`](../audit-logging/user.md).
