# IAM — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design, component boundaries and data flow. Feature folder: `docs/iam/`.

## Delegation to go-qrdb (locked)

QuicBaaS **composes** go-qrdb and go-xkms through the qrdb SDK (`go-qrdb/sdk/go`) and adds multi-protocol transports plus OIDC / OAuth2 on top. It does **not** own a user model, mTLS cert-binding scheme, barrier, or bootstrap — those are qrdb's, consumed via the SDK:

| Capability | Owner | quicbaas consumes via |
|------------|-------|------------------------|
| User/identity model (`User`, RoleBindings, ABAC, factors) | go-qrdb `pkg/user` | `sdk.User` / `sdk/go/types_user.go` |
| mTLS cert-binding identifier (SPKI SHA-256 thumbprint) | go-qrdb | `sdk.ComputeSPKIThumbprint` |
| First-boot + admin-mTLS enrollment | go-qrdb `pkg/firstboot` | `sdk.FirstBootInitialize` |
| Barrier (none/passphrase/shamir; system + per-tenant) | go-qrdb `pkg/seal` | `Security().Barrier()` |
| Ceremony (Shamir M-of-N) | go-qrdb `pkg/ceremony` | `Security().Ceremony()` |
| Hardware factors + CA | go-xkms | registered into qrdb via composer; `Security().Auth()` / `.Certificate()` |

What QuicBaaS adds on top of the qrdb user:

- **OIDC / OAuth2** — token issuance/consumer flows whose subjects are qrdb users; OAuth2 clients are a principal kind on the qrdb tenant model.
- **Multi-protocol transports** — REST / gRPC / QUIC / MCP / Unix surfacing the qrdb barrier/ceremony/credentials and the auth model.
- **ABAC policy engine + PEP** — evaluated over the qrdb user's `Attributes`.

QuicBaaS's identity store implements qrdb's `CertBinder` and user-store seams; admin/tenant-admin certs are bound by the qrdb SPKI thumbprint (the old `SOCertSPKIThumbprint` fork was deleted). The system admin provisions a tenant minimally (control plane); the tenant admin self-bootstraps their own barrier and data plane.

**Enrollment mechanisms, by scope.** System-level enrollment — the system admin
at first boot, and every Security Officer or custodian in a Shamir ceremony —
is **certificate-based, always, with no invite tokens**: those identities guard
barrier material. Three cert paths exist, selected by which inputs first-boot
receives: CSR-sign (the admin keeps the private key), generate-and-distribute
(the CA emits the bundle), and **adopt-presented** (the admin presents a
certificate they already hold, typically on a PIV card, validated against a
configured pool — nothing is issued).

The **initial tenant admin** may enrol by either the certificate paths above or
a single-use invitation whose redemption binds their first auth factor. Both
end in a tenant-scoped admin identity the system admin cannot use. Ordinary
users always enrol by invitation, because a brand-new user holds no credential
to authenticate with while enrolling their first one.


