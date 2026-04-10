# Delegated Administration & Impersonation

QuicBaaS IAM provides comprehensive delegated administration capabilities, allowing organizations to assign granular administrative privileges and enable secure user impersonation for support scenarios.

## Overview

The Admin module (`pkg/iam/admin.go`) implements:

- **Admin Assignments**: Grant administrative privileges to users with fine-grained permissions
- **Admin Roles**: Predefined roles with appropriate permission sets
- **Resource Scoping**: Limit admin access to specific tenants or resources
- **Impersonation**: Allow admins to act as users for support purposes
- **Audit Trail**: Full audit logging of all admin actions

## Admin Roles

QuicBaaS provides the following predefined admin roles:

| Role | Description |
|------|-------------|
| `super_admin` | Full access to all tenants and system operations |
| `tenant_admin` | Full access within assigned tenant(s) |
| `user_admin` | Can manage users within assigned tenant(s) |
| `group_admin` | Can manage groups within assigned tenant(s) |
| `policy_admin` | Can manage policies within assigned tenant(s) |
| `audit_admin` | Can view audit logs within assigned tenant(s) |
| `read_only` | Read-only access to assigned resources |

## Admin Permissions

### Permission Categories

#### Tenant Permissions
| Permission | Description |
|------------|-------------|
| `tenant:create` | Create new tenants |
| `tenant:read` | View tenant details |
| `tenant:update` | Modify tenant settings |
| `tenant:delete` | Delete tenants |

#### User Permissions
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

#### Group Permissions
| Permission | Description |
|------------|-------------|
| `group:create` | Create groups |
| `group:read` | View groups |
| `group:update` | Modify groups |
| `group:delete` | Delete groups |

#### Role Permissions
| Permission | Description |
|------------|-------------|
| `role:create` | Create roles |
| `role:read` | View roles |
| `role:update` | Modify roles |
| `role:delete` | Delete roles |
| `role:assign` | Assign roles to users |

#### Policy Permissions
| Permission | Description |
|------------|-------------|
| `policy:create` | Create policies |
| `policy:read` | View policies |
| `policy:update` | Modify policies |
| `policy:delete` | Delete policies |
| `policy:simulate` | Run policy simulations |

#### Audit & Session Permissions
| Permission | Description |
|------------|-------------|
| `audit:read` | View audit logs |
| `audit:export` | Export audit logs |
| `session:read` | View sessions |
| `session:revoke` | Revoke sessions |

#### GDPR Permissions
| Permission | Description |
|------------|-------------|
| `gdpr:export` | Process data export requests |
| `gdpr:delete` | Process data deletion requests |
| `consent:manage` | Manage user consents |

## Admin Assignments

### Creating an Assignment

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

### Assignment Options

| Field | Description |
|-------|-------------|
| `user_id` | User receiving admin privileges |
| `admin_role` | Predefined admin role |
| `tenant_ids` | Tenants this assignment applies to (empty = global) |
| `permissions` | Additional permissions beyond role defaults |
| `denied_permissions` | Explicitly denied permissions |
| `resource_scopes` | Limit access to specific resources |
| `expires_at` | When the assignment expires |
| `granted_by` | Admin who created the assignment |

### Resource Scoping

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

### Managing Assignments

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

### Check Permission

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

## Impersonation

Impersonation allows administrators to act as another user for support purposes. All actions during impersonation are fully audited.

### Starting Impersonation

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

### Requirements

- Admin must have `user:impersonate` permission
- Admin must have access to the target user's tenant
- A reason is required for audit purposes
- Duration defaults to 1 hour if not specified

### Ending Impersonation

```bash
# REST API
curl -X POST https://quicbaas:8080/api/v1/admin/impersonation/{session_id}/end \
  -H "Authorization: Bearer $TOKEN"

# CLI
quicbaas iam admin impersonate end --session-id imp_abc123
```

### Active Impersonations

```bash
# List active impersonation sessions
quicbaas iam admin impersonate list --admin-user admin_123
```

### Impersonation Session

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

## Audit Events

All admin operations generate audit events:

| Event | Description |
|-------|-------------|
| `admin.assignment.create` | Admin assignment created |
| `admin.assignment.update` | Admin assignment modified |
| `admin.assignment.delete` | Admin assignment removed |
| `admin.impersonation.start` | Impersonation session started |
| `admin.impersonation.end` | Impersonation session ended |

### Audit Details

Impersonation events include:
- Admin user ID
- Target user ID
- Tenant context
- Reason for impersonation
- Session duration

## Role Permission Matrix

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

## Protocol Support

Admin operations are available via all protocols:

| Protocol | Implementation |
|----------|---------------|
| REST API | `pkg/api/rest/iam_admin.go` |
| gRPC | `pkg/api/grpc/server_iam.go` |
| QUIC | `pkg/api/quic/handler_iam_admin.go` |
| MCP | `pkg/api/mcp/tools_iam_admin.go` |
| CLI | `cmd/quicbaas/iam_admin.go` |

## Security Considerations

1. **Least Privilege**: Assign the minimum role required for the task
2. **Time-Limited**: Use expiration dates for temporary access
3. **Resource Scoping**: Limit access to specific resources when possible
4. **Audit Review**: Regularly review admin actions in audit logs
5. **Impersonation Justification**: Always require and review impersonation reasons
6. **Session Limits**: Keep impersonation sessions short (1 hour default)
