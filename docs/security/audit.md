# Audit Logging

QuicBaaS provides comprehensive audit logging for security events, data access, and administrative operations with enterprise-grade features including SIEM integration, syslog forwarding, log compression, and tamper-proof signatures.

## Overview

The audit subsystem captures security-relevant events and forwards them to multiple destinations:

- Local file with rotation and compression
- Syslog (local or remote via UDP/TCP)
- SIEM systems via HTTP (CEF, LEEF, or JSON formats)

## Configuration

### Basic Configuration

```yaml
security:
  audit:
    enabled: true
    file: "/var/log/quicbaas/audit.log"
    max_size: 100           # MB before rotation
    max_backups: 10         # Number of backup files to retain
    buffer_size: 1024       # Event buffer size
    flush_interval: "5s"    # Auto-flush interval
```

### Log Signing (Tamper-Proof Logging)

Enable HMAC signatures to detect log tampering:

```yaml
security:
  audit:
    enabled: true
    sign_logs: true
    sign_key: "${AUDIT_SIGN_KEY}"  # HMAC-SHA256 key (environment variable)
```

Signed log entries include:

```json
{
  "event_id": "evt-1704634800000000000",
  "timestamp": "2025-01-07T14:00:00.000000000Z",
  "action": "read",
  "resource": "keys",
  "result": "success",
  "username": "alice",
  "ip_address": "10.0.1.50",
  "signature": "a1b2c3d4e5f6..."
}
```

### Log Compression

Enable gzip compression for rotated log files:

```yaml
security:
  audit:
    enabled: true
    compress: true
    compression_level: 6    # 1-9, or special values below
```

Compression levels:

| Level | Description | Use Case |
|-------|-------------|----------|
| -2 | Huffman only | Fastest, minimal compression |
| -1 | Default | Balanced speed/compression |
| 1 | Best speed | Fast compression |
| 6 | Default value | Good balance |
| 9 | Best compression | Maximum compression, slower |

## Syslog Integration

QuicBaaS supports both local and remote syslog destinations with RFC 5424 compliance.

### Local Syslog

```yaml
security:
  audit:
    enabled: true
    syslog_enabled: true
    syslog_network: ""         # Empty for local syslog
    syslog_facility: 16        # LOCAL0
    syslog_tag: "quicbaas"
```

### Remote Syslog (UDP)

```yaml
security:
  audit:
    enabled: true
    syslog_enabled: true
    syslog_network: "udp"
    syslog_address: "syslog.example.com:514"
    syslog_facility: 16        # LOCAL0
    syslog_tag: "quicbaas"
    syslog_hostname: "quicbaas-node-1"  # Optional, uses system hostname
```

### Remote Syslog (TCP with TLS)

```yaml
security:
  audit:
    enabled: true
    syslog_enabled: true
    syslog_network: "tcp"
    syslog_address: "syslog.example.com:6514"
    syslog_facility: 16
    syslog_tag: "quicbaas"
```

### Syslog Facilities

| Value | Facility | Description |
|-------|----------|-------------|
| 0 | kern | Kernel messages |
| 1 | user | User-level messages |
| 3 | daemon | System daemons |
| 4 | auth | Security/auth messages |
| 10 | authpriv | Security/auth (private) |
| 16 | local0 | Local use 0 (default) |
| 17-23 | local1-7 | Local use 1-7 |

### RFC 5424 Format

Remote syslog messages use RFC 5424 structured data format:

```
<134>1 2025-01-07T14:00:00.000Z quicbaas-node-1 quicbaas 12345 evt-123 [audit@quicbaas action="read" resource="keys" result="success" username="alice" ipAddress="10.0.1.50"] QuicBaaS Audit: action=read resource=keys result=success user=alice ip=10.0.1.50
```

### Severity Mapping

Audit events are mapped to syslog severity levels:

| Event Type | Severity | Level |
|------------|----------|-------|
| Successful operations | Info (6) | Normal activity |
| Key management, user changes | Notice (5) | Notable events |
| Authentication failures | Warning (4) | Potential issues |
| Access denied | Warning (4) | Security events |
| Operation failures | Error (3) | System errors |

## SIEM Integration

QuicBaaS supports direct SIEM integration via HTTP with three format options.

### Configuration

