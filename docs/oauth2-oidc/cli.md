# OAuth 2.1 / OIDC — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag, with runnable examples. Feature folder: `docs/oauth2-oidc/`.

Manage OAuth2 clients (applications) for service-account and web-app
access to QuicBaaS. Clients are tenant-scoped via the global `--tenant`
flag.

`quicbaas oauth2 client` is the mTLS-aware counterpart of
`quicbaas iam app`: it exposes the same OAuth2 application lifecycle plus
the RFC 8705 certificate-binding verbs.

Group-level flags:

| Flag | Description |
|------|-------------|
| `--output json\|table` | Output format (default `table`) |
| `-y`, `--yes` | Skip confirmation prompts |

## Subcommands

| Command | Description |
|---------|-------------|
| `client register` (alias `create`) | Register a new OAuth2 client |
| `client list` | List clients in the tenant |
| `client show <client-id>` | Show one client |
| `client update <client-id>` | Update client metadata |
| `client rotate-secret <client-id>` | Issue a new client secret |
| `client delete <client-id>` | Delete a client and revoke its credentials |
| `client pin-cert <client-id>` | Pin or rotate the mTLS certificate |

## quicbaas oauth2 client create

| Flag | Description |
|------|-------------|
| `--name <name>` | Client display name (**required**) |
| `--grant-type <type>` | Allowed grant type, repeatable (default `client_credentials`) |
| `--redirect-uri <uri>` | Allowed redirect URI, repeatable (required for `authorization_code`) |
| `--response-type <type>` | Allowed response type, repeatable (e.g. `code`) |
| `--resource-prefix <prefix>` | Resource namespace the client owns, used by ABAC policies |
| `--scope <scope>` | Allowed scope, repeatable |
| `--auth-method <method>` | `client_secret_basic` (default), `client_secret_post`, `tls_client_auth`, `self_signed_tls_client_auth` |
| `--cert-pem <path>` | PEM client certificate for the mTLS auth methods |
| `--cert-thumbprint <b64url>` | SHA-256 thumbprint, as an alternative to `--cert-pem` |

Valid `--grant-type` values: `authorization_code`,
`client_credentials`, `refresh_token`, `implicit`, `password`,
`urn:ietf:params:oauth:grant-type:device_code`. An unrecognized value is
rejected — it is **not** silently coerced to `authorization_code`.

```bash
# Machine-to-machine client
quicbaas oauth2 client create --tenant acme --name "my-service" \
  --grant-type client_credentials

# Web application
quicbaas oauth2 client create --tenant acme --name "portal" \
  --grant-type authorization_code --grant-type refresh_token \
  --redirect-uri https://portal.acme.com/callback \
  --response-type code

# Self-signed mTLS client (RFC 8705 §2.2)
quicbaas oauth2 client create --tenant acme --name "gpu-agent" \
  --auth-method self_signed_tls_client_auth --cert-pem ./client.pem
```

### The client secret is shown once

Secret-based clients print their `client_secret` exactly once, with a
`!! STORE THIS SECRET NOW !!` warning. The server persists only a hash;
the secret cannot be recovered or redisplayed. The only remedy for a lost
secret is `rotate-secret`, which invalidates the current one.

## quicbaas oauth2 client update

Accepts the same metadata flags as `create`. Only the flags you pass are
applied; repeatable flags **replace** the stored list rather than
appending to it. An omitted `--grant-type` leaves the stored grant types
untouched.

```bash
quicbaas oauth2 client update app_abc123 --name "Portal (prod)"
quicbaas oauth2 client update app_abc123 \
  --redirect-uri https://portal.acme.com/callback \
  --redirect-uri https://portal.acme.com/callback2
```

## quicbaas oauth2 client rotate-secret

```bash
quicbaas oauth2 client rotate-secret app_abc123 --tenant acme --yes
```

Prompts for confirmation unless `--yes` is passed. Any deployment still
using the previous secret stops authenticating as soon as the command
succeeds.

## Known server-side limitations

The SDK request carries more metadata than the current server accepts.
These fields are transmitted but **not persisted** by the REST or gRPC
application handlers, so the CLI deliberately does not expose flags for
them:

- `description`, `contacts`, `logo_uri`, `policy_uri`, `tos_uri`
- `access_token_ttl`, `refresh_token_ttl`, `id_token_ttl` — token
  lifetimes are configured per tenant, not per client
- `scopes` — accepted by the SDK and sent on the wire, but the REST
  application handler does not store them yet
- `token_endpoint_auth_method` and the mTLS binding fields
  (`mtls_cert_thumbprint`, `mtls_subject_dn`) — honoured by the agent
  registration path, not yet by the generic application handler
- `pkce_required` — accepted by REST, dropped by the gRPC handler

Until those handlers are extended, `--auth-method` and `--cert-pem`
affect the request the CLI builds but may not change stored state.
