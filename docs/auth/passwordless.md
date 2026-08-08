# Passwordless Authentication

QuicBaaS uses hardware-backed, phishing-resistant authentication exclusively. No user passwords are supported. All identity verification relies on hardware security keys, trusted platform modules, or platform authenticators (biometric/PIN).

## Why Passwordless

- **Phishing Resistant**: Credentials are bound to origin; attackers cannot intercept or replay them.
- **Hardware Attestation**: FIDO2/WebAuthn and TPM enable cryptographic proof of authenticator identity.
- **Standards Aligned**: Meets NIST 800-63B AAL3 (Authenticator Assurance Level 3) and FedRAMP requirements.
- **Zero Shared Secrets**: No password databases to breach; each authenticator holds its private key.

## Identity Matrix

| Identity Type | Auth Method | Hardware Requirement | Protocol |
|---|---|---|---|
| Human user | WebAuthn (passkey or security key) | FIDO2 authenticator (platform or roaming) | CTAP2/WebAuthn |
| Service account (cloud-native) | TPM IDevID + mTLS | TPM 2.0 | TLS 1.3 + RFC 8705 |
| Service account (on-prem) | xkey-issued mTLS cert | Hardware token (Trusted Platform Module) | TLS 1.3 + RFC 8705 |
| Service account (admin-minted) | mTLS client certificate (manual enrollment) | External PKI or admin distribution | TLS 1.3 + RFC 8705 |
| OAuth2 machine client | client_secret (RFC 6749 §2.3.1) | Not applicable | Bearer token in Authorization header |

**Note:** OAuth2 `client_secret` is **not a user password**. It is a machine credential identifying a confidential client application, as defined in RFC 6749 Section 2.3.1. Each new client app receives its own, and it can be rotated independently. Prefer TLS client authentication (`tls_client_auth` or `self_signed_tls_client_auth` per RFC 8705) when the client can present a TLS client certificate.

## User Flow: Invite → Activate → Login

### 1. Admin Creates Pending User

```bash
quicbaas iam users create alice@example.com \
  --email alice@example.com \
  --name "Alice Smith" \
  --tenant acme-corp
```

User is created in `pending` status. No authenticator is enrolled yet.

### 2. Invite URL Sent to User

Admin or system sends user an invite URL:

```
https://quicbaas.example.com/auth/invite?token=<signed-jwt>&user_id=<uuid>
```

The token contains the user ID, tenant ID, and a short expiration (e.g., 24 hours). The user accesses the link in a modern browser.

### 3. User Enrolls Authenticator

On the invite page, user is prompted to enroll their first passkey or security key:

- **Platform Authenticator** (recommended): Touch ID (macOS/iOS), Windows Hello, Android biometric
- **Roaming Authenticator**: YubiKey, Google Titan, Solokey (USB/NFC/BLE)

Browser's WebAuthn API guides the enrollment. The authenticator creates a public–private key pair; the public key is stored in QuicBaaS, and the private key never leaves the hardware.

### 4. User Logs In

User visits login page and selects their identity provider:

- **Sign in with Passkey**: Prompts for platform or roaming authenticator
- **Sign in with Smartcard** (if PIV is enabled): Inserts PIV card and enters PIN

No username or password field exists. The server returns a list of allowed credentials for the user after they verify their identity.

### 5. Headless Login (CLI)

The same passkey works without a browser. `quicbaas auth login` drives the
unified login ceremony (`POST /api/v1/auth/login/begin` → CTAP2 assertion →
`POST /api/v1/auth/login/finish`) against a locally connected authenticator:

```bash
quicbaas auth login --method fido2 \
  --username admin@system.qrdb.local \
  --tenant system \
  --origin https://localhost:8443
```

The relying-party id is **not** a client flag: it is carried by the
server-issued `PublicKeyCredentialRequestOptions` the begin step returns, and
the authenticator binds the assertion to it. `--origin` is the only override,
for relying parties whose origin host differs from the RP id.

The ceremony is **tenant scoped**: the WebAuthn relying party (RP id + origins)
belongs to a tenant, so the server resolves the per-tenant relying party and
fails closed when the request carries no tenant — it never falls back to a
default tenant, which would break tenant isolation. The CLI takes that scope
from the global `--tenant` flag, which defaults to the `system` tenant.

Login resolves the factor enrolled in the qrdb auth-factor store, and **every**
WebAuthn registration path writes that factor: the server enrolls the fido2
factor as it persists the credential, so a passkey registered in the browser
(self-registration, invite activation, the IAM console) is immediately usable
for headless CLI/SDK login. `quicbaas auth factor add --type fido2` runs the
same registration ceremony from a locally connected authenticator and prints
the credential id — the factor lookup key `auth login` submits on finish.

## Service Account Flow

Service accounts (non-human identities) authenticate via **mTLS with a bound access token**.

### Register a Service Account with Self-Signed TLS

```bash
quicbaas oauth2 client register \
  --client-name "my-app" \
  --auth-method self_signed_tls_client_auth \
  --cert-pem /path/to/client.crt \
  --key-pem /path/to/client.key
```

