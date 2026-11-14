# Data Encryption Keys — Engineering

> **Status: NORMATIVE** · Last verified: 2026-11-14
> Implementation contract for the DEK surface. Feature folder:
> `docs/data-encryption-keys/`.

## Exported surface

Two DEK surfaces reach users through `sdk/go`:

- **go-qrdb's per-tenant keyring** — the DEK service. go-qrdb's
  `transport.DEKClient` is re-exported as `quicbaas.QRDBDEKClient` (a type
  alias, with its `QRDBTenantDEK*` request and response types), reached by
  `Security().TenantDEK()` on every transport. On the embedded transport the
  node's own keyring is wired in (`QRDBEmbeddedServicers.DEK`, from
  `Server.GetQRDBDEKServicer`). Status, rotate, retire, encrypt, decrypt and
  rewrap; every call acts on the caller's own tenant. The CLI is
  `quicbaas dek tenant`, and `kms://` secret references resolve through it.
- **QuicBaaS's own path-addressed DEK store** — `quicbaas.DEKClient`
  (`sdk/go/client/dek`), reached by `Security().DEK()`. It is a **parallel
  implementation** of the keyring above and is removed on the owner's word;
  until then the bare name `DEKClient` keeps naming it. Eleven methods:

| Method | Purpose |
|---|---|
| `CreateDEK` | Create a data encryption key at a path |
| `GetDEK` | Read a DEK's metadata |
| `ListDEKs` | Enumerate DEKs |
| `DeleteDEK` | Remove a DEK |
| `RotateDEK` | Generate a new version of a DEK |
| `GetDEKStatus` | Report a DEK's state and AEAD usage |
| `SetDefaultDEK` | Nominate the DEK used when none is named |
| `SetDEKSafety` | Engage the safety interlock (AEAD usage limits) |
| `ClearDEKSafety` | Release the per-DEK limits |
| `StartReencryption` | Begin re-encrypting data under a new DEK version |
| `GetReencryptionProgress` | Poll that operation |

The rest of this page is about the path-addressed store.

A DEK is addressed by **path**, not by id. Every CLI verb but `list` takes a
`<path>` positional (`cmd/quicbaas/dek.go:180-717`), and the REST routes are
mounted as a wildcard subtree under `/api/v1/kms/deks/*` for the same reason.

## The safety interlock

An AEAD key is only safe for a bounded number of encryptions (nonce reuse and
the bytes-per-key bound). `SetDEKSafety` sets that bound for one DEK —
`max_operations`, `max_bytes` and a `warning_percent` — in the server's AEAD
safety tracker (`pkg/service/aead_safety.go`); `ClearDEKSafety` drops the
per-DEK bound back to the server default.

- Each encryption under the DEK counts against the bound. Past the warning
  percentage a warning is logged and counted; the use that reaches the bound
  is **refused** (`AEAD limit exceeded`), and every later use is refused too.
- **Rotation is the remedy, not one of the guarded uses.** `RotateDEK` gives a
  new key version whose usage starts at zero, so the DEK is usable again under
  the same bound. Rotating is never refused by the interlock.
- `GetDEKStatus` reports the usage the interlock counts (`usage_count`,
  `bytes_used`).

The interlock is state on the server, not a CLI flag, so it holds against
every path to the key, the API included.

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
- A use past a DEK's safety bound is refused with an AEAD-limit error. It is a
  distinct error from a permission denial — do not treat them alike, because
  the remedy (rotate the DEK) is different.
- Re-encryption failure leaves records under their existing version. It does not
  half-write a record.

## Configuration

DEK behaviour is configured under the KMS block; there is no separate `dek:`
section. See [`../kms/engineering.md`](../kms/engineering.md). Like most blocks
in this repository, it is **config-file only** — `pkg/config/loader.go` reads
one explicit list of environment variables and no DEK key appears in it.

## Test strategy

- Unit tests cover the safety interlock's refusal paths and the path-addressing
  rules: `pkg/service/kms_dek_interlock_test.go` walks set-safety, refusal at
  the bound, rotation back to zero usage and clear-safety.
- `make integration-test-dek` drives create, get, list, rotate, delete, status,
  set default, set and clear safety (rotating under the bound), start
  re-encryption and progress over REST, gRPC and the unix socket.
- The keyring re-export is proven by `sdk/go/qrdb_dek_reexport_test.go`,
  `sdk/go/qrdb_embedded_dek_test.go` and `cmd/quicbaas/embedded_qrdb_test.go`
  (rotation on a real node's keyring).
- The re-encryption progress path needs an integration test with a real barrier;
  a unit test cannot exercise the asynchronous handoff meaningfully.
- Any test that deletes a DEK must assert that data encrypted under it becomes
  unreadable. A test that only asserts the delete returned success proves
  nothing about the property the feature exists for.

## Observability

DEK operations are auditable events — creation, rotation, deletion and safety
changes in particular. See [`../audit-logging/`](../audit-logging/user.md).
