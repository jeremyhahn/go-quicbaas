# First-Boot Bootstrap

QuicBaaS uses passwordless authentication (FIDO2/WebAuthn and PIV smartcard)
by default. On a fresh installation there are no admin users and no invite
flow can be started because the invite flow itself requires an existing admin.

The first-boot bootstrap mechanism resolves this catch-22 automatically.

## How It Works

When the daemon starts with `auth.allow_password_auth: false` and
`auth.insecure_dev_mode: false` (the production defaults) it checks the admin
assignment store. If no active admin assignment exists — which is the case on
a clean data directory — it:

1. Creates a user `admin` (email `admin@quicbaas.local` by default) in the
   `default` tenant with state `pending`.
2. Grants that user a `super_admin` assignment so they can issue further invites
   after completing activation.
3. Generates a one-time invite token (24-hour TTL).
4. Prints an activation banner to **stderr** (not to the structured log stream)
   so it is always visible regardless of log level.

The banner looks like:

```
============================================================
  FIRST-BOOT BOOTSTRAP: open the URL below to register your
  admin credential. This invite is shown ONCE.

    https://<server>/ui/#/activate?token=<TOKEN>

  After activation, this message will not appear again.
============================================================
```

The operator opens the URL, registers a FIDO2 credential or PIV certificate,
and the account transitions from `pending` to `active`.

On every subsequent boot the admin assignment already exists, the check returns
immediately, and no banner is printed.

## Customising the Admin Username

Set `auth.bootstrap_admin_username` (config) or the `--bootstrap-admin-username`
flag (CLI) to override the default email address used for the bootstrap user.
The value must be a valid RFC 5322 email address.

```yaml
auth:
  bootstrap_admin_username: ops@example.com
```

## Mutual Exclusion with Password Auth

The first-boot invite path only runs when **both** of the following are false:

- `auth.allow_password_auth`
- `auth.insecure_dev_mode`

When either is true the legacy password bootstrap runs instead (creating a user
with a username/password), and no invite banner is printed. The two paths are
mutually exclusive — the system will always have exactly one admin after the
first boot regardless of which path runs.

## Security Notes

- The invite token is a 32-byte cryptographically random value encoded as hex.
- It is hashed (SHA-256) in storage; the plaintext is shown exactly once.
- The token expires after 24 hours. If it expires before activation, restart
  the daemon against an empty data directory (or delete all admin assignments)
  to trigger the first-boot flow again.
- The activation URL is printed to stderr, not to structured logs, to prevent
  it from being captured by log aggregation pipelines where it could be exposed
  to more principals than intended.
