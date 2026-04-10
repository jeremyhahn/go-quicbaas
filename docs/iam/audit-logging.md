# IAM Audit Logging

## Overview

The QuicBaaS IAM module includes comprehensive audit logging capabilities to track all security-relevant events across authentication, authorization, and administrative operations.

## Features

- **Event Types**: Authentication, authorization, user/group/role/policy management, MFA, sessions, tenants, applications
- **Categories**: Authentication, authorization, administration, security
- **Outcomes**: Success, failure, denied
- **Storage**: Thread-safe in-memory with configurable retention
- **Async Logging**: Non-blocking logging for hot paths
- **Query Capabilities**: Multi-criteria filtering, time ranges, pagination
- **Tenant Isolation**: All events are tenant-scoped

## Quick Start

### Enable Audit Logging

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/iam"

// Create audit logger
logger, err := iam.NewAuditLogger(
    iam.WithAsyncChannelSize(1000),
    iam.WithAuditStorage(customStorage), // Optional
)
if err != nil {
    return err
}
defer logger.Close()
```

### Log Events

#### Synchronous Logging

```go
event := &iam.AuditEvent{
    TenantID:  "tenant-123",
    EventType: iam.AuditEventAuthentication,
    Category:  iam.AuditCategoryAuthentication,
    Action:    "login",
    Outcome:   iam.AuditOutcomeSuccess,
    Actor: &iam.AuditActor{
        UserID: "user-456",
        Email:  "user@example.com",
    },
    IPAddress: "192.168.1.100",
    UserAgent: "Mozilla/5.0...",
}

err := logger.Log(ctx, event)
```

#### Asynchronous Logging (Hot Paths)

```go
// Non-blocking - suitable for high-throughput operations
logger.LogAsync(event)
```

#### Helper Methods

```go
// Authentication events
err := logger.LogAuthentication(ctx, tenantID, actor, "login", iam.AuditOutcomeSuccess, map[string]interface{}{
    "method": "password",
    "mfa_verified": true,
})

// Authorization checks
err := logger.LogAuthorization(ctx, tenantID, actor, resource, "read", iam.AuditOutcomeDenied)

// Administrative actions
err := logger.LogAdminAction(ctx, tenantID, actor, resource, "create_user", map[string]interface{}{
    "user_email": "newuser@example.com",
})
```

### Query Events

```go
filter := &iam.AuditFilter{
    TenantID:   "tenant-123",
    EventTypes: []iam.AuditEventType{iam.AuditEventAuthentication},
    StartTime:  &startTime,
    EndTime:    &endTime,
    Limit:      100,
    Offset:     0,
}

events, total, err := logger.Query(ctx, filter)
if err != nil {
    return err
}

for _, event := range events {
    fmt.Printf("%s: %s by %s - %s\n",
        event.Timestamp, event.Action, event.Actor.Email, event.Outcome)
}
```

### Retention Management

```go
// Delete events older than 90 days
cutoff := time.Now().AddDate(0, 0, -90)
count, err := logger.DeleteOlderThan(ctx, tenantID, cutoff)
fmt.Printf("Deleted %d old audit events\n", count)
```

## REST API

### Query Audit Events

```http
GET /api/v1/iam/audit?tenant_id=tenant-123&event_type=authentication&limit=50
```

Query Parameters:
- `tenant_id` (required): Tenant identifier
- `event_type`: Filter by event type
- `category`: Filter by category
- `outcome`: Filter by outcome (success, failure, denied)
- `actor_id`: Filter by actor user ID
- `resource_id`: Filter by resource ID
- `start_time`: ISO8601 timestamp (inclusive)
- `end_time`: ISO8601 timestamp (inclusive)
- `limit`: Results per page (default: 100, max: 1000)
- `offset`: Pagination offset

Response:
```json
{
  "events": [
    {
      "id": "event-uuid",
      "tenant_id": "tenant-123",
      "timestamp": "2025-12-23T07:30:00Z",
      "event_type": "authentication",
      "category": "authentication",
      "action": "login",
      "outcome": "success",
      "actor": {
        "user_id": "user-456",
        "email": "user@example.com"
      },
      "ip_address": "192.168.1.100",
      "user_agent": "Mozilla/5.0..."
    }
  ],
  "total": 1234,
  "limit": 50,
  "offset": 0
}
```

### Get Specific Event

```http
GET /api/v1/iam/audit/{event_id}?tenant_id=tenant-123
```

### Delete Old Events

```http
DELETE /api/v1/iam/audit?tenant_id=tenant-123&before=2025-09-23T00:00:00Z
Authorization: Bearer <admin_token>
```

**Required Permission**: `audit:delete`

**Access Control**: This operation is restricted to administrators with the `audit:delete` permission. Only `super_admin` role holders have this permission by default. Tenant admins and audit admins do NOT have delete permissions to ensure audit trail integrity.

| Admin Role | Can Delete Audit Events |
|------------|------------------------|
| `super_admin` | Yes |
| `tenant_admin` | No |
| `audit_admin` | No |
| `user_admin` | No |
| Other roles | No |

Response:
```json
{
  "deleted_count": 5432,
  "before": "2025-09-23T00:00:00Z"
}
```

Error Response (403 Forbidden):
```json
{
  "error": "forbidden",
  "error_description": "The audit:delete permission is required to delete audit events"
}
```

## Permission Model

The audit system uses the following permissions for access control:

| Permission | Description | Default Roles |
|------------|-------------|---------------|
| `audit:read` | Query and view audit events | `super_admin`, `tenant_admin`, `audit_admin`, `read_only` |
| `audit:export` | Export audit events to external systems | `super_admin`, `audit_admin` |
| `audit:delete` | Delete audit events (retention cleanup) | `super_admin` only |

### Why Delete is Restricted

The `audit:delete` permission is intentionally restricted to `super_admin` only:

1. **Audit Trail Integrity**: Audit logs provide a tamper-evident record of all security events
2. **Compliance Requirements**: Regulations like SOC 2, HIPAA, and GDPR require audit log retention
3. **Forensic Value**: Audit logs are critical for security incident investigation
4. **Separation of Duties**: Tenant admins should not be able to cover their tracks

To grant `audit:delete` to additional users, explicitly assign the permission:

```go
assignment := &iam.AdminAssignment{
    UserID:      "user-123",
    AdminRole:   iam.AdminRoleAuditAdmin,
    TenantIDs:   []string{"tenant-456"},
    Permissions: []iam.AdminPermission{
        iam.PermAuditRead,
        iam.PermAuditExport,
        iam.PermAuditDelete, // Explicitly grant delete
    },
    GrantedBy: "super-admin-id",
}
```

## Tenant Configuration

Configure audit logging in tenant settings:

```yaml
audit_policy:
  enabled: true
  retention_days: 90
  log_auth_events: true
  log_admin_events: true
  log_policy_checks: true
