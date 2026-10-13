# Credentials — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag, with runnable examples. Feature folder: `docs/credentials/`.

There is no `quicbaas credentials` command. Certificates are issued through
`quicbaas ca` (see [certificate-authority/cli.md](../certificate-authority/cli.md)):

| Task | Command |
|------|---------|
| Issue a certificate with a generated key | `quicbaas ca issue --common-name <cn> [--dns …] [--ip …] [--usage client\|server\|both]` |
| Get the CA certificate or bundle | `quicbaas ca info`, `quicbaas ca bundle` |
| List issued certificates | `quicbaas ca list [--include-revoked] [--limit N]` |
| Revoke a certificate | `quicbaas ca revoke <serial>` |

Signing an external CSR and verifying a certificate have no CLI verb.

## quicbaas credential

Post-init credential operations: submit, list, rotate, retrieve,
versions, delete, gc.

### Overview

The `credential` verb tree manages QuicBaaS's named credential
service. All seven verbs delegate to `Security().Credentials()` over
the active SDK transport — every transport (REST, gRPC, QUIC, MCP,
Unix, embedded) ships a real-wire implementation. There is no
`PendingCredentialBackendError` or `NotImplementedError` anywhere on
this path.

Source: [`cmd/quicbaas/credential.go`](../../cmd/quicbaas/credential.go).

For SDK-level docs (versioned-rotation semantics, grace period,
storage layout) see
[`docs/sdk/credentials.md`](engineering.md).

### Verb summary

| Verb | Versioned? | Purpose |
|------|------------|---------|
| `submit <name> --value <v>` | No | Non-versioned set/replace |
| `list` | n/a | Enumerate credential names (values never returned) |
| `rotate <name> --value <v> [--grace <dur>]` | Yes | Atomic versioned rotation |
| `retrieve <name> [--version N]` | Yes | Read current or specific version |
| `versions <name>` | Yes | Show metadata for current + non-expired grace versions |
| `delete <name>` | n/a | Wipe credential + all historical versions |
| `gc <name>` | Yes | Reap expired grace versions |

### submit

Submit a credential value under the given name. Non-versioned: replaces
the existing value (if any) immediately. The manual strategy notifies
any blocked retrievers; the barrier strategy persists the value via
the StorageBarrier.

```bash
quicbaas credential submit my-key --value s3cr3t
```

| Flag | Default | Description |
|------|---------|-------------|
| `--value <string>` | (required) | Credential value |

Output:

```
credential "my-key" submitted
```

For a versioned rotation that keeps the previous value retrievable,
use `rotate`.

### list

Enumerate every stored credential by name. Values are never returned.

```bash
quicbaas credential list
quicbaas credential list --output json
```

| Flag | Default | Description |
|------|---------|-------------|
| `--output <format>` | `table` | `json` or `table` |

Table output: one name per line, or `(no credentials)` for an empty
store.

Backends that cannot enumerate (future HSM-only strategies) return
an enumeration-not-supported error.

### rotate

Versioned rotation: atomically replace the current value of `<name>`
with `--value`. By default the previous value is wiped immediately.
Use `--grace` to keep it retrievable for a grace window before
automatic reaping.

```bash
quicbaas credential rotate my-key --value newSecret --grace 24h
```

| Flag | Default | Description |
|------|---------|-------------|
| `--value <string>` | (required) | New credential value |
| `--grace <duration>` | `0` (wipe immediately) | Grace period during which the previous version remains retrievable. Accepts Go duration strings (`30s`, `5m`, `24h`, `168h`). |
| `--output <format>` | `table` | `json` or `table` |

Output (table):

```
rotated my-key: previous v1 -> new v2 (previous version retrievable until 2026-05-26T14:32:11Z)
```

When `--grace 0` (or omitted) the parenthetical clause is dropped.

### retrieve

Read a credential value. Defaults to the current version; pass
`--version N` to read a specific historical version (must still be
within its grace window).

```bash
quicbaas credential retrieve my-key             # current value
quicbaas credential retrieve my-key --version 2 # historical v2
```

| Flag | Default | Description |
|------|---------|-------------|
| `--version <uint>` | `0` (current) | Specific version to retrieve |

Output: the raw value on its own line (so the CLI can be used in
pipelines: `quicbaas credential retrieve db-password | mysql -p...`).
Use the JSON output of `rotate` / `versions` for metadata.

### versions

Show every non-expired version of `<name>`. Output is metadata only —
no values.

```bash
quicbaas credential versions my-key
```

Table output:

```
VERSION    CURRENT   CREATED                   GRACE-UNTIL
2          yes       2026-05-25T14:32:11Z      -
1          no        2026-05-24T09:14:22Z      2026-05-26T14:32:11Z
```

Use `quicbaas credential retrieve --version N` to read a specific
value.

### delete

Delete a credential and all of its non-current historical versions.

```bash
quicbaas credential delete my-key
```

Idempotent — deleting a non-existent name is a no-op.

Output:

```
credential "my-key" deleted
```

### gc

Reap historical versions whose grace period has elapsed. Backends
also reap opportunistically during `retrieve` and `versions` traffic;
explicit `gc` exists so operators can force the wipe ahead of an
audit and confirm the count.

```bash
quicbaas credential gc my-key
```

Output:

```
reaped 2 expired version(s)
```

### Error types

| Type | When |
|------|------|
| `CredentialNameRequiredError` | Verb invoked without `<name>` |
| `CredentialValueRequiredError` | `submit` / `rotate` invoked without `--value` |
| (from qrdb) `CredentialNotFoundError` | `retrieve` / `delete` against an unknown name |
| (from qrdb) `CredentialVersionExpiredError` | `retrieve --version N` where N has expired |
| (from qrdb) `CredentialGraceExceededError` | `rotate --grace` above the server-side cap |

### Common patterns

#### Provision a service password

```bash
PASSWORD=$(openssl rand -base64 24)
quicbaas credential submit db-password --value "$PASSWORD"
```

#### Rolling rotation with grace

```bash
NEW_PASSWORD=$(openssl rand -base64 24)
quicbaas credential rotate db-password --value "$NEW_PASSWORD" --grace 24h

# ... later, after workers have drained the old value ...
quicbaas credential gc db-password
```

#### Audit current state

```bash
quicbaas credential list
quicbaas credential versions db-password --output json
```

#### Programmatic pipeline

```bash
PASS=$(quicbaas credential retrieve db-password)
echo "$PASS" | mysql -h db.internal -u service -p
```

### Transport coverage

All seven verbs work on every transport. See the matrix in
[`docs/sdk/credentials.md`](engineering.md#transport-coverage).

### See also

- [`docs/sdk/credentials.md`](engineering.md) — full SDK
  contract, including version metadata, storage layout, and the
  versioned-rotation lifecycle diagram.
- [`docs/sdk/security.md`](../ssh/engineering.md) —
  `Security().Credentials()` accessor.
- [`docs/credentials/README.md`](user.md) — the
  broader credentials engine (CA, database creds, dynamic secrets).
