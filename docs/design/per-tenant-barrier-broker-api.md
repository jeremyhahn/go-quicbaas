# Plan: Per-Tenant Barrier Broker API (quicbaas)

> **Status: REFERENCE** · Last verified: 2026-06-10
> Implemented plan for the per-tenant barrier broker API.

**Status:** IMPLEMENTED (2026-06-09) — except the docker-harness integration matrix (§4 Phase 4), which remains a CI follow-up.
**Date:** 2026-06-09
**Layer:** go-quicbaas (L4 broker). Consumes only `go-qrdb/sdk/go`.
**Supersedes:** the earlier draft proposal in this file.

## Implementation status (2026-06-09)

Delivered exactly to the pure shape in §0 (one canonical home = qrdb; one broker
facade; native-protobuf gRPC; reuse not duplication):

- **Prereq — qrdb gRPC = protobuf:** the 4 qrdb JSON-over-gRPC services
  (`tenant_barrier`, `audit`, `authfactor`, `user_auth_factor`) were converted to
  native protobuf first (proto + regen + servers + SDK clients + tests); qrdb
  `make test` green (101 pkgs). No `wrapperspb.BytesValue` JSON-over-gRPC remains.
- **Phase 0** — SDK re-exports (`sdk/go/qrdb.go`: `QRDBTenantBarrierInitResponse`,
  `…UnsealShareRequest`, `…UnsealProgress`).
- **Phase 1** — `service.TenantBarrierServicer` facade (pure delegation to the
  embedded qrdb `TenantClient`) + `(*Server).buildTenantBarrierService()`.
- **Phase 2** — all four transports delegate to the one facade:
  REST (`/api/v1/tenants/{id}/barrier/*` + tenant-scoped PEP authz),
  **native-protobuf gRPC** (`quicbaas.tenant.v1.TenantBarrier`),
  QUIC (`tenant.Barrier*` on the `qrdb-quic` ALPN bridge), MCP tools — each with
  `tenant_barrier` authz entries.
- **Phase 3** — quicbaas TS SDK `TenantBarrierClient` + 2 new shared
  `svelte-security` primitives (`BarrierStrategyPicker`, `ShamirSharesReveal`) +
  `TenantBarrierView.svelte`.
- **Phase 4** — unit tests: facade delegation + the security-critical authz
  mapping (barrier subpaths → `tenant_barrier`, beating the `tenants` prefix).
  quicbaas `make test` green (97 pkgs). **Remaining:** the docker-harness
  integration matrix (none/passphrase/shamir × ops × transports + cross-tenant
  denial) — run in CI/devcontainer.

This plan exposes per-tenant **barrier lifecycle** (init / unseal / unseal-share /
seal / status, including the Shamir M-of-N strategy now shipped in qrdb) through
the quicbaas broker. It is written to follow the stack's **existing** architecture
and engineering conventions and to be **DRY** — every layer reuses an established
quicbaas/qrdb seam rather than introducing a parallel one.

Requirement: per-tenant barrier management must be reachable by **remote
tenant admins** through the broker front door (the embedded qrdb node is
in-process and has no independent network listener).

---

## 0. The most technically pure shape (and why)

"Most pure" is defined by this codebase's own stated principles — **one canonical
home for the logic; the service layer is the single abstraction; every transport
delegates to it; behaviour is consistent across surfaces.** Per-tenant barrier is
therefore:

1. **Logic has exactly one home: qrdb.** quicbaas delegates, never reimplements.
   (Already true — qrdb owns barriers end-to-end.)
2. **Exactly one new quicbaas abstraction:** a `TenantBarrierServicer` service
   facade that delegates to the embedded qrdb `TenantClient`. It is structurally
   identical to the existing system-barrier `BarrierServicer` /
   `qrdbBarrierClientServicer` (`pkg/server/grpc_qrdb_barrier_adapter.go`), one
   scope down.
3. **Every transport delegates to that one facade.** Critically, per-tenant
   barrier **rides the EXISTING `qrdb-quic` ALPN wire bridge**
   (`pkg/api/quic/handler_qrdb_credentials.go` `buildQRDBHandlers`) by adding
   `tenant.Barrier*` method entries that call the same facade — exactly as the
   `barrier.*`, `credential.*`, and `ceremony.*` families already do (those
   delegate to the SAME facades the REST/gRPC transports use). REST/gRPC/MCP add
   the same thin handler layer they add for every capability.