## System Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              QuicBaaS                                        │
│                    (Single Binary, Modular Features)                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │                         IAM Module (pkg/iam/)                          │ │
│  │                        [Enabled via config]                            │ │
│  │                                                                         │ │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐   │ │
│  │  │   Tenant    │  │  Identity   │  │  WebAuthn   │  │   OAuth2/   │   │ │
│  │  │  Manager    │  │   Store     │  │   FIDO2     │  │    OIDC     │   │ │
│  │  │             │  │             │  │             │  │             │   │ │
│  │  │ - Isolation │  │ - Users     │  │ - Register  │  │ - Provider  │   │ │
│  │  │ - Quotas    │  │ - Groups    │  │ - Authn     │  │ - Consumer  │   │ │
│  │  │ - Settings  │  │ - Roles     │  │ - Creds     │  │ - Tokens    │   │ │
│  │  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘   │ │
│  │                                                                         │ │
│  │  ┌───────────────────────────────────────────────────────────────────┐ │ │
│  │  │                    ABAC Policy Engine                              │ │ │
│  │  │                                                                    │ │ │
│  │  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────────┐  │ │ │
│  │  │  │  Parser  │─▶│ Compiler │─▶│  Cache   │─▶│    Evaluator     │  │ │ │
│  │  │  │  (YAML)  │  │  (AST)   │  │ (LRU)    │  │ (zero-alloc)     │  │ │ │
│  │  │  └──────────┘  └──────────┘  └──────────┘  └──────────────────┘  │ │ │
│  │  └───────────────────────────────────────────────────────────────────┘ │ │
│  └────────────────────────────────────────────────────────────────────────┘ │
│                                         │                                    │
│                                         │ uses                               │
│                                         ▼                                    │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │                     PEP (Policy Enforcement Point)                      │ │
│  │                          (pkg/abac/)                                    │ │
│  │                                                                         │ │
│  │  - Embedded in QuicBaaS for self-enforcement                           │ │
│  │  - Exported as library for other services                              │ │
│  │  - Policy sync via gRPC streams                                        │ │
│  │  - Local evaluation (zero network latency)                             │ │
│  └────────────────────────────────────────────────────────────────────────┘ │
│                                         │                                    │
│                                         │ enforces                           │
│                                         ▼                                    │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │                        Storage Module (existing)                        │ │
│  │                                                                         │ │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐   │ │
│  │  │  PebbleDB   │  │    Raft     │  │    gRPC     │  │    REST     │   │ │
│  │  │  (storage)  │  │ (consensus) │  │    API      │  │    API      │   │ │
│  │  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘   │ │
│  └────────────────────────────────────────────────────────────────────────┘ │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Components

### Tenant Manager

Manages multi-tenant isolation:
- Tenant CRUD operations
- Tenant settings and quotas
- Key prefix isolation (`__iam:tenant:{tenant_id}:...`)

### Identity Store

User, group, and role management:
- User registration and profile management
- Group membership
- Role assignments
- Custom attributes

### WebAuthn/FIDO2

Passwordless authentication:
- Passkey registration and authentication
- Hardware security key support
- Credential management

### OAuth2/OIDC

Standards-compliant identity protocols:
- **Provider**: Issue tokens to registered applications
- **Consumer**: Federate identity from external providers (Google, GitHub, Azure)
- PKCE, Device Authorization, Client Credentials flows

### ABAC Policy Engine

Attribute-Based Access Control:
- YAML policy parsing via go-codec
- Condition compilation to bytecode
- Zero-allocation evaluation (~2-5μs)
- LRU caching with bloom filters

### PEP (Policy Enforcement Point)

Distributed authorization:
- Embedded in QuicBaaS for self-enforcement
- Exported as library for external services
- Policy sync via gRPC streams
- Local evaluation with no network latency

## Data Model

All IAM data uses the reserved prefix `__iam:`:

```
Tenants:
__iam:tenant:{tenant_id}                     → Tenant config
__iam:tenant:{tenant_id}:settings            → Tenant settings

Identity:
__iam:tenant:{tenant_id}:user:{user_id}      → User record
__iam:tenant:{tenant_id}:group:{group_id}    → Group record
__iam:tenant:{tenant_id}:role:{role_id}      → Role record

Credentials:
__iam:tenant:{tenant_id}:cred:{cred_id}      → WebAuthn credential
__iam:tenant:{tenant_id}:challenge:{id}      → WebAuthn challenge (TTL)

OAuth2/OIDC:
__iam:tenant:{tenant_id}:client:{client_id}  → OAuth client
__iam:tenant:{tenant_id}:token:{jti}         → Token metadata
__iam:tenant:{tenant_id}:code:{code}         → Auth code (TTL)
__iam:tenant:{tenant_id}:idp:{provider_id}   → External IdP config

Policies:
__iam:tenant:{tenant_id}:policy:{policy_id}  → Policy document
__iam:tenant:{tenant_id}:policy:compiled:{id}→ Compiled policy (binary)

Indexes:
__iam:idx:email:{email}                      → tenant_id:user_id
__iam:idx:username:{tenant}:{name}           → user_id
```