Returns:
```json
{
  "client_id": "svc_abc123xyz",
  "auth_method": "self_signed_tls_client_auth",
  "cnf": {
    "x5t#S256": "dBjftJeZ4CVP-mB92K27uhbUJU6OvF..."
  }
}
```

The `cnf.x5t#S256` is the SHA-256 thumbprint of the certificate (RFC 8705 §3.1). Every mTLS connection must present this certificate.

### Obtain Access Token

The client application establishes a TLS connection and requests a token:

```bash
curl -X POST https://quicbaas.example.com/oauth2/token \
  --cert /path/to/client.crt \
  --key /path/to/client.key \
  --cacert /path/to/ca.crt \
  -d "grant_type=client_credentials&client_id=svc_abc123xyz"
```

The server validates:
1. TLS connection is mutually authenticated
2. Client certificate matches the enrolled `cnf.x5t#S256`
3. Client ID is valid and not revoked

Returns:
```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIsImtpZCI6IjEiLCJ...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "cnf": {
    "x5t#S256": "dBjftJeZ4CVP-mB92K27uhbUJU6OvF..."
  }
}
```

The token's `cnf` claim is bound to the certificate. Token theft is useless without possession of the private key.

## Trusted Computing Layer

### FIDO2 / WebAuthn Attestation

When a user enrolls a passkey or security key, QuicBaaS verifies attestation (when configured):

- **Attestation Mode: None** (default): No verification. Allows platform authenticators and consumer devices.
- **Attestation Mode: Indirect**: Authenticator provides attestation certificate. Server verifies it against a trusted CA.
- **Attestation Mode: Direct**: Authenticator signs attestation data. Server verifies the signature chain to a root CA.

Enroll with FIDO2 devices and inspect the AAGUID (Authenticator AAGUID from `https://fidoalliance.org/metadata/`):

```yaml
iam:
  auth:
    webauthn:
      attestation: "direct"
      aaguid_allowlist:
        - "00000000-0000-0000-0000-000000000000"  # Platform (all)
        - "08987058-cadc-4b81-b6e1-30de50dcbe96"  # YubiKey 5
        - "73e4b956-e4cb-49b8-930b-88ef4cb4ec8a"  # Google Titan
```

### Administrator Certificate Bindings

Binding a client certificate to an administrator — either the non-interactive
`auth.bootstrap_admin_piv_cert_path` path or the init-ceremony SO certificate —
enrolls an **mtls auth factor** alongside the user-side thumbprint. Both writes
are required and serve different lookups:

| Write | Store | Consumer |
|---|---|---|
| mtls auth factor | qrdb `UserAuthFactor` store | the unified login ceremony (`auth login --method mtls`) |
| PIV cert thumbprint | user-side thumbprint index | the REST PIV login handler's user resolution |

The factor's credential is the certificate digest the login path derives from the
verified TLS peer certificate: `SHA-256(cert DER)` plus the renewal-stable
`SHA-256(SubjectPublicKeyInfo)` secondary key, so a certificate renewed with the
same keypair keeps working without re-binding.

The enrollment happens **before** the binding is persisted. A failure to enroll
therefore leaves nothing written and the operator sees a clean no-op, rather than
a bound certificate that authenticates the TLS handshake but cannot complete a
login. Re-binding the same certificate is idempotent; binding a certificate that
is already enrolled to a *different* user fails closed.

### TPM Attestation

For service accounts on premises, the server validates TPM attestation:

1. Client sends TPM-signed quote of the EK (Endorsement Key) public key
2. Server verifies the quote against the TPM EK root pool
3. Server issues a token bound to the EK public key hash (`cnf.x5t#S256`)

### xkey Root CA Pool

For xkey-issued certificates, QuicBaaS maintains a trusted root pool:

```yaml
iam:
  auth:
    mtls:
      xkey_ca_roots:
        - |
          -----BEGIN CERTIFICATE-----
          MIIDXTCCAkWgAwIBAgIJALgLyLahbdLAMA0GCSqGSIb3DQEBCwUAMEUxCzAJ
          ...
          -----END CERTIFICATE-----
```

## id_token Claims

When a user authenticates, QuicBaaS issues an `id_token` (JWT) with the following claims:

```json
{
  "sub": "user_abc123",
  "iss": "https://quicbaas.example.com",
  "aud": "client_id_xyz",
  "iat": 1234567890,
  "exp": 1234571490,
  "acr": "urn:mace:incommon:iap:silver",
  "amr": ["webauthn", "hwk"],
  "attest_method": "webauthn",
  "attest_aaguid": "00000000-0000-0000-0000-000000000000"
}
```

| Claim | Meaning |
|---|---|
| `acr` | Authentication Context Class Reference (NIST AAL level) |
| `amr` | Authentication Methods References: `webauthn`, `smartcard`, `mtls`, `hwk` (hardware key), `mfa` (multi-factor) |
| `attest_method` | `webauthn`, `tpm`, `xkey`, or `manual` |
| `attest_aaguid` | FIDO2 authenticator GUID (for WebAuthn only) |

## OIDC Discovery