4. **Reuse, never redeclare:** re-export the qrdb SDK types, reuse the
   `tenant_barrier` authz family (+ one `unseal-share` action), reuse the
   tenant-context authorizer, compose the shared `svelte-security` UI components.

The purity is: logic has one home, quicbaas has one barrier abstraction, and it
is consistent with the credential/ceremony/system-barrier surfaces already built
on the same facade+bridge pattern. The only repetition — thin per-transport
handlers — is the project's deliberate, uniform **transport-parity** pattern,
present for every capability, not accidental duplication.

### Rejected alternative: a generic opaque relay

A relay that forwards any qrdb wire frame to a single embedded-node dispatch
(zero per-op code) was considered and **rejected**. It optimizes non-duplication
but sacrifices two stated principles: it **bypasses the quicbaas service-facade
abstraction** — so the broker's authorization / ABAC / audit overlay would not
apply uniformly to a *security-critical unseal path* — and it would make
tenant-barrier **inconsistent** with the facade-based barrier/credential/ceremony
bridges. Trading single-abstraction + consistency to save thin boilerplate is
less pure for this project, not more.

### Rejected alternative: reimplementing barrier logic in quicbaas

Forbidden by locked layer ownership (qrdb is the one canonical home; composition
is one-way). Not considered further.

---

## 1. Conventions this plan is bound by (verified against the codebase)

These are the locked conventions the implementation MUST follow (sources cited):

1. **Service layer is the only abstraction; every transport delegates to it.**
   quicbaas already delegates per-tenant qrdb operations through service types
   that wrap a qrdb SDK servicer/client and pass the tenant via context — e.g.
   `databaseService` wraps `QRDBDatabaseServicer` and calls `CreateForTenant` /
   `UpdateBarrierForTenant` (`pkg/service/database.go:25-34`), and
   `iamServiceWrapper.CreateTenant` delegates tenant lifecycle to qrdb's
   `TenantServicer` via the embedded SDK transport
   (`pkg/service/quicbaas_iam_method.go`). The new barrier ops attach to this
   same seam — **no new bespoke client plumbing.**
2. **System-barrier broker API is the structural template.** The system barrier
   is a service (`pkg/service/barrier_service.go` `BarrierService`) delegated to
   qrdb via `qrdbBarrierClientServicer` (`pkg/server/grpc_qrdb_barrier_adapter.go`),
   which implements the qrdb SDK interface `qrdbsdk.BarrierServicer`
   (re-exported at `sdk/go/qrdb.go`). The per-tenant analogue mirrors this exactly,
   one scope down.
3. **Consume only `sdk/go`; types are re-exported, never re-declared.**
   `sdk/go/qrdb.go` already re-exports `QRDBTenantClient = qrdbtransport.TenantClient`
   and `QRDBTenantBarrier{Init,Unseal,Seal,Status}Request`. The plan reuses these
   and adds the few missing re-exports — it does **not** define quicbaas-local
   request/response structs that shadow qrdb's.
