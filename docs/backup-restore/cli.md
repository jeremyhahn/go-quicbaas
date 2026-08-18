# Backup & Restore — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag, with runnable examples. Feature folder: `docs/backup-restore/`.

Comprehensive documentation for QuicBaaS backup and restore operations via the command-line interface.

## Overview

QuicBaaS provides a complete backup and restore system with support for:
- Full and incremental backups
- Compression (gzip, zstd)
- Encryption (AES-256-GCM)
- Multiple storage backends (local, S3, Azure, GCS, MinIO)
- Backup verification and integrity checks
- Point-in-time recovery
- Cron-based scheduled backups with retention policies
- Credentials file with profile management

## Quick Reference

```bash
# Create a backup (local storage)
quicbaas backup create my-backup

# Create a backup to S3
quicbaas backup create my-backup --storage-type s3 --bucket my-bucket --region us-west-2

# List all backups
quicbaas backup list

# Restore from backup
quicbaas backup restore backup-my-backup-1729123456

# Delete a backup
quicbaas backup delete backup-my-backup-1729123456 --yes

# Verify backup integrity
quicbaas backup verify backup-my-backup-1729123456
```

## Storage Backend Flags

All backup commands support these persistent flags for storage configuration:

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--storage-type` | string | local | Storage backend (local, s3, azure, gcs, minio) |
| `--bucket` | string | - | S3/GCS/MinIO bucket name |
| `--region` | string | - | AWS region (default: us-east-1) |
| `--endpoint` | string | - | Custom endpoint for S3-compatible/MinIO storage |
| `--container` | string | - | Azure blob container name |
| `--credentials-file` | string | - | Path to credentials file |
| `--profile` | string | - | Named profile from credentials file |
| `--output` | string | table | Output format (json, yaml, table, raw) |

## Credential Resolution Priority

Credentials are resolved in the following order (highest to lowest priority):
1. CLI flags
2. Environment variables (AWS_*, AZURE_*, GOOGLE_*)
3. Credentials file (~/.quicbaas/backup-credentials.yaml)
4. Cloud provider defaults (AWS chain, Azure DefaultCredential, GCP ADC)

See [Backup Credentials](user.md) for detailed credential configuration.

## Commands

### backup create

Create a new backup of the QuicBaaS database.

**Syntax:**
```bash
quicbaas backup create <name> [options]
```

**Options:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--compression` | string | zstd | Compression algorithm (none, gzip, zstd) |
| `--encryption` | string | none | Encryption algorithm (none, aes256) |
| `--incremental` | bool | false | Create incremental backup |
| `--storage-path` | string | /var/lib/quicbaas/backups | Storage path for backups |
| `--encrypt-key` | string | - | Encryption key (32 bytes for AES-256) |

**Examples:**

```bash
# Basic full backup (local storage)
quicbaas backup create my-backup

# Backup with gzip compression
quicbaas backup create my-backup --compression gzip

# Encrypted backup
quicbaas backup create secure-backup \
  --encryption aes256 \
  --encrypt-key "your-32-byte-key-here-padded!!"

# Incremental backup
quicbaas backup create incremental-001 --incremental

# Backup to S3
quicbaas backup create my-backup \
  --storage-type s3 \
  --bucket quicbaas-backups \
  --region us-west-2

# Backup to Azure Blob Storage
quicbaas backup create my-backup \
  --storage-type azure \
  --container quicbaas-backups

# Backup to GCS
quicbaas backup create my-backup \
  --storage-type gcs \
  --bucket quicbaas-backups

# Backup to MinIO
quicbaas backup create my-backup \
  --storage-type minio \
  --bucket quicbaas-backups \
  --endpoint localhost:9000

# Backup using a credentials profile
quicbaas backup create my-backup \
  --profile production-s3

# Custom storage path (local)
quicbaas backup create offsite-backup \
  --storage-path /mnt/backup-drive/quicbaas

# JSON output
quicbaas backup create my-backup --output json
```

### backup list

List all available backups with their metadata.

**Syntax:**
```bash
quicbaas backup list [options]
```

**Options:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--limit` | int | 100 | Maximum number of backups to list |
| `--storage-path` | string | /var/lib/quicbaas/backups | Storage path for backups |
| `--output` | string | table | Output format (json, yaml, table) |

**Examples:**

```bash
# List all backups (local storage)
quicbaas backup list

# List with limit
quicbaas backup list --limit 10

# List from custom storage path
quicbaas backup list --storage-path /mnt/backup-drive/quicbaas

# List backups from S3
quicbaas backup list --storage-type s3 --bucket quicbaas-backups --region us-west-2

# List backups from Azure
quicbaas backup list --storage-type azure --container quicbaas-backups

