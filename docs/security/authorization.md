# Authorization in QuicBaaS

> **Overview**: QuicBaaS provides a unified authorization system that protects all API protocols (REST, gRPC, QUIC, MCP) with configurable access control.

## Authorization Providers

QuicBaaS supports three authorization providers:

| Provider | Description | Use Case |
|----------|-------------|----------|
| **RBAC** | Role-Based Access Control | Simple deployments with predefined roles |
| **ABAC** | Attribute-Based Access Control | Complex policies based on user/resource attributes |
| **Hybrid** | ABAC-first with RBAC fallback | Migration from RBAC to ABAC |

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      Client Request                             │
│              (REST / gRPC / QUIC / MCP)                         │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Protocol Handler                             │
│  ┌─────────────┐  ┌─────────────┐  ┌──────┐  ┌──────┐           │
│  │ REST        │  │ gRPC        │  │ QUIC │  │ MCP  │           │
│  │ Middleware  │  │ Interceptor │  │ Auth │  │ Auth │           │
│  └─────────────┘  └─────────────┘  └──────┘  └──────┘           │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ Extract: Subject, Resource, Action
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                  AuthorizationService                           │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  • Audit logging (all decisions)                           │ │
│  │  • Prometheus metrics collection                           │ │
│  │  • Decision ID generation                                  │ │
│  │  • Evaluation timing                                       │ │
│  └────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
                              │
            ┌─────────────────┼─────────────────┐
            ▼                 ▼                 ▼
     ┌───────────┐     ┌─────────────┐     ┌───────────┐
     │   RBAC    │     │     ABAC    │     │  Hybrid   │
     │ Provider  │     │   Provider  │     │ Provider  │
     │ (default) │     │(iam)│     │  (both)   │
     └───────────┘     └─────────────┘     └───────────┘
            │                 │                 │
            ▼                 ▼                 ▼
      RoleManager       PolicyEngine      ABAC → RBAC
                                          fallback
```

## Quick Start

### Basic RBAC Configuration (Default)

```yaml
# .quicbaas.yaml
security:
  authorization:
    provider: "rbac"
    rbac:
      enabled: true
      default_role: ""  # Deny unauthenticated requests
      allow_anonymous: true  # Allow health/metrics without auth
    audit:
      enabled: true
      log_denied: true
```

### Enabling ABAC

```yaml
security:
  authorization:
    provider: "abac"
    abac:
      enabled: true
      cache_ttl: "5m"
      policy_dir: "/etc/quicbaas/policies"
```

> **Note**: ABAC requires building with the `iam` tag:
> ```bash
> go build -tags iam ./cmd/quicbaasd
> ```

### Hybrid Mode (Migration)

```yaml
security:
  authorization:
    provider: "hybrid"
    rbac:
      enabled: true
    abac:
      enabled: true
```

## Core Concepts

### Subject

The entity making the request:

```go
type Subject struct {
    ID            string                 // Unique identifier
    Username      string                 // Human-readable name
    TenantID      string                 // Multi-tenant context
    Roles         []string               // RBAC roles
    Groups        []string               // Group memberships
    Attributes    map[string]interface{} // ABAC attributes
    Authenticated bool                   // Auth status
}
```

### Resources

Protected resources in QuicBaaS:

| Resource | Description |
|----------|-------------|
| `keys` | Key-value data operations |
| `transactions` | Transaction operations |
| `secrets` | Secrets management |
| `kms` | Key Management Service |
| `crypto` | Encryption/decryption |
| `seal` | Seal/unseal operations |
| `cluster` | Cluster management |
| `users` | User management |
| `roles` | Role management |
| `timeseries` | Time-series data |
| `vector` | Vector database |
| `watch` | Watch/subscription |
| `backups` | Backup operations |

### Actions

| Action | HTTP Methods | Description |
|--------|--------------|-------------|
| `read` | GET, HEAD, OPTIONS | Read operations |
| `write` | POST, PUT, PATCH | Write operations |
| `delete` | DELETE | Delete operations |
| `admin` | - | Administrative operations |

### Decision

Authorization result:

```go
type AuthorizationDecision struct {
    Allowed          bool   // Allow or deny
    Reason           string // Explanation
    Provider         string // rbac, abac, or hybrid
    MatchedPolicy    string // Which policy matched
    DecisionID       string // Unique ID for audit
    EvaluationTimeNs int64  // Performance metric
}
```

## Public Endpoints

These endpoints bypass authorization:

- `/api/v1/auth/login` - Authentication
- `/api/v1/health` - Health checks
- `/api/v1/health/liveness` - Kubernetes liveness probe
- `/api/v1/health/readiness` - Kubernetes readiness probe
- `/metrics` - Prometheus metrics

## Detailed Documentation

For in-depth configuration and usage:

- **[RBAC Documentation](./rbac.md)** - Role-Based Access Control
  - Built-in roles and permissions
  - Custom role creation
  - Permission matrix
  - Performance tuning

- **[ABAC Documentation](../iam/abac.md)** - Attribute-Based Access Control
  - Policy syntax and structure
  - Attribute evaluation
  - Context-based access
  - Multi-tenant scenarios

## Protocol-Specific Integration

### REST API

Authorization is enforced via `AuthzMiddleware`:

```go
// Configure server with authorization
server.SetAuthorizationService(authzService, tokenValidator)
```

### gRPC

Uses interceptors:

```go
// Unary RPC
grpc.UnaryInterceptor(auth.UnaryAuthInterceptorWithService(am, authzService, log))

