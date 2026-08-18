# Authorization — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented usage, in the order tasks are done. Feature folder: `docs/authorization/`.

QuicBaaS implements Role-Based Access Control (RBAC) for fine-grained authorization across all API protocols. This document covers the RBAC architecture, configuration, built-in roles, and usage patterns.

> **Status: RBAC is delegated to the go-qrdb Policy Decision Point (PDP).**
> The former local RBAC fork (`rbac.go` / `persistent_rbac.go`,
> `interfaces.RoleManager`, `GetRoleManager`, `auth.NewRoleManager`, `auth.Role`
> / `auth.Permission`) was **deleted** in the authz-unification. Roles and
> permissions are now **owned by go-qrdb** and reached through
> `pkg/iam/qrdb_role_storage.go` (`QRDBRoleStorage` → qrdb `RoleClient`), sharing
> ONE role ID space with the qrdb PDP. Request-time authorization uses the colon
> `resource:action` vocabulary via
> `platformtoken.Authorize(claims).RequireScope("resource:action")`; token
> subjects are qrdb users. The `RoleManager` / `auth.Role` code shown below
> describes the **deleted fork API** and is retained only as conceptual
> background — see Authorization and
> Authentication.

## Overview

### What is RBAC?

Role-Based Access Control (RBAC) is an authorization model where access decisions are based on the roles assigned to users rather than individual permissions. Users are assigned one or more roles, and each role grants specific permissions to perform actions on resources.

Key concepts:
- **Subject**: The entity making a request (user, service account)
- **Role**: A named collection of permissions (e.g., `admin`, `readonly`)
- **Resource**: The target of the action (e.g., `keys`, `secrets`, `cluster`)
- **Action**: The operation being performed (e.g., `read`, `write`, `delete`)
- **Permission**: A combination of resource and allowed actions

### How QuicBaaS Implements RBAC

QuicBaaS's RBAC implementation provides:

- **Unified authorization** across all protocols (REST, gRPC, QUIC, MCP)
- **Built-in roles** for common access patterns
- **Custom roles** for organization-specific requirements
- **Anonymous access** support for public endpoints
- **Sub-microsecond authorization checks** via O(1) map lookups
- **Audit logging** of all authorization decisions

### RBAC vs ABAC

| Feature | RBAC | ABAC |
|---------|------|------|
| Complexity | Simple | Complex |
| Performance | < 1us | < 5us |
| Use case | Role-based access | Attribute-based policies |
| Policy language | Role assignments | CEL/Rego expressions |
| Multi-tenancy | Via tenant roles | Native support |
| Context-aware | Limited | Full support |

**When to use RBAC:**
- Standard role-based access patterns
- Performance-critical applications
- Simple permission hierarchies

**When to use ABAC:**
- Complex policies based on user/resource attributes
- Time-based or location-based access
- Dynamic policy evaluation

QuicBaaS supports both models and a hybrid mode for gradual migration.

## Architecture

```
+-------------------------------------------------------------+
|                    Protocol Handlers                         |
|  REST | gRPC | QUIC | MCP                                   |
+-------+------+------+---------------------------------------+
                              |
                              v
+-------------------------------------------------------------+
|                  AuthorizationService                        |
|     Authorize(Subject, Resource, Action) -> Decision        |
|                                                             |
|  Features:                                                  |
|  - Audit logging of all decisions                           |
|  - Prometheus metrics collection                            |
|  - Optional decision caching                                |
+-------------------------------------------------------------+
                              |
                              v
+-------------------------------------------------------------+
|                    RBAC Provider                             |
|        Projects the request onto the go-qrdb PDP            |
|                                                             |
|  Features:                                                  |
|  - Context cancellation support                             |
|  - Batch authorization                                      |
|  - Anonymous access handling                                |
+-------------------------------------------------------------+
                              |
                              v
+-------------------------------------------------------------+
|           go-qrdb PDP (single source of truth)              |
|     Authorize(subject, resource:action) -> allow/deny       |
|                                                             |
|  - Roles + permissions OWNED by qrdb (one role ID space)   |
|  - QRDBRoleStorage -> qrdb RoleClient (pkg/iam)            |
|  - Colon resource:action vocab; subjects = qrdb users      |
+-------------------------------------------------------------+
```

