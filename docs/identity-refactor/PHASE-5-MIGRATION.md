# Phase 5 — Quicbaas Refactor: Consume xkms WebAuthn, Delete Duplicates

> **Status: ARCHIVE — historical record. Do NOT build from this document.** · Last verified: 2026-09-14

Status: completed 2026-05-07
Plan reference: `parsed-jingling-crane.md` § Phase 5

## Summary

Phase 5 deletes ~3,742 LOC of duplicate WebAuthn code that previously
lived in `pkg/iam` and replaces it with a thin multi-tenant adapter
over `go-xkms/pkg/webauthn`. The xkms qrdb adapter pair (introduced in
Phase 4) backs per-tenant user and credential persistence; quicbaas
keeps only the domain plumbing — the AAGUID allowlist enforcement, the
multi-tenant routing, and the REST mount points.

## What changed

### Deleted (7 files, 3,742 LOC)

- `pkg/iam/webauthn.go` (527 LOC) — `WebAuthnManager`, `BeginRegistration`,
  `FinishRegistration`, `BeginLogin`, `FinishLogin`.
- `pkg/iam/webauthn_memory.go` (431 LOC) — in-memory `MemoryCredentialStorage`,
  `MemorySessionStorage`.
- `pkg/iam/webauthn_storage.go` (84 LOC) — `CredentialStorage` and
  `SessionStorage` interfaces.
- `pkg/iam/webauthn_types.go` (200 LOC) — `WebAuthnCredential`, `WebAuthnChallenge`,
  related typed errors.
- `pkg/iam/webauthn_test.go` (1,611 LOC).
- `pkg/iam/webauthn_memory_test.go` (533 LOC).
- `pkg/iam/webauthn_types_test.go` (356 LOC).
- `pkg/server/iam_webauthn_adapter.go` (171 LOC) — protocol/map adapter
  superseded by the xkms-backed manager.
- `pkg/server/iam_webauthn_adapter_test.go` (~190 LOC).

### Extracted (Decision 1)

The `SessionStorage` interface and `MemorySessionStorage` impl were
historically mis-located inside `webauthn_*.go`. They operate over
`iam.Session` (general app sessions, not WebAuthn ceremony sessions)
and are consumed by the gRPC `SessionServiceAdapter`, the IAM service
provider, and ~30 test mocks. To preserve this consumer base, we
extracted them to dedicated files BEFORE deleting the WebAuthn
duplicates:

- `pkg/iam/session_storage_basic.go` — `SessionStorage` interface (was
  `webauthn_storage.go:58-84`).
- `pkg/iam/memory_session.go` — `MemorySessionStorage` impl (was
  `webauthn_memory.go:270-434`).

Net deletion drops to ~3,260 LOC (still meaningfully net-negative).

### Added

- `pkg/config/config.go` — `IAMWebAuthnConfig` struct and `Validate` method
  (Decision 3). Required fields: `rp_id`, `rp_display_name`, `rp_origins`.
  *(Superseded: that requirement was never enforced — `Validate` had no caller
  — and it contradicted the derive-from-issuer path added later. The type and
  its two validators now live in `pkg/config/iam_webauthn_config.go`; see
  [docs/iam/engineering.md](../iam/engineering.md) for the current
  required-versus-derived contract.)*
- `pkg/iam/webauthn_xkms_factory.go` — `WebAuthnServiceFactory` builds
  per-tenant `xkmswebauthn.Service` instances, lazily cached in a
  `sync.Map` keyed by tenant id (Option A from the plan).
- `pkg/iam/webauthn_xkms_manager.go` — `XKMSWebAuthnManager` implements
  `service.WebAuthnManager` directly. Translates the existing
  `(tenantID, userID, map[string]interface{})` REST surface into xkms's
  typed `(email, displayName, *protocol.ParsedCredentialCreationData)`
  surface. Carries the AAGUID allowlist hook for FinishRegistration
  enforcement.
- `pkg/server/iam_webauthn_config.go` — `webauthnFactoryConfigFromOperator`
  bridges `config.IAMWebAuthnConfig` to the factory's input shape with
  dev-mode defaults for unset fields.