// Streaming RPC
grpc.StreamInterceptor(auth.StreamAuthInterceptorWithService(am, authzService, log))
```

### QUIC

Server option:

```go
server := quic.NewServer(
    quic.WithAuthorizationService(authzService),
)
```

### MCP

MCP authorization includes OAuth 2.1 token validation and audience binding:

```go
server.SetAuthorizationService(authzService, authManager)
```

**MCP-Specific Requirements:**

1. **Bearer Token**: All requests must include `Authorization: Bearer <token>` header
2. **Audience Validation**: Token `aud` claim must include MCP server's resource URI
3. **Scopes**: Token must include required scopes (`mcp:tools`, `mcp:resources`)

**HTTP Error Responses:**

| Code | Error | WWW-Authenticate |
|------|-------|------------------|
| 401 | Invalid/missing token | `Bearer realm="quicbaas-mcp", error="invalid_token"` |
| 403 | Insufficient scope | `Bearer realm="quicbaas-mcp", error="insufficient_scope", scope="mcp:admin"` |

**MCP Tool Authorization:**

| Tool Pattern | Resource | Action |
|-------------|----------|--------|
| `quicbaas_get`, `quicbaas_list` | keys | read |
| `quicbaas_put`, `quicbaas_delete` | keys | write/delete |
| `quicbaas_kms_*` | kms | varies |
| `quicbaas_secrets_*` | secrets | varies |
| `quicbaas_cluster_*` | cluster | read/admin |

**Public Tools (no auth):**
- `quicbaas_health`
- `quicbaas_login`
- `quicbaas_version`
- `quicbaas_liveness`

See [MCP Authorization](../api/mcp-authorization.md) for complete OAuth 2.1 flow.

## Metrics

Authorization metrics are exposed via Prometheus:

| Metric | Type | Description |
|--------|------|-------------|
| `quicbaas_authz_requests_total` | Counter | Total authorization requests |
| `quicbaas_authz_latency_seconds` | Histogram | Request latency |

Labels: `provider`, `resource`, `action`, `allowed`

## Audit Logging

All authorization decisions are logged:

```json
{
  "level": "info",
  "msg": "Authorization denied",
  "decision_id": "dec-abc123",
  "subject_id": "user-456",
  "resource": "secrets",
  "action": "write",
  "allowed": false,
  "reason": "roles [readonly] do not have write permission on secrets",
  "provider": "rbac"
}
```

## Troubleshooting

### Common Issues

| Issue | Cause | Solution |
|-------|-------|----------|
| 401 Unauthorized | Missing/invalid token | Check JWT token validity |
| 403 Forbidden | Insufficient permissions | Verify role has required permission |
| ABAC not available | Missing build tag | Build with `-tags iam` |

### Debug Mode

Enable verbose logging:

```yaml
security:
  authorization:
    audit:
      log_allowed: true  # Log successful authorizations
```

## See Also

- [MCP Authorization](../api/mcp-authorization.md) - OAuth 2.1 for MCP
- [Authentication](./authentication.md) - JWT token management
- [IAM Overview](../iam/README.md) - Identity and Access Management
- [IAM Configuration](../iam/configuration.md) - IAM settings
- [Security Overview](./README.md) - Security features
