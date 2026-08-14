# QuicBaaS IAM API Reference

## OIDC Endpoints

### Well-Known Configuration

```
GET /.well-known/openid-configuration
```

Returns OpenID Connect discovery document.

### Authorization

```
GET /oauth2/authorize
```

Parameters:
| Parameter | Required | Description |
|-----------|----------|-------------|
| `client_id` | Yes | Application client ID |
| `redirect_uri` | Yes | Callback URL |
| `response_type` | Yes | `code` for authorization code flow |
| `scope` | Yes | Space-separated scopes |
| `state` | Recommended | CSRF protection |
| `code_challenge` | PKCE | Base64url-encoded SHA256 hash |
| `code_challenge_method` | PKCE | `S256` |

### Token Exchange

```
POST /oauth2/token
Content-Type: application/x-www-form-urlencoded
```

**Authorization Code Grant:**
```
grant_type=authorization_code
&code=<auth_code>
&redirect_uri=<callback_url>
&client_id=<client_id>
&client_secret=<client_secret>
&code_verifier=<pkce_verifier>
```

**Refresh Token Grant:**
```
grant_type=refresh_token
&refresh_token=<token>
&client_id=<client_id>
&client_secret=<client_secret>
```

**Client Credentials Grant:**
```
grant_type=client_credentials
&client_id=<client_id>
&client_secret=<client_secret>
&scope=<scopes>
```

Response:
```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "refresh_token": "dGhpcyBpcyBhIHJl...",
  "id_token": "eyJhbGciOiJSUzI1NiIs...",
  "scope": "openid profile email"
}
```

### User Info

```
GET /oauth2/userinfo
Authorization: Bearer <access_token>
```

Response:
```json
{
  "sub": "user_abc123",
  "name": "Alice Smith",
  "email": "alice@acme.com",
  "email_verified": true,
  "tenant_id": "acme-corp",
  "roles": ["developer", "team-lead"],
  "groups": ["engineering"]
}
```

### JWKS

```
GET /.well-known/jwks.json
```

Returns JSON Web Key Set for token verification.

---

## IAM Management API

### Tenants

**List Tenants**
```
GET /api/v1/tenants
Authorization: Bearer <admin_token>
```

**Create Tenant**
```
POST /api/v1/tenants
Authorization: Bearer <admin_token>
Content-Type: application/json

{
  "id": "acme-corp",
  "name": "Acme Corporation",
  "settings": {
    "max_users": 1000,
    "mfa_required": true
  }
}
```

**Get Tenant**
```
GET /api/v1/tenants/{tenant_id}
```

**Update Tenant**
```
PUT /api/v1/tenants/{tenant_id}
```

**Delete Tenant**
```
DELETE /api/v1/tenants/{tenant_id}
```

### Users

**List Users**
```
GET /api/v1/tenants/{tenant_id}/users
```

Query Parameters:
| Parameter | Description |
|-----------|-------------|
| `limit` | Max results (default 100) |
| `offset` | Pagination offset |
| `email` | Filter by email |
| `role` | Filter by role |
| `group` | Filter by group |

**Create User**
```
POST /api/v1/tenants/{tenant_id}/users
Content-Type: application/json

{
  "email": "alice@acme.com",
  "name": "Alice Smith",
  "roles": ["developer"],
  "groups": ["engineering"],
  "attributes": {
    "department": "engineering",
    "team_id": "team-west"
  }
}
```

**Get User**
```
GET /api/v1/tenants/{tenant_id}/users/{user_id}
```

**Update User**
```
PUT /api/v1/tenants/{tenant_id}/users/{user_id}
```

**Delete User**
```
DELETE /api/v1/tenants/{tenant_id}/users/{user_id}
```

**Assign Role**
```
POST /api/v1/tenants/{tenant_id}/users/{user_id}/roles
Content-Type: application/json

{
  "role": "admin"
}
```

**Remove Role**
```
DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/roles/{role}
```

### Groups

**List Groups**
```
GET /api/v1/tenants/{tenant_id}/groups
```

**Create Group**
```
POST /api/v1/tenants/{tenant_id}/groups
Content-Type: application/json

{
  "id": "engineering",
  "name": "Engineering Team",
  "description": "All engineering staff"
}
```