4. **Transport parity.** Every broker capability is exposed across REST, gRPC,
   QUIC, and MCP via map-based dispatch, all routing into the one service
   interface (per the repo's transport-parity rule).
5. **Typed errors only** (never `fmt.Errorf`); errors map to transport status
   codes via the existing quicbaas error→status mapping.
6. **Authorization: default-deny, reuse the existing permission family.** The
   `tenant_barrier` family is already mapped across all transports' authz tables
   (`pkg/api/{grpc,mcp,rest}/authz_*`). Reuse it; add exactly one new action,
   `unseal-share`.
7. **UI: compose the shared `svelte-security` component library**, do not clone.
   `sdk/typescript/svelte-security/src/components/` already ships
   `BarrierStatusBadge`, `BarrierUnsealForm`, `ShamirShareInput`, and
   `BootstrapMethodPicker` — the exact primitives this feature needs.
8. **Crypto stays in qrdb.** quicbaas adds no seal logic; it proxies, authorizes,
   and audits. (Locked layer ownership: qrdb owns barriers; quicbaas is the front
   door.)

---

## 2. DRY map — reuse vs. new

| Concern | Reused (existing) | New (added) |
|---|---|---|
| qrdb client | `QRDBTenantClient` (`= qrdbtransport.TenantClient`), already re-exported, already used for tenant lifecycle | — |
| Request/response types | `QRDBTenantBarrier{Init,Unseal,Seal,Status}Request` | **re-exports only**: `QRDBTenantBarrierInitResponse`, `QRDBTenantBarrierUnsealShareRequest`, `QRDBTenantBarrierUnsealProgress`, `QRDBTenantBarrierStatusResponse` (add to `sdk/go/qrdb.go`) |
| Service | extend the existing tenant servicer seam (the `iamServiceWrapper` / tenant-method path that already delegates to qrdb `TenantServicer`) | 5 thin methods that forward to `QRDBTenantClient` |
| Delegation adapter | pattern from `qrdbBarrierClientServicer` (system barrier) | per-tenant equivalent forwarding to `client.Tenants()` |
| Tenant scoping | the broker's existing tenant-context + tenant-scoped authorizer (same one IAM tenant + data-plane endpoints use) | — |
| Authz vocabulary | `tenant_barrier` permission family across all transports | one action: `unseal-share` |
| proto | `api/proto/barrier.proto` message/RPC conventions | per-tenant RPCs (mirror shapes) |
| Transports | existing handler registration + map dispatch per transport | thin handlers, one op set |
| UI primitives | `svelte-security`: `BarrierUnsealForm`, `ShamirShareInput`, `BarrierStatusBadge`, `BootstrapMethodPicker` | one tenant-scoped view that composes them |
| Audit | existing broker audit pipeline | `tenant_barrier.*` event (no key material) |

**Anti-duplication callouts:**
- Do **not** clone qrdb's `TenantManager.svelte`; compose the `svelte-security`
  components instead (qrdb's admin UI and quicbaas's UI should both consume that
  shared library — if a primitive is missing, add it to `svelte-security`, not to
  one app).
- Do **not** redeclare wire structs; if a transport needs a JSON shape, alias the
  re-exported qrdb SDK type.
- Do **not** add a second tenant-scoping mechanism; reuse the authorizer that
  already gates the IAM tenant + per-tenant database endpoints.

---

## 3. Architecture & endpoint model

quicbaas scopes per-tenant data-plane operations by the **caller's tenant from
the request context** (e.g. `databaseService.*ForTenant`), while tenant *admin*
operations (`CreateTenant`, …) are operator/tenant-admin actions. Barrier
init/seal are admin-lifecycle operations; unseal/unseal-share are tenant-admin
self-service. To stay consistent with the IAM tenant resource (where
`CreateTenant` lives) the **primary surface nests under the tenant resource**:

```
POST   /api/v1/tenants/{id}/barrier/init           init  (200 {strategy,shares[],threshold,total_shares} for shamir; 204 otherwise)
POST   /api/v1/tenants/{id}/barrier/unseal          single-key unseal (passphrase)  -> 204
POST   /api/v1/tenants/{id}/barrier/unseal-share    {share} -> 200 {required,submitted,complete,sealed}
POST   /api/v1/tenants/{id}/barrier/seal            -> 204
GET    /api/v1/tenants/{id}/barrier/status          -> 200 {sealed,initialized,strategy,hardware_backed,shamir_threshold,shamir_total}
```

These mirror the qrdb REST shapes 1:1 so the quicbaas handlers are pass-throughs.
gRPC: a `quicbaas.tenant.v1.TenantBarrier` service in `api/proto/` mirroring the
`quicbaas.barrier.v1.Barrier` message/RPC conventions, each RPC carrying
`tenant_id`. QUIC/MCP mirror the same op set per the transport-parity rule.

**Authorization:** the broker enforces, at its front door, that the caller is an
admin of `{id}` and that `{id}` is not the system tenant (qrdb applies
`ScopeTenantOnly` on its side too — defense in depth). Reuse the IAM tenant
authorizer; add the `tenant_barrier:unseal-share` mapping to every transport's
authz table alongside the existing init/unseal/seal/status entries.

---

## 4. Implementation phases

### Phase 0 — qrdb SDK re-exports (prerequisite, in go-qrdb)
Add to `go-qrdb/sdk/go/qrdb.go` (next to the existing `QRDBTenantBarrier*Request`
re-exports): `QRDBTenantBarrierInitResponse`, `QRDBTenantBarrierUnsealShareRequest`,
`QRDBTenantBarrierUnsealProgress`, `QRDBTenantBarrierStatusResponse`
(`= qrdbtransport.…`). Pure aliases; no logic. (These types already exist in the
qrdb SDK from the per-tenant-shamir work.)

### Phase 1 — service + delegation (backend core)
- `pkg/service/tenant_barrier_service.go`: a `TenantBarrierService` interface +
  impl that holds a `qrdbsdk.QRDBTenantClient` and forwards `Init/Unseal/
  UnsealShare/Seal/Status`, scoping by `tenant_id`. Compile-time interface check.
  Construction gated on a live platform-shard qrdb client, exactly like
  `newQRDBBarrierClientServicer`. Reuse the existing tenant servicer wiring so the
  client is the same embedded-node transport already used for `CreateTenant`.
- Typed errors; translate `qrdbsdk.ErrTenantSealed`, volume-pool-not-configured,
  invalid-strategy into the broker's error→status mapping.
- Audit hook: emit `tenant_barrier.{init,unseal,seal}` (no share/passphrase bytes).

### Phase 2 — transports (parity), all delegating to the one facade
- **REST** handlers under the IAM tenant routes (`pkg/api/rest/`); share material
  proxied straight through, never buffered/logged.
- **gRPC**: extend the barrier proto convention (mirror `quicbaas.barrier.v1`)
  with tenant-scoped RPCs; thin handlers → facade.
- **QUIC**: do NOT add a new mechanism — add `tenant.BarrierInit`,
  `tenant.BarrierUnseal`, `tenant.BarrierUnsealShare`, `tenant.BarrierSeal`,
  `tenant.BarrierStatus` entries to the existing `buildQRDBHandlers` table on the
  `qrdb-quic` ALPN, each delegating to the `TenantBarrierServicer` facade and
  marshalling the canonical `qrdbv1` shapes — exactly as `barrier.*` does today.
- **MCP**: thin tool handlers → facade.
- Add `tenant_barrier:unseal-share` to each transport's authz table (the other
  `tenant_barrier` actions already exist); keep the tenant-scope gate.

