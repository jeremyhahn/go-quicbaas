# OAuth 2.1 / OIDC — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented guide to registering clients, obtaining tokens, and federating
> an external identity provider. Feature folder: `docs/oauth2-oidc/`.

This feature is two things that share a protocol:

1. **QuicBaaS as an OAuth 2.1 / OIDC provider** — it issues tokens to clients,
   including MCP clients, with mandatory PKCE and Dynamic Client Registration.
2. **QuicBaaS as a federation client** — it delegates authentication to an
   external IdP and maps the result onto a local identity.

## Prerequisites

- IAM enabled. OAuth clients are IAM applications and tokens carry IAM claims.
- TLS. Authorization-code flows over plaintext are not a supported
  configuration.
- For federation, an external IdP that will issue you a client id and secret,
  and a redirect URI it will accept.

## First run — register a client and get a token

```bash
# Register a confidential client.
quicbaas oauth2 client register

# List what exists.
quicbaas oauth2 client list

# Inspect one.
quicbaas oauth2 client show <client-id>
```

The verb is `register`, not `create`, and the read verb is `show`, not `get` —
`cmd/quicbaas/oauth2_client.go:154` and `:448`. The full group is `register`,
`list`, `show`, `update`, `delete`, `rotate-secret` and `pin-cert`.

The secret is shown **once**, at creation. There is no verb to read it back —
rotate instead:

```bash
quicbaas oauth2 client rotate-secret <client-id>
```

Rotation invalidates the previous secret immediately. Deploy the new one before
you rotate, not after.

## Common tasks

### Point an MCP client at this server

MCP clients discover the authorization server rather than being configured with
it. The discovery documents are served at their well-known paths and the flow is
OAuth 2.1 with **mandatory PKCE (S256)** and Resource Indicators (RFC 8707).
The authorization-code step does not complete yet, because `/oauth2/authorize`
does not authenticate the user: see the known gap in `api.md`.
`api.md` has the endpoint list and the exact discovery payloads.

Two things bite here:

- **Bearer header only.** Tokens in the request body — `_token`, `accessToken` —
  were removed in v1.1.0. A client still sending them gets an authentication
  failure that does not mention the body.
- **Audience matters.** With Resource Indicators in play, a token minted for one
  resource is not accepted at another. "The token is valid but rejected" is
  almost always an audience mismatch.

### Federate an external IdP

```bash
quicbaas iam provider create   # register the external IdP
quicbaas iam provider list
quicbaas iam provider get <provider-id>
```

Then link an existing local user to an external identity, or let first login do
it, depending on your policy:

```bash
quicbaas iam identity link
quicbaas iam identity list
quicbaas iam identity unlink
```

### Inspect a token

Token introspection answers "is this token live, and what does it carry" without
trusting the client's copy of the claims. `engineering.md` covers the
introspection endpoint and the audit records it emits.

### Revoke access

Revoking a *session* is IAM's job (`quicbaas iam session revoke`,
`revoke-all`, `revoke-others`). Deleting an OAuth *client* stops new tokens
being issued to it but does not retract tokens already outstanding — plan for
the token lifetime, not the deletion.

## Before you rely on this in production

**Verify the current state of ID-token validation before treating federated
login as an authentication boundary.** The design document for the identity
broker self-flags a critical hole: an OIDC `parseIDToken` path that does not
verify signature, `iss`, `aud` or `nonce`; `ClaimMappings.Groups` and `Roles`
ignored; federation storage in memory. That document's status is "design (for
review)", so it may describe a proposal rather than shipped code — **this
migration did not verify it against the implementation**, and it is recorded
here rather than quietly omitted because the failure mode if it is live is
accepting a forged assertion. Check it, or ask the owner, before depending on
federated identity.

The same document's in-memory federation storage claim carries the ordinary
consequence: provider registrations that do not survive a restart.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `invalid_client` on a flow that worked yesterday | The secret was rotated. There is no read-back; the new value is only shown at rotation. |
| Token accepted by one endpoint, rejected by another | Audience mismatch. Resource Indicators bind a token to a resource. |
| Authentication fails and the client is sending a token | Tokens must be in the `Authorization: Bearer` header. Body parameters were removed in v1.1.0. |
| PKCE errors from a client that "supports OAuth" | PKCE with S256 is mandatory, not negotiated. `plain` is not accepted. |
| Federated login succeeds but the user has no roles | Claim mappings for groups and roles may not be applied. See the caveat above. |
| Providers disappear after a restart | Federation storage may be in memory. See the caveat above. |

## What this feature does not do

- **It is not a user directory.** Users, groups and roles are IAM's
  (`../iam/`); this feature authenticates and issues tokens.
- **It is not the authorization engine.** Deciding what a token may do is
  `../authorization/`. A valid token is not an allow decision.
- **It does not proxy an IdP's own admin API.** Managing users inside Okta or
  Entra stays in Okta or Entra.
