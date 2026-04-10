# Object Storage Access Control

RBAC, ABAC, and ACL-based access control for object storage.

## Overview

Access control provides:
- Role-Based Access Control (RBAC) with built-in roles
- Attribute-Based Access Control (ABAC) for fine-grained policies
- Per-object ACLs for granular permissions
- Tenant isolation
- Namespace-level permissions

## Access Control Layers

```
┌───────────────────────────────────────────────────────────────────┐
│                       Access Control Stack                        │
├───────────────────────────────────────────────────────────────────┤
│                                                                   │
│  Layer 1: Authentication                                          │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  JWT Token / API Key / mTLS                                 │  │
│  │  → Establishes identity (subject)                           │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                              ↓                                    │
│  Layer 2: RBAC (Role-Based)                                       │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  User has role → Role has permissions                       │  │
│  │  objstore-admin, objstore-readwrite, etc.             │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                              ↓                                    │
│  Layer 3: ABAC (Attribute-Based)                                  │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  Policies based on attributes                               │  │
│  │  tenant, namespace, time, IP, object size, etc.             │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                              ↓                                    │
│  Layer 4: Object ACL                                              │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  Per-object permissions                                     │  │
│  │  Owner, grants, public access                               │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                                                                   │
└───────────────────────────────────────────────────────────────────┘
```

## Resource Types

| Resource | Description |
|----------|-------------|
| `objstore` | Object operations (read, write, delete) |
| `bucket` | Bucket/namespace management |
| `catalog` | Catalog queries and metadata |

## Actions

| Action | Description |
|--------|-------------|
| `read` | Read objects |
| `write` | Write/update objects |
| `delete` | Delete objects |
| `replicate` | Copy to other backends |
| `migrate` | Move between backends |
| `archive` | Move to archive tier |
| `admin` | Administrative operations |

## RBAC - Built-in Roles

### objstore-admin

Full access to object storage:

```yaml
name: objstore-admin
description: Full access to object storage
permissions:
  - resource: objstore
    actions: ["*"]
  - resource: bucket
    actions: ["*"]
  - resource: catalog
    actions: ["*"]
```

### objstore-readwrite

Read and write objects, no admin:

```yaml
name: objstore-readwrite
description: Read/write objects, no admin
permissions:
  - resource: objstore
    actions: [read, write, delete]
  - resource: catalog
    actions: [read]
```

### objstore-readonly

Read-only access:

```yaml
name: objstore-readonly
description: Read-only access to objects
permissions:
  - resource: objstore
    actions: [read]
  - resource: catalog
    actions: [read]
```

### Custom Roles

Define custom roles in configuration:

```yaml
auth:
  rbac:
    custom_roles:
      - name: tenant-acme-admin
        description: ACME Corp object storage admin
        permissions:
          - resource: objstore
            actions: [read, write, delete, replicate, archive]
          - resource: catalog
            actions: [read, write]
        conditions:
          tenant_id: tenant-acme

      - name: backup-operator
        description: Backup operations only
        permissions:
          - resource: objstore
            actions: [read, write]
          - resource: catalog
            actions: [read]
        conditions:
          namespace_prefix: backups:
```

## ABAC - Attribute-Based Policies

### Policy Structure

```go
type ObjectStorePolicy struct {
    Name        string
    Description string
    Rules       []PolicyRule
}

type PolicyRule struct {
    Effect     string           // "allow" or "deny"
    Principal  PrincipalMatch
    Action     []string
    Resource   ResourceMatch
    Condition  map[string]any
}
```

### Tenant Isolation Policy

```yaml
policies:
  - name: tenant-isolation
    description: Tenants can only access their own namespace
    rules:
      - effect: allow
        principal:
          attribute: tenant_id
          equals: "${request.tenant_id}"
        action:
          - objstore:read
          - objstore:write
          - objstore:delete
        resource:
          attribute: namespace
          starts_with: "${subject.tenant_id}:"
```

### Time-Based Access

