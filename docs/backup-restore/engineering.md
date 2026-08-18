# Backup & Restore — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/backup-restore/`.

QuicBaaS supports multiple storage backends for backup operations.

## Supported Backends

| Backend | Type Flag | Description |
|---------|-----------|-------------|
| Local | `local` | Local filesystem storage |
| AWS S3 | `s3` | Amazon S3 or S3-compatible storage |
| Azure | `azure` | Azure Blob Storage |
| GCS | `gcs` | Google Cloud Storage |
| MinIO | `minio` | Self-hosted S3-compatible storage |

## Local Storage

Local filesystem storage for development and testing.

**Required Options:**
- `--storage-path`: Directory path for backups (default: `/var/lib/quicbaas/backups`)

**Example:**
```bash
quicbaas backup create my-backup \
  --storage-type local \
  --storage-path /var/lib/quicbaas/backups
```

## AWS S3

Amazon S3 storage for production workloads.

**Required Options:**
- `--bucket`: S3 bucket name

**Optional Options:**
- `--region`: AWS region (default: `us-east-1`)
- `--endpoint`: Custom S3-compatible endpoint

**Environment Variables:**
- `AWS_ACCESS_KEY_ID`: Access key ID
- `AWS_SECRET_ACCESS_KEY`: Secret access key
- `AWS_SESSION_TOKEN`: Session token (for temporary credentials)
- `AWS_REGION` / `AWS_DEFAULT_REGION`: Default region
- `AWS_ENDPOINT`: Custom endpoint

**Example:**
```bash
# Using environment variables
export AWS_ACCESS_KEY_ID="AKIAIOSFODNN7EXAMPLE"
export AWS_SECRET_ACCESS_KEY="wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"

quicbaas backup create my-backup \
  --storage-type s3 \
  --bucket quicbaas-backups \
  --region us-west-2
```

**IAM Policy (Minimum Permissions):**
```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:PutObject",
        "s3:DeleteObject",
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::quicbaas-backups",
        "arn:aws:s3:::quicbaas-backups/*"
      ]
    }
  ]
}
```

## Azure Blob Storage

Azure Blob Storage for enterprise Azure environments.

**Required Options:**
- `--container`: Azure blob container name

**Environment Variables:**
- `AZURE_STORAGE_ACCOUNT`: Storage account name
- `AZURE_STORAGE_KEY`: Storage account key
- `AZURE_STORAGE_CONNECTION_STRING`: Full connection string (alternative)
- `AZURE_STORAGE_CONTAINER`: Default container name

**Example:**
```bash
# Using account name and key
export AZURE_STORAGE_ACCOUNT="mystorageaccount"
export AZURE_STORAGE_KEY="base64encodedkey=="

quicbaas backup create my-backup \
  --storage-type azure \
  --container quicbaas-backups
```

```bash
# Using connection string
export AZURE_STORAGE_CONNECTION_STRING="DefaultEndpointsProtocol=https;AccountName=..."

quicbaas backup create my-backup \
  --storage-type azure \
  --container quicbaas-backups
```

## Google Cloud Storage

GCS for Google Cloud environments with Application Default Credentials (ADC) support.

**Required Options:**
- `--bucket`: GCS bucket name

**Environment Variables:**
- `GOOGLE_APPLICATION_CREDENTIALS`: Path to service account JSON file
- `GOOGLE_CLOUD_PROJECT` / `GCS_PROJECT_ID`: Project ID
- `GCS_BUCKET` / `GOOGLE_CLOUD_BUCKET`: Default bucket name

**Example:**
```bash
# Using service account credentials
export GOOGLE_APPLICATION_CREDENTIALS="/path/to/service-account.json"

quicbaas backup create my-backup \
  --storage-type gcs \
  --bucket quicbaas-backups
```

**Service Account Permissions:**
- `storage.objects.create`
- `storage.objects.get`
- `storage.objects.delete`
- `storage.objects.list`
- `storage.buckets.get`

## MinIO

Self-hosted S3-compatible storage.

**Required Options:**
- `--bucket`: Bucket name
- `--endpoint`: MinIO server endpoint (e.g., `localhost:9000`)

**Optional Options:**
- `--region`: Region (optional)

**Environment Variables:**
- `MINIO_ACCESS_KEY` / `MINIO_ROOT_USER`: Access key
- `MINIO_SECRET_KEY` / `MINIO_ROOT_PASSWORD`: Secret key
- `MINIO_ENDPOINT`: Server endpoint
- `MINIO_BUCKET`: Default bucket
- `MINIO_REGION`: Region
- `MINIO_USE_SSL`: Enable SSL (`true`/`false`)

**Example:**
```bash
# Local MinIO instance
export MINIO_ACCESS_KEY="minioadmin"
export MINIO_SECRET_KEY="minioadmin"

quicbaas backup create my-backup \
  --storage-type minio \
  --bucket quicbaas-backups \
  --endpoint localhost:9000
```

```bash
# MinIO with SSL
quicbaas backup create my-backup \
  --storage-type minio \
  --bucket quicbaas-backups \
  --endpoint minio.example.com:9000
```

## Custom S3-Compatible Endpoints

Use the `--endpoint` flag with `--storage-type s3` for S3-compatible services.

**Example (Wasabi):**
```bash
quicbaas backup create my-backup \
  --storage-type s3 \
  --bucket quicbaas-backups \
  --region us-east-1 \
  --endpoint s3.wasabisys.com
```

**Example (DigitalOcean Spaces):**
```bash
quicbaas backup create my-backup \
  --storage-type s3 \
  --bucket quicbaas-backups \
  --region nyc3 \
  --endpoint nyc3.digitaloceanspaces.com
```

## Using Profiles

Instead of specifying credentials via environment variables, use a credentials file with named profiles.

```bash
# Use default profile
quicbaas backup create my-backup --profile production-s3

# Use specific credentials file
quicbaas backup create my-backup \
  --credentials-file /etc/quicbaas/credentials.yaml \
  --profile staging-azure
```

See Backup Credentials for credentials file configuration.

## See Also

- [Backup CLI Commands](cli.md)
- Backup Credentials
- Backup and Recovery Operations