```

Or via API:

```go
tenant.Settings.AuditPolicy = &iam.AuditPolicy{
    Enabled:         true,
    RetentionDays:   90,
    LogAuthEvents:   true,
    LogAdminEvents:  true,
    LogPolicyChecks: true,
}
```

## Event Types

### Authentication Events
- Login attempts (success/failure)
- Logout
- MFA enrollment
- MFA verification
- Password changes
- Session creation/termination
- WebAuthn credential registration
- WebAuthn authentication

### Authorization Events
- Policy evaluation results
- Permission checks
- Access denials

### Administrative Events
- User creation/modification/deletion
- Group membership changes
- Role assignments/revocations
- Policy creation/updates/deletion
- Tenant configuration changes
- Application registration
- Audit event deletion (logged before execution)

### MFA Events
- Enrollment started/completed
- Challenge creation/verification
- Backup code usage
- MFA disabled

### Session Events
- Session created
- Session extended
- Session expired
- Session revoked

## Storage Implementation

### In-Memory Storage

The default in-memory storage is suitable for development and small deployments:

```go
storage := iam.NewMemoryAuditStorage(
    iam.WithMaxEventsPerTenant(10000), // FIFO eviction after limit
)

logger, err := iam.NewAuditLogger(
    iam.WithAuditStorage(storage),
)
```

Features:
- Thread-safe (RWMutex)
- Tenant-indexed for fast queries
- Time-ordered events
- FIFO eviction when limits reached
- Memory-bounded growth

### Custom Storage Backend

Implement the `AuditStorage` interface for persistent storage:

```go
type AuditStorage interface {
    SaveEvent(ctx context.Context, event *AuditEvent) error
    GetEvent(ctx context.Context, tenantID, eventID string) (*AuditEvent, error)
    QueryEvents(ctx context.Context, filter *AuditFilter) ([]*AuditEvent, int, error)
    DeleteEventsBefore(ctx context.Context, tenantID string, before time.Time) (int, error)
    Close() error
}
```

## Performance Considerations

### Async Logging

Use `LogAsync()` for high-throughput operations to avoid blocking:

```go
// Hot path - authentication middleware
logger.LogAsync(&iam.AuditEvent{
    TenantID:  tenantID,
    EventType: iam.AuditEventAuthentication,
    Action:    "api_call",
    Outcome:   iam.AuditOutcomeSuccess,
})
```

Channel buffer size can be configured:

```go
logger, err := iam.NewAuditLogger(
    iam.WithAsyncChannelSize(5000), // Larger buffer for high load
)
```

### Query Optimization

- Use specific filters to reduce result sets
- Limit time ranges for better performance
- Use pagination for large result sets
- Index on tenant, event type, and timestamp

### Memory Management

Configure max events per tenant to prevent unbounded growth:

```go
storage := iam.NewMemoryAuditStorage(
    iam.WithMaxEventsPerTenant(50000),
)
```

When limit is reached, oldest events are automatically evicted (FIFO).

## Security Considerations

### Access Control

- Query operations require `audit:read` permission and are tenant-scoped
- Export operations require `audit:export` permission
- Deletion operations require `audit:delete` permission (super_admin only by default)
- Audit events are immutable after creation
- Sensitive data should not be logged in details

### Compliance

- Retention periods should comply with regulatory requirements
- Authentication events help with SOC 2, ISO 27001 compliance
- Authorization events support principle of least privilege auditing
- Administrative actions provide audit trail for governance
- Delete operations are themselves audited before execution

### Data Protection

- Consider encrypting audit events at rest
- Implement secure deletion (overwrite, not just unlink)
- Export to secure, append-only storage for long-term retention
- Protect against tampering (cryptographic signing)

## Best Practices

1. **Log Consistently**: Use helper methods for consistent event structure
2. **Include Context**: Add relevant details to the Details field
3. **Use Async for Hot Paths**: Avoid blocking critical paths
4. **Set Retention Policies**: Configure appropriate retention periods
5. **Monitor Storage**: Alert on storage growth or dropped events
6. **Regular Cleanup**: Run retention cleanup jobs regularly
7. **Secure Access**: Restrict audit log access to administrators
8. **Export for Analysis**: Integrate with SIEM for security monitoring
9. **Protect Delete Access**: Only grant `audit:delete` to trusted super admins
10. **Audit the Auditors**: Monitor who accesses and deletes audit logs

## Examples

### Complete Authentication Flow

```go
// Login attempt
actor := &iam.AuditActor{
    Email: email,
}

