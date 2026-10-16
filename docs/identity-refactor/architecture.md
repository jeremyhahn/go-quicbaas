# Identity refactor: architecture

> **Status: NORMATIVE** · Last verified: 2026-10-15
> The design for the SPEC row `identity-refactor`. The UI route guard is
> built. The layering is future work.

## The problem

Sessions, factors, RBAC and the login handlers live in `pkg/iam`. go-qrdb
already has overlapping primitives: `LoginResolver`, `Principal`, `Identity`,
`FactorStore`, `UserLookup`, the mTLS middleware and the RBAC engine. A
downstream service that wants login, sessions and RBAC must reimplement them,
although the primitives already exist one layer down.

## The layering

Each repo owns one concern and re-exports what it builds on, so a consumer
imports one SDK and gets everything below it.

```
go-quicbaas   pkg/iam: tenants, policies, OIDC, audit, IAM admin surface
              login and session through the go-xkms SDK; the UI is an SDK consumer
     ▲ inherits
go-xkms       hardware crypto and PIV slots; auth.Factor adapters for its hardware
              re-exports go-qrdb's auth surface (Principal, Session, LoginResolver)
     ▲ inherits
go-qrdb       login resolver, factors, RBAC, session lifecycle, registration primitive
```

| Concern | Owner |
|---|---|
| `Principal`, `Identity` and `Session` types | go-qrdb |
| `FactorStore`, `UserLookup`, `LoginResolver` | go-qrdb |
| Session cookie issuance, token minting and refresh | go-qrdb |
| Auth HTTP middleware and the RBAC engine | go-qrdb |
| The self-service registration primitive | go-qrdb |
| FIDO2 / WebAuthn and PIV / mTLS login handlers | go-qrdb |
| Hardware key signing and PIV slot management | go-xkms |
| `auth.Factor` adapters that put HSM-backed keys into go-qrdb's `FactorStore` | go-xkms |
| Tenant CRUD | go-quicbaas |
| Policy, ABAC and audit | go-quicbaas |
| OIDC discovery and federation | go-quicbaas |
| The user-management UI and the IAM admin surface | go-quicbaas |

Moving a concern down to go-qrdb or go-xkms removes the go-quicbaas copy once
the go-quicbaas side consumes the lower layer through `sdk/go`.

## Authentication on UI routes

Every UI route except `login`, `register` and `invite-activate` requires a
valid session. Those three are the `PUBLIC_ROUTES` set in
`cmd/quicbaas/frontend/src/views/MainApp.svelte`.

- **Validating the token.** On load, the UI validates the stored
  `quicbaas_token` with `client.auth.authValidate`. It validates again on every
  `storage` event for that key, so a logout in one tab also signs out the other
  tabs.
- **The guard.** It waits until the validation result is known. If the session
  is unauthenticated and the route is not public, it redirects to `/login`.
  The same check decides whether the application chrome renders. There is no
  second flag that could drift from it.
- **Logout.** `client.auth.logout` revokes the session on the server. The UI
  clears its local state even if that call fails.

## What this does not do

- QuicBaaS keeps no user model of its own; identities are go-qrdb users.
- The UI guard is not an authorization check. Every API call is still
  authorized on the server.
