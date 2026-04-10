# Backup Credentials

QuicBaaS supports a credentials file for managing multiple storage backend profiles.

## Credentials File Location

Default location: `~/.quicbaas/backup-credentials.yaml`

Override with: `--credentials-file /path/to/credentials.yaml`

## File Format

```yaml
# Default profile when --profile is not specified
default_profile: production-s3

profiles:
  # Profile name
  production-s3:
    type: s3
    bucket: quicbaas-backups
    region: us-west-2
    credentials:
      access_key: ${AWS_ACCESS_KEY_ID}
      secret_key: ${AWS_SECRET_ACCESS_KEY}
    config:
      endpoint: https://s3.example.com  # Optional
```

## Profile Configuration

### Common Fields

| Field | Required | Description |
|-------|----------|-------------|
| `type` | Yes | Storage type (local, s3, azure, gcs, minio) |
| `bucket` | Varies | Bucket name (s3, gcs, minio) |
| `container` | Varies | Container name (azure) |
| `region` | No | Region (s3, minio) |
| `credentials` | No | Credential key-value pairs |
| `config` | No | Additional configuration |

### S3 Profile

```yaml
production-s3:
  type: s3
  bucket: quicbaas-backups
  region: us-west-2
  credentials:
    access_key: ${AWS_ACCESS_KEY_ID}
    secret_key: ${AWS_SECRET_ACCESS_KEY}
    session_token: ${AWS_SESSION_TOKEN}  # Optional
  config:
    endpoint: https://s3.custom.com  # Optional
```

### Azure Profile

```yaml
staging-azure:
  type: azure
  container: quicbaas-backups
  credentials:
    account_name: ${AZURE_STORAGE_ACCOUNT}
    account_key: ${AZURE_STORAGE_KEY}
    # Or use connection string:
    # connection_string: ${AZURE_STORAGE_CONNECTION_STRING}
```

### GCS Profile

```yaml
production-gcs:
  type: gcs
  bucket: quicbaas-backups
  credentials:
    credentials_file: ${GOOGLE_APPLICATION_CREDENTIALS}
  config:
    project_id: my-gcp-project
```

### MinIO Profile

```yaml
local-minio:
  type: minio
  bucket: quicbaas-backups
  region: us-east-1
  credentials:
    access_key: ${MINIO_ACCESS_KEY}
    secret_key: ${MINIO_SECRET_KEY}
  config:
    endpoint: localhost:9000
    use_ssl: false
```

### Local Profile

```yaml
local-dev:
  type: local
  config:
    base_path: /var/lib/quicbaas/backups
```

## Environment Variable Expansion

Credentials support environment variable references using `${VAR}` or `$VAR` syntax.

```yaml
credentials:
  access_key: ${AWS_ACCESS_KEY_ID}      # Recommended
  secret_key: $AWS_SECRET_ACCESS_KEY    # Also works
```

Variables are expanded at runtime, allowing secure credential management through environment variables.

## Profile Selection

### Via CLI Flag

```bash
quicbaas backup create my-backup --profile production-s3
```

### Via Default Profile

Set `default_profile` in the credentials file:

```yaml
default_profile: production-s3
```

Then use without `--profile`:
```bash
quicbaas backup create my-backup --credentials-file ~/.quicbaas/backup-credentials.yaml
```

## Credential Resolution Priority

When multiple credential sources exist, QuicBaaS resolves them in order:

1. **CLI flags** (highest priority)
2. **Environment variables**
3. **Credentials file profile**
4. **Cloud provider defaults** (AWS chain, Azure DefaultCredential, GCP ADC)

Example: If `--bucket` is specified on CLI, it overrides the profile's bucket setting.

## Security Best Practices

### File Permissions

Restrict access to the credentials file:

```bash
chmod 600 ~/.quicbaas/backup-credentials.yaml
```

### Never Store Raw Credentials

Use environment variable references instead of hardcoded values:

```yaml
# Good - uses environment variable
credentials:
  access_key: ${AWS_ACCESS_KEY_ID}

# Bad - hardcoded credential
credentials:
  access_key: AKIAIOSFODNN7EXAMPLE
```

### Use Secrets Management

Populate environment variables from a secrets manager:

```bash
export AWS_ACCESS_KEY_ID=$(vault kv get -field=access_key secret/quicbaas)
export AWS_SECRET_ACCESS_KEY=$(vault kv get -field=secret_key secret/quicbaas)

quicbaas backup create my-backup --profile production-s3
```

### Separate Profiles for Environments

Use distinct profiles for different environments:

```yaml
profiles:
  production-s3:
    type: s3
    bucket: quicbaas-prod-backups
    # ...

  staging-s3:
    type: s3
    bucket: quicbaas-staging-backups
    # ...
```

### Use Temporary Credentials

For AWS, prefer temporary credentials with session tokens:

```yaml
credentials:
  access_key: ${AWS_ACCESS_KEY_ID}
  secret_key: ${AWS_SECRET_ACCESS_KEY}
  session_token: ${AWS_SESSION_TOKEN}
```

## Complete Example

```yaml
# ~/.quicbaas/backup-credentials.yaml
default_profile: production-s3

profiles:
  production-s3:
    type: s3
    bucket: quicbaas-prod-backups
    region: us-west-2
    credentials:
      access_key: ${AWS_ACCESS_KEY_ID}
      secret_key: ${AWS_SECRET_ACCESS_KEY}

  staging-azure:
    type: azure
    container: quicbaas-staging
    credentials:
      account_name: ${AZURE_STORAGE_ACCOUNT}
      account_key: ${AZURE_STORAGE_KEY}

  production-gcs:
    type: gcs
    bucket: quicbaas-gcs-backups
    credentials:
      credentials_file: ${GOOGLE_APPLICATION_CREDENTIALS}
    config:
      project_id: my-gcp-project

  local-minio:
    type: minio
    bucket: quicbaas-local
    credentials:
      access_key: ${MINIO_ACCESS_KEY}
      secret_key: ${MINIO_SECRET_KEY}
    config:
      endpoint: localhost:9000
      use_ssl: false

  local-dev:
    type: local
    config:
      base_path: /var/lib/quicbaas/backups
```

## See Also

- [Backup CLI Commands](../cli/backup.md)
- [Backup Storage Backends](backup-storage-backends.md)
- [Backup and Recovery Operations](backup-recovery.md)