**Get Group**
```
GET /api/v1/tenants/{tenant_id}/groups/{group_id}
```

**Add User to Group**
```
POST /api/v1/tenants/{tenant_id}/groups/{group_id}/members
Content-Type: application/json

{
  "user_id": "user_abc123"
}
```

**Remove User from Group**
```
DELETE /api/v1/tenants/{tenant_id}/groups/{group_id}/members/{user_id}
```

### Roles

**List Roles**
```
GET /api/v1/tenants/{tenant_id}/roles
```

**Create Role**
```
POST /api/v1/tenants/{tenant_id}/roles
Content-Type: application/json

{
  "id": "team-lead",
  "name": "Team Lead",
  "description": "Can manage team resources"
}
```

### Applications (OAuth Clients)

**List Applications**
```
GET /api/v1/tenants/{tenant_id}/apps
```

**Register Application**
```
POST /api/v1/tenants/{tenant_id}/apps
Content-Type: application/json

{
  "name": "order-service",
  "redirect_uris": [
    "https://orders.acme.com/callback"
  ],
  "resource_prefix": "orders://",
  "grant_types": ["authorization_code", "refresh_token"],
  "response_types": ["code"]
}
```

Response:
```json
{
  "client_id": "app_ord_abc123xyz",
  "client_secret": "sk_live_xxxxxxxxxxxxx",
  "name": "order-service",
  "redirect_uris": ["https://orders.acme.com/callback"],
  "resource_prefix": "orders://"
}
```

**Rotate Client Secret**
```
POST /api/v1/tenants/{tenant_id}/apps/{client_id}/rotate-secret
```

---

## Policy API

**List Policies**
```
GET /api/v1/tenants/{tenant_id}/policies
```

**Create Policy**
```
POST /api/v1/tenants/{tenant_id}/policies
Content-Type: application/yaml

apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: order-access
  tenant: acme-corp
spec:
  rules:
    - name: read-own
      effect: allow
      actions: [read]
      resources:
        - orders://orders/${subject.id}/**
      subjects:
        authenticated: true
```

**Get Policy**
```
GET /api/v1/tenants/{tenant_id}/policies/{policy_name}
```

**Update Policy**
```
PUT /api/v1/tenants/{tenant_id}/policies/{policy_name}
```

**Delete Policy**
```
DELETE /api/v1/tenants/{tenant_id}/policies/{policy_name}
```

**Validate Policy (Dry Run)**
```
POST /api/v1/tenants/{tenant_id}/policies/validate
Content-Type: application/yaml
```

**Test Policy Decision**
```
POST /api/v1/tenants/{tenant_id}/policies/test
Content-Type: application/json

{
  "subject": {
    "id": "user_abc123",
    "roles": ["developer"],
    "groups": ["engineering"]
  },
  "action": "read",
  "resource": "orders://orders/123"
}
```

Response:
```json
{
  "allowed": true,
  "matched_policy": "order-access",
  "matched_rule": "read-own",
  "evaluation_time_us": 3
}
```

---

## WebAuthn API

WebAuthn provides passwordless authentication using hardware security keys and platform authenticators. For detailed integration guides and frontend examples, see the [WebAuthn Guide](webauthn.md).

The ceremony is served by the canonical xkms-native mount at
`/api/v1/iam/{tenant_id}/webauthn/*`. The legacy
`/api/v1/tenants/{tenant_id}/webauthn/*` surface (including its
per-user credential CRUD endpoints) was deleted. The opaque ceremony
session id travels in the `X-Session-Id` header: it is returned by
the begin verbs and must be echoed on the finish verbs.

### Begin Registration

Initiates WebAuthn credential registration for an authenticated user.
Authorization-gated (`user_authfactors:add`).

```
POST /api/v1/iam/{tenant_id}/webauthn/registration/begin
Authorization: Bearer <token>
Content-Type: application/json

{
  "email": "alice@example.com",
  "display_name": "Alice Smith"
}
```

Response: `X-Session-Id` header plus the JSON-marshaled
`PublicKeyCredentialCreationOptions` (`{"publicKey": {...}}`).

### Complete Registration

Completes registration. The request body is the raw
`navigator.credentials.create()` PublicKeyCredential JSON.

