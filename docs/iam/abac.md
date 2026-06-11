# Attribute-Based Access Control (ABAC)

QuicBaaS implements enterprise-grade ABAC authorization through the IAM module, enabling fine-grained access control based on attributes of subjects, resources, and environmental context.

## Overview

### What is ABAC?

Attribute-Based Access Control (ABAC) is an authorization model that evaluates access requests based on attributes rather than static role assignments. ABAC policies can incorporate:

- **Subject attributes**: User properties like department, clearance level, or group membership
- **Resource attributes**: Properties of the data being accessed like owner, classification, or age
- **Action**: The operation being performed (read, write, delete, admin)
- **Context**: Environmental factors like time of day, IP address, or MFA status

### QuicBaaS ABAC Implementation

QuicBaaS implements ABAC through the `pkg/iam` module with the following components:

| Component | Description |
|-----------|-------------|
| `PolicyEvaluator` | Core engine that evaluates authorization requests against compiled policies |
| `PolicyCompiler` | Compiles YAML policies into optimized data structures for fast evaluation |
| `PolicyCache` | Caches compiled policies per tenant for sub-microsecond lookups |
| `ABACProvider` | Implements `interfaces.AuthorizationProvider` wrapping the PolicyEvaluator |

### Build Tag Requirement

ABAC functionality is always compiled into every build; no build tag is required:

```bash
go build ./cmd/quicbaasd
```

When built without `iam`, ABAC features are not available and the system falls back to RBAC-only mode.

### When to Use ABAC vs RBAC

| Use Case | Recommended Approach |
|----------|---------------------|
| Simple role-based access (admin, user, guest) | RBAC |
| Department or team-based access | ABAC |
| Resource ownership rules | ABAC |
| Time-based access restrictions | ABAC |
| IP-based access control | ABAC |
| Multi-tenant data isolation | ABAC |
| Complex business rules | ABAC |
| Gradual RBAC-to-ABAC migration | Hybrid |

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Protocol Handlers                         │
│  REST  │  gRPC  │  QUIC  │  MCP                             │
└────────┴────────┴────────┴──────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                  AuthorizationService                        │
│     Authorize(Subject, Resource, Action) -> Decision        │
│                                                             │
│  - Unified interface for all protocols                      │
│  - Audit logging of decisions                               │
│  - Prometheus metrics collection                            │
│  - Optional decision caching                                │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    ABAC Provider                             │
│                  (pkg/iam/evaluator.go)                     │
│                                                             │
│  - Wraps PolicyEvaluator                                    │
│  - Converts interfaces.AuthorizationRequest to iam.Check   │
│  - Always compiled (no build tag since C1-G)                │
│  - Thread-safe via atomic operations                        │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    PolicyEvaluator                           │
│                (pkg/iam/evaluator.go)                       │
│                                                             │
│  Evaluate(subject, resource, action, context) -> decision  │
│                                                             │
│  1. Load applicable policies from cache                     │
│  2. Match subject against policy selectors                  │
│  3. Evaluate deny rules first (sorted by priority)          │
│  4. Evaluate allow rules (first match wins)                 │
│  5. Default deny if no rules match                          │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    PolicyCache                               │
│                                                             │
│  - Per-tenant compiled policy storage                       │
│  - Configurable TTL (default: 5m)                           │
│  - Thread-safe concurrent access                            │
│  - Automatic invalidation on policy updates                 │
└─────────────────────────────────────────────────────────────┘
```

## Key Concepts

### Subject

The entity making the request. Defined in `pkg/interfaces/authz.go`:

```go
type Subject struct {
    // ID is the unique identifier (user ID, service account ID)
    ID string

    // Username is the human-readable name
    Username string

    // TenantID identifies the tenant for multi-tenant deployments
    TenantID string

    // Roles contains RBAC roles assigned to the subject
    Roles []string

    // Groups contains group memberships
    Groups []string

    // Attributes contains custom attributes for ABAC evaluation
    // Example: {"department": "engineering", "clearance_level": "secret"}
    Attributes map[string]interface{}

    // Authenticated indicates whether the subject is authenticated
    Authenticated bool
}
```

**Subject attributes in policies:**
```yaml
${subject.id}           # User/service ID
${subject.name}         # Username
${subject.email}        # Email address
${subject.tenant}       # Tenant ID
${subject.roles}        # List of roles
${subject.groups}       # List of groups
${subject.attrs.<key>}  # Custom attribute
```

### Resource

What is being accessed. QuicBaaS defines standard resources in `pkg/interfaces/authz.go`:

| Resource | Description |
|----------|-------------|
| `keys` | Key-value data operations |
| `transactions` | Transaction operations |
| `cluster` | Cluster management operations |
| `backups` | Backup and restore operations |
| `users` | User management operations |
| `roles` | Role management operations |
| `secrets` | Secrets management operations |
| `kms` | Key Management Service operations |
| `crypto` | Cryptographic operations (encrypt/decrypt/sign/verify) |
| `seal` | Seal/unseal operations |
| `health` | Health check operations |
| `metrics` | Metrics access |
| `watch` | Watch/subscription operations |
| `timeseries` | Timeseries data operations |
| `vector` | Vector database operations |
| `*` | All resources (wildcard) |

### Action

What operation is being performed:

| Action | Description |
|--------|-------------|
| `read` | Read operations (GET, LIST, QUERY) |
| `write` | Write operations (PUT, POST, UPDATE) |
| `delete` | Delete operations |
| `admin` | Administrative operations |
| `*` | All actions (wildcard) |

### Context

Additional contextual information for policy evaluation:

```go
type AuthorizationRequest struct {
    Subject  Subject
    Resource string
    Action   string

    // Context contains additional contextual information
    // Example: {"ip_address": "192.168.1.100", "time": "2025-01-15T10:30:00Z"}
    Context map[string]interface{}
}
```

**Context variables in policies:**
```yaml
${request.ip}          # Client IP address
${request.method}      # HTTP method / gRPC method
${request.protocol}    # Protocol (rest, grpc, quic)
${request.id}          # Request ID
${request.headers.<k>} # Request header
${time.hour}           # Current hour (0-23)
${time.weekday}        # Day of week (mon, tue, etc.)
${env.cluster}         # Cluster ID
${env.region}          # Geographic region
```

### Policy

Rules that determine access. See [ABAC Policy Syntax](abac-policy-syntax.md) for complete reference.

## Policy Structure

### Basic Policy Format

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: policy-name
  tenant: tenant-id
  description: Human-readable description
  labels:
    environment: production
    team: engineering
  priority: 100  # Higher = evaluated first

spec:
  subjects:
    # Who this policy applies to
    groups: [engineering]
    match:
      department: engineering

  variables:
    # Reusable expressions
    tenant_prefix: "tenant/${subject.tenant}/"
    is_owner: resource.owner == subject.id

  rules:
    - name: rule-name
      effect: allow  # or deny
      actions: [read, write]
      resources:
        - keys/${tenant_prefix}**
      when:
        - subject.authenticated
        - request.ip in 10.0.0.0/8
      unless:
        - subject.suspended
```

