# WebAuthn / FIDO2 Authentication

## Overview

QuicBaaS IAM provides passwordless authentication via WebAuthn/FIDO2,
supporting hardware security keys (YubiKey, etc.) and platform
authenticators (Touch ID, Windows Hello, Android biometrics).

**Phase 5 update (2026-05-07).** The WebAuthn relying-party logic is no
longer reimplemented inside `pkg/iam`. The canonical implementation
lives in `go-xkms/pkg/webauthn`; quicbaas wraps it with a thin
multi-tenant manager (`iam.XKMSWebAuthnManager`) and per-tenant
service factory (`iam.WebAuthnServiceFactory`). User and credential
persistence flows through xkms's qrdb adapter
(`xkms.NewQRDBUserStoreAdapter`) so each tenant has an isolated user
namespace inside the platform tenant's qrdb keyspace.

REST surface is mounted at one canonical path:

- `/api/v1/iam/{tenant_id}/webauthn/{registration,login}/{begin,finish}`
  (plus `registration/status`), served by xkms's native WebAuthn HTTP
  handler via the per-tenant factory.

The legacy `/api/v1/tenants/{tenant_id}/webauthn/*` mount (including
its credential-management endpoints) was deleted. Browser and
TypeScript-SDK login now uses the unified login ceremony at
`/api/v1/auth/login/{begin,finish}` (`type: fido2`), which mints the
OIDC bearer + refresh token; the xkms mount remains the ceremony
surface the Go SDK's `Security().Auth().FIDO2()` REST transport
drives.

## Features

- **Passkey Support**: Platform authenticators (Touch ID, Face ID, Windows Hello)
- **Hardware Keys**: USB, NFC, and BLE security keys
- **Multi-Credential**: Users can register multiple authenticators
- **Credential Management**: Rename, list, and delete credentials
- **Sign Count Tracking**: Clone detection for hardware keys
- **Attestation Options**: None, indirect, or direct attestation

## Quick Start

### Configuration

Configure the relying-party in your QuicBaaS configuration:

```yaml
iam:
  webauthn:
    rp_id: "example.com"
    rp_display_name: "Example Application"
    rp_origins:
      - "https://example.com"
      - "https://app.example.com"
    timeout: "60s"
    user_verification: "preferred"        # required | preferred | discouraged
    attestation_preference: "none"        # none | indirect | direct
    resident_key_requirement: "preferred" # discouraged | preferred | required
    authenticator_attachment: ""          # platform | cross-platform | "" (any)
    debug: false
  aaguid:
    mode: "log_only"                      # log_only | enforce
    allowlist_file: "/etc/quicbaasd/aaguid.json"
```

The `iam.webauthn.*` keys are validated at daemon startup
(`config.IAMWebAuthnConfig.Validate`). Production deployments must
provide an explicit `rp_id`, `rp_display_name`, and at least one
`rp_origin`; the dev defaults (`localhost`, `QuicBaaS`,
`https://localhost`) are applied only when the operator omits the
field entirely.

### Go SDK Usage

```go
import (
    "github.com/jeremyhahn/go-quicbaas/pkg/iam"
    qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// Build the per-tenant WebAuthn factory. The qrdb client is required:
// each tenant's user + credential records are persisted via xkms's
// qrdb adapter pair under the platform tenant's KV namespace.
factory, err := iam.NewWebAuthnServiceFactory(iam.WebAuthnFactoryParams{
    Config: iam.WebAuthnFactoryConfig{
        RPID:          "example.com",
        RPDisplayName: "Example Application",
        RPOrigins:     []string{"https://example.com"},
        Timeout:       60 * time.Second,
    },
    QRDB: qrdbClient,
})
if err != nil {
    return err
}

// Wrap the factory in the multi-tenant manager. The manager
// implements service.WebAuthnManager and is the surface the REST
// handlers consume.
manager, err := iam.NewXKMSWebAuthnManager(factory)
if err != nil {
    return err
}

// Optionally apply an AAGUID allowlist. The manager intercepts
// FinishRegistration and rejects credentials whose AAGUID is not
// permitted (in strict mode).
manager.SetAAGUIDAllowlist(allowlist)
```