```yaml
policies:
  - name: business-hours-only
    description: Write access only during business hours
    rules:
      - effect: allow
        action: [objstore:write]
        condition:
          time_between: ["09:00", "17:00"]
          day_of_week: [1, 2, 3, 4, 5]  # Mon-Fri
```

### IP-Based Restriction

```yaml
policies:
  - name: archive-ip-restriction
    description: Archive operations from internal network only
    rules:
      - effect: allow
        action: [objstore:archive]
        condition:
          ip_in_range: "10.0.0.0/8"
```

### Size-Based Policy

```yaml
policies:
  - name: size-limit
    description: Limit object size for regular users
    rules:
      - effect: deny
        principal:
          role_not: [objstore-admin]
        action: [objstore:write]
        condition:
          object_size_greater_than: 104857600  # 100MB
```

## Object ACLs

### ACL Structure

```go
type ObjectACL struct {
    Owner       string        // User who created the object
    OwnerAccess []string      // Owner's permissions
    Public      bool          // Publicly readable?
    Grants      []ACLGrant    // Explicit grants
}

type ACLGrant struct {
    Principal     string   // User ID, role, group, or "*"
    PrincipalType string   // "user", "role", "group", "tenant"
    Permissions   []string // "read", "write", "delete", "admin"
}
```

### Catalog Entry with ACL

```json
{
  "key": "tenant-acme:docs/report.pdf",
  "acl": {
    "owner": "user@example.com",
    "owner_access": ["read", "write", "delete", "admin"],
    "public": false,
    "grants": [
      {
        "principal": "finance-team",
        "principal_type": "group",
        "permissions": ["read", "write"]
      },
      {
        "principal": "auditor@example.com",
        "principal_type": "user",
        "permissions": ["read"]
      }
    ]
  }
}
```

### ACL Evaluation

```go
func checkObjectACL(entry *CatalogEntry, subject *Subject, action string) bool {
    acl := entry.ACL

    // Owner has full access
    if acl.Owner == subject.UserID {
        return true
    }

    // Check explicit grants
    for _, grant := range acl.Grants {
        if matchesPrincipal(grant, subject) && hasPermission(grant, action) {
            return true
        }
    }

    // Check public access for read
    if action == "read" && acl.Public {
        return true
    }

    return false
}
```

## Authorization Flow

```
Request: PUT tenant-acme:docs/report.pdf
                    │
                    ▼
┌─────────────────────────────────────────┐
│ 1. Extract Subject from Token           │
│    - UserID: user@example.com           │
│    - TenantID: tenant-acme              │
│    - Roles: [objstore-readwrite]     │
└─────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────┐
│ 2. RBAC Check                           │
│    - Role: objstore-readwrite        │
│    - Has permission: objstore:write  │
│    → PASS                               │
└─────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────┐
│ 3. ABAC Check                           │
│    - Policy: tenant-isolation           │
│    - Subject tenant: tenant-acme        │
│    - Resource namespace: tenant-acme    │
│    → PASS                               │
└─────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────┐
│ 4. ACL Check (if object exists)         │
│    - Check owner/grants                 │
│    → PASS or N/A for new objects        │
└─────────────────────────────────────────┘
                    │
                    ▼
            Request Authorized
```

## Configuration

### Enable Access Control

```yaml
auth:
  enabled: true

  rbac:
    enabled: true
    default_roles:
      - objstore-readonly

  abac:
    enabled: true
    policies_file: /etc/quicbaas/policies.yaml

object_storage:
  access_control:
    enabled: true
    enforce_tenant_isolation: true
    default_acl:
      owner_access: [read, write, delete, admin]
      public: false
```

### Tenant Configuration

```yaml
object_storage:
  tenants:
    tenant-acme:
      allowed_roles:
        - objstore-readwrite
        - objstore-admin
        - tenant-acme-admin
```

## CLI Commands

### Manage ACLs

```bash
# Get object ACL
quicbaas object acl get tenant-acme:docs/report.pdf

# Set owner
quicbaas object acl set-owner tenant-acme:docs/report.pdf newowner@example.com

# Add grant
quicbaas object acl grant tenant-acme:docs/report.pdf \
  --principal finance-team \
  --type group \
  --permissions read,write

# Remove grant
quicbaas object acl revoke tenant-acme:docs/report.pdf \
  --principal finance-team

# Make public
quicbaas object acl set-public tenant-acme:docs/readme.txt true
```