### Request Flow

1. **Protocol Handler** receives a request with JWT token
2. **Authentication** extracts user identity and roles from token
3. **Authorization Service** creates an `AuthorizationRequest`
4. **RBAC Provider** projects the request onto the **go-qrdb PDP**, which evaluates `resource:action` permissions against qrdb-owned roles
5. **Decision** returned with allow/deny, reason, and audit metadata

## Built-in Roles

QuicBaaS includes three built-in roles that cannot be modified or deleted:

### admin

Full administrative access to all resources and actions.

```go
Role{
    Name:        "admin",
    Description: "Full administrative access to all resources",
    Permissions: []*Permission{
        {Resource: "*", Actions: []string{"*"}},
    },
}
```

**Use case:** System administrators, operators with full control.

### readwrite

Read and write access to database operations, without administrative privileges.

```go
Role{
    Name:        "readwrite",
    Description: "Read and write access to database operations",
    Permissions: []*Permission{
        {Resource: "keys", Actions: []string{"read", "write", "delete"}},
        {Resource: "indexes", Actions: []string{"read", "write"}},
        {Resource: "transactions", Actions: []string{"read", "write"}},
        {Resource: "watch", Actions: []string{"read"}},
        {Resource: "metrics", Actions: []string{"read"}},
        {Resource: "health", Actions: []string{"read"}},
    },
}
```

**Use case:** Applications with read/write data access.

### readonly

Read-only access to database operations.

```go
Role{
    Name:        "readonly",
    Description: "Read-only access to database operations",
    Permissions: []*Permission{
        {Resource: "keys", Actions: []string{"read"}},
        {Resource: "indexes", Actions: []string{"read"}},
        {Resource: "metrics", Actions: []string{"read"}},
        {Resource: "health", Actions: []string{"read"}},
    },
}
```

**Use case:** Monitoring systems, read-only dashboards, analytics.

## Resources and Actions

### Resources

| Resource | Description |
|----------|-------------|
| `keys` | Key-value data operations (Get, Put, Delete, Range) |
| `transactions` | Transaction operations (Begin, Commit, Rollback) |
| `cluster` | Cluster management (node operations, rebalancing) |
| `backups` | Backup and restore operations |
| `users` | User management (create, delete, modify users) |
| `roles` | Role management (create, delete, modify roles) |
| `secrets` | Secrets management (store, retrieve, delete secrets) |
| `kms` | Key Management Service operations |
| `crypto` | Cryptographic operations (encrypt, decrypt, sign, verify) |
| `seal` | Seal/unseal operations |
| `health` | Health check endpoints |
| `metrics` | Metrics access |
| `watch` | Watch/subscription operations |
| `timeseries` | Timeseries data operations |
| `vector` | Vector database operations |
| `indexes` | Index management operations |
| `*` | Wildcard - matches all resources |

### Actions

| Action | HTTP Methods | gRPC Operations | Description |
|--------|--------------|-----------------|-------------|
| `read` | GET, HEAD | Get, List, Query | Read operations |
| `write` | POST, PUT, PATCH | Put, Create, Update | Write operations |
| `delete` | DELETE | Delete, Remove | Delete operations |
| `admin` | - | Admin RPCs | Administrative operations (includes all other actions) |
| `*` | - | - | Wildcard - matches all actions |

### Action Hierarchy

The `admin` action implicitly includes all other actions:
```
admin
  |-> read
  |-> write
  |-> delete
```

The `*` wildcard explicitly matches any action.

## Configuration

### Full YAML Configuration