# List backups using a profile
quicbaas backup list --profile production-s3

# JSON output
quicbaas backup list --output json
```

### backup restore

Restore data from a backup.

**Syntax:**
```bash
quicbaas backup restore <backup-id> [options]
```

**Options:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--point-in-time` | string | - | Restore to specific timestamp (RFC3339 format) |
| `--verify` | bool | true | Verify backup integrity before restoring |
| `--skip-existing` | bool | false | Skip keys that already exist |
| `--storage-path` | string | /var/lib/quicbaas/backups | Storage path for backups |
| `--encrypt-key` | string | - | Encryption key if backup is encrypted |
| `--output` | string | table | Output format (json, yaml, table) |

**Examples:**

```bash
# Basic restore (local storage)
quicbaas backup restore backup-prod-1729123456

# Restore with verification
quicbaas backup restore backup-prod-1729123456 --verify

# Restore without overwriting existing keys
quicbaas backup restore backup-prod-1729123456 --skip-existing

# Point-in-time restore
quicbaas backup restore backup-prod-1729123456 \
  --point-in-time 2025-10-14T12:00:00Z

# Restore encrypted backup
quicbaas backup restore backup-secure-001 \
  --encrypt-key "your-32-byte-key-here-padded!!"

# Restore from S3
quicbaas backup restore backup-prod-1729123456 \
  --storage-type s3 \
  --bucket quicbaas-backups \
  --region us-west-2

# Restore using a profile
quicbaas backup restore backup-prod-1729123456 --profile production-s3
```

### backup delete

Delete a backup from storage.

**Syntax:**
```bash
quicbaas backup delete <backup-id> [options]
```

**Options:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-y, --yes` | bool | false | Skip confirmation prompt |
| `--storage-path` | string | /var/lib/quicbaas/backups | Storage path for backups |
| `--output` | string | table | Output format (json, yaml, table) |

**Examples:**

```bash
# Delete with confirmation
quicbaas backup delete backup-prod-1729123456

# Delete without confirmation
quicbaas backup delete backup-prod-1729123456 --yes

# Delete from custom storage path
quicbaas backup delete backup-prod-1729123456 \
  --storage-path /mnt/backup-drive/quicbaas \
  --yes
```

### backup verify

Verify the integrity of a backup.

**Syntax:**
```bash
quicbaas backup verify <backup-id> [options]
```

**Options:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--storage-path` | string | /var/lib/quicbaas/backups | Storage path for backups |
| `--encrypt-key` | string | - | Encryption key if backup is encrypted |
| `--output` | string | table | Output format (json, yaml, table) |

**Examples:**

```bash
# Basic verification
quicbaas backup verify backup-prod-1729123456

# Verify from custom storage path
quicbaas backup verify backup-prod-1729123456 \
  --storage-path /mnt/backup-drive/quicbaas

# Verify encrypted backup
quicbaas backup verify backup-secure-001 \
  --encrypt-key "your-32-byte-key-here-padded!!"

# JSON output
quicbaas backup verify backup-prod-1729123456 --output json
```

## Scheduled Backups

QuicBaaS supports automated cron-based backup scheduling configured via the server configuration file. Scheduled backups run automatically based on a cron expression and can include retention policies for automatic cleanup.

### Configuration

Scheduled backups are configured in the QuicBaaS server configuration file:

```yaml
backup:
  storage_backend: s3
  storage_config:
    bucket: prod-quicbaas-backups
    region: us-east-1
  default_compression: zstd
  default_encryption: aes-256-gcm
  encryption_key: ${BACKUP_ENCRYPTION_KEY}
  timeout: 30m
  schedule:
    enabled: true
    cron_expression: "0 */4 * * *"  # Every 4 hours
    backup_type: full
    retention_policy:
      max_backups: 90
      max_age: 2160h  # 90 days
      keep_daily: 14
      keep_weekly: 8
      keep_monthly: 12
```

### Schedule Configuration Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | bool | Yes | Enable or disable scheduled backups |
| `cron_expression` | string | Yes | Cron expression for backup schedule |
| `backup_type` | string | No | Backup type: "full" or "incremental" (default: "full") |
| `retention_policy` | object | No | Automatic cleanup configuration |

### Retention Policy Fields

| Field | Type | Description |
|-------|------|-------------|
| `max_backups` | int | Maximum number of backups to keep (0 = unlimited) |
| `max_age` | duration | Maximum age of backups (e.g., "720h" for 30 days) |
| `keep_daily` | int | Number of daily backups to preserve |
| `keep_weekly` | int | Number of weekly backups to preserve |
| `keep_monthly` | int | Number of monthly backups to preserve |

