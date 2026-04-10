# GDPR Compliance

QuicBaaS IAM provides comprehensive GDPR compliance features for managing user consent, data export (Right to Access), and data deletion (Right to Erasure).

## Overview

The GDPR module (`pkg/iam/gdpr.go`) implements:

- **Consent Management**: Track and manage user consent for various data processing purposes
- **Data Export**: Allow users to request and download their personal data
- **Data Deletion**: Process deletion requests with configurable grace periods

## Consent Management

### Consent Purposes

QuicBaaS supports the following consent purposes:

| Purpose | Description |
|---------|-------------|
| `marketing` | Marketing communications |
| `analytics` | Analytics and usage tracking |
| `personalization` | Personalized experiences |
| `third_party` | Sharing with third parties |
| `data_processing` | General data processing |
| `data_retention` | Extended data retention |
| `functional` | Functional features and cookies |

### Consent Status

| Status | Description |
|--------|-------------|
| `granted` | Consent was given |
| `denied` | Consent was explicitly denied |
| `withdrawn` | Consent was previously granted but withdrawn |
| `pending` | Consent has not been given or denied |

### API Operations

#### Grant Consent

```bash
# REST API
curl -X POST https://quicbaas:8080/api/v1/tenants/{tenant_id}/gdpr/consent \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "user_id": "user_123",
    "purpose": "marketing",
    "version": "1.0",
    "expires_at": "2026-01-01T00:00:00Z"
  }'

# CLI
quicbaas iam gdpr consent grant --tenant acme-corp \
  --user-id user_123 --purpose marketing --version 1.0
```

#### Withdraw Consent

```bash
# REST API
curl -X DELETE https://quicbaas:8080/api/v1/tenants/{tenant_id}/gdpr/consent \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"user_id": "user_123", "purpose": "marketing"}'

# CLI
quicbaas iam gdpr consent withdraw --tenant acme-corp \
  --user-id user_123 --purpose marketing
```

#### List User Consents

```bash
# REST API
curl https://quicbaas:8080/api/v1/tenants/{tenant_id}/users/{user_id}/consents \
  -H "Authorization: Bearer $TOKEN"

# CLI
quicbaas iam gdpr consent list --tenant acme-corp --user-id user_123
```

## Data Export (Right to Access)

Users can request an export of all their personal data stored in QuicBaaS.

### Export Formats

| Format | Description |
|--------|-------------|
| `json` | JSON format (default) |
| `csv` | CSV format |
| `xml` | XML format |

### Data Categories

Exports can include or exclude specific data categories:

| Category | Description |
|----------|-------------|
| `profile` | User profile data (email, name, attributes) |
| `sessions` | Session history |
| `consents` | Consent records |
| `groups` | Group memberships |
| `roles` | Role assignments |
| `audit_logs` | User's audit log entries |
| `credentials` | Credential metadata (not secrets) |
| `mfa` | MFA enrollment info |

### Export Flow

1. **Request Export**: User initiates export request
2. **Processing**: System collects user data
3. **Completion**: Download link generated (valid for 7 days by default)
4. **Download**: User downloads their data

### API Operations

#### Request Export

```bash
# REST API
curl -X POST https://quicbaas:8080/api/v1/tenants/{tenant_id}/gdpr/export \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "user_id": "user_123",
    "format": "json",
    "include_categories": ["profile", "sessions", "consents"]
  }'

# CLI
quicbaas iam gdpr export request --tenant acme-corp \
  --user-id user_123 --format json
```

#### Check Export Status

```bash
# REST API
curl https://quicbaas:8080/api/v1/tenants/{tenant_id}/gdpr/export/{export_id} \
  -H "Authorization: Bearer $TOKEN"

# CLI
quicbaas iam gdpr export status --tenant acme-corp --export-id exp_abc123
```

#### Generate Export

