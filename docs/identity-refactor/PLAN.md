# Identity & Auth Refactor — Formal Plan

Status: **draft, awaiting sign-off** · Owner: in progress · Last updated: 2026-05-06

## 1. Context

The QuicBaaS UI does not enforce authentication on its routes — any caller can
load `#/dashboard`, `#/jobs`, `#/iam`, etc. without a valid session. There is
no self-service registration path; every user today is admin-invited. The
identity surface (sessions, factors, RBAC, login handlers) lives entirely in
`go-quicbaas/pkg/iam`, even though `go-qrdb` already has overlapping
primitives in `go-qrdb/pkg/auth` (`LoginResolver`, `Principal`, `Identity`,
`FactorStore`, `UserLookup`, mTLS middleware, RBAC engine). `go-xkms` is the
hardware-crypto layer (PIV slots, signing keys) but does not yet re-export
auth primitives; its xkmsctl CLI uses `auth_factor_handlers.go` directly.

The result is duplicated logic and an opaque inheritance story: a downstream
service that wants login + session + RBAC must reimplement, even though the
primitives already exist a layer down.

## 2. Goals

1. Every route in the QuicBaaS UI requires a valid session, except `/login`
   and `/invite/:token`. Unauthenticated traffic is redirected to `/login`.
2. A daemon configuration switch enables self-service registration:
   - Hardware-factor only (FIDO2 passkey or PIV smartcard). No passwords.
   - The registering user always creates their own tenant — they cannot
     join an existing tenant. (Joining is admin-invite-only and remains so.)
   - Configurable: admin-approval-required vs. auto-activated.
   - Configurable: email-verification-required (default off).
   - Configurable: default role assigned to the registering user inside
     their newly-created tenant.
3. Layer the auth surface so each project owns one concern and re-exports its
   inheritance to consumers via SDK:
   - **`go-qrdb`** — login, session, factor abstraction, RBAC primitives.
   - **`go-xkms`** — hardware crypto + PIV slots, *re-exporting* go-qrdb's
     auth surface so xkms consumers get login/session for free.
   - **`go-quicbaas`** — IAM domain logic (tenants, policies, OIDC
     federation), *consuming* go-xkms's SDK for both crypto and auth.

## 3. Non-goals

- Password authentication. Hardware factors only.
- Multi-factor combinations (e.g. PIV + WebAuthn). One hardware factor is
  sufficient at registration; users can enroll a second later.
- Migrating existing tenants/users (no install in production yet).
- OIDC federated identity providers (deferred).
- Email-delivery infrastructure. We define the verification *interface* but
  the SMTP/SES wiring is out of scope for this refactor.

## 4. Target architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                       go-quicbaas                                │
│  pkg/iam — tenants, policies, OIDC, audit, IAM admin surface     │
│  pkg/api/rest/iam_login_*  — uses go-xkms SDK for login/session  │
│  cmd/quicbaas/frontend     — UI, SDK consumer                    │
│  sdk/{typescript,go}       — re-exports xkms auth surface         │
└─────────────────────────────────────────────────────────────────┘
                               ▲ inherits
┌─────────────────────────────────────────────────────────────────┐
│                          go-xkms                                 │
│  pkg/hardware             — PIV slots, HSM, signing              │
│  re-exports go-qrdb.auth  — Principal, Session, LoginResolver    │
│  pkg/auth_xkms            — adapters: xkms hardware → auth.Factor │
│  sdk/{typescript,go}      — auth + crypto unified surface        │
└─────────────────────────────────────────────────────────────────┘
                               ▲ inherits