### Cron Expression Examples

| Expression | Description |
|------------|-------------|
| `0 2 * * *` | Daily at 2:00 AM |
| `0 */4 * * *` | Every 4 hours |
| `0 */6 * * *` | Every 6 hours |
| `0 0 * * 0` | Weekly on Sunday at midnight |
| `0 0 1 * *` | Monthly on the 1st at midnight |
| `30 0 4 * * *` | Daily at 4:00:30 AM (with optional seconds) |
| `@hourly` | Every hour |
| `@daily` | Daily at midnight |
| `@weekly` | Weekly on Sunday at midnight |

### Programmatic Scheduler APIs

The backup manager provides APIs for monitoring scheduled backups:

**GetNextScheduledBackup()** - Returns the next scheduled backup time:
```go
nextBackup, running := manager.GetNextScheduledBackup()
if running {
    fmt.Printf("Next backup at: %s\n", nextBackup.Format(time.RFC3339))
}
```

**IsSchedulerRunning()** - Check if the scheduler is active:
```go
if manager.IsSchedulerRunning() {
    fmt.Println("Backup scheduler is running")
}
```

**StopScheduledBackups()** - Gracefully stop scheduled backups:
```go
manager.StopScheduledBackups()
```

For detailed API documentation, see the [Backup and Recovery Operations Guide](architecture.md).

## Best Practices

### Backup Strategy

1. **Regular Full Backups**: Schedule full backups weekly or monthly
2. **Incremental Backups**: Use daily incremental backups for efficiency
3. **Verification**: Always verify backups after creation
4. **Retention Policy**: Implement a retention policy to manage storage
5. **Off-site Storage**: Store backups in a different location for disaster recovery

### Compression

- **zstd** (recommended): Best balance of compression ratio and speed
- **gzip**: Good compatibility, moderate compression
- **none**: Use for pre-compressed data or maximum speed

### Encryption

- Always encrypt backups containing sensitive data
- Use strong, randomly generated 32-byte keys for AES-256
- Store encryption keys securely (e.g., in a key management service)
- Never commit encryption keys to version control

### Storage

- **Local**: Fast, good for development and testing
- **S3**: Scalable, durable, recommended for production
- **Azure Blob Storage**: Enterprise-ready with Azure integration
- **GCS**: Native Google Cloud integration with ADC support
- **MinIO**: Self-hosted S3-compatible storage
- Use different storage backends for redundancy

See [Backup Storage Backends](engineering.md) for detailed configuration.

### Automated Backup Script

For environments where server-side scheduling is not preferred, use a shell script with cron:

```bash
#!/bin/bash
# automated-backup.sh

BACKUP_NAME="daily-$(date +%Y%m%d)"
STORAGE_PATH="/mnt/backup-drive/quicbaas"
ENCRYPT_KEY="your-32-byte-key-here-padded!!"

# Create backup
quicbaas backup create "$BACKUP_NAME" \
  --compression zstd \
  --encryption aes256 \
  --encrypt-key "$ENCRYPT_KEY" \
  --storage-path "$STORAGE_PATH" \
  --output json > "/var/log/quicbaas/backup-${BACKUP_NAME}.json"

# Verify backup
BACKUP_ID=$(jq -r '.BackupID' "/var/log/quicbaas/backup-${BACKUP_NAME}.json")
quicbaas backup verify "$BACKUP_ID" \
  --storage-path "$STORAGE_PATH" \
  --encrypt-key "$ENCRYPT_KEY"

# Delete old backups (keep last 7 days)
quicbaas backup list --storage-path "$STORAGE_PATH" --output json | \
  jq -r '.Backups[] | select(.Timestamp < (now - 604800)) | .ID' | \
  xargs -I {} quicbaas backup delete {} --yes --storage-path "$STORAGE_PATH"
```

## Error Handling

Common errors and solutions:

**"Encryption key must be exactly 32 bytes"**
- Solution: Ensure your encryption key is exactly 32 bytes long.

**"Backup not found"**
- Solution: Verify the backup ID and storage path are correct.

**"Checksum verification failed"**
- Solution: The backup may be corrupted. Try restoring from a different backup.

**"Permission denied"**
- Solution: Ensure you have read/write permissions to the storage path.

**"Scheduler not starting"**
- Solution: Verify the cron expression syntax is valid.

**"Scheduled backup not running"**
- Solution: Ensure `schedule.enabled` is set to `true` in the configuration.

## See Also

- CLI Overview
- [Backup and Recovery Operations Guide](architecture.md)
- [Backup Storage Backends](engineering.md)
- [Backup Credentials](user.md)
- [Security Guide](../security-hardening/user.md)
- Operations Guide