```yaml
security:
  authorization:
    # Provider type: "rbac" (default), "abac", or "hybrid"
    provider: "rbac"

    rbac:
      # Enable RBAC authorization
      enabled: true

      # Default role for unauthenticated requests
      # Options: "readonly", "readwrite", "admin", or custom role name
      # Leave empty to deny unauthenticated requests
      default_role: ""

      # Allow anonymous access to public endpoints (health, metrics)
      allow_anonymous: true

    # Audit logging configuration
    audit:
      # Enable audit logging of authorization decisions
      enabled: true

      # Log denied authorization requests
      log_denied: true

      # Log allowed authorization requests (enable for compliance)
      log_allowed: false
```

### Environment Variables

```bash
# Provider configuration
QUICBAAS_SECURITY_AUTHORIZATION_PROVIDER=rbac

# RBAC settings
QUICBAAS_SECURITY_AUTHORIZATION_RBAC_ENABLED=true
QUICBAAS_SECURITY_AUTHORIZATION_RBAC_DEFAULT_ROLE=
QUICBAAS_SECURITY_AUTHORIZATION_RBAC_ALLOW_ANONYMOUS=true

# Audit settings
QUICBAAS_SECURITY_AUTHORIZATION_AUDIT_ENABLED=true
QUICBAAS_SECURITY_AUTHORIZATION_AUDIT_LOG_DENIED=true
QUICBAAS_SECURITY_AUTHORIZATION_AUDIT_LOG_ALLOWED=false
```

### CLI Flags

```bash
quicbaas server start \
  --authz-provider=rbac \
  --authz-rbac-enabled=true \
  --authz-rbac-default-role="" \
  --authz-rbac-allow-anonymous=true \
  --authz-audit-enabled=true \
  --authz-audit-log-denied=true
```

## Permission Matrix

Default permissions for built-in roles:

| Resource | admin | readwrite | readonly |
|----------|-------|-----------|----------|
| keys | RWD | RWD | R |
| transactions | RWD | RW | - |
| watch | RWD | R | - |
| indexes | RWD | RW | R |
| cluster | RWD | - | - |
| backups | RWD | - | - |
| users | RWD | - | - |
| roles | RWD | - | - |
| secrets | RWD | - | - |
| kms | RWD | - | - |
| crypto | RWD | - | - |
| seal | RWD | - | - |
| health | RWD | R | R |
| metrics | RWD | R | R |
| timeseries | RWD | - | - |
| vector | RWD | - | - |

Legend: R = read, W = write, D = delete, - = no access

## API Reference

### Subject

Represents the entity making an authorization request.

```go
type Subject struct {
    // ID is the unique identifier for the subject
    ID string

    // Username is the human-readable name
    Username string

    // TenantID for multi-tenant deployments
    TenantID string

    // Roles assigned to the subject
    Roles []string

    // Groups the subject belongs to
    Groups []string

    // Attributes for ABAC policy evaluation
    Attributes map[string]interface{}

    // Authenticated indicates if the subject is authenticated
    Authenticated bool
}
```

### AuthorizationRequest

Represents a request for authorization.

```go
type AuthorizationRequest struct {
    // Subject making the request
    Subject Subject

    // Resource being accessed
    Resource string

    // Action being performed
    Action string

    // Context for policy evaluation
    Context map[string]interface{}
}
```

### AuthorizationDecision

Represents the result of an authorization check.

```go
type AuthorizationDecision struct {
    // Allowed indicates whether the request is authorized
    Allowed bool

    // Reason provides a human-readable explanation
    Reason string

    // Provider that made the decision
    Provider string

    // MatchedPolicy is the role/policy that matched
    MatchedPolicy string

    // DecisionID for audit purposes
    DecisionID string

    // EvaluationTimeNs is the evaluation time in nanoseconds
    EvaluationTimeNs int64
}
```

### AuthorizationProvider Interface