### Phase 3 — UI (compose shared components)
- One tenant-scoped barrier view in the quicbaas GUI that composes
  `BootstrapMethodPicker` (strategy select), `ShamirShareInput` + the init
  share-display-with-save-ack, `BarrierUnsealForm` (passphrase + shamir quorum),
  and `BarrierStatusBadge`. If any primitive needs extension (e.g. a
  share-display-with-ack variant), add it to `svelte-security` so both qrdb and
  quicbaas benefit.
- TS SDK: extend the quicbaas TS client to call the new endpoints, reusing the
  qrdb TS SDK tenant-barrier types/shape (the qrdb TS SDK already models these).

### Phase 4 — tests
- Unit: service delegation (mock `QRDBTenantClient`), authz tenant-scope (own
  tenant allowed, cross-tenant + system-tenant denied), error mapping.
- Integration matrix (extend `make integration-test`): per-tenant
  {none, passphrase, shamir} × {init, unseal/unseal-share, seal, status} × all
  transports, plus cross-tenant denial. Reuse the qrdb integration fixtures where
  possible.
- No `t.Skip` / `-short` / `time.Sleep`; ≥90% coverage on the new packages.

---

## 5. Risks & open decisions

- **Volume-pool prerequisite.** A cryptographic per-tenant barrier needs a qrdb
  storage volume pool on the embedded node; surface qrdb's
  `volume pool not configured` cleanly and document it (mirrors qrdb admin UI).
- **Init timing.** Decide: fold barrier init into the tenant-create flow
  (self-bootstrap) vs. keep it an explicit post-create step. qrdb models it
  explicit; recommend matching for symmetry and to avoid coupling tenant CRUD to
  seal state.
- **Path vs. context scoping.** Primary surface is path-based `{id}` under the
  IAM tenant resource (admin-oriented, matches `CreateTenant`). A context-scoped
  convenience alias (caller's own tenant) can be added later if tenant-admin
  self-service ergonomics demand it — but only one service backs both.
- **Scope of MCP exposure.** Confirm whether per-tenant barrier ops belong in the
  MCP tool surface (operator automation) or REST/gRPC only.

## 6. Effort & sequencing

Bounded and mostly mechanical: a thin proxy over an already-shipped, already-
tested qrdb SDK surface, structurally identical to the existing system-barrier
broker API one scope down, with the UI assembled from existing shared components.
Recommended order: Phase 0 → 1 → 2 → 3 → 4. Phases 0–2 unblock API consumers;
Phase 3 (UI) and the TS SDK can follow. This is net-new product surface (like the
deferred PIV/WebAuthn item), not an alignment fix — the stack is already aligned
without it.
