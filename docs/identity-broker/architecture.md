# QuicBaaS Identity Broker

Status: **Design (for review)** · Targets: **FedRAMP Moderate, FedRAMP High, DoD IL4/IL5**

## 1. Thesis

A multi-service platform needs a **central identity broker** that owns tenant→IdP federation,
issues one sender-constrained **platform access token**, and lets every downstream microservice
stop being an OIDC relying party and instead validate one token against one trust anchor.

**QuicBaaS *is* that broker.** Rather than stand up a separate identity service, the broker
function lives in QuicBaaS:

- QuicBaaS already **composes xkms** (TPM2 / PKCS#11 / cloud-KMS) → FIPS 140-2/3,
  hardware/KMS-backed asymmetric signing keys. This is the hard part of Layer 4, already done.
- QuicBaaS already owns **multi-tenant IAM**: per-tenant users, RBAC/ABAC + PEP/PDP,
  per-tenant federation scaffolding, OIDC/OAuth2 issuance, the qrdb barrier (key custody),
  and the unified login ceremony (mTLS | WebAuthn | PIV).
- Downstream services become **pure validators**: one issuer, one JWKS, DPoP-bound,
  scope→RBAC, `tenant_id`→RLS.

Per-tenant Okta (and any OIDC IdP) federation lives in **QuicBaaS's federation layer**.

## 2. Two per-tenant axes (how the deployment models collapse)

The four deployment models — **shared, dedicated, managed, integrated** — are not four code
paths. They are two independent **per-tenant** settings on one multi-tenant core:

| Axis | Values | Where modeled today |
|------|--------|---------------------|
| **Isolation** | `prefix` (shared barrier + namespace) · `barrier` (own root key + DEK) | qrdb `TenantInfo.IsolationType` — **built** |
| **Identity source** | `local` (QuicBaaS is the IdP) · `federated` (tenant brings its IdP, e.g. Okta) | quicbaas `pkg/iam/federation*` — **scaffold** |

| Deployment model | Isolation | Identity source | Notes |
|---|---|---|---|
| **Shared** (multi-tenant SaaS) | `prefix` | `local` or `federated` | Many tenants per deployment; per-tenant config + **global fallback** |
| **Dedicated** | `barrier` | `local` or `federated` | Own crypto isolation (root key/DEK) |
| **Managed** | either | either | An *operational* overlay (operator-run), not a new technical axis |
| **Integrated** | either | `federated` → Okta | QuicBaaS brokers; the broker token is the platform token |

A single QuicBaaS deployment can host shared `local` tenants, a `dedicated` tenant, and an
Okta-`integrated` tenant **simultaneously**. "Shared" is a hosting *mode*, not a tenant — so
the **system tenant stays control-plane-only** and per-tenant config falls back to the
**global/server default**, never to "the system tenant as a tenant" (see §6).

## 3. Five-layer zero-trust model mapped to the stack

Each layer's owner in our stack:

| Layer | Mechanism | Owner |
|------|-----------|-------|
| **0 Boundary** (SC-7) | Private ingress; downstream services reachable only from QuicBaaS SG; no public path | Deployment (GovCloud SG/ALB) |
| **1 Channel** (SC-8/13, IA-9) | mTLS, FIPS-validated; client-cert SAN = service identity | qrdb mTLS + xkms TrustedCA / DoD PKI / AWS Private CA |
| **2 User identity** (IA-2, AC-3) | Platform access token: single issuer/JWKS, `aud=api://<svc>`, `scp`, `tenant_id` | **QuicBaaS broker** (this doc) |
| **3 Sender-constraint** | DPoP (RFC 9449) now; cert-bound (RFC 8705) later | **QuicBaaS** issues `cnf.jkt`; downstream verifies |
| **4 Crypto** (SC-13) | FIPS 140-2/3; KMS/HSM-backed asymmetric keys; **no HS256** | **xkms** (TPM2/PKCS#11/cloud-KMS) via the qrdb barrier |
| **5 Revocation/audit** (AC-12, AU-2/3) | Short-lived tokens; introspection; dual-identity audit | QuicBaaS (token TTL + introspection + audit) |

Layer 4 is the strategic advantage: because QuicBaaS composes xkms, the broker signs platform
tokens with **hardware/KMS-resident keys** out of the box — satisfying FIPS for FedRAMP
High / IL5 without bolting on a separate KMS integration. (Today's OIDC signing key is an
**ephemeral in-process RSA key** — `pkg/server/iam_init.go` — which is the #1 production gap
to close; see §7.)

## 4. The platform access token (issuer contract)

QuicBaaS issues this; downstream services validate it.

| Claim | Type | Meaning |
|-------|------|---------|
| `iss` | string | The broker issuer. **Per-tenant issuer with global fallback** (§6) — but a *single trust anchor per deployment* for validators (one JWKS root). |
| `aud` | string/array | Target service resource, e.g. `api://app` (audience-scoped per call) |
| `sub` | string | Stable user id (qrdb uint64 user id, or federated `sub`) |
| `tenant_id` | string | Drives downstream RLS / tenant isolation |
| `scp` | array | Granted scopes (e.g. `app.read`, `app.admin`) → downstream RBAC |
| `roles` | array | Optional supplemental RBAC input |
| `iat`/`exp` | number | Short-lived: **5–15 min** |
| `jti` | string | Unique id (revocation/introspection) |
| `cnf` | object | `{"jkt":"<thumbprint>"}` — DPoP key binding (Layer 3) |

- **Signing: ES256 / RS256 only — never HS256.** Key is xkms/KMS-backed; public key at the
  broker JWKS endpoint, selected by `kid`.
- **Scope catalog** is owned per service and mapped from the tenant's IdP groups (broker
  translates e.g. `App-Admins` → `app.admin` via fail-closed group→role mapping).
- **DPoP (RFC 9449):** broker holds the DPoP key, signs a proof per call; validators check
  `htm`/`htu`/`iat`/`jti`-replay and that `SHA-256(jwk) == cnf.jkt`. Forward path: RFC 8705
  cert-bound tokens (`cnf.x5t#S256`) once Okta-for-Gov supports it.

### Single trust anchor vs per-tenant issuer

A validator wants **one** trusted issuer (collapses N×M → N). Reconcile with per-tenant
identity ownership as follows: **the broker is the single issuer/JWKS trust anchor; the
tenant dimension rides in `tenant_id` + `scp`, not in a different signing identity.**
Per-tenant *issuer URL/key* is an option only for `dedicated`/contractual-isolation tenants
that require their own cryptographic issuer; by default all tenants share the broker's
issuer + JWKS (multiple `kid`s allowed for rotation), and validators trust one anchor. This
keeps the FedRAMP "one ATO boundary, N federations + 1 internal anchor" property.

## 5. Federation (the "integrated / BYO-Okta" path)

QuicBaaS's `pkg/iam/federation*` is correctly **per-tenant** (`ExternalProvider{TenantID,
IssuerURL, ClientID, …, ClaimMappings, DefaultRoles}`) and has a working
`BeginLogin→HandleCallback→JIT` OIDC flow + the **broker** model (Okta authenticates →
QuicBaaS mints its own platform token). The production hardening it needs:

| Gap (early scaffold) | Fix |
|---|---|
| `parseIDToken` decoded the IdP token **without verifying signature/iss/aud/nonce** — critical hole | A hardened OIDC validator: RS256 + JWKS-cache + exact `iss`/`aud`/nonce checks |
| `ClaimMappings.Groups/Roles` defined but ignored; JIT assigned only static `DefaultRoles` | **Fail-closed** group→role/scope mapping |
| Federation storage was **in-memory** (lost on restart) | Persist per-tenant providers/links/state via the qrdb-backed stores |
| No Okta **Management-API** provisioning (manual tenant onboarding) | A self-contained Okta app/group/claim/policy provisioning client |
| SAML is an enum stub | Defer; OIDC-first |

After this, "a tenant brings its own Okta" is real and secure, and every downstream service
drops its own Okta config entirely.

## 6. Per-tenant config + fallback (the resolution rule)

Mirror the existing `iam.TenantSettings` per-field-fallback idiom (e.g. `DisplayName`→`Name`,
`SandboxPolicy`→server default) — **not** the barrier (which inherits *strategies* once at
creation and is otherwise isolated, with no runtime fallback).

- Add to `iam.TenantSettings`: `WebAuthn {RPID, RPDisplayName, RPOrigins}`,
  `OIDC {Issuer, SigningKeyRef, …}`, and reuse `ExternalProvider` for federation.
- **Resolve per request, per field**, falling back to the operator-configured **global/server
  default** when empty. A tenant can override just `RPID` and inherit everything else, or
  configure nothing and run on global defaults.
- WebAuthn RP: change `WebAuthnServiceFactory.buildService(tenantID)` to resolve the tenant's
  RP config (with global fallback) instead of hardcoding the global `Config` — this is the
  fix for the per-virtual-host RPID question that started this thread.
- **System tenant = control-plane/privilege only.** It is RBAC-special (cross-tenant scope;
  explicitly *denied* tenant-only permissions + key material) and must not double as the RP
  or issuer for ordinary tenant logins. If shared user-facing resources are ever needed,
  model a **separate ordinary tenant**, distinct from `system`.

## 7. Compliance control mapping

| Control | Requirement | Where satisfied in this design |
|---|---|---|
| SC-7 | Boundary protection | Layer 0 — private ingress, SG restriction (deployment) |
| SC-8 / SC-13 | Transmission confidentiality; FIPS crypto | Layer 1 mTLS (qrdb/xkms TrustedCA) + Layer 4 xkms FIPS/KMS keys |
| IA-9 | Service identity | Layer 1 — mTLS client-cert SAN |
| IA-2 | User identity (CAC/PIV, phishing-resistant) | Layer 2 — platform token; PIV/WebAuthn at the broker or Okta-for-Gov |
| AC-3 / AC-4 | Access enforcement / flow control | Layer 2 — `scp`→RBAC (PEP/PDP), `tenant_id`→RLS/isolation |
| AC-12 | Session termination | Layer 5 — 5–15 min tokens + introspection |
| AU-2 / AU-3 | Audit events & content | Layer 5 — dual-identity (service cert SAN + user `sub`/`tenant_id`/`scp`) audit |
| SC-13 (FIPS) | Validated modules | FIPS Go build + FIPS TLS + **xkms HSM/KMS keys; remove HS256/ephemeral keys** |

FedRAMP **Moderate**: either network isolation *or* an audience-restricted token + private
ingress is defensible — the broker token + mTLS already exceeds this. FedRAMP **High / IL4-5**:
all five layers bound together (mTLS channel + DPoP-bound token), which this design delivers.

## 8. Layer ownership (who builds what)

- **qrdb** — tenancy primitive (`IsolationType` prefix|barrier, per-tenant barrier/DEK), the
  user/RBAC primitive, mTLS identity, the factor-agnostic login framework, the barrier (key custody).
- **xkms** — all hardware/KMS crypto: FIPS signing keys for the platform token, TrustedCA for
  mTLS, WebAuthn/PIV verification.
- **quicbaas** — **the broker**: per-tenant federation (BYO Okta), platform-token issuance
  (issuer/JWKS, scopes, `tenant_id`, DPoP `cnf`), OAuth2/OIDC, per-tenant identity config +
  fallback, scope→RBAC, introspection, dual-identity audit. The single SDK frontage.
- **downstream services** — pure validators: one issuer/JWKS, DPoP, `scp`→RBAC,
  `tenant_id`→RLS. They consume the quicbaas SDK; they hold no per-tenant Okta config.

## 9. Gaps to close (grounded in current code)

1. **FIPS signing keys** — replace the ephemeral in-process RSA OIDC key
   (`pkg/server/iam_init.go`) with xkms/KMS-backed ES256/RS256 keys, with `kid` rotation +
   JWKS publication. (Prereq for High/IL5; also enables per-tenant/dedicated issuers.)
2. **DPoP (RFC 9449)** — add `cnf.jkt` issuance to the platform token + a DPoP proof
   verifier the SDK exposes for downstream validators.
3. **Federation hardening** — proper IdP token validation, claim→role/scope mapping
   (fail-closed), persistent per-tenant stores, optional Okta Management-API provisioner.
4. **Per-tenant identity config + fallback** — `TenantSettings.{WebAuthn,OIDC}` + the
   `buildService`/issuer resolvers with global fallback.
5. **Scope catalog + scope→RBAC** — per-service scope registry; map tenant IdP groups → scopes.
6. **Introspection + dual-identity audit** — RFC 7662 introspection endpoint; audit both
   service-cert and user identity.
7. **mTLS + private ingress** — deployment posture (GovCloud SG, internal ingress).

## 10. Rollout phases (proposed)

1. **Foundation/crypto** — xkms/KMS-backed platform-token signing key (remove HS256/ephemeral),
   `kid` + JWKS. (Gap 1)
2. **Token contract + validation** — platform-token claim set + a reusable SDK validator
   (single issuer/JWKS, `aud`, `exp`); scope→RBAC + `tenant_id`→RLS contract. (Gaps 2/5)
3. **DPoP sender-constraint** — issue + verify. (Gap 2)
4. **Federation hardening + Okta-in-quicbaas** — hardened IdP token validator + claim mapping +
   persistence + provisioner. (Gap 3)
5. **Per-tenant identity config + fallback** — RP + issuer resolution. (Gap 4)
6. **Revocation/audit + mTLS/private ingress** — introspection, dual-identity audit, GovCloud
   posture. (Gaps 6/7)
7. **Downstream cutover** — a consuming service drops Okta-direct/HS256 and becomes a pure
   validator via the quicbaas SDK; the reference consumer pattern for every other microservice.

## Open questions

- Single broker issuer vs per-tenant issuer for `dedicated`/IL5-contractual tenants (§4) —
  default single anchor; per-tenant issuer as an opt-in for hard isolation.
- DPoP now vs wait for Okta-for-Gov RFC 8705 cert-bound tokens (start DPoP, upgrade later).
- Where the broker runs relative to qrdb embedding (in-process node vs standalone broker
  service) for each deployment model.

## 11. Operator SSO + local-factor break-glass (Phase 5)

Operator (staff) SSO is an **additive** control-plane login path layered on the
always-available local-factor login. Two invariants are enforced in code.

### Operator SSO via the system tenant

- An `ExternalProvider` may be registered against `qrdbsdk.SystemTenantID`
  (`"system"`). Federation registration (`FederationManager.RegisterProvider`)
  and login (`HandleCallback`) place **no restriction** on the system tenant —
  the system tenant is a first-class federation target.
- A federated callback for the system tenant runs the standard broker path
  (`processAuthentication` -> `generateTokenResult`) and mints an internal
  quicbaas bearer for the operator. JIT-provisioned operators are created in the
  system tenant; claim->role mapping applies as for any tenant.
- The brief config path is `iam.OperatorSSORegistrar`
  (`pkg/iam/operator_sso.go`), reachable via
  `iamComponents.OperatorSSORegistrar()`. `RegisterOperatorProvider` forces the
  provider onto the system tenant so an operator IdP cannot be mis-targeted at a
  data-plane tenant.

### Break-glass independence (federation is never required)

- Local-factor login (mTLS / WebAuthn / PIV) for the system/admin tenant routes
  through the **embedded qrdb login ceremony** (`pkg/server/login_factor_verifiers.go`)
  and the identity store's cert-thumbprint recognition
  (`IdentityStore.GetUserByPIVThumbprint`). Neither path ever touches the
  `FederationManager`.
- Consequence: an external-IdP outage cannot take down control-plane login. This
  is proven by `TestBreakGlass_LocalFactorLoginWithIdPUnreachable`, which wires a
  federation manager whose every call errors and shows admin cert-recognition
  still succeeds, and by `TestBreakGlass_LocalFactorLoginWithFederationUnconfigured`
  (federation `nil`).

### Bootstrap-ordering invariant

- The bootstrap admin is created and cert-bound by `firstBootBootstrap` **before**
  any operator IdP can be configured. `OperatorSSORegistrar` gates system-tenant
  registration on `AdminManager.HasAdmin` and fails closed with
  `ErrBootstrapNotReady` when no admin exists yet (no admin -> can't configure
  Okta). It also fails closed if the admin-presence check itself errors.
- The bootstrap admin lives in the prefix-isolated system tenant (shared/system
  store, **not** a per-tenant barrier), so cert/local login resolves before any
  tenant barrier unseals — proven by `TestBreakGlass_CertLoginBeforeBarrierUnseal`.