// Failed login
logger.LogAuthentication(ctx, tenantID, actor, "login", iam.AuditOutcomeFailure, map[string]interface{}{
    "reason": "invalid_password",
})

// Successful login
actor.UserID = user.ID
logger.LogAuthentication(ctx, tenantID, actor, "login", iam.AuditOutcomeSuccess, map[string]interface{}{
    "method": "password",
})

// MFA challenge
logger.LogAsync(&iam.AuditEvent{
    TenantID:  tenantID,
    EventType: iam.AuditEventMFAEvent,
    Category:  iam.AuditCategorySecurity,
    Action:    "mfa_challenge",
    Outcome:   iam.AuditOutcomeSuccess,
    Actor:     actor,
})
```

### Policy Enforcement Logging

```go
// Denied by policy
logger.LogAuthorization(ctx, tenantID,
    &iam.AuditActor{UserID: user.ID, Email: user.Email},
    &iam.AuditResource{Type: "document", ID: docID, Name: filename},
    "read",
    iam.AuditOutcomeDenied,
)
```

### Administrative Changes

```go
// User creation
logger.LogAdminAction(ctx, tenantID,
    &iam.AuditActor{UserID: adminID, Email: adminEmail},
    &iam.AuditResource{Type: "user", ID: newUser.ID, Name: newUser.Email},
    "create_user",
    map[string]interface{}{
        "roles": newUser.Roles,
        "groups": newUser.Groups,
    },
)
```

### Audit Deletion with Logging

```go
// Before deleting, log the action itself
logger.LogAdminAction(ctx, tenantID,
    &iam.AuditActor{UserID: adminID, Email: adminEmail},
    &iam.AuditResource{Type: "audit", ID: tenantID},
    "delete_audit_events",
    map[string]interface{}{
        "before":     cutoffTime,
        "permission": "audit:delete",
    },
)

// Then perform the deletion
count, err := logger.DeleteOlderThan(ctx, tenantID, cutoffTime)
```

## Monitoring

### Metrics to Track

- Events logged per second
- Async queue depth
- Storage utilization
- Query latency
- Retention cleanup duration
- Dropped events (when async queue full)

### Alerts

- High async queue depth (> 80% capacity)
- Storage approaching limits
- Retention cleanup failures
- Unusual event patterns (e.g., spike in failed logins)
- Unauthorized audit delete attempts

## Troubleshooting

### Async Events Not Appearing

- Check async channel buffer size
- Verify worker goroutine is running
- Check for panics in storage layer
- Ensure Close() is called on shutdown for event draining

### High Memory Usage

- Reduce max events per tenant
- Decrease retention period
- Implement storage backend (vs in-memory)
- Run cleanup more frequently

### Slow Queries

- Add more specific filters
- Reduce time ranges
- Use pagination
- Consider storage backend with better indexing

### Audit Delete Permission Denied

- Verify the user has `super_admin` role
- Check if `audit:delete` is explicitly granted in the assignment
- Verify the assignment is active and not expired
- Check tenant scope matches the target tenant

## See Also

- [Admin & Impersonation](admin-impersonation.md) - Delegated administration roles
- [API Reference](api-reference.md) - Complete API documentation
- [GDPR Compliance](gdpr.md) - Data export and deletion