```yaml
security:
  audit:
    enabled: true
    siem:
      enabled: true
      endpoint: "https://siem.example.com/api/events"
      format: "cef"            # cef, leef, or json
      batch_size: 100          # Events per batch
      flush_interval: "10s"    # Flush interval
      timeout: "30s"           # HTTP timeout
      max_retries: 3           # Retry attempts
      retry_delay: "1s"        # Initial retry delay
      tls_skip_verify: false   # Verify TLS certificates
      headers:
        Authorization: "Bearer ${SIEM_TOKEN}"
        X-Custom-Header: "value"
```

### CEF Format (ArcSight)

Common Event Format for ArcSight and similar SIEM systems:

```yaml
security:
  audit:
    siem:
      enabled: true
      format: "cef"
      cef_vendor: "QuicBaaS"
      cef_product: "QuicBaaS"
      cef_version: "1.0"
```

Example CEF output:

```
CEF:0|QuicBaaS|QuicBaaS|1.0|1001|Authentication Attempt - success|1|rt=1704634800000 src=10.0.1.50 suser=alice act=authenticate cs1=auth cs1Label=resource outcome=success
```

CEF severity mapping (0-10 scale):

| Severity | Event Type |
|----------|------------|
| 1 | Normal successful operations |
| 4 | Administrative operations |
| 5 | Key exports, cross-tenant operations |
| 6 | Key operation failures |
| 7 | Authentication failures |
| 8 | Access denied |

### LEEF Format (QRadar)

Log Event Extended Format for IBM QRadar:

```yaml
security:
  audit:
    siem:
      enabled: true
      format: "leef"
      leef_vendor: "QuicBaaS"
      leef_product: "QuicBaaS"
      leef_version: "1.0"
```

Example LEEF output:

```
LEEF:2.0|QuicBaaS|QuicBaaS|1.0|1001|devTime=2025-01-07T14:00:00Z	src=10.0.1.50	usrName=alice	action=authenticate	resource=auth	result=success	sev=1
```

### JSON Format (Splunk, Elasticsearch)

Newline-delimited JSON for general SIEM systems:

```yaml
security:
  audit:
    siem:
      enabled: true
      format: "json"
```

Example JSON output:

```json
{"timestamp":"2025-01-07T14:00:00.000000000Z","event_id":"evt-123","event_type":"audit","source":"quicbaas","source_ip":"10.0.1.50","user":"alice","user_id":"user-123","action":"authenticate","resource":"auth","outcome":"success","severity":1}
```

### Signature IDs

SIEM events include signature IDs for correlation:

| ID Range | Category |
|----------|----------|
| 1001-1099 | Authentication/Authorization |
| 2001-2099 | Data operations (read, write, delete) |
| 3001-3099 | Backup/Restore |
| 4001-4099 | Cluster operations |
| 5001-5099 | User/Role management |
| 6001-6099 | Key management |
| 7001-7099 | Configuration changes |
| 8001-8099 | Tenant operations |
| 9999 | Unknown action |

## Audit Event Types

### Authentication Events

| Action | Description |
|--------|-------------|
| authenticate | Login attempt (success/failure) |
| authorize | Permission check |

### Data Operations

| Action | Description |
|--------|-------------|
| read | Key/data read operation |
| write | Key/data write operation |
| delete | Key/data deletion |

### Key Management

| Action | Description |
|--------|-------------|
| key.generate | New key created |
| key.rotate | Key rotation |
| key.export | Key exported (security-sensitive) |
| key.import | Key imported |
| key.delete | Key deleted |

### Cluster Operations

| Action | Description |
|--------|-------------|
| cluster.join | Node joined cluster |
| cluster.leave | Node left cluster |

### Administrative Operations

| Action | Description |
|--------|-------------|
| user.create | User created |
| user.delete | User deleted |
| role.create | Role created |
| role.update | Role updated |
| config.change | Configuration changed |
| schema.change | Schema changed |
| backup | Backup operation |
| restore | Restore operation |

### Tenant Operations

| Action | Description |
|--------|-------------|
| tenant.switch | SuperAdmin switched tenant context |
| cross_tenant.read | Cross-tenant read operation |
| cross_tenant.write | Cross-tenant write operation |
| cross_tenant.delete | Cross-tenant delete operation |
| tenant.dek.create | Tenant DEK created |
| tenant.dek.rotate | Tenant DEK rotated |
| tenant.dek.delete | Tenant DEK deleted |
| tenant.access.denied | Cross-tenant access denied |

