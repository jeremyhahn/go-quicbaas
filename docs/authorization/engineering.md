# Authorization — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/authorization/`.

Complete reference for QuicBaaS IAM's Attribute-Based Access Control policy language.

## Policy Document Structure

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: string                    # Unique policy name
  tenant: string                  # Owning tenant
  description: string             # Human description
  labels: map[string]string       # Arbitrary labels
  priority: int                   # Evaluation order (higher=first)

spec:
  subjects: SubjectSelector       # WHO this applies to
  variables: map[string]Expression # Reusable expressions
  rules: []Rule                   # Authorization rules
```

## Subject Selection

```yaml
subjects:
  # Match specific entities
  users: [alice, bob, charlie]      # Specific user IDs
  groups: [engineering, devops]     # Group memberships
  roles: [admin, developer]         # Role assignments
  services: [api-gateway, worker]   # Service accounts

  # Match by attributes
  match:
    department: engineering         # Exact match
    clearance: { gte: 3 }          # Numeric comparison
    location: { in: [us, eu, uk] } # Set membership
    email: { endswith: @acme.com } # Pattern match

  # Match all authenticated users
  authenticated: true

  # Match all (including anonymous)
  any: true
```

## Rule Structure

```yaml
rules:
  - name: rule-identifier           # Unique within policy
    effect: allow | deny            # Authorization decision
    priority: 100                   # Rule priority (optional)

    actions:                        # WHAT operations
      - read
      - write
      - delete
      - list
      - admin
      - "*"                         # Wildcard (all actions)

    resources:                      # WHICH resources
      - keys/tenant/${subject.tenant}/**
      - indexes/production/*
      - "keys/shared/public-*"

    when:                           # Conditions (ALL must be true)
      - <condition>
      - <condition>

    unless:                         # Negative conditions (ANY fails)
      - <condition>
```

## Condition Operators

### Equality

```yaml
subject.department == "engineering"     # Equals
subject.department != "hr"              # Not equals
subject.department eq "engineering"     # Equals (alternate)
subject.department neq "hr"             # Not equals (alternate)
```

### Numeric Comparison

```yaml
subject.clearance > 3                   # Greater than
subject.clearance >= 3                  # Greater than or equal
subject.clearance < 5                   # Less than
subject.clearance <= 5                  # Less than or equal
resource.size between 0..10MB           # Range (inclusive)
```

### String Patterns

```yaml
subject.email endswith "@acme.com"      # Suffix match
resource.name startswith "report-"      # Prefix match
resource.path contains "/admin/"        # Substring match
resource.name matches "doc-[0-9]+"      # Regex match
resource.name like "report-*-final"     # Glob pattern
```

### Set Membership

```yaml
subject.role in [admin, operator]       # Value in set
subject.role notin [guest, anonymous]   # Value not in set
```

### Collection Operations

```yaml
subject.roles contains admin            # List contains value
subject.roles notcontains guest         # List doesn't contain
subject.roles containsall [a, b]        # List contains ALL values
subject.roles containsany [a, b, c]     # List contains ANY value
subject.groups overlaps [admins, ops]   # Lists have common elements
subject.groups disjoint [banned]        # Lists have no common elements
```

### Existence

```yaml
subject.mfa_verified exists             # Attribute is set
resource.owner notexists                # Attribute is not set
subject.metadata.custom_field exists    # Nested attribute check
```

### Time Operators

```yaml
# Time of day
time.hour between 9..17                 # Hour range (24h format)
time.minute < 30                        # Minute of hour

# Day of week
time.weekday in [mon, tue, wed, thu, fri]  # Weekdays
time.weekday in [sat, sun]              # Weekends
time.weekday notin [sat, sun]           # Not weekends

# Date comparison
time.date before 2025-12-31             # Before date
time.date after 2025-01-01              # After date
time.date between 2025-01-01..2025-12-31# Date range

# Duration / Age
time.since(subject.password_changed) < 90d   # Time since event
time.since(subject.last_login) < 7d     # Recent activity
time.until(resource.expires) > 1h       # Time until event
resource.age < 30d                      # Resource age

# Combined
time.hour between 9..17 and time.weekday in [mon..fri]
```

### Network Operators

```yaml
# IP address
request.ip in 10.0.0.0/8               # Single CIDR
request.ip in [10.0.0.0/8, 172.16.0.0/12, 203.0.113.0/24]  # Multiple
request.ip notin 0.0.0.0/0             # Exclusion
request.ip == 203.0.113.100            # Exact IP

# Named networks (defined in tenant config)
request.ip in $corporate_network        # Named network reference
request.ip in $allowed_vpns             # Named network list
```

### Logical Operators

```yaml
# Implicit AND - all conditions in 'when' list
when:
  - subject.authenticated
  - request.ip in 10.0.0.0/8
  - time.hour between 9..17

# Explicit AND
when:
  - and:
      - subject.role == admin
      - subject.mfa_verified exists

# Inline AND
when:
  - subject.authenticated and request.ip in 10.0.0.0/8

# Explicit OR
when:
  - or:
      - subject.role == admin
      - resource.owner == subject.id
      - resource.public == true

# Inline OR
when:
  - subject.role == admin or resource.owner == subject.id

# NOT
when:
  - not subject.suspended
  - not (resource.archived and resource.locked)

# Using 'unless' (equivalent to NOT)
unless:
  - subject.suspended
  - resource.deleted

# Complex combinations
when:
  - and:
      - subject.authenticated
      - or:
          - subject.role == admin
          - and:
              - resource.owner == subject.id
              - time.hour between 9..17

# Quantifiers
when:
  - all:                                # All must be true
      - subject.authenticated
      - subject.mfa_verified exists
      - request.ip in $corporate_network

  - any:                                # At least one must be true
      - subject.role == admin
      - subject.role == operator
      - resource.public == true

  - none:                               # None must be true
      - subject.suspended
      - subject.locked
      - resource.deleted
```

## Variable References

### Built-in Variables

```yaml
# Subject variables (from authenticated user/service)
${subject.id}                           # User/service ID
${subject.name}                         # Username
${subject.email}                        # Email address
${subject.tenant}                       # Tenant ID
${subject.roles}                        # List of roles
${subject.groups}                       # List of groups
${subject.attrs.<key>}                  # Custom attribute

# Resource variables (from request context)
${resource.key}                         # Full resource key
${resource.type}                        # Resource type (keys, indexes)
${resource.owner}                       # Resource owner ID
${resource.tenant}                      # Resource tenant
${resource.tags.<key>}                  # Resource tag
${resource.meta.<key>}                  # Resource metadata

# Request variables (from current request)
${request.ip}                           # Client IP
${request.method}                       # HTTP method / gRPC method
${request.protocol}                     # rest, grpc, quic
${request.id}                           # Request ID
${request.headers.<key>}                # Request header

# Environment variables (from system)
${env.cluster}                          # Cluster ID
${env.node}                             # Node ID
${env.region}                           # Geographic region
${env.environment}                      # dev, staging, production
```

### Custom Variables

```yaml
spec:
  variables:
    tenant_prefix: "tenant/${subject.tenant}/"
    is_owner: resource.owner == subject.id
    is_admin: subject.roles contains admin
    is_business_hours: time.hour between 9..17 and time.weekday in [mon..fri]

  rules:
    - when:
        - ${is_owner} or ${is_admin}
        - ${is_business_hours}
      resources:
        - keys/${tenant_prefix}**
```

## Vector Database Context

Vector operations provide additional context attributes for fine-grained access control.

### Vector Context Variables

```yaml
# Vector operation context (automatically populated for vector operations)
${context.namespace}                    # Target namespace (e.g., "embeddings-prod")
${context.vector_id}                    # Target vector ID (if applicable)
${context.operation}                    # Operation type: upsert, search, delete, batch_upsert, etc.
${context.batch_size}                   # Number of vectors in batch operations
${context.has_metadata}                 # Whether vector has metadata (boolean)
${context.metadata_count}               # Number of metadata fields
${context.tenant_id}                    # Tenant ID from request context
${context.user_id}                      # User ID from request context
```

### Vector Resource Format

```yaml
resources:
  # All vector namespaces
  - vector:namespace:*

  # Specific namespace
  - vector:namespace:embeddings-prod

  # Namespace prefix pattern
  - vector:namespace:ml-*

  # Tenant-scoped namespaces
  - vector:namespace:${subject.tenant}/*
```

### Vector Policy Examples

#### Namespace-Level Access

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: ml-team-embeddings
  tenant: acme-corp
  description: ML team full access to embeddings namespaces

spec:
  subjects:
    groups: [ml-engineers]

  rules:
    - name: full-access-embeddings
      effect: allow
      actions:
        - vector:read
        - vector:write
        - vector:delete
        - vector:search
      resources:
        - vector:namespace:embeddings-*
```

#### Batch Size Limits

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: batch-size-limit
  tenant: acme-corp
  description: Limit batch operations for non-admin users

spec:
  subjects:
    authenticated: true

  rules:
    - name: deny-large-batches
      effect: deny
      actions: [vector:write]
      resources:
        - vector:namespace:*
      when:
        - context.batch_size > 1000
      unless:
        - subject.roles contains vector-admin
```

#### Read-Only Search Access

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: search-only-access
  tenant: acme-corp
  description: Allow search operations only

spec:
  subjects:
    groups: [api-consumers]

  rules:
    - name: allow-search
      effect: allow
      actions: [vector:search, vector:read]
      resources:
        - vector:namespace:public-*
      when:
        - context.operation in [search, hybrid_search, search_by_id]
```

#### Maintenance Window Restrictions

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: maintenance-window
  tenant: acme-corp
  description: Admin operations only during maintenance windows

spec:
  subjects:
    roles: [vector-admin]

  rules:
    - name: allow-admin-during-maintenance
      effect: allow
      actions: [vector:admin]
      resources:
        - vector:namespace:*
      when:
        - time.hour between 2..6
        - time.weekday in [saturday, sunday]

    - name: deny-admin-outside-maintenance
      effect: deny
      priority: 100
      actions: [vector:admin]
      resources:
        - vector:namespace:production-*
      unless:
        - time.hour between 2..6
```

#### Cross-Namespace Protection

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: namespace-isolation
  tenant: acme-corp
  description: Enforce namespace isolation

spec:
  subjects:
    authenticated: true

  rules:
    - name: allow-own-namespace
      effect: allow
      actions: [vector:read, vector:write, vector:search]
      resources:
        - vector:namespace:${subject.id}-*

    - name: deny-cross-namespace
      effect: deny
      priority: 1000
      actions: [vector:write, vector:delete]
      resources:
        - vector:namespace:*
      unless:
        - context.namespace startswith ${subject.id}
        - subject.roles contains vector-admin
```

#### Metadata-Based Restrictions

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: metadata-required
  tenant: acme-corp
  description: Require metadata for production vectors

spec:
  subjects:
    authenticated: true

  rules:
    - name: require-metadata-prod
      effect: deny
      actions: [vector:write]
      resources:
        - vector:namespace:production-*
      when:
        - not context.has_metadata
      unless:
        - subject.roles contains vector-admin
```

### Vector Actions Reference

| Action | Description | Methods |
|--------|-------------|---------|
| `vector:read` | Read vectors and stats | GetVector, ListVectors, GetVectorStats, Health |
| `vector:write` | Create/update vectors | UpsertVector, BatchUpsertVectors, UpdateMetadata |
| `vector:delete` | Delete vectors | DeleteVector, BatchDeleteVectors |
| `vector:search` | Search operations | SearchSimilar, SearchByID, HybridSearch, SearchByMetadata |
| `vector:namespace` | Namespace management | CreateNamespace, DeleteNamespace, UpdateNamespace |
| `vector:admin` | Admin operations | RebuildIndex, GetIndexProgress, CompactNamespace |
| `vector:stats` | Statistics access | GetVectorStats, GetNamespaceInfo |

---

## Resource Patterns

```yaml
resources:
  # Exact match
  - keys/config/database

  # Single-level wildcard
  - keys/users/*/profile           # Matches keys/users/alice/profile

  # Multi-level wildcard
  - keys/tenant/acme/**            # Matches any depth under acme/

  # Variable substitution
  - keys/tenant/${subject.tenant}/**
  - keys/users/${subject.id}/**

  # Glob patterns
  - keys/reports/2024-*-final
  - keys/cache/session-[0-9]*
```

## Complete Example

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: engineering-team-access
  tenant: acme-corp
  description: Access control for engineering team members
  labels:
    team: engineering
    environment: production

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
    # Engineers can read all tenant data
    - name: read-tenant-data
      effect: allow
      actions: [read, list]
      resources:
        - keys/${tenant_prefix}**
        - indexes/${tenant_prefix}**
      when:
        - subject.authenticated
        - request.ip in $corporate_network

    # Engineers can write to their own namespace
    - name: write-own-data
      effect: allow
      actions: [write, delete]
      resources:
        - keys/${user_prefix}**
      when:
        - ${is_business_hours}

    # Engineers can write to shared project data if they own it
    - name: write-project-data
      effect: allow
      actions: [write]
      resources:
        - keys/${tenant_prefix}projects/**
      when:
        - or:
            - ${is_owner}
            - subject.roles contains project-lead

    # Admins have full access
    - name: admin-full-access
      effect: allow
      actions: ["*"]
      resources:
        - keys/${tenant_prefix}**
        - indexes/${tenant_prefix}**
      when:
        - ${is_admin}
        - subject.mfa_verified exists
        - time.since(subject.mfa_timestamp) < 4h

    # Block access to production data outside business hours
    - name: deny-production-off-hours
      effect: deny
      priority: 1000
      actions: [write, delete]
      resources:
        - keys/${tenant_prefix}production/**
      unless:
        - ${is_business_hours}
        - ${is_admin}

    # Block all cross-tenant access
    - name: deny-cross-tenant
      effect: deny
      priority: 9999
      actions: ["*"]
      resources:
        - keys/tenant/**
      unless:
        - resource.path startswith ${tenant_prefix}
```

## Evaluation Order

1. **Explicit deny** rules are evaluated first (sorted by priority)
2. If any deny matches, request is **denied** immediately
3. **Allow** rules are evaluated next (sorted by priority)
4. If any allow matches, request is **allowed**
5. If no rules match, **implicit deny** (default)

## Best Practices

1. **Use variables** for repeated expressions
2. **Start with deny-all**, add specific allows
3. **Use priority** for deny rules to ensure they run first
4. **Keep policies focused** - one policy per logical grouping
5. **Test with dry-run** before applying
6. **Use named networks** instead of hardcoded CIDRs
7. **Require MFA** for sensitive operations
8. **Time-bound access** where appropriate
