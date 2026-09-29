# Bootstrap — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/bootstrap/`.

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
`quicbaas auth login --method mtls` resolves it), writes the issued material to
`<data-dir>/admin/`, prints a banner to **stderr** — not the structured log
stream — so it is visible regardless of log level, and **last of all** grants the
`super_admin` assignment.

The ordering is load-bearing. The grant is what the next boot reads as
"bootstrap completed", so every step it vouches for runs before it is written: a
bootstrap that fails anywhere leaves no grant behind, and the next boot re-enters
the (idempotent) sequence instead of starting a server whose administrator never
received a credential.

On every subsequent boot that check finds a usable system administrator, returns
immediately, and no banner is printed.

## The Daemon Refuses to Start Without an Administrator

A failed first-boot is **fatal**, whatever the cause. The daemon returns a
`*NoAdministratorError` from IAM initialization and startup aborts before any
listener comes up.

It used to log

```
First-boot bootstrap failed — no admin credential will be created this boot
```

at WARN and carry on serving. The result was a live node answering
`/health/ready` with 200 while no admin user, no admin assignment and no
certificate existed — readiness knows only about raft, so it had nothing to say
about administrability — and the warning itself was invisible at `--log-level
error`. The cause does not grade the severity: a missing config line and corrupt
storage leave the same hole, and neither is fixable from inside a process that
has already started serving.

The error names the cause and the remedy, and the cause is reachable with
`errors.Is` (for example `ErrNoSPIFFETrustDomain`) as well as printed:

```
server refuses to start: first-boot created no administrator, so nobody can
administer this server. cause: first-boot bootstrap: issue admin certificate:
first-boot: iam.spiffe_trust_domain is required to issue the admin certificate
(example: iam.spiffe_trust_domain: quicbaas.example.test). Fix the cause and
start again — first-boot has not completed and will resume from where it stopped.
```

Restarting resumes; it does not corrupt. Because the grant is written last,
there is nothing half-granted to clean up.

The one cause that is **not** resumable is an undelivered admin bundle, and the
message withholds the closing reassurance for it alone: that cause carries its
own terminal remedy (wipe and re-bootstrap — see below), and telling the operator
in the same sentence that a restart would resume sends them round a loop that
refuses identically every time.

### A count is not an administrator

"An administrator exists" is not "the assignment listing is non-empty". The
check requires a grant that is `super_admin`, **system-scoped** (no tenant ids —
the shape first-boot creates), and held by a user who still exists and may
authenticate. An orphaned grant whose user was deleted, a tenant-scoped
administrator, or a suspended account all satisfy a count while leaving nobody
able to administer the platform, and first-boot re-enters rather than skipping on
any of them.

A lookup that cannot answer is **not** read as absence: it is reported, and the
boot fails closed.

### A re-entry that cannot deliver a credential refuses

qrdb's first-boot is idempotent and issues no certificate once the admin user
exists. So a re-entry on a CA-backed deployment has no bundle to write, and the
operator would be left without the private key for a certificate already bound to
them. That case fails with `AdminBundleNotDeliveredError` rather than completing:
reissue-on-re-entry is not implemented, so the remedy is to wipe and
re-bootstrap. A CA-less deployment is unaffected — there was never a certificate
to deliver, and that is its completed state.

### How the refusal is tested

Each guarantee above has a unit test that fails when the guarantee is
reverted. They run against the fresh-install fixture in
`pkg/server/first_boot_fresh_install_test.go`, which comes in two shapes:

- **`newFreshInstallNode`** — a real embedded qrdb node on which no first-boot
  has run, driven through `initializeIAMComponents`, the function production
  calls.
- **`newFreshInstall`** — in-memory IAM components whose user and role writes
  pass through a fault injector and a call recorder. This lets a test stop
  qrdb's own first-boot part-way through.

| Guarantee | Reverting it fails |
|---|---|
| A failed first-boot refuses the boot (`*NoAdministratorError`) | `TestInitializeIAMComponents_RefusesWhenFirstBootFailsOnAFreshInstall` |
| A failure inside qrdb's first-boot resumes through the role-name resolver (`RoleAdapter.GetRoleByName`) | `TestFirstBootBootstrap_ResumesAfterAFailureInsideQRDBFirstBoot` |
| A CA-backed re-entry with no reissued certificate refuses (`AdminBundleNotDeliveredError`) | `TestFirstBootBootstrap_ReEntryWithoutAReissuedCertRefuses` |

The `startService` tests stub the IAM seam. They prove the caller refuses a
`*NoAdministratorError`, not that one is produced; producing one is the first
row's job.

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

Without a trust domain the certificate would carry no SPIFFE SAN, login would
fall back to home-realm discovery, and a freshly initialized node has no HRD
routing entries — so `POST /api/v1/auth/login/piv` would fail closed with
`401 login tenant could not be resolved` even with an explicit `X-Tenant` header.

First-boot therefore **fails closed** (`ErrNoSPIFFETrustDomain`) rather than
issuing a credential nobody can use, and since that failure is fatal the daemon
refuses to start: a CA-backed deployment with no `iam.spiffe_trust_domain` does
not boot. Existing deployments are unaffected — a node that already has an
administrator never re-enters first-boot.

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