## Policy Processing Pipeline

### Phase 1: Parsing

```
YAML Policy → go-codec Unmarshal → Policy AST (structs)
```

### Phase 2: Compilation

For each condition string like `"subject.role == admin"`:

1. **Lexer/Tokenizer**: `[subject][.][role][==][admin]`
2. **AST Builder**: `BinaryExpr{Left: Path, Op: Eq, Right: Str}`
3. **Bytecode Gen**:
   ```
   LOAD_PATH 0     ; subject.role
   LOAD_CONST 1    ; "admin"
   CMP_EQ          ; compare
   ```

### Phase 3: Optimization

1. **Resource Pattern Trie**: O(k) lookup for resource matching
2. **Deny Rule Bloom Filter**: Quick check for potential denies
3. **Subject Selector Index**: Fast policy lookup by role/group/user

### Phase 4: Caching

- Storage: `__iam:tenant:acme:policy:compiled:p123 → MessagePack`
- In-Memory: LRU cache with TTL (5 minutes default)

## Runtime Evaluation

Per-request authorization (~2-5μs typical):

1. **Quick Deny Check** (Bloom Filter) - ~10ns
2. **Get Applicable Policies** (Cache) - ~100ns
3. **Resource Pattern Match** (Trie) - ~50ns
4. **Evaluate Deny Rules First** - ~1μs
5. **Evaluate Allow Rules** - ~1μs
6. **Default Deny** if no match

## Policy Distribution

For external services using the PEP library:

```
┌───────────────────────┐
│   QuicBaaS (IAM)      │
│   gRPC Stream Server  │
└───────────┬───────────┘
            │ Policy sync
            │
┌───────────┼───────────┐
│           │           │
▼           ▼           ▼
Go Service  Python SDK  Sidecar
(pkg/abac)  (SDK)       (proxy)
```

Sync Protocol:
1. PEP connects to QuicBaaS IAM
2. Sends: `SubscribeRequest{service_id, resource_patterns}`
3. Receives: Full policy snapshot (initial sync)
4. Receives: Incremental updates (stream)
5. PEP applies updates to local cache
6. Version tracking prevents stale data


---

## Delegated Administration & Impersonation

QuicBaaS IAM provides comprehensive delegated administration capabilities, allowing organizations to assign granular administrative privileges and enable secure user impersonation for support scenarios.

### Overview

The Admin module (`pkg/iam/admin.go`) implements:

- **Admin Assignments**: Grant administrative privileges to users with fine-grained permissions
- **Admin Roles**: Predefined roles with appropriate permission sets
- **Resource Scoping**: Limit admin access to specific tenants or resources
- **Impersonation**: Allow admins to act as users for support purposes
- **Audit Trail**: Full audit logging of all admin actions

### Admin Roles

QuicBaaS provides the following predefined admin roles:

| Role | Description |
|------|-------------|
| `super_admin` | Full administrative permission set. Tenant reach is decided by the assignment's `tenant_ids`, not the role: the assignment acts in the tenants it names, or system-tenant scope alone when `tenant_ids` is empty (go-quicbaas#357) |
| `tenant_admin` | Full access within assigned tenant(s) |
| `user_admin` | Can manage users within assigned tenant(s) |
| `group_admin` | Can manage groups within assigned tenant(s) |
| `policy_admin` | Can manage policies within assigned tenant(s) |
| `audit_admin` | Can view audit logs within assigned tenant(s) |
| `read_only` | Read-only access to assigned resources |

### Admin Permissions

#### Permission Categories

##### Tenant Permissions
| Permission | Description |
|------------|-------------|
| `tenant:create` | Create new tenants |
| `tenant:read` | View tenant details |
| `tenant:update` | Modify tenant settings |
| `tenant:delete` | Delete tenants |