```go
type AuthorizationProvider interface {
    // Authorize checks if the request is allowed
    Authorize(ctx context.Context, req *AuthorizationRequest) (*AuthorizationDecision, error)

    // BatchAuthorize performs multiple authorization checks
    BatchAuthorize(ctx context.Context, reqs []*AuthorizationRequest) ([]*AuthorizationDecision, error)

    // GetProviderType returns the provider type identifier
    GetProviderType() string

    // Close releases any resources
    Close() error
}
```

### AuthorizationService Interface

```go
type AuthorizationService interface {
    // Authorize performs a single authorization check with audit logging
    Authorize(ctx context.Context, req *AuthorizationRequest) (*AuthorizationDecision, error)

    // BatchAuthorize performs multiple authorization checks
    BatchAuthorize(ctx context.Context, reqs []*AuthorizationRequest) ([]*AuthorizationDecision, error)

    // GetProvider returns the underlying authorization provider
    GetProvider() AuthorizationProvider

    // GetProviderType returns the type of the underlying provider
    GetProviderType() string

    // Close releases any resources
    Close() error
}
```

### Role storage (go-qrdb `RoleClient`)

There is **no local `RoleManager`**. Role CRUD is delegated to go-qrdb through
`pkg/iam/qrdb_role_storage.go`. `QRDBRoleStorage` implements the IAM role-storage
seam by forwarding to the qrdb `RoleClient` (`qrdbtransport.RoleClient`), so a
role created by quicbaas and a role evaluated by the qrdb PDP resolve to ONE
record (one role ID space):

```go
// pkg/iam/qrdb_role_storage.go — delegates to the qrdb RoleClient.
type QRDBRoleStorage struct { /* resolver RoleClientResolver */ }

func (s *QRDBRoleStorage) CreateRole(ctx context.Context, r *Role) error
func (s *QRDBRoleStorage) GetRole(ctx context.Context, tenantID, roleID string) (*Role, error)
func (s *QRDBRoleStorage) UpdateRole(ctx context.Context, r *Role) error
func (s *QRDBRoleStorage) DeleteRole(ctx context.Context, tenantID, roleID string) error
func (s *QRDBRoleStorage) ListRoles(ctx context.Context, tenantID string, opts ListOptions) ([]*Role, error)
```

Request-time permission checks are performed by the SDK `platformtoken` package
against the token's resolved scopes, using the colon `resource:action`
vocabulary:

```go
// sdk/go/platformtoken — fail-closed scope check.
if err := platformtoken.Authorize(claims).RequireScope("keys:read"); err != nil {
    // *ScopeDeniedError names the concrete scope that was missing.
    return err
}
```

## Usage Examples

> **Note:** the `auth.NewRoleManager` / `auth.Role` / `auth.Permission` snippets
> in this section show the **deleted local fork API**, retained as conceptual
> background. In current code, roles are managed through the qrdb `RoleClient`
> (see *Role storage* above) and permissions are checked with
> `platformtoken.Authorize(...).RequireScope("resource:action")`.

### Configuring RBAC in QuicBaaS

```yaml
# .quicbaas.yaml
security:
  authorization:
    provider: "rbac"
    rbac:
      enabled: true
      allow_anonymous: true
    audit:
      enabled: true
      log_denied: true
```

### Creating Custom Roles

```go
package main

import (
    "github.com/jeremyhahn/go-quicbaas/pkg/auth"
    "github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func main() {
    log, _ := logger.New(logger.LogConfig{Level: "info"})
    rm := auth.NewRoleManager(log) // LEGACY: deleted fork API; roles now via qrdb RoleClient

    // Create a custom role for data scientists
    dataScientistRole := &auth.Role{
        Name:        "data-scientist",
        Description: "Access to vector and timeseries data",
        Permissions: []*auth.Permission{
            {
                Resource: "vector",
                Actions:  []string{"read", "write"},
            },
            {
                Resource: "timeseries",
                Actions:  []string{"read", "write"},
            },
            {
                Resource: "keys",
                Actions:  []string{"read"},
            },
            {
                Resource: "health",
                Actions:  []string{"read"},
            },
        },
    }

    if err := rm.AddRole(dataScientistRole); err != nil {
        log.Fatalf("Failed to add role: %v", err)
    }
}
```

