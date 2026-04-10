# QuicBaaS IAM Architecture

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