### Check Permissions

```bash
# Check if user has access
quicbaas auth check \
  --user user@example.com \
  --resource objstore \
  --action write \
  --context "key=tenant-acme:docs/report.pdf"
```

## REST API

### Get Object ACL

```http
GET /api/v1/objects/{key}/acl
```

**Response:**
```json
{
  "key": "tenant-acme:docs/report.pdf",
  "acl": {
    "owner": "user@example.com",
    "owner_access": ["read", "write", "delete", "admin"],
    "public": false,
    "grants": [
      {
        "principal": "finance-team",
        "principal_type": "group",
        "permissions": ["read", "write"]
      }
    ]
  }
}
```

### Update Object ACL

```http
PUT /api/v1/objects/{key}/acl
```

**Request:**
```json
{
  "grants": [
    {
      "principal": "finance-team",
      "principal_type": "group",
      "permissions": ["read", "write"]
    },
    {
      "principal": "auditor@example.com",
      "principal_type": "user",
      "permissions": ["read"]
    }
  ],
  "public": false
}
```

### Add Grant

```http
POST /api/v1/objects/{key}/acl/grants
```

**Request:**
```json
{
  "principal": "new-user@example.com",
  "principal_type": "user",
  "permissions": ["read"]
}
```

### Remove Grant

```http
DELETE /api/v1/objects/{key}/acl/grants/{principal}
```

## gRPC Method Mapping

```go
func mapObjectStorageMethod(method string) (resource, action string) {
    switch method {
    // Read operations
    case "GetObject", "HeadObject":
        return "objstore", "read"
    case "ListObjects", "SearchObjects":
        return "catalog", "read"
    case "GetCatalogEntry", "GetLocations":
        return "catalog", "read"

    // Write operations
    case "PutObject", "UpdateMetadata":
        return "objstore", "write"
    case "CopyObject":
        return "objstore", "replicate"

    // Delete operations
    case "DeleteObject":
        return "objstore", "delete"

    // Admin operations
    case "CreateBucket", "DeleteBucket", "SetBucketPolicy":
        return "bucket", "admin"
    case "Replicate":
        return "objstore", "replicate"
    case "Migrate":
        return "objstore", "migrate"
    case "Archive":
        return "objstore", "archive"

    default:
        return "objstore", "admin"
    }
}
```

## Audit Logging

All access control decisions are logged:

```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "event": "authorization",
  "subject": {
    "user_id": "user@example.com",
    "tenant_id": "tenant-acme",
    "roles": ["objstore-readwrite"]
  },
  "resource": "objstore",
  "action": "write",
  "object_key": "tenant-acme:docs/report.pdf",
  "decision": "allowed",
  "reason": "RBAC role permission",
  "source_ip": "10.0.1.50"
}
```

## Metrics

```
# Authorization decisions
quicbaas_objstore_authz_decisions_total{decision="allowed"}
quicbaas_objstore_authz_decisions_total{decision="denied"}

# By check type
quicbaas_objstore_authz_rbac_checks_total
quicbaas_objstore_authz_abac_checks_total
quicbaas_objstore_authz_acl_checks_total

# Latency
quicbaas_objstore_authz_check_duration_seconds
```

## Troubleshooting

### Common Errors

| Error | Cause | Resolution |
|-------|-------|------------|
| `permission denied` | No matching permission | Check role assignments |
| `tenant mismatch` | Cross-tenant access | Verify tenant isolation policy |
| `acl check failed` | Object ACL blocks access | Update ACL or use admin |

### Debugging

```bash
# Check user permissions
quicbaas auth list-permissions --user user@example.com

# View effective policies
quicbaas auth list-policies --tenant tenant-acme

# Test authorization
quicbaas auth check \
  --user user@example.com \
  --resource objstore \
  --action write \
  --context "key=tenant-acme:docs/report.pdf" \
  --verbose
```