##### User Permissions
| Permission | Description |
|------------|-------------|
| `user:create` | Create new users |
| `user:read` | View user details |
| `user:update` | Modify user profiles |
| `user:delete` | Delete users |
| `user:impersonate` | Impersonate users |
| `user:reset_mfa` | Reset user MFA |
| `user:suspend` | Suspend user accounts |
| `user:unlock` | Unlock user accounts |

##### Group Permissions
| Permission | Description |
|------------|-------------|
| `group:create` | Create groups |
| `group:read` | View groups |
| `group:update` | Modify groups |
| `group:delete` | Delete groups |

##### Role Permissions
| Permission | Description |
|------------|-------------|
| `role:create` | Create roles |
| `role:read` | View roles |
| `role:update` | Modify roles |
| `role:delete` | Delete roles |
| `role:assign` | Assign roles to users |

##### Policy Permissions
| Permission | Description |
|------------|-------------|
| `policy:create` | Create policies |
| `policy:read` | View policies |
| `policy:update` | Modify policies |
| `policy:delete` | Delete policies |
| `policy:simulate` | Run policy simulations |

##### Audit & Session Permissions
| Permission | Description |
|------------|-------------|
| `audit:read` | View audit logs |
| `audit:export` | Export audit logs |
| `session:read` | View sessions |
| `session:revoke` | Revoke sessions |

##### GDPR Permissions
| Permission | Description |
|------------|-------------|
| `gdpr:export` | Process data export requests |
| `gdpr:delete` | Process data deletion requests |
| `consent:manage` | Manage user consents |

### Admin Assignments

#### Creating an Assignment

```bash
# REST API
curl -X POST https://quicbaas:8080/api/v1/admin/assignments \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "user_id": "admin_user_123",
    "admin_role": "tenant_admin",
    "tenant_ids": ["acme-corp", "widgets-inc"],
    "granted_by": "super_admin_001",
    "expires_at": "2026-01-01T00:00:00Z"
  }'

# CLI
quicbaas iam admin assignment create \
  --user-id admin_user_123 \
  --role tenant_admin \
  --tenants acme-corp,widgets-inc \
  --expires 2026-01-01
```

#### Assignment Options

