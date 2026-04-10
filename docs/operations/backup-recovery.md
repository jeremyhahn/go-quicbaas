# Backup and Disaster Recovery Guide

This guide covers backup strategies, restore procedures, and disaster recovery planning for QuicBaaS clusters.

## Table of Contents

- [Backup Overview](#backup-overview)
- [Configuring Scheduled Backups](#configuring-scheduled-backups)
- [Scheduler Configuration Reference](#scheduler-configuration-reference)
- [Scheduler Management APIs](#scheduler-management-apis)
- [Manual Backup Procedures](#manual-backup-procedures)
- [Restore Procedures](#restore-procedures)
- [Disaster Recovery Planning](#disaster-recovery-planning)
- [Backup Encryption](#backup-encryption)
- [Testing Backups](#testing-backups)

---

## Backup Overview

QuicBaaS provides a comprehensive backup system that creates consistent snapshots of all data across Raft shards. Backups include all key-value data, shard metadata, and Raft state information necessary for complete cluster recovery.

### Backup Types

QuicBaaS supports two backup types:

| Type | Description | Use Case |
|------|-------------|----------|
| **Full** | Complete snapshot of all data | Initial backups, weekly/monthly archives |
| **Incremental** | Changes since the last backup | Daily backups, reduced storage requirements |

Full backups capture the entire database state and serve as the base for incremental backups. Incremental backups store only the delta from the previous backup, significantly reducing storage requirements and backup duration.

### Backup Storage Backends

QuicBaaS supports multiple storage backends for backup storage:

| Backend | Description | Configuration Key |
|---------|-------------|-------------------|
| **Local** | Local filesystem storage | `local` |
| **S3** | Amazon S3 or S3-compatible storage | `s3` |
| **GCS** | Google Cloud Storage | `gcs` |
| **Azure** | Azure Blob Storage | `azure` |
| **MinIO** | Self-hosted S3-compatible storage | `minio` |

#### Local Storage Configuration

```yaml
backup:
  storage_backend: local
  storage_config:
    path: /var/lib/quicbaas/backups
    prefix: backups
```

#### S3 Storage Configuration

```yaml
backup:
  storage_backend: s3
  storage_config:
    bucket: my-quicbaas-backups
    region: us-east-1
    prefix: backups
    # Optional: Use IAM role or specify credentials
    access_key_id: ${AWS_ACCESS_KEY_ID}
    secret_access_key: ${AWS_SECRET_ACCESS_KEY}
```

#### GCS Storage Configuration

```yaml
backup:
  storage_backend: gcs
  storage_config:
    bucket: my-quicbaas-backups
    prefix: backups
    # Uses Application Default Credentials or specify path
    credentials_file: /path/to/service-account.json
```

#### Azure Blob Storage Configuration

```yaml
backup:
  storage_backend: azure
  storage_config:
    container: quicbaas-backups
    account_name: mystorageaccount
    account_key: ${AZURE_STORAGE_KEY}
    prefix: backups
```

#### MinIO Configuration

```yaml
backup:
  storage_backend: minio
  storage_config:
    endpoint: minio.example.com:9000
    bucket: quicbaas-backups
    access_key: ${MINIO_ACCESS_KEY}
    secret_key: ${MINIO_SECRET_KEY}
    use_ssl: true
    prefix: backups
```

### Backup Components

Each backup contains:

- **Shard Snapshots**: Consistent snapshots for each Raft shard
- **Raft State**: Log index and term for each shard at backup time
- **Metadata**: Version, cluster ID, node ID, key counts, and timestamps
- **Checksums**: SHA-256 checksums for data integrity verification

### Raft Metadata in Backups

Each shard snapshot captures the exact Raft state at backup time, enabling precise recovery:

| Field | Type | Description |
|-------|------|-------------|
| `shard_id` | uint64 | Unique identifier for the Raft shard |
| `index` | uint64 | Raft log index at snapshot time |
| `term` | uint64 | Raft election term at snapshot time |
| `checksum` | string | SHA-256 checksum for integrity verification |
| `size` | int64 | Size of the shard snapshot in bytes |

The Raft index and term values are obtained via `GetShardRaftState()` and provide a consistency reference point. During restore, these values help verify that the backup represents a consistent cluster state.

Example shard metadata in backup:

```json
{
  "shards": {
    "1": {
      "shard_id": 1,
      "index": 12345,
      "term": 5,
      "checksum": "sha256:def456...",
      "size": 34952533
    },
    "2": {
      "shard_id": 2,
      "index": 12340,
      "term": 5,
      "checksum": "sha256:abc123...",
      "size": 28456789
    }
  }
}
```

---

## Configuring Scheduled Backups

Automated backups are configured through the schedule section of the backup configuration. QuicBaaS uses a cron-based scheduler built on the [robfig/cron](https://github.com/robfig/cron) library with panic recovery and graceful shutdown support.

### Cron Expression Syntax

QuicBaaS uses standard cron expressions with optional seconds:

```
second (optional, 0-59)
| minute (0-59)
| | hour (0-23)
| | | day of month (1-31)
| | | | month (1-12)
| | | | | day of week (0-6, Sunday=0)
| | | | | |
* * * * * *
```

Common cron expressions:

| Expression | Description |
|------------|-------------|
| `0 2 * * *` | Daily at 2:00 AM |
| `0 */6 * * *` | Every 6 hours |
| `0 0 * * 0` | Weekly on Sunday at midnight |
| `0 0 1 * *` | Monthly on the 1st at midnight |
| `0 0 1 */3 *` | Quarterly on the 1st at midnight |
| `30 0 4 * * *` | Daily at 4:00:30 AM (with seconds) |
| `0 30 */2 * * *` | Every 2 hours at 30 minutes past |
| `@hourly` | Every hour (descriptor syntax) |
| `@daily` | Daily at midnight |
| `@weekly` | Weekly on Sunday at midnight |
| `@monthly` | Monthly on the 1st at midnight |

---

## Scheduler Configuration Reference

### ScheduleConfig Struct

The `ScheduleConfig` struct defines all parameters for scheduled backups:

```go
type ScheduleConfig struct {
    // Enabled controls whether scheduled backups are active.
    Enabled bool

    // CronExpression defines the backup schedule using cron syntax.
    // Examples: "0 2 * * *" for daily at 2 AM, "0 */4 * * *" for every 4 hours.
    CronExpression string

    // BackupType specifies the type of backup to create.
    // Valid values: "full" or "incremental"
    BackupType BackupType

    // RetentionPolicy defines automated cleanup rules for old backups.
    RetentionPolicy *RetentionPolicy
}
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `Enabled` | bool | Yes | false | Enables or disables the backup scheduler |
| `CronExpression` | string | Yes (if enabled) | - | Cron expression defining backup schedule |
| `BackupType` | string | No | "full" | Type of backup: "full" or "incremental" |
| `RetentionPolicy` | object | No | nil | Automated retention policy configuration |

### RetentionPolicy Struct

The `RetentionPolicy` struct defines rules for automatic backup cleanup:

```go
type RetentionPolicy struct {
    // MaxBackups is the maximum number of backups to keep (0 = unlimited).
    MaxBackups int

    // MaxAge is the maximum age of backups to keep (0 = unlimited).
    MaxAge time.Duration

    // KeepDaily specifies how many daily backups to keep.
    KeepDaily int

    // KeepWeekly specifies how many weekly backups to keep.
    KeepWeekly int

    // KeepMonthly specifies how many monthly backups to keep.
    KeepMonthly int
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `MaxBackups` | int | 0 (unlimited) | Maximum number of backups to retain |
| `MaxAge` | duration | 0 (unlimited) | Maximum age before backup deletion |
| `KeepDaily` | int | 0 | Number of daily backups to preserve |
| `KeepWeekly` | int | 0 | Number of weekly backups to preserve |
| `KeepMonthly` | int | 0 | Number of monthly backups to preserve |

### YAML Configuration Examples

#### Minimal Scheduled Backup

```yaml
backup:
  storage_backend: local
  storage_config:
    path: /var/lib/quicbaas/backups
  schedule:
    enabled: true
    cron_expression: "0 2 * * *"
```

#### Full Configuration with Retention

```yaml
backup:
  storage_backend: s3
  storage_config:
    bucket: prod-quicbaas-backups
    region: us-east-1
    prefix: prod/backups
  default_compression: zstd
  default_encryption: aes-256-gcm
  encryption_key: ${BACKUP_ENCRYPTION_KEY}
  work_dir: /tmp/quicbaas-backup-work
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

#### Incremental Backup Strategy

```yaml
backup:
  storage_backend: s3
  storage_config:
    bucket: prod-quicbaas-backups
    region: us-east-1
  schedule:
    enabled: true
    cron_expression: "0 */2 * * *"  # Every 2 hours
    backup_type: incremental
    retention_policy:
      max_backups: 336  # 2 weeks of 2-hourly incrementals
      max_age: 336h     # 14 days
```

#### Development Environment

```yaml
backup:
  storage_backend: local
  storage_config:
    path: /var/lib/quicbaas/backups
  default_compression: gzip
  default_encryption: none
  timeout: 15m
  schedule:
    enabled: true
    cron_expression: "0 0 * * *"  # Daily at midnight
    backup_type: full
    retention_policy:
      max_backups: 7
      max_age: 168h  # 7 days
```

#### Cross-Region Backup Strategy

```yaml
# Primary region backup
backup:
  storage_backend: s3
  storage_config:
    bucket: us-east-1-quicbaas-backups
    region: us-east-1
    prefix: primary
  schedule:
    enabled: true
    cron_expression: "0 */2 * * *"  # Every 2 hours
    backup_type: full
    retention_policy:
      max_backups: 168  # 2 weeks of 2-hourly backups

# Configure S3 cross-region replication for DR copy
```

---

## Scheduler Management APIs

The backup manager provides APIs for monitoring and controlling the backup scheduler.

### GetNextScheduledBackup

Returns the next scheduled backup time.

```go
// GetNextScheduledBackup returns the next scheduled backup time.
// Returns (time.Time{}, false) if the scheduler is not running.
func (m *BackupManager) GetNextScheduledBackup() (time.Time, bool)
```

**Returns:**
- `time.Time`: The next scheduled backup time
- `bool`: True if the scheduler is running, false otherwise

**Example Usage:**

```go
manager, _ := backup.NewBackupManager(config, storage, raft, logger)

nextBackup, running := manager.GetNextScheduledBackup()
if running {
    fmt.Printf("Next backup scheduled at: %s\n", nextBackup.Format(time.RFC3339))
    fmt.Printf("Time until next backup: %s\n", time.Until(nextBackup))
} else {
    fmt.Println("Backup scheduler is not running")
}
```

### IsSchedulerRunning

Returns the current state of the backup scheduler.

```go
// IsSchedulerRunning returns true if the backup scheduler is running.
func (m *BackupManager) IsSchedulerRunning() bool
```

**Returns:**
- `bool`: True if the scheduler is actively running, false otherwise

**Example Usage:**

```go
manager, _ := backup.NewBackupManager(config, storage, raft, logger)

if manager.IsSchedulerRunning() {
    fmt.Println("Backup scheduler is active")

    // Get next scheduled time
    if next, ok := manager.GetNextScheduledBackup(); ok {
        fmt.Printf("Next backup: %s\n", next.Format(time.RFC3339))
    }
} else {
    fmt.Println("Backup scheduler is stopped or not configured")
}
```

### StopScheduledBackups

Gracefully stops the cron scheduler.

```go
// StopScheduledBackups stops the cron scheduler.
// Waits for any running backup jobs to complete before returning.
func (m *BackupManager) StopScheduledBackups()
```

**Example Usage:**

```go
// Stop scheduled backups before maintenance
manager.StopScheduledBackups()
fmt.Println("Scheduled backups stopped")

// Perform maintenance...

// To re-enable, create a new manager with schedule config
```

### ApplyRetentionPolicy

Manually triggers retention policy execution.

```go
// ApplyRetentionPolicy applies the configured retention policy.
// This is automatically called after each scheduled backup.
func (m *BackupManager) ApplyRetentionPolicy(ctx context.Context) error
```

**Example Usage:**

```go
ctx := context.Background()

// Manually apply retention policy
if err := manager.ApplyRetentionPolicy(ctx); err != nil {
    log.Printf("Retention policy error: %v", err)
}
```

### Programmatic Scheduler Control

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/jeremyhahn/go-quicbaas/pkg/backup"
    "github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func main() {
    // Create configuration with scheduling
    config := &backup.Config{
        StorageBackend: "local",
        StorageConfig: map[string]string{
            "path": "/var/lib/quicbaas/backups",
        },
        DefaultCompression: backup.CompressionZstd,
        Timeout:            30 * time.Minute,
        Schedule: &backup.ScheduleConfig{
            Enabled:        true,
            CronExpression: "0 */4 * * *", // Every 4 hours
            BackupType:     backup.BackupTypeFull,
            RetentionPolicy: &backup.RetentionPolicy{
                MaxBackups: 30,
                MaxAge:     720 * time.Hour, // 30 days
            },
        },
    }

    // Initialize backup manager (scheduler starts automatically)
    manager, err := backup.NewBackupManager(config, storage, raft, log)
    if err != nil {
        panic(err)
    }
    defer manager.Close()

    // Check scheduler status
    if manager.IsSchedulerRunning() {
        fmt.Println("Scheduler is running")

        // Get next backup time
        if next, ok := manager.GetNextScheduledBackup(); ok {
            fmt.Printf("Next backup at: %s\n", next.Format(time.RFC3339))
            fmt.Printf("Time remaining: %s\n", time.Until(next).Round(time.Second))
        }
    }

    // Manually trigger a backup outside the schedule
    ctx := context.Background()
    backup, err := manager.CreateBackup(ctx, backup.BackupTypeFull)
    if err != nil {
        panic(err)
    }
    fmt.Printf("Manual backup created: %s\n", backup.ID)

    // The scheduler continues running in the background...
}
```

---

## Manual Backup Procedures

### CLI Commands for Backup

#### Create a Full Backup

```bash
# Basic full backup with default compression (zstd)
quicbaas backup create my-backup

# Full backup with gzip compression
quicbaas backup create my-backup --compression gzip

# Full backup with no compression
quicbaas backup create my-backup --compression none

# Encrypted backup with AES-256
quicbaas backup create my-backup --encryption aes256 \
  --encrypt-key "your-32-byte-encryption-key!!"

# Backup to custom storage path
quicbaas backup create my-backup --storage-path /data/backups
```

#### Create an Incremental Backup

```bash
# Incremental backup (based on last full backup)
quicbaas backup create my-incremental --incremental

# Encrypted incremental backup
quicbaas backup create my-incremental --incremental \
  --encryption aes256 \
  --encrypt-key "your-32-byte-encryption-key!!"
```

#### List Available Backups

```bash
# List all backups as table (default)
quicbaas backup list

# List backups as JSON
quicbaas backup list --output json

# List first 10 backups
quicbaas backup list --limit 10

# List backups from specific storage path
quicbaas backup list --storage-path /data/backups
```

Example output:

```
BACKUP ID                            TYPE         SIZE      COMPRESSION  ENCRYPTION  SHARDS  KEYS    TIMESTAMP
backup-prod-1729123456               full         100 MB    zstd         none        3       10000   2025-10-16T18:30:56Z
backup-prod-1729209856               incremental  10 MB     zstd         aes256      3       1000    2025-10-17T18:30:56Z
```

#### Delete a Backup

```bash
# Delete with confirmation prompt
quicbaas backup delete backup-prod-1729123456

# Delete without confirmation
quicbaas backup delete backup-prod-1729123456 --yes
```

### Verifying Backup Integrity

Always verify backup integrity before relying on a backup for disaster recovery:

```bash
# Verify a backup
quicbaas backup verify backup-prod-1729123456

# Verify with JSON output
quicbaas backup verify backup-prod-1729123456 --output json

# Verify encrypted backup
quicbaas backup verify backup-prod-1729123456 \
  --encrypt-key "your-32-byte-encryption-key!!"
```

Verification checks:

1. Backup metadata validity
2. All shard snapshots present
3. SHA-256 checksums for all data
4. Compression format readability
5. Encryption headers (if encrypted)
6. Data structure integrity

Example verification output:

```
Verifying backup 'backup-prod-1729123456'...
  Checking backup metadata...
  Verifying backup checksum...
  Checking shard snapshots...
  Verifying shard 1 of 3...
  Verifying shard 2 of 3...
  Verifying shard 3 of 3...
  Verification complete!

BACKUP ID                  VALID   CHECKSUM  SHARDS  ISSUES  TIMESTAMP
backup-prod-1729123456     true    valid     3       0       2025-10-17T10:00:00Z
```

### REST API for Backups

Backup operations are also available through the REST API. The following endpoints are supported:

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/v1/backups` | Create a new backup |
| `GET` | `/api/v1/backups` | List all backups |
| `DELETE` | `/api/v1/backups/{id}` | Delete a backup |
| `POST` | `/api/v1/backups/{id}/restore` | Restore from a backup |
| `POST` | `/api/v1/backups/{id}/verify` | Verify backup integrity |
| `GET` | `/api/v1/backups/{id}/status` | Get backup status |

#### Verify via REST

```bash
# Verify backup integrity
curl -X POST http://localhost:8080/api/v1/backups/backup-prod-1729123456/verify
```

Response:
```json
{
  "valid": true,
  "checksum_match": true,
  "issues": []
}
```

#### Get Backup Status via REST

```bash
# Get backup status including integrity information
curl http://localhost:8080/api/v1/backups/backup-prod-1729123456/status
```

Response:
```json
{
  "backup_id": "backup-prod-1729123456",
  "valid": true,
  "checksum_match": true,
  "issues": []
}
```

For full REST API documentation including request/response schemas for all backup endpoints, see the [REST API Reference](../api/rest.md#backup-operations).

---

## Restore Procedures

### Full Cluster Restore

Use this procedure when recovering from complete cluster failure or setting up a new cluster from backup.

#### Prerequisites

1. New cluster nodes are initialized but not yet containing data
2. Backup storage is accessible from all nodes
3. Encryption key is available (if backup is encrypted)

#### Procedure

```bash
# Step 1: Stop all QuicBaaS instances
systemctl stop quicbaas

# Step 2: Clear existing data directories (if any)
rm -rf /var/lib/quicbaas/data/*

# Step 3: Verify backup integrity
quicbaas backup verify backup-prod-1729123456 \
  --storage-path s3://prod-quicbaas-backups/backups

# Step 4: Restore from backup on each node
quicbaas backup restore backup-prod-1729123456 \
  --verify \
  --storage-path s3://prod-quicbaas-backups/backups

# Step 5: Start QuicBaaS instances
systemctl start quicbaas

# Step 6: Verify cluster health
quicbaas cluster status
```

#### Restore with Encryption Key

```bash
quicbaas backup restore backup-prod-1729123456 \
  --verify \
  --encrypt-key "your-32-byte-encryption-key!!" \
  --storage-path s3://prod-quicbaas-backups/backups
```

### Single Node Recovery

Use this procedure when a single node fails and needs to rejoin the cluster.

#### Procedure

```bash
# Step 1: Stop the failed node
systemctl stop quicbaas

# Step 2: Clear node data directory
rm -rf /var/lib/quicbaas/data/*

# Step 3: Restore from latest backup
quicbaas backup restore backup-prod-1729123456 \
  --verify

# Step 4: Start the node (it will sync with cluster)
systemctl start quicbaas

# Step 5: Monitor sync progress
quicbaas node status
```

The node will automatically:
1. Restore data from the backup
2. Rejoin the Raft cluster
3. Catch up with any changes since the backup was taken

### Point-in-Time Recovery

Point-in-time recovery allows restoring to a specific timestamp, useful for recovering from data corruption or accidental deletions.

```bash
# Restore to specific timestamp (RFC3339 format)
quicbaas backup restore backup-prod-1729123456 \
  --point-in-time 2025-10-14T12:00:00Z \
  --verify

# Restore without overwriting existing keys
quicbaas backup restore backup-prod-1729123456 \
  --point-in-time 2025-10-14T12:00:00Z \
  --skip-existing
```

Note: Point-in-time recovery requires:
- A backup taken before the target timestamp
- Transaction logs from the backup point to the target timestamp

### Selective Restore Options

```bash
# Skip keys that already exist (merge mode)
quicbaas backup restore backup-prod-1729123456 --skip-existing

# Restore without checksum verification (faster, not recommended)
quicbaas backup restore backup-prod-1729123456 --verify=false
```

---

## Disaster Recovery Planning

### RPO/RTO Considerations

| Metric | Definition | QuicBaaS Guidelines |
|--------|------------|---------------------|
| **RPO** (Recovery Point Objective) | Maximum acceptable data loss | Determined by backup frequency |
| **RTO** (Recovery Time Objective) | Maximum acceptable downtime | Depends on cluster size and backup size |

#### RPO Planning Table

| Backup Frequency | Maximum Data Loss | Storage Cost |
|------------------|-------------------|--------------|
| Every 15 minutes | 15 minutes | Very High |
| Hourly | 1 hour | High |
| Every 4 hours | 4 hours | Medium |
| Daily | 24 hours | Low |

#### RTO Estimation

| Cluster Size | Backup Size | Estimated Restore Time |
|--------------|-------------|------------------------|
| 3 nodes | 10 GB | 5-10 minutes |
| 3 nodes | 100 GB | 30-60 minutes |
| 5 nodes | 500 GB | 2-4 hours |
| 7 nodes | 1 TB | 4-8 hours |

### Cross-Region Backup Strategies

#### Active-Passive DR

```
                         PRIMARY REGION (US-EAST-1)
  +----------+  +----------+  +----------+
  |  Node 1  |  |  Node 2  |  |  Node 3  |
  +----+-----+  +----+-----+  +----+-----+
       |             |             |
       +-------------+-------------+
                     |
                     v
            +-----------------+
            |   S3 Backups    |-----> S3 Cross-Region Replication
            |  (us-east-1)    |
            +-----------------+
                                           |
                                           v
                          DR REGION (US-WEST-2)
            +-----------------+
            |   S3 Backups    |<--- Replicated from us-east-1
            |  (us-west-2)    |
            +--------+--------+
                     |
                     v (On failover)
  +----------+  +----------+  +----------+
  |  Node 1  |  |  Node 2  |  |  Node 3  |
  +----------+  +----------+  +----------+
```

#### Configuration for Cross-Region Replication

```yaml
# Primary region configuration
backup:
  storage_backend: s3
  storage_config:
    bucket: quicbaas-backups-us-east-1
    region: us-east-1
  schedule:
    enabled: true
    cron_expression: "0 */2 * * *"
    backup_type: full
```

Enable S3 cross-region replication:

```bash
# AWS CLI to enable cross-region replication
aws s3api put-bucket-replication \
  --bucket quicbaas-backups-us-east-1 \
  --replication-configuration file://replication.json
```

### Failover Procedures

#### Automated Failover Checklist

1. Detect primary cluster failure (monitoring/alerting)
2. Verify backup availability in DR region
3. Identify latest valid backup
4. Provision DR cluster infrastructure
5. Restore from backup
6. Update DNS/load balancer
7. Verify application connectivity
8. Monitor DR cluster health

#### Manual Failover Script

```bash
#!/bin/bash
# DR Failover Script

DR_REGION="us-west-2"
BACKUP_BUCKET="quicbaas-backups-${DR_REGION}"

# Find latest backup
LATEST_BACKUP=$(quicbaas backup list --storage-path s3://${BACKUP_BUCKET} \
  --output json | jq -r '.backups[0].id')

echo "Latest backup: ${LATEST_BACKUP}"

# Verify backup integrity
quicbaas backup verify ${LATEST_BACKUP} \
  --storage-path s3://${BACKUP_BUCKET}

# Restore to DR cluster
for node in dr-node-1 dr-node-2 dr-node-3; do
  ssh ${node} "quicbaas backup restore ${LATEST_BACKUP} \
    --storage-path s3://${BACKUP_BUCKET} \
    --verify"
done

# Start DR cluster
for node in dr-node-1 dr-node-2 dr-node-3; do
  ssh ${node} "systemctl start quicbaas"
done

# Verify cluster health
quicbaas cluster status --endpoint dr-node-1:2379

echo "Failover complete. Update DNS records."
```

---

## Backup Encryption

### Enabling Encryption

QuicBaaS uses AES-256-GCM encryption (FIPS 140-2 approved) for backup encryption.

#### Generate Encryption Key

```bash
# Generate a secure 32-byte key
openssl rand -base64 32 | tr -d '\n' > /etc/quicbaas/backup-key

# Or using QuicBaaS KMS
quicbaas kms key create backup-encryption --size 256
quicbaas kms key export backup-encryption --format base64
```

#### Configure Encrypted Backups

```yaml
backup:
  default_encryption: aes-256-gcm
  encryption_key: ${BACKUP_ENCRYPTION_KEY}
  # Or specify key file
  # encryption_key_file: /etc/quicbaas/backup-key
```

#### Create Encrypted Backup via CLI

```bash
quicbaas backup create prod-backup \
  --encryption aes256 \
  --encrypt-key "$(cat /etc/quicbaas/backup-key)"
```

### Key Management for Backups

#### Best Practices

1. **Key Rotation**: Rotate encryption keys periodically (quarterly recommended)
2. **Key Storage**: Store keys in a secure vault (HashiCorp Vault, AWS KMS, Azure Key Vault)
3. **Key Backup**: Backup encryption keys separately from database backups
4. **Access Control**: Limit access to encryption keys using IAM policies

#### Using QuicBaaS KMS Integration

```yaml
backup:
  encryption:
    provider: quicbaas-kms
    key_id: backup-encryption-key
    auto_rotate: true
    rotation_period: 90d
```

#### Using AWS KMS

```yaml
backup:
  encryption:
    provider: aws-kms
    key_id: arn:aws:kms:us-east-1:123456789012:key/12345678-1234-1234-1234-123456789012
    region: us-east-1
```

#### Using HashiCorp Vault

```yaml
backup:
  encryption:
    provider: vault
    vault_addr: https://vault.example.com:8200
    vault_path: secret/quicbaas/backup-key
    vault_token: ${VAULT_TOKEN}
```

### Decryption for Restore

To restore an encrypted backup, you must provide the same encryption key used during backup:

```bash
# Restore encrypted backup
quicbaas backup restore backup-prod-1729123456 \
  --encrypt-key "$(cat /etc/quicbaas/backup-key)" \
  --verify

# Verify encrypted backup
quicbaas backup verify backup-prod-1729123456 \
  --encrypt-key "$(cat /etc/quicbaas/backup-key)"
```

**Warning**: If the encryption key is lost, encrypted backups cannot be recovered.

---

## Testing Backups

Regular backup testing is critical for ensuring disaster recovery readiness.

### Regular Restore Testing

#### Monthly Restore Test Procedure

1. **Select Test Backup**
   ```bash
   # List recent backups
   quicbaas backup list --limit 5
   ```

2. **Provision Test Environment**
   ```bash
   # Create isolated test cluster (Docker example)
   docker-compose -f docker-compose.test.yml up -d
   ```

3. **Restore to Test Environment**
   ```bash
   quicbaas backup restore backup-prod-1729123456 \
     --verify \
     --storage-path s3://prod-quicbaas-backups
   ```

4. **Validate Data Integrity**
   ```bash
   # Run data validation queries
   quicbaas data get users user1
   quicbaas data scan users --limit 100

   # Compare key counts
   quicbaas cluster stats
   ```

5. **Document Results**
   - Restore duration
   - Data validation results
   - Any issues encountered

6. **Cleanup Test Environment**
   ```bash
   docker-compose -f docker-compose.test.yml down -v
   ```

### Validation Procedures

#### Automated Validation Script

```bash
#!/bin/bash
# backup-validation.sh

set -e

BACKUP_ID=$1
STORAGE_PATH=$2
ENCRYPTION_KEY=$3

echo "=== Backup Validation: ${BACKUP_ID} ==="

# Step 1: Verify backup structure
echo "Step 1: Verifying backup integrity..."
if [ -n "${ENCRYPTION_KEY}" ]; then
  quicbaas backup verify ${BACKUP_ID} \
    --storage-path ${STORAGE_PATH} \
    --encrypt-key "${ENCRYPTION_KEY}" \
    --output json > /tmp/verify-result.json
else
  quicbaas backup verify ${BACKUP_ID} \
    --storage-path ${STORAGE_PATH} \
    --output json > /tmp/verify-result.json
fi

VALID=$(jq -r '.valid' /tmp/verify-result.json)
if [ "${VALID}" != "true" ]; then
  echo "FAILED: Backup verification failed"
  jq '.errors' /tmp/verify-result.json
  exit 1
fi
echo "PASSED: Backup integrity verified"

# Step 2: Check backup metadata
echo "Step 2: Checking backup metadata..."
SHARDS=$(jq -r '.shards_valid' /tmp/verify-result.json)
echo "  Shards validated: ${SHARDS}"

# Step 3: Verify checksum
CHECKSUM=$(jq -r '.checksum_valid' /tmp/verify-result.json)
if [ "${CHECKSUM}" != "true" ]; then
  echo "FAILED: Checksum validation failed"
  exit 1
fi
echo "PASSED: Checksum validated"

echo "=== Validation Complete: All checks passed ==="
```

#### Integration with CI/CD

```yaml
# .github/workflows/backup-test.yml
name: Backup Validation

on:
  schedule:
    - cron: '0 6 * * 1'  # Weekly on Monday at 6 AM

jobs:
  validate-backup:
    runs-on: ubuntu-latest
    steps:
      - name: Get latest backup ID
        id: get-backup
        run: |
          BACKUP_ID=$(quicbaas backup list --output json | jq -r '.backups[0].id')
          echo "backup_id=${BACKUP_ID}" >> $GITHUB_OUTPUT

      - name: Verify backup
        run: |
          quicbaas backup verify ${{ steps.get-backup.outputs.backup_id }} \
            --storage-path s3://prod-quicbaas-backups

      - name: Test restore
        run: |
          docker-compose up -d quicbaas-test
          quicbaas backup restore ${{ steps.get-backup.outputs.backup_id }} \
            --verify \
            --storage-path s3://prod-quicbaas-backups

      - name: Validate data
        run: |
          ./scripts/validate-data.sh

      - name: Cleanup
        if: always()
        run: docker-compose down -v
```

### Backup Monitoring Metrics

Monitor these Prometheus metrics for backup health:

| Metric | Description | Alert Threshold |
|--------|-------------|-----------------|
| `quicbaas_backup_backups_total` | Total backups created | N/A |
| `quicbaas_backup_backup_errors_total` | Backup failures | > 0 |
| `quicbaas_backup_backup_duration_seconds` | Backup duration | > 2x normal |
| `quicbaas_backup_backup_size_bytes` | Backup size | Unexpected growth |
| `quicbaas_backup_last_backup_timestamp_seconds` | Last successful backup | > backup_interval |
| `quicbaas_backup_scheduled_backups_total` | Scheduled backup executions | N/A |
| `quicbaas_backup_scheduled_backup_errors_total` | Scheduled backup failures | > 0 |
| `quicbaas_backup_retention_executions_total` | Retention policy executions | N/A |
| `quicbaas_backup_restore_errors_total` | Restore failures | > 0 |
| `quicbaas_backup_active_backups` | Backups in progress | > 1 (concurrent) |

#### Example Prometheus Alerting Rules

```yaml
groups:
  - name: quicbaas-backup
    rules:
      - alert: BackupFailed
        expr: increase(quicbaas_backup_backup_errors_total[1h]) > 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "QuicBaaS backup failed"
          description: "Backup operation failed in the last hour"

      - alert: ScheduledBackupFailed
        expr: increase(quicbaas_backup_scheduled_backup_errors_total[1h]) > 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "QuicBaaS scheduled backup failed"
          description: "Scheduled backup operation failed in the last hour"

      - alert: BackupOverdue
        expr: time() - quicbaas_backup_last_backup_timestamp_seconds > 28800
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: "QuicBaaS backup overdue"
          description: "No successful backup in the last 8 hours"

      - alert: BackupDurationHigh
        expr: histogram_quantile(0.95, quicbaas_backup_backup_duration_seconds_bucket) > 3600
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "QuicBaaS backup taking too long"
          description: "95th percentile backup duration exceeds 1 hour"
```

---

## Appendix

### Backup File Structure

```
backups/
└── backup-abc123/
    ├── data                    # Compressed/encrypted backup data
    └── metadata.json           # Backup metadata (JSON)
```

### Metadata JSON Schema

```json
{
  "id": "backup-abc123",
  "type": "full",
  "timestamp": "2025-10-17T10:00:00Z",
  "checksum": "sha256:abc123...",
  "size": 104857600,
  "compression": "zstd",
  "encryption": "aes-256-gcm",
  "metadata": {
    "version": "1.0.0",
    "node_id": 1,
    "cluster_id": "prod-cluster",
    "num_keys": 10000,
    "num_shards": 3,
    "duration": "5m30s",
    "storage_backend": "s3"
  },
  "shards": {
    "1": {
      "shard_id": 1,
      "index": 12345,
      "term": 5,
      "checksum": "sha256:def456...",
      "size": 34952533
    },
    "2": {
      "shard_id": 2,
      "index": 12340,
      "term": 5,
      "checksum": "sha256:ghi789...",
      "size": 28456789
    },
    "3": {
      "shard_id": 3,
      "index": 12348,
      "term": 5,
      "checksum": "sha256:jkl012...",
      "size": 41448278
    }
  }
}
```

### ShardSnapshot Structure

Each shard snapshot contains:

```json
{
  "shard_id": 1,
  "index": 12345,
  "term": 5,
  "data": "<base64-encoded snapshot data>",
  "checksum": "sha256:def456...",
  "size": 34952533
}
```

| Field | Description |
|-------|-------------|
| `shard_id` | Raft shard identifier |
| `index` | Raft log index at snapshot time (from GetShardRaftState) |
| `term` | Raft election term at snapshot time (from GetShardRaftState) |
| `data` | Serialized key-value data for the shard |
| `checksum` | SHA-256 checksum of the data |
| `size` | Size in bytes |

### Troubleshooting

| Issue | Cause | Solution |
|-------|-------|----------|
| Backup fails with timeout | Large dataset or slow storage | Increase `timeout` in config |
| Checksum verification fails | Data corruption | Retry backup, check storage health |
| Restore fails with key error | Wrong encryption key | Verify encryption key matches backup |
| Storage access denied | IAM permissions | Check storage backend credentials |
| Backup size unexpectedly large | Compression disabled | Enable compression (`zstd` recommended) |
| Scheduler not starting | Invalid cron expression | Verify cron syntax |
| Scheduled backup not running | `enabled: false` in config | Set `schedule.enabled: true` |
| Retention not cleaning up | No retention policy | Configure `retention_policy` |