### Checking Permissions Programmatically

```go
package main

import (
    "context"

    "github.com/jeremyhahn/go-quicbaas/pkg/auth"
    "github.com/jeremyhahn/go-quicbaas/pkg/auth/providers"
    "github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
    "github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func main() {
    log, _ := logger.New(logger.LogConfig{Level: "info"})

    // Create role manager with built-in roles
    rm := auth.NewRoleManager(log) // LEGACY: deleted fork API; roles now via qrdb RoleClient

    // Create RBAC provider
    provider, err := providers.NewRBACProvider(rm, log)
    if err != nil {
        log.Fatalf("Failed to create provider: %v", err)
    }
    defer provider.Close()

    // Create authorization request
    req := &interfaces.AuthorizationRequest{
        Subject: interfaces.Subject{
            ID:            "user-123",
            Username:      "alice",
            Roles:         []string{"readwrite"},
            Authenticated: true,
        },
        Resource: "keys",
        Action:   "write",
    }

    // Check authorization
    ctx := context.Background()
    decision, err := provider.Authorize(ctx, req)
    if err != nil {
        log.Fatalf("Authorization error: %v", err)
    }

    if decision.Allowed {
        log.Infof("Access granted: %s", decision.Reason)
    } else {
        log.Warnf("Access denied: %s", decision.Reason)
    }
}
```

### Batch Authorization

```go
package main

import (
    "context"

    "github.com/jeremyhahn/go-quicbaas/pkg/auth"
    "github.com/jeremyhahn/go-quicbaas/pkg/auth/providers"
    "github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
    "github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func main() {
    log, _ := logger.New(logger.LogConfig{Level: "info"})
    rm := auth.NewRoleManager(log) // LEGACY: deleted fork API; roles now via qrdb RoleClient
    provider, _ := providers.NewRBACProvider(rm, log)
    defer provider.Close()

    // Create multiple authorization requests
    subject := interfaces.Subject{
        ID:            "user-456",
        Username:      "bob",
        Roles:         []string{"readonly"},
        Authenticated: true,
    }

    reqs := []*interfaces.AuthorizationRequest{
        {Subject: subject, Resource: "keys", Action: "read"},
        {Subject: subject, Resource: "keys", Action: "write"},
        {Subject: subject, Resource: "secrets", Action: "read"},
    }

    // Batch authorization check
    ctx := context.Background()
    decisions, err := provider.BatchAuthorize(ctx, reqs)
    if err != nil {
        log.Fatalf("Batch authorization error: %v", err)
    }

    for i, decision := range decisions {
        log.Infof("Request %d: resource=%s action=%s allowed=%v reason=%s",
            i, reqs[i].Resource, reqs[i].Action, decision.Allowed, decision.Reason)
    }
}
```

### Using in HTTP Middleware

```go
package main

import (
    "net/http"

    "github.com/jeremyhahn/go-quicbaas/pkg/auth"
    "github.com/jeremyhahn/go-quicbaas/pkg/auth/providers"
    "github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

func AuthorizationMiddleware(provider interfaces.AuthorizationProvider) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Extract user from context (set by authentication middleware)
            user := r.Context().Value("user").(interfaces.Subject)

            // Map HTTP method to action
            action := mapMethodToAction(r.Method)
            resource := extractResource(r.URL.Path)

            req := &interfaces.AuthorizationRequest{
                Subject:  user,
                Resource: resource,
                Action:   action,
            }

            decision, err := provider.Authorize(r.Context(), req)
            if err != nil {
                http.Error(w, "Authorization error", http.StatusInternalServerError)
                return
            }

            if !decision.Allowed {
                http.Error(w, decision.Reason, http.StatusForbidden)
                return
            }

            next.ServeHTTP(w, r)
        })
    }
}

func mapMethodToAction(method string) string {
    switch method {
    case "GET", "HEAD":
        return "read"
    case "POST", "PUT", "PATCH":
        return "write"
    case "DELETE":
        return "delete"
    default:
        return "read"
    }
}

func extractResource(path string) string {
    // Implementation depends on your URL structure
    // Example: /api/v1/keys/foo -> "keys"
    return "keys"
}
```

