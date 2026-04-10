# WebAuthn / FIDO2 Authentication

## Overview

QuicBaaS IAM provides passwordless authentication via WebAuthn/FIDO2, supporting hardware security keys (YubiKey, etc.) and platform authenticators (Touch ID, Windows Hello, Android biometrics).

## Features

- **Passkey Support**: Platform authenticators (Touch ID, Face ID, Windows Hello)
- **Hardware Keys**: USB, NFC, and BLE security keys
- **Multi-Credential**: Users can register multiple authenticators
- **Credential Management**: Rename, list, and delete credentials
- **Sign Count Tracking**: Clone detection for hardware keys
- **Attestation Options**: None, indirect, or direct attestation

## Quick Start

### Configuration

Enable WebAuthn in your QuicBaaS IAM configuration:

```yaml
iam:
  enabled: true
  auth:
    webauthn:
      enabled: true
      rp_id: "example.com"
      rp_name: "Example Application"
      rp_origins:
        - "https://example.com"
        - "https://app.example.com"
      attestation: "none"        # none, indirect, direct
      user_verification: "preferred"  # required, preferred, discouraged
      timeout: 60s
```

### Go SDK Usage

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/iam"

// Create WebAuthn manager
config := &iam.WebAuthnConfig{
    RPID:             "example.com",
    RPName:           "Example Application",
    RPOrigins:        []string{"https://example.com"},
    Attestation:      "none",
    UserVerification: "preferred",
    Timeout:          60 * time.Second,
}

deps := iam.WebAuthnDependencies{
    CredentialStorage: credStorage,
    IdentityStore:     identityStore,
    SessionStorage:    sessionStore,
}

manager, err := iam.NewWebAuthnManager(config, deps)
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

Initiate credential registration for an authenticated user.

```http
POST /api/v1/tenants/{tenant_id}/webauthn/register/begin
Authorization: Bearer <access_token>
```

Response:
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

Submit the authenticator's attestation response. The credential data uses the standard WebAuthn format with proper base64url encoding.

```http
POST /api/v1/tenants/{tenant_id}/webauthn/register/finish
Authorization: Bearer <access_token>
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

Response:
```json
{
  "id": "credential-id-base64url",
  "user_id": "user_abc123",
  "name": "Authenticator 2025-01-07",
  "created_at": "2025-01-07T10:30:00Z",
  "transports": ["internal"]
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

Initiate WebAuthn authentication for a user.

```http
POST /api/v1/tenants/{tenant_id}/webauthn/login/begin
Content-Type: application/json

{
  "email": "alice@example.com"
}
```

Response:
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

Submit the authenticator's assertion response.

```http
POST /api/v1/tenants/{tenant_id}/webauthn/login/finish
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

Response:
```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "refresh_token": "dGhpcyBpcyBhIHJlZnJlc2g...",
  "user": {
    "id": "user_abc123",
    "email": "alice@example.com",
    "name": "Alice Smith"
  }
}
```

### Credential Management

#### List Credentials

```http
GET /api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials
Authorization: Bearer <access_token>
```

Response:
```json
{
  "credentials": [
    {
      "id": "credential-id-1",
      "name": "MacBook Touch ID",
      "created_at": "2025-01-01T10:00:00Z",
      "last_used_at": "2025-01-07T08:30:00Z",
      "transports": ["internal"]
    },
    {
      "id": "credential-id-2",
      "name": "YubiKey 5",
      "created_at": "2025-01-05T14:00:00Z",
      "last_used_at": null,
      "transports": ["usb", "nfc"]
    }
  ]
}
```

#### Rename Credential

```http
PUT /api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials/{credential_id}
Authorization: Bearer <access_token>
Content-Type: application/json

{
  "name": "Work Laptop Touch ID"
}
```

#### Delete Credential

```http
DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials/{credential_id}
Authorization: Bearer <access_token>
```

## Frontend Integration

### JavaScript/TypeScript

Using the Web Authentication API:

```javascript
// Registration
async function registerWebAuthn() {
  // 1. Get options from server
  const beginResponse = await fetch('/api/v1/tenants/my-tenant/webauthn/register/begin', {
    method: 'POST',
    headers: { 'Authorization': `Bearer ${token}` }
  });
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

  // 5. Send to server
  const finishResponse = await fetch('/api/v1/tenants/my-tenant/webauthn/register/finish', {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(body)
  });

  return finishResponse.json();
}

// Authentication
async function loginWebAuthn(email) {
  // 1. Get options from server
  const beginResponse = await fetch('/api/v1/tenants/my-tenant/webauthn/login/begin', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email })
  });
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

  // 5. Send to server
  const finishResponse = await fetch('/api/v1/tenants/my-tenant/webauthn/login/finish', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
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

  const deleteCredential = async (credentialId) => {
    await fetch(`/api/v1/tenants/my-tenant/users/me/webauthn/credentials/${credentialId}`, {
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

### Storage Interface

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