┌─────────────────────────────────────────────────────────────────┐
│                          go-qrdb                                 │
│  pkg/auth                 — login resolver, factors, RBAC        │
│  pkg/auth/session  (NEW)  — session lifecycle, storage, cookies  │
│  pkg/auth/register (NEW)  — self-service registration primitive  │
│  sdk/{typescript,go} (NEW for auth) — AuthClient, SessionClient  │
└─────────────────────────────────────────────────────────────────┘
```

Each layer owns its concern and re-exports its inheritance so a consumer can
import a single SDK and get everything below.

### What lives where

| Concern | Owner | Notes |
|---|---|---|
| `Principal` / `Identity` / `Session` types | go-qrdb | Already partly there |
| `FactorStore`, `UserLookup`, `LoginResolver` | go-qrdb | Already there |
| Session cookie issuance, JWT minting, refresh | go-qrdb (new) | Today in quicbaas/pkg/iam/session.go |
| Auth HTTP middleware | go-qrdb | Already there |
| RBAC engine | go-qrdb | Already there |
| Self-service registration primitive | go-qrdb (new) | `RegistrationRequest` + `Register()` |
| FIDO2 / WebAuthn handlers | go-qrdb (new) | Today in quicbaas |
| PIV / mTLS login handler | go-qrdb (new) | Today in quicbaas |
| Hardware key signing, PIV slot mgmt | go-xkms | Already there |
| `auth.Factor` adapters for xkms hardware | go-xkms (new) | Wires HSM-backed keys into go-qrdb's FactorStore |
| Tenant CRUD | go-quicbaas | Domain concept of multi-tenancy |
| Policy DSL, ABAC, audit | go-quicbaas | Domain |
| OIDC discovery + federation | go-quicbaas | Domain |
| User-management UI, IAM admin surface | go-quicbaas | Domain |

## 5. Phases

### Phase 1 — Frontend route guard + redirect (go-quicbaas only)

**Scope:** Pure UI / quicbaas SDK work. No lower-stack changes.

- `MainApp.svelte`: gate every render branch on a `isAuthenticated` derived
  state. If not authenticated and route ∉ {`login`, `invite-activate`},
  call `routeLogin()`.
- The `quicbaas_token` localStorage value is the auth signal. On load,
  `client.auth.authValidate(token)` confirms the token; on 401/expired,
  clear and redirect.
- Existing `client.auth.logout(token)` (added in the previous session)
  remains the way to drop the session.

**Deliverable:** anyone hitting `/dashboard` without a token is sent to
`/login`. No backend change.

**Rough size:** 1 day.

### Phase 2 — Self-service registration (go-quicbaas only)

**Scope:** New endpoint, config, register page. Stays in quicbaas for now —
will be relocated in Phase 4.

- New config block:
  ```yaml
  iam:
    self_service_registration:
      enabled: false                      # default off
      require_admin_approval: true        # default safe
      require_email_verification: false   # default off
      default_role: viewer                # role inside the user's new tenant
  ```
- New endpoint: `POST /api/v1/auth/register`. Body:
  ```json
  {
    "email": "...",
    "display_name": "...",
    "tenant_name": "Acme Corp",
    "factor": {
      "kind": "webauthn",
      "registration_response": "..."
    }
  }
  ```
  Or `factor.kind = "piv"` + the cert thumbprint after a TLS-renegotiate.
- Server flow:
  1. Validate config flag is enabled.
  2. Create new tenant from `tenant_name` (auto-derive id from name).
  3. Create user, enroll factor, attach `default_role`. The user is also
     granted the built-in `tenant_admin` role on their new tenant so they
     can manage it.
  4. If `require_admin_approval`, mark the user `pending` and return
     a "pending approval" response. Otherwise activate immediately and
     return a session.
  5. If `require_email_verification`, mark the user `unverified` and
     queue a verification email (out-of-band; we just log a stub).
- New UI route `/register` with the matching form, factor enrollment, and
  pending/active-state handling.

**Deliverable:** an unauthenticated visitor can register from the login
screen, get a brand-new tenant, and (config-permitting) immediately start
using the app.

**Open design questions (need a call):** see § 7 below.

**Rough size:** 2-3 days.

### Phase 3 — Push session + auth surface down to go-qrdb

**Scope:** go-qrdb refactor. Pure additive; no deletions until Phase 5.

Move *only* the surface that is generic and reusable:

| From | To | Reason |
|---|---|---|
| `quicbaas/pkg/iam/session.go` | `qrdb/pkg/auth/session.go` (new) | Pure session lifecycle |
| `quicbaas/pkg/iam/session_storage.go` | `qrdb/pkg/auth/session_storage.go` (new) | Storage primitive |
| `quicbaas/pkg/iam/session_types.go` | `qrdb/pkg/auth/session_types.go` (new) | Types |
| `quicbaas/pkg/api/rest/iam_login_piv.go` | `qrdb/pkg/auth/login_piv.go` (new) | mTLS handler is generic |
| `quicbaas/pkg/api/rest/iam_login_webauthn*.go` | `qrdb/pkg/auth/login_webauthn.go` (new) | WebAuthn is generic |
| (new) `Register` primitive | `qrdb/pkg/auth/register.go` (new) | Carve out from quicbaas |

Stays in quicbaas:

- `tenant.go`, `tenant_provisioner.go`, `tenant_dek.go` — tenant lifecycle.
- `policy*.go`, `compiler*.go` — policy DSL.
- `oidc_*.go` — OIDC federation.
- `admin*.go`, `audit*.go` — IAM admin surface.

**SDK additions** (go-qrdb sdk/typescript + sdk/go): new `AuthClient` with
`register`, `loginPIV`, `loginWebAuthn`, `logout`, `validate`, `current`,
plus a new `SessionClient`.

**Deliverable:** go-qrdb exposes the full auth/session/factor surface
through its SDK; existing quicbaas tests still pass against the new home.

**Rough size:** 1 week. Big test surface to validate.

### Phase 4 — go-xkms re-exports + adds hardware crypto layer

**Scope:** go-xkms refactor. Pure additive.

- `go-xkms/sdk/typescript` and `sdk/go`: re-export `go-qrdb`'s `AuthClient`
  and `SessionClient` so xkms consumers see them on `xkms.auth.*` /
  `xkms.session.*`.
- New `pkg/auth_xkms` package: adapters that turn xkms-managed hardware
  factors (PIV slot, HSM key) into `qrdb.auth.Factor` implementations, so
  the go-qrdb `LoginResolver` can authenticate against hardware-backed
  identity without knowing about xkms internals.
- xkmsctl: gain `xkmsctl auth login`, `xkmsctl auth register`, `xkmsctl
  auth session list` etc. — same surface as the SDK.

**Deliverable:** go-xkms's SDK exposes both crypto AND auth, so a
downstream caller imports `@xkms/sdk` and gets everything.

**Rough size:** 3-4 days.

### Phase 5 — go-quicbaas consumes the go-xkms SDK; remove duplicated code

**Scope:** quicbaas refactor. Net negative LOC.

- Replace direct `pkg/iam/session*` usage with calls into go-xkms SDK.
- Remove `iam_login_piv.go`, `iam_login_webauthn*.go` from
  quicbaas/pkg/api/rest — now thin pass-throughs to go-xkms HTTP routes.
- quicbaas frontend SDK becomes a thin re-export of go-xkms TS SDK plus
  the quicbaas-specific surface (orchestrator, projects, policies, etc.).
- Delete the moved code's old home in quicbaas; tests that referenced it
  redirect at the SDK.

**Deliverable:** one source of truth for auth (in go-qrdb), one for crypto
(in go-xkms), and a thin domain layer in go-quicbaas. Same external
behaviour, far less code.

**Rough size:** 4-5 days, gated on Phase 3 + 4 stability.

## 6. Test strategy

- **Phase 1**: route-guard unit tests in Vitest; e2e in Playwright (visit
  every protected route unauthenticated → expect redirect to /login).
- **Phase 2**: handler-level tests for `/api/v1/auth/register` covering
  enabled/disabled, admin-approval on/off, email-verification on/off,
  factor kind webauthn/piv, tenant-name collision (id derive), invalid
  factor; UI tests for the form.
- **Phase 3**: every test in `quicbaas/pkg/iam/session*_test.go` and
  `iam_login_*_test.go` migrates to go-qrdb. Add SDK contract tests
  (TS + Go) that exercise the new surface against an embedded daemon.
- **Phase 4**: add adapter tests for `auth_xkms` against a mock HSM. SDK
  re-export tests confirm `xkms.auth.*` reaches the same handlers.
- **Phase 5**: full regression — quicbaas integration suite must pass
  unchanged. Net coverage should not drop.

## 7. Open design questions (need a call before Phase 2 lands)

These are small but they bind the schema, so I want to confirm once before
locking them in.

1. **Tenant id derivation.** When a user types tenant name "Acme Corp" do we
   derive id `acme-corp` (kebab-case), `acme_corp` (snake), or generate a
   ULID and treat the typed name as label only? My recommendation: ULID id +
   kebab-case slug as a secondary lookup. Avoids name collisions forever.
2. **Tenant ownership.** A self-registered user creates a tenant. They are
   the implicit admin of *their own* tenant. Do they receive (a) only the
   built-in `tenant_admin` role, (b) only the configured `default_role`,
   or (c) both? My recommendation: (c). `tenant_admin` so they can manage
   their tenant; `default_role` so cross-tenant policies still apply
   uniformly. The `default_role` only affects platform-level grants.
3. **Email verification deferred state.** Until verified, can the user log
   in with reduced rights (read-only) or are they blocked entirely? My
   recommendation: blocked entirely. Simpler state machine.
4. **Admin-approval rejection path.** If admin-approval is on and an admin
   rejects the user, are they soft-deleted (factor + tenant kept for 30
   days) or hard-deleted? My recommendation: soft-deleted with TTL, easy to
   re-approve, easy to wipe.
5. **Admin-approval *who* approves.** With self-registration on, every new
   tenant has a single user (the registrant). There's no other admin to
   approve them. So admin-approval-on means "an external operator running
   `quicbaasctl iam users approve`", not "a tenant admin". Confirm this is
   what you want, or whether admin-approval mode should imply that the
   feature is for invited-into-existing-tenant users only — but that
   contradicts the rule that joining requires invite. So admin-approval
   should mean: a daemon-level admin approves new self-registered tenants.
   Confirm?

## 8. Risks

- **Phase 3 is the big one.** Moving session storage between repos changes
  on-disk layout. Need a backup/restore plan even though no production
  installs exist. Mitigation: keep on-disk format identical, only the Go
  package path changes.
- **Cross-repo test orchestration.** Phase 5 requires all three repos to
  have green CI together. Mitigation: pin SDK version in quicbaas's go.mod
  and bump explicitly per phase.
- **Frontend route guard race.** `authValidate` is async; we don't want a
  flash of authenticated content before the validation resolves. Mitigation:
  show a brief "Checking session…" splash until the first validate
  resolves.

## 9. Out of scope

- Password authentication, ever.
- Federated identity providers (Google/Okta) — separate effort.
- Group-based RBAC (we have role-based; group hierarchy is post-refactor).
- Per-tenant theming or branding on the login page.
- Migration tooling for existing data (no production installs).

## 10. Sequencing

I recommend landing in this order; each is independently shippable:

1. Phase 1 (route guard) — value immediately, no architectural risk.
2. Phase 2 (registration in quicbaas) — answers § 7 questions, lets us
   ship the feature before the refactor. Since the schema lives in go-qrdb
   eventually, we'll keep the wire format stable so Phase 3+ doesn't break
   existing clients.
3. Phase 3, 4, 5 in order — each is additive until Phase 5's deletions.

Total elapsed: ~3 weeks calendar time, depending on review cycles.