QuicBaaS publishes authentication capabilities at standard discovery endpoints:

```bash
curl https://quicbaas.example.com/.well-known/openid-configuration
```

Key metadata:

```json
{
  "issuer": "https://quicbaas.example.com",
  "token_endpoint": "https://quicbaas.example.com/oauth2/token",
  "authorization_endpoint": "https://quicbaas.example.com/oauth2/authorize",
  "userinfo_endpoint": "https://quicbaas.example.com/oauth2/userinfo",
  "jwks_uri": "https://quicbaas.example.com/.well-known/jwks.json",
  "token_endpoint_auth_methods_supported": [
    "client_secret_basic",
    "client_secret_post",
    "tls_client_auth",
    "self_signed_tls_client_auth"
  ],
  "response_types_supported": [
    "code"
  ],
  "grant_types_supported": [
    "authorization_code",
    "refresh_token",
    "client_credentials"
  ]
}
```

**Deprecated (removed):** `password` and `implicit` grant types are no longer supported. OAuth 2.1 obsoletes them; QuicBaaS is passwordless-first.

## What's Removed

- **User Passwords**: No `password` field in user creation or login. All user authentication is WebAuthn or smartcard.
- **OAuth2 Password Grant**: The `password` grant type (RFC 6749 §4.3) is removed. Use `authorization_code` with PKCE instead.
- **OAuth2 Implicit Grant**: Removed per OAuth 2.1. Use `authorization_code` with PKCE.
- **POST /api/v1/auth/login**: Returns 404. Replaced by WebAuthn `/webauthn/login/begin` and `/webauthn/login/finish`.

## Configuration

Enable passwordless auth in QuicBaaS config:

```yaml
iam:
  enabled: true
  auth:
    webauthn:
      enabled: true
      rp_id: "quicbaas.example.com"
      rp_name: "QuicBaaS"
      rp_origins:
        - "https://quicbaas.example.com"
      attestation: "none"
      user_verification: "preferred"
      timeout: 60s
```

> **Defaults / derivation.** When `rp_id` and `rp_origins` are omitted they are
> derived from the canonical server issuer URL (the same value used for the
> OIDC issuer claim and the first-boot activation banner): `rp_id` becomes the
> issuer hostname and `rp_origins` becomes the issuer origin **including the
> port** (default scheme ports 443/80 are omitted to match the browser-sent
> `Origin` header). In the stock dev setup (`server.host: 127.0.0.1`, REST port
> 18080) this yields `rp_id: localhost` and `rp_origins: [https://localhost:18080]`
> — loopback IPs are canonicalised to `localhost` because a WebAuthn RP ID must
> be a domain, not an IP literal. If `server.host` is an explicit routable IP
> literal and no explicit `rp_id` is set, WebAuthn initialisation fails closed
> with a clear validation error. Explicit `rp_id` / `rp_origins` always win.

```yaml
# (continued)

  # Optional: PIV smartcard
  piv:
    enabled: false

  # Optional: TPM attestation
  tpm:
    enabled: false
    ek_ca_roots: [...]

  # Optional: xkey integration
  xkey:
    enabled: false
    ca_roots: [...]
```

## Troubleshooting

| Issue | Cause | Solution |
|---|---|---|
| "Authenticator not allowed" | AAGUID is not in allowlist | Add the device AAGUID to `iam.auth.webauthn.aaguid_allowlist` or disable attestation |
| "Certificate verification failed" | mTLS client cert not enrolled | Re-register the service account with `quicbaas oauth2 client register` |
| "Token bound to wrong credential" | `cnf.x5t#S256` mismatch | Ensure client always presents the enrolled certificate; check cert renewal |
| "Challenge expired" | WebAuthn ceremony took >60s | Increase `iam.auth.webauthn.timeout` or prompt user to retry |
| `auth login` returns bare `UNAUTHORIZED` at begin | Login sent no tenant scope; the per-tenant WebAuthn relying party fails closed | Pass `--tenant <id>` (defaults to `system`) |
| `auth login` returns `UNAUTHORIZED` at finish | No fido2 factor is enrolled for the credential in the qrdb auth-factor store | Enroll with `quicbaas auth factor add --type fido2` |
| `auth login --method mtls` returns `UNAUTHORIZED` at finish | The certificate is not enrolled as an mtls factor — it was never bound, or the binding predates the write-through | Re-run the binding (`auth.bootstrap_admin_piv_cert_path` on first boot, or the SO-cert init ceremony), or enroll explicitly with `quicbaas auth factor add --type mtls --cert <path>` |

## References

- [FIDO2 / WebAuthn Spec](https://www.w3.org/TR/webauthn-2/)
- [NIST 800-63B](https://pages.nist.gov/800-63-3/sp800-63b.html) – Authenticator Assurance Level 3
- [RFC 6749](https://tools.ietf.org/html/rfc6749) – OAuth 2.0 Authorization Framework
- [RFC 8705](https://tools.ietf.org/html/rfc8705) – OAuth 2.0 Mutual-TLS Client Authentication
- [OIDC Core](https://openid.net/specs/openid-connect-core-1_0.html)