```bash
# REST API
curl -X POST https://quicbaas:8080/api/v1/tenants/{tenant_id}/gdpr/export/{export_id}/generate \
  -H "Authorization: Bearer $TOKEN"

# CLI
quicbaas iam gdpr export generate --tenant acme-corp --export-id exp_abc123
```

## Data Deletion (Right to Erasure)

Users can request deletion of their personal data with a configurable grace period.

### Deletion Flow

1. **Request Deletion**: User initiates deletion request
2. **Grace Period**: Configurable waiting period (default: 30 days)
3. **Cancellation Window**: User can cancel during grace period
4. **Processing**: After grace period, deletion is executed
5. **Completion**: Summary of deleted data provided

### Deletion Categories

Users can selectively delete or preserve specific data categories:

- **Delete Categories**: Specify which categories to delete (empty = all)
- **Preserve Categories**: Specify which categories to keep (for legal holds)

### API Operations

#### Request Deletion

```bash
# REST API
curl -X POST https://quicbaas:8080/api/v1/tenants/{tenant_id}/gdpr/deletion \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "user_id": "user_123",
    "reason": "User requested account deletion",
    "grace_period_days": 30,
    "preserve_categories": ["audit_logs"]
  }'

# CLI
quicbaas iam gdpr deletion request --tenant acme-corp \
  --user-id user_123 --reason "Account closure" --grace-period 30
```

#### Cancel Deletion

```bash
# REST API
curl -X DELETE https://quicbaas:8080/api/v1/tenants/{tenant_id}/gdpr/deletion/{deletion_id} \
  -H "Authorization: Bearer $TOKEN"

# CLI
quicbaas iam gdpr deletion cancel --tenant acme-corp --deletion-id del_abc123
```

#### Check Deletion Status

```bash
# REST API
curl https://quicbaas:8080/api/v1/tenants/{tenant_id}/gdpr/deletion/{deletion_id} \
  -H "Authorization: Bearer $TOKEN"

# CLI
quicbaas iam gdpr deletion status --tenant acme-corp --deletion-id del_abc123
```

#### Process Deletion (Admin)

After grace period expires:

```bash
# REST API
curl -X POST https://quicbaas:8080/api/v1/tenants/{tenant_id}/gdpr/deletion/{deletion_id}/process \
  -H "Authorization: Bearer $TOKEN"

# CLI
quicbaas iam gdpr deletion process --tenant acme-corp --deletion-id del_abc123
```

### Deletion Summary

After processing, a summary is provided:

```json
{
  "profile_deleted": true,
  "sessions_deleted": 5,
  "audit_logs_anonymized": 150,
  "consents_deleted": 3,
  "credentials_deleted": 2,
  "group_memberships_removed": 4,
  "role_assignments_removed": 2,
  "tokens_revoked": 3
}
```

## Configuration

### Default Settings

| Setting | Default | Description |
|---------|---------|-------------|
| Grace Period | 30 days | Time before deletion is executed |
| Export Download TTL | 7 days | How long export download links are valid |
| Export Schema Version | 1.0 | Current export data schema version |

## Audit Integration

All GDPR operations are logged to the audit system:

| Event | Description |
|-------|-------------|
| `consent.granted` | Consent was granted |
| `consent.updated` | Consent was updated |
| `consent.withdrawn` | Consent was withdrawn |
| `data_export.requested` | Export request created |
| `data_export.completed` | Export generation completed |
| `data_deletion.requested` | Deletion request created |
| `data_deletion.cancelled` | Deletion request cancelled |
| `data_deletion.completed` | Deletion executed |

## Protocol Support

GDPR operations are available via all protocols:

| Protocol | Implementation |
|----------|---------------|
| REST API | `pkg/api/rest/iam_gdpr.go` |
| gRPC | `pkg/api/grpc/server_iam.go` |
| QUIC | `pkg/api/quic/handler_iam_gdpr.go` |
| MCP | `pkg/api/mcp/tools_iam_gdpr.go` |
| CLI | `cmd/quicbaas/iam_gdpr.go` |
