# Backup Storage

QuicBaaS's backup manager uses go-objstore for storing database backups to any supported object storage backend.

## Configuration

```yaml
object_storage:
  backends:
    backup-store:
      type: s3
      config:
        bucket: my-quicbaas-backups
        region: us-east-1
        access_key: ${AWS_ACCESS_KEY_ID}
        secret_key: ${AWS_SECRET_ACCESS_KEY}
        prefix: backups/

backup:
  enabled: true
  storage_backend: backup-store  # References named backend
  work_dir: /tmp/quicbaas-backup
  retention_days: 30
  compression: zstd
  schedule: "0 2 * * *"  # Daily at 2 AM
```

## Backup Options

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `enabled` | No | false | Enable backup functionality |
| `storage_backend` | Yes | - | Named backend from `object_storage.backends` |
| `work_dir` | No | /tmp | Temporary directory for backup operations |
| `retention_days` | No | 30 | Days to retain backups |
| `compression` | No | zstd | Compression algorithm (none, gzip, zstd) |
| `schedule` | No | - | Cron expression for scheduled backups |

## CLI Commands

### Create Backup

```bash
quicbaas backup create
```

### List Backups

```bash
quicbaas backup list
```

### Restore Backup

```bash
quicbaas backup restore --id <backup-id>
```

### Delete Backup

```bash
quicbaas backup delete --id <backup-id>
```

## Backup Types

### Full Backup

Complete snapshot of all data:

```bash
quicbaas backup create --type full
```

### Incremental Backup

Only changes since last backup:

```bash
quicbaas backup create --type incremental
```

## Compression

| Algorithm | Speed | Ratio | Use Case |
|-----------|-------|-------|----------|
| `none` | Fastest | 1:1 | When storage is cheap |
| `gzip` | Medium | Good | Compatibility |
| `zstd` | Fast | Best | Recommended default |

## Storage Organization

Backups are stored with the following key structure:

```
{prefix}/backups/{backup-id}/metadata.json
{prefix}/backups/{backup-id}/shard-{n}.data
```

## Multi-Cloud Backup

Store backups across multiple cloud providers for disaster recovery:

```yaml
object_storage:
  backends:
    primary-backup:
      type: s3
      config:
        bucket: quicbaas-backup-primary
        region: us-east-1

    secondary-backup:
      type: gcs
      config:
        bucket: quicbaas-backup-secondary
        project: my-project

backup:
  enabled: true
  storage_backend: primary-backup
  # Mirror to secondary (future feature)
```