## REST API Flow

### Registration Flow

The registration flow creates a new WebAuthn credential for a user.

```
Client                    Server                    Authenticator
  |                          |                           |
  |  1. POST /register/begin |                           |
  |------------------------->|                           |
  |                          |                           |
  |  2. CredentialCreation   |                           |
  |<-------------------------|                           |
  |                          |                           |
  |  3. Create Credential    |                           |
  |-------------------------------------------------->   |
  |                          |                           |
  |  4. Attestation Response |                           |
  |<--------------------------------------------------|  |
  |                          |                           |
  |  5. POST /register/finish|                           |
  |------------------------->|                           |
  |                          |                           |
  |  6. Credential Stored    |                           |
  |<-------------------------|                           |
```

#### Step 1: Begin Registration

Initiate credential registration for an authenticated user. The
opaque ceremony session id is returned in the `X-Session-Id` response
header and must be echoed on finish.

```http
POST /api/v1/iam/{tenant_id}/webauthn/registration/begin
Authorization: Bearer <access_token>
Content-Type: application/json

{
  "email": "alice@example.com",
  "display_name": "Alice Smith"
}
```

Response (`X-Session-Id: <session>` header + options body):
```json
{
  "publicKey": {
    "challenge": "dGhpcyBpcyBhIHJhbmRvbSBjaGFsbGVuZ2U",
    "rp": {
      "id": "example.com",
      "name": "Example Application"
    },
    "user": {
      "id": "dXNlcl9hYmMxMjM",
      "name": "alice@example.com",
      "displayName": "Alice Smith"
    },
    "pubKeyCredParams": [
      {"type": "public-key", "alg": -7},
      {"type": "public-key", "alg": -257}
    ],
    "timeout": 60000,
    "attestation": "none",
    "excludeCredentials": [
      {
        "type": "public-key",
        "id": "existing-credential-id"
      }
    ]
  }
}
```

#### Step 2: Complete Registration

Submit the authenticator's attestation response — the raw
`navigator.credentials.create()` PublicKeyCredential JSON is the
request body, correlated by the `X-Session-Id` header from begin.

```http
POST /api/v1/iam/{tenant_id}/webauthn/registration/finish
Authorization: Bearer <access_token>
X-Session-Id: <session>
Content-Type: application/json

{
  "id": "credential-id-base64url",
  "rawId": "credential-id-base64url",
  "type": "public-key",
  "response": {
    "clientDataJSON": "eyJ0eXBlIjoid2ViYXV0aG4uY3JlYXRlIi...",
    "attestationObject": "o2NmbXRkbm9uZWdhdHRTdG10oGhh..."
  },
  "authenticatorAttachment": "platform"
}
```

**Credential Parsing**: The server parses the attestation response using the `go-webauthn/webauthn` library's `protocol.ParseCredentialCreationResponse` function, which handles:
- Base64url decoding of `id` and `rawId` fields
- Parsing of `clientDataJSON` to extract challenge, origin, and type
- CBOR decoding of `attestationObject` to extract authenticator data
- Public key extraction and COSE key parsing
- Attestation verification (when attestation is not "none")

Response (xkms auth response):
```json
{
  "token": "<post-registration token>",
  "user_id": "dXNlcl9hYmMxMjM"
}
```

### Authentication Flow

The login flow authenticates a user with their WebAuthn credential.

```
Client                    Server                    Authenticator
  |                          |                           |
  |  1. POST /login/begin    |                           |
  |------------------------->|                           |
  |                          |                           |
  |  2. CredentialAssertion  |                           |
  |<-------------------------|                           |
  |                          |                           |
  |  3. Get Assertion        |                           |
  |-------------------------------------------------->   |
  |                          |                           |
  |  4. Signed Assertion     |                           |
  |<--------------------------------------------------|  |
  |                          |                           |
  |  5. POST /login/finish   |                           |
  |------------------------->|                           |
  |                          |                           |
  |  6. Session Token        |                           |
  |<-------------------------|                           |
```