### Subject Selection

```yaml
subjects:
  # Match specific entities
  users: [alice, bob]
  groups: [engineering, devops]
  roles: [admin, developer]
  services: [api-gateway]

  # Match by attributes
  match:
    department: engineering
    clearance: { gte: 3 }
    location: { in: [us, eu] }

  # Match all authenticated users
  authenticated: true

  # Match all (including anonymous)
  any: true
```

### Rule Structure

```yaml
rules:
  - name: rule-identifier
    effect: allow | deny
    priority: 100  # Optional, higher = first

    actions:
      - read
      - write
      - "*"  # Wildcard

    resources:
      - keys/tenant/${subject.tenant}/**  # Multi-level wildcard
      - keys/users/*/profile               # Single-level wildcard

    when:    # All conditions must be true
      - subject.authenticated
      - request.ip in 10.0.0.0/8

    unless:  # Any true condition blocks
      - subject.suspended
```

### Complete Example

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: engineering-data-access
  tenant: acme-corp
  description: Allow engineering team access to their data
  priority: 100

spec:
  subjects:
    groups: [engineering]
    match:
      department: engineering
      clearance: { gte: 2 }

  variables:
    tenant_prefix: "tenant/${subject.tenant}/"
    user_prefix: "tenant/${subject.tenant}/users/${subject.id}/"
    is_owner: resource.owner == subject.id
    is_admin: subject.roles contains admin
    is_business_hours: time.hour between 9..17 and time.weekday in [mon, tue, wed, thu, fri]

  rules:
    # Read access to all tenant data
    - name: read-tenant-data
      effect: allow
      actions: [read, list]
      resources:
        - keys/${tenant_prefix}**
      when:
        - subject.authenticated
        - request.ip in $corporate_network

    # Write access to own namespace
    - name: write-own-data
      effect: allow
      actions: [write, delete]
      resources:
        - keys/${user_prefix}**
      when:
        - ${is_business_hours}

    # Admin full access with MFA
    - name: admin-full-access
      effect: allow
      actions: ["*"]
      resources:
        - keys/${tenant_prefix}**
      when:
        - ${is_admin}
        - subject.mfa_verified exists

    # Block cross-tenant access
    - name: deny-cross-tenant
      effect: deny
      priority: 9999
      actions: ["*"]
      resources:
        - keys/tenant/**
      unless:
        - resource.path startswith ${tenant_prefix}
```

## Hybrid Provider

The Hybrid provider enables gradual migration from RBAC to ABAC by combining both authorization strategies.

### How It Works

```
┌─────────────────────────────────────────────┐
│            HybridProvider                    │
│                                             │
│  1. Try ABAC provider (if available)        │
│     └─> If successful: return ABAC decision │
│     └─> If error: continue to RBAC          │
│                                             │
│  2. Fall back to RBAC provider              │
│     └─> Return RBAC decision                │
│                                             │
│  Decision.Provider indicates source:        │
│  - "hybrid(abac)" - ABAC made decision      │
│  - "hybrid(rbac)" - RBAC made decision      │
└─────────────────────────────────────────────┘
```

### Configuration

```yaml
security:
  authorization:
    provider: "hybrid"  # Enable hybrid mode

    rbac:
      enabled: true
      default_role: "readonly"

    abac:
      enabled: true
      cache_ttl: "5m"
      policy_dir: "/etc/quicbaas/policies"
```

### Migration Path

1. **Phase 1: RBAC Only**
   - Start with `provider: "rbac"`
   - Define roles and permissions

2. **Phase 2: Hybrid Mode**
   - Switch to `provider: "hybrid"`
   - Add ABAC policies for new use cases
   - RBAC continues handling existing access patterns

3. **Phase 3: ABAC Primary**
   - Convert remaining RBAC rules to ABAC policies
   - RBAC serves as fallback safety net

4. **Phase 4: ABAC Only**
   - Switch to `provider: "abac"`
   - Remove RBAC configuration

## Configuration

### Full Configuration Reference

```yaml
# quicbaas.yaml
security:
  authorization:
    # Provider type: "rbac", "abac", or "hybrid"
    provider: "abac"

    # RBAC configuration
    rbac:
      # Enable RBAC authorization (default: true)
      enabled: true

      # Default role for unauthenticated requests
      # Options: "readonly", "readwrite", "admin", or custom role
      # If empty, unauthenticated requests are denied
      default_role: ""

      # Allow anonymous access to public endpoints
      # Default: true (health, metrics accessible without auth)
      allow_anonymous: true

    # ABAC configuration
    abac:
      # Enable ABAC authorization
      # Default: false
      enabled: true

      # Cache TTL for policy evaluation results
      # Set to 0 to disable caching
      # Default: 5m
      cache_ttl: "5m"

      # Directory containing ABAC policy files
      # Watches for changes and auto-reloads
      policy_dir: "/etc/quicbaas/policies"

    # Audit logging configuration
    audit:
      # Enable audit logging of authorization decisions
      # Default: true
      enabled: true

      # Log denied authorization requests
      # Default: true
      log_denied: true

      # Log allowed authorization requests
      # Default: false (enable for compliance)
      log_allowed: false
```

### Environment Variables

Configuration can be set via environment variables:

```bash
QUICBAAS_SECURITY_AUTHORIZATION_PROVIDER=abac
QUICBAAS_SECURITY_AUTHORIZATION_ABAC_ENABLED=true
QUICBAAS_SECURITY_AUTHORIZATION_ABAC_CACHE_TTL=5m
QUICBAAS_SECURITY_AUTHORIZATION_ABAC_POLICY_DIR=/etc/quicbaas/policies
```

## Building with ABAC Support

### Build Commands

```bash
# Build server (ABAC/IAM always compiled)
go build ./cmd/quicbaasd

# Build CLI
go build ./cmd/quicbaas

# Build with FIPS
GOFIPS140=latest go build -tags fips ./cmd/quicbaasd

# Run tests
go test ./pkg/iam/...
```

### Verify ABAC Availability

```bash
# Check provider availability at runtime
./quicbaas config validate --config quicbaas.yaml
# Will error if ABAC is configured but the policy engine cannot be built
```

## API Reference

### AuthorizationProvider Interface

Defined in `pkg/interfaces/authz.go`:

```go
type AuthorizationProvider interface {
    // Authorize checks if the request is allowed
    // Performance target: < 5us with cache
    Authorize(ctx context.Context, req *AuthorizationRequest) (*AuthorizationDecision, error)

    // BatchAuthorize performs multiple checks efficiently
    BatchAuthorize(ctx context.Context, reqs []*AuthorizationRequest) ([]*AuthorizationDecision, error)

    // GetProviderType returns "rbac", "abac", or "hybrid"
    GetProviderType() string

    // Close releases resources
    Close() error
}
```

### ABACProvider

Defined in `pkg/iam/evaluator.go` (always compiled since C1-G):

```go
// ABACProvider implements AuthorizationProvider using the IAM PolicyEvaluator
type ABACProvider struct {
    evaluator *iam.PolicyEvaluator
    tenantID  string
    logger    *logger.Logger
    closed    atomic.Bool
}

// NewABACProvider creates a new ABAC authorization provider
func NewABACProvider(
    evaluator *iam.PolicyEvaluator,
    tenantID string,
    log *logger.Logger,
) (*ABACProvider, error)
```

### PolicyEvaluator

Defined in `pkg/iam/evaluator.go`:

```go
type PolicyEvaluator struct {
    compiler *PolicyCompiler
    cache    *PolicyCache
}

// NewPolicyEvaluator creates a new policy evaluator
func NewPolicyEvaluator(compiler *PolicyCompiler, cache *PolicyCache) *PolicyEvaluator

// Evaluate evaluates an authorization request
// Target: 2-5us total evaluation time
func (e *PolicyEvaluator) Evaluate(ctx context.Context, req *CheckRequest) (*CheckResponse, error)
```

### AuthorizationDecision

```go
type AuthorizationDecision struct {
    // Whether the request is authorized
    Allowed bool

    // Human-readable explanation
    Reason string

    // Provider that made the decision ("rbac", "abac", "hybrid(abac)", "hybrid(rbac)")
    Provider string

    // Policy/rule that matched
    // RBAC: role name (e.g., "admin")
    // ABAC: policy/rule (e.g., "engineering-access/read-data")
    MatchedPolicy string

    // Unique identifier for audit
    DecisionID string

    // Evaluation time in nanoseconds
    EvaluationTimeNs int64
}
```

## Usage Examples

### Creating a Policy Programmatically

```go
policy := &iam.Policy{
    APIVersion: "abac.quicbaas.io/v1",
    Kind:       "Policy",
    Metadata: iam.PolicyMetadata{
        Name:        "api-access",
        Tenant:      "my-tenant",
        Description: "API service access policy",
        Priority:    100,
    },
    Spec: iam.PolicySpec{
        Subjects: iam.SubjectSelector{
            Services: []string{"api-gateway"},
        },
        Rules: []iam.Rule{
            {
                Name:      "allow-read",
                Effect:    iam.EffectAllow,
                Actions:   []string{"read"},
                Resources: []string{"keys/**"},
            },
        },
    },
}

// Validate policy
if err := policy.Validate(); err != nil {
    log.Fatal(err)
}
```

### Evaluating Access

```go
// Create authorization request
req := &interfaces.AuthorizationRequest{
    Subject: interfaces.Subject{
        ID:            "user-123",
        Username:      "alice",
        TenantID:      "acme",
        Roles:         []string{"developer"},
        Groups:        []string{"engineering"},
        Attributes:    map[string]interface{}{
            "department":      "engineering",
            "clearance_level": 3,
        },
        Authenticated: true,
    },
    Resource: "keys",
    Action:   "read",
    Context: map[string]interface{}{
        "ip_address": "192.168.1.100",
    },
}

// Evaluate
decision, err := provider.Authorize(ctx, req)
if err != nil {
    log.Fatal(err)
}

if decision.Allowed {
    fmt.Printf("Access granted by %s (policy: %s)\n",
        decision.Provider, decision.MatchedPolicy)
} else {
    fmt.Printf("Access denied: %s\n", decision.Reason)
}
```

### Multi-Tenant Scenario

```go
// Create per-tenant evaluators
tenantEvaluators := make(map[string]*iam.PolicyEvaluator)

for _, tenant := range tenants {
    compiler := iam.NewPolicyCompiler()
    cache := iam.NewPolicyCache(iam.PolicyCacheOptions{
        TTL:      5 * time.Minute,
        MaxItems: 1000,
    })

    // Load tenant-specific policies
    policies, err := loadPoliciesForTenant(tenant.ID)
    if err != nil {
        log.Fatal(err)
    }

    for _, policy := range policies {
        compiled, err := compiler.Compile(policy)
        if err != nil {
            log.Fatal(err)
        }
        cache.Store(tenant.ID, compiled)
    }

    tenantEvaluators[tenant.ID] = iam.NewPolicyEvaluator(compiler, cache)
}

// Use appropriate evaluator based on request
func authorize(tenantID string, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
    evaluator, ok := tenantEvaluators[tenantID]
    if !ok {
        return nil, fmt.Errorf("unknown tenant: %s", tenantID)
    }

    provider, err := providers.NewABACProvider(evaluator, tenantID, logger)
    if err != nil {
        return nil, err
    }

    return provider.Authorize(ctx, req)
}
```

### Context-Based Access Control

```go
// Time-based access
req := &interfaces.AuthorizationRequest{
    Subject: subject,
    Resource: "production-db",
    Action:   "write",
    Context: map[string]interface{}{
        "ip_address":  "10.0.0.50",
        "request_time": time.Now(),
        "mfa_verified": true,
    },
}

// Policy with time conditions
/*
rules:
  - name: business-hours-only
    effect: allow
    actions: [write]
    resources: [production-db]
    when:
      - time.hour between 9..17
      - time.weekday in [mon, tue, wed, thu, fri]
      - request.ip in 10.0.0.0/8
      - subject.mfa_verified exists
*/
```

## Performance

### Target Latencies

| Operation | Target Latency | Notes |
|-----------|----------------|-------|
| RBAC check | < 1us | Simple map lookup |
| ABAC check (cached) | < 5us | With compiled policy cache |
| ABAC check (cold) | < 50us | Policy compilation required |
| Batch authorization | ~3us per request | Amortized overhead |

### Optimization Strategies

1. **Policy Caching**: Compiled policies are cached per-tenant with configurable TTL
2. **Resource Trie**: Resources are matched using a trie data structure for O(log n) lookups
3. **Subject Pre-filtering**: Policies are filtered by subject before rule evaluation
4. **Priority Ordering**: Rules are pre-sorted by priority to enable early exits

### Cache Configuration

```yaml
abac:
  # Time-to-live for cached policy compilations
  cache_ttl: "5m"

  # Maximum number of cached policies (per tenant)
  # Uncomment to override default
  # max_cache_size: 10000
