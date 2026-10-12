# Accounts — User Guide

> **Status: NORMATIVE** · Last verified: 2026-10-11
> How to pool provider accounts, cap their spend and read what the work cost, in the order you do it. Feature folder: `docs/accounts/`. Conflicts with this document are defects.

A pooled account is one provider login (a `claude`, `codex` or `opencode`
subscription or key) that the factory spawns agents under. You give a tenant
several, and each spawn takes the one with the most headroom. The design is in
[architecture.md](architecture.md); every flag is in [cli.md](cli.md).

The examples use the tenant `acme` and the server `quicbaas.example.com`.

## 1. Store the credential as a tenant secret

Keep each login in the tenant's secrets store and have the account name it.
Writing the secret later is then the whole rotation.

Write it as a principal of the account's tenant, such as a tenant
administrator. A system administrator's secret write lands in the system
keyspace, where the tenant's account does not look, and the account create is
then refused with "the secret does not exist in this tenant".

```bash
claude setup-token | quicbaas --tenant acme secrets put \
  agents/claude/claude1/oauth_token --from-stdin
quicbaas --tenant acme secrets put agents/codex/reviewer/auth_json \
  --from-file ~/.codex/auth.json
```

Secrets are described in
[../secrets/dynamic-secrets.md](../secrets/dynamic-secrets.md).

## 2. Create the accounts

```bash
quicbaas --tenant acme account create --provider claude --display-name claude1 \
  --credential-secret-ref agents/claude/claude1/oauth_token
quicbaas --tenant acme account create --provider claude --display-name claude2 \
  --credential-secret-ref agents/claude/claude2/oauth_token
quicbaas --tenant acme account create --provider codex --display-name reviewer \
  --credential-secret-ref agents/codex/reviewer/auth_json \
  --credential-secret-file .codex/auth.json
```

- A `claude` token is projected into `CLAUDE_CODE_OAUTH_TOKEN`. A `codex` or
  `opencode` login is a file, so name it with `--credential-secret-file`.
- The secret must resolve when you create the account, or the create is
  refused.
- `--project <id>` scopes an account to one project. Without it the account
  serves every project in the tenant.
- The first account of a scope becomes its default. `--default` takes the
  default from another account.

Check the pool:

```bash
quicbaas --tenant acme account list
quicbaas --tenant acme account get acct_abc123
```

`credential_health` reads `unknown` for a token whose expiry was never
declared. That is expected for a static token.

## 3. Cap each account

Set how many sessions an account may run at once, how it is billed and how
much it may spend:

```bash
quicbaas --tenant acme account update acct_abc123 --max-concurrent-sessions 2
quicbaas --tenant acme account update acct_abc123 \
  --billing subscription --monthly-fee 200
quicbaas --tenant acme account update acct_abc123 \
  --spend-window name=five-hour,size=5h,tokens=2000000 \
  --spend-window name=weekly,size=168h,usd=400
quicbaas --tenant acme account update acct_abc123 \
  --breaker-failures 3 --breaker-window 1h --breaker-cooldown 30m
```

- A spent account drops out of selection until its oldest turn leaves the
  window. A spawn moves on to an account with headroom.
- The breaker takes an account out for the cooldown after that many failures
  in the window.
- `--billing api` bills each turn at list price. A subscription bills its
  monthly fee instead, shared over the tenant's reports. Without `--billing`,
  an account's billed cost is reported as unknown, not zero.

See an account's live state:

```bash
quicbaas --tenant acme account usage acct_abc123
```

## 4. Cap the tenant and its projects

The tenant's own limits cover every turn charged to it. When a window is
spent, the factory defers new work in every project of the tenant until it
frees.

```bash
quicbaas --tenant acme orchestrator spend-policy set \
  --spend-window name=monthly,size=720h,usd=2000
quicbaas --tenant acme orchestrator spend-policy get
```

A project carries the same windows on its own record; see
[../projects/architecture.md](../projects/architecture.md). The tenant is
checked first, then the project.

## 5. Spawn on the pool