| Field | Description |
|-------|-------------|
| `user_id` | User receiving admin privileges |
| `admin_role` | Predefined admin role |
| `tenant_ids` | Tenants this assignment applies to, named explicitly. Empty scopes the assignment to the SYSTEM tenant alone (unscoped and system-tenant checks only) — it does NOT mean every tenant, and there is no all-tenants sentinel (go-quicbaas#357) |
| `permissions` | Additional permissions beyond role defaults |
| `denied_permissions` | Explicitly denied permissions |
| `resource_scopes` | Limit access to specific resources |
| `expires_at` | When the assignment expires |
| `granted_by` | Admin who created the assignment |

#### Resource Scoping

Limit admin access to specific resources within tenants:

```json
{
  "resource_scopes": [
    {
      "resource_type": "user",
      "resource_pattern": "user:engineering-*"
    },
    {
      "resource_type": "group",
      "resource_pattern": "group:team-*"
    }
  ]
}
```

Patterns support:
- Exact match: `user:john.doe`
- Prefix wildcard: `user:engineering-*`
- Suffix wildcard: `*-admin`

#### Managing Assignments

```bash
# List assignments
quicbaas iam admin assignment list --user-id admin_user_123

# Get assignment details
quicbaas iam admin assignment get --assignment-id adm_abc123

# Update assignment
quicbaas iam admin assignment update --assignment-id adm_abc123 \
  --role user_admin --active false

# Delete assignment
quicbaas iam admin assignment delete --assignment-id adm_abc123
```

#### Check Permission

```bash
# REST API
curl -X POST https://quicbaas:8080/api/v1/admin/check \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "user_id": "admin_user_123",
    "permission": "user:create",
    "tenant_id": "acme-corp"
  }'

# CLI
quicbaas iam admin check \
  --user-id admin_user_123 \
  --permission user:create \
  --tenant acme-corp
```

Response:

```json
{
  "allowed": true,
  "assignment_id": "adm_abc123",
  "admin_role": "tenant_admin",
  "reason": "granted by assignment adm_abc123"
}
```

### Impersonation

Impersonation allows administrators to act as another user for support purposes. All actions during impersonation are fully audited.

#### Starting Impersonation

```bash
# REST API
curl -X POST https://quicbaas:8080/api/v1/admin/impersonation/start \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "admin_user_id": "admin_123",
    "target_user_id": "user_456",
    "tenant_id": "acme-corp",
    "reason": "Investigating support ticket #12345",
    "duration": "1h"
  }'

# CLI
quicbaas iam admin impersonate start \
  --target-user user_456 \
  --tenant acme-corp \
  --reason "Support ticket #12345" \
  --duration 1h
```

#### Requirements

- Admin must have `user:impersonate` permission
- Admin's assignment must name the target user's tenant explicitly in
  `tenant_ids` (an empty-`tenant_ids` assignment — the firstboot
  super_admin's shape — reaches only system-tenant users; a system-tenant
  principal is therefore denied impersonation of tenant users by default,
  and the one break-glass path is an assignment explicitly scoped to that
  tenant — go-quicbaas#357)
- The target must resolve as a real user of the target tenant through the
  identity store before any session is minted; a deployment without a
  target resolver wired cannot impersonate at all (fails closed)
- A reason is required for audit purposes
- Duration defaults to 1 hour if not specified

#### Ending Impersonation

```bash
# REST API
curl -X POST https://quicbaas:8080/api/v1/admin/impersonation/{session_id}/end \
  -H "Authorization: Bearer $TOKEN"

# CLI
quicbaas iam admin impersonate end --session-id imp_abc123
```

#### Active Impersonations

```bash
# List active impersonation sessions
quicbaas iam admin impersonate list --admin-user admin_123
```

#### Impersonation Session

```json
{
  "id": "imp_abc123",
  "admin_user_id": "admin_123",
  "target_user_id": "user_456",
  "tenant_id": "acme-corp",
  "reason": "Support ticket #12345",
  "started_at": "2025-01-15T10:00:00Z",
  "expires_at": "2025-01-15T11:00:00Z",
  "ended_at": null,
  "audit_log_id": "audit_xyz789"
}
```

### Audit Events

All admin operations generate audit events:

| Event | Description |
|-------|-------------|
| `admin.assignment.create` | Admin assignment created |
| `admin.assignment.update` | Admin assignment modified |
| `admin.assignment.delete` | Admin assignment removed |
| `admin.impersonation.start` | Impersonation session started |
| `admin.impersonation.end` | Impersonation session ended |

#### Audit Details

Impersonation events include:
- Admin user ID
- Target user ID
- Tenant context
- Reason for impersonation
- Session duration

### Role Permission Matrix

| Role | Tenant | User | Group | Role | Policy | Audit | Session | GDPR |
|------|--------|------|-------|------|--------|-------|---------|------|
| `super_admin` | CRUD | CRUD+I | CRUD | CRUD+A | CRUD+S | R+E | R+V | Full |
| `tenant_admin` | RU | CRUD | CRUD | CRUD+A | CRUD+S | R | R+V | Full |
| `user_admin` | - | CRUD | R | R+A | - | - | - | - |
| `group_admin` | - | R | CRUD | - | - | - | - | - |
| `policy_admin` | - | - | - | - | CRUD+S | - | - | - |
| `audit_admin` | - | - | - | - | - | R+E | - | - |
| `read_only` | R | R | R | R | R | R | R | - |

Legend: C=Create, R=Read, U=Update, D=Delete, I=Impersonate, A=Assign, S=Simulate, E=Export, V=Revoke

### Protocol Support

Admin operations are available via all protocols:

| Protocol | Implementation |
|----------|---------------|
| REST API | `pkg/api/rest/iam_admin.go` |
| gRPC | `pkg/api/grpc/server_iam.go` |
| QUIC | `pkg/api/quic/handler_iam_admin.go` |
| MCP | `pkg/api/mcp/tools_iam_admin.go` |
| CLI | `cmd/quicbaas/iam_admin.go` |

### Security Considerations

1. **Least Privilege**: Assign the minimum role required for the task
2. **Time-Limited**: Use expiration dates for temporary access
3. **Resource Scoping**: Limit access to specific resources when possible
4. **Audit Review**: Regularly review admin actions in audit logs
5. **Impersonation Justification**: Always require and review impersonation reasons
6. **Session Limits**: Keep impersonation sessions short (1 hour default)


---

## Passwordless Authentication

QuicBaaS uses hardware-backed, phishing-resistant authentication exclusively. No user passwords are supported. All identity verification relies on hardware security keys, trusted platform modules, or platform authenticators (biometric/PIN).

### Why Passwordless

- **Phishing Resistant**: Credentials are bound to origin; attackers cannot intercept or replay them.
- **Hardware Attestation**: FIDO2/WebAuthn and TPM enable cryptographic proof of authenticator identity.
- **Standards Aligned**: Meets NIST 800-63B AAL3 (Authenticator Assurance Level 3) and FedRAMP requirements.
- **Zero Shared Secrets**: No password databases to breach; each authenticator holds its private key.

### Identity Matrix

| Identity Type | Auth Method | Hardware Requirement | Protocol |
|---|---|---|---|
| Human user | WebAuthn (passkey or security key) | FIDO2 authenticator (platform or roaming) | CTAP2/WebAuthn |
| Service account (cloud-native) | TPM IDevID + mTLS | TPM 2.0 | TLS 1.3 + RFC 8705 |
| Service account (on-prem) | xkey-issued mTLS cert | Hardware token (Trusted Platform Module) | TLS 1.3 + RFC 8705 |
| Service account (admin-minted) | mTLS client certificate (manual enrollment) | External PKI or admin distribution | TLS 1.3 + RFC 8705 |
| OAuth2 machine client | client_secret (RFC 6749 §2.3.1) | Not applicable | Bearer token in Authorization header |

**Note:** OAuth2 `client_secret` is **not a user password**. It is a machine credential identifying a confidential client application, as defined in RFC 6749 Section 2.3.1. Each new client app receives its own, and it can be rotated independently. Prefer TLS client authentication (`tls_client_auth` or `self_signed_tls_client_auth` per RFC 8705) when the client can present a TLS client certificate.

### User Flow: Invite → Activate → Login

#### 1. Admin Creates Pending User

```bash
quicbaas iam users create alice@example.com \
  --email alice@example.com \
  --name "Alice Smith" \
  --tenant acme-corp
```

User is created in `pending` status. No authenticator is enrolled yet.

#### 2. Invite URL Sent to User

Admin or system sends user an invite URL:

```
https://quicbaas.example.com/auth/invite?token=<signed-jwt>&user_id=<uuid>
```

The token contains the user ID, tenant ID, and a short expiration (e.g., 24 hours). The user accesses the link in a modern browser.

#### 3. User Enrolls Authenticator

On the invite page, user is prompted to enroll their first passkey or security key:

- **Platform Authenticator** (recommended): Touch ID (macOS/iOS), Windows Hello, Android biometric
- **Roaming Authenticator**: YubiKey, Google Titan, Solokey (USB/NFC/BLE)

Browser's WebAuthn API guides the enrollment. The authenticator creates a public–private key pair; the public key is stored in QuicBaaS, and the private key never leaves the hardware.

#### 4. User Logs In

User visits login page and selects their identity provider:

- **Sign in with Passkey**: Prompts for platform or roaming authenticator
- **Sign in with Smartcard** (if PIV is enabled): Inserts PIV card and enters PIN

No username or password field exists. The server returns a list of allowed credentials for the user after they verify their identity.

#### 5. Headless Login (CLI)

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

### Service Account Flow

Service accounts (non-human identities) authenticate via **mTLS with a bound access token**.

#### Register a Service Account with Self-Signed TLS

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

#### Obtain Access Token

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

### Trusted Computing Layer

#### FIDO2 / WebAuthn Attestation

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

#### Administrator Certificate Bindings

Binding a client certificate to an administrator — the non-interactive
`auth.bootstrap_admin_piv_cert_path` path — enrolls an **mtls auth factor**
alongside the user-side thumbprint. (The SO identity is CSR-based; the server
never holds officer private keys, so there is no server-issued SO certificate
to bind.) Both writes are required and serve different lookups:

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

#### TPM Attestation

For service accounts on premises, the server validates TPM attestation:

1. Client sends TPM-signed quote of the EK (Endorsement Key) public key
2. Server verifies the quote against the TPM EK root pool
3. Server issues a token bound to the EK public key hash (`cnf.x5t#S256`)

#### xkey Root CA Pool

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

### id_token Claims

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

### OIDC Discovery

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

### What's Removed

- **User Passwords**: No `password` field in user creation or login. All user authentication is WebAuthn or smartcard.
- **OAuth2 Password Grant**: The `password` grant type (RFC 6749 §4.3) is removed. Use `authorization_code` with PKCE instead.
- **OAuth2 Implicit Grant**: Removed per OAuth 2.1. Use `authorization_code` with PKCE.
- **Password login**: There is no password verb. Login is the
  unified factor ceremony `POST /api/v1/auth/login/begin` +
  `POST /api/v1/auth/login/finish` (mtls / fido2 / piv proof), or the
  tenant WebAuthn mount `/api/v1/iam/{tenant_id}/webauthn/login/{begin,finish}`.

### Configuration

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

### Troubleshooting

| Issue | Cause | Solution |
|---|---|---|
| "Authenticator not allowed" | AAGUID is not in allowlist | Add the device AAGUID to `iam.auth.webauthn.aaguid_allowlist` or disable attestation |
| "Certificate verification failed" | mTLS client cert not enrolled | Re-register the service account with `quicbaas oauth2 client register` |
| "Token bound to wrong credential" | `cnf.x5t#S256` mismatch | Ensure client always presents the enrolled certificate; check cert renewal |
| "Challenge expired" | WebAuthn ceremony took >60s | Increase `iam.auth.webauthn.timeout` or prompt user to retry |
| `auth login` returns bare `UNAUTHORIZED` at begin | Login sent no tenant scope; the per-tenant WebAuthn relying party fails closed | Pass `--tenant <id>` (defaults to `system`) |
| `auth login` returns `UNAUTHORIZED` at finish | No fido2 factor is enrolled for the credential in the qrdb auth-factor store | Enroll with `quicbaas auth factor add --type fido2` |
| `auth login --method mtls` returns `UNAUTHORIZED` at finish | The certificate is not enrolled as an mtls factor — it was never bound, or the binding predates the write-through | Re-run the binding (`auth.bootstrap_admin_piv_cert_path` on first boot, or the SO-cert init ceremony), or enroll explicitly with `quicbaas auth factor add --type mtls --cert <path>` |

### References

- [FIDO2 / WebAuthn Spec](https://www.w3.org/TR/webauthn-2/)
- [NIST 800-63B](https://pages.nist.gov/800-63-3/sp800-63b.html) – Authenticator Assurance Level 3
- [RFC 6749](https://tools.ietf.org/html/rfc6749) – OAuth 2.0 Authorization Framework
- [RFC 8705](https://tools.ietf.org/html/rfc8705) – OAuth 2.0 Mutual-TLS Client Authentication
- [OIDC Core](https://openid.net/specs/openid-connect-core-1_0.html)
