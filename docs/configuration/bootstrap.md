# First-Boot Bootstrap

QuicBaaS uses passwordless authentication by default. On a fresh installation
there are no admin users, so the daemon must establish the first administrator
itself. Admin enrollment is **certificate-based** — there are no invite tokens
and no activation URL for the platform admin.

## How It Works

When the daemon starts with `auth.allow_password_auth: false` and
`auth.insecure_dev_mode: false` (the production defaults) it checks the admin
assignment store. If no active admin assignment exists — the case on a clean
data directory — it delegates to qrdb's canonical first-boot, which:

1. Ensures the reserved `system` tenant and the six built-in roles exist.
2. Creates the platform `admin` user with the `Admin@system` grant.
3. Issues the admin's mTLS certificate from the platform xkms CA and binds its
   **canonical SPKI thumbprint** (base64url SHA-256 over SubjectPublicKeyInfo)
   to the user, so the certificate authenticates as that admin.

QuicBaaS then enrolls the certificate as an `mtls` auth factor (so
`quicbaas auth login --method mtls` resolves it), grants a `super_admin`
assignment, writes the issued material to `<data-dir>/admin/`, and prints a
banner to **stderr** — not the structured log stream — so it is visible
regardless of log level.

On every subsequent boot the admin assignment already exists, the check returns
immediately, and no banner is printed.

## Enrollment Paths

Both paths end with the same SPKI thumbprint bound to the admin user. The
difference is only where the private key lives.

### CSR-sign (preferred — the key never reaches the server)

Set `auth.bootstrap_admin_csr_path` to a PEM-encoded PKCS#10 CSR the
administrator generated on their own machine. The platform CA signs it and only
the certificate is written:

```yaml
auth:
  bootstrap_admin_csr_path: /etc/quicbaas/admin.csr
```

```
<data-dir>/admin/admin.crt    # give this to the administrator
```

An empty or unreadable CSR file **fails the bootstrap closed** rather than
silently falling back to generate-and-distribute, which would leave a private
key on a host the operator deliberately kept it off.

### Generate-and-distribute (default)

With no CSR configured, the daemon generates a P-256 keypair and CSR, signs it
through the same CA seam, and writes both artifacts:

```
<data-dir>/admin/admin.crt
<data-dir>/admin/admin.key    # 0600
```

Copy **both** to the administrator's workstation over a secure channel and
delete them from the server.

> The generated keypair is created in-process and signed via the CSR seam rather
> than through the CA's generate-and-issue call. The xkms CA retains generated
> keys in its own keystore and returns only the certificate, which would leave
> the operator holding a credential whose private key they can never obtain.

### Pre-provisioned certificate (out-of-band)

`auth.bootstrap_admin_piv_cert_path` binds an existing certificate instead of
issuing one. It binds **both** thumbprints — the canonical SPKI thumbprint that
qrdb's cert index resolves natively, and the legacy hex SHA-256 of the
certificate DER that the broker-side fallback index resolves — and transitions
the user to `active`. Primary use case: integration clusters that need a
deterministic admin identity.

## Certificate Authority Requirement

Certificate issuance requires a CA. `initializeCA` returns early when
`security.tls_enabled` is false, so a TLS-disabled deployment creates the
tenant, roles and admin **without** a certificate and logs a warning; the admin
then enrolls through the pre-provisioned path or a later ceremony.

`quicbaasd init` therefore turns TLS on for its own run even though it binds no
listener, so the CA — which is file-backed under the data dir — exists for
first-boot to issue from and for the subsequent `quicbaasd start` to load.

## SPIFFE Trust Domain Is Required for Headless Login

Set `iam.spiffe_trust_domain`. It is what makes the issued admin certificate
usable:

```yaml
iam:
  spiffe_trust_domain: quicbaas.example.test
```

The xkms CA stamps a tenant-scoped SPIFFE URI SAN
(`spiffe://<trust-domain>/tenant/system/user/admin`) onto the admin certificate
**only when a trust domain is configured**, and REST login resolves — and pins —
the tenant from exactly that SAN.

With no trust domain the certificate carries no SPIFFE SAN, so login falls back
to home-realm discovery. A freshly initialized node has no HRD routing entries,
so `POST /api/v1/auth/login/piv` fails closed with
`401 login tenant could not be resolved` even with an explicit `X-Tenant` header.
The daemon logs a warning at first boot when this is the case.

`quicbaasd init` propagates this setting from the loaded configuration. It does
**not** synthesize a default: the trust domain is pinned at login, so a
synthesized value would be baked into every issued certificate and would
invalidate all of them the moment a real trust domain was configured.

## Mutual Exclusion with Password Auth

The first-boot certificate path only runs when **both** of the following are
false:

- `auth.allow_password_auth`
- `auth.insecure_dev_mode`

When either is true the legacy password bootstrap runs instead and no
certificate banner is printed. The two paths are mutually exclusive — the
system always has exactly one admin after first boot regardless of which runs.

## Separation of Duties

Under FIPS 140-3 (`GOFIPS140`, detected via `fips.Enabled()`) first-boot runs in
strict least-privilege mode: the bootstrap admin keeps only the canonical
`Admin@system` grant and the all-permission `Owner` role is never seeded.

## Invite Tokens

Invite tokens are **not** part of admin bootstrap. They remain in use for
ordinary user onboarding: `POST /api/v1/iam/{tenant}/users` mints a one-time
token for each user it creates, redeemed at `/api/v1/iam/invites/{token}/...`.
That surface is unrelated to the platform admin established here.

## Security Notes

- The bound identifier is the canonical SPKI thumbprint, so the binding survives
  certificate renewal with the same keypair.
- The admin bundle is written with mode `0600` inside a `0700` directory.
- The banner goes to stderr, not structured logs, so it is not captured by log
  aggregation pipelines where it could reach more principals than intended.
- Prefer the CSR-sign path in production: it is the only path where the admin's
  private key never exists on the server.