```bash
# Let the strategy pick, and allow rotation across the pool
quicbaas --tenant acme orchestrator spawn --backend claude --account-auto

# Start on one account, rotatable only among the listed ones
quicbaas --tenant acme orchestrator spawn --backend claude \
  --account-id acct_a --account-allow acct_a --account-allow acct_b

# Pin one account; never rotated
quicbaas --tenant acme orchestrator spawn --backend claude --account-id acct_a
```

Factory work (the SDLC runner) selects from the pool on its own; see
[../sdlc/architecture.md](../sdlc/architecture.md).

## 6. Rotate

**A credential.** For an account over a tenant secret, write the secret. The
next spawn takes the new value, on the control plane and on every worker host:

```bash
claude setup-token | quicbaas --tenant acme secrets put \
  agents/claude/claude1/oauth_token --from-stdin
```

For an inline account, use `account rotate --credential-file`.

**A running session.** Move it onto another account without losing its
transcript:

```bash
quicbaas --tenant acme orchestrator rotate-account sess_abc123 \
  --reason "seat retired" --hold-outgoing
```

Only a session spawned with `--account-auto` or `--account-allow` can rotate.

## 7. Read what the work cost

```bash
quicbaas --tenant acme orchestrator cost
quicbaas --tenant acme orchestrator cost --project p1
quicbaas --tenant acme orchestrator cost --project p1 --task t1 --job j1
quicbaas --tenant acme orchestrator cost --from 2026-01-01T00:00:00Z
quicbaas --tenant acme orchestrator cost --host gpu-host.internal:9091
```

Each report has two money figures:

- **Equivalent**: the list price of the tokens.
- **Billed**: what was added to the bill, which is zero per turn on a
  subscription.

A dash means the turns were not priced. It does not mean free. A model
missing from the price table is unpriced, and so is a local engine with no
power figure. The price table is configuration; see
[engineering.md](engineering.md#configuration).

## 8. Cost local inference

Local turns are costed from electricity: device watts × seconds at the
tenant's price per kWh.

```bash
quicbaas --tenant acme orchestrator cost-settings set \
  --electricity-price 0.15 --engine-watts openvino=12 --engine-watts ollama=250
quicbaas --tenant acme orchestrator cost-settings get
```

`set` replaces the settings, so name every value you want to keep. `--clear`
returns to the configuration.

## 9. Recover an account

| `status` | Why | What to do |
| --- | --- | --- |
| `error` | The secret could not be read, the provider rejected the login, or an `oauth2` renewal failed. `last_error` says which. | For a referenced account, fix the secret, then run `account update <id> --credential-secret-ref <ref>`; a successful check returns it to `active`. For an inline account, `account rotate`. |
| `limited` | The provider reported a usage limit | Nothing. It rejoins the pool at `available_at`. |
| `revoked` | An operator revoked it | Revocation is final. Create a new account. |

## 10. When work does not start

A task the factory cannot run is either **deferred**, which retries by itself,
or **parked**, which waits for you. Its reason names a fault class:

| Class | Meaning | What to do |
| --- | --- | --- |
| `accounts_spent`, `account_quota_exhausted` | Every account, or the pinned one, is spent | Wait for the window, raise the cap, or add an account |
| `accounts_cooling_down`, `account_limited` | Provider limits are holding the pool | Wait for `available_at`, or add an account |
| `accounts_saturated` | Every account is at its session cap | Wait, or raise `--max-concurrent-sessions` |
| `account_breaker_tripped` | The account failed repeatedly | Wait for the cooldown; check `last_error` |
| `project_spend`, `tenant_spend` (deferral axis) | A project or tenant window is spent | Wait, or raise the window |
| `no_provider_account` | No account for the provider in this scope | Create one (step 2) |
| `account_credential_missing` | The account's secret is gone | Restore the secret (step 9) |
| `account_auth_rejected` | The provider refused the login | Rotate the credential (step 6) |
| `account_refresher_missing` | An `oauth2` account has no token endpoint | Configure the provider; see [engineering.md](engineering.md#configuration) |

Each of these is refunded or requeued rather than charged as a failed
attempt. How admission and attempt
accounting work is in [architecture.md](architecture.md#job-admission).