```
POST /api/v1/iam/{tenant_id}/webauthn/registration/finish
Authorization: Bearer <token>
X-Session-Id: <session>
Content-Type: application/json

{
  "id": "credential-id-base64url",
  "rawId": "credential-id-base64url",
  "type": "public-key",
  "response": {
    "clientDataJSON": "eyJ0eXBlIjoid2ViYXV0aG4uY3JlYXRlIi...",
    "attestationObject": "o2NmbXRkbm9uZWdhdHRTdG10oGhh..."
  }
}
```

Response:
```json
{
  "token": "<post-registration token>",
  "user_id": "dXNlcl9hYmMxMjM"
}
```

Registration write-through enrolls the credential as a qrdb
`fido2` user auth factor, so login can resolve it and the factor
appears under `GET /v1/users/factors/list`.

### Registration Status

```
GET /api/v1/iam/{tenant_id}/webauthn/registration/status?user_id=<user-id>
Authorization: Bearer <token>
```

### Begin Authentication

Initiates WebAuthn authentication for a user by email. Public — the
ceremony IS the authentication mechanism.

```
POST /api/v1/iam/{tenant_id}/webauthn/login/begin
Content-Type: application/json

{
  "email": "alice@example.com"
}
```

Response: `X-Session-Id` header plus the JSON-marshaled
`PublicKeyCredentialRequestOptions` (`{"publicKey": {...}}`).

### Complete Authentication

Completes authentication. The request body is the raw
`navigator.credentials.get()` PublicKeyCredential JSON.