## Result Types

| Result | Description |
|--------|-------------|
| success | Operation completed successfully |
| failure | Operation failed due to error |
| denied | Operation denied due to authorization |

## Programmatic Access

### Go API

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/security"

// Create audit logger
cfg := &security.AuditConfig{
    Enabled:          true,
    File:             "/var/log/quicbaas/audit.log",
    SignLogs:         true,
    SignKey:          "your-hmac-key",
    Compress:         true,
    CompressionLevel: security.CompressionLevelDefault,
    SyslogEnabled:    true,
    SyslogNetwork:    "udp",
    SyslogAddress:    "syslog.example.com:514",
    SyslogFacility:   security.SyslogFacilityLocal0,
    SyslogTag:        "quicbaas",
    SIEMConfig: &security.SIEMConfig{
        Enabled:   true,
        Endpoint:  "https://siem.example.com/api/events",
        Format:    security.SIEMFormatCEF,
        BatchSize: 100,
    },
}

logger, err := security.NewAuditLogger(cfg, log)
if err != nil {
    return err
}
defer logger.Close()

// Log authentication success
err = logger.LogAuthSuccess(ctx, "alice", "user-123", "10.0.1.50")

// Log authentication failure
err = logger.LogAuthFailure(ctx, "bob", "10.0.1.50", "invalid credentials")

// Log authorization denial
err = logger.LogAuthzDenied(ctx, "alice", "user-123", "admin/config", "write", "10.0.1.50")

// Log data access
err = logger.LogDataAccess(ctx, "alice", "user-123", security.ActionRead, "my-key", "10.0.1.50", true)

// Log key operations
err = logger.LogKeyExport(ctx, "alice", "user-123", "key-123", "AES-256-GCM",
    "wrapping-key", "RSA-OAEP", "10.0.1.50", true)

// Log custom event
err = logger.Log(ctx, &security.AuditEvent{
    Action:    "custom.operation",
    Resource:  "custom-resource",
    Result:    security.ResultSuccess,
    Username:  "alice",
    IPAddress: "10.0.1.50",
    Details: map[string]string{
        "custom_field": "custom_value",
    },
})
```

### Verify Log Integrity

```go
// Verify individual event signature
event := &security.AuditEvent{...}
if logger.VerifySignature(event) {
    fmt.Println("Event signature valid")
} else {
    fmt.Println("Event signature invalid - possible tampering!")
}
```

## Best Practices

### Log Retention

1. **Compliance Requirements**: Retain logs per regulatory requirements (PCI-DSS: 1 year, HIPAA: 6 years)
2. **Storage Planning**: Calculate storage based on event volume and compression ratio
3. **Archival**: Compress and archive old logs to cold storage

### Security

1. **Sign All Logs**: Enable log signing in production
2. **Secure Sign Key**: Store sign key in KMS or HSM
3. **Restrict Access**: Limit log file permissions to root/admin only
4. **Encrypt in Transit**: Use TLS for remote syslog and SIEM

### Monitoring

1. **Alert on Failures**: Monitor audit subsystem health
2. **High Volume Events**: Alert on unusual event patterns
3. **Access Denied**: Alert on repeated authorization denials

### Performance

1. **Batching**: Use SIEM batching to reduce HTTP overhead
2. **Async Delivery**: Syslog and SIEM exports are asynchronous
3. **Compression**: Enable compression for high-volume deployments

## Troubleshooting

### Syslog Connection Issues

Check syslog exporter status:

```go
exporter := logger.GetSyslogExporter()
if exporter != nil && !exporter.IsConnected() {
    // Attempt reconnection
    err := exporter.Reconnect()
}
```

### SIEM Delivery Failures

Check SIEM exporter statistics:

```go
exporter := logger.GetSIEMExporter()
sent, errors := exporter.GetStats()
fmt.Printf("Events sent: %d, Errors: %d\n", sent, errors)
```

### Missing Events

1. Check `flush_interval` - reduce for more immediate delivery
2. Verify `buffer_size` is adequate for event volume
3. Check for log rotation issues with `max_size` and `max_backups`

## See Also

- [Security README](README.md) - Security overview
- [Hardening Guide](hardening-guide.md) - Production hardening
- [Authentication](authentication.md) - Authentication configuration
- [RBAC](rbac.md) - Authorization and access control
