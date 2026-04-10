# Storage Backends

Configuration reference for each supported object storage backend.

## Local Filesystem

File-based storage for local or single-node deployments.

```yaml
object_storage:
  backends:
    local-store:
      type: local
      config:
        path: /var/lib/quicbaas/objects
```

| Field | Required | Description |
|-------|----------|-------------|
| `path` | Yes | Directory path for object storage |

## Amazon S3

AWS S3 or S3-compatible storage.

```yaml
object_storage:
  backends:
    s3-store:
      type: s3
      config:
        bucket: my-bucket
        region: us-east-1
        access_key: ${AWS_ACCESS_KEY_ID}
        secret_key: ${AWS_SECRET_ACCESS_KEY}
        prefix: quicbaas/
        endpoint: ""  # Optional, for custom endpoints
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `bucket` | Yes | - | S3 bucket name |
| `region` | Yes | - | AWS region |
| `access_key` | Yes | - | AWS access key ID |
| `secret_key` | Yes | - | AWS secret access key |
| `prefix` | No | "" | Key prefix for all objects |
| `endpoint` | No | AWS default | Custom S3 endpoint URL |

## MinIO

Self-hosted S3-compatible storage.

```yaml
object_storage:
  backends:
    minio-store:
      type: minio
      config:
        endpoint: minio.internal:9000
        bucket: my-bucket
        access_key: ${MINIO_ACCESS_KEY}
        secret_key: ${MINIO_SECRET_KEY}
        use_ssl: false
        prefix: data/
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `endpoint` | Yes | - | MinIO server endpoint |
| `bucket` | Yes | - | Bucket name |
| `access_key` | Yes | - | MinIO access key |
| `secret_key` | Yes | - | MinIO secret key |
| `use_ssl` | No | false | Enable TLS |
| `prefix` | No | "" | Key prefix |

## Google Cloud Storage

GCP object storage.

```yaml
object_storage:
  backends:
    gcs-store:
      type: gcs
      config:
        bucket: my-bucket
        project: my-project-id
        credentials_file: /path/to/credentials.json
        prefix: quicbaas/
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `bucket` | Yes | - | GCS bucket name |
| `project` | No | - | GCP project ID |
| `credentials_file` | No | - | Path to service account JSON |
| `prefix` | No | "" | Key prefix |

## Azure Blob Storage

Microsoft Azure blob storage.

```yaml
object_storage:
  backends:
    azure-store:
      type: azure
      config:
        account_name: mystorageaccount
        account_key: ${AZURE_STORAGE_KEY}
        container: my-container
        prefix: quicbaas/
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `account_name` | Yes | - | Azure storage account name |
| `account_key` | Yes | - | Azure storage account key |
| `container` | Yes | - | Blob container name |
| `prefix` | No | "" | Key prefix |

## Memory (Testing)

In-memory storage for development and testing.

```yaml
object_storage:
  backends:
    test-store:
      type: memory
```

No configuration required. Data is not persisted.

**Use cases:**
- Unit tests
- Integration tests
- Local development
- Ephemeral environments

## API Reference

### Storage Interface

All backends implement `common.Storage`:

```go
type Storage interface {
    Put(key string, data io.Reader) error
    Get(key string) (io.ReadCloser, error)
    Delete(key string) error
    List(prefix string) ([]string, error)
    Exists(ctx context.Context, key string) (bool, error)
}
```

### Factory Functions

```go
// Create backend by type
storage, err := config.CreateStorageBackendByType("s3", configMap)

// Get named backend from config
storage, err := config.GetStorageBackend("s3-primary", cfg)
```

## Security Best Practices

1. **Credentials** - Use environment variables or secrets management
2. **Encryption at Rest** - Enable server-side encryption on cloud backends
3. **Access Control** - Use IAM policies to restrict bucket access
4. **Network** - Use TLS for all communication
5. **Key Rotation** - Regularly rotate access keys