## Performance

### Target Latency

| Operation | Target | Typical |
|-----------|--------|---------|
| Single authorization check | < 1us | ~500ns |
| Batch authorization (10 requests) | < 5us | ~3us |
| Role lookup | O(1) | ~100ns |

### Caching Behavior

- **Role definitions** are cached in memory at startup
- **Permission lookups** use O(1) map access
- **No external dependencies** during authorization checks
- **Thread-safe** via RWMutex for role management

### Benchmark Results

Historical numbers from the pre-unification local fork (the `RoleManager`
micro-benchmark no longer exists — permission evaluation now happens in the
go-qrdb PDP). Retained as an order-of-magnitude reference for the provider layer:

```
BenchmarkRBACProvider_Authorize-8          3000000       450 ns/op       96 B/op      2 allocs/op
BenchmarkRBACProvider_BatchAuthorize_10-8  500000        2850 ns/op      960 B/op     20 allocs/op
```

### Optimization Tips

1. **Minimize role count**: Fewer roles per user means faster permission checks
2. **Use specific roles**: Avoid wildcard permissions when possible
3. **Batch requests**: Use `BatchAuthorize` for multiple checks
4. **Pre-validate roles**: Use `ValidateRoles` during user creation

## Troubleshooting

### Common Issues

#### "authentication required" Error

**Cause:** Request lacks authentication or token is invalid.

**Solution:**
1. Verify JWT token is included in request headers
2. Check token expiration
3. Ensure `allow_anonymous: true` for public endpoints

```bash
# Check if endpoint allows anonymous access
curl -v http://localhost:8080/health
```

#### "no roles assigned" Error

**Cause:** Authenticated user has no roles.

**Solution:**
1. Assign roles to user during creation
2. Check JWT token contains `roles` claim
3. Verify role names match defined roles

```bash
# List user roles
quicbaas user get alice --show-roles
```

#### "roles [X] do not have Y permission on Z" Error

**Cause:** User's roles lack required permission.

**Solution:**
1. Add required permission to existing role
2. Create a new role with the permission
3. Assign an appropriate role to the user

```bash
# Check role permissions
quicbaas role get readwrite

# Assign additional role
quicbaas user assign-role alice admin
```

#### Custom Role Not Found

**Cause:** Custom role was not persisted or not loaded.

**Solution:**
1. Verify role was created successfully
2. Check for typos in role name
3. Ensure role manager is properly initialized

```go
// Verify role exists
if rm.GetRole("custom-role") == nil {
    log.Error("Role not found")
}
```

### Debugging Authorization

Enable debug logging:

```yaml
logging:
  level: debug

security:
  authorization:
    audit:
      enabled: true
      log_allowed: true
      log_denied: true
```

Check authorization decisions in logs:

```
level=debug msg="authorization check" subject=alice resource=keys action=write allowed=true matched_policy=readwrite evaluation_time_ns=450
```

### Audit Log Analysis

Query audit logs for denied requests:

```bash
grep "allowed=false" /var/log/quicbaas/audit.log | tail -20
```

Common patterns to look for:
- Repeated denials from same user (possible misconfiguration)
- Denials for expected resources (missing permissions)
- Unexpected resources being accessed (security concern)

## See Also

- Authentication - JWT authentication
- ABAC - Attribute-based access control
- Security Overview - Security architecture
- [API Reference](../multiprotocol-api/architecture.md) - Full API documentation
