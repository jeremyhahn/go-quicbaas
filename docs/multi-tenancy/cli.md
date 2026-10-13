# Multi-Tenancy — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag for tenant lifecycle and per-tenant barrier control.
> Feature folder: `docs/multi-tenancy/`.

Two command groups matter here, and confusing them is the most consequential
mistake available in this feature.

- `quicbaas iam tenant …` — tenant lifecycle (`cmd/quicbaas/iam_tenant.go:41`).
- `quicbaas iam tenant barrier …` — the **tenant's own** encryption barrier
  (`cmd/quicbaas/tenant_barrier.go:108`, registered at `iam_tenant.go:64`).
- `quicbaas barrier …` — the **system** barrier protecting the control plane.
  Different feature, different keys, different blast radius. See
  [`../barrier/cli.md`](../barrier/cli.md).

Sealing a tenant barrier crypto-shreds that tenant's data. Sealing the system
barrier stops the control plane. They are one word apart on the command line.

## `quicbaas iam tenant`

| Verb | Positional | Purpose |
|---|---|---|
| `create <id>` | tenant id | Create a tenant |
| `provision <id>` | tenant id | Create a tenant, its Admin role and its admin in one step |
| `get <id>` | tenant id | Read one tenant |
| `list` | — | List tenants |
| `update <id>` | tenant id | Modify a tenant |
| `delete <id>` | tenant id | Remove a tenant |

Inherited from the `iam` group (`cmd/quicbaas/iam.go:79-80`):

| Flag | Type | Default | Description |
|---|---|---|---|
| `--output` | string | `table` | `json`, `yaml` or `table` |
| `--yes`, `-y` | bool | `false` | Skip confirmation prompts |

Plus the root persistent flags — `--transport`, `--address`, `--tenant`,
`--timeout`, `--tls*` — documented in
[`../multiprotocol-api/cli.md`](../multiprotocol-api/cli.md).

### Examples

```bash
quicbaas iam tenant create acme-corp
quicbaas iam tenant list --output json
quicbaas iam tenant get acme-corp
quicbaas iam tenant delete acme-corp --yes
```

`--tenant` (root, default `system`) selects the tenant a command *operates as*.
The positional `<id>` names the tenant being *acted on*. They are not the same
argument, and an administrative call typically sets neither to the other.

### `provision`

A system admin's one-step onboarding: the tenant, its tenant-scoped Admin
role, and the tenant admin. An empty isolation type means a per-tenant
`barrier` (`create` defaults to `prefix`), but the server accepts only
`--isolation-type prefix` for now and refuses the others with
`ProvisionBarrierIsolationError`; see [user.md](user.md).

| Flag | Default | Description |
|---|---|---|
| `--name` | tenant id | Display name |
| `--isolation-type` | `barrier` | `prefix`, `barrier`, `volume` or `raft`; only `prefix` is accepted for now |
| `--admin-username` | `admin` | Tenant admin login name |
| `--admin-enrollment` | `cert` | `cert` or `invite` |
| `--admin-csr` | — | Admin's PEM CSR file; required for `cert`, refused for `invite` |
| `--admin-cert-out` | — | Write the issued certificate here (mode 0600); `cert` only, otherwise printed |
| `--invite-expiry-hours` | server default | Invite lifetime; `invite` only |

```bash
quicbaas iam tenant provision acme-corp --isolation-type prefix --admin-csr admin.csr --admin-cert-out admin.crt
quicbaas iam tenant provision acme-corp --isolation-type prefix --admin-enrollment invite --admin-username alice
```

In `invite` mode the admin is created pending and the command prints a
single-use invite token once; deliver it over a secure channel. Flag
conflicts are refused locally before any call; the server's validation stays
authoritative.

## `quicbaas iam tenant barrier`

| Verb | Purpose |
|---|---|
| `status` | Report the tenant barrier's state |
| `init` | Create the tenant's barrier under its chosen seal strategy |
| `unseal` | Unseal with the tenant's credential |
| `unseal-share` | Contribute one Shamir share toward a quorum unseal |
| `seal` | Seal the barrier — **crypto-shreds the tenant's data** |
| `officer list` | List the tenant's Shamir officers |
| `officer status` | Report share/quorum status |

### Examples

```bash
quicbaas iam tenant barrier status --tenant acme-corp
printf '%s' "$PP" | quicbaas iam tenant barrier init --tenant acme-corp \
    --strategy passphrase --passphrase-stdin
printf '%s' "$PP" | quicbaas iam tenant barrier unseal --tenant acme-corp --passphrase-stdin
quicbaas iam tenant barrier unseal-share --tenant acme-corp
quicbaas iam tenant barrier officer status --tenant acme-corp
```

`init` and `unseal` take the tenant passphrase on stdin
(`--passphrase-stdin`, preferred), in `QUICBAAS_BARRIER_PASSPHRASE`, or on
argv as `--passphrase`, in that precedence order. Prefer stdin: an argv
passphrase is readable by every process on the host for the life of the
call. See [barrier/cli.md §3a](../barrier/cli.md).

### Two behaviours worth knowing before you need them

- **A tenant on a shared barrier has no barrier to manage.** These calls are
  rejected as **entitlement** violations, not authorization failures
  (`cmd/quicbaas/tenant_barrier.go:120-123`). The distinction is deliberate:
  the caller is permitted, the tenant simply has no such object. Reading it as
  an authz problem sends you to fix permissions that were never wrong.
- **The barrier is the tenant's, not the operator's.** An infrastructure
  operator holding neither the passphrase nor a Shamir quorum cannot unseal it.
  That is the security property; it is also the reason there is no operator
  override to reach for at 3am.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Operation completed. |
| `1` | Rejected — bad arguments, missing entitlement, insufficient permission, unreachable server, or a seal/unseal that did not satisfy the strategy. |