```
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

Response:
```json
{
  "token": "<post-login token>",
  "user_id": "dXNlcl9hYmMxMjM"
}
```

The mount's login token is not an OIDC bearer. Clients that need the
OIDC access/refresh pair (SPA, TypeScript SDK, CLI) authenticate
through the unified login ceremony instead:
`POST /api/v1/auth/login/begin` (`type: fido2`) then
`POST /api/v1/auth/login/finish`, which verifies the assertion and
mints the bearer plus rotating refresh token.

### Credential Management

Enrolled passkeys are qrdb user auth factors. List, remove, rotate,
and relabel them via the auth-factor surface
(`/v1/users/factors/*` REST wire, `Security().Auth().Factors()` SDK,
`quicbaas auth factor` CLI).

---

## Session Management API

### List Sessions

```
GET /api/v1/iam/sessions
Authorization: Bearer <access_token>
```

Returns all active sessions for the authenticated user.

Response:
```json
{
  "sessions": [
    {
      "id": "sess_abc123",
      "created": "2025-01-15T10:30:00Z",
      "last_active": "2025-01-15T14:22:00Z",
      "expires_at": "2025-01-15T18:30:00Z",
      "mfa_verified": true,
      "ip_address": "192.168.1.100",
      "user_agent": "Mozilla/5.0...",
      "device_info": "Chrome on macOS"
    }
  ]
}
```

### Get Session

```
GET /api/v1/iam/sessions/{session_id}
Authorization: Bearer <access_token>
```

### Revoke Session

```
DELETE /api/v1/iam/sessions/{session_id}
Authorization: Bearer <access_token>
```

### Revoke All Sessions

```
DELETE /api/v1/iam/sessions
Authorization: Bearer <access_token>
```

Query Parameters:
| Parameter | Description |
|-----------|-------------|
| `except` | Session ID to keep active (optional) |

### Revoke Other Sessions

```
POST /api/v1/iam/sessions/revoke-others
Authorization: Bearer <access_token>
Content-Type: application/json

{
  "keep_session_id": "sess_current123"
}
```

---

## Audit Logging API

### Query Audit Events

```
GET /api/v1/iam/audit
Authorization: Bearer <admin_token>
```

**Required Permission**: `audit:read`

Query Parameters:
| Parameter | Description |
|-----------|-------------|
| `tenant_id` | Tenant ID (required) |
| `event_type` | Filter by event type (e.g., "user.login", "policy.created") |
| `category` | Filter by category ("authentication", "authorization", "management") |
| `outcome` | Filter by outcome ("success", "failure") |
| `actor_id` | Filter by actor ID |
| `resource_id` | Filter by resource ID |
| `start_time` | Events after this time (RFC3339) |
| `end_time` | Events before this time (RFC3339) |
| `limit` | Max results (default 100, max 1000) |
| `offset` | Pagination offset |

Response:
```json
{
  "events": [
    {
      "id": "evt_abc123",
      "tenant_id": "acme-corp",
      "event_type": "user.login",
      "category": "authentication",
      "outcome": "success",
      "actor_id": "user_xyz",
      "actor_type": "user",
      "ip_address": "192.168.1.100",
      "user_agent": "Mozilla/5.0...",
      "timestamp": "2025-01-15T10:30:00Z",
      "details": {
        "method": "webauthn",
        "mfa_used": true
      }
    }
  ],
  "total": 150,
  "limit": 100,
  "offset": 0
}
```

### Get Audit Event

```
GET /api/v1/iam/audit/{event_id}?tenant_id={tenant_id}
Authorization: Bearer <admin_token>
```

**Required Permission**: `audit:read`

### Delete Old Audit Events

```
DELETE /api/v1/iam/audit?tenant_id={tenant_id}&before={timestamp}
Authorization: Bearer <admin_token>
```

**Required Permission**: `audit:delete`

**Access Control**: This endpoint is restricted to administrators with the `audit:delete` permission. Only users with the `super_admin` role have this permission by default. Tenant admins and audit admins do NOT have delete permissions to preserve audit trail integrity.

| Admin Role | Has audit:delete |
|------------|------------------|
| `super_admin` | Yes |
| `tenant_admin` | No |
| `audit_admin` | No |
| `user_admin` | No |

Response:
```json
{
  "deleted_count": 1234,
  "before": "2024-01-01T00:00:00Z"
}
```

Error Response (403 Forbidden):
```json
{
  "error": "forbidden",
  "error_description": "The audit:delete permission is required to delete audit events"
}
```

---

## MFA API

### Get MFA Status

```
GET /api/v1/iam/mfa/status
Authorization: Bearer <access_token>
```

Response:
```json
{
  "enabled": true,
  "method": "totp",
  "enrolled_at": "2025-01-10T08:00:00Z",
  "last_verified": "2025-01-15T10:30:00Z",
  "backup_codes_remaining": 8
}
```

### Begin MFA Enrollment

```
POST /api/v1/iam/mfa/enroll
Authorization: Bearer <access_token>
Content-Type: application/json

{
  "method": "totp"
}
```

Response:
```json
{
  "enrollment_id": "enroll_abc123",
  "secret": "JBSWY3DPEHPK3PXP",
  "provisioning_uri": "otpauth://totp/QuicBaaS:user@example.com?secret=JBSWY3DPEHPK3PXP&issuer=QuicBaaS",
  "expires_at": "2025-01-15T10:40:00Z"
}
```

### Complete MFA Enrollment

```
POST /api/v1/iam/mfa/enroll/verify
Authorization: Bearer <access_token>
Content-Type: application/json

{
  "enrollment_id": "enroll_abc123",
  "code": "123456"
}
```

### Disable MFA

```
DELETE /api/v1/iam/mfa
Authorization: Bearer <access_token>
```

### Verify MFA Code

```
POST /api/v1/iam/mfa/verify
Authorization: Bearer <access_token>
Content-Type: application/json

{
  "code": "123456"
}
```

### Generate Backup Codes

```
POST /api/v1/iam/mfa/backup-codes
Authorization: Bearer <access_token>
```

Response:
```json
{
  "codes": [
    "AAAA-BBBB-CCCC",
    "DDDD-EEEE-FFFF",
    "GGGG-HHHH-IIII",
    "..."
  ],
  "warning": "Store these codes securely. They will not be shown again."
}
```

### Get Backup Code Status

```
GET /api/v1/iam/mfa/backup-codes/status
Authorization: Bearer <access_token>
```

Response:
```json
{
  "total_codes": 10,
  "remaining_codes": 8,
  "generated_at": "2025-01-10T08:00:00Z"
}
```

---

## PEP gRPC API

### Service Definition

```protobuf
service PolicyEnforcementPoint {
  // Single authorization check
  rpc Check(CheckRequest) returns (CheckResponse);

  // Batch authorization check
  rpc CheckBatch(CheckBatchRequest) returns (CheckBatchResponse);

  // Subscribe to policy updates
  rpc Subscribe(SubscribeRequest) returns (stream PolicyUpdate);
}

message CheckRequest {
  Subject subject = 1;
  string action = 2;
  string resource = 3;
  map<string, string> context = 4;
}

message CheckResponse {
  bool allowed = 1;
  string matched_policy = 2;
  string matched_rule = 3;
  int64 evaluation_time_ns = 4;
}

message SubscribeRequest {
  string service_id = 1;
  repeated string resource_patterns = 2;
}

message PolicyUpdate {
  enum Type {
    SNAPSHOT = 0;
    CREATED = 1;
    UPDATED = 2;
    DELETED = 3;
  }
  Type type = 1;
  bytes policy_data = 2;
  string policy_id = 3;
  int64 version = 4;
}
```

### PEP REST API

**Single Authorization Check**
```
POST /api/v1/iam/pep/check
Authorization: Bearer <access_token>
Content-Type: application/json

{
  "subject": {
    "id": "user_abc123",
    "tenant_id": "acme-corp",
    "roles": ["developer"],
    "groups": ["engineering"],
    "attributes": {
      "department": "engineering"
    }
  },
  "action": "read",
  "resource": "orders://orders/12345",
  "context": {
    "ip_address": "192.168.1.100",
    "time": "2025-01-15T10:30:00Z"
  }
}
```

Response:
```json
{
  "allowed": true,
  "matched_policy": "order-access",
  "matched_rule": "read-own",
  "evaluation_time_ns": 2500
}
```

**Batch Authorization Check**
```
POST /api/v1/iam/pep/check-batch
Authorization: Bearer <access_token>
Content-Type: application/json

{
  "subject": {
    "id": "user_abc123",
    "tenant_id": "acme-corp",
    "roles": ["developer"]
  },
  "checks": [
    {"action": "read", "resource": "orders://orders/123"},
    {"action": "write", "resource": "orders://orders/123"},
    {"action": "delete", "resource": "orders://orders/123"}
  ]
}
```

Response:
```json
{
  "results": [
    {"allowed": true, "matched_policy": "order-access", "matched_rule": "read-own"},
    {"allowed": true, "matched_policy": "order-access", "matched_rule": "write-own"},
    {"allowed": false, "matched_policy": "", "matched_rule": ""}
  ],
  "total_evaluation_time_ns": 7500
}
```

---

## Error Responses

All errors follow this format:

```json
{
  "error": "invalid_request",
  "error_description": "The redirect_uri is not registered",
  "error_uri": "https://docs.quicbaas.io/iam/errors#invalid_request"
}
```

### OAuth2 Errors

| Error | Description |
|-------|-------------|
| `invalid_request` | Missing or invalid parameter |
| `invalid_client` | Client authentication failed |
| `invalid_grant` | Invalid authorization code or refresh token |
| `unauthorized_client` | Client not authorized for this grant type |
| `unsupported_grant_type` | Grant type not supported |
| `invalid_scope` | Requested scope is invalid |

### API Errors

| Status | Error | Description |
|--------|-------|-------------|
| 400 | `bad_request` | Invalid request body |
| 401 | `unauthorized` | Missing or invalid token |
| 403 | `forbidden` | Insufficient permissions |
| 404 | `not_found` | Resource not found |
| 409 | `conflict` | Resource already exists |
| 429 | `rate_limited` | Too many requests |
| 500 | `internal_error` | Server error |

### WebAuthn Errors

| Error | HTTP Status | Description |
|-------|-------------|-------------|
| `challenge_expired` | 400 | Challenge TTL exceeded |
| `challenge_not_found` | 400 | Challenge not found or already used |
| `credential_not_found` | 404 | Credential does not exist |
| `credential_invalid` | 400 | Credential validation failed |
| `registration_failed` | 400 | Registration verification failed |
| `authentication_failed` | 401 | Authentication verification failed |

---

## See Also

- [WebAuthn Guide](webauthn.md) - Detailed WebAuthn integration guide
- [Audit Logging](audit-logging.md) - Audit logging and permissions
- [ABAC Policy Syntax](abac-policy-syntax.md) - Policy language reference
- [Developer Guide](developer-guide.md) - Integration overview