#### Step 1: Begin Login

Initiate WebAuthn authentication for a user. This route is public —
WebAuthn IS the authentication mechanism. The ceremony session id is
returned in the `X-Session-Id` response header.

```http
POST /api/v1/iam/{tenant_id}/webauthn/login/begin
Content-Type: application/json

{
  "email": "alice@example.com"
}
```

Response (`X-Session-Id: <session>` header + options body):
```json
{
  "publicKey": {
    "challenge": "YW5vdGhlciByYW5kb20gY2hhbGxlbmdl",
    "timeout": 60000,
    "rpId": "example.com",
    "allowCredentials": [
      {
        "type": "public-key",
        "id": "credential-id-1",
        "transports": ["internal"]
      },
      {
        "type": "public-key",
        "id": "credential-id-2",
        "transports": ["usb", "nfc"]
      }
    ],
    "userVerification": "preferred"
  }
}
```

#### Step 2: Complete Login

Submit the authenticator's assertion response — the raw
`navigator.credentials.get()` PublicKeyCredential JSON is the request
body, correlated by the `X-Session-Id` header from begin.

```http
POST /api/v1/iam/{tenant_id}/webauthn/login/finish
X-Session-Id: <session>
Content-Type: application/json

{
  "id": "credential-id-base64url",
  "rawId": "credential-id-base64url",
  "type": "public-key",
  "response": {
    "clientDataJSON": "eyJ0eXBlIjoid2ViYXV0aG4uZ2V0Ii...",
    "authenticatorData": "SZYN5YgOjGh0NBcPZHZgW4...",
    "signature": "MEUCIQCYrNzWB..."
  }
}
```

**Credential Parsing**: The server parses the assertion response using the `go-webauthn/webauthn` library's `protocol.ParseCredentialRequestResponse` function, which handles:
- Base64url decoding of credential IDs
- Challenge verification against stored session
- Signature verification using the stored public key
- Sign count validation for clone detection
- User verification flag checking

Response (xkms auth response):
```json
{
  "token": "<post-login token>",
  "user_id": "dXNlcl9hYmMxMjM"
}
```

The xkms mount's login token is not an OIDC bearer. Applications that
need the full OIDC access/refresh token pair (the SPA, the TypeScript
SDK, the CLI) log in through the unified ceremony instead:
`POST /api/v1/auth/login/begin` (`type: fido2`) issues the WebAuthn
challenge and `POST /api/v1/auth/login/finish` verifies the assertion
server-side and mints the bearer + rotating refresh token.

### Credential Management

The legacy per-user credential CRUD endpoints
(`/api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials`)
were deleted with the legacy mount. Enrolled WebAuthn credentials are
managed as qrdb user auth factors via
`Security().Auth().Factors()` (list / remove / rotate / relabel) and
the `quicbaas auth factor` CLI.

## Frontend Integration

### JavaScript/TypeScript

Using the Web Authentication API:

```javascript
// Registration (canonical xkms mount; session travels in X-Session-Id)
async function registerWebAuthn(email, displayName) {
  // 1. Get options from server
  const beginResponse = await fetch('/api/v1/iam/my-tenant/webauthn/registration/begin', {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({ email, display_name: displayName })
  });
  const sessionId = beginResponse.headers.get('X-Session-Id');
  const options = await beginResponse.json();

  // 2. Decode base64url fields for the browser API
  options.publicKey.challenge = base64urlToBuffer(options.publicKey.challenge);
  options.publicKey.user.id = base64urlToBuffer(options.publicKey.user.id);
  if (options.publicKey.excludeCredentials) {
    options.publicKey.excludeCredentials = options.publicKey.excludeCredentials.map(c => ({
      ...c,
      id: base64urlToBuffer(c.id)
    }));
  }

  // 3. Create credential with authenticator
  const credential = await navigator.credentials.create(options);

  // 4. Encode response for server
  const body = {
    id: credential.id,
    rawId: bufferToBase64url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: bufferToBase64url(credential.response.clientDataJSON),
      attestationObject: bufferToBase64url(credential.response.attestationObject)
    }
  };

  // 5. Send to server (raw PublicKeyCredential JSON, session header)
  const finishResponse = await fetch('/api/v1/iam/my-tenant/webauthn/registration/finish', {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Content-Type': 'application/json',
      'X-Session-Id': sessionId
    },
    body: JSON.stringify(body)
  });

  return finishResponse.json();
}

// Authentication (ceremony-only; for an OIDC bearer use
// POST /api/v1/auth/login/{begin,finish} with type "fido2" instead)
async function loginWebAuthn(email) {
  // 1. Get options from server
  const beginResponse = await fetch('/api/v1/iam/my-tenant/webauthn/login/begin', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email })
  });
  const sessionId = beginResponse.headers.get('X-Session-Id');
  const options = await beginResponse.json();

  // 2. Decode base64url fields
  options.publicKey.challenge = base64urlToBuffer(options.publicKey.challenge);
  options.publicKey.allowCredentials = options.publicKey.allowCredentials.map(c => ({
    ...c,
    id: base64urlToBuffer(c.id)
  }));

  // 3. Get assertion from authenticator
  const assertion = await navigator.credentials.get(options);

  // 4. Encode response for server
  const body = {
    id: assertion.id,
    rawId: bufferToBase64url(assertion.rawId),
    type: assertion.type,
    response: {
      clientDataJSON: bufferToBase64url(assertion.response.clientDataJSON),
      authenticatorData: bufferToBase64url(assertion.response.authenticatorData),
      signature: bufferToBase64url(assertion.response.signature)
    }
  };

  // 5. Send to server (raw PublicKeyCredential JSON, session header)
  const finishResponse = await fetch('/api/v1/iam/my-tenant/webauthn/login/finish', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Session-Id': sessionId
    },
    body: JSON.stringify(body)
  });

  return finishResponse.json();
}

// Helper functions
function base64urlToBuffer(base64url) {
  const base64 = base64url.replace(/-/g, '+').replace(/_/g, '/');
  const padding = '='.repeat((4 - base64.length % 4) % 4);
  const binary = atob(base64 + padding);
  return Uint8Array.from(binary, c => c.charCodeAt(0));
}

function bufferToBase64url(buffer) {
  const bytes = new Uint8Array(buffer);
  let binary = '';
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}
```

### React Integration

```jsx
import { useState } from 'react';

function WebAuthnManager() {
  const [credentials, setCredentials] = useState([]);
  const [loading, setLoading] = useState(false);

  const registerPasskey = async () => {
    setLoading(true);
    try {
      await registerWebAuthn();
      await loadCredentials();
    } catch (err) {
      console.error('Registration failed:', err);
    } finally {
      setLoading(false);
    }
  };

  const deleteCredential = async (factorId) => {
    // Enrolled passkeys are qrdb user auth factors; remove via the
    // auth-factor surface (see Security().Auth().Factors()).
    await fetch(`/v1/users/factors/${factorId}`, {
      method: 'DELETE',
      headers: { 'Authorization': `Bearer ${token}` }
    });
    await loadCredentials();
  };

  return (
    <div>
      <h2>Passkeys</h2>
      <button onClick={registerPasskey} disabled={loading}>
        {loading ? 'Registering...' : 'Add Passkey'}
      </button>

      <ul>
        {credentials.map(cred => (
          <li key={cred.id}>
            {cred.name}
            <span>Last used: {cred.last_used_at || 'Never'}</span>
            <button onClick={() => deleteCredential(cred.id)}>Remove</button>
          </li>
        ))}
      </ul>
    </div>
  );
}
```

## Credential Storage

Phase 5 moves credential persistence to xkms. quicbaas no longer
defines a `CredentialStorage` interface; the per-tenant xkms
WebAuthn service uses `xkms.NewQRDBUserStoreAdapter` to read and
write credentials through qrdb's KV client under the
`__xkms:webauthn/` keyspace prefix. Operators do not need to
configure a separate credential store — the platform's qrdb
deployment is the single source of truth.