```

### Monitoring Performance

```bash
# Prometheus metrics
quicbaas_authorization_duration_seconds{provider="abac", decision="allow"}
quicbaas_authorization_duration_seconds{provider="abac", decision="deny"}
quicbaas_authorization_cache_hits_total
quicbaas_authorization_cache_misses_total
quicbaas_policy_evaluation_rules_total
```

## Troubleshooting

### Common Issues

#### Policy Not Matching

**Symptoms**: Expected allow/deny not occurring

**Debugging**:
1. Enable debug logging:
   ```yaml
   logging:
     level: debug
   ```

2. Check policy evaluation logs:
   ```
   level=debug msg="authorization decision made"
     subject=user-123 resource=keys action=read
     allowed=false matched_policy="" latency_ns=2341
   ```

3. Verify subject attributes match policy selectors

4. Use policy simulation:
   ```go
   result, err := evaluator.Simulate(ctx, &iam.PolicySimulationRequest{
       Policy:       policy,
       CheckRequest: checkReq,
       IncludeTrace: true,
   })
   for _, step := range result.EvaluationSteps {
       fmt.Printf("Step %d: %s - %v (%s)\n",
           step.StepNumber, step.Action, step.Result, step.Details)
   }
   ```

#### Cache Invalidation Issues

**Symptoms**: Policy changes not taking effect

**Solution**:
1. Check cache TTL is appropriate for your use case
2. Force cache invalidation on policy update:
   ```go
   cache.Invalidate(tenantID, policyName)
   ```
3. Reduce cache TTL for development:
   ```yaml
   abac:
     cache_ttl: "30s"
   ```

#### Performance Degradation

**Symptoms**: Authorization checks exceeding 5us

**Diagnosis**:
1. Check number of policies per tenant
2. Review policy complexity (many conditions)
3. Monitor cache hit rate

**Solutions**:
1. Consolidate similar policies
2. Use priority to order deny rules first (early exit)
3. Increase cache size if hit rate is low
4. Pre-warm cache on startup

### Debug Logging

Enable verbose authorization logging:

```yaml
logging:
  level: debug
  format: json

security:
  authorization:
    audit:
      enabled: true
      log_denied: true
      log_allowed: true  # Enable for debugging
```

Sample debug output:
```json
{
  "level": "debug",
  "msg": "authorization decision made",
  "provider": "abac",
  "subject": "user-123",
  "resource": "keys",
  "action": "read",
  "allowed": true,
  "matched_policy": "engineering-access/read-data",
  "latency_ns": 2341,
  "decision_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

## See Also

- [ABAC Policy Syntax Reference](abac-policy-syntax.md) - Complete policy language documentation
- [IAM Architecture](architecture.md) - Overall IAM system design
- [API Reference](api-reference.md) - REST and gRPC API documentation
- [Audit Logging](audit-logging.md) - Authorization audit logging configuration
