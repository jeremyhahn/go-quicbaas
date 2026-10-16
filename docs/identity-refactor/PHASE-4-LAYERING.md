# Phase 4 — Layering follow-up

> **Status: ARCHIVE — historical record. Do NOT build from this document.** · Last verified: 2026-10-15

Status: **completed** · Last updated: 2026-05-08

## 1. The discovery

When Phase 4 started, the brief read xkms's `pkg/rbac` and `pkg/authz`
as duplicates of qrdb's identity-layer RBAC and scheduled them for
deletion. A pre-implementation verification spike showed that read was
half right: the **shape** of those packages is identity-shaped (subject
strings, role names, no per-key fields, six FIPS-aligned role names that
collide with qrdb's), but the **content** is key-domain.

Specifically: xkms's `Admin` role grants `{keys:*, certificates:*,
secrets:*, barrier:*, audit:*, backends:*, pin:*, credentials:*,
passwords:*}`. qrdb's permission registry (`pkg/permission/constants.go`)
contains none of those — qrdb's `Admin` grants `{kv:*, mvcc:*,
transaction:*, tenant:list:cross, tenant:approve, ...}`. Same role
names, completely non-overlapping permission vocabularies.

If those packages had been deleted and routed through qrdb's RBAC
authorizer, every key/cert/secret operation in xkms would have been
silently denied because qrdb's authorizer would not recognise the
permissions.

## 2. The corrected shape (LDAP / AD pattern)

Identity layer (qrdb): tenants, users, roles, role memberships, and
permissions for identity-domain operations only (tenant CRUD, user CRUD,
KV/MVCC, audit). qrdb is the source of truth for *who* and *what role
assignments*.

Application layer (xkms, quicbaas, ...): each consuming application owns
its own role -> permission map for its own domain. xkms maintains
`{keys, certs, secrets, ...}` permissions in its own packages. quicbaas
maintains its own static-password / team-domain permissions in pkg/iam
(scheduled for Phase 5 deletion of the webauthn duplication only —
quicbaas's tenant/policy domain logic stays put).

## 3. Concrete changes in xkms

| Action  | Target                                                       | Why                                                        |
|---------|--------------------------------------------------------------|------------------------------------------------------------|
| Rename  | `pkg/policy` -> `pkg/keypolicy`                              | Source-tree clarity — these are key-domain Allow/Deny rules. |
| Rename  | `pkg/rbac` -> `pkg/keyrbac`                                  | Source-tree clarity — these are key-domain role/perm maps. |
| Rename  | `pkg/authz` -> `pkg/keyauthz`                                | Source-tree clarity — this is the key-domain authorizer.   |
| Delete  | `pkg/auth.Identity` struct + `IsCrossTenant`/`HasPermission` | Re-aliased to `qrdbauth.Identity` — single identity shape.  |
| Delete  | `pkg/user.UserRBACAdapter`                                   | Replaced by `pkg/user.QRDBMembership` (qrdb-backed helpers).|
| Add     | `pkg/auth.IsCrossTenant(*Identity)`                          | System-tenant rule helper consumed by middleware.          |
| Add     | `pkg/auth.FirstRole(*Identity)`                              | Replaces three near-identical `extractRole` helpers.       |
| Update  | 50+ middleware / handler call sites                          | `id.Claims["roles"]` -> `id.Roles`, etc.                    |

## 4. Concrete changes in qrdb

**None.** qrdb's permission registry, RBAC, and SDK are unchanged. The
boundary documentation (`docs/auth/layering.md`) is added so future
contributors don't try to push key-domain permissions upstream.

## 5. Concrete changes in quicbaas

**None in this phase.** The webauthn duplication scheduled for Phase 5
is still scheduled for Phase 5 — that's domain-IAM duplication and lives
in quicbaas's `pkg/iam`. This document is the architectural follow-up
to the layering in [architecture.md](architecture.md).

## 6. Why this matters going forward

- New crypto operations in xkms add permissions in `pkg/keyrbac` only;
  no qrdb release coupling.
- New identity operations in qrdb add permissions in
  `pkg/permission/constants.go`; downstream consumers don't need to
  redeploy in lockstep.
- Authorization bugs are scoped: a wrong role assignment in xkms's
  keyrbac cannot grant cross-tenant access because qrdb's
  system-tenant rule sits upstream.

## 7. Verification

```sh
# No old import paths anywhere outside pkg/auth/policy and pkg/seal/policy.
grep -r 'go-xkms/pkg/\(policy\|rbac\|authz\)\b' --include='*.go' \
    <go-xkms-checkout>/ \
    | grep -v "/pkg/auth/policy" | grep -v "/pkg/seal/policy"
# (zero hits)

# All imports use the renamed paths.
grep -r 'go-xkms/pkg/\(keypolicy\|keyrbac\|keyauthz\)\b' --include='*.go' \
    <go-xkms-checkout>/ | wc -l
# (>0)

# auth.Identity references in production code resolve to the qrdb alias.
grep -r 'auth\.Identity\b' --include='*.go' <go-xkms-checkout>/ \
    | grep -v 'qrdbauth\|qrdb/pkg/auth\|_test.go'
# (only legitimate uses of the type alias; the standalone struct is gone)
```

## See also

- [architecture.md](architecture.md) — the identity-refactor layering.
- `<go-xkms-checkout>/docs/auth/layering.md` — xkms-side layering doc.
- `<go-qrdb-checkout>/docs/auth/layering.md` — qrdb-side layering doc.