The legacy storage interface documented in earlier revisions
(`iam.CredentialStorage`) has been removed; this section is kept
for historical reference only.

### Storage Interface (legacy / removed)

The WebAuthn manager uses a credential storage interface:

```go
type CredentialStorage interface {
    // Credential operations
    CreateCredential(ctx context.Context, credential *WebAuthnCredential) error
    GetCredential(ctx context.Context, tenantID, credentialID string) (*WebAuthnCredential, error)
    GetCredentialsByUser(ctx context.Context, tenantID, userID string) ([]*WebAuthnCredential, error)
    UpdateCredential(ctx context.Context, credential *WebAuthnCredential) error
    DeleteCredential(ctx context.Context, tenantID, credentialID string) error

    // Challenge operations
    StoreChallenge(ctx context.Context, challenge *WebAuthnChallenge) error
    GetChallenge(ctx context.Context, challengeID string) (*WebAuthnChallenge, error)
    DeleteChallenge(ctx context.Context, challengeID string) error
}
```

### Credential Structure

```go
type WebAuthnCredential struct {
    ID              string    // Base64url-encoded credential ID
    UserID          string    // Owner user ID
    TenantID        string    // Tenant scope
    Name            string    // User-friendly name
    PublicKey       []byte    // COSE-encoded public key
    AttestationType string    // none, indirect, direct
    AAGUID          []byte    // Authenticator GUID
    SignCount       uint32    // Signature counter
    Transports      []string  // usb, nfc, ble, internal
    CreatedAt       time.Time
    LastUsedAt      *time.Time
}
```

## Security Considerations

### Challenge Handling

- Challenges are cryptographically random (32 bytes)
- Challenges have a configurable TTL (default 60 seconds)
- Challenges are deleted after use (success or failure)
- Challenge replay is prevented by server-side validation

### Sign Count Verification

The sign count is tracked and verified to detect credential cloning:

```go
// During login, sign count is verified
if response.Authenticator.SignCount <= storedCredential.SignCount {
    // Potential clone detected
    // Log warning but may still allow if sign count is 0
}
```

### Attestation

| Mode | Security | Use Case |
|------|----------|----------|
| `none` | Basic | Consumer applications, passkeys |
| `indirect` | Moderate | Enterprise with some verification |
| `direct` | High | High-security environments |

### User Verification

| Mode | Description |
|------|-------------|
| `required` | Biometric/PIN always required |
| `preferred` | Use if available, fallback allowed |
| `discouraged` | Skip verification (not recommended) |

## Error Handling

### Common Errors

| Error | HTTP Status | Description |
|-------|-------------|-------------|
| `challenge_expired` | 400 | Challenge TTL exceeded |
| `challenge_not_found` | 400 | Challenge not found or already used |
| `credential_not_found` | 404 | Credential does not exist |
| `credential_invalid` | 400 | Credential validation failed |
| `user_not_found` | 404 | User not found in tenant |
| `registration_failed` | 400 | Registration verification failed |
| `authentication_failed` | 401 | Authentication verification failed |

### Error Response Format

```json
{
  "error": "challenge_expired",
  "error_description": "The WebAuthn challenge has expired. Please try again."
}
```

## Best Practices

1. **Always Use HTTPS**: WebAuthn requires secure contexts
2. **Match Origins**: RP origins must match exactly (including port for development)
3. **Handle Errors Gracefully**: Show user-friendly messages
4. **Provide Fallback**: Offer alternative authentication methods
5. **Encourage Multiple Credentials**: Users should register backup authenticators
6. **Clear Credential Names**: Help users identify their authenticators
7. **Monitor Sign Counts**: Alert on potential clone detection
8. **Regular Cleanup**: Remove expired challenges periodically

## See Also

- [API Reference](api-reference.md) - Complete API documentation
- [Authentication](../security/authentication.md) - General authentication guide
- [MFA](api-reference.md#mfa-api) - TOTP multi-factor authentication