- `pkg/api/rest/iam_xkms_webauthn_mount.go` — `MountXKMSWebAuthnRoutes`
  registers xkms's native HTTP handlers under
  `/api/v1/iam/{tenant_id}/webauthn` (Plan step 3 / 5).
  *(Superseded: `MountXKMSWebAuthnRoutes` is DELETED. It was a second
  registration of the same five paths, on a router the unified-API-server
  architecture never serves, and it returned early and silently when the
  xkms-backed manager was not yet resolvable. The module route table
  (`routes_iam.go`) is now the single owner, dispatching through
  `moduleWebAuthnHandler`; go-quicbaas#529.)*

### Modified

- `pkg/server/iam_init.go` — constructs the qrdb-backed factory + manager
  on daemon startup; passes it through `IAMComponents.GetWebAuthnManager`.
  The qrdb client is sourced from `s.qrdbClient.ForShard(qbshards.PlatformShardID)`.
- `pkg/api/rest/iam_invite.go` — `ActivateFIDO2Begin` and `ActivateFIDO2Finish`
  now resolve the user's email from the IdentityStore before invoking
  WebAuthn (xkms keys ceremony sessions by email, not by quicbaas user id).
- `pkg/iam/gdpr.go` — `GDPRManagerDependencies.CredentialStorage` field
  removed; the credential-export and credential-delete code paths are
  no-ops pending an xkms-backed re-implementation.
- `pkg/iam/stub.go` — removed `!iam` stubs for `WebAuthnManager`,
  `WebAuthnConfig`, `WebAuthnDependencies`, `MemoryCredentialStorage`,
  `NewWebAuthnManager`, `NewMemoryCredentialStorage`.

### Tests

New test files cover the new code paths to maintain ≥90% coverage on
new code:

- `pkg/config/iam_webauthn_config_test.go` — `IAMWebAuthnConfig.Validate`
  table-driven (happy path, every required field, every enum branch,
  origin-index-in-message).
- `pkg/iam/webauthn_xkms_factory_test.go` — factory construction error
  paths, empty-tenant rejection, error message formatting, AAGUID
  setter atomicity.
- `pkg/iam/webauthn_xkms_manager_test.go` — `marshalToMap` round-trip,
  `parseCredentialCreationMap` / `parseCredentialAssertionMap` error
  paths, no-session error paths for FinishRegistration / FinishLogin,
  internal `regKey` invariant.
- `pkg/server/iam_webauthn_config_test.go` — operator-config to
  factory-config translation, default application, origin-slice copy.
- `pkg/api/rest/iam_xkms_webauthn_mount_test.go` — dispatcher missing
  tenant id, prefix-strip routing, no-op when factory unavailable.

Existing test fixtures were updated to drop the credential-storage
mock (`pkg/api/grpc/iam_service_adapter_unit_test.go`) and to assert
nil-credential export in the GDPR manager test
(`pkg/iam/gdpr_test.go`).

## Architecture flow

```
Browser → REST /api/v1/iam/{tenant}/webauthn/registration/begin
         ├─ buildXKMSWebAuthnDispatcher: strip prefix, resolve tenant
         ├─ WebAuthnServiceFactory.ServiceFor(tenant) → cached xkms.Service
         │  └─ NewQRDBUserStoreAdapter(qrdb.Users(), qrdb.KV(), tenant)
         └─ xkms.Handler.BeginRegistration(...)
              ├─ xkms.UserStore.Create / GetByEmail (qrdb-backed)
              └─ xkms.CredentialStore.Save (qrdb-backed, KV namespace
                 __xkms:webauthn/credentials/<id>)
```

At the time of this migration the legacy
`/api/v1/tenants/{tenant_id}/webauthn` routes still routed through
`IAMHandler.BeginRegistration` → `service.WebAuthnManager` → the same
per-tenant xkms service. **That legacy mount has since been deleted**;
the canonical `/api/v1/iam/{tenant_id}/webauthn` mount is the only
REST ceremony surface, and browser/TypeScript-SDK login uses the
unified ceremony at `/api/v1/auth/login/{begin,finish}`.

## Decisions baked in

1. **Decision 1 — Extract SessionStorage first.** Avoids mass
   refactoring of the gRPC SessionServiceAdapter and ~30 test mocks.
2. **Decision 2 — Delete CredentialStorage abstraction outright.**
   GDPR's credential-export feature is no longer reachable from
   quicbaas; an xkms-backed re-implementation is a follow-up.
3. **Decision 3 — Add `IAMWebAuthnConfig` now.** The dev defaults
   (`localhost` / `QuicBaaS`) are still applied when the operator
   leaves the field empty, but the config schema is in place.
4. **Decision 4 — Wire qrdb client end-to-end.** The factory pulls
   the platform-shard qrdb client from `Server.qrdbClient.ForShard(PlatformShardID)`.
5. **Decision 5 — One coherent landing.** No 5a/5b split.

## Verification

- `go test -tags 'rest'` clean for `pkg/iam/...`, `pkg/api/...`,
  `pkg/server/...`, `pkg/service/...`, `pkg/config/...`.
- `go vet -tags 'rest'` clean.
  (Historical: the `iam` build tag was removed; IAM is always compiled.)
- LOC delta: -3,742 (deleted) + ~700 (new factory + manager + tests
  + config + mount) = net ~-3,000 LOC.

## Backward compatibility

- (Historical) the legacy `/api/v1/tenants/{tenant_id}/webauthn/...`
  routes remained wired at the time of this migration. They were
  deleted in the later FIDO2/PIV surface unification; clients now use
  the canonical mount or the unified login ceremony.
- The `service.WebAuthnManager` interface is unchanged; downstream
  consumers see no signature drift.
- `iam.SessionStorage` and `iam.MemorySessionStorage` retain their
  public API (just live in different files).

## Follow-up work (out of scope for Phase 5)

- xkms-backed credential export hook for the GDPR manager.
- Tenant-aware credential rename / list / delete UI (currently the
  legacy IAMHandler exposes them via the deleted code path; the
  Phase-5 manager has not yet wired them through).
- AAGUID allowlist enforcement for the assertion (login) ceremony —
  currently only enforced on registration.
